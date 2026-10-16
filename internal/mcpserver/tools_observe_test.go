package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/lineage"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// observeCH answers the observe tools' queries: DESCRIBE with the exporter's
// logs columns, system.tables with lineageRows, and anything else with rows
// copies of one log row. It records every message.
type observeCH struct {
	mu          sync.Mutex
	sent        []tunnel.GatewayMessage
	rows        int
	lineageRows string
}

func (f *observeCH) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sent = append(f.sent, msg)
	result := func(data string) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	}
	switch {
	case strings.HasPrefix(msg.SQL, "DESCRIBE TABLE"):
		cols := []string{}
		for name, typ := range map[string]string{
			"Timestamp": "DateTime64(9)", "Body": "String", "SeverityText": "LowCardinality(String)", "SeverityNumber": "UInt8",
			"ServiceName": "LowCardinality(String)", "TraceId": "String", "SpanId": "String",
			"ResourceAttributes": "Map(LowCardinality(String), String)", "ScopeAttributes": "Map(LowCardinality(String), String)",
			"LogAttributes": "Map(LowCardinality(String), String)", "ScopeName": "String", "EventName": "String",
		} {
			cols = append(cols, fmt.Sprintf(`{"name":%q,"type":%q}`, name, typ))
		}
		return result("[" + strings.Join(cols, ",") + "]")
	case strings.Contains(msg.SQL, "FROM system.tables"):
		return result(f.lineageRows)
	case strings.Contains(msg.SQL, "hostName()"):
		return result(`[{"host":"h1","hosts":"1"}]`)
	case strings.Contains(msg.SQL, "system.dictionaries"), strings.Contains(msg.SQL, "system.view_refreshes"):
		return result(`[]`)
	}
	rows := make([]string, f.rows)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"timestamp":"2026-10-16T08:00:00Z","timestamp_ns":"%d","severity":"ERROR","service":"api","body":"boom","trace_id":"t","span_id":"s","resource":{},"scope":{},"attributes":{"k":"v"},"row_id":"%d"}`, 1760601600000000000-i, i)
	}
	return result("[" + strings.Join(rows, ",") + "]")
}

func (f *observeCH) messages() []tunnel.GatewayMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]tunnel.GatewayMessage(nil), f.sent...)
}

type observeEnv struct {
	deps      Deps
	ak        *authedKey
	ch        *observeCH
	conn      string
	otherConn string
}

func newObserveEnv(t *testing.T, allowed string, pro bool) *observeEnv {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	other, err := db.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other-token"})
	if err != nil {
		t.Fatal(err)
	}
	ch := &observeCH{rows: 1, lineageRows: "[]"}
	agent := testutil.NewAgent(t, db, "worker-token", ch.respond)
	t.Cleanup(background.Wait)
	cfg := &config.Config{}
	if pro {
		cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	}
	ak := &authedKey{key: &database.MCPKey{ID: "k1", Name: "test", ConnectionID: conn, CHUser: "default", AllowedDatabases: allowed}}
	return &observeEnv{deps: Deps{DB: db, Gateway: agent.Gateway, Config: cfg}, ak: ak, ch: ch, conn: conn, otherConn: other}
}

func (e *observeEnv) session(t *testing.T) *mcp.ClientSession {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerObserveTools(srv, e.deps, e.ak)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

func (e *observeEnv) call(t *testing.T, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := e.session(t).CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func (e *observeEnv) logsSource(t *testing.T, conn, db string) string {
	t.Helper()
	m := telemetry.DefaultLogsMapping()
	id, err := e.deps.DB.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Kind: telemetry.KindLogs, Name: "logs " + db, Database: db, Table: "otel_logs", Logs: &m, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func decodeResult(t *testing.T, res *mcp.CallToolResult) map[string]any {
	t.Helper()
	if res.IsError {
		t.Fatalf("tool error: %s", resultText(res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatalf("decode %s: %v", resultText(res), err)
	}
	return out
}

func TestObserveToolsProGating(t *testing.T) {
	free := []string{"telemetry_sources", "search_logs", "ingest_status", "table_lineage"}
	pro := []string{"search_traces", "get_trace", "service_map", "list_metrics", "query_metrics", "list_monitors"}
	for _, isPro := range []bool{false, true} {
		e := newObserveEnv(t, "", isPro)
		res, err := e.session(t).ListTools(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, tool := range res.Tools {
			got[tool.Name] = true
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Errorf("%s must be annotated read-only", tool.Name)
			}
		}
		for _, name := range free {
			if !got[name] {
				t.Errorf("pro=%v: community tool %s missing", isPro, name)
			}
		}
		for _, name := range pro {
			if got[name] != isPro {
				t.Errorf("pro=%v: %s registered=%v", isPro, name, got[name])
			}
		}
	}
}

func TestTelemetrySourcesScopedAndFiltered(t *testing.T) {
	e := newObserveEnv(t, "otel", false)
	mine := e.logsSource(t, e.conn, "otel")
	e.logsSource(t, e.conn, "secret")
	e.logsSource(t, e.otherConn, "otel")

	out := decodeResult(t, e.call(t, "telemetry_sources", nil))
	sources := out["sources"].([]any)
	if len(sources) != 1 || sources[0].(map[string]any)["id"] != mine {
		t.Fatalf("want only the allowed source of this connection, got %v", sources)
	}
	if len(e.ch.messages()) != 0 {
		t.Error("telemetry_sources must not query ClickHouse")
	}
}

func TestSearchLogsRefusesOtherConnectionAndDisallowedDatabase(t *testing.T) {
	e := newObserveEnv(t, "otel", false)
	foreign := e.logsSource(t, e.otherConn, "otel")
	secret := e.logsSource(t, e.conn, "secret")

	res := e.call(t, "search_logs", map[string]any{"source_id": foreign})
	if !res.IsError || !strings.Contains(resultText(res), "not found") {
		t.Errorf("source of another connection: want not found, got %s", resultText(res))
	}
	res = e.call(t, "search_logs", map[string]any{"source_id": secret})
	if !res.IsError || !strings.Contains(resultText(res), "allowlist") {
		t.Errorf("source in a disallowed database: want refusal, got %s", resultText(res))
	}
	// No source_id: the disallowed source is not picked either.
	res = e.call(t, "search_logs", nil)
	if !res.IsError || !strings.Contains(resultText(res), "no enabled logs source") {
		t.Errorf("default source: want none available, got %s", resultText(res))
	}
	if msgs := e.ch.messages(); len(msgs) != 0 {
		t.Fatalf("refused calls must not reach ClickHouse, sent %q", msgs[0].SQL)
	}
}

func TestSearchLogsCapsLimitAndStripsSettings(t *testing.T) {
	e := newObserveEnv(t, "", false)
	e.logsSource(t, e.conn, "otel")
	e.ch.rows = 3

	out := decodeResult(t, e.call(t, "search_logs", map[string]any{"limit": 100000, "query": "service:api", "since": "15m"}))
	var search *tunnel.GatewayMessage
	for _, m := range e.ch.messages() {
		if strings.Contains(m.SQL, "row_hash") {
			search = &m
		}
	}
	if search == nil {
		t.Fatal("search query not sent")
	}
	if !strings.Contains(search.SQL, "LIMIT 501") {
		t.Errorf("limit must be capped at 500 (+1 to detect more), SQL: %s", search.SQL)
	}
	if strings.Contains(search.SQL, "SETTINGS") {
		t.Errorf("readonly=1 refuses a SETTINGS clause; it must be cut: %s", search.SQL)
	}
	if search.Settings["readonly"] != "1" || search.Settings["max_execution_time"] != "25" {
		t.Errorf("settings: %v", search.Settings)
	}
	if rows := out["rows"].([]any); len(rows) != 3 {
		t.Errorf("rows: %d", len(rows))
	}
	if _, ok := out["next_cursor"]; ok {
		t.Error("no next_cursor when fewer rows than the limit came back")
	}

	e.ch.rows = 3
	out = decodeResult(t, e.call(t, "search_logs", map[string]any{"limit": 2}))
	if rows := out["rows"].([]any); len(rows) != 2 {
		t.Errorf("limit 2: %d rows", len(rows))
	}
	cur, _ := out["next_cursor"].(string)
	if _, err := telemetry.DecodeCursor(cur); err != nil {
		t.Errorf("next_cursor %q: %v", cur, err)
	}
}

// Every builder the observe tools run must end in QuerySettings, or
// runTelemetry would send a SETTINGS clause that readonly=1 refuses.
func TestTelemetryBuildersEndInQuerySettings(t *testing.T) {
	lm, tm := telemetry.DefaultLogsMapping(), telemetry.DefaultTracesMapping()
	mt := telemetry.DefaultMetricsTables()
	logs := &telemetry.Source{Kind: telemetry.KindLogs, Database: "otel", Table: "otel_logs", Logs: &lm, Enabled: true}
	traces := &telemetry.Source{Kind: telemetry.KindTraces, Database: "otel", Table: "otel_traces", Traces: &tm, Enabled: true}
	metrics := &telemetry.Source{Kind: telemetry.KindMetrics, Database: "otel", Tables: &mt, Enabled: true}
	from, to := "2026-10-16T07:00:00Z", "2026-10-16T08:00:00Z"
	cols := map[string]string{"Timestamp": "DateTime64(9)"}
	sp := telemetry.ServiceMapParams{From: from, To: to}
	mp := telemetry.MetricsParams{From: from, To: to, Metric: "m", Type: telemetry.MetricGauge, Aggregation: "avg", BucketSeconds: 60, LimitSeries: 10}
	must := func(s string, err error) string {
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	for name, sql := range map[string]string{
		"SearchSQL":          must(telemetry.SearchSQL(logs, cols, telemetry.LogsParams{From: from, To: to}, "", 51, nil)),
		"ByTraceSQL":         must(telemetry.ByTraceSQL(logs, "t", nil)),
		"TraceSearchSQL":     must(telemetry.TraceSearchSQL(traces, cols, telemetry.TracesParams{From: from, To: to}, 51, nil)),
		"TraceSpansSQL":      telemetry.TraceSpansSQL(traces, "t", nil),
		"TraceBoundsSQL":     telemetry.TraceBoundsSQL(traces, "t"),
		"ServiceMapCountSQL": must(telemetry.ServiceMapCountSQL(traces, sp)),
		"ServiceMapNodesSQL": must(telemetry.ServiceMapNodesSQL(traces, sp, 1)),
		"ServiceMapEdgesSQL": must(telemetry.ServiceMapEdgesSQL(traces, sp, 1)),
		"CatalogSQL":         must(telemetry.CatalogSQL(metrics, from, to)),
		"MetricQuerySQL":     must(telemetry.MetricQuerySQL(metrics, mp)),
	} {
		if !strings.HasSuffix(strings.TrimSpace(sql), telemetry.QuerySettings) {
			t.Errorf("%s does not end in QuerySettings: ...%s", name, sql[max(0, len(sql)-120):])
		}
		cut := strings.TrimSuffix(strings.TrimSpace(sql), telemetry.QuerySettings)
		if strings.Contains(cut, "SETTINGS") {
			t.Errorf("%s has another SETTINGS clause", name)
		}
	}
}

func TestRangeArgsResolve(t *testing.T) {
	now := time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		args     rangeArgs
		from, to string
		err      string
	}{
		{args: rangeArgs{}, from: "2026-10-16T07:00:00Z", to: "2026-10-16T08:00:00Z"},
		{args: rangeArgs{Since: "7d"}, from: "2026-10-09T08:00:00Z", to: "2026-10-16T08:00:00Z"},
		{args: rangeArgs{Since: "15m", To: "2026-10-15T00:00:00Z"}, from: "2026-10-14T23:45:00Z", to: "2026-10-15T00:00:00Z"},
		{args: rangeArgs{From: "2026-10-16T06:00:00Z"}, from: "2026-10-16T06:00:00Z", to: "2026-10-16T08:00:00Z"},
		{args: rangeArgs{From: "yesterday"}, err: "RFC 3339"},
		{args: rangeArgs{From: "2026-10-16T09:00:00Z"}, err: "before"},
		{args: rangeArgs{Since: "-1h"}, err: "between"},
		{args: rangeArgs{Since: "week"}, err: "look like"},
	} {
		from, to, err := tc.args.resolve(now)
		if tc.err != "" {
			if err == nil || !strings.Contains(err.Error(), tc.err) {
				t.Errorf("%+v: want error %q, got %v", tc.args, tc.err, err)
			}
			continue
		}
		if err != nil || from != tc.from || to != tc.to {
			t.Errorf("%+v: got %s %s %v, want %s %s", tc.args, from, to, err, tc.from, tc.to)
		}
	}
}

func TestTraceSearchQuery(t *testing.T) {
	q, err := traceSearchQuery(searchTracesArgs{Query: "a OR b", Service: `pay"ments`, Operation: "GET /x", MinDurationMs: 250.5, ErrorsOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	want := `(a OR b) AND service:"pay\"ments" AND span_name:"GET /x" AND duration:>=250.5 AND (status:error OR status:status_code_error)`
	if q != want {
		t.Errorf("got  %s\nwant %s", q, want)
	}
	// The combined query must compile against the traces schema.
	tm := telemetry.DefaultTracesMapping()
	src := &telemetry.Source{Kind: telemetry.KindTraces, Database: "otel", Table: "otel_traces", Traces: &tm}
	if _, err := telemetry.TracesWhere(src, map[string]string{}, telemetry.TracesParams{Q: q}); err != nil {
		t.Errorf("compile: %v", err)
	}
	if _, err := traceSearchQuery(searchTracesArgs{MinDurationMs: -1}); err == nil {
		t.Error("negative duration must be rejected")
	}
}

func TestListMonitorsScoped(t *testing.T) {
	e := newObserveEnv(t, "otel", true)
	allowed := e.logsSource(t, e.conn, "otel")
	secret := e.logsSource(t, e.conn, "secret")
	foreign := e.logsSource(t, e.otherConn, "otel")
	add := func(conn, source, name string) {
		if _, err := e.deps.DB.CreateTelemetryMonitor(&database.TelemetryMonitor{ConnectionID: conn, Name: name, Kind: "logs", SourceID: source,
			WindowSeconds: 300, IntervalSeconds: 60, Comparator: ">", Threshold: 1, Severity: "warning", Enabled: true}); err != nil {
			t.Fatal(err)
		}
	}
	add(e.conn, allowed, "visible")
	add(e.conn, secret, "secret db")
	add(e.otherConn, foreign, "other connection")

	out := decodeResult(t, e.call(t, "list_monitors", nil))
	monitors := out["monitors"].([]any)
	if len(monitors) != 1 || monitors[0].(map[string]any)["name"] != "visible" {
		t.Fatalf("want only the visible monitor, got %v", monitors)
	}
	if _, ok := monitors[0].(map[string]any)["created_by"]; ok {
		t.Error("created_by must not be returned")
	}
}

func TestIngestStatusScopedAndFiltered(t *testing.T) {
	e := newObserveEnv(t, "otel", false)
	pipeline := func(conn, name, sourceType, sourceCfg, sinkDB string) string {
		id, err := e.deps.DB.CreatePipeline(name, "", conn, "tester")
		if err != nil {
			t.Fatal(err)
		}
		nodes := []database.PipelineNode{
			{ID: id + "-src", NodeType: sourceType, ConfigEncrypted: sourceCfg},
			{ID: id + "-sink", NodeType: "sink_clickhouse", ConfigEncrypted: `{"database":"` + sinkDB + `","table":"logs","password":"enc:v1:x"}`},
		}
		if err := e.deps.DB.SavePipelineGraph(id, nodes, nil, ""); err != nil {
			t.Fatal(err)
		}
		return id
	}
	visible := pipeline(e.conn, "otlp logs", "source_ingest", `{"signal":"logs"}`, "otel")
	pipeline(e.conn, "secret logs", "source_ingest", `{"signal":"logs"}`, "secret")
	pipeline(e.otherConn, "other conn", "source_ingest", `{"signal":"logs"}`, "otel")
	pipeline(e.conn, "kafka", "source_kafka", `{}`, "otel")

	res := e.call(t, "ingest_status", nil)
	text := resultText(res)
	out := decodeResult(t, res)
	items := out["pipelines"].([]any)
	if len(items) != 1 {
		t.Fatalf("want only the visible push pipeline, got %s", text)
	}
	p := items[0].(map[string]any)
	if p["id"] != visible || p["signal"] != "logs" {
		t.Errorf("pipeline: %v", p)
	}
	if strings.Contains(text, "enc:v1") || strings.Contains(text, "password") {
		t.Errorf("sink secrets leaked: %s", text)
	}
	if !strings.Contains(fmt.Sprint(out["note"]), "live counters are unavailable") {
		t.Errorf("without a spool the note must say so: %v", out["note"])
	}

	res = e.call(t, "ingest_status", map[string]any{"pipeline_id": "nope"})
	if !res.IsError {
		t.Errorf("unknown pipeline: want error, got %s", resultText(res))
	}
}

const observeLineageRows = `[
 {"database":"otel","name":"src","engine":"MergeTree","dependencies_database":["otel"],"dependencies_table":["mv"]},
 {"database":"otel","name":"mv","engine":"MaterializedView","ddl":"CREATE MATERIALIZED VIEW otel.mv TO otel.tgt AS SELECT * FROM otel.src","as_select":"SELECT * FROM otel.src"},
 {"database":"otel","name":"tgt","engine":"MergeTree"},
 {"database":"secret","name":"v","engine":"View","ddl":"CREATE VIEW secret.v AS SELECT * FROM otel.tgt","as_select":"SELECT * FROM otel.tgt"}
]`

func TestTableLineageAllowList(t *testing.T) {
	e := newObserveEnv(t, "otel", false)
	e.ch.lineageRows = observeLineageRows

	res := e.call(t, "table_lineage", map[string]any{"database": "secret", "table": "v"})
	if !res.IsError || !strings.Contains(resultText(res), "allowlist") {
		t.Errorf("disallowed database: want refusal, got %s", resultText(res))
	}
	if len(e.ch.messages()) != 0 {
		t.Fatal("refused call must not reach ClickHouse")
	}

	out := decodeResult(t, e.call(t, "table_lineage", map[string]any{"database": "otel", "table": "tgt"}))
	text := fmt.Sprint(out)
	if strings.Contains(text, "secret") {
		t.Errorf("node in a disallowed database leaked: %v", out)
	}
	if !strings.Contains(text, "otel.src") || !strings.Contains(text, "otel.mv") {
		t.Errorf("upstream missing: %v", out)
	}
	for _, m := range e.ch.messages() {
		if strings.Contains(m.SQL, "FROM system.tables") && m.Settings["param_databases"] != "['otel']" {
			t.Errorf("allow-listed key must load only its databases, param_databases=%q", m.Settings["param_databases"])
		}
	}

	res = e.call(t, "table_lineage", map[string]any{"database": "otel", "table": "nope"})
	if !res.IsError || !strings.Contains(resultText(res), "not found") {
		t.Errorf("unknown table: %s", resultText(res))
	}
}

func TestLineageWalk(t *testing.T) {
	g := lineage.Graph{
		Nodes: []lineage.Node{{ID: "a.src", Database: "a"}, {ID: "a.mv", Database: "a"}, {ID: "a.tgt", Database: "a"}, {ID: "a.rpt", Database: "a"}, {ID: "b.v", Database: "b"}, {ID: "a.after_b", Database: "a"}, {ID: "external:s3", Database: ""}},
		Edges: []lineage.Edge{
			{From: "a.src", To: "a.mv", Kind: "triggers"},
			{From: "a.mv", To: "a.tgt", Kind: "writes"},
			{From: "a.tgt", To: "a.rpt", Kind: "reads"},
			{From: "a.tgt", To: "b.v", Kind: "reads"},
			{From: "b.v", To: "a.after_b", Kind: "reads"},
			{From: "external:s3", To: "a.src", Kind: "reads"},
		},
	}
	all := func(lineage.Node) bool { return true }
	onlyA := func(n lineage.Node) bool { return n.Database != "b" }
	ids := func(nodes []lineage.Node) string {
		var s []string
		for _, n := range nodes {
			s = append(s, n.ID)
		}
		return strings.Join(s, ",")
	}
	for _, tc := range []struct {
		root, dir string
		depth     int
		keep      func(lineage.Node) bool
		nodes     string
		edges     int
	}{
		{"a.tgt", "upstream", 1, all, "a.mv,a.tgt", 1},
		{"a.tgt", "upstream", 10, all, "a.mv,a.src,a.tgt,external:s3", 3},
		{"a.tgt", "downstream", 10, all, "a.after_b,a.rpt,a.tgt,b.v", 3},
		{"a.tgt", "downstream", 10, onlyA, "a.rpt,a.tgt", 1},
		{"a.mv", "both", 1, all, "a.mv,a.src,a.tgt", 2},
	} {
		nodes, edges, found := lineageWalk(g, tc.root, tc.dir, tc.depth, tc.keep)
		if !found || ids(nodes) != tc.nodes || len(edges) != tc.edges {
			t.Errorf("%s %s depth %d: nodes %s edges %v, want %s and %d edges", tc.root, tc.dir, tc.depth, ids(nodes), edges, tc.nodes, tc.edges)
		}
	}
	if _, _, found := lineageWalk(g, "b.v", "both", 3, onlyA); found {
		t.Error("a root the filter drops must not be found")
	}
}
