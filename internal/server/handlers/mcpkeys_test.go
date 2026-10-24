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

// allow_actions is accepted only with read_write and defaults to off.
func TestMCPKeyAllowActionsRequiresReadWrite(t *testing.T) {
	db, err := dbtest.Open(t, "mcpkeys-actions.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: "cht_22222222222222222222222222222222", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	h := &MCPKeysHandler{DB: db, Config: &config.Config{AppSecretKey: "mcp-keys-secret"}}
	r := chi.NewRouter()
	r.Route("/keys", h.Routes)
	create := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/keys/", strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ClickhouseUser: "admin", UserRole: "admin"}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	base := `"connection_id":"` + connID + `","ch_user":"u","ch_password":"pw"`

	if rec := create(`{"name":"a",` + base + `,"scopes":"read","allow_actions":true}`); rec.Code != http.StatusBadRequest {
		t.Errorf("read key with actions: want 400, got %d %s", rec.Code, rec.Body.String())
	}
	for _, c := range []struct {
		body string
		want bool
	}{
		{`{"name":"b",` + base + `,"scopes":"read_write"}`, false},
		{`{"name":"c",` + base + `,"scopes":"read_write","allow_actions":true}`, true},
	} {
		rec := create(c.body)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		var created struct {
			Key database.MCPKey `json:"key"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &created)
		if created.Key.AllowActions != c.want {
			t.Errorf("%s: allow_actions = %v, want %v", c.body, created.Key.AllowActions, c.want)
		}
	}
}

// An SSO admin with a per-person ClickHouse user can create a key that runs
// as them: the list says where, the row references the identity and holds no
// password, and the option is refused where they have no live user.
func TestMCPKeyRunsAsMe(t *testing.T) {
	db, err := dbtest.Open(t, "mcpkeys-as-me.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	mkConn := func(name, token string) string {
		t.Helper()
		id, err := db.CreateConnection(database.CreateConnectionParams{Name: name, TunnelToken: token, Type: database.ConnectionTypeTunnel})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	prod := mkConn("prod", "cht_33333333333333333333333333333333")
	other := mkConn("other", "cht_44444444444444444444444444444444")
	locked := mkConn("locked", "cht_55555555555555555555555555555555")
	ident, err := db.UpsertSSOIdentity("https://idp.example", "sub-ana", "ana@x.com", "Ana", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{prod, locked} {
		if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{IdentityID: ident.ID, ConnectionID: c, CHUser: "sso:ana@x.com", EncryptedPassword: "enc"}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetSSOClickHouseUserLocked(ident.ID, locked, true); err != nil {
		t.Fatal(err)
	}

	h := &MCPKeysHandler{DB: db, Config: &config.Config{AppSecretKey: "mcp-keys-secret"}}
	r := chi.NewRouter()
	r.Route("/keys", h.Routes)
	ana := &middleware.SessionInfo{ConnectionID: prod, ClickhouseUser: "sso:ana@x.com", AuthSubject: "ana@x.com", UserRole: "admin", SSOIdentityID: ident.ID}
	shared := &middleware.SessionInfo{ConnectionID: prod, ClickhouseUser: "svc", AuthSubject: "bob@x.com", UserRole: "admin"}
	call := func(sess *middleware.SessionInfo, method, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/keys/", strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), sess))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	var listed struct {
		AsMe map[string]string `json:"as_me"`
	}
	_ = json.Unmarshal(call(ana, http.MethodGet, "").Body.Bytes(), &listed)
	if len(listed.AsMe) != 1 || listed.AsMe[prod] != "sso:ana@x.com" {
		t.Fatalf("as_me = %v, want only prod (other has no user, locked is locked)", listed.AsMe)
	}
	listed.AsMe = nil
	_ = json.Unmarshal(call(shared, http.MethodGet, "").Body.Bytes(), &listed)
	if len(listed.AsMe) != 0 {
		t.Fatalf("shared-account session: as_me = %v, want empty", listed.AsMe)
	}

	rec := call(ana, http.MethodPost, `{"name":"nightly","connection_id":"`+prod+`","as_me":true,"ch_user":"ignored","ch_password":"ignored","scopes":"read_write"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create as me: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		Key database.MCPKey `json:"key"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	k, err := db.GetMCPKeyByID(created.Key.ID)
	if err != nil || k == nil {
		t.Fatalf("load key: %v", err)
	}
	if k.SSOIdentityID != ident.ID || k.CHPasswordEnc != "" || k.CHUser != "sso:ana@x.com" || k.Subject != "ana@x.com" || k.GrantRole != "admin" || k.Kind != "api" {
		t.Fatalf("stored key = %+v, want a reference to ana's user with no password", k)
	}

	for name, c := range map[string]struct {
		sess *middleware.SessionInfo
		conn string
	}{
		"no per-person user": {ana, other},
		"locked user":        {ana, locked},
		"shared account":     {shared, prod},
	} {
		rec := call(c.sess, http.MethodPost, `{"name":"x","connection_id":"`+c.conn+`","as_me":true}`)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
}
