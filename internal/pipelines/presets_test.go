package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestJSONTypeSupported(t *testing.T) {
	for v, want := range map[string]bool{
		"25.8.33.6": true, "25.3.1.1": true, "26.1.2": true, "25.2.9.1": false,
		"24.8.4.13": false, "23.3.1": false, "": false, "garbage": false,
	} {
		if got := jsonTypeSupported(v); got != want {
			t.Errorf("jsonTypeSupported(%q) = %v", v, got)
		}
	}
}

func TestWideEventsDDL(t *testing.T) {
	got := wideEventsDDL("logs", "events", "MergeTree", true, false, 0)
	want := "CREATE TABLE IF NOT EXISTS `logs`.`events` (\n" +
		"  `timestamp` DateTime64(3) CODEC(Delta, ZSTD(1)),\n" +
		"  `service` LowCardinality(String),\n" +
		"  `level` LowCardinality(String),\n" +
		"  `message` String CODEC(ZSTD(1)),\n" +
		"  `trace_id` String,\n" +
		"  `attributes` JSON,\n" +
		"  INDEX idx_message `message` TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 4,\n" +
		"  INDEX idx_trace_id `trace_id` TYPE bloom_filter(0.001) GRANULARITY 1\n" +
		") ENGINE = MergeTree\n" +
		"PARTITION BY toDate(`timestamp`)\n" +
		"ORDER BY (`service`, `level`, `timestamp`)\n" +
		"SETTINGS non_replicated_deduplication_window = 1000"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}

	// Older servers get a map; raw, TTL and a replicated engine.
	got = wideEventsDDL("d", "t", "ReplicatedMergeTree", false, true, 14)
	for _, part := range []string{
		"`attributes` Map(LowCardinality(String), String) CODEC(ZSTD(1)),\n  `raw` String CODEC(ZSTD(3)),\n",
		"TTL toDateTime(`timestamp`) + toIntervalDay(14)\nSETTINGS ttl_only_drop_parts = 1",
	} {
		if !strings.Contains(got, part) {
			t.Fatalf("missing %q in\n%s", part, got)
		}
	}
	if strings.Contains(got, "non_replicated_deduplication_window") {
		t.Fatalf("replicated engine got a dedup window:\n%s", got)
	}
}

func TestPresetResolutionAndValidation(t *testing.T) {
	cases := []struct {
		fields map[string]interface{}
		want   string
	}{
		{map[string]interface{}{}, PresetCustom},
		{map[string]interface{}{sinkOTelSignal: SignalLogs}, PresetOTelLogs},
		{map[string]interface{}{sinkOTelSignal: SignalTraces}, PresetOTelTraces},
		{map[string]interface{}{"preset": "wide_events", sinkOTelSignal: SignalLogs}, PresetWideEvents},
		{map[string]interface{}{"preset": "custom", sinkOTelSignal: SignalTraces}, PresetCustom},
		{map[string]interface{}{"preset": "otel_traces"}, PresetOTelTraces},
	}
	for _, c := range cases {
		if got := sinkPreset(c.fields); got != c.want {
			t.Errorf("sinkPreset(%v) = %s, want %s", c.fields, got, c.want)
		}
	}
	sink := NewClickHouseSink(nil, nil, "")
	base := func(extra map[string]interface{}) ConnectorConfig {
		f := map[string]interface{}{"database": "d", "table": "t"}
		for k, v := range extra {
			f[k] = v
		}
		return ConnectorConfig{Fields: f}
	}
	for _, ok := range []map[string]interface{}{
		{}, {"preset": ""}, {"preset": "wide_events", "ttl_days": float64(30), "mapping": map[string]interface{}{"service": "app_name, svc", "level": []interface{}{"lvl"}}},
	} {
		if err := sink.Validate(base(ok)); err != nil {
			t.Errorf("rejected %v: %v", ok, err)
		}
	}
	for _, bad := range []map[string]interface{}{
		{"preset": "columnar"}, {"ttl_days": float64(-1)}, {"ttl_days": 1.5}, {"ttl_days": "x"},
		{"mapping": "service"}, {"mapping": map[string]interface{}{"colour": "x"}}, {"mapping": map[string]interface{}{"level": float64(1)}},
	} {
		if err := sink.Validate(base(bad)); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
}

func TestParseEventTime(t *testing.T) {
	want := time.Date(2025, 10, 16, 10, 30, 0, 123000000, time.UTC)
	for _, v := range []interface{}{
		"2025-10-16T10:30:00.123Z",
		"2025-10-16T12:30:00.123+02:00",
		"2025-10-16 10:30:00.123",
		"2025-10-16T10:30:00.123",
		float64(1760610600.123),
		json.Number("1760610600123"),
		"1760610600123",
		json.Number("1760610600123000"),
		json.Number("1760610600123000000"),
	} {
		got, ok := parseEventTime(v)
		if !ok || got.UTC().Sub(want).Abs() > time.Millisecond {
			t.Errorf("parseEventTime(%v) = %v %v", v, got.UTC(), ok)
		}
	}
	for _, v := range []interface{}{"yesterday", "", nil, true, float64(-5), map[string]interface{}{}} {
		if _, ok := parseEventTime(v); ok {
			t.Errorf("parsed %v", v)
		}
	}
}

func TestCanonicalLevel(t *testing.T) {
	for in, want := range map[interface{}]string{
		"INFO": "info", "Warning": "warn", "ERR": "error", "critical": "fatal", "dbg": "debug", "notice": "info",
		"custom": "custom", float64(30): "info", float64(50): "error", json.Number("60"): "fatal",
		float64(9): "info", float64(17): "error", "13": "warn", true: "",
	} {
		if got := canonicalLevel(in); got != want {
			t.Errorf("canonicalLevel(%v) = %q, want %q", in, got, want)
		}
	}
}

func wideRowOf(t *testing.T, raw string, fields map[string]interface{}, target wideTarget) map[string]interface{} {
	t.Helper()
	roles, err := wideMapping(fields)
	if err != nil {
		t.Fatal(err)
	}
	return wideRow(Record{RawJSON: []byte(raw)}, roles, target, time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC))
}

func TestWideRowFilebeat(t *testing.T) {
	raw := `{"@timestamp":"2025-10-16T10:30:00.123Z","message":"GET /health 200","log":{"level":"WARNING","file":{"path":"/var/log/app.log"}},` +
		`"service":{"name":"api","version":"1.2"},"host":{"name":"web-1"},"trace":{"id":"abc123"},"http":{"status":200}}`
	row := wideRowOf(t, raw, nil, wideTarget{attrs: "json", raw: true})
	if row["timestamp"] != "2025-10-16T10:30:00.123Z" || row["service"] != "api" || row["level"] != "warn" ||
		row["message"] != "GET /health 200" || row["trace_id"] != "abc123" {
		t.Fatalf("roles: %v", row)
	}
	attrs := row["attributes"].(map[string]interface{})
	b, _ := json.Marshal(attrs)
	if string(b) != `{"host":{"name":"web-1"},"http":{"status":200},"log":{"file":{"path":"/var/log/app.log"}},"service":{"version":"1.2"}}` {
		t.Fatalf("attributes: %s", b)
	}
	if row["raw"] != raw {
		t.Fatalf("raw: %v", row["raw"])
	}

	// The same record for a Map column: dotted keys, string values.
	row = wideRowOf(t, raw, nil, wideTarget{attrs: "map"})
	flat := row["attributes"].(map[string]string)
	if flat["host.name"] != "web-1" || flat["http.status"] != "200" || flat["log.file.path"] != "/var/log/app.log" || flat["service.version"] != "1.2" || len(flat) != 4 {
		t.Fatalf("map attributes: %v", flat)
	}
	if _, ok := row["raw"]; ok {
		t.Fatal("raw written without keep_raw")
	}
}

func TestWideRowLogfmtAndEpochs(t *testing.T) {
	// What transform_parse makes of `ts=1760610600 level=error msg="boom" app=billing user=7`.
	row := wideRowOf(t, `{"ts":"1760610600","level":"error","msg":"boom","app":"billing","user":"7"}`, nil, wideTarget{attrs: "map"})
	if row["timestamp"] != "2025-10-16T10:30:00.000Z" || row["level"] != "error" || row["message"] != "boom" || row["service"] != "billing" {
		t.Fatalf("logfmt: %v", row)
	}
	if flat := row["attributes"].(map[string]string); len(flat) != 1 || flat["user"] != "7" {
		t.Fatalf("logfmt attributes: %v", flat)
	}
	for raw, want := range map[string]string{
		`{"time":1760610600123}`:           "2025-10-16T10:30:00.123Z",
		`{"timestamp":1760610600123456}`:   "2025-10-16T10:30:00.123Z",
		`{"ts":1760610600123456789}`:       "2025-10-16T10:30:00.123Z",
		`{"ts":1760610600.5}`:              "2025-10-16T10:30:00.500Z",
		`{"ts":"not a time","x":1}`:        "2030-01-01T00:00:00.000Z",
		`{"@timestamp":null,"level":30}`:   "2030-01-01T00:00:00.000Z",
		`{"Timestamp":"2025-10-16 10:30"}`: "2025-10-16T10:30:00.000Z",
	} {
		if got := wideRowOf(t, raw, nil, wideTarget{attrs: "json"})["timestamp"]; got != want {
			t.Errorf("%s: timestamp %v, want %s", raw, got, want)
		}
	}
	// An unparsable ts stays in attributes; a pino level is mapped.
	row = wideRowOf(t, `{"ts":"not a time","level":30,"msg":{"text":"structured"}}`, nil, wideTarget{attrs: "json"})
	attrs := row["attributes"].(map[string]interface{})
	if row["level"] != "info" || row["message"] != "" || attrs["ts"] != "not a time" || attrs["msg"] == nil {
		t.Fatalf("leftovers: %v", row)
	}
}

func TestWideRowMappingOverride(t *testing.T) {
	fields := map[string]interface{}{"mapping": map[string]interface{}{
		"service": "meta.component", "message": "text", "timestamp": "when", "level": "", "trace_id": []interface{}{"ctx.trace"},
	}}
	row := wideRowOf(t, `{"meta":{"component":"queue"},"text":"hi","when":"2025-10-16T10:30:00Z","severity":"debug","ctx":{"trace":"t1"},"service":"ignored"}`, fields, wideTarget{attrs: "json"})
	if row["service"] != "queue" || row["message"] != "hi" || row["timestamp"] != "2025-10-16T10:30:00.000Z" || row["level"] != "debug" || row["trace_id"] != "t1" {
		t.Fatalf("override: %v", row)
	}
	attrs := row["attributes"].(map[string]interface{})
	if attrs["service"] != "ignored" || attrs["meta"] != nil || attrs["ctx"] != nil {
		t.Fatalf("override attributes: %v", attrs)
	}
}

// The record's own maps are not changed by the mapping (routed sinks share them).
func TestWideRowLeavesRecordData(t *testing.T) {
	data := map[string]interface{}{"service": map[string]interface{}{"name": "api"}, "message": "m"}
	roles, _ := wideMapping(nil)
	wideRow(Record{Data: data}, roles, wideTarget{attrs: "json"}, time.Now())
	if data["service"].(map[string]interface{})["name"] != "api" || data["message"] != "m" {
		t.Fatalf("record changed: %v", data)
	}
}

func TestInferColumns(t *testing.T) {
	var recs []map[string]interface{}
	for i := 0; i < 150; i++ {
		r := map[string]interface{}{
			"id":       json.Number(fmt.Sprint(i)),
			"amount":   json.Number(fmt.Sprint(i)),
			"ok":       i%2 == 0,
			"at":       fmt.Sprintf("2025-10-16T10:%02d:00Z", i%60),
			"env":      []string{"prod", "staging", "dev"}[i%3],
			"path":     fmt.Sprintf("/item/%d", i),
			"tags":     []interface{}{"a", "b"},
			"scores":   []interface{}{json.Number("1"), json.Number("2.5")},
			"nested":   []interface{}{map[string]interface{}{"x": 1}},
			"meta":     map[string]interface{}{"k": "v"},
			"mixed":    json.Number("1"),
			"note":     nil,
			"big":      json.Number("18446744073709551615"),
			"maybe":    nil,
			"maybeenv": "eu",
		}
		if i == 7 {
			r["amount"] = json.Number("7.25")
			r["mixed"] = "seven"
			r["maybe"] = json.Number("3")
			r["maybeenv"] = nil
		}
		if i >= 100 {
			r["late"] = "only after the sample"
		}
		recs = append(recs, r)
	}
	types := map[string]string{}
	for _, c := range InferColumns(recs, true) {
		types[c.Name] = c.Type
	}
	want := map[string]string{
		"id": "Int64", "amount": "Float64", "ok": "Bool", "at": "DateTime64(3)",
		"env": "LowCardinality(String)", "path": "String", "tags": "Array(String)", "scores": "Array(Float64)",
		"nested": "String", "meta": "JSON", "mixed": "String", "note": "Nullable(String)", "big": "String",
		"maybe": "Nullable(Int64)", "maybeenv": "LowCardinality(Nullable(String))",
	}
	if len(types) != len(want) {
		t.Fatalf("columns: %v", types)
	}
	for k, v := range want {
		if types[k] != v {
			t.Errorf("%s = %s, want %s", k, types[k], v)
		}
	}

	// Before 25.3 objects are a map of strings, or JSON text.
	old := map[string]string{}
	for _, c := range InferColumns([]map[string]interface{}{{"m": map[string]interface{}{"a": "x"}, "o": map[string]interface{}{"n": json.Number("1")}}}, false) {
		old[c.Name] = c.Type
	}
	if old["m"] != "Map(String, String)" || old["o"] != "String" {
		t.Fatalf("old server types: %v", old)
	}
	// A small sample never gets LowCardinality; float64 input works too.
	small := InferColumns([]map[string]interface{}{{"env": "prod", "n": float64(1)}, {"env": "prod", "n": float64(2)}}, true)
	if small[0].Type != "String" || small[1].Type != "Int64" {
		t.Fatalf("small sample: %v", small)
	}
	// The inferred types pass the column type check.
	for _, c := range InferColumns(recs, false) {
		if err := validateColumnType(c.Type); err != nil {
			t.Errorf("%s: %v", c.Name, err)
		}
	}
}

// presetAgent answers version(), system.columns (from cols) and records
// every statement.
type presetAgent struct {
	mu      sync.Mutex
	version string
	cols    map[string]map[string]string // table -> name -> type
	sqls    []string
	inserts []tunnel.GatewayMessage
}

func (p *presetAgent) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.sqls = append(p.sqls, msg.SQL)
	data := `[]`
	switch {
	case strings.HasPrefix(msg.SQL, "SELECT version()"):
		data = fmt.Sprintf(`[{"v":%q}]`, p.version)
	case strings.Contains(msg.SQL, "system.columns"):
		var rows []map[string]string
		for n, typ := range p.cols[msg.Settings["param_t"]] {
			rows = append(rows, map[string]string{"name": n, "type": typ})
		}
		b, _ := json.Marshal(rows)
		data = string(b)
	case strings.HasPrefix(msg.SQL, "INSERT"):
		p.inserts = append(p.inserts, msg)
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
}

func (p *presetAgent) statements(prefix string) []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	var out []string
	for _, s := range p.sqls {
		if strings.HasPrefix(s, prefix) {
			out = append(out, s)
		}
	}
	return out
}

func TestSinkWideEventsCreatesAndMaps(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	pa := &presetAgent{version: "24.8.4.13", cols: map[string]map[string]string{}}
	agent := testutil.NewAgent(t, db, "worker-token", pa.respond)
	pipelineAccount(t, db, conn)
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d", "table": "ev", "create_table": true, "preset": "wide_events", "ttl_days": float64(7), "keep_raw": true}}
	if err := sink.Validate(cfg); err != nil {
		t.Fatal(err)
	}
	// The table the server would have after the CREATE.
	pa.cols["ev"] = map[string]string{"timestamp": "DateTime64(3)", "service": "LowCardinality(String)", "level": "LowCardinality(String)", "message": "String", "trace_id": "String", "attributes": "Map(LowCardinality(String), String)", "raw": "String"}
	batch := Batch{Records: []Record{{RawJSON: []byte(`{"ts":1760610600,"level":"E","msg":"x","app":"a","k":{"n":1}}`)}}}
	if _, err := sink.WriteBatch(context.Background(), cfg, batch); err != nil {
		t.Fatal(err)
	}
	creates := pa.statements("CREATE TABLE")
	if len(creates) != 1 || creates[0] != wideEventsDDL("d", "ev", "MergeTree", false, true, 7) {
		t.Fatalf("create: %v", creates)
	}
	ins := pa.inserts[0]
	line := strings.SplitN(ins.SQL, "\n", 2)[1]
	want := `{"attributes":{"k.n":"1"},"level":"e","message":"x","raw":"{\"ts\":1760610600,\"level\":\"E\",\"msg\":\"x\",\"app\":\"a\",\"k\":{\"n\":1}}","service":"a","timestamp":"2025-10-16T10:30:00.000Z","trace_id":""}` + "\n"
	if line != want {
		t.Fatalf("row\n%s\nwant\n%s", line, want)
	}
	if ins.Settings["date_time_input_format"] != "best_effort" || ins.Settings["input_format_skip_unknown_fields"] != "" {
		t.Fatalf("settings: %v", ins.Settings)
	}
}

func TestSinkCustomInfersFromBatchAndEvolves(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	pa := &presetAgent{version: "25.8.1.1", cols: map[string]map[string]string{}}
	agent := testutil.NewAgent(t, db, "worker-token", pa.respond)
	pipelineAccount(t, db, conn)
	pid, err := db.CreatePipeline("p", "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	runID, err := db.CreatePipelineRun(pid, "running")
	if err != nil {
		t.Fatal(err)
	}
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d", "table": "c", "create_table": true, "add_new_columns": true, sinkRunID: runID, sinkPipelineID: pid}}
	first := Batch{Records: []Record{
		{RawJSON: []byte(`{"id":1,"name":"a","meta":{"k":1}}`)},
		{RawJSON: []byte(`{"id":2,"name":null,"meta":{"k":2}}`)},
	}}
	if _, err := sink.WriteBatch(context.Background(), cfg, first); err != nil {
		t.Fatal(err)
	}
	creates := pa.statements("CREATE TABLE")
	if len(creates) != 1 || !strings.Contains(creates[0], "`id` Int64,\n  `meta` JSON,\n  `name` Nullable(String)\n)") {
		t.Fatalf("create: %v", creates)
	}
	if len(pa.statements("ALTER")) != 0 {
		t.Fatal("altered a table it just created")
	}

	// The created table has the inferred columns; new keys are added once.
	pa.cols["c"] = map[string]string{"id": "Int64", "name": "Nullable(String)", "meta": "JSON"}
	second := Batch{Records: []Record{
		{RawJSON: []byte(`{"id":"3","name":5,"score":1.5,"at":"2025-10-16T10:30:00Z"}`)},
		{RawJSON: []byte(`{"id":4,"score":2}`)},
	}}
	for i := 0; i < 2; i++ {
		if _, err := sink.WriteBatch(context.Background(), cfg, second); err != nil {
			t.Fatal(err)
		}
	}
	alters := pa.statements("ALTER")
	if len(alters) != 1 || alters[0] != "ALTER TABLE `d`.`c` ADD COLUMN IF NOT EXISTS `at` DateTime64(3), ADD COLUMN IF NOT EXISTS `score` Float64" {
		t.Fatalf("alters: %v", alters)
	}
	logs, _ := db.GetPipelineRunLogs(runID, 10)
	if len(logs) != 2 || !strings.Contains(logs[0].Message+logs[1].Message, "Added column score Float64 to d.c") {
		t.Fatalf("run log: %+v", logs)
	}
	audits, _ := db.GetAuditLogs(10)
	n := 0
	for _, a := range audits {
		if a.Action == "pipeline.column_added" && a.Details != nil && strings.HasPrefix(*a.Details, "Pipeline "+pid+": Added column") {
			n++
		}
	}
	if n != 2 {
		t.Fatalf("audit entries: %d", n)
	}

	// With the flag off nothing is altered and inserts go as they are.
	off := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg.Fields["add_new_columns"] = false
	if _, err := off.WriteBatch(context.Background(), cfg, Batch{Records: []Record{{RawJSON: []byte(`{"id":5,"brand_new":true}`)}}}); err != nil {
		t.Fatal(err)
	}
	if len(pa.statements("ALTER")) != 1 {
		t.Fatal("altered with add_new_columns off")
	}
}

// A wide events table gets the raw column when keep_raw is turned on after
// it was created, and nothing for other new keys.
func TestSinkWideEventsEvolvesLayoutOnly(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	pa := &presetAgent{version: "25.8.1.1", cols: map[string]map[string]string{
		"w": {"timestamp": "DateTime64(3)", "service": "LowCardinality(String)", "level": "LowCardinality(String)", "message": "String", "trace_id": "String", "attributes": "JSON"},
	}}
	agent := testutil.NewAgent(t, db, "worker-token", pa.respond)
	pipelineAccount(t, db, conn)
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d", "table": "w", "preset": "wide_events", "add_new_columns": true}}
	rec := Batch{Records: []Record{{RawJSON: []byte(`{"message":"m","new_key":1}`)}}}
	if _, err := sink.WriteBatch(context.Background(), cfg, rec); err != nil {
		t.Fatal(err)
	}
	if n := len(pa.statements("ALTER")); n != 0 {
		t.Fatalf("%d alters without keep_raw", n)
	}
	if !strings.Contains(pa.inserts[0].SQL, `"attributes":{"new_key":1}`) {
		t.Fatalf("insert: %s", pa.inserts[0].SQL)
	}
	sink = NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg.Fields["keep_raw"] = true
	if _, err := sink.WriteBatch(context.Background(), cfg, rec); err != nil {
		t.Fatal(err)
	}
	alters := pa.statements("ALTER")
	if len(alters) != 1 || alters[0] != "ALTER TABLE `d`.`w` ADD COLUMN IF NOT EXISTS `raw` String CODEC(ZSTD(3))" {
		t.Fatalf("alters: %v", alters)
	}
	if !strings.Contains(pa.inserts[1].SQL, `"raw":"{\"message\":\"m\",\"new_key\":1}"`) {
		t.Fatalf("raw not written after the column was added: %s", pa.inserts[1].SQL)
	}
}

// Sinks saved before presets: no preset means custom (OpenTelemetry for an
// ingest logs or traces source); a sink that does not create its table
// inserts exactly as before.
func TestSinkWithoutPresetKeepsBehaviour(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	pa := &presetAgent{version: "25.8.1.1"}
	agent := testutil.NewAgent(t, db, "worker-token", pa.respond)
	pipelineAccount(t, db, conn)
	rec := Batch{Records: []Record{{RawJSON: []byte(`{"a":1}`)}}, DedupToken: "tok"}

	plain := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d", "table": "plain"}}
	if _, err := NewClickHouseSink(agent.Gateway, db, "test-secret").WriteBatch(context.Background(), plain, rec); err != nil {
		t.Fatal(err)
	}
	if got := pa.sqls; len(got) != 1 || got[0] != "INSERT INTO `d`.`plain` FORMAT JSONEachRow\n{\"a\":1}\n" {
		t.Fatalf("statements: %q", got)
	}
	// The query tag is the only addition: a sink outside a run has no
	// pipeline to point at.
	if s := pa.inserts[0].Settings; len(s) != 2 || s["insert_deduplication_token"] != "tok" || s["log_comment"] != (tunnel.QueryTag{Surface: "pipeline"}).LogComment() {
		t.Fatalf("settings: %v", s)
	}

	otel := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d", "table": "otel_logs", "create_table": true, sinkOTelSignal: SignalLogs, sinkOTelTTLDays: 3}}
	if _, err := NewClickHouseSink(agent.Gateway, db, "test-secret").WriteBatch(context.Background(), otel, rec); err != nil {
		t.Fatal(err)
	}
	creates := pa.statements("CREATE TABLE")
	if len(creates) != 1 || !strings.Contains(creates[0], "TimestampTime DateTime DEFAULT") || !strings.Contains(creates[0], "toIntervalDay(3)") {
		t.Fatalf("otel create: %v", creates)
	}
	if s := pa.inserts[1].Settings; s["input_format_skip_unknown_fields"] != "1" || s["date_time_input_format"] != "" {
		t.Fatalf("otel settings: %v", s)
	}

	// An explicit otel_traces preset works without an ingest source and
	// takes the sink's ttl_days.
	traces := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "d", "table": "spans", "create_table": true, "preset": "otel_traces", "ttl_days": float64(9)}}
	if _, err := NewClickHouseSink(agent.Gateway, db, "test-secret").WriteBatch(context.Background(), traces, rec); err != nil {
		t.Fatal(err)
	}
	creates = pa.statements("CREATE")
	if len(creates) != 4 || !strings.Contains(creates[1], "toIntervalDay(9)") || !strings.HasPrefix(creates[3], "CREATE MATERIALIZED VIEW") {
		t.Fatalf("traces create: %v", creates)
	}
}
