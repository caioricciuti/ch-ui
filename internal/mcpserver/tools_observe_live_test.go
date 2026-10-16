package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// Against a real ClickHouse with the exporter's tables: every observe tool
// runs under readonly=1 (the builders' SETTINGS clause is cut) and returns
// the rows that were written.
func TestObserveToolsLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	user, password := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if user == "" {
		user = "default"
	}
	client := connector.NewCHClient(endpoint, false)
	execute := func(sql string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := client.Execute(ctx, sql, user, password, nil)
		return err
	}
	otel := fmt.Sprintf("chui_mcp_otel_%d", time.Now().UnixNano())
	t.Cleanup(func() {
		if err := execute("DROP DATABASE IF EXISTS " + otel); err != nil {
			t.Error(err)
		}
	})
	now := time.Now().UTC().Add(-5 * time.Minute).Format("2006-01-02 15:04:05")
	for _, sql := range []string{
		"CREATE DATABASE " + otel,
		`CREATE TABLE ` + otel + `.otel_logs (
			Timestamp DateTime64(9), TraceId String, SpanId String, SeverityText LowCardinality(String), SeverityNumber UInt8,
			ServiceName LowCardinality(String), Body String, ResourceAttributes Map(LowCardinality(String), String),
			ScopeName String, ScopeAttributes Map(LowCardinality(String), String), LogAttributes Map(LowCardinality(String), String), EventName String
		) ENGINE = MergeTree ORDER BY (ServiceName, Timestamp)`,
		`CREATE TABLE ` + otel + `.otel_traces (
			Timestamp DateTime64(9), TraceId String, SpanId String, ParentSpanId String, SpanName LowCardinality(String),
			SpanKind LowCardinality(String), ServiceName LowCardinality(String), ResourceAttributes Map(LowCardinality(String), String),
			SpanAttributes Map(LowCardinality(String), String), Duration UInt64, StatusCode LowCardinality(String), StatusMessage String,
			Events Nested(Timestamp DateTime64(9), Name LowCardinality(String), Attributes Map(LowCardinality(String), String)),
			Links Nested(TraceId String, SpanId String, TraceState String, Attributes Map(LowCardinality(String), String))
		) ENGINE = MergeTree ORDER BY (ServiceName, Timestamp)`,
		`CREATE MATERIALIZED VIEW ` + otel + `.errors_mv ENGINE = MergeTree ORDER BY Timestamp AS SELECT Timestamp, Body FROM ` + otel + `.otel_logs WHERE SeverityText = 'ERROR'`,
		`INSERT INTO ` + otel + `.otel_logs (Timestamp, TraceId, SpanId, SeverityText, ServiceName, Body, LogAttributes) VALUES
			('` + now + `', 't1', 'b', 'ERROR', 'api', 'payment failed', {'order':'42'}),
			('` + now + `', '', '', 'INFO', 'web', 'hello', {})`,
		`INSERT INTO ` + otel + `.otel_traces (Timestamp, TraceId, SpanId, ParentSpanId, SpanName, SpanKind, ServiceName, Duration, StatusCode) VALUES
			('` + now + `', 't1', 'a', '', 'GET /pay', 'Server', 'web', 9000000, 'Ok'),
			('` + now + `', 't1', 'b', 'a', 'charge', 'Client', 'api', 4000000, 'Error')`,
	} {
		if err := execute(sql); err != nil {
			t.Fatalf("%v\n%s", err, sql)
		}
	}

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	t.Cleanup(background.Wait)
	cfg := &config.Config{}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	lm, tm := telemetry.DefaultLogsMapping(), telemetry.DefaultTracesMapping()
	logsID, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Kind: telemetry.KindLogs, Name: "logs", Database: otel, Table: "otel_logs", Logs: &lm, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	tracesID, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Kind: telemetry.KindTraces, Name: "traces", Database: otel, Table: "otel_traces", Traces: &tm, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	e := &observeEnv{
		deps: Deps{DB: db, Gateway: agent.Gateway, Config: cfg},
		ak:   &authedKey{key: &database.MCPKey{ID: "live", Name: "live", ConnectionID: conn, CHUser: user, AllowedDatabases: otel}, chPassword: password},
	}
	call := func(tool string, args map[string]any) string {
		t.Helper()
		res, err := e.session(t).CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			t.Fatal(err)
		}
		if res.IsError {
			t.Fatalf("%s: %s", tool, resultText(res))
		}
		return resultText(res)
	}

	if out := call("search_logs", map[string]any{"source_id": logsID, "query": "payment"}); !strings.Contains(out, "payment failed") || strings.Contains(out, "hello") {
		t.Errorf("search_logs: %s", out)
	}
	if out := call("search_logs", map[string]any{"severity": []string{"INFO"}}); !strings.Contains(out, "hello") || strings.Contains(out, "payment") {
		t.Errorf("search_logs severity: %s", out)
	}
	if out := call("search_traces", map[string]any{"source_id": tracesID, "errors_only": true, "min_duration_ms": 1}); !strings.Contains(out, `"trace_id":"t1"`) {
		t.Errorf("search_traces: %s", out)
	}
	var trace struct {
		Trace telemetry.TraceSummary `json:"trace"`
		Spans []telemetry.Span       `json:"spans"`
		Logs  []map[string]any       `json:"logs"`
	}
	if err := json.Unmarshal([]byte(call("get_trace", map[string]any{"trace_id": "t1"})), &trace); err != nil {
		t.Fatal(err)
	}
	if len(trace.Spans) != 2 || trace.Spans[1].Depth != 1 || trace.Trace.ErrorCount != 1 || len(trace.Logs) != 1 {
		t.Errorf("get_trace: %+v", trace)
	}
	if out := call("service_map", map[string]any{"source_id": tracesID}); !strings.Contains(out, `"from":"web"`) || !strings.Contains(out, `"to":"api"`) {
		t.Errorf("service_map: %s", out)
	}
	if out := call("table_lineage", map[string]any{"database": otel, "table": "otel_logs", "direction": "downstream"}); !strings.Contains(out, otel+".errors_mv") {
		t.Errorf("table_lineage: %s", out)
	}
}
