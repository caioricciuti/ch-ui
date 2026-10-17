package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestPipelineRoutingAPI(t *testing.T) {
	db, err := dbtest.Open(t, "routing.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, _ := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	id, _ := db.CreatePipeline("routed", "", conn, "analyst")

	sp, err := spool.Open(t.TempDir(), spool.Options{SyncMode: spool.SyncNone})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	cfg := &config.Config{AppSecretKey: "k"}
	pro := config.ProNone
	cfg.SetProAccessForTest(func() config.ProAccess { return pro })
	runner := pipelines.NewRunner(db, nil, cfg)
	runner.SetHub(pipelines.NewHub(sp))
	t.Cleanup(runner.Stop)
	router := (&PipelinesHandler{DB: db, Runner: runner, Config: cfg}).Routes()
	as := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "analyst", ClickhouseUser: "alice", ConnectionID: conn}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	var e struct {
		Error  string `json:"error"`
		NodeID string `json:"node_id"`
	}
	decodeErr := func(rec *httptest.ResponseRecorder) {
		e.Error, e.NodeID = "", ""
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
	}

	routeCfg := `{"mode":"first","unmatched":"drop","routes":[{"id":"errors","name":"Errors","rules":[{"field":"level","op":"eq","value":"error"}]},{"id":"rest"}]}`
	graph := func(route string) string {
		return `{"nodes":[
			{"id":"src","node_type":"source_webhook","label":"Webhook","config":{}},
			{"id":"rt","node_type":"transform_route","label":"Route","config":` + route + `},
			{"id":"k1","node_type":"sink_clickhouse","label":"Errors","config":{"database":"default","table":"errors"}},
			{"id":"k2","node_type":"sink_clickhouse","label":"Rest","config":{"database":"default","table":"rest"}}
		],"edges":[
			{"id":"e1","source_node_id":"src","target_node_id":"rt"},
			{"id":"e2","source_node_id":"rt","target_node_id":"k1","source_handle":"errors"},
			{"id":"e3","source_node_id":"rt","target_node_id":"k2","source_handle":"rest"}
		]}`
	}

	// Save: a bad route config is refused naming the node; a good one saves
	// without Pro.
	rec := as(http.MethodPut, "/"+id+"/graph", graph(`{"routes":[{"id":"Errors"}]}`))
	if decodeErr(rec); rec.Code != http.StatusBadRequest || e.NodeID != "rt" || !strings.HasPrefix(e.Error, "Route: route 1: id must be") {
		t.Fatalf("bad save: %d %s", rec.Code, rec.Body)
	}
	if rec := as(http.MethodPut, "/"+id+"/graph", graph(routeCfg)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}

	// Start and preview need Pro.
	rec = as(http.MethodPost, "/"+id+"/start", "")
	if decodeErr(rec); rec.Code != http.StatusPaymentRequired || e.Error != "Routing needs CH-UI Pro" || e.NodeID != "rt" {
		t.Fatalf("start without Pro: %d %s", rec.Code, rec.Body)
	}
	preview := func(nodes, branch string) *httptest.ResponseRecorder {
		body := fmt.Sprintf(`{"nodes":[%s],"branch":%q,"records":[{"level":"error","n":1},{"level":"info","n":2}]}`, nodes, branch)
		return as(http.MethodPost, "/"+id+"/preview-transforms", body)
	}
	routeNode := `{"id":"rt","node_type":"transform_route","config":` + routeCfg + `}`
	mapNode := `{"id":"m","node_type":"transform_map","config":{"set":[{"field":"branch","value":"errors"}]}}`
	rec = preview(routeNode, "")
	if decodeErr(rec); rec.Code != http.StatusPaymentRequired || e.Error != "Routing needs CH-UI Pro" {
		t.Fatalf("preview without Pro: %d %s", rec.Code, rec.Body)
	}
	pro = config.ProActive

	type step struct {
		NodeID string           `json:"node_id"`
		Output []map[string]any `json:"output"`
		Kept   []int            `json:"kept"`
		Routes []struct {
			Handle string `json:"handle"`
			Kept   []int  `json:"kept"`
		} `json:"routes"`
		Unmatched []int `json:"unmatched"`
	}
	var out struct {
		Steps []step `json:"steps"`
	}
	rec = preview(routeNode, "")
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK || len(out.Steps) != 1 {
		t.Fatalf("route preview: %d %s", rec.Code, rec.Body)
	}
	if st := out.Steps[0]; st.NodeID != "rt" || len(st.Routes) != 2 || fmt.Sprint(st.Routes[0].Kept) != "[0]" || fmt.Sprint(st.Routes[1].Kept) != "[1]" || len(st.Unmatched) != 0 || st.Unmatched == nil {
		t.Fatalf("route step: %s", rec.Body)
	}
	out.Steps = nil
	rec = preview(routeNode+","+mapNode, "errors")
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK || len(out.Steps) != 2 ||
		fmt.Sprint(out.Steps[0].Kept) != "[0]" || len(out.Steps[1].Output) != 1 || out.Steps[1].Output[0]["branch"] != "errors" || out.Steps[1].Routes != nil {
		t.Fatalf("branch preview: %d %s", rec.Code, rec.Body)
	}
	rec = preview(routeNode+","+mapNode, "")
	if decodeErr(rec); rec.Code != http.StatusBadRequest || e.Error != "Set branch to preview nodes after a Route node" {
		t.Fatalf("branch missing: %d %s", rec.Code, rec.Body)
	}

	// A shape problem at start names the node, so the editor can mark it.
	broken := strings.Replace(graph(routeCfg), `,
			{"id":"e3","source_node_id":"rt","target_node_id":"k2","source_handle":"rest"}`, "", 1)
	if rec := as(http.MethodPut, "/"+id+"/graph", broken); rec.Code != http.StatusOK {
		t.Fatalf("save broken: %d %s", rec.Code, rec.Body)
	}
	rec = as(http.MethodPost, "/"+id+"/start", "")
	if decodeErr(rec); rec.Code != http.StatusBadRequest || e.NodeID != "rt" || e.Error != `Route: route "rest" is not connected to a sink` {
		t.Fatalf("start with an unconnected route: %d %s", rec.Code, rec.Body)
	}
	// A sink config error names the sink; a pipeline-wide problem has no
	// node_id.
	noTable := strings.Replace(graph(routeCfg), `"table":"rest"`, `"table":""`, 1)
	if rec := as(http.MethodPut, "/"+id+"/graph", noTable); rec.Code != http.StatusOK {
		t.Fatalf("save without table: %d %s", rec.Code, rec.Body)
	}
	rec = as(http.MethodPost, "/"+id+"/start", "")
	if decodeErr(rec); rec.Code != http.StatusBadRequest || e.NodeID != "k2" || e.Error != "Rest: table is required" {
		t.Fatalf("start with a bad sink: %d %s", rec.Code, rec.Body)
	}
	if rec := as(http.MethodPut, "/"+id+"/graph", `{"nodes":[{"id":"src","node_type":"source_webhook","label":"Webhook","config":{}}],"edges":[]}`); rec.Code != http.StatusOK {
		t.Fatalf("save without sink: %d %s", rec.Code, rec.Body)
	}
	rec = as(http.MethodPost, "/"+id+"/start", "")
	if decodeErr(rec); rec.Code != http.StatusBadRequest || e.NodeID != "" || !strings.HasPrefix(e.Error, "Add a ClickHouse sink") || strings.Contains(rec.Body.String(), "node_id") {
		t.Fatalf("start without a sink: %d %s", rec.Code, rec.Body)
	}
	if rec := as(http.MethodPut, "/"+id+"/graph", graph(routeCfg)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}

	// Running: status has the route counters and one entry per sink.
	if rec := as(http.MethodPost, "/"+id+"/start", ""); rec.Code != http.StatusOK {
		t.Fatalf("start with Pro: %d %s", rec.Code, rec.Body)
	}
	t.Cleanup(func() { _ = runner.StopPipeline(id) })
	rec = as(http.MethodGet, "/"+id+"/status", "")
	var st struct {
		Transforms []map[string]any `json:"transforms"`
		Sinks      []map[string]any `json:"sinks"`
		Rows       *int64           `json:"rows_ingested"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || len(st.Transforms) != 1 || len(st.Sinks) != 2 || st.Rows == nil {
		t.Fatalf("status: %s", rec.Body)
	}
	if r := st.Transforms[0]; r["node_id"] != "rt" || fmt.Sprint(r["outputs"]) != "[map[handle:errors out:0] map[handle:rest out:0]]" || fmt.Sprint(r["unmatched_dropped"]) != "0" {
		t.Fatalf("route counters: %s", rec.Body)
	}
	if s := st.Sinks[0]; s["node_id"] != "k1" || fmt.Sprint(s["rows"]) != "0" || fmt.Sprint(s["batches"]) != "0" || s["errors"] == nil || st.Sinks[1]["node_id"] != "k2" {
		t.Fatalf("sinks: %s", rec.Body)
	}
}
