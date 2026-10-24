package handlers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/safe"
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
		if got := (chUserAuth{legacyPassword: c.pw}).mask(c.in); got != c.want {
			t.Fatalf("mask(%q) = %q, want %q", c.in, got, c.want)
		}
	}
	// A hashed auth masks its hash and salt, in the statement and anywhere
	// an error quotes them.
	hashed := chUserAuth{Method: "sha256_hash", Hash: strings.Repeat("ab", 32), Salt: "s4lt"}
	sql := "CREATE USER `a`" + hashed.clause(false)
	if got := hashed.mask(sql); got != "CREATE USER `a` IDENTIFIED WITH sha256_hash BY '***' SALT '***'" {
		t.Fatalf("mask(hashed) = %q", got)
	}
	if got := hashed.mask("near " + hashed.Hash + " and s4lt"); strings.Contains(got, hashed.Hash) || strings.Contains(got, "s4lt") {
		t.Fatalf("mask(error) = %q", got)
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

// Admin → Users sends a hash made in the browser: it reaches ClickHouse as
// IDENTIFIED WITH sha256_hash, is masked everywhere it comes back, and needs
// no deprecation notice. The old plaintext fields still work, marked
// deprecated, and a malformed or mixed request is refused before any SQL.
func TestClickHouseUserHashedAuth(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type != "query" {
			return nil
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
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			ConnectionID: conn, ClickhouseUser: "default", EncryptedPassword: enc, UserRole: "admin",
		}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	hash := strings.Repeat("0a", 32)
	sent := func(fragment string) bool {
		for _, m := range agent.Messages() {
			if strings.Contains(m.SQL, fragment) {
				return true
			}
		}
		return false
	}

	rec := do("POST", "/clickhouse-users", `{"name":"ana","auth":{"method":"sha256_hash","hash":"`+hash+`","salt":"c0ffee"}}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create hashed: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), hash) || strings.Contains(rec.Body.String(), "c0ffee") {
		t.Fatalf("hash or salt in response: %s", rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "IDENTIFIED WITH sha256_hash BY '***' SALT '***'") {
		t.Fatalf("masked command missing: %s", rec.Body.String())
	}
	if rec.Header().Get("Deprecation") != "" {
		t.Fatal("hashed create marked deprecated")
	}
	if !sent("CREATE USER `ana` IDENTIFIED WITH sha256_hash BY '" + hash + "' SALT 'c0ffee'") {
		t.Fatal("hashed CREATE USER never reached ClickHouse")
	}

	rec = do("PUT", "/clickhouse-users/ana/password", `{"auth":{"method":"no_password"}}`)
	if rec.Code != http.StatusOK || !sent("ALTER USER IF EXISTS `ana` IDENTIFIED WITH no_password") {
		t.Fatalf("change to no_password: %d %s", rec.Code, rec.Body.String())
	}

	rec = do("POST", "/clickhouse-users", `{"name":"old","password":"pw"}`)
	if rec.Code != http.StatusCreated || rec.Header().Get("Deprecation") != "true" {
		t.Fatalf("plaintext create: %d deprecation=%q", rec.Code, rec.Header().Get("Deprecation"))
	}
	logs, err := db.GetAuditLogs(20)
	if err != nil {
		t.Fatal(err)
	}
	noted := false
	for _, l := range logs {
		if l.Details != nil && strings.Contains(*l.Details, "name=old") && strings.Contains(*l.Details, "deprecated=plaintext_password_field") {
			noted = true
		}
	}
	if !noted {
		t.Fatal("audit log does not mark the deprecated plaintext field")
	}

	before := len(agent.Messages())
	for name, body := range map[string]string{
		"short hash":   `{"name":"x","auth":{"method":"sha256_hash","hash":"abc"}}`,
		"salt on sha1": `{"name":"x","auth":{"method":"double_sha1_hash","hash":"` + strings.Repeat("a", 40) + `","salt":"s"}}`,
		"plaintext":    `{"name":"x","auth":{"method":"plaintext_password","hash":"pw"}}`,
		"both":         `{"name":"x","password":"pw","auth":{"method":"no_password"}}`,
		"bad salt":     `{"name":"x","auth":{"method":"sha256_hash","hash":"` + hash + `","salt":"a'b"}}`,
	} {
		if rec := do("POST", "/clickhouse-users", body); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rec.Code, rec.Body.String())
		}
	}
	if n := len(agent.Messages()) - before; n != 0 {
		t.Fatalf("%d statements sent for refused requests", n)
	}
}

// The browser sends sha256(password + salt) as hex with the salt. A user
// created or changed from that hash signs in with the password, and only
// with it, on a real ClickHouse.
func TestHashedChUserSignInLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, adminPass := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	name := "chui_test_hashed_" + time.Now().Format("150405")
	t.Cleanup(func() {
		_, _ = client.Execute(context.Background(), "DROP USER IF EXISTS "+safe.Identifier(name), admin, adminPass, nil)
	})
	hashed := func(password, salt string) chUserAuth {
		sum := sha256.Sum256([]byte(password + salt))
		return chUserAuth{Method: "sha256_hash", Hash: hex.EncodeToString(sum[:]), Salt: salt}
	}
	signsIn := func(password string) bool {
		_, err := client.Execute(ctx, "SELECT currentUser()", name, password, nil)
		return err == nil
	}

	if _, err := client.Execute(ctx, buildCreateChUserSQL(name, hashed("first-Pw-1", "0123abcd")), admin, adminPass, nil); err != nil {
		t.Fatalf("create: %v", err)
	}
	if !signsIn("first-Pw-1") || signsIn("wrong") {
		t.Fatal("created user: the password must sign in and a wrong one must not")
	}
	alter := "ALTER USER " + safe.Identifier(name) + hashed("second-Pw-2", "feedbeef").clause(true)
	if _, err := client.Execute(ctx, alter, admin, adminPass, nil); err != nil {
		t.Fatalf("alter: %v", err)
	}
	if !signsIn("second-Pw-2") || signsIn("first-Pw-1") {
		t.Fatal("changed user: only the new password must sign in")
	}
}
