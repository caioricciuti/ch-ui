package handlers

import (
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
	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func repointDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := dbtest.Open(t, "repoint.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func repointSession(t *testing.T, db *database.DB, connID, token string) {
	t.Helper()
	if _, err := db.CreateSession(database.CreateSessionParams{
		ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: "enc", Token: token,
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), UserRole: "analyst",
	}); err != nil {
		t.Fatal(err)
	}
}

// setCreatedAt pins a connection's creation time, so "oldest" is stable
// within one second.
func setCreatedAt(t *testing.T, db *database.DB, id, at string) {
	t.Helper()
	if _, err := db.Conn().Exec("UPDATE connections SET created_at = ? WHERE id = ?", at, id); err != nil {
		t.Fatal(err)
	}
}

// Pointing a direct connection at another ClickHouse server clears what
// belonged to the old one and says so; a new URL for the same server
// clears nothing.
func TestUpdateConnectionURLResetsOnlyOnServerChange(t *testing.T) {
	db := repointDB(t)
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: "repoint-token", Type: database.ConnectionTypeDirect, ClickHouseURL: "http://ch.example:8123"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(connID, "svc", "enc"); err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("alice", connID, "admin"); err != nil {
		t.Fatal(err)
	}
	repointSession(t, db, connID, "alice-sess")

	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	h := &ConnectionsHandler{DB: db, Gateway: gw, Config: &config.Config{}, Provisioner: &ssoidentity.Provisioner{DB: db, Gateway: gw}}
	r := chi.NewRouter()
	r.Put("/{id}", h.Update)
	put := func(url string) map[string]any {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/"+connID, strings.NewReader(`{"clickhouse_url":"`+url+`"}`))
		req.RemoteAddr = "198.51.100.50:5000"
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "admin", ClickhouseUser: "root"}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("update to %s: %d %s", url, rec.Code, rec.Body.String())
		}
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	// Same host and (default) port, written differently.
	out := put("http://CH.example/")
	if _, ok := out["reset"]; ok {
		t.Fatalf("same server reported a reset: %v", out)
	}
	if s, _ := db.GetSession("alice-sess"); s == nil {
		t.Fatal("same server change signed alice out")
	}
	if user, _, _ := db.GetConnectionSSOAccount(connID); user != "svc" {
		t.Fatalf("same server change cleared the SSO account: %q", user)
	}
	if conn, _ := db.GetConnectionByID(connID); conn.ClickHouseURL != "http://CH.example" {
		t.Fatalf("url = %q", conn.ClickHouseURL)
	}

	out = put("http://other.example:8123")
	reset, ok := out["reset"].(map[string]any)
	if !ok {
		t.Fatalf("different server: no reset in %v", out)
	}
	if reset["sessions"] != float64(1) || reset["role_overrides"] != float64(1) || reset["sso_account"] != true {
		t.Fatalf("reset = %v", reset)
	}
	if out["clickhouse_url"] != "http://other.example:8123" {
		t.Fatalf("response = %v", out)
	}
	if s, _ := db.GetSession("alice-sess"); s != nil {
		t.Fatal("session on the old server survived")
	}
	if role, _ := db.GetUserRole("alice", connID); role != "" {
		t.Fatalf("role override survived: %q", role)
	}
	if user, _, _ := db.GetConnectionSSOAccount(connID); user != "" {
		t.Fatalf("SSO account survived: %q", user)
	}
	a := findAudit(auditActions(t, db), "connection.reset", "1 session(s) signed out")
	if a == nil || deref(a.Username) != "root" || deref(a.ConnectionID) != connID || deref(a.IPAddress) != "198.51.100.50" {
		t.Fatalf("connection.reset audit = %+v", a)
	}
}

// Setup saving the same setup connection with another server resets it.
func TestSetupSaveResetsOnServerChange(t *testing.T) {
	h, db, _, _ := setupFixture(t)
	conn, err := h.saveConnection("Prod", "http://clickhouse:8123")
	if err != nil {
		t.Fatal(err)
	}
	repointSession(t, db, conn.ID, "early")
	if _, err := h.saveConnection("Prod", "http://clickhouse:8123/"); err != nil {
		t.Fatal(err)
	}
	if s, _ := db.GetSession("early"); s == nil {
		t.Fatal("same URL signed the session out")
	}
	if _, err := h.saveConnection("Prod", "http://elsewhere:8123"); err != nil {
		t.Fatal(err)
	}
	if s, _ := db.GetSession("early"); s != nil {
		t.Fatal("session on the old server survived")
	}
	if rows := auditRows(t, db, "connection.reset"); len(rows) != 1 || deref(rows[0].Username) != setupActor {
		t.Fatalf("connection.reset rows = %+v", rows)
	}
}

// hideEmbedded makes embeddedID the connection setup replaced, so the login
// picker hides it.
func hideEmbedded(t *testing.T, db *database.DB, setupID string) string {
	t.Helper()
	embeddedID, err := db.CreateConnection(database.CreateConnectionParams{
		Name: "Embedded", TunnelToken: "emb", IsEmbedded: true,
		Type: database.ConnectionTypeDirect, ClickHouseURL: "http://unreachable:8123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingSetupConnectionID, setupID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingSetupEmbeddedURL, "http://unreachable:8123"); err != nil {
		t.Fatal(err)
	}
	if hiddenLoginConnectionID(db) != embeddedID {
		t.Fatal("embedded connection not hidden")
	}
	return embeddedID
}

func TestOIDCConnectionIDDefault(t *testing.T) {
	db := repointDB(t)
	h := &AuthHandler{DB: db}
	if _, err := h.oidcConnectionID(oidc.Settings{}); err == nil {
		t.Fatal("no connections: want an error")
	}

	a, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"})
	if err != nil {
		t.Fatal(err)
	}
	embedded := hideEmbedded(t, db, a)
	setCreatedAt(t, db, embedded, "2026-01-01 00:00:00")
	setCreatedAt(t, db, a, "2026-01-02 00:00:00")
	setCreatedAt(t, db, b, "2026-01-03 00:00:00")

	get := func() string {
		t.Helper()
		id, err := h.oidcConnectionID(oidc.Settings{})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// No SSO account anywhere: the oldest visible connection, not the
	// hidden embedded one.
	if got := get(); got != a {
		t.Fatalf("got %s, want the oldest visible (a)", got)
	}
	// An SSO account on the hidden connection does not make it a target.
	if err := db.SetConnectionSSOAccount(embedded, "svc", "enc"); err != nil {
		t.Fatal(err)
	}
	if got := get(); got != a {
		t.Fatalf("got %s, want a: the hidden connection is skipped", got)
	}
	// The connection with SSO set up wins over an older one without.
	if err := db.SetConnectionSSOAccount(b, "svc", "enc"); err != nil {
		t.Fatal(err)
	}
	if got := get(); got != b {
		t.Fatalf("got %s, want b (has an SSO account)", got)
	}
	// A configured connection always wins.
	if id, err := h.oidcConnectionID(oidc.Settings{ConnectionID: a}); err != nil || id != a {
		t.Fatalf("configured: %s %v", id, err)
	}
}

// Without a connectionId, password login picks the only visible connection
// and refuses to guess between several.
func TestLoginWithoutConnectionID(t *testing.T) {
	db, gw, connID := credentialFixture(t)
	if err := db.SetUserRole("alice", connID, "analyst"); err != nil {
		t.Fatal(err)
	}
	h := &AuthHandler{DB: db, Gateway: gw, RateLimiter: middleware.NewRateLimiter(db), Config: &config.Config{AppSecretKey: "test-secret"}}

	// The hidden embedded connection is older but skipped.
	embedded := hideEmbedded(t, db, connID)
	setCreatedAt(t, db, embedded, "2026-01-01 00:00:00")
	setCreatedAt(t, db, connID, "2026-01-02 00:00:00")
	alice := map[string]string{"username": "alice", "password": "right"}
	rec, _ := postLogin(t, h, "203.0.113.70", alice)
	if rec.Code != http.StatusOK {
		t.Fatalf("one visible connection: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), connID) {
		t.Fatalf("signed in to the wrong connection: %s", rec.Body.String())
	}

	if _, err := db.CreateConnection(database.CreateConnectionParams{Name: "second", TunnelToken: "second"}); err != nil {
		t.Fatal(err)
	}
	rec, _ = postLogin(t, h, "203.0.113.70", alice)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "connectionId is required") {
		t.Fatalf("two visible connections: %d %s, want 400", rec.Code, rec.Body.String())
	}
	if rec, _ := postLogin(t, h, "203.0.113.70", map[string]string{"username": "alice", "password": "right", "connectionId": connID}); rec.Code != http.StatusOK {
		t.Fatalf("explicit connectionId: %d %s", rec.Code, rec.Body.String())
	}
}
