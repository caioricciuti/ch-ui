package brain

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/brain/tools"
)

// sse writes Anthropic-style SSE events: an event line and a data line.
func sse(w http.ResponseWriter, events ...map[string]any) {
	w.Header().Set("Content-Type", "text/event-stream")
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev["type"], b)
	}
}

func msgStart(input, cacheRead, cacheWrite int) map[string]any {
	return map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_1", "type": "message", "role": "assistant", "content": []any{},
		"usage": map[string]any{"input_tokens": input, "output_tokens": 1, "cache_read_input_tokens": cacheRead, "cache_creation_input_tokens": cacheWrite},
	}}
}

func blockStart(i int, block map[string]any) map[string]any {
	return map[string]any{"type": "content_block_start", "index": i, "content_block": block}
}

func delta(i int, d map[string]any) map[string]any {
	return map[string]any{"type": "content_block_delta", "index": i, "delta": d}
}

func blockStop(i int) map[string]any { return map[string]any{"type": "content_block_stop", "index": i} }

func msgDelta(stop string, output int) map[string]any {
	return map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": stop}, "usage": map[string]any{"output_tokens": output}}
}

var msgStop = map[string]any{"type": "message_stop"}
var ping = map[string]any{"type": "ping"}

func textReply(text string, stop string) []map[string]any {
	return []map[string]any{
		msgStart(10, 0, 0),
		blockStart(0, map[string]any{"type": "text", "text": ""}),
		delta(0, map[string]any{"type": "text_delta", "text": text}),
		blockStop(0),
		msgDelta(stop, 4),
		msgStop,
	}
}

// fakeAnthropic answers each request with the next scripted reply and
// records the requests.
type fakeAnthropic struct {
	mu       sync.Mutex
	replies  []func(w http.ResponseWriter)
	requests []map[string]any
	headers  []http.Header
}

func (f *fakeAnthropic) handler(t *testing.T) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.requests = append(f.requests, body)
		f.headers = append(f.headers, r.Header.Clone())
		var reply func(w http.ResponseWriter)
		if len(f.replies) > 0 {
			reply, f.replies = f.replies[0], f.replies[1:]
		}
		f.mu.Unlock()
		if reply == nil {
			sse(w, textReply("done", "end_turn")...)
			return
		}
		reply(w)
	}
}

func (f *fakeAnthropic) request(i int) map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.requests[i]
}

func (f *fakeAnthropic) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.requests)
}

func newFakeAnthropic(t *testing.T, replies ...func(w http.ResponseWriter)) (*fakeAnthropic, *anthropicProvider, ProviderConfig) {
	t.Helper()
	f := &fakeAnthropic{replies: replies}
	srv := httptest.NewServer(f.handler(t))
	t.Cleanup(srv.Close)
	p := &anthropicProvider{client: srv.Client(), wait: func(context.Context, time.Duration) error { return nil }}
	return f, p, ProviderConfig{Kind: "anthropic", BaseURL: srv.URL, APIKey: "sk-ant-test"}
}

func reply(events ...map[string]any) func(w http.ResponseWriter) {
	return func(w http.ResponseWriter) { sse(w, events...) }
}

func TestAnthropicStreamsTextWithCachingAndUsage(t *testing.T) {
	t.Parallel()
	f, p, cfg := newFakeAnthropic(t, reply(
		msgStart(12, 300, 40),
		ping,
		blockStart(0, map[string]any{"type": "text", "text": ""}),
		delta(0, map[string]any{"type": "text_delta", "text": "Hello"}),
		delta(0, map[string]any{"type": "text_delta", "text": " world"}),
		blockStop(0),
		msgDelta("end_turn", 9),
		msgStop,
	))

	var got strings.Builder
	res, err := p.StreamChat(context.Background(), cfg, "claude-sonnet-5-5", []Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hi"},
	}, func(d string) error { got.WriteString(d); return nil })
	if err != nil {
		t.Fatal(err)
	}
	if got.String() != "Hello world" {
		t.Fatalf("streamed %q", got.String())
	}
	if res.InputTokens != 12 || res.OutputTokens != 9 || res.CacheReadTokens != 300 || res.CacheWriteTokens != 40 {
		t.Fatalf("usage %+v", res)
	}

	f.mu.Lock()
	h := f.headers[0]
	f.mu.Unlock()
	if h.Get("x-api-key") != "sk-ant-test" || h.Get("anthropic-version") != "2023-06-01" || !strings.Contains(h.Get("content-type"), "application/json") {
		t.Fatalf("headers %v", h)
	}
	req := f.request(0)
	for _, k := range []string{"temperature", "top_p", "tool_choice", "tools"} {
		if _, ok := req[k]; ok {
			t.Errorf("request must not send %s", k)
		}
	}
	if req["stream"] != true || req["max_tokens"] != float64(16384) || req["model"] != "claude-sonnet-5-5" {
		t.Errorf("request %v", req)
	}
	sys, _ := req["system"].([]any)
	if len(sys) != 1 {
		t.Fatalf("system %v", req["system"])
	}
	block := sys[0].(map[string]any)
	if block["text"] != "be brief" || block["cache_control"].(map[string]any)["type"] != "ephemeral" {
		t.Errorf("system block %v", block)
	}
	msgs := req["messages"].([]any)
	if len(msgs) != 1 || msgs[0].(map[string]any)["role"] != "user" {
		t.Errorf("messages %v", msgs)
	}
}

func TestAnthropicToolUseWithInputJSONDelta(t *testing.T) {
	t.Parallel()
	f, p, cfg := newFakeAnthropic(t, reply(
		msgStart(50, 0, 0),
		blockStart(0, map[string]any{"type": "thinking", "thinking": "", "signature": ""}),
		delta(0, map[string]any{"type": "thinking_delta", "thinking": "need tables"}),
		delta(0, map[string]any{"type": "signature_delta", "signature": "sig-abc"}),
		blockStop(0),
		blockStart(1, map[string]any{"type": "text", "text": ""}),
		delta(1, map[string]any{"type": "text_delta", "text": "Looking."}),
		blockStop(1),
		blockStart(2, map[string]any{"type": "tool_use", "id": "toolu_1", "name": "describe_table", "input": map[string]any{}}),
		delta(2, map[string]any{"type": "input_json_delta", "partial_json": ""}),
		delta(2, map[string]any{"type": "input_json_delta", "partial_json": `{"database":"de`}),
		delta(2, map[string]any{"type": "input_json_delta", "partial_json": `fault","table":"t"}`}),
		blockStop(2),
		msgDelta("tool_use", 30),
		msgStop,
	))

	defs := []tools.Definition{
		{Type: "function", Function: tools.FunctionDef{Name: "list_tables", Description: "List", Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}},
		{Type: "function", Function: tools.FunctionDef{Name: "describe_table", Description: "Describe", Parameters: json.RawMessage(`{"type":"object","properties":{"database":{"type":"string"}}}`)}},
	}
	res, err := p.StreamChatTools(context.Background(), cfg, "claude-sonnet-5-5", []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "describe t"},
	}, defs, func(string) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if res.FinishReason != "tool_calls" || len(res.ToolCalls) != 1 {
		t.Fatalf("result %+v", res)
	}
	tc := res.ToolCalls[0]
	if tc.ID != "toolu_1" || tc.Function.Name != "describe_table" || tc.Function.Arguments != `{"database":"default","table":"t"}` {
		t.Fatalf("tool call %+v", tc)
	}
	if res.Content != "Looking." {
		t.Errorf("content %q", res.Content)
	}
	var blocks []map[string]any
	if err := json.Unmarshal(res.ProviderBlocks, &blocks); err != nil || len(blocks) != 3 {
		t.Fatalf("provider blocks %s", res.ProviderBlocks)
	}
	if blocks[0]["type"] != "thinking" || blocks[0]["signature"] != "sig-abc" || blocks[0]["thinking"] != "need tables" {
		t.Errorf("thinking block %v", blocks[0])
	}
	if in, _ := blocks[2]["input"].(map[string]any); in["table"] != "t" {
		t.Errorf("tool_use input %v", blocks[2])
	}

	req := f.request(0)
	if tcj, _ := req["tool_choice"].(map[string]any); tcj["type"] != "auto" {
		t.Errorf("tool_choice %v", req["tool_choice"])
	}
	ts := req["tools"].([]any)
	first, last := ts[0].(map[string]any), ts[1].(map[string]any)
	if _, ok := first["cache_control"]; ok {
		t.Error("only the last tool carries cache_control")
	}
	if last["cache_control"] == nil || last["input_schema"] == nil || last["name"] != "describe_table" {
		t.Errorf("last tool %v", last)
	}
}

func TestAnthropicRequestReplaysBlocksAndGroupsToolResults(t *testing.T) {
	t.Parallel()
	blocks := json.RawMessage(`[{"type":"thinking","thinking":"","signature":"s1"},{"type":"tool_use","id":"a","name":"x","input":{}},{"type":"tool_use","id":"b","name":"y","input":{}}]`)
	req := buildAnthropicRequest("claude-haiku-5-5", 0, []ChatMessage{
		{Role: "system", Content: "sys"},
		{Role: "user", Content: "earlier"},
		{Role: "assistant", Content: "earlier answer"},
		{Role: "user", Content: "now"},
		{Role: "assistant", ProviderBlocks: blocks, ToolCalls: []ToolCallEmit{{ID: "a"}, {ID: "b"}}},
		{Role: "tool", ToolCallID: "a", Content: `{"ok":1}`},
		{Role: "tool", ToolCallID: "b", Content: `{"error":"boom"}`, IsError: true},
	}, nil)
	if req.MaxTokens != 8192 {
		t.Errorf("haiku default max_tokens %d", req.MaxTokens)
	}
	if len(req.Messages) != 5 {
		t.Fatalf("messages %+v", req.Messages)
	}
	asst := req.Messages[3]
	if asst.Role != "assistant" || asst.Content[0]["signature"] != "s1" || len(asst.Content) != 3 {
		t.Fatalf("assistant replay %+v", asst)
	}
	results := req.Messages[4]
	if results.Role != "user" || len(results.Content) != 2 {
		t.Fatalf("tool results must share one user message: %+v", results)
	}
	if _, ok := results.Content[0]["is_error"]; ok {
		t.Error("a successful result has no is_error")
	}
	if results.Content[1]["is_error"] != true || results.Content[1]["tool_use_id"] != "b" {
		t.Errorf("failed result %v", results.Content[1])
	}
	if results.Content[1]["cache_control"] == nil {
		t.Error("the newest block carries a cache breakpoint")
	}
	if req.Messages[0].Content[0]["cache_control"] != nil {
		t.Error("only the newest message carries the message breakpoint")
	}
}

func TestAnthropicRetriesRateLimitAndOverloaded(t *testing.T) {
	t.Parallel()
	f, p, cfg := newFakeAnthropic(t,
		func(w http.ResponseWriter) {
			w.Header().Set("retry-after", "7")
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"type":"error","error":{"type":"rate_limit_error","message":"slow down"}}`)
		},
		func(w http.ResponseWriter) {
			w.WriteHeader(529)
			fmt.Fprint(w, `{"type":"error","error":{"type":"overloaded_error","message":"Overloaded"}}`)
		},
		reply(textReply("ok", "end_turn")...),
	)
	var waits []time.Duration
	p.wait = func(_ context.Context, d time.Duration) error { waits = append(waits, d); return nil }

	var got strings.Builder
	if _, err := p.StreamChat(context.Background(), cfg, "claude-sonnet-5-5", []Message{{Role: "user", Content: "hi"}}, func(d string) error { got.WriteString(d); return nil }); err != nil {
		t.Fatal(err)
	}
	if got.String() != "ok" || f.count() != 3 {
		t.Fatalf("got %q after %d requests", got.String(), f.count())
	}
	if len(waits) != 2 || waits[0] != 7*time.Second || waits[1] != 2*time.Second {
		t.Errorf("waits %v: want retry-after first, then backoff", waits)
	}
}

func TestAnthropicSurfacesAPIErrors(t *testing.T) {
	t.Parallel()
	_, p, cfg := newFakeAnthropic(t, func(w http.ResponseWriter) {
		w.Header().Set("request-id", "req_9")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"max_tokens: too large"}}`)
	})
	_, err := p.StreamChat(context.Background(), cfg, "claude-sonnet-5-5", []Message{{Role: "user", Content: "hi"}}, func(string) error { return nil })
	var ae *AnthropicError
	if !errors.As(err, &ae) || ae.Status != 400 || ae.Type != "invalid_request_error" {
		t.Fatalf("err %v", err)
	}
	if !strings.Contains(err.Error(), "max_tokens: too large") || !strings.Contains(err.Error(), "req_9") {
		t.Errorf("message %q", err.Error())
	}

	_, p2, cfg2 := newFakeAnthropic(t)
	cfg2.APIKey = ""
	if _, err := p2.StreamChat(context.Background(), cfg2, "m", []Message{{Role: "user", Content: "hi"}}, func(string) error { return nil }); err == nil {
		t.Error("a missing key must fail before any request")
	}
}

func TestAnthropicErrorEventMidStream(t *testing.T) {
	t.Parallel()
	_, p, cfg := newFakeAnthropic(t, reply(
		msgStart(20, 5, 0),
		blockStart(0, map[string]any{"type": "text", "text": ""}),
		delta(0, map[string]any{"type": "text_delta", "text": "partial"}),
		map[string]any{"type": "error", "error": map[string]any{"type": "overloaded_error", "message": "Overloaded"}},
	))
	res, err := p.StreamChatTools(context.Background(), cfg, "claude-sonnet-5-5", []ChatMessage{{Role: "user", Content: "hi"}}, nil, func(string) error { return nil })
	var ae *AnthropicError
	if !errors.As(err, &ae) || ae.Type != "overloaded_error" || !strings.Contains(err.Error(), "Overloaded") {
		t.Fatalf("err %v", err)
	}
	if res == nil || res.InputTokens != 20 || res.CacheReadTokens != 5 || res.Content != "partial" {
		t.Errorf("partial result %+v", res)
	}
}

func TestAnthropicMaxTokensAndRefusal(t *testing.T) {
	t.Parallel()
	_, p, cfg := newFakeAnthropic(t,
		reply(textReply("cut", "max_tokens")...),
		reply(textReply("", "refusal")...),
	)
	cfg.MaxTokens = 2048
	res, err := p.StreamChat(context.Background(), cfg, "claude-sonnet-5-5", []Message{{Role: "user", Content: "long"}}, func(string) error { return nil })
	if err != nil || res.StopReason != "length" {
		t.Fatalf("max_tokens: %+v %v", res, err)
	}
	tr, err := p.StreamChatTools(context.Background(), cfg, "claude-sonnet-5-5", []ChatMessage{{Role: "user", Content: "x"}}, nil, func(string) error { return nil })
	if err != nil || tr.FinishReason != "refusal" || tr.StopReason != "refusal" {
		t.Fatalf("refusal: %+v %v", tr, err)
	}
}

func TestAnthropicMaxTokensSetting(t *testing.T) {
	t.Parallel()
	f, p, cfg := newFakeAnthropic(t)
	cfg.MaxTokens = 2048
	if _, err := p.StreamChat(context.Background(), cfg, "claude-sonnet-5-5", []Message{{Role: "user", Content: "x"}}, func(string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	if f.request(0)["max_tokens"] != float64(2048) {
		t.Errorf("max_tokens %v", f.request(0)["max_tokens"])
	}
}

func TestAnthropicStripsThinkingWhenReplayIsRejected(t *testing.T) {
	t.Parallel()
	f, p, cfg := newFakeAnthropic(t,
		func(w http.ResponseWriter) {
			w.WriteHeader(http.StatusBadRequest)
			fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"messages.1.content.0: Invalid `+"`signature`"+` in `+"`thinking`"+` block."}}`)
		},
		reply(textReply("fine", "end_turn")...),
	)
	msgs := []ChatMessage{
		{Role: "user", Content: "q"},
		{Role: "assistant", ProviderBlocks: json.RawMessage(`[{"type":"thinking","thinking":"","signature":"old"},{"type":"tool_use","id":"t1","name":"x","input":{}}]`)},
		{Role: "tool", ToolCallID: "t1", Content: "r"},
	}
	res, err := p.StreamChatTools(context.Background(), cfg, "claude-sonnet-5-5", msgs, nil, func(string) error { return nil })
	if err != nil || res.Content != "fine" {
		t.Fatalf("%+v %v", res, err)
	}
	retry := f.request(1)["messages"].([]any)[1].(map[string]any)["content"].([]any)
	if len(retry) != 1 || retry[0].(map[string]any)["type"] != "tool_use" {
		t.Errorf("retry should drop the thinking block and keep tool_use: %v", retry)
	}
}

func TestAnthropicContinuesPausedTurn(t *testing.T) {
	t.Parallel()
	f, p, cfg := newFakeAnthropic(t,
		reply(textReply("part one ", "pause_turn")...),
		reply(textReply("part two", "end_turn")...),
	)
	res, err := p.StreamChatTools(context.Background(), cfg, "claude-sonnet-5-5", []ChatMessage{{Role: "user", Content: "go"}}, nil, func(string) error { return nil })
	if err != nil || res.Content != "part one part two" || res.FinishReason != "stop" {
		t.Fatalf("%+v %v", res, err)
	}
	if res.InputTokens != 20 || res.OutputTokens != 8 {
		t.Errorf("usage across continuations %+v", res.ChatResult)
	}
	second := f.request(1)["messages"].([]any)
	if len(second) != 2 || second[1].(map[string]any)["role"] != "assistant" {
		t.Errorf("continuation must resend the paused content: %v", second)
	}
}

func TestAnthropicListModels(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" || r.Header.Get("x-api-key") != "k" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		fmt.Fprint(w, `{"data":[{"id":"claude-sonnet-5-5"},{"id":"claude-haiku-5-5"}]}`)
	}))
	defer srv.Close()
	p := newAnthropicProvider(srv.Client())
	models, err := p.ListModels(context.Background(), ProviderConfig{BaseURL: srv.URL + "/v1", APIKey: "k"})
	if err != nil || len(models) != 2 || models[0] != "claude-sonnet-5-5" {
		t.Fatalf("%v %v", models, err)
	}
	// A proxy without the models endpoint gets the known Claude models.
	models, err = p.ListModels(context.Background(), ProviderConfig{BaseURL: srv.URL + "/proxy", APIKey: "k"})
	if err != nil || len(models) != len(AnthropicModels) {
		t.Fatalf("fallback %v %v", models, err)
	}
}

func TestPriceTableCost(t *testing.T) {
	t.Parallel()
	prices := DefaultPrices()
	cost, ok := prices.Cost("claude-sonnet-5-5", ChatResult{InputTokens: 1_000_000, OutputTokens: 100_000, CacheReadTokens: 1_000_000, CacheWriteTokens: 0}.PriceUsage())
	if !ok || cost < 3.199 || cost > 3.201 {
		t.Fatalf("cost %v %v", cost, ok)
	}
	if _, ok := prices.Lookup("anthropic/claude-haiku-5-5"); !ok {
		t.Error("prefixed model ids should match")
	}
	if _, ok := prices.Cost("gpt-x", ChatResult{InputTokens: 1}.PriceUsage()); ok {
		t.Error("unknown model has no cost")
	}
	if ParsePriceTable("not json") != nil {
		t.Error("invalid JSON parses to nil")
	}
}
