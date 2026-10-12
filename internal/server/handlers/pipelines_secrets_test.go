package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func pipelineRequestAs(router http.Handler, role, connectionID, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: role, ConnectionID: connectionID}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestPipelineSecretsAreEncryptedAndMasked(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "ch-ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreatePipeline("p", "", conn, "analyst")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AppSecretKey: "app-secret", DatabasePath: filepath.Join(dir, "ch-ui.db")}
	router := (&PipelinesHandler{DB: db, Config: cfg}).Routes()

	graph := func(source string) string {
		return `{"nodes":[{"id":"` + id + `-s","node_type":"source_kafka","label":"k","config":` + source + `},` +
			`{"id":"` + id + `-k","node_type":"sink_clickhouse","label":"c","config":{"database":"default","table":"t"}}],` +
			`"edges":[{"id":"` + id + `-e","source_node_id":"` + id + `-s","target_node_id":"` + id + `-k"}]}`
	}
	stored := func() map[string]interface{} {
		nodes, _, err := db.GetPipelineGraph(id)
		if err != nil {
			t.Fatal(err)
		}
		for _, n := range nodes {
			if n.NodeType == "source_kafka" {
				var m map[string]interface{}
				json.Unmarshal([]byte(n.ConfigEncrypted), &m)
				return m
			}
		}
		t.Fatal("no source node")
		return nil
	}

	if rec := pipelineRequestAs(router, "analyst", conn, http.MethodPut, "/"+id+"/graph", graph(`{"brokers":"b:9092","topic":"t","sasl_password":"hunter2"}`)); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
	}
	first, _ := stored()["sasl_password"].(string)
	if first == "" || strings.Contains(first, "hunter2") {
		t.Fatalf("secret stored in plaintext: %q", first)
	}

	rec := pipelineRequestAs(router, "viewer", conn, http.MethodGet, "/"+id, "")
	if strings.Contains(rec.Body.String(), "hunter2") || strings.Contains(rec.Body.String(), first) {
		t.Fatal("GET leaks the secret or its ciphertext")
	}
	if !strings.Contains(rec.Body.String(), `\"sasl_password\":\"***\"`) {
		t.Fatalf("GET does not mask: %s", rec.Body.String())
	}

	// Saving the masked form keeps the stored secret.
	if rec := pipelineRequestAs(router, "analyst", conn, http.MethodPut, "/"+id+"/graph", graph(`{"brokers":"b:9092","topic":"t2","sasl_password":"***"}`)); rec.Code != http.StatusOK {
		t.Fatalf("resave: %d", rec.Code)
	}
	if got := stored()["sasl_password"]; got != first {
		t.Fatalf("masked resave changed the secret: %v", got)
	}
}

func TestSQLiteSourceIsAdminOnly(t *testing.T) {
	dir := t.TempDir()
	db, err := database.Open(filepath.Join(dir, "ch-ui.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, _ := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	id, _ := db.CreatePipeline("p", "", conn, "admin")
	cfg := &config.Config{AppSecretKey: "app-secret", DatabasePath: filepath.Join(dir, "ch-ui.db")}
	router := (&PipelinesHandler{DB: db, Config: cfg}).Routes()

	graph := func(path string) string {
		src, _ := json.Marshal(map[string]interface{}{"db_type": "sqlite", "connection_string": path, "query": "SELECT 1"})
		return `{"nodes":[{"id":"` + id + `-s","node_type":"source_database","label":"d","config":` + string(src) + `}],"edges":[]}`
	}
	outside := filepath.Join(t.TempDir(), "app.db")
	if rec := pipelineRequestAs(router, "analyst", conn, http.MethodPut, "/"+id+"/graph", graph(outside)); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst save: %d, want 403", rec.Code)
	}
	if rec := pipelineRequestAs(router, "admin", conn, http.MethodPut, "/"+id+"/graph", graph(filepath.Join(dir, "ch-ui.db"))); rec.Code != http.StatusBadRequest {
		t.Fatalf("admin save of CH-UI's own database: %d, want 400", rec.Code)
	}
	if rec := pipelineRequestAs(router, "admin", conn, http.MethodPut, "/"+id+"/graph", graph(outside)); rec.Code != http.StatusOK {
		t.Fatalf("admin save: %d %s", rec.Code, rec.Body.String())
	}
	if rec := pipelineRequestAs(router, "analyst", conn, http.MethodPost, "/"+id+"/start", ""); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst start: %d, want 403", rec.Code)
	}
}
