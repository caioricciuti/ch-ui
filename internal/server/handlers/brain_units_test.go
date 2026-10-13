package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/go-chi/chi/v5"

	braincore "github.com/caioricciuti/ch-ui/internal/brain"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestAutoTitleCutsOnRunes(t *testing.T) {
	title := autoTitle(strings.Repeat("ção ", 30))
	if !utf8.ValidString(title) {
		t.Fatalf("title is not valid UTF-8: %q", title)
	}
	if !strings.HasSuffix(title, "...") || utf8.RuneCountInString(title) > 51 {
		t.Errorf("unexpected title %q", title)
	}
	if autoTitle("short") != "short" {
		t.Error("short prompts are kept")
	}
}

func TestBuildBrainHistoryKeepsNewestWithinBudget(t *testing.T) {
	var msgs []database.BrainMessage
	for i := 0; i < 40; i++ {
		role := "user"
		if i%2 == 1 {
			role = "assistant"
		}
		msgs = append(msgs, database.BrainMessage{ID: "m" + string(rune('A'+i)), Role: role, Content: strings.Repeat("w", 4000), Status: "complete"})
	}
	msgs = append(msgs, database.BrainMessage{ID: "last", Role: "user", Content: "latest question", Status: "complete"})
	msgs = append(msgs, database.BrainMessage{ID: "streaming", Role: "assistant", Content: "", Status: "streaming"})

	kept, omitted := buildBrainHistory(msgs, nil, 5000)
	if len(kept) == 0 || kept[len(kept)-1].Content != "latest question" {
		t.Fatal("the prompt being answered must always be kept")
	}
	if omitted == 0 {
		t.Fatal("older messages should be left out")
	}
	total := 0
	for _, m := range kept {
		total += estimateTokens(m.Content)
	}
	if total > 5000 {
		t.Errorf("kept %d tokens, budget 5000", total)
	}
	if kept[0].Role != "user" {
		t.Error("the window should start on a user turn")
	}
	if !strings.Contains(historyOmittedNote(omitted), "left out") {
		t.Error("the model should be told turns were left out")
	}
}

func TestBuildBrainHistoryAddsToolSummary(t *testing.T) {
	msgs := []database.BrainMessage{
		{ID: "u1", Role: "user", Content: "make a dashboard", Status: "complete"},
		{ID: "a1", Role: "assistant", Content: "Done.", Status: "complete"},
		{ID: "u2", Role: "user", Content: "add a panel to it", Status: "complete"},
	}
	calls := []database.BrainToolCall{{MessageID: "a1", ToolName: "create_dashboard", Status: "success", OutputJSON: `{"dashboard_id":"d-42","name":"Sales","open_url":"/x"}`}}
	kept, _ := buildBrainHistory(msgs, calls, 10000)
	if len(kept) != 3 || !strings.Contains(kept[1].Content, "dashboard_id=d-42") || !strings.Contains(kept[1].Content, "<tool_output") {
		t.Fatalf("assistant turn should carry the fenced tool summary: %+v", kept)
	}
}

func TestCompactTurnElidesOldestToolOutput(t *testing.T) {
	msgs := []braincore.ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "tool", Name: "run_query", ToolCallID: "1", Content: strings.Repeat("a", 5000)},
		{Role: "tool", Name: "run_query", ToolCallID: "2", Content: strings.Repeat("b", 5000)},
	}
	compactTurn(msgs, 6000)
	if !strings.Contains(msgs[1].Content, "elided") {
		t.Error("oldest tool output should be elided")
	}
	if !strings.HasPrefix(msgs[2].Content, "bbb") {
		t.Error("newest tool output must be kept")
	}
}

func TestRankSchemaTablesUsesColumnNames(t *testing.T) {
	tables := []schemaTable{{db: "a", name: "t1"}, {db: "a", name: "t2"}, {db: "a", name: "orders"}}
	cols := map[string][]string{"a.t2": {"revenue", "country"}}
	got := rankSchemaTables(tables, cols, "revenue by country")
	if got[0].name != "t2" {
		t.Errorf("column matches should rank t2 first, got %v", got)
	}
	got = rankSchemaTables(tables, cols, "orders by country")
	if got[0].name != "orders" {
		t.Errorf("a name match outweighs one column match, got %v", got)
	}
}

func TestPickRecommendedModelPrefersCurrentClaude(t *testing.T) {
	mk := func(names ...string) []database.BrainModel {
		var out []database.BrainModel
		for i, n := range names {
			out = append(out, database.BrainModel{ID: string(rune('a' + i)), Name: n})
		}
		return out
	}
	cases := []struct {
		models []string
		want   string
	}{
		{[]string{"gpt-5", "claude-haiku-5-5", "claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1"}, "claude-sonnet-5-5"},
		{[]string{"claude-fable-5-1", "claude-opus-5-5", "claude-haiku-5-5"}, "claude-opus-5-5"},
		{[]string{"claude-fable-5-1", "gpt-4o"}, "gpt-4o"},
		{[]string{"gpt-5-mini", "gpt-5"}, "gpt-5"},
		{[]string{"anthropic/claude-sonnet-5-5", "openai/gpt-5"}, "anthropic/claude-sonnet-5-5"},
	}
	for _, c := range cases {
		if got := pickRecommendedModel(mk(c.models...)); got == nil || got.Name != c.want {
			t.Errorf("%v: got %v, want %s", c.models, got, c.want)
		}
	}
}

func TestSSEStreamHeartbeat(t *testing.T) {
	rec := httptest.NewRecorder()
	s := newSSEStream(rec, rec)
	stop := s.startHeartbeat(context.Background(), 5*time.Millisecond)
	time.Sleep(30 * time.Millisecond)
	stop()
	_ = s.send(map[string]interface{}{"type": "done"})
	body := rec.Body.String()
	if !strings.Contains(body, ": ping\n\n") {
		t.Error("heartbeat should write SSE comments")
	}
	if !strings.HasSuffix(body, "data: {\"type\":\"done\"}\n\n") {
		t.Errorf("events must not interleave with pings: %q", body)
	}
}

// RunQueryArtifact applies the same governance policies as the editor,
// refuses a FORMAT clause and records the query in history.
func TestBrainRunQueryArtifactGuardrailsAndHistory(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1},{"x":2}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
	})
	store := governance.NewStore(db)
	if err := store.UpsertSyncState(conn, string(governance.SyncAccess), "idle", nil, nil, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CreatePolicy(conn, "Block hr", "", "table", "hr", "salaries", "", "analyst", "warn", "block", "admin"); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AppSecretKey: "k"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	enc, _ := crypto.Encrypt("pw", "k")
	session := &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: "u", EncryptedPassword: enc, UserRole: "viewer"}
	chatID, _ := db.CreateBrainChat(middleware.Actor(session), conn, "t", "", "", "", "", "")
	// No Guardrails wired: the handler builds one on its database.
	h := &BrainHandler{DB: db, Gateway: agent.Gateway, Config: cfg}
	r := chi.NewRouter()
	r.Post("/chats/{chatID}/artifacts/query", h.RunQueryArtifact)
	post := func(q string) *httptest.ResponseRecorder {
		body, _ := json.Marshal(map[string]any{"query": q})
		req := httptest.NewRequest(http.MethodPost, "/chats/"+chatID+"/artifacts/query", strings.NewReader(string(body)))
		req = req.WithContext(middleware.SetSession(req.Context(), session))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	if rr := post("SELECT * FROM hr.salaries"); rr.Code != http.StatusForbidden || !strings.Contains(rr.Body.String(), "policy_blocked") {
		t.Fatalf("want policy block, got %d %s", rr.Code, rr.Body.String())
	}
	for _, m := range agent.Messages() {
		if strings.Contains(m.SQL, "salaries") {
			t.Fatal("blocked query reached ClickHouse")
		}
	}
	if rr := post("SELECT 1 FORMAT CSV"); rr.Code != http.StatusBadRequest {
		t.Errorf("FORMAT: want 400, got %d", rr.Code)
	}
	if rr := post("SELECT x FROM analytics.events"); rr.Code != http.StatusOK {
		t.Fatalf("allowed query: %d %s", rr.Code, rr.Body.String())
	}
	hist, _ := db.GetQueryHistory("u", conn, "", "", 10, 0)
	if len(hist) != 1 || hist[0].Source != "brain" || hist[0].RowsReturned == nil || *hist[0].RowsReturned != 2 {
		t.Errorf("want one brain history row with 2 rows, got %+v", hist)
	}
}
