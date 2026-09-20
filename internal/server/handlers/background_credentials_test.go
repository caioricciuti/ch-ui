package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

func TestBackgroundAccountAdminRoutes(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	h := &ConnectionsHandler{DB: db, Config: &config.Config{AppSecretKey: "secret"}}
	r := chi.NewRouter()
	r.Route("/connections/{id}/background-credentials", h.BackgroundCredentialRoutes)
	call := func(role, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/connections/"+conn+"/background-credentials"+path, strings.NewReader(body))
		if role != "" {
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: "shared-sso", AuthSubject: "operator@example.test"}))
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	for _, role := range []string{"", "viewer", "analyst"} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			path := "/"
			if method == http.MethodPut {
				path = "/schedule"
			}
			if rr := call(role, method, path, `{"mode":"disabled"}`); rr.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d", role, method, rr.Code)
			}
		}
	}
	if rr := call("admin", "GET", "/", ""); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "password") {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	for _, body := range []string{`{"mode":"invalid"}`, `{"mode":"service_account","username":"worker"}`, `{"mode":"disabled","unexpected":true}`} {
		if rr := call("admin", "PUT", "/schedule", body); rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid input: %d", rr.Code)
		}
	}
	if rr := call("admin", "PUT", "/unknown", `{"mode":"disabled"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown worker: %d", rr.Code)
	}
	if rr := call("admin", "PUT", "/schedule", `{"mode":"service_account","username":"worker","password":"test"}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline verification: %d", rr.Code)
	}
	for _, mode := range []string{"disabled", "session"} {
		if rr := call("admin", "PUT", "/schedule", `{"mode":"`+mode+`"}`); rr.Code != http.StatusOK {
			t.Fatalf("save %s: %d %s", mode, rr.Code, rr.Body.String())
		}
	}
	logs, err := db.GetAuditLogs(10)
	if err != nil || len(logs) != 2 {
		t.Fatalf("audit: %d %v", len(logs), err)
	}
	if logs[0].Username == nil || *logs[0].Username != "operator@example.test" {
		t.Fatal("audit must identify the human administrator")
	}
}

func TestBackgroundAccountVerificationAndRotation(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "verify.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "verify", TunnelToken: "verify-token"})
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "verify-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if msg.Type != "query" || msg.Password == "wait" {
			return nil
		}
		if msg.Password == "rejected-secret" {
			return &tunnel.AgentMessage{Type: "query_error", Error: "Authentication failed: rejected-secret"}
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"1":1}]`)}
	})
	h := &ConnectionsHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}}
	r := chi.NewRouter()
	r.Route("/connections/{id}/background-credentials", h.BackgroundCredentialRoutes)
	call := func(password string, ctx context.Context) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]string{"mode": "service_account", "username": "worker", "password": password})
		req := httptest.NewRequest("PUT", "/connections/"+conn+"/background-credentials/schedule", strings.NewReader(string(body)))
		req = req.WithContext(middleware.SetSession(ctx, &middleware.SessionInfo{UserRole: "admin", ClickhouseUser: "admin"}))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	for _, password := range []string{"first-secret", "rotated-secret", ""} {
		rr := call(password, context.Background())
		if rr.Code != 200 {
			t.Fatalf("save: %d %s", rr.Code, rr.Body.String())
		}
		if strings.Contains(rr.Body.String(), "password") || (password != "" && strings.Contains(rr.Body.String(), password)) {
			t.Fatal("response leaked credentials")
		}
		user, got, err := db.BackgroundCredentials(conn, "schedule", "test-secret")
		if err != nil || user != "worker" || got != password {
			t.Fatalf("saved credentials: %v", err)
		}
		before, _ := db.GetBackgroundCredential(conn, "schedule")
		if rr := call("rejected-secret", context.Background()); rr.Code != 400 || strings.Contains(rr.Body.String(), "rejected-secret") {
			t.Fatalf("failed verification: %d %s", rr.Code, rr.Body.String())
		}
		after, _ := db.GetBackgroundCredential(conn, "schedule")
		if before != after {
			t.Fatal("failed verification replaced account")
		}
	}
	before, _ := db.GetBackgroundCredential(conn, "schedule")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if rr := call("wait", ctx); rr.Code != 400 {
		t.Fatalf("cancel: %d", rr.Code)
	}
	after, _ := db.GetBackgroundCredential(conn, "schedule")
	if before != after {
		t.Fatal("cancelled verification replaced account")
	}
	deadline := time.Now().Add(time.Second)
	for {
		msgs := agent.Messages()
		if len(msgs) > 0 && msgs[len(msgs)-1].Type == "cancel_query" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("query cancellation not forwarded")
		}
		time.Sleep(time.Millisecond)
	}
	logs, err := db.GetAuditLogs(100)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(logs)
	for _, secret := range []string{"first-secret", "rotated-secret", "rejected-secret", after.EncryptedPassword} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("audit leaked credentials")
		}
	}
	if _, _, err := db.BackgroundCredentials(conn, "schedule", "wrong-secret"); err == nil {
		t.Fatal("wrong key accepted")
	}
}
