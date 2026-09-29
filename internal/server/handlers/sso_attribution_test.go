package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// SSO people share one ClickHouse service account. Audit rows must name the
// person (middleware.Actor) and record the shared account as ch_user; a
// password user's row keeps ch_user NULL because the two would be equal.

// ssoServiceAccount is declared in sso_per_person_test.go.

func attributionFixture(t *testing.T) (*database.DB, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "attribution.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "attr", TunnelToken: "attr"})
	if err != nil {
		t.Fatal(err)
	}
	return db, conn
}

func attributionSessions(conn, role string) (alice, bob, pw *middleware.SessionInfo) {
	alice = &middleware.SessionInfo{ConnectionID: conn, UserRole: role, ClickhouseUser: ssoServiceAccount, AuthSubject: "alice@example.com"}
	bob = &middleware.SessionInfo{ConnectionID: conn, UserRole: role, ClickhouseUser: ssoServiceAccount, AuthSubject: "bob@example.com"}
	pw = &middleware.SessionInfo{ConnectionID: conn, UserRole: role, ClickhouseUser: "carol_ch"}
	return alice, bob, pw
}

// auditByDetails indexes rows of one action by their details text.
func auditByDetails(t *testing.T, db *database.DB, action string) map[string]database.AuditLog {
	t.Helper()
	logs, err := db.GetAuditLogsFiltered(100, "", action, "", "")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]database.AuditLog{}
	for _, l := range logs {
		if l.Details != nil {
			out[*l.Details] = l
		}
	}
	return out
}

func assertAuditRow(t *testing.T, row database.AuditLog, ok bool, wantUser string, wantCHUser *string) {
	t.Helper()
	if !ok {
		t.Fatalf("audit row for %q missing", wantUser)
	}
	if row.Username == nil || *row.Username != wantUser {
		t.Fatalf("username = %v, want %q", deref(row.Username), wantUser)
	}
	switch {
	case wantCHUser == nil && row.ChUser != nil:
		t.Fatalf("%s: ch_user = %q, want NULL", wantUser, *row.ChUser)
	case wantCHUser != nil && (row.ChUser == nil || *row.ChUser != *wantCHUser):
		t.Fatalf("%s: ch_user = %v, want %q", wantUser, deref(row.ChUser), *wantCHUser)
	}
}

func TestSavedQueryCreateAuditNamesSSOPerson(t *testing.T) {
	db, conn := attributionFixture(t)
	h := &SavedQueriesHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/saved", h.Routes)

	alice, bob, pw := attributionSessions(conn, "analyst")
	for name, s := range map[string]*middleware.SessionInfo{"q-alice": alice, "q-bob": bob, "q-carol": pw} {
		req := httptest.NewRequest(http.MethodPost, "/saved/", strings.NewReader(`{"name":"`+name+`","query":"SELECT 1"}`))
		req = req.WithContext(middleware.SetSession(req.Context(), s))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		if rr.Code != http.StatusCreated {
			t.Fatalf("%s: status %d: %s", name, rr.Code, rr.Body.String())
		}
	}

	rows := auditByDetails(t, db, "saved_query.created")
	sa := ssoServiceAccount
	row, ok := rows["q-alice"]
	assertAuditRow(t, row, ok, "alice@example.com", &sa)
	row, ok = rows["q-bob"]
	assertAuditRow(t, row, ok, "bob@example.com", &sa)
	row, ok = rows["q-carol"]
	assertAuditRow(t, row, ok, "carol_ch", nil)

	queries, err := db.GetSavedQueries()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{"q-alice": "alice@example.com", "q-bob": "bob@example.com", "q-carol": "carol_ch"}
	for _, q := range queries {
		if q.CreatedBy == nil || *q.CreatedBy != want[q.Name] {
			t.Fatalf("%s: created_by = %v, want %q", q.Name, deref(q.CreatedBy), want[q.Name])
		}
	}
	if len(queries) != 3 {
		t.Fatalf("got %d saved queries, want 3", len(queries))
	}
}

func TestSetUserRoleAuditNamesSSOAdmin(t *testing.T) {
	db, conn := attributionFixture(t)
	h := &AdminHandler{DB: db}
	alice, bob, pw := attributionSessions(conn, "admin")

	for target, s := range map[string]*middleware.SessionInfo{"target_a": alice, "target_b": bob, "target_c": pw} {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", target)
		req := httptest.NewRequest(http.MethodPut, "/user-roles/"+target, strings.NewReader(`{"role":"analyst"}`))
		ctx := context.WithValue(req.Context(), chi.RouteCtxKey, rctx)
		req = req.WithContext(middleware.SetSession(ctx, s))
		rr := httptest.NewRecorder()
		h.SetUserRole(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d: %s", target, rr.Code, rr.Body.String())
		}
	}

	rows := auditByDetails(t, db, "user_role.set")
	sa := ssoServiceAccount
	row, ok := rows[`Set role for "target_a" to analyst`]
	assertAuditRow(t, row, ok, "alice@example.com", &sa)
	row, ok = rows[`Set role for "target_b" to analyst`]
	assertAuditRow(t, row, ok, "bob@example.com", &sa)
	row, ok = rows[`Set role for "target_c" to analyst`]
	assertAuditRow(t, row, ok, "carol_ch", nil)
}

// Logout reads the stored session (AuthSubject is a pointer there), not the
// middleware SessionInfo, so it gets its own check.
func TestLogoutAuditNamesSSOPerson(t *testing.T) {
	db, conn := attributionFixture(t)
	h := &AuthHandler{DB: db, Config: &config.Config{}}
	expires := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)

	for token, subject := range map[string]string{"tok-alice": "alice@example.com", "tok-bob": "bob@example.com"} {
		if _, err := db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: ssoServiceAccount, Token: token, ExpiresAt: expires, AuthSubject: subject}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "carol_ch", Token: "tok-carol", ExpiresAt: expires}); err != nil {
		t.Fatal(err)
	}

	for _, token := range []string{"tok-alice", "tok-bob", "tok-carol"} {
		req := httptest.NewRequest(http.MethodPost, "/logout", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: token})
		rr := httptest.NewRecorder()
		h.Logout(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: status %d", token, rr.Code)
		}
	}

	logs, err := db.GetAuditLogsFiltered(100, "", "user.logout", "", "")
	if err != nil {
		t.Fatal(err)
	}
	byUser := map[string]database.AuditLog{}
	for _, l := range logs {
		byUser[deref(l.Username)] = l
	}
	if len(byUser) != 3 {
		t.Fatalf("got %d distinct logout actors, want 3: %v", len(byUser), byUser)
	}
	sa := ssoServiceAccount
	row, ok := byUser["alice@example.com"]
	assertAuditRow(t, row, ok, "alice@example.com", &sa)
	row, ok = byUser["bob@example.com"]
	assertAuditRow(t, row, ok, "bob@example.com", &sa)
	row, ok = byUser["carol_ch"]
	assertAuditRow(t, row, ok, "carol_ch", nil)
}
