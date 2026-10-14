package middleware

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

// SSO people share one ClickHouse service account. A role override on that
// account name must not reach them; an override on one person must reach only
// that person, and their IdP-mapped role applies otherwise.
func TestSessionRoleIsPerSSOPerson(t *testing.T) {
	db, err := dbtest.Open(t, "session.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	connID, err := db.CreateConnection(database.CreateConnectionParams{
		Name:        "sso",
		TunnelToken: "cht_session_test",
		Type:        database.ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, p := range []database.CreateSessionParams{
		{ConnectionID: connID, ClickhouseUser: "svc_sso", EncryptedPassword: "enc", Token: "tok-alice", ExpiresAt: expires, UserRole: "viewer", AuthSubject: "alice@example.com"},
		{ConnectionID: connID, ClickhouseUser: "svc_sso", EncryptedPassword: "enc", Token: "tok-bob", ExpiresAt: expires, UserRole: "analyst", AuthSubject: "bob@example.com"},
		{ConnectionID: connID, ClickhouseUser: "svc_sso", EncryptedPassword: "enc", Token: "tok-pw", ExpiresAt: expires, UserRole: "viewer"},
	} {
		if _, err := db.CreateSession(p); err != nil {
			t.Fatalf("CreateSession %s: %v", p.Token, err)
		}
	}

	// The pre-v2.13.2 way to "set a role" for an SSO person, which used to
	// promote every SSO person on the connection.
	if err := db.SetUserRole("svc_sso", "admin"); err != nil {
		t.Fatalf("SetUserRole svc_sso: %v", err)
	}
	if err := db.SetUserRole("sso:alice@example.com", "analyst"); err != nil {
		t.Fatalf("SetUserRole alice: %v", err)
	}

	roleFor := func(token string) (string, string) {
		t.Helper()
		var got *SessionInfo
		h := Session(db, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got = GetSession(r)
		}))
		req := httptest.NewRequest(http.MethodGet, "/api/session", nil)
		req.AddCookie(&http.Cookie{Name: "chui_session", Value: token})
		h.ServeHTTP(httptest.NewRecorder(), req)
		if got == nil {
			t.Fatalf("%s: no session in context", token)
		}
		return got.UserRole, got.AuthSubject
	}

	if role, subject := roleFor("tok-alice"); role != "analyst" || subject != "alice@example.com" {
		t.Fatalf("alice: role %q subject %q, want her own override analyst", role, subject)
	}
	if role, _ := roleFor("tok-bob"); role != "analyst" {
		t.Fatalf("bob: role %q, want his IdP role analyst (not the service account admin)", role)
	}
	if role, _ := roleFor("tok-pw"); role != "admin" {
		t.Fatalf("password login as svc_sso: role %q, want admin", role)
	}
}

// A request naming a connection other than its session's is refused with
// 409 connection_changed; a request without the header passes.
func TestSessionConnectionGuard(t *testing.T) {
	db, err := dbtest.Open(t, "guard.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "guard", TunnelToken: "cht_guard_test", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	if _, err := db.CreateSession(database.CreateSessionParams{
		ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: "enc", Token: "tok-guard",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	serve := func(header string) *httptest.ResponseRecorder {
		h := Session(db, nil)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		}))
		req := httptest.NewRequest(http.MethodGet, "/api/query", nil)
		req.AddCookie(&http.Cookie{Name: "chui_session", Value: "tok-guard"})
		if header != "" {
			req.Header.Set(ConnectionHeader, header)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	if rec := serve(""); rec.Code != http.StatusNoContent {
		t.Fatalf("no header: %d, want 204", rec.Code)
	}
	if rec := serve(connID); rec.Code != http.StatusNoContent {
		t.Fatalf("matching header: %d, want 204", rec.Code)
	}
	rec := serve("some-other-connection")
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "connection_changed") {
		t.Fatalf("mismatched header: %d %s, want 409 connection_changed", rec.Code, rec.Body.String())
	}
}
