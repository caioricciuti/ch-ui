package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// Creating, rotating and revoking an MCP key are audited and attributed to
// the person (SSO email), not the shared ClickHouse service account.
func TestMCPKeyLifecycleIsAuditedByActor(t *testing.T) {
	db, err := dbtest.Open(t, "mcpkeys.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: "cht_11111111111111111111111111111111", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	h := &MCPKeysHandler{DB: db, Config: &config.Config{AppSecretKey: "mcp-keys-secret"}}
	r := chi.NewRouter()
	r.Route("/keys", h.Routes)
	session := &middleware.SessionInfo{ClickhouseUser: "svc_account", AuthSubject: "bob@example.com", UserRole: "admin"}
	call := func(method, target, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), session))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	rec := call(http.MethodPost, "/keys/", `{"name":"ci","connection_id":"`+connID+`","ch_user":"reader","ch_password":"pw","allowed_databases":"analytics"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Key database.MCPKey `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.Key.CreatedBy != "bob@example.com" {
		t.Errorf("created_by = %q, want the person, not the ClickHouse account", created.Key.CreatedBy)
	}

	rec = call(http.MethodPost, "/keys/"+created.Key.ID+"/rotate", "")
	if rec.Code != http.StatusCreated {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var rotated struct {
		Key database.MCPKey `json:"key"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rotated)

	rec = call(http.MethodDelete, "/keys/"+rotated.Key.ID, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}

	logs, err := db.GetAuditLogs(50)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, l := range logs {
		if !strings.HasPrefix(l.Action, "mcp.key.") {
			continue
		}
		seen[l.Action] = true
		if l.Username == nil || *l.Username != "bob@example.com" {
			t.Errorf("%s: username = %v, want bob@example.com", l.Action, l.Username)
		}
		if l.Details == nil || strings.Contains(*l.Details, "=pw") {
			t.Errorf("%s: details missing or leaking the password: %v", l.Action, l.Details)
		}
	}
	for _, a := range []string{"mcp.key.created", "mcp.key.rotated", "mcp.key.revoked"} {
		if !seen[a] {
			t.Errorf("missing audit entry %s", a)
		}
	}
}
