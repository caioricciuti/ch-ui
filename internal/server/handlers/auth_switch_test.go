package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const switchSecret = "test-secret"

// switchFixture is credentialFixture plus a handler and a session "sess" for
// alice whose stored password is storedPassword. authSubject makes it an SSO
// session. alice has an explicit analyst role so role resolution never
// queries the fake agent.
func switchFixture(t *testing.T, storedPassword, authSubject string) (*AuthHandler, *database.DB, string) {
	t.Helper()
	db, gw, connID := credentialFixture(t)
	h := &AuthHandler{DB: db, Gateway: gw, RateLimiter: middleware.NewRateLimiter(db), Config: &config.Config{AppSecretKey: switchSecret}}
	enc, err := crypto.Encrypt(storedPassword, switchSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("alice", connID, "analyst"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSession(database.CreateSessionParams{
		ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: enc, Token: "sess",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), UserRole: "analyst", AuthSubject: authSubject,
	}); err != nil {
		t.Fatal(err)
	}
	return h, db, connID
}

func postSwitch(h *AuthHandler, body string, header map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/switch-connection", strings.NewReader(body))
	req.RemoteAddr = "198.51.100.30:5000"
	req.Header.Set("Content-Type", "application/json")
	for k, v := range header {
		req.Header.Set(k, v)
	}
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess"})
	rec := httptest.NewRecorder()
	h.SwitchConnection(rec, req)
	return rec
}

func newSessionToken(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionCookie && c.Value != "" {
			return c.Value
		}
	}
	t.Fatalf("no session cookie in %d %s", rec.Code, rec.Body.String())
	return ""
}

func auditActions(t *testing.T, db *database.DB) []database.AuditLog {
	t.Helper()
	logs, err := db.GetAuditLogs(100)
	if err != nil {
		t.Fatal(err)
	}
	return logs
}

func findAudit(logs []database.AuditLog, action, detailPart string) *database.AuditLog {
	for i := range logs {
		if logs[i].Action == action && logs[i].Details != nil && strings.Contains(*logs[i].Details, detailPart) {
			return &logs[i]
		}
	}
	return nil
}

func TestSwitchConnectionReusesCurrentPassword(t *testing.T) {
	h, db, connID := switchFixture(t, "right", "")
	rec := postSwitch(h, `{"connection_id":"`+connID+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("reuse: %d %s", rec.Code, rec.Body.String())
	}
	token := newSessionToken(t, rec)
	if token == "sess" {
		t.Fatal("token not rotated")
	}
	if s, _ := db.GetSession("sess"); s != nil {
		t.Fatal("old session still exists")
	}
	s, err := db.GetSession(token)
	if err != nil || s == nil || s.ClickhouseUser != "alice" {
		t.Fatalf("new session: %+v %v", s, err)
	}
	if pw, _ := crypto.Decrypt(s.EncryptedPassword, switchSecret); pw != "right" {
		t.Fatalf("stored password not carried over: %q", pw)
	}
	a := findAudit(auditActions(t, db), "user.switch_connection", "Switched connection from ch to ch")
	if a == nil || a.Username == nil || *a.Username != "alice" {
		t.Fatalf("switch audit row missing or wrong: %+v", a)
	}
}

func TestSwitchConnectionRejectedReuseIsNotCounted(t *testing.T) {
	h, db, connID := switchFixture(t, rejectedPassword, "")
	for i := 0; i < MaxAttemptsPerIP+1; i++ {
		rec := postSwitch(h, `{"connection_id":"`+connID+`","username":"alice"}`, nil)
		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("reuse attempt %d: %d %s, want 401", i+1, rec.Code, rec.Body.String())
		}
		var out map[string]interface{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if out["needs_password"] != true {
			t.Fatalf("reuse attempt %d: needs_password missing: %v", i+1, out)
		}
	}
	// The caller is still signed in and can still type the right password.
	if s, _ := db.GetSession("sess"); s == nil {
		t.Fatal("rejected reuse dropped the session")
	}
	if rec := postSwitch(h, `{"connection_id":"`+connID+`","username":"alice","password":"right"}`, nil); rec.Code != http.StatusOK {
		t.Fatalf("typed password after rejected reuse: %d %s", rec.Code, rec.Body.String())
	}
}

func TestSwitchConnectionSSOKeepsIdentity(t *testing.T) {
	h, db, connID := switchFixture(t, "irrelevant", "alice@example.com")
	enc, err := crypto.Encrypt("svc-pass", switchSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(connID, "svc_sso", enc); err != nil {
		t.Fatal(err)
	}
	// Request credentials are ignored for SSO sessions.
	rec := postSwitch(h, `{"connection_id":"`+connID+`","username":"default","password":"`+rejectedPassword+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sso switch: %d %s", rec.Code, rec.Body.String())
	}
	s, _ := db.GetSession(newSessionToken(t, rec))
	if s == nil {
		t.Fatal("new session missing")
	}
	if s.AuthSubject == nil || *s.AuthSubject != "alice@example.com" {
		t.Fatalf("auth subject lost: %v", s.AuthSubject)
	}
	if s.ClickhouseUser != "svc_sso" || s.EncryptedPassword != enc {
		t.Fatalf("session not on the service account: %q", s.ClickhouseUser)
	}
	if s.UserRole == nil || *s.UserRole != "analyst" {
		t.Fatalf("role not kept: %v", s.UserRole)
	}
	a := findAudit(auditActions(t, db), "user.switch_connection", "Switched connection")
	if a == nil || a.Username == nil || *a.Username != "alice@example.com" || a.ChUser == nil || *a.ChUser != "svc_sso" {
		t.Fatalf("sso switch audit row wrong: %+v", a)
	}
}

func TestSwitchConnectionSSOTargetWithoutAccount(t *testing.T) {
	h, db, _ := switchFixture(t, "irrelevant", "alice@example.com")
	other, err := db.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other-token"})
	if err != nil {
		t.Fatal(err)
	}
	rec := postSwitch(h, `{"connection_id":"`+other+`"}`, nil)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "sso_not_configured") {
		t.Fatalf("sso target without account: %d %s, want 409 sso_not_configured", rec.Code, rec.Body.String())
	}
	if s, _ := db.GetSession("sess"); s == nil {
		t.Fatal("refused switch dropped the session")
	}
}

func TestSwitchConnectionRejectsHiddenConnection(t *testing.T) {
	h, db, connID := switchFixture(t, "right", "")
	embeddedID, err := db.CreateConnection(database.CreateConnectionParams{
		Name: "Embedded", TunnelToken: "emb", IsEmbedded: true,
		Type: database.ConnectionTypeDirect, ClickHouseURL: "http://unreachable:8123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingSetupConnectionID, connID); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSetting(settingSetupEmbeddedURL, "http://unreachable:8123"); err != nil {
		t.Fatal(err)
	}
	rec := postSwitch(h, `{"connection_id":"`+embeddedID+`"}`, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("hidden connection: %d %s, want 400", rec.Code, rec.Body.String())
	}
}

func TestSwitchConnectionRequestChecks(t *testing.T) {
	h, _, connID := switchFixture(t, "right", "")
	body := `{"connection_id":"` + connID + `"}`

	req := httptest.NewRequest(http.MethodPost, "/switch-connection", strings.NewReader(body))
	req.Header.Set("Content-Type", "text/plain")
	req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess"})
	rec := httptest.NewRecorder()
	h.SwitchConnection(rec, req)
	if rec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("text/plain: %d, want 415", rec.Code)
	}

	if rec := postSwitch(h, body, map[string]string{"Origin": "https://evil.example"}); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign origin: %d, want 403", rec.Code)
	}
	// httptest requests go to example.com: the app's own origin passes.
	if rec := postSwitch(h, body, map[string]string{"Origin": "http://example.com", "Content-Type": "application/json; charset=utf-8"}); rec.Code != http.StatusOK {
		t.Fatalf("same origin: %d %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestSwitchConnectionConcurrentSwitchesOneWins(t *testing.T) {
	h, db, connID := switchFixture(t, "right", "")
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = postSwitch(h, `{"connection_id":"`+connID+`"}`, nil).Code
		}(i)
	}
	wg.Wait()
	ok := 0
	for _, c := range codes {
		switch c {
		case http.StatusOK:
			ok++
		case http.StatusConflict, http.StatusUnauthorized:
		default:
			t.Fatalf("unexpected status %d (codes %v)", c, codes)
		}
	}
	if ok != 1 {
		t.Fatalf("codes %v: want exactly one success", codes)
	}
	sessions, err := db.GetActiveSessionsByConnection(connID, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("%d sessions after concurrent switches, want 1 (no orphan)", len(sessions))
	}
}

func TestSwitchConnectionUnreachableIsAudited(t *testing.T) {
	h, db, connID := switchFixture(t, unreachablePassword, "")
	rec := postSwitch(h, `{"connection_id":"`+connID+`"}`, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("unreachable: %d %s, want 503", rec.Code, rec.Body.String())
	}
	if findAudit(auditActions(t, db), "user.login_failed", "clickhouse unreachable (switch connection)") == nil {
		t.Fatal("no audit row for an unreachable switch")
	}
	if s, _ := db.GetSession("sess"); s == nil {
		t.Fatal("unreachable switch dropped the session")
	}
}

func TestConnectionsReportSSOReadinessOnlyToSSOSessions(t *testing.T) {
	list := func(h *AuthHandler, cookie string) []connectionInfo {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/connections", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		h.Connections(rec, req)
		var out struct {
			Connections []connectionInfo `json:"connections"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out.Connections
	}

	h, _, _ := switchFixture(t, "irrelevant", "alice@example.com")
	for _, c := range list(h, "sess") {
		if c.SSOReady == nil || *c.SSOReady {
			t.Fatalf("sso session, no service account: sso_ready = %v, want false", c.SSOReady)
		}
	}
	for _, c := range list(h, "") {
		if c.SSOReady != nil {
			t.Fatal("sign-in page must not see sso_ready")
		}
	}

	hp, _, _ := switchFixture(t, "right", "")
	for _, c := range list(hp, "sess") {
		if c.SSOReady != nil {
			t.Fatal("password session must not see sso_ready")
		}
	}
}

// serverSwitchFixture signs alice in (stored password "right") on a
// connection described by current and puts a live agent behind a second
// connection described by target. It returns the target's ID and the agent,
// whose messages show which passwords reached the target.
func serverSwitchFixture(t *testing.T, current, target database.CreateConnectionParams) (*AuthHandler, *database.DB, string, *testutil.Agent) {
	t.Helper()
	db, err := dbtest.Open(t, "switch.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	current.Name, current.TunnelToken = "current", "current-token"
	target.Name, target.TunnelToken = "target", "target-token"
	currentID, err := db.CreateConnection(current)
	if err != nil {
		t.Fatal(err)
	}
	targetID, err := db.CreateConnection(target)
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "target-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type != "test_connection" {
			return nil
		}
		ok := msg.Password == "right"
		if !ok {
			return &tunnel.AgentMessage{Type: "test_result", Online: &ok,
				Error: "ClickHouse error: Code: 516. DB::Exception: alice: Authentication failed. (AUTHENTICATION_FAILED)"}
		}
		return &tunnel.AgentMessage{Type: "test_result", Online: &ok, Version: "25.1"}
	})
	deadline := time.Now().Add(5 * time.Second)
	for !agent.Gateway.IsTunnelOnline(targetID) {
		if time.Now().After(deadline) {
			t.Fatal("agent never came online")
		}
		time.Sleep(10 * time.Millisecond)
	}
	h := &AuthHandler{DB: db, Gateway: agent.Gateway, RateLimiter: middleware.NewRateLimiter(db), Config: &config.Config{AppSecretKey: switchSecret}}
	enc, err := crypto.Encrypt("right", switchSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetUserRole("alice", targetID, "analyst"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateSession(database.CreateSessionParams{
		ConnectionID: currentID, ClickhouseUser: "alice", EncryptedPassword: enc, Token: "sess",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), UserRole: "analyst",
	}); err != nil {
		t.Fatal(err)
	}
	return h, db, targetID, agent
}

func directConn(url string) database.CreateConnectionParams {
	return database.CreateConnectionParams{Type: database.ConnectionTypeDirect, ClickHouseURL: url}
}

func testedPasswords(agent *testutil.Agent) []string {
	var out []string
	for _, m := range agent.Messages() {
		if m.Type == "test_connection" {
			out = append(out, m.Password)
		}
	}
	return out
}

func TestSwitchConnectionSameServerReusesPassword(t *testing.T) {
	// Same host and port: case and the scheme's default port do not matter.
	h, db, targetID, agent := serverSwitchFixture(t, directConn("http://CH.internal:8123"), directConn("http://ch.internal/"))
	rec := postSwitch(h, `{"connection_id":"`+targetID+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("same server: %d %s, want 200", rec.Code, rec.Body.String())
	}
	if got := testedPasswords(agent); len(got) != 1 || got[0] != "right" {
		t.Fatalf("passwords sent to target: %v, want the stored one", got)
	}
	s, _ := db.GetSession(newSessionToken(t, rec))
	if s == nil || s.ConnectionID != targetID {
		t.Fatalf("session not moved to the target: %+v", s)
	}
}

func TestSwitchConnectionOtherServerNeedsPassword(t *testing.T) {
	cases := []struct {
		name            string
		current, target database.CreateConnectionParams
	}{
		{"different host", directConn("http://ch-a:8123"), directConn("http://ch-b:8123")},
		{"different port", directConn("http://ch:8123"), directConn("http://ch:9123")},
		{"agent target", directConn("http://ch:8123"), database.CreateConnectionParams{}},
		{"agent to agent", database.CreateConnectionParams{}, database.CreateConnectionParams{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, db, targetID, agent := serverSwitchFixture(t, tc.current, tc.target)
			rec := postSwitch(h, `{"connection_id":"`+targetID+`"}`, nil)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("%d %s, want 401", rec.Code, rec.Body.String())
			}
			var out map[string]interface{}
			_ = json.Unmarshal(rec.Body.Bytes(), &out)
			if out["needs_password"] != true || out["reason"] != "different_server" || out["message"] != "Different server, enter the password for alice." {
				t.Fatalf("response: %v", out)
			}
			if strings.Contains(rec.Body.String(), "right") {
				t.Fatal("stored password leaked into the response")
			}
			if got := testedPasswords(agent); len(got) != 0 {
				t.Fatalf("target received passwords %v, want none", got)
			}
			if a := findAudit(auditActions(t, db), "user.login_failed", ""); a != nil {
				t.Fatalf("failure recorded against the target: %+v", a)
			}
			if s, _ := db.GetSession("sess"); s == nil {
				t.Fatal("refused switch dropped the session")
			}
			// The typed password goes through.
			if rec := postSwitch(h, `{"connection_id":"`+targetID+`","username":"alice","password":"right"}`, nil); rec.Code != http.StatusOK {
				t.Fatalf("typed password: %d %s", rec.Code, rec.Body.String())
			}
		})
	}
}
