package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestPipelineTransformsAPI(t *testing.T) {
	db, err := dbtest.Open(t, "transforms.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, _ := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	id, _ := db.CreatePipeline("hook", "", conn, "analyst")

	sp, err := spool.Open(t.TempDir(), spool.Options{SyncMode: spool.SyncNone, SegmentMaxAge: 20 * time.Millisecond})
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
	as := func(role, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: "alice", ConnectionID: conn}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	type errResp struct {
		Error  string `json:"error"`
		NodeID string `json:"node_id"`
	}
	decodeErr := func(rec *httptest.ResponseRecorder) errResp {
		var e errResp
		_ = json.Unmarshal(rec.Body.Bytes(), &e)
		return e
	}

	graph := func(transforms string) string {
		return `{"nodes":[
			{"id":"src","node_type":"source_webhook","label":"Webhook","config":{}},
			` + transforms + `
			{"id":"sink","node_type":"sink_clickhouse","label":"Sink","config":{"database":"default","table":"t"}}
		],"edges":[
			{"id":"e1","source_node_id":"src","target_node_id":"parse"},
			{"id":"e2","source_node_id":"parse","target_node_id":"sample"},
			{"id":"e3","source_node_id":"sample","target_node_id":"sink"}
		]}`
	}
	parseNode := `{"id":"parse","node_type":"transform_parse","label":"Parse","config":{"field":"msg","format":"logfmt","target":""}},`
	sampleNode := `{"id":"sample","node_type":"transform_sample","label":"Keep half","config":{"keep_percent":50}},`

	// Save: a bad transform config is refused naming the node; a Pro node
	// saves without Pro.
	rec := as("analyst", http.MethodPut, "/"+id+"/graph", graph(`{"id":"parse","node_type":"transform_parse","label":"Parse","config":{"field":"msg","format":"regex","pattern":"(["}},`+sampleNode))
	if e := decodeErr(rec); rec.Code != http.StatusBadRequest || e.NodeID != "parse" || !strings.HasPrefix(e.Error, "Parse: pattern") {
		t.Fatalf("bad save: %d %s", rec.Code, rec.Body)
	}
	if rec := as("analyst", http.MethodPut, "/"+id+"/graph", graph(parseNode+sampleNode)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}

	// Start without Pro: 402, node_id names the node.
	rec = as("analyst", http.MethodPost, "/"+id+"/start", "")
	if e := decodeErr(rec); rec.Code != http.StatusPaymentRequired || e.Error != "Sampling needs CH-UI Pro" || e.NodeID != "sample" {
		t.Fatalf("start without Pro: %d %s", rec.Code, rec.Body)
	}

	preview := func(nodes, records string) *httptest.ResponseRecorder {
		body := `{"nodes":[` + nodes + `]`
		if records != "" {
			body += `,"records":` + records
		}
		return as("analyst", http.MethodPost, "/"+id+"/preview-transforms", body+"}")
	}
	freeNodes := `{"id":"p1","node_type":"transform_parse","config":{"field":"msg","format":"logfmt","target":""}},
		{"id":"f1","node_type":"transform_filter","config":{"mode":"drop","rules":[{"field":"level","op":"eq","value":"debug"}]}}`

	// Unsaved configs with pasted records.
	rec = preview(freeNodes, `[{"msg":"level=debug a=1"},{"msg":"level=info big=1"},{"msg":"=oops","n":12345678901234567890}]`)
	var out struct {
		Input []map[string]any `json:"input"`
		Steps []struct {
			NodeID  string           `json:"node_id"`
			Output  []map[string]any `json:"output"`
			Dropped int              `json:"dropped"`
			Errors  []struct {
				Index   int    `json:"index"`
				Message string `json:"message"`
			} `json:"errors"`
			Kept  []int `json:"kept"`
			Drops []struct {
				Index  int    `json:"index"`
				Reason string `json:"reason"`
			} `json:"drops"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("preview: %d %s", rec.Code, rec.Body)
	}
	if len(out.Input) != 3 || len(out.Steps) != 2 || out.Steps[0].NodeID != "p1" || len(out.Steps[0].Errors) != 1 || out.Steps[0].Errors[0].Index != 2 ||
		out.Steps[1].Dropped != 1 || out.Steps[1].Drops[0].Index != 0 || out.Steps[1].Drops[0].Reason == "" || fmt.Sprint(out.Steps[1].Kept) != "[1 2]" || len(out.Steps[1].Output) != 2 || out.Steps[1].Output[0]["level"] != "info" {
		t.Fatalf("preview result: %s", rec.Body)
	}
	if !strings.Contains(rec.Body.String(), "12345678901234567890") {
		t.Fatalf("big integer lost digits: %s", rec.Body)
	}

	// Invalid config: 400 with the node id.
	rec = preview(`{"id":"bad","node_type":"transform_filter","config":{"rules":[{"field":"a","op":"like"}]}}`, `[{"a":1}]`)
	if e := decodeErr(rec); rec.Code != http.StatusBadRequest || e.NodeID != "bad" || !strings.Contains(e.Error, "unknown op") {
		t.Fatalf("invalid config: %d %s", rec.Code, rec.Body)
	}
	// Pro node: 402 without Pro, 200 with it.
	sampleSpec := `{"id":"s1","node_type":"transform_sample","config":{"keep_percent":100}}`
	rec = preview(sampleSpec, `[{"a":1}]`)
	if e := decodeErr(rec); rec.Code != http.StatusPaymentRequired || e.Error != "Sampling needs CH-UI Pro" || e.NodeID != "s1" {
		t.Fatalf("Pro preview without Pro: %d %s", rec.Code, rec.Body)
	}
	pro = config.ProActive
	if rec := preview(sampleSpec, `[{"a":1}]`); rec.Code != http.StatusOK {
		t.Fatalf("Pro preview with Pro: %d %s", rec.Code, rec.Body)
	}
	// No records and nothing to sample.
	rec = preview(freeNodes, "")
	if e := decodeErr(rec); rec.Code != http.StatusBadRequest || e.Error != "No sample records yet; send some data or paste records" {
		t.Fatalf("no sample: %d %s", rec.Code, rec.Body)
	}
	// Limits.
	many := "[" + strings.TrimSuffix(strings.Repeat(`{"a":1},`, 101), ",") + "]"
	if rec := preview(freeNodes, many); rec.Code != http.StatusBadRequest {
		t.Fatalf("101 records: %d", rec.Code)
	}
	huge := `[{"a":"` + strings.Repeat("x", 1<<20) + `"}]`
	if rec := preview(freeNodes, huge); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over 1 MB: %d", rec.Code)
	}
	if rec := as("viewer", http.MethodPost, "/"+id+"/preview-transforms", `{"nodes":[]}`); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer preview: %d", rec.Code)
	}

	// Running: the preview uses the last records that entered the chain and
	// the status reports per-node counters.
	if rec := as("analyst", http.MethodPost, "/"+id+"/start", ""); rec.Code != http.StatusOK {
		t.Fatalf("start with Pro: %d %s", rec.Code, rec.Body)
	}
	t.Cleanup(func() { _ = runner.StopPipeline(id) })
	var code int
	deadline := time.Now().Add(5 * time.Second)
	for code = 0; code != http.StatusOK && time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		w := httptest.NewRecorder()
		pipelines.HandleWebhook(w, httptest.NewRequest(http.MethodPost, "/api/pipelines/webhook/"+id, strings.NewReader(`{"msg":"level=info n=1"}`+"\n"+`{"msg":"level=debug n=2"}`)))
		code = w.Code
	}
	if code != http.StatusOK {
		t.Fatalf("webhook post: %d", code)
	}
	for len(runner.RecentRecords(id)) < 2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	rec = preview(freeNodes, "")
	out.Input, out.Steps = nil, nil
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK || len(out.Input) != 2 || out.Input[1]["msg"] != "level=debug n=2" {
		t.Fatalf("ring preview: %d %s", rec.Code, rec.Body)
	}
	rec = as("viewer", http.MethodGet, "/"+id+"/status", "")
	var st struct {
		Transforms []map[string]any `json:"transforms"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &st); err != nil || len(st.Transforms) != 2 || st.Transforms[0]["node_id"] != "parse" || fmt.Sprint(st.Transforms[0]["in"]) != "2" {
		t.Fatalf("status: %s", rec.Body)
	}
}
