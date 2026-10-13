package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// The Brain artifact query runs with readonly=1 and its limits as settings,
// and refuses a SETTINGS clause that would try to lift them.
func TestBrainRunQueryArtifactIsReadonly(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var mu sync.Mutex
	var sent []tunnel.GatewayMessage
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		mu.Lock()
		sent = append(sent, msg)
		mu.Unlock()
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
	})
	password, err := crypto.Encrypt("pw", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	session := &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: "u", EncryptedPassword: password, UserRole: "admin"}
	chatID, err := db.CreateBrainChat(middleware.Actor(session), conn, "t", "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	h := &BrainHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-key"}}
	r := chi.NewRouter()
	r.Post("/chats/{chatID}/artifacts/query", h.RunQueryArtifact)
	post := func(query string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"query": query})
		req := httptest.NewRequest(http.MethodPost, "/chats/"+chatID+"/artifacts/query", strings.NewReader(string(body)))
		req = req.WithContext(middleware.SetSession(req.Context(), session))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}

	if rr := post("SELECT 1"); rr.Code != http.StatusOK {
		t.Fatalf("want 200, got %d: %s", rr.Code, rr.Body.String())
	}
	mu.Lock()
	if len(sent) != 1 {
		mu.Unlock()
		t.Fatalf("want one query sent, got %d", len(sent))
	}
	s := sent[0].Settings
	mu.Unlock()
	if s["readonly"] != "1" || s["max_result_rows"] == "" || s["max_execution_time"] == "" || s["max_result_bytes"] == "" || s["result_overflow_mode"] != "break" {
		t.Errorf("want readonly=1 and caps as settings, got %v", s)
	}

	if rr := post("SELECT 1 SETTINGS max_result_rows = 0"); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "SETTINGS") {
		t.Errorf("SETTINGS clause: want 400, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr := post("SELECT 'SETTINGS a = 1' AS s"); rr.Code != http.StatusOK {
		t.Errorf("SETTINGS inside a string is fine, got %d: %s", rr.Code, rr.Body.String())
	}
	if rr := post("DROP TABLE t"); rr.Code != http.StatusBadRequest {
		t.Errorf("write: want 400, got %d", rr.Code)
	}
}

func TestEscapeLiteralBackslash(t *testing.T) {
	for in, want := range map[string]string{
		"it's": "it''s",
		`a\`:   `a\\`,
		`a\'`:  `a\\''`,
	} {
		if got := safe.Literal(in); got != want {
			t.Errorf("safe.Literal(%q) = %q, want %q", in, got, want)
		}
	}
}
