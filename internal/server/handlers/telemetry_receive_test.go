package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// fakeCH answers DESCRIBE and CREATE for the receive setup: tables maps
// "db.table" to its columns, a CREATE TABLE adds the exporter columns.
type fakeCH struct {
	mu       sync.Mutex
	tables   map[string][]string
	creates  []string
	denyDDL  bool
	noDBs    map[string]bool
	describe int
}

var (
	describeRe = regexp.MustCompile("DESCRIBE TABLE `([^`]+)`\\.`([^`]+)`")
	createRe   = regexp.MustCompile("^CREATE TABLE IF NOT EXISTS `([^`]+)`\\.`([^`]+)`")
)

func (f *fakeCH) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	if msg.Type != "query" {
		return nil
	}
	sql := msg.SQL
	if sql == "" {
		sql = msg.Query
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	fail := func(format string, args ...interface{}) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_error", Error: fmt.Sprintf(format, args...)}
	}
	if m := describeRe.FindStringSubmatch(sql); m != nil {
		f.describe++
		if f.noDBs[m[1]] {
			return fail("Code: 81. DB::Exception: Database %s does not exist. (UNKNOWN_DATABASE)", m[1])
		}
		cols, ok := f.tables[m[1]+"."+m[2]]
		if !ok {
			return fail("Code: 60. DB::Exception: Table %s.%s does not exist. (UNKNOWN_TABLE)", m[1], m[2])
		}
		rows := make([]map[string]string, 0, len(cols))
		for _, c := range cols {
			rows = append(rows, map[string]string{"name": c, "type": "String"})
		}
		data, _ := json.Marshal(rows)
		return &tunnel.AgentMessage{Type: "query_result", Data: data, Meta: json.RawMessage(`[{"name":"name","type":"String"},{"name":"type","type":"String"}]`)}
	}
	if strings.HasPrefix(sql, "CREATE") {
		if f.denyDDL {
			return fail("Code: 497. DB::Exception: alice: Not enough privileges. (ACCESS_DENIED)")
		}
		f.creates = append(f.creates, sql)
		if m := createRe.FindStringSubmatch(sql); m != nil && !strings.HasSuffix(m[2], "_trace_id_ts") {
			key := m[1] + "." + m[2]
			if _, ok := f.tables[key]; !ok {
				f.tables[key] = otelColumns(sql)
			}
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	}
	return fail("Code: 62. DB::Exception: unexpected query in test: %s", sql)
}

// otelColumns are the columns of the exporter table a CREATE makes.
func otelColumns(ddl string) []string {
	if strings.Contains(ddl, "ParentSpanId") {
		m := telemetry.DefaultTracesMapping()
		src := telemetry.Source{Kind: telemetry.KindTraces, Traces: &m}
		return append(src.RequiredColumns(), "Events.Timestamp", "Events.Name", "Links.TraceId")
	}
	m := telemetry.DefaultLogsMapping()
	src := telemetry.Source{Kind: telemetry.KindLogs, Logs: &m}
	return src.RequiredColumns()
}

type receiveEnv struct {
	t      *testing.T
	db     *database.DB
	conn   string
	ch     *fakeCH
	h      *TelemetryHandler
	runner *pipelines.Runner
	router http.Handler
	enc    string
}

func newReceiveEnv(t *testing.T) *receiveEnv {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	ch := &fakeCH{tables: map[string][]string{}, noDBs: map[string]bool{}}
	agent := testutil.NewAgent(t, db, "worker-token", ch.respond)
	cfg := &config.Config{AppSecretKey: "test-secret"}
	enc, err := crypto.Encrypt("pw", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	sp, err := spool.Open(t.TempDir(), spool.Options{SyncMode: spool.SyncNone})
	if err != nil {
		t.Fatal(err)
	}
	hub := pipelines.NewHub(sp)
	runner := pipelines.NewRunner(db, agent.Gateway, cfg)
	runner.SetHub(hub)
	t.Cleanup(func() { runner.Stop(); sp.Close() })
	h := &TelemetryHandler{DB: db, Gateway: agent.Gateway, Config: cfg, Runner: runner}
	return &receiveEnv{t: t, db: db, conn: conn, ch: ch, h: h, runner: runner, router: h.Routes(), enc: enc}
}

func (e *receiveEnv) post(role, body string) (*httptest.ResponseRecorder, map[string]interface{}) {
	e.t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/receive", strings.NewReader(body))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
		UserRole: role, ClickhouseUser: "alice", ConnectionID: e.conn, EncryptedPassword: e.enc,
	}))
	rec := httptest.NewRecorder()
	e.router.ServeHTTP(rec, req)
	var out map[string]interface{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return rec, out
}

func (e *receiveEnv) pipelines() []database.Pipeline {
	e.t.Helper()
	all, err := e.db.GetPipelines()
	if err != nil {
		e.t.Fatal(err)
	}
	return all
}

func TestReceiveRejectsBadRequests(t *testing.T) {
	e := newReceiveEnv(t)
	for _, c := range []struct {
		name, role, body string
		want             int
	}{
		{"viewer", "viewer", `{"signal":"logs"}`, http.StatusForbidden},
		{"bad json", "analyst", `{`, http.StatusBadRequest},
		{"no signal", "analyst", `{}`, http.StatusBadRequest},
		{"metrics", "analyst", `{"signal":"metrics"}`, http.StatusBadRequest},
		{"bad database", "analyst", `{"signal":"logs","database":"a-b"}`, http.StatusBadRequest},
		{"bad table", "analyst", "{\"signal\":\"logs\",\"table\":\"t`; DROP\"}", http.StatusBadRequest},
		{"negative ttl", "analyst", `{"signal":"logs","ttl_days":-1}`, http.StatusBadRequest},
		{"ttl too long", "analyst", `{"signal":"logs","ttl_days":36501}`, http.StatusBadRequest},
	} {
		t.Run(c.name, func(t *testing.T) {
			if rec, _ := e.post(c.role, c.body); rec.Code != c.want {
				t.Fatalf("status = %d, want %d: %s", rec.Code, c.want, rec.Body)
			}
		})
	}
	if e.ch.describe != 0 || len(e.pipelines()) != 0 {
		t.Fatalf("a rejected request reached ClickHouse (%d) or made a pipeline (%d)", e.ch.describe, len(e.pipelines()))
	}

	// Background pipelines turned off: refused before anything is made.
	if err := e.db.SetBackgroundCredential(e.conn, database.BackgroundCredential{Worker: "pipeline", Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	rec, out := e.post("admin", `{"signal":"logs"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(fmt.Sprint(out["error"]), "Background accounts") {
		t.Fatalf("disabled background account: %d %s", rec.Code, rec.Body)
	}
	if e.ch.describe != 0 || len(e.pipelines()) != 0 {
		t.Fatal("disabled background account still reached ClickHouse or made a pipeline")
	}
}

func TestReceiveWithoutSpool(t *testing.T) {
	e := newReceiveEnv(t)
	e.h.Runner = pipelines.NewRunner(e.db, nil, &config.Config{})
	if rec, _ := e.post("analyst", `{"signal":"logs"}`); rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(e.pipelines()) != 0 {
		t.Fatal("pipeline created without a spool")
	}
}

func TestReceiveSetsUpLogs(t *testing.T) {
	e := newReceiveEnv(t)
	rec, out := e.post("analyst", `{"signal":"logs"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if out["database"] != "default" || out["table"] != "otel_logs" || out["table_created"] != true {
		t.Fatalf("answer: %v", out)
	}
	if len(e.ch.creates) != 1 || !strings.Contains(e.ch.creates[0], "toIntervalDay(30)") || !strings.Contains(e.ch.creates[0], "non_replicated_deduplication_window") {
		t.Fatalf("CREATE statements: %v", e.ch.creates)
	}

	p := out["pipeline"].(map[string]interface{})
	id := fmt.Sprint(p["id"])
	if p["name"] != "Receive logs" || p["status"] != "running" || p["signal"] != "logs" {
		t.Fatalf("pipeline: %v", p)
	}
	if stored, _ := e.db.GetPipelineByID(id); stored == nil || stored.Status != "running" || stored.ConnectionID != e.conn || stored.CreatedBy == nil || *stored.CreatedBy != "alice" {
		t.Fatalf("stored pipeline: %+v", stored)
	}
	nodes, edges, err := e.db.GetPipelineGraph(id)
	if err != nil || len(nodes) != 2 || len(edges) != 1 {
		t.Fatalf("graph: %v %v %v", nodes, edges, err)
	}
	cfgs := map[string]map[string]interface{}{}
	for _, n := range nodes {
		var cfg map[string]interface{}
		_ = json.Unmarshal([]byte(n.ConfigEncrypted), &cfg)
		cfgs[n.ID] = cfg
	}
	if src := cfgs[id+"-source-1"]; src["signal"] != "logs" || src["ttl_days"] != float64(30) {
		t.Fatalf("source config: %v", src)
	}
	if sink := cfgs[id+"-sink-1"]; sink["database"] != "default" || sink["table"] != "otel_logs" || sink["create_table"] != true {
		t.Fatalf("sink config: %v", sink)
	}
	if edges[0].SourceNodeID != id+"-source-1" || edges[0].TargetNodeID != id+"-sink-1" {
		t.Fatalf("edge: %+v", edges[0])
	}
	if e.runner.GetRunningMetrics(id) == nil {
		t.Fatal("pipeline is not running")
	}

	secret, _ := out["secret"].(string)
	key := out["key"].(map[string]interface{})
	if !strings.HasPrefix(secret, "chi_") || key["name"] != "Default key" || key["prefix"] != secret[4:12] || key["created_by"] != "alice" {
		t.Fatalf("key: %v secret %q", key, secret)
	}
	if strings.Contains(rec.Body.String(), "hash") {
		t.Fatalf("answer leaks the key hash: %s", rec.Body)
	}
	if keys, _ := e.db.ListIngestKeys(id); len(keys) != 1 || keys[0].ID != key["id"] {
		t.Fatalf("stored keys: %+v", keys)
	}

	source := out["source"].(map[string]interface{})
	sources, _ := e.db.ListTelemetrySources(e.conn)
	if len(sources) != 1 || sources[0].ID != source["id"] || sources[0].Kind != telemetry.KindLogs || sources[0].Database != "default" || sources[0].Table != "otel_logs" || sources[0].Logs == nil || *sources[0].Logs != telemetry.DefaultLogsMapping() {
		t.Fatalf("sources: %+v", sources)
	}

	for _, action := range []string{"pipeline.created", "pipeline.started", "ingest_key.created", "telemetry.source.created"} {
		logs, err := e.db.GetAuditLogsFiltered(10, "", action, "", "")
		if err != nil || len(logs) != 1 {
			t.Fatalf("%s audit: %v %v", action, logs, err)
		}
		if b, _ := json.Marshal(logs[0]); strings.Contains(string(b), secret) {
			t.Fatalf("%s audit leaks the secret: %s", action, b)
		}
	}

	// Again on the same table: reused, numbered pipeline, same source.
	rec, again := e.post("admin", `{"signal":"logs","ttl_days":7}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("second call: %d %s", rec.Code, rec.Body)
	}
	if again["table_created"] != false || len(e.ch.creates) != 1 {
		t.Fatalf("existing table not reused: %v, creates %d", again["table_created"], len(e.ch.creates))
	}
	if name := again["pipeline"].(map[string]interface{})["name"]; name != "Receive logs 2" {
		t.Fatalf("second pipeline name: %v", name)
	}
	if again["source"].(map[string]interface{})["id"] != source["id"] {
		t.Fatalf("second call made another source: %v", again["source"])
	}
	if sources, _ := e.db.ListTelemetrySources(e.conn); len(sources) != 1 {
		t.Fatalf("sources after the second call: %d", len(sources))
	}
	if logs, _ := e.db.GetAuditLogsFiltered(10, "", "telemetry.source.created", "", ""); len(logs) != 1 {
		t.Fatalf("reused source audited as created: %d", len(logs))
	}
}

func TestReceiveSetsUpTraces(t *testing.T) {
	e := newReceiveEnv(t)
	rec, out := e.post("analyst", `{"signal":"traces","database":"obs","table":"spans","ttl_days":0}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	if len(e.ch.creates) != 3 {
		t.Fatalf("traces need the spans table, the lookup table and its view: %v", e.ch.creates)
	}
	for _, stmt := range e.ch.creates {
		if strings.Contains(stmt, "TTL ") {
			t.Fatalf("ttl_days 0 must create no TTL: %s", stmt)
		}
	}
	if !strings.Contains(e.ch.creates[0], "`obs`.`spans`") || !strings.Contains(e.ch.creates[1], "`obs`.`spans_trace_id_ts`") {
		t.Fatalf("CREATE statements: %v", e.ch.creates)
	}
	if p := out["pipeline"].(map[string]interface{}); p["name"] != "Receive traces" || p["signal"] != "traces" {
		t.Fatalf("pipeline: %v", p)
	}
	source := out["source"].(map[string]interface{})
	if source["kind"] != "traces" || source["database"] != "obs" || source["table"] != "spans" {
		t.Fatalf("source: %v", source)
	}
}

func TestReceiveRefusesIncompatibleTable(t *testing.T) {
	e := newReceiveEnv(t)
	e.ch.tables["default.otel_logs"] = []string{"Timestamp", "Body", "message"}
	rec, out := e.post("analyst", `{"signal":"logs"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d: %s", rec.Code, rec.Body)
	}
	missing, _ := out["missing"].([]interface{})
	got := fmt.Sprint(missing)
	if !strings.Contains(got, "SeverityText") || !strings.Contains(got, "ServiceName") || strings.Contains(got, "Body") {
		t.Fatalf("missing: %v", missing)
	}
	if !strings.Contains(fmt.Sprint(out["error"]), "Pick another table name") {
		t.Fatalf("error: %v", out["error"])
	}
	if len(e.ch.creates) != 0 || len(e.pipelines()) != 0 {
		t.Fatal("an incompatible table still made a table or a pipeline")
	}
}

func TestReceiveClickHouseErrors(t *testing.T) {
	e := newReceiveEnv(t)
	e.ch.denyDDL = true
	rec, out := e.post("analyst", `{"signal":"logs"}`)
	if rec.Code != http.StatusForbidden || !strings.Contains(fmt.Sprint(out["error"]), "CREATE TABLE") {
		t.Fatalf("no CREATE grant: %d %s", rec.Code, rec.Body)
	}
	e.ch.noDBs["nope"] = true
	if rec, _ := e.post("analyst", `{"signal":"logs","database":"nope"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown database: %d %s", rec.Code, rec.Body)
	}
	if len(e.pipelines()) != 0 {
		t.Fatal("a ClickHouse error still made a pipeline")
	}
}

func TestReceiveRollsBack(t *testing.T) {
	e := newReceiveEnv(t)
	e.h.startPipeline = func(string) error { return errors.New("boom") }
	if rec, _ := e.post("analyst", `{"signal":"logs"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("start failure: %d %s", rec.Code, rec.Body)
	}
	if n := len(e.pipelines()); n != 0 {
		t.Fatalf("start failure left %d pipelines", n)
	}

	// The key fails after the pipeline started: it is stopped and removed,
	// the table this call created stays.
	e.h.startPipeline = nil
	var started string
	e.h.newIngestKey = func() (string, string, string, error) {
		all := e.pipelines()
		if len(all) == 1 {
			started = all[0].ID
		}
		return "", "", "", errors.New("no entropy")
	}
	if rec, _ := e.post("analyst", `{"signal":"logs"}`); rec.Code != http.StatusInternalServerError {
		t.Fatalf("key failure: %d %s", rec.Code, rec.Body)
	}
	if started == "" {
		t.Fatal("the pipeline did not exist when the key was made")
	}
	if n := len(e.pipelines()); n != 0 {
		t.Fatalf("key failure left %d pipelines", n)
	}
	if e.runner.GetRunningMetrics(started) != nil {
		t.Fatal("rolled back pipeline still running")
	}
	if keys, _ := e.db.ListIngestKeys(started); len(keys) != 0 {
		t.Fatalf("rolled back pipeline kept keys: %v", keys)
	}
	if sources, _ := e.db.ListTelemetrySources(e.conn); len(sources) != 0 {
		t.Fatalf("rolled back setup registered a source: %v", sources)
	}
	if _, ok := e.ch.tables["default.otel_logs"]; !ok {
		t.Fatal("the created table should stay")
	}
	for _, action := range []string{"pipeline.created", "ingest_key.created"} {
		if logs, _ := e.db.GetAuditLogsFiltered(10, "", action, "", ""); len(logs) != 0 {
			t.Fatalf("a rolled back setup was audited as %s", action)
		}
	}
}
