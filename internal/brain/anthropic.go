package brain

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/brain/tools"
)

// Native Anthropic Messages API provider (kind "anthropic"), written on
// net/http like the other providers.
//
// What it sends: the system prompt as a top-level block and the tool list,
// each with an ephemeral cache_control marker so the stable prefix (tools,
// then system) is cached, plus one marker on the newest message so the
// earlier iterations of an agent turn are read from cache too. It never
// sends temperature or top_p (current Claude models reject non-default
// sampling) and only ever uses tool_choice auto.

const (
	anthropicDefaultBaseURL = "https://api.anthropic.com"
	anthropicVersion        = "2023-06-01"
	// anthropicMaxRetries bounds retries on 429 and 529 (overloaded).
	anthropicMaxRetries = 3
	// anthropicMaxRetryWait caps one wait, also when retry-after asks for more.
	anthropicMaxRetryWait = 60 * time.Second
	// anthropicMaxContinuations bounds pause_turn continuations in one call.
	anthropicMaxContinuations = 3
)

// AnthropicModels are the current Claude models offered when the models
// endpoint is not reachable (some proxies do not expose it). Any other
// model id can still be added by hand.
var AnthropicModels = []string{
	"claude-sonnet-5-5",
	"claude-opus-5-5",
	"claude-haiku-5-5",
	"claude-fable-5-1",
}

// AnthropicDefaultMaxTokens is the output cap sent when the admin has not
// set one. Thinking counts toward it, so it is generous; Haiku answers
// quick tasks and gets less.
func AnthropicDefaultMaxTokens(model string) int {
	if strings.Contains(strings.ToLower(model), "haiku") {
		return 8192
	}
	return 16384
}

type anthropicProvider struct {
	client *http.Client
	// wait sleeps before a retry; tests replace it.
	wait func(ctx context.Context, d time.Duration) error
}

func newAnthropicProvider(client *http.Client) *anthropicProvider {
	return &anthropicProvider{client: client, wait: sleepCtx}
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// messagesURL accepts a root URL (https://api.anthropic.com), a /v1 base or
// a full proxy path ending in /v1.
func (p *anthropicProvider) endpoint(cfg ProviderConfig, path string) string {
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if base == "" {
		base = anthropicDefaultBaseURL
	}
	if !strings.HasSuffix(base, "/v1") {
		base += "/v1"
	}
	return base + path
}

func (p *anthropicProvider) setHeaders(req *http.Request, cfg ProviderConfig) {
	req.Header.Set("x-api-key", strings.TrimSpace(cfg.APIKey))
	req.Header.Set("anthropic-version", anthropicVersion)
	req.Header.Set("content-type", "application/json")
}

// -------- request shapes --------

type anthropicCacheControl struct {
	Type string `json:"type"`
}

var ephemeral = &anthropicCacheControl{Type: "ephemeral"}

type anthropicSystemBlock struct {
	Type         string                 `json:"type"`
	Text         string                 `json:"text"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicTool struct {
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	InputSchema  json.RawMessage        `json:"input_schema"`
	CacheControl *anthropicCacheControl `json:"cache_control,omitempty"`
}

type anthropicMessage struct {
	Role    string           `json:"role"`
	Content []map[string]any `json:"content"`
}

type anthropicRequest struct {
	Model      string                 `json:"model"`
	MaxTokens  int                    `json:"max_tokens"`
	Stream     bool                   `json:"stream"`
	System     []anthropicSystemBlock `json:"system,omitempty"`
	Messages   []anthropicMessage     `json:"messages"`
	Tools      []anthropicTool        `json:"tools,omitempty"`
	ToolChoice map[string]string      `json:"tool_choice,omitempty"`
}

// buildAnthropicRequest converts the provider-neutral chat into a Messages
// API request. System messages become the top-level system block, tool
// results are grouped into one user message, and an assistant message that
// carries the provider's own blocks (thinking with its signature, text,
// tool_use) is replayed unchanged.
func buildAnthropicRequest(model string, maxTokens int, messages []ChatMessage, toolDefs []tools.Definition) anthropicRequest {
	req := anthropicRequest{Model: model, MaxTokens: maxTokens, Stream: true}
	if req.MaxTokens <= 0 {
		req.MaxTokens = AnthropicDefaultMaxTokens(model)
	}

	var system []string
	var out []anthropicMessage
	push := func(role string, blocks ...map[string]any) {
		if len(blocks) == 0 {
			return
		}
		if n := len(out); n > 0 && out[n-1].Role == role {
			out[n-1].Content = append(out[n-1].Content, blocks...)
			return
		}
		out = append(out, anthropicMessage{Role: role, Content: blocks})
	}

	for _, m := range messages {
		switch m.Role {
		case "system":
			if s := strings.TrimSpace(m.Content); s != "" {
				system = append(system, s)
			}
		case "user":
			if strings.TrimSpace(m.Content) != "" {
				push("user", map[string]any{"type": "text", "text": m.Content})
			}
		case "assistant":
			if len(m.ProviderBlocks) > 0 {
				var blocks []map[string]any
				if json.Unmarshal(m.ProviderBlocks, &blocks) == nil && len(blocks) > 0 {
					push("assistant", blocks...)
					continue
				}
			}
			var blocks []map[string]any
			if strings.TrimSpace(m.Content) != "" {
				blocks = append(blocks, map[string]any{"type": "text", "text": m.Content})
			}
			for _, tc := range m.ToolCalls {
				input := json.RawMessage(tc.Function.Arguments)
				if !json.Valid(input) || !bytes.HasPrefix(bytes.TrimSpace(input), []byte("{")) {
					input = json.RawMessage("{}")
				}
				blocks = append(blocks, map[string]any{"type": "tool_use", "id": tc.ID, "name": tc.Function.Name, "input": input})
			}
			push("assistant", blocks...)
		case "tool":
			content := m.Content
			if strings.TrimSpace(content) == "" {
				content = "(no output)"
			}
			block := map[string]any{"type": "tool_result", "tool_use_id": m.ToolCallID, "content": content}
			if m.IsError {
				block["is_error"] = true
			}
			push("user", block)
		}
	}

	if len(system) > 0 {
		req.System = []anthropicSystemBlock{{Type: "text", Text: strings.Join(system, "\n\n"), CacheControl: ephemeral}}
	}
	for i, d := range toolDefs {
		schema := d.Function.Parameters
		if len(bytes.TrimSpace(schema)) == 0 || string(bytes.TrimSpace(schema)) == "null" {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		t := anthropicTool{Name: d.Function.Name, Description: d.Function.Description, InputSchema: schema}
		if i == len(toolDefs)-1 {
			t.CacheControl = ephemeral
		}
		req.Tools = append(req.Tools, t)
	}
	if len(req.Tools) > 0 {
		req.ToolChoice = map[string]string{"type": "auto"}
	}
	// One more breakpoint on the newest user content, so the next iteration
	// of an agent turn reads everything before it from cache.
	if n := len(out); n > 0 && out[n-1].Role == "user" {
		blocks := out[n-1].Content
		last := blocks[len(blocks)-1]
		if t, _ := last["type"].(string); t == "text" || t == "tool_result" {
			cp := make(map[string]any, len(last)+1)
			for k, v := range last {
				cp[k] = v
			}
			cp["cache_control"] = ephemeral
			blocks[len(blocks)-1] = cp
		}
	}
	req.Messages = out
	return req
}

// stripThinking removes thinking blocks from every assistant message. It is
// the one-time recovery when the API rejects a replayed thinking block (for
// example after older tool outputs of the turn were shortened).
func stripThinking(req *anthropicRequest) {
	for i := range req.Messages {
		if req.Messages[i].Role != "assistant" {
			continue
		}
		kept := req.Messages[i].Content[:0:0]
		for _, b := range req.Messages[i].Content {
			if t, _ := b["type"].(string); t == "thinking" || t == "redacted_thinking" {
				continue
			}
			kept = append(kept, b)
		}
		req.Messages[i].Content = kept
	}
	msgs := req.Messages[:0:0]
	for _, m := range req.Messages {
		if len(m.Content) > 0 {
			msgs = append(msgs, m)
		}
	}
	req.Messages = msgs
}

func hasThinking(req *anthropicRequest) bool {
	for _, m := range req.Messages {
		if m.Role != "assistant" {
			continue
		}
		for _, b := range m.Content {
			if t, _ := b["type"].(string); t == "thinking" || t == "redacted_thinking" {
				return true
			}
		}
	}
	return false
}

// -------- errors --------

// AnthropicError is an error the Messages API returned, as a status code
// plus its error type and message.
type AnthropicError struct {
	Status    int
	Type      string
	Message   string
	RequestID string
}

func (e *AnthropicError) Error() string {
	var sb strings.Builder
	sb.WriteString("Anthropic API error")
	if e.Status > 0 {
		sb.WriteString(fmt.Sprintf(" (%d", e.Status))
		if e.Type != "" {
			sb.WriteString(" " + e.Type)
		}
		sb.WriteString(")")
	} else if e.Type != "" {
		sb.WriteString(" (" + e.Type + ")")
	}
	if e.Message != "" {
		sb.WriteString(": " + e.Message)
	}
	if e.RequestID != "" {
		sb.WriteString(" [request " + e.RequestID + "]")
	}
	return sb.String()
}

func parseAnthropicError(status int, body []byte, requestID string) *AnthropicError {
	e := &AnthropicError{Status: status, RequestID: requestID}
	var parsed struct {
		Error struct {
			Type    string `json:"type"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &parsed) == nil && (parsed.Error.Type != "" || parsed.Error.Message != "") {
		e.Type, e.Message = parsed.Error.Type, parsed.Error.Message
	} else {
		msg := strings.TrimSpace(string(body))
		if len(msg) > 500 {
			msg = msg[:500] + "..."
		}
		e.Message = msg
	}
	return e
}

// isThinkingReplayError reports a 400 caused by a replayed thinking block.
func isThinkingReplayError(err error) bool {
	var ae *AnthropicError
	if !errors.As(err, &ae) || ae.Status != http.StatusBadRequest {
		return false
	}
	msg := strings.ToLower(ae.Message)
	return strings.Contains(msg, "thinking") || strings.Contains(msg, "signature")
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status == 529
}

// retryDelay honors retry-after (seconds or an HTTP date), else backs off
// exponentially from one second.
func retryDelay(attempt int, retryAfter string) time.Duration {
	d := time.Duration(1<<attempt) * time.Second
	if ra := strings.TrimSpace(retryAfter); ra != "" {
		if secs, err := strconv.ParseFloat(ra, 64); err == nil && secs >= 0 {
			d = time.Duration(secs * float64(time.Second))
		} else if at, err := http.ParseTime(ra); err == nil {
			d = time.Until(at)
		}
	}
	if d < 0 {
		d = 0
	}
	if d > anthropicMaxRetryWait {
		d = anthropicMaxRetryWait
	}
	return d
}

// -------- streaming --------

// anthropicTurn is one parsed streamed response.
type anthropicTurn struct {
	blocks     []map[string]any
	stopReason string
	usage      ChatResult
	text       strings.Builder
}

type anthropicUsage struct {
	InputTokens              int `json:"input_tokens"`
	OutputTokens             int `json:"output_tokens"`
	CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     int `json:"cache_read_input_tokens"`
}

func (u *anthropicUsage) applyTo(r *ChatResult) {
	if u == nil {
		return
	}
	// message_delta usage is cumulative; a zero means "not reported here".
	if u.InputTokens > 0 {
		r.InputTokens = u.InputTokens
	}
	if u.OutputTokens > 0 {
		r.OutputTokens = u.OutputTokens
	}
	if u.CacheCreationInputTokens > 0 {
		r.CacheWriteTokens = u.CacheCreationInputTokens
	}
	if u.CacheReadInputTokens > 0 {
		r.CacheReadTokens = u.CacheReadInputTokens
	}
}

type anthropicEvent struct {
	Type    string `json:"type"`
	Index   int    `json:"index"`
	Message *struct {
		Usage *anthropicUsage `json:"usage"`
	} `json:"message"`
	ContentBlock json.RawMessage `json:"content_block"`
	Delta        *struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		PartialJSON string `json:"partial_json"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
		StopReason  string `json:"stop_reason"`
	} `json:"delta"`
	Usage *anthropicUsage `json:"usage"`
	Error *struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
}

// send posts one request with retries on 429/529 and returns the open
// streaming response.
func (p *anthropicProvider) send(ctx context.Context, cfg ProviderConfig, body []byte) (*http.Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.endpoint(cfg, "/messages"), bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create provider request: %w", err)
		}
		p.setHeaders(req, cfg)
		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("provider request failed: %w", err)
		}
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return resp, nil
		}
		errBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		apiErr := parseAnthropicError(resp.StatusCode, errBody, resp.Header.Get("request-id"))
		if !retryable(resp.StatusCode) || attempt >= anthropicMaxRetries {
			return nil, apiErr
		}
		if err := p.wait(ctx, retryDelay(attempt, resp.Header.Get("retry-after"))); err != nil {
			return nil, err
		}
	}
}

// stream runs one request and parses the SSE response. Text deltas go to
// onDelta as they arrive. On a mid-stream error the usage seen so far is
// still returned with the error.
func (p *anthropicProvider) stream(ctx context.Context, cfg ProviderConfig, req anthropicRequest, onDelta func(string) error) (*anthropicTurn, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}
	resp, err := p.send(ctx, cfg, body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	turn := &anthropicTurn{}
	partial := map[int]*strings.Builder{}
	index := map[int]int{} // event index -> position in turn.blocks

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		var ev anthropicEvent
		if err := json.Unmarshal([]byte(data), &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			if ev.Message != nil {
				ev.Message.Usage.applyTo(&turn.usage)
			}
		case "content_block_start":
			var block map[string]any
			if json.Unmarshal(ev.ContentBlock, &block) != nil || block == nil {
				continue
			}
			index[ev.Index] = len(turn.blocks)
			turn.blocks = append(turn.blocks, block)
			if t, _ := block["type"].(string); t == "text" {
				if s, _ := block["text"].(string); s != "" {
					turn.text.WriteString(s)
					if err := onDelta(s); err != nil {
						return turn, err
					}
				}
			}
		case "content_block_delta":
			pos, ok := index[ev.Index]
			if !ok || ev.Delta == nil {
				continue
			}
			block := turn.blocks[pos]
			switch ev.Delta.Type {
			case "text_delta":
				prev, _ := block["text"].(string)
				block["text"] = prev + ev.Delta.Text
				if ev.Delta.Text != "" {
					turn.text.WriteString(ev.Delta.Text)
					if err := onDelta(ev.Delta.Text); err != nil {
						return turn, err
					}
				}
			case "input_json_delta":
				b := partial[ev.Index]
				if b == nil {
					b = &strings.Builder{}
					partial[ev.Index] = b
				}
				b.WriteString(ev.Delta.PartialJSON)
			case "thinking_delta":
				prev, _ := block["thinking"].(string)
				block["thinking"] = prev + ev.Delta.Thinking
			case "signature_delta":
				block["signature"] = ev.Delta.Signature
			}
		case "content_block_stop":
			pos, ok := index[ev.Index]
			if !ok {
				continue
			}
			block := turn.blocks[pos]
			if t, _ := block["type"].(string); t == "tool_use" {
				input := json.RawMessage("{}")
				if b := partial[ev.Index]; b != nil && strings.TrimSpace(b.String()) != "" {
					if raw := json.RawMessage(b.String()); json.Valid(raw) {
						input = raw
					} else {
						// A cut-off input (max_tokens) is not valid JSON;
						// mark it so the tool is not run with half its args.
						block["_invalid_input"] = true
					}
				}
				block["input"] = input
			}
			if t, _ := block["type"].(string); t == "text" {
				delete(block, "citations")
			}
		case "message_delta":
			if ev.Delta != nil && ev.Delta.StopReason != "" {
				turn.stopReason = ev.Delta.StopReason
			}
			ev.Usage.applyTo(&turn.usage)
		case "message_stop":
			return turn, nil
		case "error":
			apiErr := &AnthropicError{}
			if ev.Error != nil {
				apiErr.Type, apiErr.Message = ev.Error.Type, ev.Error.Message
			}
			return turn, apiErr
		}
	}
	if err := scanner.Err(); err != nil {
		return turn, fmt.Errorf("read provider stream: %w", err)
	}
	if turn.stopReason == "" {
		return turn, errors.New("Anthropic stream ended before the message was complete")
	}
	return turn, nil
}

// finishReason maps a stop_reason onto the loop's finish reasons.
func anthropicFinishReason(stop string) string {
	switch stop {
	case "tool_use":
		return "tool_calls"
	case "max_tokens", "model_context_window_exceeded":
		return "length"
	case "refusal":
		return "refusal"
	case "end_turn", "stop_sequence", "":
		return "stop"
	default:
		return stop
	}
}

// run streams one logical call: it retries once without thinking blocks if
// the API rejects a replayed one, continues a paused turn, and sums usage
// across every request it made.
func (p *anthropicProvider) run(ctx context.Context, cfg ProviderConfig, req anthropicRequest, onDelta func(string) error) (*StreamChatToolsResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("provider API key is not configured")
	}
	result := &StreamChatToolsResult{}
	var allBlocks []map[string]any
	var text strings.Builder
	stripped := false

	for cont := 0; ; cont++ {
		turn, err := p.stream(ctx, cfg, req, onDelta)
		if turn != nil {
			result.ChatResult.Add(&turn.usage)
		}
		if err != nil && !stripped && isThinkingReplayError(err) && hasThinking(&req) {
			stripThinking(&req)
			stripped = true
			cont--
			continue
		}
		if err != nil {
			result.Content = text.String() + turnText(turn)
			return result, err
		}
		text.WriteString(turn.text.String())
		allBlocks = append(allBlocks, turn.blocks...)

		if turn.stopReason == "pause_turn" && cont < anthropicMaxContinuations {
			// Send the paused content back as-is so the model resumes.
			req.Messages = append(req.Messages, anthropicMessage{Role: "assistant", Content: turn.blocks})
			continue
		}
		result.FinishReason = anthropicFinishReason(turn.stopReason)
		if turn.stopReason == "pause_turn" {
			result.FinishReason = "stop"
		}
		if result.FinishReason == "length" || result.FinishReason == "refusal" {
			result.StopReason = result.FinishReason
		}
		break
	}

	result.Content = text.String()
	clean := make([]map[string]any, 0, len(allBlocks))
	for _, b := range allBlocks {
		t, _ := b["type"].(string)
		if t == "tool_use" {
			id, _ := b["id"].(string)
			name, _ := b["name"].(string)
			args := "{}"
			if raw, ok := b["input"].(json.RawMessage); ok {
				args = string(raw)
			}
			if invalid, _ := b["_invalid_input"].(bool); invalid {
				delete(b, "_invalid_input")
				args = ""
			}
			result.ToolCalls = append(result.ToolCalls, ToolCallEmit{ID: id, Type: "function", Function: ToolCallFunc{Name: name, Arguments: args}})
		}
		if t == "text" {
			if s, _ := b["text"].(string); strings.TrimSpace(s) == "" {
				continue // the API rejects empty text blocks on replay
			}
		}
		clean = append(clean, b)
	}
	if len(clean) > 0 {
		if raw, err := json.Marshal(clean); err == nil {
			result.ProviderBlocks = raw
		}
	}
	return result, nil
}

func turnText(t *anthropicTurn) string {
	if t == nil {
		return ""
	}
	return t.text.String()
}

// StreamChat is the plain (no tools) streaming chat.
func (p *anthropicProvider) StreamChat(ctx context.Context, cfg ProviderConfig, model string, messages []Message, onDelta func(string) error) (*ChatResult, error) {
	chat := make([]ChatMessage, 0, len(messages))
	for _, m := range messages {
		chat = append(chat, ChatMessage{Role: m.Role, Content: m.Content})
	}
	res, err := p.run(ctx, cfg, buildAnthropicRequest(model, cfg.MaxTokens, chat, nil), onDelta)
	if res == nil {
		return nil, err
	}
	out := res.ChatResult
	return &out, err
}

// StreamChatTools is the tool-aware streaming chat used by the agent loop.
func (p *anthropicProvider) StreamChatTools(ctx context.Context, cfg ProviderConfig, model string, messages []ChatMessage, toolDefs []tools.Definition, onDelta func(string) error) (*StreamChatToolsResult, error) {
	return p.run(ctx, cfg, buildAnthropicRequest(model, cfg.MaxTokens, messages, toolDefs), onDelta)
}

// ListModels reads the models endpoint. A proxy without it gets the
// current Claude models instead.
func (p *anthropicProvider) ListModels(ctx context.Context, cfg ProviderConfig) ([]string, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("provider API key is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.endpoint(cfg, "/models?limit=1000"), nil)
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	p.setHeaders(req, cfg)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return append([]string(nil), AnthropicModels...), nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, parseAnthropicError(resp.StatusCode, body, resp.Header.Get("request-id"))
	}
	var parsed struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}
	models := make([]string, 0, len(parsed.Data))
	for _, m := range parsed.Data {
		if id := strings.TrimSpace(m.ID); id != "" {
			models = append(models, id)
		}
	}
	if len(models) == 0 {
		return append([]string(nil), AnthropicModels...), nil
	}
	return models, nil
}
