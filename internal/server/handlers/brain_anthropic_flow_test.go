package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// fakeClaude is a scripted Anthropic Messages endpoint: each request gets
// the next list of SSE events.
type fakeClaude struct {
	mu       sync.Mutex
	replies  [][]map[string]any
	requests []map[string]any
}

func (f *fakeClaude) handler(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	f.mu.Lock()
	f.requests = append(f.requests, body)
	events := claudeText("done", "end_turn", 1, 1)
	if len(f.replies) > 0 {
		events, f.replies = f.replies[0], f.replies[1:]
	}
	f.mu.Unlock()
	w.Header().Set("Content-Type", "text/event-stream")
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev["type"], b)
	}
}

func (f *fakeClaude) request(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	if i >= len(f.requests) {
		return nil
	}
	return f.requests[i]
}

func claudeStart(input, cacheRead, cacheWrite int) map[string]any {
	return map[string]any{"type": "message_start", "message": map[string]any{"usage": map[string]any{
		"input_tokens": input, "output_tokens": 1, "cache_read_input_tokens": cacheRead, "cache_creation_input_tokens": cacheWrite,
	}}}
}

func claudeText(text, stop string, input, output int) []map[string]any {
	return []map[string]any{
		claudeStart(input, 0, 0),
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "text", "text": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "text_delta", "text": text}},
		{"type": "content_block_stop", "index": 0},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": stop}, "usage": map[string]any{"output_tokens": output}},
		{"type": "message_stop"},
	}
}

func claudeToolUse(id, name, args string) []map[string]any {
	return []map[string]any{
		claudeStart(100, 0, 2000),
		{"type": "content_block_start", "index": 0, "content_block": map[string]any{"type": "thinking", "thinking": "", "signature": ""}},
		{"type": "content_block_delta", "index": 0, "delta": map[string]any{"type": "signature_delta", "signature": "sig-1"}},
		{"type": "content_block_stop", "index": 0},
		{"type": "content_block_start", "index": 1, "content_block": map[string]any{"type": "tool_use", "id": id, "name": name, "input": map[string]any{}}},
		{"type": "content_block_delta", "index": 1, "delta": map[string]any{"type": "input_json_delta", "partial_json": args}},
		{"type": "content_block_stop", "index": 1},
		{"type": "message_delta", "delta": map[string]any{"stop_reason": "tool_use"}, "usage": map[string]any{"output_tokens": 40}},
		{"type": "message_stop"},
	}
}

// newClaudeRig is the Brain rig with an Anthropic provider chosen as the
// agent model in settings.
func newClaudeRig(t *testing.T, replies ...[]map[string]any) (*brainRig, *fakeClaude) {
	t.Helper()
	rg := newBrainRig(t, "admin", nil, nil)
	fc := &fakeClaude{replies: replies}
	srv := httptest.NewServer(http.HandlerFunc(fc.handler))
	t.Cleanup(srv.Close)

	key, err := crypto.Encrypt("sk-ant-test", rg.h.Config.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	providerID, err := rg.db.CreateBrainProvider("Claude", "anthropic", srv.URL, &key, true, false, "t")
	if err != nil {
		t.Fatal(err)
	}
	modelID, err := rg.db.EnsureBrainModel(providerID, "claude-sonnet-5-5", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := rg.db.UpdateBrainModel(modelID, "Sonnet", true, true); err != nil {
		t.Fatal(err)
	}
	if err := rg.db.SetSetting(settingBrainAgentModel, modelID); err != nil {
		t.Fatal(err)
	}
	return rg, fc
}

func (rg *brainRig) runTurn(t *testing.T, prompt string) (done map[string]any, text string) {
	t.Helper()
	var sb strings.Builder
	rg.stream(t, context.Background(), prompt, func(ev map[string]any) bool {
		switch ev["type"] {
		case "delta":
			sb.WriteString(ev["delta"].(string))
		case "done":
			done = ev
			return false
		case "error":
			t.Fatalf("stream error: %v", ev["error"])
		}
		return true
	})
	return done, sb.String()
}

// A multi-step tool turn on Anthropic replays the thinking and tool_use
// blocks, answers with a tool_result, and sums the usage of both calls on
// the message, in the ledger and in the done event.
func TestBrainAnthropicToolLoopAndUsage(t *testing.T) {
	rg, fc := newClaudeRig(t,
		claudeToolUse("toolu_1", "list_dashboards", `{}`),
		append([]map[string]any{claudeStart(30, 2000, 0)}, claudeText("All set.", "end_turn", 0, 12)[1:]...),
	)

	done, text := rg.runTurn(t, "what dashboards exist?")
	if text != "All set." {
		t.Fatalf("text %q", text)
	}
	second := fc.request(1)
	if second == nil {
		t.Fatal("expected a second request after the tool ran")
	}
	msgs := second["messages"].([]any)
	if len(msgs) != 3 {
		t.Fatalf("messages %v", msgs)
	}
	asst := msgs[1].(map[string]any)["content"].([]any)
	if asst[0].(map[string]any)["signature"] != "sig-1" || asst[1].(map[string]any)["type"] != "tool_use" {
		t.Errorf("assistant blocks must be replayed unchanged: %v", asst)
	}
	result := msgs[2].(map[string]any)["content"].([]any)[0].(map[string]any)
	if result["type"] != "tool_result" || result["tool_use_id"] != "toolu_1" {
		t.Errorf("tool result %v", result)
	}
	if _, ok := second["temperature"]; ok {
		t.Error("no temperature for Claude")
	}

	usage, _ := done["usage"].(map[string]any)
	if usage["model"] != "claude-sonnet-5-5" || usage["input_tokens"] != float64(130) || usage["output_tokens"] != float64(52) ||
		usage["cache_read_tokens"] != float64(2000) || usage["cache_write_tokens"] != float64(2000) {
		t.Fatalf("done usage %v", usage)
	}
	if _, ok := usage["cost_usd"]; !ok {
		t.Error("default prices give a cost for Sonnet 5.5")
	}

	msgsDB, err := rg.db.GetBrainMessages(rg.chatID)
	if err != nil {
		t.Fatal(err)
	}
	last := msgsDB[len(msgsDB)-1]
	if last.Model == nil || *last.Model != "claude-sonnet-5-5" || last.InputTokens != 130 || last.CacheReadTokens != 2000 {
		t.Errorf("stored message usage %+v", last)
	}
	var rows, in int
	var purpose string
	if err := rg.db.Conn().QueryRow(`SELECT count(*), max(purpose), sum(input_tokens) FROM brain_usage`).Scan(&rows, &purpose, &in); err != nil {
		t.Fatal(err)
	}
	if rows != 1 || purpose != "chat" || in != 130 {
		t.Errorf("ledger rows=%d purpose=%s input=%d", rows, purpose, in)
	}
}

// A turn cut at max_tokens tells the person, and the daily budget blocks
// the next turn before anything is sent.
func TestBrainAnthropicMaxTokensNoteAndBudget(t *testing.T) {
	rg, fc := newClaudeRig(t, claudeText("Partial answer", "max_tokens", 50, 60))
	if err := rg.db.SetSetting(settingBrainDailyBudget, "100"); err != nil {
		t.Fatal(err)
	}
	_, text := rg.runTurn(t, "write a lot")
	if !strings.Contains(text, "Partial answer") || !strings.Contains(text, "cut off") {
		t.Fatalf("text %q", text)
	}

	body := strings.NewReader(`{"content":"again"}`)
	resp, err := http.Post(rg.app.URL+"/chats/"+rg.chatID+"/messages/stream", "application/json", body)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	if resp.StatusCode != http.StatusTooManyRequests || !strings.Contains(fmt.Sprint(out["error"]), "daily Brain budget") {
		t.Fatalf("status %d body %v", resp.StatusCode, out)
	}
	if fc.request(1) != nil {
		t.Error("no provider call once the budget is used up")
	}
}

// Ask AI streams the SQL, uses the quick model and lands in the ledger.
func TestBrainAskAIStreamsWithQuickModel(t *testing.T) {
	rg, fc := newClaudeRig(t, claudeText("```sql\nSELECT 1\n```", "end_turn", 40, 8))
	providerID := ""
	if err := rg.db.Conn().QueryRow(`SELECT id FROM brain_providers WHERE kind = 'anthropic'`).Scan(&providerID); err != nil {
		t.Fatal(err)
	}
	quickID, err := rg.db.EnsureBrainModel(providerID, "claude-haiku-5-5", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := rg.db.SetBrainModelActive(quickID, true); err != nil {
		t.Fatal(err)
	}
	if err := rg.db.SetSetting(settingBrainQuickModel, quickID); err != nil {
		t.Fatal(err)
	}

	resp, err := http.Post(rg.app.URL+"/generate-sql/stream", "application/json", strings.NewReader(`{"question":"one"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	raw := new(strings.Builder)
	buf := make([]byte, 4096)
	for {
		n, rerr := resp.Body.Read(buf)
		raw.Write(buf[:n])
		if rerr != nil {
			break
		}
	}
	var done map[string]any
	for _, line := range strings.Split(raw.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev map[string]any
		if json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev) == nil && ev["type"] == "done" {
			done = ev
		}
	}
	if done == nil || done["sql"] != "SELECT 1" || done["model"] != "claude-haiku-5-5" {
		t.Fatalf("done %v (stream %q)", done, raw.String())
	}
	if fc.request(0)["model"] != "claude-haiku-5-5" {
		t.Errorf("Ask AI should use the quick model, sent %v", fc.request(0)["model"])
	}
	var purpose string
	var in int
	if err := rg.db.Conn().QueryRow(`SELECT purpose, input_tokens FROM brain_usage`).Scan(&purpose, &in); err != nil {
		t.Fatal(err)
	}
	if purpose != "ask_ai" || in != 40 {
		t.Errorf("ledger %s %d", purpose, in)
	}
}
