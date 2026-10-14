package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// roleScopeFixture is an agent connection ("live") whose ClickHouse reports
// every user as a ClickHouse admin, plus a second saved connection ("other")
// with no agent.
func roleScopeFixture(t *testing.T) (h *AuthHandler, db *database.DB, live, other string) {
	t.Helper()
	db, err := dbtest.Open(t, "role-scope.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	live, err = db.CreateConnection(database.CreateConnectionParams{Name: "live", TunnelToken: "role-scope-token"})
	if err != nil {
		t.Fatal(err)
	}
	other, err = db.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "role-scope-other"})
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "role-scope-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type == "test_connection" {
			ok := true
			return &tunnel.AgentMessage{Type: "test_result", Online: &ok, Version: "25.8"}
		}
		// system.users is readable: role detection says admin.
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"1":1}]`), Meta: json.RawMessage(`[{"name":"1","type":"UInt8"}]`)}
	})
	deadline := time.Now().Add(5 * time.Second)
	for !agent.Gateway.IsTunnelOnline(live) {
		if time.Now().After(deadline) {
			t.Fatal("agent never came online")
		}
		time.Sleep(10 * time.Millisecond)
	}
	h = &AuthHandler{DB: db, Gateway: agent.Gateway, RateLimiter: middleware.NewRateLimiter(db), Config: &config.Config{AppSecretKey: "test-secret"}}
	return h, db, live, other
}

func TestResolveUserRoleOverrideIsPerConnection(t *testing.T) {
	h, db, live, other := roleScopeFixture(t)
	if err := db.SetUserRole("default", other, "admin"); err != nil {
		t.Fatal(err)
	}

	// Same username, other connection: the admin override does not follow,
	// and an admin already exists, so ClickHouse admin alone is not enough.
	role, note := h.resolveUserRole(live, "default", "pw", "198.51.100.40")
	if role != "viewer" || note == "" {
		t.Fatalf("role = %q note = %q, want viewer with a note", role, note)
	}
	if r, _ := db.GetUserRole("default", live); r != "" {
		t.Fatalf("override written on live: %q", r)
	}

	// An override on this connection applies.
	if err := db.SetUserRole("default", live, "analyst"); err != nil {
		t.Fatal(err)
	}
	if role, _ := h.resolveUserRole(live, "default", "pw", "198.51.100.40"); role != "analyst" {
		t.Fatalf("role with override on live = %q, want analyst", role)
	}
}

func TestResolveUserRoleBootstrapsOnSavedConnectionOnly(t *testing.T) {
	h, db, live, other := roleScopeFixture(t)
	role, _ := h.resolveUserRole(live, "default", "pw", "198.51.100.41")
	if role != "admin" {
		t.Fatalf("first ClickHouse admin = %q, want admin", role)
	}
	if r, _ := db.GetUserRole("default", live); r != "admin" {
		t.Fatalf("bootstrap override on live = %q, want admin", r)
	}
	if r, _ := db.GetUserRole("default", other); r != "" {
		t.Fatalf("bootstrap leaked to other connection: %q", r)
	}
	if n, _ := db.CountUsersWithRole("admin"); n != 1 {
		t.Fatalf("admins = %d, want 1", n)
	}
}

func TestResolveUserRoleLoginURLConnectionNeverAdmin(t *testing.T) {
	h, db, live, _ := roleScopeFixture(t)
	if err := db.SetSetting(database.SettingLoginURLConnectionIDs, `["`+live+`"]`); err != nil {
		t.Fatal(err)
	}

	// No admin anywhere, ClickHouse says admin: still no bootstrap.
	role, note := h.resolveUserRole(live, "default", "pw", "198.51.100.42")
	if role != "analyst" || !strings.Contains(note, "login page") {
		t.Fatalf("role = %q note = %q, want analyst with a note", role, note)
	}
	if n, _ := db.CountUsersWithRole("admin"); n != 0 {
		t.Fatalf("admins = %d, want 0 (no bootstrap from a login page connection)", n)
	}

	// An admin override for the same name elsewhere never applies here.
	other, err := db.CreateConnection(database.CreateConnectionParams{Name: "saved", TunnelToken: "role-scope-saved"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("default", other, "admin"); err != nil {
		t.Fatal(err)
	}
	if role, _ := h.resolveUserRole(live, "default", "pw", "198.51.100.42"); role != "analyst" {
		t.Fatalf("role = %q, want analyst", role)
	}
}

func TestLoginRefusesReservedUsername(t *testing.T) {
	h, _ := loginURLFixture(t, false)
	for _, name := range []string{"sso:victim@corp.example", "SSO:Victim@corp.example"} {
		rec, out := postLogin(t, h, "198.51.100.43", map[string]string{"username": name, "password": "x"})
		if rec.Code != http.StatusBadRequest || out["error"] != errReservedUsername {
			t.Fatalf("%s: got %d %v, want 400 %q", name, rec.Code, out, errReservedUsername)
		}
	}
}

func TestAdminRoleEndpointsArePerConnection(t *testing.T) {
	db, err := dbtest.Open(t, "role-endpoints.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mk := func(name string) string {
		id, err := db.CreateConnection(database.CreateConnectionParams{Name: name, TunnelToken: "tok-" + name})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	a, b, l := mk("a"), mk("b"), mk("l")
	if err := db.SetSetting(database.SettingLoginURLConnectionIDs, `["`+l+`"]`); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("root", a, "admin"); err != nil {
		t.Fatal(err)
	}
	h := &AdminHandler{DB: db}
	admin := &middleware.SessionInfo{ConnectionID: a, ClickhouseUser: "root", UserRole: "admin"}

	call := func(method, username, query, body string) *httptest.ResponseRecorder {
		t.Helper()
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", username)
		req := httptest.NewRequest(method, "/user-roles/"+username+query, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), admin))
		rec := httptest.NewRecorder()
		if method == http.MethodPut {
			h.SetUserRole(rec, req)
		} else {
			h.DeleteUserRole(rec, req)
		}
		return rec
	}
	role := func(key, conn string) string {
		t.Helper()
		r, err := db.GetUserRole(key, conn)
		if err != nil {
			t.Fatal(err)
		}
		return r
	}

	// Defaults to the admin's connection.
	if rec := call(http.MethodPut, "bob", "", `{"role":"analyst"}`); rec.Code != http.StatusOK {
		t.Fatalf("set on current: %d %s", rec.Code, rec.Body.String())
	}
	if role("bob", a) != "analyst" || role("bob", b) != "" {
		t.Fatalf("bob: a=%q b=%q, want analyst on a only", role("bob", a), role("bob", b))
	}
	// An explicit connection.
	if rec := call(http.MethodPut, "bob", "", `{"role":"admin","connection_id":"`+b+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("set on b: %d %s", rec.Code, rec.Body.String())
	}
	if role("bob", b) != "admin" || role("bob", a) != "analyst" {
		t.Fatalf("bob: a=%q b=%q", role("bob", a), role("bob", b))
	}
	// Login page connections take no override.
	if rec := call(http.MethodPut, "bob", "", `{"role":"viewer","connection_id":"`+l+`"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("set on login page connection: %d, want 400", rec.Code)
	}
	// SSO overrides are global whatever connection is sent.
	if rec := call(http.MethodPut, "sso:carol@example.com", "", `{"role":"analyst","connection_id":"`+b+`"}`); rec.Code != http.StatusOK {
		t.Fatalf("set sso: %d %s", rec.Code, rec.Body.String())
	}
	if role("sso:carol@example.com", l) != "analyst" {
		t.Fatal("SSO override is not global")
	}

	// Delete only touches the named connection.
	if rec := call(http.MethodDelete, "bob", "?connection_id="+b, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete on b: %d %s", rec.Code, rec.Body.String())
	}
	if role("bob", b) != "" || role("bob", a) != "analyst" {
		t.Fatalf("after delete: a=%q b=%q", role("bob", a), role("bob", b))
	}

	// The last admin is still protected.
	if rec := call(http.MethodDelete, "root", "", ""); rec.Code != http.StatusBadRequest {
		t.Fatalf("delete last admin: %d, want 400", rec.Code)
	}

	rows := auditByDetails(t, db, "user_role.set")
	if _, ok := rows[`Set role for "bob" to admin on connection "b"`]; !ok {
		t.Fatalf("audit rows = %v", rows)
	}
	if _, ok := rows[`Set role for "sso:carol@example.com" to analyst on all connections`]; !ok {
		t.Fatalf("audit rows = %v", rows)
	}
}
