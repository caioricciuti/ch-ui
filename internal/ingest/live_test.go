package ingest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// End to end against a live ClickHouse (CHUI_TEST_CLICKHOUSE_URL,
// passwordless default user): two ingest pipelines (logs, traces) with
// create_table, OTLP protobuf requests with a key, rows in the exporter
// tables, read back with the Telemetry page's own queries.
func TestLiveIngestOTLPToTelemetry(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CHUI_TEST_CLICKHOUSE_URL not set")
	}
	if u := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"); u != "" && u != "default" || os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD") != "" {
		t.Skip("the live ingest test needs the passwordless default user")
	}
	client := connector.NewCHClient(endpoint, false)
	query := func(sql string, settings map[string]string) []map[string]interface{} {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, "default", "", settings)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return res.Data
	}
	scalar := func(sql string) string {
		t.Helper()
		rows := query(sql, nil)
		if len(rows) == 0 {
			return ""
		}
		for _, v := range rows[0] {
			return fmt.Sprint(v)
		}
		return ""
	}
	dbName := fmt.Sprintf("chui_ingest_%d", time.Now().UnixNano())
	query("CREATE DATABASE "+dbName, nil)
	t.Cleanup(func() { query("DROP DATABASE IF EXISTS "+dbName, nil) })

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	enc, err := crypto.Encrypt("", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "pipeline", Mode: "service_account", Username: "default", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}

	newPipeline := func(signal, table string) (string, string) {
		t.Helper()
		id, err := db.CreatePipeline("ingest-"+signal, "", conn, "tester")
		if err != nil {
			t.Fatal(err)
		}
		nodes := []database.PipelineNode{
			{ID: id + "-src", NodeType: "source_ingest", ConfigEncrypted: fmt.Sprintf(`{"signal":%q,"ttl_days":30}`, signal)},
			{ID: id + "-sink", NodeType: "sink_clickhouse", ConfigEncrypted: fmt.Sprintf(`{"database":%q,"table":%q,"create_table":true}`, dbName, table)},
		}
		edges := []database.PipelineEdge{{ID: id + "-e", SourceNodeID: id + "-src", TargetNodeID: id + "-sink"}}
		if err := db.SavePipelineGraph(id, nodes, edges, ""); err != nil {
			t.Fatal(err)
		}
		plain, hash, prefix, err := GenerateKey()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.CreateIngestKey(id, "live", hash, prefix, "tester"); err != nil {
			t.Fatal(err)
		}
		return id, plain
	}
	logsID, logsKey := newPipeline("logs", "otel_logs")
	tracesID, tracesKey := newPipeline("traces", "otel_traces")

	sp, err := spool.Open(t.TempDir(), spool.Options{SegmentMaxAge: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	hub := pipelines.NewHub(sp)
	cfg := &config.Config{AppSecretKey: "test-secret", DatabasePath: filepath.Join(t.TempDir(), "ch-ui.db")}
	runner := pipelines.NewRunner(db, agent.Gateway, cfg)
	runner.SetHub(hub)
	for _, id := range []string{logsID, tracesID} {
		if err := runner.StartPipeline(id); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { runner.Stop(); hub.Close() })
	deadline := time.Now().Add(10 * time.Second)
	for hub.Endpoint(logsID) == nil || hub.Endpoint(tracesID) == nil {
		if time.Now().After(deadline) {
			t.Fatal("ingest endpoints not registered")
		}
		time.Sleep(20 * time.Millisecond)
	}

	r := chi.NewRouter()
	r.Route("/ingest", NewReceiver(db, hub).Routes)
	post := func(path, key string, body []byte) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(gz(body)))
		req.Header.Set("Content-Type", "application/x-protobuf")
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Authorization", "Bearer "+key)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
	traceID := bytes.Repeat([]byte{0xab}, 16)
	now := uint64(time.Now().UnixNano())
	post("/ingest/v1/logs", logsKey, otlpLogsProto("checkout", now, traceID, "first line", "second line", "third line"))
	post("/ingest/v1/traces", tracesKey, otlpTraceProto("checkout", traceID, now))

	waitRows := func(table string, want string) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for {
			if scalar("SELECT count() FROM system.tables WHERE database = '"+dbName+"' AND name = '"+table+"'") == "1" {
				if got := scalar("SELECT count() FROM " + dbName + "." + table); got == want {
					return
				}
			}
			if time.Now().After(deadline) {
				p, _ := db.GetPipelineByID(logsID)
				t.Fatalf("%s: rows never reached %s (pipeline %+v)", table, want, p)
			}
			time.Sleep(200 * time.Millisecond)
		}
	}
	waitRows("otel_logs", "3")
	waitRows("otel_traces", "2")
	waitRows("otel_traces_trace_id_ts", "1")

	describe := func(table string) map[string]string {
		cols := map[string]string{}
		for _, row := range query("DESCRIBE TABLE "+dbName+"."+table, nil) {
			cols[fmt.Sprint(row["name"])] = fmt.Sprint(row["type"])
		}
		return cols
	}
	// The tables look like what Telemetry's source detection proposes:
	// exporter column sets under names its system.tables filter matches.
	logCols, spanCols := describe("otel_logs"), describe("otel_traces")
	for _, c := range []string{"Body", "Timestamp", "SeverityText"} {
		if _, ok := logCols[c]; !ok {
			t.Fatalf("otel_logs lacks %s", c)
		}
	}
	for _, c := range []string{"SpanId", "ParentSpanId", "Duration", "Timestamp"} {
		if _, ok := spanCols[c]; !ok {
			t.Fatalf("otel_traces lacks %s", c)
		}
	}
	if !telemetry.HasNestedPrefix(spanCols, "Events") {
		t.Fatal("otel_traces has no Events nested columns")
	}

	lm := telemetry.DefaultLogsMapping()
	logsSrc := &telemetry.Source{Kind: telemetry.KindLogs, Database: dbName, Table: "otel_logs", Logs: &lm}
	sql, err := telemetry.SearchSQL(logsSrc, logCols, telemetry.LogsParams{Q: "service:checkout"}, "asc", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	rows := query(sql, nil)
	var bodies []string
	for _, row := range rows {
		bodies = append(bodies, fmt.Sprint(row["body"]))
		if row["trace_id"] != strings.Repeat("ab", 16) || row["service"] != "checkout" || row["severity"] != "INFO" {
			t.Fatalf("log row: %v", row)
		}
	}
	sort.Strings(bodies)
	if strings.Join(bodies, "|") != "first line|second line|third line" {
		t.Fatalf("log bodies: %v", bodies)
	}

	tm := telemetry.DefaultTracesMapping()
	tracesSrc := &telemetry.Source{Kind: telemetry.KindTraces, Database: dbName, Table: "otel_traces", Traces: &tm}
	if got := query(telemetry.TraceIDTsLookupSQL(tracesSrc), nil); len(got) != 1 {
		t.Fatalf("trace id lookup table not found: %v", got)
	}
	spans := query(telemetry.TraceSpansSQL(tracesSrc, strings.Repeat("ab", 16), nil), nil)
	if len(spans) != 2 {
		t.Fatalf("trace spans: %v", spans)
	}

	// A replayed segment (same token) is dropped on these plain MergeTree
	// tables: the sink created them with a dedup window.
	before := scalar("SELECT count() FROM " + dbName + ".otel_logs")
	// A current timestamp: rows already past the 30 day TTL are dropped on insert.
	row := fmt.Sprintf(`{"Timestamp":"%d.000000000","Body":"replay","ServiceName":"checkout"}`, time.Now().Unix())
	for i := 0; i < 2; i++ {
		query("INSERT INTO "+dbName+".otel_logs FORMAT JSONEachRow "+row, map[string]string{"insert_deduplication_token": "chui-live-replay", "input_format_skip_unknown_fields": "1"})
	}
	if got := scalar("SELECT count() FROM " + dbName + ".otel_logs"); got != fmt.Sprint(mustInt(t, before)+1) {
		t.Fatalf("after a replayed insert rows = %s, want %s + 1", got, before)
	}

	v := hub.View(logsID, time.Now())
	if v.Received.Records != 3 || v.Written.Rows != 3 || v.Spool.Segments != 0 {
		t.Fatalf("logs stats: %+v", v)
	}
}

func mustInt(t *testing.T, s string) int {
	t.Helper()
	var n int
	if _, err := fmt.Sscan(s, &n); err != nil {
		t.Fatal(err)
	}
	return n
}
