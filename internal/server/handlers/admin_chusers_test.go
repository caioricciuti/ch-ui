package handlers

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// The new password of a ClickHouse user never comes back in a response,
// an error body or the log, even when ClickHouse quotes the statement back.
func TestClickHouseUserPasswordNeverEchoed(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type != "query" {
			return nil
		}
		if strings.Contains(msg.SQL, "broken") {
			return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 62. DB::Exception: Syntax error in: " + msg.SQL}
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`), Meta: json.RawMessage(`[]`)}
	})
	cfg := &config.Config{AppSecretKey: "test-secret"}
	enc, err := crypto.Encrypt("adminpw", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	r := chi.NewRouter()
	(&AdminHandler{DB: db, Gateway: agent.Gateway, Config: cfg}).Routes(r)

	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	const secret = "S3cr3t-pa'ss"
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			ConnectionID: conn, ClickhouseUser: "default", EncryptedPassword: enc, UserRole: "admin",
		}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	pw, _ := json.Marshal(secret)
	for _, c := range []struct {
		name, method, path, body string
		want                     int
	}{
		{"create", "POST", "/clickhouse-users", `{"name":"ana","password":` + string(pw) + `}`, http.StatusCreated},
		{"create plaintext", "POST", "/clickhouse-users", `{"name":"ana2","auth_type":"plaintext_password","password":` + string(pw) + `}`, http.StatusCreated},
		{"create error", "POST", "/clickhouse-users", `{"name":"broken","password":` + string(pw) + `}`, http.StatusBadGateway},
		{"update", "PUT", "/clickhouse-users/ana/password", `{"password":` + string(pw) + `}`, http.StatusOK},
		{"update error", "PUT", "/clickhouse-users/broken/password", `{"password":` + string(pw) + `}`, http.StatusBadGateway},
	} {
		rec := do(c.method, c.path, c.body)
		raw := rec.Body.String()
		if rec.Code != c.want {
			t.Fatalf("%s: %d %s", c.name, rec.Code, raw)
		}
		if strings.Contains(raw, "S3cr3t") {
			t.Fatalf("%s: password in body: %s", c.name, raw)
		}
		if !strings.Contains(raw, "BY '***'") {
			t.Fatalf("%s: masked command missing: %s", c.name, raw)
		}
	}
	if strings.Contains(logs.String(), "S3cr3t") {
		t.Fatalf("password in log: %s", logs.String())
	}
	// The real statement still carries the password to ClickHouse.
	sent := false
	for _, m := range agent.Messages() {
		if strings.Contains(m.SQL, "S3cr3t-pa''ss") {
			sent = true
		}
	}
	if !sent {
		t.Fatal("password never reached ClickHouse")
	}
}

func TestMaskChUserPassword(t *testing.T) {
	for _, c := range []struct{ in, pw, want string }{
		{"CREATE USER `a` IDENTIFIED WITH sha256_password BY 'x''y'", "x'y", "CREATE USER `a` IDENTIFIED WITH sha256_password BY '***'"},
		{"ALTER USER `a` IDENTIFIED BY 'p'", "p", "ALTER USER `a` IDENTIFIED BY '***'"},
		{"error near hunter2", "hunter2", "error near ***"},
		{"ALTER USER `a` IDENTIFIED WITH no_password", "", "ALTER USER `a` IDENTIFIED WITH no_password"},
	} {
		if got := maskChUserPassword(c.in, c.pw); got != c.want {
			t.Fatalf("mask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// Governance has its own CREATE USER endpoint; a ClickHouse error that quotes
// the statement must not carry the password into the response or the log.
func TestGovernanceCreateChUserPasswordNeverEchoed(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type != "query" {
			return nil
		}
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 62. DB::Exception: Syntax error in: " + msg.SQL}
	})
	cfg := &config.Config{AppSecretKey: "test-secret"}
	enc, err := crypto.Encrypt("adminpw", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	var logs bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })

	h := &GovernanceHandler{DB: db, Gateway: agent.Gateway, Config: cfg}
	req := httptest.NewRequest("POST", "/access/users", strings.NewReader(`{"name":"gov_user","auth_type":"sha256_password","password":"G0v-S3cr3t"}`))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
		ConnectionID: conn, ClickhouseUser: "default", EncryptedPassword: enc, UserRole: "admin",
	}))
	rec := httptest.NewRecorder()
	h.CreateChUser(rec, req)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("want 502, got %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "G0v-S3cr3t") || strings.Contains(logs.String(), "G0v-S3cr3t") {
		t.Fatalf("password echoed: body %s log %s", rec.Body.String(), logs.String())
	}
}
