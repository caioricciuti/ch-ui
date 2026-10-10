package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// setupFixture returns a handler with a live code, a fixed clock the test can
// move, and the code in display form.
func setupFixture(t *testing.T) (*SetupHandler, *database.DB, *time.Time, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "setup.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	now := time.Date(2026, 10, 10, 12, 0, 0, 0, time.UTC)
	h := &SetupHandler{DB: db, RateLimiter: middleware.NewRateLimiter(db), Now: func() time.Time { return now }}
	code, err := h.issueCode()
	if err != nil {
		t.Fatal(err)
	}
	return h, db, &now, code
}

func postSetup(t *testing.T, h *SetupHandler, ip string, body map[string]string) (*httptest.ResponseRecorder, map[string]interface{}) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/auth/setup", strings.NewReader(string(raw)))
	req.RemoteAddr = ip + ":40000"
	rec := httptest.NewRecorder()
	h.Setup(rec, req)
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
	return rec, out
}

func auditRows(t *testing.T, db *database.DB, action string) []database.AuditLog {
	t.Helper()
	logs, err := db.GetAuditLogsFiltered(100, "", action, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return logs
}

func setupConnections(t *testing.T, db *database.DB) []database.Connection {
	t.Helper()
	conns, err := db.GetConnections()
	if err != nil {
		t.Fatal(err)
	}
	return conns
}

func TestSetupCodeFormat(t *testing.T) {
	code, err := generateSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 14 || code[4] != '-' || code[9] != '-' {
		t.Fatalf("code %q is not XXXX-XXXX-XXXX", code)
	}
	for _, r := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(crockfordAlphabet, r) {
			t.Fatalf("code %q has non-Crockford rune %q", code, r)
		}
	}
}

func TestSetupWrongCode(t *testing.T) {
	h, db, _, code := setupFixture(t)
	wrong := "0000-0000-0000"
	if wrong == code {
		wrong = "1111-1111-1111"
	}
	rec, out := postSetup(t, h, "198.51.100.7", map[string]string{"code": wrong, "name": "ch", "clickhouse_url": "http://localhost:8123"})
	if rec.Code != http.StatusUnauthorized || out["error"] != "Invalid setup code" || out["success"] != false {
		t.Fatalf("got %d %v, want 401 Invalid setup code", rec.Code, out)
	}
	entry, err := db.GetRateLimit("setup:ip:198.51.100.7")
	if err != nil || entry == nil || entry.Attempts != 1 {
		t.Fatalf("rate limit entry = %+v, err %v; want 1 attempt", entry, err)
	}
	rows := auditRows(t, db, "setup.code_rejected")
	if len(rows) != 1 || rows[0].IPAddress == nil || *rows[0].IPAddress != "198.51.100.7" {
		t.Fatalf("code_rejected rows = %+v, want one with the client IP", rows)
	}
	if strings.Contains(deref(rows[0].Details), code) || strings.Contains(deref(rows[0].Details), wrong) {
		t.Fatal("audit details must not contain a code")
	}
	if len(setupConnections(t, db)) != 0 {
		t.Fatal("wrong code must not create a connection")
	}
}

func TestSetupMissingCodeIs400(t *testing.T) {
	h, db, _, _ := setupFixture(t)
	rec, _ := postSetup(t, h, "198.51.100.8", map[string]string{"name": "ch", "clickhouse_url": "http://localhost:8123"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rec.Code)
	}
	if entry, _ := db.GetRateLimit("setup:ip:198.51.100.8"); entry != nil {
		t.Fatal("a missing code is not counted as a bad code")
	}
}

func TestSetupCreatesThenUpdatesSameConnection(t *testing.T) {
	h, db, _, code := setupFixture(t)

	// Lower case and no dashes are accepted.
	loose := strings.ToLower(strings.ReplaceAll(code, "-", ""))
	rec, out := postSetup(t, h, "198.51.100.9", map[string]string{"code": loose, "name": "Prod", "clickhouse_url": "http://clickhouse:8123/"})
	if rec.Code != http.StatusOK || out["success"] != true {
		t.Fatalf("first save: got %d %v", rec.Code, out)
	}
	connObj, _ := out["connection"].(map[string]interface{})
	id, _ := connObj["id"].(string)
	if id == "" || connObj["name"] != "Prod" {
		t.Fatalf("connection = %v", connObj)
	}
	if stored, _ := db.GetSetting(settingSetupConnectionID); stored != id {
		t.Fatalf("setup_connection_id = %q, want %q", stored, id)
	}
	conn, err := db.GetConnectionByID(id)
	if err != nil || conn == nil {
		t.Fatalf("get connection: %v", err)
	}
	if conn.Type != database.ConnectionTypeDirect || conn.IsEmbedded || conn.ClickHouseURL != "http://clickhouse:8123" {
		t.Fatalf("connection = %+v, want direct, not embedded, trailing slash trimmed", conn)
	}

	rec, out = postSetup(t, h, "198.51.100.9", map[string]string{"code": code, "name": "Prod 2", "clickhouse_url": "https://ch.internal:8443"})
	if rec.Code != http.StatusOK {
		t.Fatalf("second save: got %d %v", rec.Code, out)
	}
	connObj, _ = out["connection"].(map[string]interface{})
	if connObj["id"] != id || connObj["name"] != "Prod 2" {
		t.Fatalf("second save connection = %v, want same id %s renamed", connObj, id)
	}
	conns := setupConnections(t, db)
	if len(conns) != 1 {
		t.Fatalf("got %d connections, want 1", len(conns))
	}
	if conns[0].ClickHouseURL != "https://ch.internal:8443" || conns[0].Name != "Prod 2" {
		t.Fatalf("connection not updated: %+v", conns[0])
	}

	saved := auditRows(t, db, "setup.connection_saved")
	if len(saved) != 2 {
		t.Fatalf("connection_saved rows = %d, want 2", len(saved))
	}
	for _, row := range saved {
		if deref(row.Username) != "setup" || deref(row.IPAddress) != "198.51.100.9" || deref(row.ConnectionID) != id {
			t.Fatalf("connection_saved row = %+v", row)
		}
	}
	if !strings.Contains(deref(saved[0].Details)+deref(saved[1].Details), "https://ch.internal:8443") {
		t.Fatal("connection_saved details should carry the URL")
	}

	// Deleted by hand: the next save creates a fresh one.
	if err := db.DeleteConnection(id); err != nil {
		t.Fatal(err)
	}
	rec, out = postSetup(t, h, "198.51.100.9", map[string]string{"code": code, "name": "Again", "clickhouse_url": "http://localhost:8123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("save after delete: got %d %v", rec.Code, out)
	}
	connObj, _ = out["connection"].(map[string]interface{})
	newID, _ := connObj["id"].(string)
	if newID == "" || newID == id {
		t.Fatalf("save after delete returned id %q, want a new one", newID)
	}
	if stored, _ := db.GetSetting(settingSetupConnectionID); stored != newID {
		t.Fatalf("setup_connection_id = %q, want %q", stored, newID)
	}
}

func TestSetupNeverTouchesEmbeddedConnection(t *testing.T) {
	h, db, _, code := setupFixture(t)
	embeddedID, err := db.CreateConnection(database.CreateConnectionParams{
		Name: "Embedded", TunnelToken: "emb", IsEmbedded: true,
		Type: database.ConnectionTypeDirect, ClickHouseURL: "http://unreachable:8123",
	})
	if err != nil {
		t.Fatal(err)
	}
	// Even if the stored id points at the embedded connection, it is not edited.
	if err := db.SetSetting(settingSetupConnectionID, embeddedID); err != nil {
		t.Fatal(err)
	}
	rec, out := postSetup(t, h, "198.51.100.10", map[string]string{"code": code, "name": "Mine", "clickhouse_url": "http://localhost:8123"})
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %v", rec.Code, out)
	}
	emb, _ := db.GetConnectionByID(embeddedID)
	if emb == nil || emb.ClickHouseURL != "http://unreachable:8123" || emb.Name != "Embedded" {
		t.Fatalf("embedded connection changed: %+v", emb)
	}
	if len(setupConnections(t, db)) != 2 {
		t.Fatal("setup should have created its own connection")
	}
}

func TestSetupClosed(t *testing.T) {
	t.Run("flag set", func(t *testing.T) {
		h, db, _, code := setupFixture(t)
		if err := db.SetSetting(settingSetupClosedAt, "2026-10-10T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		rec, out := postSetup(t, h, "198.51.100.11", map[string]string{"code": code, "name": "x", "clickhouse_url": "http://localhost:8123"})
		if rec.Code != http.StatusNotFound || out["error"] != "Setup is closed" {
			t.Fatalf("got %d %v, want 404 Setup is closed", rec.Code, out)
		}
	})
	t.Run("admin exists", func(t *testing.T) {
		h, db, _, code := setupFixture(t)
		if err := db.SetUserRole("bob", "admin"); err != nil {
			t.Fatal(err)
		}
		rec, out := postSetup(t, h, "198.51.100.12", map[string]string{"code": code, "name": "x", "clickhouse_url": "http://localhost:8123"})
		if rec.Code != http.StatusNotFound || out["error"] != "Setup is closed" {
			t.Fatalf("got %d %v, want 404 Setup is closed", rec.Code, out)
		}
	})
}

func TestSetupExpired(t *testing.T) {
	h, _, now, code := setupFixture(t)
	*now = now.Add(setupCodeTTL + time.Second)
	rec, out := postSetup(t, h, "198.51.100.13", map[string]string{"code": code, "name": "x", "clickhouse_url": "http://localhost:8123"})
	if rec.Code != http.StatusGone || out["error"] != "Setup code expired. Restart CH-UI to get a new one." {
		t.Fatalf("got %d %v, want 410", rec.Code, out)
	}
}

func TestSetupBurnsAfterTenBadCodes(t *testing.T) {
	h, db, _, code := setupFixture(t)
	wrong := "0000-0000-0000"
	if wrong == code {
		wrong = "1111-1111-1111"
	}
	// Distinct IPs so the per-IP limit (5) does not stop this first.
	for i := 0; i < setupMaxBadCodes; i++ {
		rec, _ := postSetup(t, h, fmt.Sprintf("203.0.113.%d", i+1), map[string]string{"code": wrong, "name": "x", "clickhouse_url": "http://localhost:8123"})
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("bad code %d: got %d, want 401", i+1, rec.Code)
		}
		if i == setupMaxBadCodes-2 && len(auditRows(t, db, "setup.code_burned")) != 0 {
			t.Fatal("code burned too early")
		}
	}
	if n := len(auditRows(t, db, "setup.code_burned")); n != 1 {
		t.Fatalf("code_burned rows = %d, want 1", n)
	}
	rec, _ := postSetup(t, h, "203.0.113.200", map[string]string{"code": code, "name": "x", "clickhouse_url": "http://localhost:8123"})
	if rec.Code != http.StatusGone {
		t.Fatalf("right code after burn: got %d, want 410", rec.Code)
	}
	if h.Open() {
		t.Fatal("setup_open must be false after the code is burned")
	}
}

func TestSetupPerIPRateLimit(t *testing.T) {
	h, _, _, code := setupFixture(t)
	wrong := "0000-0000-0000"
	if wrong == code {
		wrong = "1111-1111-1111"
	}
	for i := 0; i < setupMaxAttemptsPerIP; i++ {
		if rec, _ := postSetup(t, h, "198.51.100.14", map[string]string{"code": wrong}); rec.Code != http.StatusUnauthorized {
			t.Fatalf("attempt %d: got %d, want 401", i+1, rec.Code)
		}
	}
	rec, out := postSetup(t, h, "198.51.100.14", map[string]string{"code": code, "name": "x", "clickhouse_url": "http://localhost:8123"})
	if rec.Code != http.StatusTooManyRequests || out["success"] != false || out["retryAfter"] == nil {
		t.Fatalf("got %d %v, want 429 with retryAfter", rec.Code, out)
	}
}

func TestSetupRejectsBadURLAndName(t *testing.T) {
	h, db, _, code := setupFixture(t)
	for _, body := range []map[string]string{
		{"code": code, "name": "x", "clickhouse_url": "http://169.254.169.254:8123"},
		{"code": code, "name": "", "clickhouse_url": "http://localhost:8123"},
		{"code": code, "name": strings.Repeat("n", setupMaxNameLength+1), "clickhouse_url": "http://localhost:8123"},
	} {
		rec, out := postSetup(t, h, "198.51.100.15", body)
		if rec.Code != http.StatusBadRequest || out["error"] == "" {
			t.Fatalf("%v: got %d %v, want 400", body, rec.Code, out)
		}
	}
	if len(setupConnections(t, db)) != 0 {
		t.Fatal("rejected requests must not create connections")
	}
}

func TestValidateSetupClickHouseURL(t *testing.T) {
	rejected := []string{
		"",
		"ftp://ch:21",
		"http://u:p@h:8123",
		"http://u@h:8123",
		"http://169.254.169.254:8123",
		"http://169.254.1.1",
		"http://[fe80::1]:8123",
		"http://[::ffff:169.254.169.254]:8123",
		"http://[fd00:ec2::254]",
		"http://metadata.google.internal",
		"http://METADATA.google.internal.:80",
		"http://h:8123/?query",
		"http://h:8123/?",
		"http://h:8123/#frag",
		"http://2852039166:8123",
		"http://0xA9FEA9FE",
		"http://:8123",
	}
	for _, raw := range rejected {
		if got, err := validateSetupClickHouseURL(raw); err == nil {
			t.Errorf("%q accepted as %q, want rejected", raw, got)
		}
	}
	accepted := map[string]string{
		"http://localhost:8123":       "http://localhost:8123",
		"http://127.0.0.1:8123/":      "http://127.0.0.1:8123",
		"http://10.0.0.5:8123":        "http://10.0.0.5:8123",
		"http://192.168.1.20:8123":    "http://192.168.1.20:8123",
		"https://ch.example.com":      "https://ch.example.com",
		"http://[::1]:8123":           "http://[::1]:8123",
		"http://clickhouse:8123/path": "http://clickhouse:8123/path",
	}
	for raw, want := range accepted {
		got, err := validateSetupClickHouseURL(raw)
		if err != nil || got != want {
			t.Errorf("%q: got %q, %v; want %q", raw, got, err, want)
		}
	}
}

func authConfigSetupOpen(t *testing.T, h *AuthHandler) interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	h.AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/api/auth/config", nil))
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out["setup_open"]
}

func TestAuthConfigSetupOpen(t *testing.T) {
	h, db, now, _ := setupFixture(t)
	auth := &AuthHandler{DB: db, Setup: h}
	if got := authConfigSetupOpen(t, auth); got != true {
		t.Fatalf("setup_open = %v, want true", got)
	}
	if got := authConfigSetupOpen(t, &AuthHandler{DB: db}); got != false {
		t.Fatalf("no setup handler: setup_open = %v, want false", got)
	}

	*now = now.Add(setupCodeTTL + time.Second)
	if got := authConfigSetupOpen(t, auth); got != false {
		t.Fatalf("expired: setup_open = %v, want false", got)
	}

	h2, db2, _, _ := setupFixture(t)
	if err := db2.SetSetting(settingSetupClosedAt, "2026-10-10T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if got := authConfigSetupOpen(t, &AuthHandler{DB: db2, Setup: h2}); got != false {
		t.Fatalf("closed flag: setup_open = %v, want false", got)
	}

	h3, db3, _, _ := setupFixture(t)
	if err := db3.SetUserRole("bob", "admin"); err != nil {
		t.Fatal(err)
	}
	if got := authConfigSetupOpen(t, &AuthHandler{DB: db3, Setup: h3}); got != false {
		t.Fatalf("admin exists: setup_open = %v, want false", got)
	}
}

func TestCloseSetupOnAdminLogin(t *testing.T) {
	h, db, _, _ := setupFixture(t)

	for _, role := range []string{"viewer", "analyst"} {
		closeSetupOnAdminLogin(db, h, role, "carol", "198.51.100.20")
		if v, _ := db.GetSetting(settingSetupClosedAt); v != "" {
			t.Fatalf("role %s closed setup", role)
		}
	}
	if !h.codeLive() {
		t.Fatal("non-admin login must keep the code")
	}

	closeSetupOnAdminLogin(db, h, "admin", "alice", "198.51.100.21")
	v, _ := db.GetSetting(settingSetupClosedAt)
	if _, err := time.Parse(time.RFC3339, v); err != nil {
		t.Fatalf("setup_closed_at = %q, want RFC3339", v)
	}
	if h.codeLive() {
		t.Fatal("admin login must discard the code")
	}
	rows := auditRows(t, db, "setup.closed")
	if len(rows) != 1 || deref(rows[0].Username) != "alice" || deref(rows[0].IPAddress) != "198.51.100.21" {
		t.Fatalf("setup.closed rows = %+v", rows)
	}

	// Sticky: a second admin login does not rewrite the flag or audit again.
	closeSetupOnAdminLogin(db, nil, "admin", "dave", "198.51.100.22")
	if v2, _ := db.GetSetting(settingSetupClosedAt); v2 != v {
		t.Fatalf("flag rewritten: %q -> %q", v, v2)
	}
	if n := len(auditRows(t, db, "setup.closed")); n != 1 {
		t.Fatalf("setup.closed rows = %d, want 1", n)
	}
}

func TestSetupStart(t *testing.T) {
	t.Run("admin exists closes setup", func(t *testing.T) {
		db, err := database.Open(filepath.Join(t.TempDir(), "start.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		if err := db.SetUserRole("bob", "admin"); err != nil {
			t.Fatal(err)
		}
		h := &SetupHandler{DB: db}
		h.Start()
		if v, _ := db.GetSetting(settingSetupClosedAt); v == "" {
			t.Fatal("startup with an admin must write setup_closed_at")
		}
		if h.codeLive() || h.Open() {
			t.Fatal("no code may be issued when an admin exists")
		}
		if n := len(auditRows(t, db, "setup.closed")); n != 1 {
			t.Fatalf("setup.closed rows = %d, want 1", n)
		}
		// Sticky: removing the admin does not reopen setup.
		if err := db.DeleteUserRole("bob"); err != nil {
			t.Fatal(err)
		}
		h.Start()
		if h.codeLive() {
			t.Fatal("setup reopened after the admin was removed")
		}
	})
	t.Run("fresh install issues a code", func(t *testing.T) {
		db, err := database.Open(filepath.Join(t.TempDir(), "fresh.db"))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		h := &SetupHandler{DB: db}
		h.Start()
		if !h.Open() {
			t.Fatal("fresh install should have setup open")
		}
		if v, _ := db.GetSetting(settingSetupClosedAt); v != "" {
			t.Fatal("fresh install must not close setup")
		}
	})
}
