package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

func newBrainConnectionFixture(t *testing.T) (*BrainHandler, *database.DB, string, string) {
	t.Helper()
	db, err := dbtest.Open(t, "brain-conn.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connA, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	connB, err := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"})
	if err != nil {
		t.Fatal(err)
	}
	return &BrainHandler{DB: db, Config: &config.Config{AppSecretKey: "test-key"}}, db, connA, connB
}

// A chat started on one connection does not open, continue or run queries
// while the session is on another: its history would meet the other
// server's schema and credentials.
func TestBrainChatStaysOnConnection(t *testing.T) {
	h, db, connA, connB := newBrainConnectionFixture(t)
	chatID, err := db.CreateBrainChat("ana", connB, "on b", "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	router := chi.NewRouter()
	h.Routes(router)
	serve := func(connID, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ClickhouseUser: "ana", UserRole: "analyst", ConnectionID: connID}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	base := "/chats/" + chatID
	cross := []struct{ method, path, body string }{
		{http.MethodGet, base, ""},
		{http.MethodPut, base, `{"title":"moved"}`},
		{http.MethodDelete, base, ""},
		{http.MethodGet, base + "/messages", ""},
		{http.MethodPost, base + "/messages/stream", `{"content":"hi"}`},
		{http.MethodGet, base + "/artifacts", ""},
		{http.MethodPost, base + "/artifacts/query", `{"query":"SELECT 1"}`},
	}
	for _, c := range cross {
		if rec := serve(connA, c.method, c.path, c.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s %s from another connection: status %d, want 404", c.method, c.path, rec.Code)
		}
	}
	chat, err := db.GetBrainChatByIDForUser(chatID, "ana")
	if err != nil || chat == nil {
		t.Fatalf("chat gone after cross-connection requests: %v", err)
	}
	if chat.Title != "on b" {
		t.Errorf("cross-connection update changed the title to %q", chat.Title)
	}
	if msgs, _ := db.GetBrainMessages(chatID); len(msgs) != 0 {
		t.Errorf("cross-connection stream stored %d messages", len(msgs))
	}

	if rec := serve(connB, http.MethodGet, base, ""); rec.Code != http.StatusOK {
		t.Errorf("get on own connection: status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := serve(connB, http.MethodGet, base+"/messages", ""); rec.Code != http.StatusOK {
		t.Errorf("messages on own connection: status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := serve(connB, http.MethodDelete, base, ""); rec.Code != http.StatusOK {
		t.Errorf("delete on own connection: status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

// Models and pipelines attached to a chat by id read as not found when they
// belong to another connection.
func TestBrainEntityContextStaysOnConnection(t *testing.T) {
	h, db, connA, connB := newBrainConnectionFixture(t)
	ownModel, err := db.CreateModel(connA, "own_model", "", "default", "view", "SELECT 'own model sql'", "", "", "ana")
	if err != nil {
		t.Fatal(err)
	}
	otherModel, err := db.CreateModel(connB, "other_model", "", "default", "view", "SELECT 'other model sql'", "", "", "ana")
	if err != nil {
		t.Fatal(err)
	}
	ownPipeline, err := db.CreatePipeline("own pipeline", "", connA, "ana")
	if err != nil {
		t.Fatal(err)
	}
	otherPipeline, err := db.CreatePipeline("other pipeline", "", connB, "ana")
	if err != nil {
		t.Fatal(err)
	}

	prompt := h.buildEntityContextPrompt([]entityContext{
		{Type: "model", ID: ownModel, Name: "own_model"},
		{Type: "model", ID: otherModel, Name: "other_model"},
		{Type: "pipeline", ID: ownPipeline, Name: "own pipeline"},
		{Type: "pipeline", ID: otherPipeline, Name: "other pipeline"},
	}, database.SavedQueryViewer{Actor: "ana", Role: "analyst"}, connA)

	for _, want := range []string{
		"own model sql",
		"Model 'other_model' (id=" + otherModel + "): not found",
		"Pipeline 'own pipeline' (id=" + ownPipeline + "): status=",
		"Pipeline 'other pipeline' (id=" + otherPipeline + "): not found",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt missing %q:\n%s", want, prompt)
		}
	}
	if strings.Contains(prompt, "other model sql") {
		t.Errorf("prompt carries another connection's model SQL:\n%s", prompt)
	}
}
