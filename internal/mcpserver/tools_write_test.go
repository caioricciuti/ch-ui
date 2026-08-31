package mcpserver

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// writeDeps builds deps plus one read key and one read_write key.
func writeDeps(t *testing.T) (Deps, string, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	connID, err := db.CreateConnection(database.CreateConnectionParams{
		Name:        "test-conn",
		TunnelToken: "cht_00000000000000000000000000000000",
		Type:        database.ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	cfg := &config.Config{AppSecretKey: "test-secret-key-for-mcp-tests"}
	enc, _ := crypto.Encrypt("chpass", cfg.AppSecretKey)

	readKey, readHash, readPrefix := GenerateKey()
	if _, err := db.CreateMCPKey("reader", readHash, readPrefix, connID, "default", enc, "read", "", "admin"); err != nil {
		t.Fatalf("create read key: %v", err)
	}
	writeKey, writeHash, writePrefix := GenerateKey()
	if _, err := db.CreateMCPKey("writer", writeHash, writePrefix, connID, "default", enc, "read_write", "", "admin"); err != nil {
		t.Fatalf("create write key: %v", err)
	}

	return Deps{DB: db, Gateway: tunnel.NewGateway(db), Config: cfg}, readKey, writeKey
}

func callTool(t *testing.T, h http.Handler, key, name, argsJSON string) string {
	t.Helper()
	body := `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"` + name + `","arguments":` + argsJSON + `}}`
	rec := mcpRequest(t, h, key, body)
	if rec.Code != http.StatusOK {
		t.Fatalf("%s transport: want 200, got %d: %s", name, rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestScopeGating(t *testing.T) {
	deps, readKey, writeKey := writeDeps(t)
	h := Handler(deps)

	writeTools := []string{"save_query", "create_dashboard", "create_model", "create_pipeline"}

	readList := mcpRequest(t, h, readKey, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`).Body.String()
	for _, tool := range writeTools {
		if strings.Contains(readList, `"`+tool+`"`) {
			t.Errorf("read key must not see %q", tool)
		}
	}
	// list tools are available to every key
	if !strings.Contains(readList, "list_models") {
		t.Error("read key should see list_models")
	}

	writeList := mcpRequest(t, h, writeKey, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`).Body.String()
	for _, tool := range writeTools {
		if !strings.Contains(writeList, `"`+tool+`"`) {
			t.Errorf("write key should see %q", tool)
		}
	}

	// calling a write tool with a read key is refused by the protocol
	res := callTool(t, h, readKey, "save_query", `{"name":"x","sql":"SELECT 1"}`)
	if !strings.Contains(res, "error") {
		t.Errorf("read key calling save_query should error, got: %s", res)
	}
}

func TestSaveQueryTool(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)

	res := callTool(t, h, writeKey, "save_query", `{"name":"top signups","sql":"SELECT count() FROM signups","description":"daily"}`)
	if strings.Contains(res, "isError") {
		t.Fatalf("save_query failed: %s", res)
	}
	queries, err := deps.DB.GetSavedQueries()
	if err != nil || len(queries) != 1 {
		t.Fatalf("expected 1 saved query, got %d (%v)", len(queries), err)
	}
	if queries[0].Name != "top signups" || queries[0].CreatedBy == nil || *queries[0].CreatedBy != "mcp:writer" {
		t.Errorf("unexpected saved query: %+v", queries[0])
	}
}

func TestCreateDashboardTool(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)

	res := callTool(t, h, writeKey, "create_dashboard",
		`{"name":"Signups","panels":[{"name":"Total","sql":"SELECT count() FROM s","type":"stat"},{"name":"By day","sql":"SELECT day, count() FROM s GROUP BY day"}]}`)
	if strings.Contains(res, "isError") {
		t.Fatalf("create_dashboard failed: %s", res)
	}
	dashboards, err := deps.DB.GetDashboards()
	if err != nil || len(dashboards) != 1 {
		t.Fatalf("expected 1 dashboard, got %d (%v)", len(dashboards), err)
	}
	panels, err := deps.DB.GetPanelsByDashboard(dashboards[0].ID)
	if err != nil || len(panels) != 2 {
		t.Fatalf("expected 2 panels, got %d (%v)", len(panels), err)
	}
	// second panel auto-laid-out next to the first
	if panels[0].LayoutX == panels[1].LayoutX && panels[0].LayoutY == panels[1].LayoutY {
		t.Error("panels should not overlap in the automatic layout")
	}
}

func TestCreateModelTool(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)

	res := callTool(t, h, writeKey, "create_model",
		`{"name":"daily_rollup","sql":"SELECT day, count() c FROM events GROUP BY day","materialization":"table"}`)
	if strings.Contains(res, "isError") {
		t.Fatalf("create_model failed: %s", res)
	}
	if !strings.Contains(res, "draft") {
		t.Errorf("model should be created as draft: %s", res)
	}

	// invalid identifier is rejected
	res = callTool(t, h, writeKey, "create_model", `{"name":"bad name!","sql":"SELECT 1"}`)
	if !strings.Contains(res, "isError") {
		t.Errorf("invalid model name should be rejected: %s", res)
	}

	// duplicate name on the same connection is rejected
	res = callTool(t, h, writeKey, "create_model", `{"name":"daily_rollup","sql":"SELECT 2"}`)
	if !strings.Contains(res, "isError") {
		t.Errorf("duplicate model name should be rejected: %s", res)
	}
}

func TestCreatePipelineTool(t *testing.T) {
	deps, _, writeKey := writeDeps(t)
	h := Handler(deps)

	res := callTool(t, h, writeKey, "create_pipeline",
		`{"name":"events-ingest","source_type":"source_webhook","target_database":"default","target_table":"events"}`)
	if strings.Contains(res, "isError") {
		t.Fatalf("create_pipeline failed: %s", res)
	}
	pipelines, err := deps.DB.GetPipelines()
	if err != nil || len(pipelines) != 1 {
		t.Fatalf("expected 1 pipeline, got %d (%v)", len(pipelines), err)
	}
	if pipelines[0].Status != "draft" {
		t.Errorf("pipeline should be draft, got %s", pipelines[0].Status)
	}
	nodes, edges, err := deps.DB.GetPipelineGraph(pipelines[0].ID)
	if err != nil || len(nodes) != 2 || len(edges) != 1 {
		t.Fatalf("expected 2 nodes + 1 edge, got %d/%d (%v)", len(nodes), len(edges), err)
	}

	// bad source type rejected
	res = callTool(t, h, writeKey, "create_pipeline",
		`{"name":"x","source_type":"source_ftp","target_database":"d","target_table":"t"}`)
	if !strings.Contains(res, "isError") {
		t.Errorf("unknown source type should be rejected: %s", res)
	}
}
