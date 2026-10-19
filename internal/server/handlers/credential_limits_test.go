package handlers

import (
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

const (
	rejectedPassword    = "wrong"
	unreachablePassword = "unreachable"
)

// credentialFixture is a connection with a live agent that rejects the
// password "wrong", reports ClickHouse unreachable for "unreachable" and
// accepts anything else.
func credentialFixture(t *testing.T) (*database.DB, *tunnel.Gateway, string) {
	t.Helper()
	db, err := dbtest.Open(t, "creds.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "ch", TunnelToken: "creds-token"})
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "creds-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type != "test_connection" {
			return nil
		}
		offline := false
		switch msg.Password {
		case rejectedPassword:
			return &tunnel.AgentMessage{Type: "test_result", Online: &offline,
				Error: "ClickHouse error: Code: 516. DB::Exception: default: Authentication failed. (AUTHENTICATION_FAILED)"}
		case unreachablePassword:
			return &tunnel.AgentMessage{Type: "test_result", Online: &offline,
				Error: `request failed: Post "http://ch:9000/?default_format=JSON": malformed HTTP response "\x00"`}
		}
		online := true
		return &tunnel.AgentMessage{Type: "test_result", Online: &online, Version: "25.1"}
	})
	deadline := time.Now().Add(5 * time.Second)
	for !agent.Gateway.IsTunnelOnline(connID) {
		if time.Now().After(deadline) {
			t.Fatal("agent never came online")
		}
		time.Sleep(10 * time.Millisecond)
	}
	return db, agent.Gateway, connID
}

func TestSwitchConnectionSharesLoginRateLimit(t *testing.T) {
	db, gw, connID := credentialFixture(t)
	h := &AuthHandler{DB: db, Gateway: gw, RateLimiter: middleware.NewRateLimiter(db), Config: &config.Config{AppSecretKey: "test-secret"}}
	if _, err := db.CreateSession(database.CreateSessionParams{
		ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: "x", Token: "sess-1",
		ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339), UserRole: "viewer",
	}); err != nil {
		t.Fatal(err)
	}
	switchAs := func(password string) *httptest.ResponseRecorder {
		body := `{"connection_id":"` + connID + `","username":"victim","password":"` + password + `"}`
		req := httptest.NewRequest(http.MethodPost, "/switch-connection", strings.NewReader(body))
		req.RemoteAddr = "198.51.100.20:5000"
		req.Header.Set("Content-Type", "application/json")
		req.AddCookie(&http.Cookie{Name: SessionCookie, Value: "sess-1"})
		rec := httptest.NewRecorder()
		h.SwitchConnection(rec, req)
		return rec
	}

	// Unreachable ClickHouse is a 503 and never counts.
	for i := 0; i < MaxAttemptsPerUser+1; i++ {
		if rec := switchAs(unreachablePassword); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "Port 9000") {
			t.Fatalf("unreachable attempt %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	for i := 0; i < MaxAttemptsPerUser; i++ {
		if rec := switchAs(rejectedPassword); rec.Code != http.StatusUnauthorized {
			t.Fatalf("rejected attempt %d: %d %s", i+1, rec.Code, rec.Body.String())
		}
	}
	if rec := switchAs(rejectedPassword); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt past the limit: %d, want 429", rec.Code)
	}
	// The lockout is shared with login for the same user and connection.
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"connection_id":"`+connID+`","username":"victim","password":"right"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "192.0.2.99:5000"
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("login after switch lockout: %d, want 429", rec.Code)
	}
}

func TestConnectionTestEndpoint(t *testing.T) {
	db, gw, connID := credentialFixture(t)
	h := &ConnectionsHandler{DB: db, Gateway: gw, Config: &config.Config{}, RateLimiter: middleware.NewRateLimiter(db)}
	r := chi.NewRouter()
	r.Post("/connections/{id}/test", h.TestConnection)
	call := func(role, id, password string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/connections/"+id+"/test", strings.NewReader(`{"username":"bob","password":"`+password+`"}`))
		req.RemoteAddr = "198.51.100.30:5000"
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: "admin"}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	for _, role := range []string{"viewer", "analyst"} {
		if rec := call(role, connID, "ok"); rec.Code != http.StatusForbidden {
			t.Fatalf("%s: %d, want 403", role, rec.Code)
		}
	}
	if rec := call("admin", "missing", "ok"); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown connection: %d, want 404", rec.Code)
	}
	if rec := call("admin", connID, "ok"); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
		t.Fatalf("good credentials: %d %s", rec.Code, rec.Body.String())
	}
	if rec := call("admin", connID, unreachablePassword); rec.Code != http.StatusBadGateway {
		t.Fatalf("unreachable: %d, want 502", rec.Code)
	}
	for i := 0; i < MaxAttemptsPerUser; i++ {
		if rec := call("admin", connID, rejectedPassword); rec.Code != http.StatusBadRequest {
			t.Fatalf("rejected attempt %d: %d, want 400", i+1, rec.Code)
		}
	}
	if rec := call("admin", connID, rejectedPassword); rec.Code != http.StatusTooManyRequests {
		t.Fatalf("attempt past the limit: %d, want 429", rec.Code)
	}
}

func TestConnectionTestFallbackLimiter(t *testing.T) {
	db, gw, connID := credentialFixture(t)
	h := &ConnectionsHandler{DB: db, Gateway: gw}
	r := chi.NewRouter()
	r.Post("/connections/{id}/test", h.TestConnection)
	var last int
	for i := 0; i < MaxAttemptsPerUser+1; i++ {
		req := httptest.NewRequest(http.MethodPost, "/connections/"+connID+"/test", strings.NewReader(`{"username":"bob","password":"wrong"}`))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "admin"}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		last = rec.Code
	}
	if last != http.StatusTooManyRequests {
		t.Fatalf("without a wired limiter the endpoint must still lock: got %d", last)
	}
}

func TestSetupInstructionsUseAppURL(t *testing.T) {
	tests := []struct {
		appURL string
		want   string
	}{
		{"https://ch-ui.example.com", "--url wss://ch-ui.example.com/connect --key tok"},
		{"http://10.0.0.5:3488/", "--url ws://10.0.0.5:3488/connect --key tok"},
		{"https://example.com/chui", "--url wss://example.com/chui/connect --key tok"},
		{"http://localhost:3488", "--url <YOUR_SERVER_URL>/connect --key tok"},
		{"http://127.0.0.1:3488", "--url <YOUR_SERVER_URL>/connect --key tok"},
		{"", "--url <YOUR_SERVER_URL>/connect --key tok"},
	}
	for _, tc := range tests {
		got := getSetupInstructions(&config.Config{AppURL: tc.appURL}, "tok")
		if !strings.HasSuffix(got["connect"], tc.want) || !strings.HasSuffix(got["service"], tc.want) {
			t.Fatalf("app_url %q: got %v, want suffix %q", tc.appURL, got, tc.want)
		}
	}
	if got := getSetupInstructions(nil, "tok"); !strings.Contains(got["connect"], "<YOUR_SERVER_URL>") {
		t.Fatalf("nil config: %v", got)
	}
}
