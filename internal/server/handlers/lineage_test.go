package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

const lineageTablesFixture = `[
 {"database":"app","name":"src","uuid":"u1","engine":"MergeTree","engine_full":"MergeTree ORDER BY id","ddl":"","as_select":"","dependencies_database":["app","app"],"dependencies_table":["mv_to","mv_inner"],"total_rows":10,"total_bytes":100},
 {"database":"app","name":"tgt","uuid":"u2","engine":"MergeTree","engine_full":"MergeTree ORDER BY id","ddl":"","as_select":"","dependencies_database":[],"dependencies_table":[],"total_rows":"20","total_bytes":"200"},
 {"database":"app","name":"mv_to","uuid":"u3","engine":"MaterializedView","engine_full":"","ddl":"CREATE MATERIALIZED VIEW app.mv_to TO app.tgt (` + "`id`" + ` UInt64) AS SELECT id FROM app.src","as_select":"SELECT id FROM app.src","dependencies_database":[],"dependencies_table":[],"total_rows":null,"total_bytes":null},
 {"database":"app","name":"mv_inner","uuid":"aaaaaaaa-0000-0000-0000-000000000001","engine":"MaterializedView","engine_full":"","ddl":"CREATE MATERIALIZED VIEW app.mv_inner (` + "`id`" + ` UInt64) ENGINE = MergeTree ORDER BY id AS SELECT id FROM app.src","as_select":"SELECT id FROM app.src","dependencies_database":[],"dependencies_table":[],"total_rows":null,"total_bytes":null},
 {"database":"app","name":".inner_id.aaaaaaaa-0000-0000-0000-000000000001","uuid":"u5","engine":"MergeTree","engine_full":"","ddl":"","as_select":"","dependencies_database":[],"dependencies_table":[],"total_rows":5,"total_bytes":50},
 {"database":"rpt","name":"v0","uuid":"u7","engine":"View","engine_full":"","ddl":"CREATE VIEW rpt.v0 AS SELECT * FROM app.tgt AS t JOIN rpt.local USING id","as_select":"SELECT * FROM app.tgt AS t INNER JOIN rpt.local USING (id)","dependencies_database":[],"dependencies_table":[],"total_rows":null,"total_bytes":null},
 {"database":"app","name":"d","uuid":"u6","engine":"Dictionary","engine_full":"","ddl":"CREATE DICTIONARY app.d (` + "`id`" + ` UInt64) PRIMARY KEY id SOURCE(CLICKHOUSE(TABLE 'tgt')) LIFETIME(0) LAYOUT(FLAT())","as_select":"","dependencies_database":[],"dependencies_table":[],"total_rows":null,"total_bytes":null}
]`

// lineageFake answers the lineage queries; fail names the system tables
// that return an error instead.
type lineageFake struct {
	mu   sync.Mutex
	fail map[string]string
}

func (f *lineageFake) setFail(fail map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fail = fail
}

func (f *lineageFake) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	result := func(data string) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	}
	for table, errText := range f.fail {
		if strings.Contains(msg.SQL, table) {
			return &tunnel.AgentMessage{Type: "query_error", Error: errText}
		}
	}
	switch {
	case strings.Contains(msg.SQL, "system.dictionaries"):
		return result(`[{"database":"app","name":"d","source":"ClickHouse: app.tgt","status":"LOADED"}]`)
	case strings.Contains(msg.SQL, "system.view_refreshes"):
		return result(`[]`)
	case strings.Contains(msg.SQL, "hostName()") && strings.Contains(msg.SQL, "system.clusters"):
		return result(`[{"host":"chn1","hosts":3}]`)
	case strings.Contains(msg.SQL, "hostName()"):
		return result(`[{"host":"chn1"}]`)
	case strings.Contains(msg.SQL, "{name:String}") && msg.Settings["param_name"] == "hidden":
		return result(`[]`)
	case strings.Contains(msg.SQL, "{name:String}"):
		return result(`[
 {"database":"app","name":"mv_inner","uuid":"aaaaaaaa-0000-0000-0000-000000000001","engine":"MaterializedView","engine_full":"","ddl":"CREATE MATERIALIZED VIEW app.mv_inner (` + "`id`" + ` UInt64) ENGINE = MergeTree ORDER BY id AS SELECT id FROM app.src","total_rows":null,"total_bytes":null},
 {"database":"app","name":".inner_id.aaaaaaaa-0000-0000-0000-000000000001","uuid":"u5","engine":"MergeTree","engine_full":"MergeTree ORDER BY id","ddl":"CREATE TABLE x","total_rows":5,"total_bytes":50}]`)
	case strings.Contains(msg.SQL, "system.tables"):
		return result(lineageTablesFixture)
	}
	return &tunnel.AgentMessage{Type: "query_error", Error: "unexpected query"}
}

type lineageEnv struct {
	t      *testing.T
	h      *LineageHandler
	agent  *testutil.Agent
	fake   *lineageFake
	router chi.Router
	conn   string
	pass   string
}

func newLineageEnv(t *testing.T) *lineageEnv {
	db, conn := testutil.WorkerDB(t)
	fake := &lineageFake{}
	agent := testutil.NewAgent(t, db, "worker-token", fake.respond)
	encrypted, err := crypto.Encrypt("caller-password", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &LineageHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}}
	r := chi.NewRouter()
	r.Route("/api/lineage", h.Routes)
	return &lineageEnv{t: t, h: h, agent: agent, fake: fake, router: r, conn: conn, pass: encrypted}
}

func (e *lineageEnv) get(path, user string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	if user != "" {
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: e.conn, ClickhouseUser: user, EncryptedPassword: e.pass}))
	}
	rr := httptest.NewRecorder()
	e.router.ServeHTTP(rr, req)
	return rr
}

func (e *lineageEnv) graph(path, user string) lineageResponse {
	e.t.Helper()
	rr := e.get(path, user)
	if rr.Code != 200 {
		e.t.Fatalf("%s: %d %s", path, rr.Code, rr.Body.String())
	}
	var out lineageResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		e.t.Fatal(err)
	}
	out.raw = rr.Body.String()
	return out
}

type lineageResponse struct {
	Nodes []map[string]any `json:"nodes"`
	Edges []map[string]any `json:"edges"`
	Meta  struct {
		Host        string   `json:"host"`
		MultiNode   bool     `json:"multi_node"`
		GeneratedAt string   `json:"generated_at"`
		Cached      bool     `json:"cached"`
		NodeCount   int      `json:"node_count"`
		Warnings    []string `json:"warnings"`
	} `json:"meta"`
	raw string
}

func (r lineageResponse) node(id string) map[string]any {
	for _, n := range r.Nodes {
		if n["id"] == id {
			return n
		}
	}
	return nil
}

func (r lineageResponse) hasEdge(from, to, kind, confidence string) bool {
	for _, e := range r.Edges {
		if e["from"] == from && e["to"] == to && e["kind"] == kind && e["confidence"] == confidence {
			return true
		}
	}
	return false
}

func TestLineageGraph(t *testing.T) {
	e := newLineageEnv(t)
	for _, path := range []string{"/api/lineage", "/api/lineage/node?id=app.src"} {
		if rr := e.get(path, ""); rr.Code != 401 {
			t.Fatalf("%s anonymous: %d", path, rr.Code)
		}
	}
	if len(e.agent.Messages()) != 0 {
		t.Fatal("anonymous request reached ClickHouse")
	}
	e.fake.setFail(map[string]string{"system.dictionaries": "Code: 497. DB::Exception: alice: Not enough privileges"})

	g := e.graph("/api/lineage", "alice")
	if g.Meta.Host != "chn1" || !g.Meta.MultiNode || g.Meta.Cached || g.Meta.NodeCount != len(g.Nodes) || len(g.Nodes) != 7 {
		t.Fatalf("meta: %+v nodes=%d", g.Meta, len(g.Nodes))
	}
	if _, err := time.Parse(time.RFC3339, g.Meta.GeneratedAt); err != nil {
		t.Fatalf("generated_at: %q", g.Meta.GeneratedAt)
	}
	if len(g.Meta.Warnings) != 1 || !strings.Contains(g.Meta.Warnings[0], "system.dictionaries not readable") {
		t.Fatalf("warnings: %q", g.Meta.Warnings)
	}
	for _, want := range [][4]string{
		{"app.src", "app.mv_to", "triggers", "system"},
		{"app.src", "app.mv_inner", "triggers", "system"},
		{"app.mv_to", "app.tgt", "writes", "parsed"},
		{"app.tgt", "app.d", "sources", "parsed"},
		{"app.tgt", "rpt.v0", "reads", "parsed"},
		{"rpt.local", "rpt.v0", "reads", "parsed"},
	} {
		if !g.hasEdge(want[0], want[1], want[2], want[3]) {
			t.Fatalf("missing edge %v in %v", want, g.Edges)
		}
	}
	if len(g.Edges) != 6 {
		t.Fatalf("edges: %v", g.Edges)
	}
	mvInner := g.node("app.mv_inner")
	if mvInner == nil || mvInner["kind"] != "mv" || mvInner["rows"] != float64(5) || mvInner["bytes"] != float64(50) || mvInner["refresh"] != nil {
		t.Fatalf("mv_inner: %v", mvInner)
	}
	if tgt := g.node("app.tgt"); tgt["rows"] != float64(20) || tgt["engine"] != "MergeTree" || tgt["database"] != "app" || tgt["name"] != "tgt" {
		t.Fatalf("quoted counts: %v", tgt)
	}
	if strings.Contains(g.raw, ".inner_id") || strings.Contains(g.raw, "CREATE ") || strings.Contains(g.raw, `"ddl"`) {
		t.Fatalf("graph leaks inner tables or definitions: %s", g.raw)
	}

	msgs := e.agent.Messages()
	if len(msgs) != 4 {
		t.Fatalf("expected 4 metadata queries, got %d", len(msgs))
	}
	for _, m := range msgs {
		if m.User != "alice" || m.Password != "caller-password" {
			t.Fatal("not run as the session user")
		}
		if m.Settings["readonly"] != "1" || !strings.HasPrefix(m.SQL, "SELECT ") || strings.Contains(m.SQL, "query_log") {
			t.Fatalf("unsafe query: %s %v", m.SQL, m.Settings)
		}
	}

	// Cached for the same connection, user and databases.
	if g := e.graph("/api/lineage", "alice"); !g.Meta.Cached || len(g.Nodes) != 7 {
		t.Fatalf("second call not cached: %+v", g.Meta)
	}
	if n := len(e.agent.Messages()); n != 4 {
		t.Fatalf("cache hit ran %d queries", n-4)
	}
	// refresh=1 bypasses the cache and stores the new result.
	if g := e.graph("/api/lineage?refresh=1", "alice"); g.Meta.Cached {
		t.Fatal("refresh served from cache")
	}
	if n := len(e.agent.Messages()); n != 8 {
		t.Fatalf("refresh ran %d queries, want 4", n-4)
	}
	// Another ClickHouse user never shares an entry.
	if g := e.graph("/api/lineage", "bob"); g.Meta.Cached {
		t.Fatal("cache shared across users")
	}
	// Nor does another database list.
	before := len(e.agent.Messages())
	filtered := e.graph("/api/lineage?databases=app,%20app,x'y", "alice")
	if filtered.Meta.Cached {
		t.Fatal("cache shared across database lists")
	}
	// A view in another database that reads app.tgt through its own FROM is
	// a neighbour; what else it reads (rpt.local) is two edges away.
	if !filtered.hasEdge("app.tgt", "rpt.v0", "reads", "parsed") || filtered.node("rpt.v0")["kind"] != "view" ||
		filtered.node("rpt.local") != nil || len(filtered.Nodes) != 6 {
		t.Fatalf("cross-database reader: nodes %v edges %v", filtered.Nodes, filtered.Edges)
	}
	for _, m := range e.agent.Messages()[before:] {
		if strings.Contains(m.SQL, "x'y") || strings.Contains(m.SQL, "'app'") {
			t.Fatalf("database names in SQL: %s", m.SQL)
		}
		if strings.Contains(m.SQL, "system.tables") && m.Settings["param_databases"] != `['app','x''y']` {
			t.Fatalf("databases param: %q", m.Settings["param_databases"])
		}
	}
	// Expired entries are rebuilt.
	e.h.cache.now = func() time.Time { return time.Now().Add(time.Minute) }
	if g := e.graph("/api/lineage", "alice"); g.Meta.Cached {
		t.Fatal("expired entry served")
	}
}

func TestLineageOptionalTablesDegrade(t *testing.T) {
	e := newLineageEnv(t)
	e.fake.setFail(map[string]string{
		"system.view_refreshes": "Code: 497. DB::Exception: Not enough privileges",
		"system.clusters":       "Code: 497. DB::Exception: Not enough privileges",
	})
	g := e.graph("/api/lineage", "alice")
	if g.Meta.Host != "chn1" || g.Meta.MultiNode {
		t.Fatalf("host fallback: %+v", g.Meta)
	}
	want := []string{"system.view_refreshes not readable", "system.clusters not readable"}
	if len(g.Meta.Warnings) != len(want) {
		t.Fatalf("warnings: %q", g.Meta.Warnings)
	}
	for i, w := range want {
		if !strings.HasPrefix(g.Meta.Warnings[i], w) {
			t.Fatalf("warnings: %q", g.Meta.Warnings)
		}
	}

	// A server without system.view_refreshes has no refreshable MVs: no warning.
	e.fake.setFail(map[string]string{"system.view_refreshes": "Code: 60. DB::Exception: Table system.view_refreshes doesn't exist. (UNKNOWN_TABLE)"})
	if g := e.graph("/api/lineage?refresh=1", "alice"); len(g.Meta.Warnings) != 0 {
		t.Fatalf("warnings: %q", g.Meta.Warnings)
	}

	// Older column names: retried with the status only.
	e.fake.setFail(map[string]string{"last_success_time": "Code: 47. DB::Exception: Missing columns: 'last_success_time'"})
	before := len(e.agent.Messages())
	if g := e.graph("/api/lineage?refresh=1", "alice"); len(g.Meta.Warnings) != 0 {
		t.Fatalf("warnings: %q", g.Meta.Warnings)
	}
	retried := false
	for _, m := range e.agent.Messages()[before:] {
		retried = retried || strings.HasPrefix(m.SQL, "SELECT database, view, toString(status) AS status FROM system.view_refreshes")
	}
	if !retried {
		t.Fatal("view_refreshes not retried without times")
	}

	// system.tables is required: its failure is an error, without the remote text.
	e.fake.setFail(map[string]string{"FROM system.tables": "Code: 497. secret remote details"})
	rr := e.get("/api/lineage?refresh=1", "alice")
	if rr.Code != 502 || strings.Contains(rr.Body.String(), "secret remote details") {
		t.Fatalf("tables failure: %d %s", rr.Code, rr.Body.String())
	}
	if rr := e.get("/api/lineage?databases="+strings.Repeat("x", 2000), "alice"); rr.Code != 400 {
		t.Fatalf("long database name: %d", rr.Code)
	}
}

func TestLineageOlderServerWithoutAsSelect(t *testing.T) {
	e := newLineageEnv(t)
	var mu sync.Mutex
	var tablesSQL []string
	base := e.fake.respond
	agent := testutil.NewAgent(t, e.h.DB, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.Contains(msg.SQL, "FROM system.tables") {
			mu.Lock()
			tablesSQL = append(tablesSQL, msg.SQL)
			mu.Unlock()
			if strings.Contains(msg.SQL, "as_select AS") || strings.Contains(msg.SQL, ", as_select") {
				return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 47. DB::Exception: Missing columns: 'as_select' while processing query"}
			}
		}
		return base(msg)
	})
	e.h.Gateway = agent.Gateway
	g := e.graph("/api/lineage", "alice")
	if len(tablesSQL) != 2 || !strings.Contains(tablesSQL[1], "'' AS as_select") {
		t.Fatalf("no fallback: %q", tablesSQL)
	}
	// Definitions are parsed from the DDL instead.
	if !g.hasEdge("app.mv_to", "app.tgt", "writes", "parsed") || !g.hasEdge("app.src", "app.mv_to", "triggers", "system") {
		t.Fatalf("edges: %v", g.Edges)
	}
}

func TestLineageNode(t *testing.T) {
	e := newLineageEnv(t)
	rr := e.get("/api/lineage/node?id=app.mv_inner", "alice")
	if rr.Code != 200 {
		t.Fatalf("node: %d %s", rr.Code, rr.Body.String())
	}
	var n map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &n); err != nil {
		t.Fatal(err)
	}
	if n["id"] != "app.mv_inner" || n["kind"] != "mv" || n["rows"] != float64(5) || n["bytes"] != float64(50) ||
		!strings.HasPrefix(n["ddl"].(string), "CREATE MATERIALIZED VIEW") || n["engine"] != "MaterializedView" {
		t.Fatalf("node: %v", n)
	}
	for _, k := range []string{"database", "name", "engine_full"} {
		if _, ok := n[k]; !ok {
			t.Fatalf("node lacks %s", k)
		}
	}
	msgs := e.agent.Messages()
	last := msgs[len(msgs)-1]
	if last.Settings["param_database"] != "app" || last.Settings["param_name"] != "mv_inner" || strings.Contains(last.SQL, "mv_inner") {
		t.Fatalf("node query not parameterized: %s %v", last.SQL, last.Settings)
	}

	for path, want := range map[string]int{"/api/lineage/node?id=nodot": 400, "/api/lineage/node": 400, "/api/lineage/node?id=app.": 400} {
		if rr := e.get(path, "alice"); rr.Code != want {
			t.Fatalf("%s: %d", path, rr.Code)
		}
	}

	// Not visible: 404. database and name address a database with a dot.
	before := len(e.agent.Messages())
	if rr := e.get("/api/lineage/node?database=my.db&name=hidden", "alice"); rr.Code != 404 {
		t.Fatalf("hidden object: %d %s", rr.Code, rr.Body.String())
	}
	if msgs := e.agent.Messages()[before:]; len(msgs) != 1 || msgs[0].Settings["param_database"] != "my.db" {
		t.Fatalf("database and name params: %+v", msgs)
	}
}

func TestLineageTablesSQL(t *testing.T) {
	all := lineageTablesSQL(false, false, true)
	if strings.Contains(all, "{databases") || !strings.Contains(all, "as_select AS as_select") {
		t.Fatalf("unfiltered: %s", all)
	}
	wide := lineageTablesSQL(true, false, true)
	// Definitions of views, MVs and dictionaries are read in every database,
	// so readers outside the selection still link to it.
	if strings.Contains(wide, "AND has(") || !strings.Contains(wide, "as_select AS as_select") ||
		!strings.Contains(wide, "if(engine IN ('View', 'MaterializedView', 'Dictionary'), create_table_query, '') AS ddl") {
		t.Fatalf("filtered: %s", wide)
	}
	strict := lineageTablesSQL(true, true, false)
	if !strings.Contains(strict, "AND has({databases:Array(String)}, database)") || !strings.Contains(strict, "'' AS as_select") {
		t.Fatalf("strict: %s", strict)
	}
	for _, sql := range []string{all, wide, strict} {
		if !strings.Contains(sql, "LIMIT 50001") || strings.Contains(sql, "query_log") {
			t.Fatalf("unbounded: %s", sql)
		}
	}
}
