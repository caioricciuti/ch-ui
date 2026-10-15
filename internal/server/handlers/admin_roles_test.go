package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// The UI percent-encodes role keys, and SSO keys always hold ':' and '@'.
func TestRoleUsernameParamDecodes(t *testing.T) {
	cases := map[string]string{
		"sso%3Abob%40example.com": "sso:bob@example.com",
		"analyst":                 "analyst",
		"ana%40corp.com":          "ana@corp.com",
	}
	for raw, want := range cases {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", raw)
		req := httptest.NewRequest(http.MethodPut, "/user-roles/x", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		got, ok := roleUsernameParam(req)
		if !ok || got != want {
			t.Fatalf("%q: got %q ok=%v, want %q", raw, got, ok, want)
		}
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("username", "%zz")
	req := httptest.NewRequest(http.MethodPut, "/user-roles/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if _, ok := roleUsernameParam(req); ok {
		t.Fatal("malformed escape must be rejected")
	}
}

// Removing an override never leaves sessions on a guessed role: the
// person's sessions it reached are signed out so sign-in works the role out
// again, and their OAuth tokens stop leaning on the issue-time role.
func TestDeleteUserRoleSignsOutAffectedSessions(t *testing.T) {
	db, err := dbtest.Open(t, "role-delete.db")
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
	a, b := mk("a"), mk("b")
	exp := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, p := range []database.CreateSessionParams{
		{ConnectionID: a, ClickhouseUser: "bob", Token: "bob-a", UserRole: "analyst"},
		{ConnectionID: b, ClickhouseUser: "bob", Token: "bob-b", UserRole: "analyst"},
		{ConnectionID: a, ClickhouseUser: "svc", Token: "carol-a", UserRole: "admin", AuthSubject: "carol@example.com"},
		{ConnectionID: a, ClickhouseUser: "svc", Token: "dave-a", UserRole: "analyst", AuthSubject: "dave@example.com"},
	} {
		p.EncryptedPassword, p.ExpiresAt = "enc", exp
		if _, err := db.CreateSession(p); err != nil {
			t.Fatal(err)
		}
	}
	for _, o := range []struct{ key, conn, role string }{
		{"root", a, "admin"}, {"bob", a, "analyst"}, {"sso:carol@example.com", "", "admin"},
	} {
		if err := db.SetUserRole(o.key, o.conn, o.role); err != nil {
			t.Fatal(err)
		}
	}
	carolKey, err := db.CreateMCPKey(database.CreateMCPKeyParams{
		Name: "c", KeyHash: "h-carol", KeyPrefix: "chm_c", ConnectionID: a, CHUser: "svc", CHPasswordEnc: "x",
		Scopes: "read_write", Kind: "oauth", Subject: "carol@example.com", GrantRole: "admin",
	})
	if err != nil {
		t.Fatal(err)
	}

	h := &AdminHandler{DB: db}
	admin := &middleware.SessionInfo{ConnectionID: a, ClickhouseUser: "root", UserRole: "admin"}
	del := func(username, query string) {
		t.Helper()
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", username)
		req := httptest.NewRequest(http.MethodDelete, "/user-roles/"+username+query, nil)
		req = req.WithContext(middleware.SetSession(context.WithValue(req.Context(), chi.RouteCtxKey, rctx), admin))
		rec := httptest.NewRecorder()
		h.DeleteUserRole(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("delete %s: %d %s", username, rec.Code, rec.Body.String())
		}
	}
	alive := func(token string) bool {
		t.Helper()
		s, err := db.GetSession(token)
		if err != nil {
			t.Fatal(err)
		}
		return s != nil
	}

	del("bob", "?connection_id="+a)
	if alive("bob-a") || !alive("bob-b") {
		t.Fatalf("bob: session on a alive=%v (want signed out), on b alive=%v (want kept)", alive("bob-a"), alive("bob-b"))
	}

	del("sso:carol@example.com", "")
	if alive("carol-a") || !alive("dave-a") {
		t.Fatalf("carol signed out=%v, dave kept=%v", !alive("carol-a"), alive("dave-a"))
	}
	k, err := db.GetMCPKeyByID(carolKey.ID)
	if err != nil || k.GrantRole != "" {
		t.Fatalf("carol's OAuth token keeps grant role %q (%v)", k.GrantRole, err)
	}
	if role, _ := db.OAuthKeyRole(k); role != "viewer" {
		t.Fatalf("carol's OAuth token role with no session and no override = %q, want viewer", role)
	}
}
