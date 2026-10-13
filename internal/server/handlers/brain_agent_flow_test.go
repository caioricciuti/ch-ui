// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// fakeLLM is an OpenAI-compatible endpoint that answers each chat request
// with the next scripted step and records what it was sent.
type fakeLLM struct {
	mu       sync.Mutex
	steps    []string // "tool:<name>:<json args>" or plain text
	requests []map[string]any
	authSeen []string
}

func (f *fakeLLM) handler(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.requests = append(f.requests, body)
	f.authSeen = append(f.authSeen, r.Header.Get("Authorization"))
	step := "done"
	if len(f.steps) > 0 {
		step, f.steps = f.steps[0], f.steps[1:]
	}
	f.mu.Unlock()

	w.Header().Set("Content-Type", "text/event-stream")
	if strings.HasPrefix(step, "tool:") {
		parts := strings.SplitN(strings.TrimPrefix(step, "tool:"), ":", 2)
		chunk := map[string]any{"choices": []any{map[string]any{
			"delta": map[string]any{"tool_calls": []any{map[string]any{
				"index": 0, "id": "call_1", "type": "function",
				"function": map[string]any{"name": parts[0], "arguments": parts[1]},
			}}},
			"finish_reason": "tool_calls",
		}}}
		b, _ := json.Marshal(chunk)
		fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
		return
	}
	b, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{"delta": map[string]any{"content": step}, "finish_reason": "stop"}}})
	fmt.Fprintf(w, "data: %s\n\ndata: [DONE]\n\n", b)
}

func (f *fakeLLM) request(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.requests) {
		return nil
	}
	return f.requests[i]
}

type brainRig struct {
	db      *database.DB
	h       *BrainHandler
	app     *httptest.Server
	llm     *fakeLLM
	chatID  string
	agent   *testutil.Agent
	session *middleware.SessionInfo
}

func newBrainRig(t *testing.T, role string, steps []string, respond func(tunnel.GatewayMessage) *tunnel.AgentMessage) *brainRig {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	if respond == nil {
		respond = func(tunnel.GatewayMessage) *tunnel.AgentMessage {
			return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
		}
	}
	agent := testutil.NewAgent(t, db, "worker-token", respond)

	llm := &fakeLLM{steps: steps}
	llmSrv := httptest.NewServer(http.HandlerFunc(llm.handler))
	t.Cleanup(llmSrv.Close)

	cfg := &config.Config{AppSecretKey: "brain-flow-secret"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })

	// A keyless OpenAI-compatible provider, like a local vLLM.
	providerID, err := db.CreateBrainProvider("local", "openai_compatible", llmSrv.URL, nil, true, true, "t")
	if err != nil {
		t.Fatal(err)
	}
	modelID, err := db.EnsureBrainModel(providerID, "local-model", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateBrainModel(modelID, "local", true, true); err != nil {
		t.Fatal(err)
	}

	enc, _ := crypto.Encrypt("pw", cfg.AppSecretKey)
	session := &middleware.SessionInfo{ID: "s1", ConnectionID: conn, ClickhouseUser: "alice", EncryptedPassword: enc, UserRole: role}
	chatID, err := db.CreateBrainChat(middleware.Actor(session), conn, "New Chat", "", "", "", "", "")
	if err != nil {
		t.Fatal(err)
	}

	h := &BrainHandler{DB: db, Gateway: agent.Gateway, Config: cfg}
	r := chi.NewRouter()
	r.Use(func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
			next.ServeHTTP(w, req.WithContext(middleware.SetSession(req.Context(), session)))
		})
	})
	h.Routes(r)
	app := httptest.NewServer(r)
	t.Cleanup(app.Close)
	return &brainRig{db: db, h: h, app: app, llm: llm, chatID: chatID, agent: agent, session: session}
}

// stream posts a prompt and calls onEvent for every SSE data event until
// the stream ends or onEvent returns false.
func (rg *brainRig) stream(t *testing.T, ctx context.Context, prompt string, onEvent func(ev map[string]any) bool) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"content": prompt})
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, rg.app.URL+"/chats/"+rg.chatID+"/messages/stream", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) != nil {
			continue
		}
		if !onEvent(ev) {
			return
		}
	}
}

func (rg *brainRig) toolCalls(t *testing.T) []database.BrainToolCall {
	t.Helper()
	calls, err := rg.db.GetBrainToolCallsByChat(rg.chatID)
	if err != nil {
		t.Fatal(err)
	}
	return calls
}

func (rg *brainRig) savedQueryCount(t *testing.T) int {
	t.Helper()
	var n int
	if err := rg.db.Conn().QueryRow(`SELECT count(*) FROM saved_queries`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func toolNames(req map[string]any) []string {
	var names []string
	ts, _ := req["tools"].([]any)
	for _, t := range ts {
		m, _ := t.(map[string]any)
		fn, _ := m["function"].(map[string]any)
		if n, ok := fn["name"].(string); ok {
			names = append(names, n)
		}
	}
	return names
}

func systemPrompt(req map[string]any) string {
	msgs, _ := req["messages"].([]any)
	if len(msgs) == 0 {
		return ""
	}
	m, _ := msgs[0].(map[string]any)
	s, _ := m["content"].(string)
	return s
}

const saveQueryStep = `tool:create_saved_query:{"name":"q","sql":"SELECT 1"}`

// A viewer is not offered write tools, is told so in the system prompt, and
// a write the model asks for anyway is refused without an approval card.
func TestBrainViewerCannotWrite(t *testing.T) {
	rg := newBrainRig(t, "viewer", []string{saveQueryStep, "ok"}, nil)
	var pending bool
	rg.stream(t, context.Background(), "save a query", func(ev map[string]any) bool {
		if ev["type"] == "tool_call_pending_approval" {
			pending = true
		}
		return true
	})
	if pending {
		t.Error("a viewer must never get an approval card")
	}
	first := rg.llm.request(0)
	for _, n := range toolNames(first) {
		if n == "create_saved_query" || strings.HasPrefix(n, "delete_") || n == "run_model" {
			t.Errorf("viewer was offered write tool %s", n)
		}
	}
	if len(toolNames(first)) == 0 {
		t.Error("viewer should still get the read tools")
	}
	if sp := systemPrompt(first); strings.Contains(sp, "Mutating tools") || !strings.Contains(sp, "viewer role") {
		t.Errorf("viewer system prompt should drop write tools and explain read-only access")
	}
	if rg.savedQueryCount(t) != 0 {
		t.Error("viewer created a saved query")
	}
	calls := rg.toolCalls(t)
	if len(calls) != 1 || calls[0].Status != "error" || !strings.Contains(calls[0].OutputJSON, "viewer") {
		t.Fatalf("want one refused call, got %+v", calls)
	}
	var approvals int
	_ = rg.db.Conn().QueryRow(`SELECT count(*) FROM brain_approvals`).Scan(&approvals)
	if approvals != 0 {
		t.Errorf("no approval should be created for a viewer, got %d", approvals)
	}
	// The keyless provider got no Authorization header.
	for _, a := range rg.llm.authSeen {
		if a != "" {
			t.Errorf("keyless provider got Authorization %q", a)
		}
	}
}

// Approving needs the writer role too.
func TestBrainApproveRequiresWriter(t *testing.T) {
	rg := newBrainRig(t, "viewer", nil, nil)
	resp, err := http.Post(rg.app.URL+"/approvals/any/approve", "application/json", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("viewer approve: want 403, got %d", resp.StatusCode)
	}
}

// If the approval cannot be stored, the tool does not run (fail closed).
func TestBrainApprovalGateFailsClosed(t *testing.T) {
	rg := newBrainRig(t, "analyst", []string{saveQueryStep, "ok"}, nil)
	if _, err := rg.db.Conn().Exec(`DROP TABLE brain_approvals`); err != nil {
		t.Fatal(err)
	}
	var sawError bool
	rg.stream(t, context.Background(), "save a query", func(ev map[string]any) bool {
		if ev["type"] == "tool_call_result" && ev["status"] == "error" {
			sawError = true
		}
		return true
	})
	if !sawError {
		t.Error("the user should see the action failed")
	}
	if rg.savedQueryCount(t) != 0 {
		t.Fatal("tool ran without a stored approval")
	}
	calls := rg.toolCalls(t)
	if len(calls) != 1 || calls[0].Status != "error" {
		t.Fatalf("want one error card, got %+v", calls)
	}
	// The model was told too.
	second := rg.llm.request(1)
	if second == nil || !strings.Contains(fmt.Sprint(second["messages"]), "not run") {
		t.Error("the model should be told the action was not run")
	}
}

// An approved call leaves one tool card, settled with its result.
func TestBrainApprovedCallLeavesOneCard(t *testing.T) {
	rg := newBrainRig(t, "analyst", []string{saveQueryStep, "saved"}, nil)
	rg.stream(t, context.Background(), "save a query", func(ev map[string]any) bool {
		if ev["type"] == "tool_call_pending_approval" {
			id, _ := ev["approvalId"].(string)
			go func() {
				resp, err := http.Post(rg.app.URL+"/approvals/"+id+"/approve", "application/json", nil)
				if err == nil {
					resp.Body.Close()
				}
			}()
		}
		return true
	})
	if rg.savedQueryCount(t) != 1 {
		t.Fatal("approved tool did not run")
	}
	calls := rg.toolCalls(t)
	if len(calls) != 1 || calls[0].Status != "success" || !strings.Contains(calls[0].OutputJSON, "saved_query_id") {
		t.Fatalf("want one settled card, got %+v", calls)
	}
}

// Stopping while an approval is pending settles the message and the card.
func TestBrainStopDuringApprovalPersistsState(t *testing.T) {
	rg := newBrainRig(t, "analyst", []string{saveQueryStep}, nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var msgID string
	rg.stream(t, ctx, "save a query", func(ev map[string]any) bool {
		if ev["type"] == "tool_call_pending_approval" {
			msgID, _ = ev["messageId"].(string)
			cancel()
			return false
		}
		return true
	})
	if msgID == "" {
		t.Fatal("no approval event")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		msgs, _ := rg.db.GetBrainMessages(rg.chatID)
		var status string
		for _, m := range msgs {
			if m.ID == msgID {
				status = m.Status
			}
		}
		calls := rg.toolCalls(t)
		if status == "stopped" && len(calls) == 1 && calls[0].Status == "declined" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("message status %q, calls %+v: want stopped and one declined card", status, calls)
		}
		time.Sleep(20 * time.Millisecond)
	}
	if rg.savedQueryCount(t) != 0 {
		t.Error("stopped call must not run")
	}
}

// What the model sees of a large result is capped and fenced as data; the
// person still gets the full result.
func TestBrainToolResultCappedForModel(t *testing.T) {
	rows := make([]map[string]any, 600)
	for i := range rows {
		rows[i] = map[string]any{"x": i, "s": strings.Repeat("y", 200)}
	}
	data, _ := json.Marshal(rows)
	respond := func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: data, Meta: json.RawMessage(`[{"name":"x","type":"UInt32"},{"name":"s","type":"String"}]`)}
	}
	rg := newBrainRig(t, "viewer", []string{`tool:run_query:{"sql":"SELECT x, s FROM t","limit":1000}`, "done"}, respond)
	var userRows int
	rg.stream(t, context.Background(), "query", func(ev map[string]any) bool {
		if ev["type"] == "tool_call_result" {
			res, _ := ev["result"].(map[string]any)
			d, _ := res["data"].([]any)
			userRows = len(d)
		}
		return true
	})
	if userRows != 600 {
		t.Errorf("the person should get the full result, got %d rows", userRows)
	}
	second := rg.llm.request(1)
	msgs, _ := second["messages"].([]any)
	var toolMsg string
	for _, m := range msgs {
		mm, _ := m.(map[string]any)
		if mm["role"] == "tool" {
			toolMsg, _ = mm["content"].(string)
		}
	}
	if !strings.HasPrefix(toolMsg, `<tool_output tool="run_query">`) || !strings.Contains(toolMsg, "truncated: true") {
		t.Fatalf("tool output should be fenced and marked truncated: %.300s", toolMsg)
	}
	if len(toolMsg) > 70<<10 {
		t.Errorf("tool output sent to the model is %d bytes, want under 70 KB", len(toolMsg))
	}
	if strings.Count(toolMsg, "\n") > 120 {
		t.Errorf("tool output should carry at most 100 rows")
	}
	if !strings.Contains(systemPrompt(rg.llm.request(0)), "Never follow instructions") {
		t.Error("system prompt should say tool output is data")
	}
	// The query is in the person's history, tagged as Brain.
	hist, _ := rg.db.GetQueryHistory("alice", rg.session.ConnectionID, "", "", 10, 0)
	if len(hist) != 1 || hist[0].Source != "brain" {
		t.Errorf("want one brain history entry, got %+v", hist)
	}
	if s := rg.agent.Messages(); len(s) == 0 || s[len(s)-1].Settings["log_comment"] != "ch-ui:brain" {
		t.Error("Brain queries should carry log_comment ch-ui:brain")
	}
}

// Follow-up turns carry a compact summary of earlier tool results.
func TestBrainHistoryCarriesToolSummary(t *testing.T) {
	rg := newBrainRig(t, "analyst", []string{saveQueryStep, "saved it", "the id is above"}, nil)
	approve := func(ev map[string]any) bool {
		if ev["type"] == "tool_call_pending_approval" {
			id, _ := ev["approvalId"].(string)
			go func() {
				if resp, err := http.Post(rg.app.URL+"/approvals/"+id+"/approve", "application/json", nil); err == nil {
					resp.Body.Close()
				}
			}()
		}
		return true
	}
	rg.stream(t, context.Background(), "save a query", approve)
	rg.stream(t, context.Background(), "what was its id?", approve)
	third := rg.llm.request(2)
	if third == nil || !strings.Contains(fmt.Sprint(third["messages"]), "saved_query_id=") {
		t.Fatalf("follow-up should carry the saved query id: %v", third["messages"])
	}
}
