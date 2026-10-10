package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func loginURLFixture(t *testing.T, allow bool) (*AuthHandler, *database.DB) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "login-url.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	h := &AuthHandler{
		DB:          db,
		Gateway:     gw,
		RateLimiter: middleware.NewRateLimiter(db),
		Config:      &config.Config{AppSecretKey: "test-secret", AllowLoginURL: allow},
	}
	return h, db
}

func postLogin(t *testing.T, h *AuthHandler, ip string, body map[string]string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/login", strings.NewReader(string(raw)))
	req.RemoteAddr = ip + ":40000"
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return rec, out
}

// createFromLogin runs the same validate-then-find-or-create steps as Login.
func createFromLogin(t *testing.T, h *AuthHandler, raw string) (*database.Connection, bool, error) {
	t.Helper()
	chURL, err := validateSetupClickHouseURL(raw)
	if err != nil {
		t.Fatalf("validate %q: %v", raw, err)
	}
	return h.loginURLConnection(chURL, "alice", "198.51.100.20")
}

func storedLoginURLIDs(t *testing.T, db *database.DB) []string {
	t.Helper()
	ids, err := loginURLConnectionIDs(db)
	if err != nil {
		t.Fatal(err)
	}
	return ids
}

func TestAuthConfigReportsLoginURLAllowed(t *testing.T) {
	for _, allow := range []bool{false, true} {
		h, _ := loginURLFixture(t, allow)
		rec := httptest.NewRecorder()
		h.AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
		var out map[string]interface{}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		if out["login_url_allowed"] != allow {
			t.Fatalf("allow=%v: login_url_allowed = %v", allow, out["login_url_allowed"])
		}
	}
}

func TestLoginURLDisabledIs403(t *testing.T) {
	h, db := loginURLFixture(t, false)
	rec, out := postLogin(t, h, "198.51.100.1", map[string]string{"username": "alice", "password": "x", "clickhouse_url": "http://ch.example:8123"})
	if rec.Code != http.StatusForbidden || out["success"] != false || out["error"] != errLoginURLDisabled {
		t.Fatalf("got %d %v, want 403 %q", rec.Code, out, errLoginURLDisabled)
	}
	if len(setupConnections(t, db)) != 0 {
		t.Fatal("disabled login URL must not create a connection")
	}
}

func TestLoginURLWithConnectionIDIs400(t *testing.T) {
	h, _ := loginURLFixture(t, true)
	for _, field := range []string{"connectionId", "connection_id"} {
		rec, out := postLogin(t, h, "198.51.100.2", map[string]string{"username": "alice", field: "abc", "clickhouse_url": "http://ch.example:8123"})
		if rec.Code != http.StatusBadRequest || out["error"] != errLoginURLAndConn {
			t.Fatalf("%s: got %d %v, want 400 %q", field, rec.Code, out, errLoginURLAndConn)
		}
	}
}

func TestLoginURLBadURLIs400(t *testing.T) {
	h, db := loginURLFixture(t, true)
	cases := map[string]string{
		"ftp://ch.example:8123":          "clickhouse_url must be a valid http:// or https:// URL",
		"http://169.254.169.254":         "clickhouse_url must not point at a link-local or cloud metadata address",
		"http://user:pw@ch.example:8123": "clickhouse_url must not contain a username or password",
		"http://ch.example:8123/?a=1":    "clickhouse_url must not contain a query string",
	}
	for raw, want := range cases {
		rec, out := postLogin(t, h, "198.51.100.3", map[string]string{"username": "alice", "clickhouse_url": raw})
		if rec.Code != http.StatusBadRequest || out["error"] != want {
			t.Fatalf("%s: got %d %v, want 400 %q", raw, rec.Code, out, want)
		}
	}
	if len(setupConnections(t, db)) != 0 {
		t.Fatal("a rejected URL must not create a connection")
	}
}

func TestLoginURLIPRateLimitRunsFirst(t *testing.T) {
	h, db := loginURLFixture(t, true)
	for i := 0; i < MaxAttemptsPerIP; i++ {
		h.RateLimiter.RecordAttempt("ip:198.51.100.4", "ip")
	}
	rec, out := postLogin(t, h, "198.51.100.4", map[string]string{"username": "alice", "clickhouse_url": "http://ch.example:8123"})
	if rec.Code != http.StatusTooManyRequests || out["error"] != "Too many login attempts from this IP" {
		t.Fatalf("got %d %v, want the IP rate limit", rec.Code, out)
	}
	if len(setupConnections(t, db)) != 0 {
		t.Fatal("a rate limited request must not create a connection")
	}
}

func TestLoginURLCreatesThenReuses(t *testing.T) {
	h, db := loginURLFixture(t, true)

	conn, created, err := createFromLogin(t, h, "http://CH.Example:8123/")
	if err != nil || !created {
		t.Fatalf("first call: created=%v err=%v", created, err)
	}
	if conn.Type != database.ConnectionTypeDirect || conn.IsEmbedded {
		t.Fatalf("connection = %+v, want a non-embedded direct connection", conn)
	}
	if conn.ClickHouseURL != "http://ch.example:8123" || conn.Name != "ch.example:8123" {
		t.Fatalf("url %q name %q", conn.ClickHouseURL, conn.Name)
	}
	if ids := storedLoginURLIDs(t, db); len(ids) != 1 || ids[0] != conn.ID {
		t.Fatalf("recorded ids = %v, want [%s]", ids, conn.ID)
	}
	rows := auditRows(t, db, "connection.created_from_login")
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if deref(rows[0].Username) != "alice" || deref(rows[0].IPAddress) != "198.51.100.20" ||
		deref(rows[0].ConnectionID) != conn.ID || !strings.Contains(deref(rows[0].Details), "http://ch.example:8123") {
		t.Fatalf("audit row = %+v", rows[0])
	}

	again, created, err := createFromLogin(t, h, "HTTP://ch.example:8123")
	if err != nil || created || again.ID != conn.ID {
		t.Fatalf("second call: id=%v created=%v err=%v, want reuse of %s", again, created, err, conn.ID)
	}
	if n := len(setupConnections(t, db)); n != 1 {
		t.Fatalf("connections = %d, want 1", n)
	}
	if n := len(auditRows(t, db, "connection.created_from_login")); n != 1 {
		t.Fatalf("audit rows after reuse = %d, want 1", n)
	}
}

func TestLoginURLReusesAdminConnectionButNotEmbedded(t *testing.T) {
	h, db := loginURLFixture(t, true)
	if _, err := db.CreateConnection(database.CreateConnectionParams{Name: "Local", TunnelToken: "emb", IsEmbedded: true, Type: database.ConnectionTypeDirect, ClickHouseURL: "http://localhost:8123"}); err != nil {
		t.Fatal(err)
	}
	adminID, err := db.CreateConnection(database.CreateConnectionParams{Name: "Prod", TunnelToken: "prod", Type: database.ConnectionTypeDirect, ClickHouseURL: "http://prod.example:8123"})
	if err != nil {
		t.Fatal(err)
	}

	conn, created, err := createFromLogin(t, h, "http://prod.example:8123/")
	if err != nil || created || conn.ID != adminID {
		t.Fatalf("got %+v created=%v err=%v, want the admin connection", conn, created, err)
	}
	if ids := storedLoginURLIDs(t, db); len(ids) != 0 {
		t.Fatalf("reusing an admin connection must not record it, got %v", ids)
	}

	conn, created, err = createFromLogin(t, h, "http://localhost:8123")
	if err != nil || !created || conn.IsEmbedded {
		t.Fatalf("got %+v created=%v err=%v, want a new connection beside the embedded one", conn, created, err)
	}
}

func TestLoginURLCap(t *testing.T) {
	h, db := loginURLFixture(t, true)
	var first *database.Connection
	for i := 0; i < loginURLMaxConnections; i++ {
		conn, created, err := createFromLogin(t, h, fmt.Sprintf("http://ch-%d.example:8123", i))
		if err != nil || !created {
			t.Fatalf("create %d: created=%v err=%v", i, created, err)
		}
		if i == 0 {
			first = conn
		}
	}

	// A new URL is refused, by the helper and by Login.
	if _, _, err := createFromLogin(t, h, "http://new.example:8123"); !errors.Is(err, errLoginURLCapReached) {
		t.Fatalf("err = %v, want errLoginURLCapReached", err)
	}
	rec, out := postLogin(t, h, "198.51.100.5", map[string]string{"username": "alice", "clickhouse_url": "http://new.example:8123"})
	if rec.Code != http.StatusTooManyRequests || out["success"] != false || out["error"] != errLoginURLCapped {
		t.Fatalf("got %d %v, want 429 %q", rec.Code, out, errLoginURLCapped)
	}
	if entry, _ := db.GetRateLimit("ip:198.51.100.5"); entry != nil {
		t.Fatal("hitting the cap must not count a login attempt")
	}

	// An existing URL still resolves at the cap.
	conn, created, err := createFromLogin(t, h, "http://ch-0.example:8123/")
	if err != nil || created || conn.ID != first.ID {
		t.Fatalf("existing URL at cap: %+v created=%v err=%v", conn, created, err)
	}

	// Deleted ids stop counting, and are pruned on the next create.
	if err := db.DeleteConnection(first.ID); err != nil {
		t.Fatal(err)
	}
	conn, created, err = createFromLogin(t, h, "http://new.example:8123")
	if err != nil || !created {
		t.Fatalf("after delete: created=%v err=%v", created, err)
	}
	ids := storedLoginURLIDs(t, db)
	if len(ids) != loginURLMaxConnections {
		t.Fatalf("recorded ids = %d, want %d", len(ids), loginURLMaxConnections)
	}
	for _, id := range ids {
		if id == first.ID {
			t.Fatal("deleted id still recorded")
		}
	}
	if ids[len(ids)-1] != conn.ID {
		t.Fatalf("last recorded id = %s, want %s", ids[len(ids)-1], conn.ID)
	}
}

// TestLoginURLFullLoginAtCap signs in through Login with a URL that matches a
// saved connection while the cap is full, using a fake agent for the tunnel.
func TestLoginURLFullLoginAtCap(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "login-url-full.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	existingID, err := db.CreateConnection(database.CreateConnectionParams{Name: "Prod", TunnelToken: "login-url-token", Type: database.ConnectionTypeDirect, ClickHouseURL: "http://prod.example:8123"})
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "login-url-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type == "test_connection" {
			ok := true
			return &tunnel.AgentMessage{Type: "test_result", Online: &ok, Version: "25.8"}
		}
		// Role detection: no access to system tables means viewer.
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 497. ACCESS_DENIED"}
	})
	h := &AuthHandler{
		DB:          db,
		Gateway:     agent.Gateway,
		RateLimiter: middleware.NewRateLimiter(db),
		Config:      &config.Config{AppSecretKey: "test-secret", AllowLoginURL: true},
	}
	for i := 0; i < loginURLMaxConnections; i++ {
		if _, _, err := createFromLogin(t, h, fmt.Sprintf("http://ch-%d.example:8123", i)); err != nil {
			t.Fatal(err)
		}
	}

	rec, out := postLogin(t, h, "198.51.100.6", map[string]string{"username": "bob", "password": "pw", "clickhouse_url": "HTTP://Prod.Example:8123/"})
	if rec.Code != http.StatusOK || out["success"] != true {
		t.Fatalf("got %d %v, want 200", rec.Code, out)
	}
	connObj, _ := out["connection"].(map[string]interface{})
	if connObj["id"] != existingID || connObj["name"] != "Prod" {
		t.Fatalf("connection = %v, want %s Prod", connObj, existingID)
	}
	if out["user_role"] != "viewer" {
		t.Fatalf("user_role = %v, want viewer", out["user_role"])
	}
	var cookie *http.Cookie
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("no session cookie")
	}
	session, err := db.GetSession(cookie.Value)
	if err != nil || session == nil || session.ConnectionID != existingID || session.ClickhouseUser != "bob" {
		t.Fatalf("session = %+v, err %v", session, err)
	}
	if n := len(auditRows(t, db, "user.login")); n != 1 {
		t.Fatalf("user.login rows = %d, want 1", n)
	}
}
