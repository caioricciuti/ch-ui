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
	"strings"

	"github.com/caioricciuti/ch-ui/internal/brain/tools"
)

// ChatMessage is the rich message format used in the agentic loop. Unlike
// Message (text-only), it can carry tool calls and tool results.
type ChatMessage struct {
	Role       string         `json:"role"` // "system" | "user" | "assistant" | "tool"
	Content    string         `json:"content,omitempty"`
	ToolCalls  []ToolCallEmit `json:"tool_calls,omitempty"`   // role=assistant
	ToolCallID string         `json:"tool_call_id,omitempty"` // role=tool
	Name       string         `json:"name,omitempty"`         // role=tool (function name)
}

// ToolCallEmit is the model's request to invoke a function.
type ToolCallEmit struct {
	ID       string       `json:"id"`
	Type     string       `json:"type"` // always "function"
	Function ToolCallFunc `json:"function"`
}

type ToolCallFunc struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON-encoded string
}

// StreamChatToolsResult is what the agentic provider call returns.
type StreamChatToolsResult struct {
	ChatResult
	FinishReason string         // "stop" | "tool_calls" | "length" | ""
	ToolCalls    []ToolCallEmit // populated when FinishReason == "tool_calls"
	Content      string         // any text emitted before the tool calls (or final text)
}

// toolsCapable is the optional capability for providers that support
// native function calling.
type toolsCapable interface {
	StreamChatTools(
		ctx context.Context,
		cfg ProviderConfig,
		model string,
		messages []ChatMessage,
		toolDefs []tools.Definition,
		onDelta func(string) error,
	) (*StreamChatToolsResult, error)
}

// SupportsTools reports whether a Provider implements native function calling.
func SupportsTools(p Provider) bool {
	_, ok := p.(toolsCapable)
	return ok
}

// CallWithTools runs a tool-aware streaming chat through any Provider. If
// the underlying provider does not implement the capability (e.g.,
// Ollama), it returns ErrToolsUnsupported and the caller should fall back
// to plain StreamChat.
func CallWithTools(
	p Provider,
	ctx context.Context,
	cfg ProviderConfig,
	model string,
	messages []ChatMessage,
	toolDefs []tools.Definition,
	onDelta func(string) error,
) (*StreamChatToolsResult, error) {
	tp, ok := p.(toolsCapable)
	if !ok {
		return nil, ErrToolsUnsupported
	}
	return tp.StreamChatTools(ctx, cfg, model, messages, toolDefs, onDelta)
}

// StreamChatTools is the tool-aware streaming chat. Text deltas are
// surfaced via onDelta; when the model decides to call tools, this
// returns with FinishReason="tool_calls" and ToolCalls populated.
func (p *openAIProvider) StreamChatTools(
	ctx context.Context,
	cfg ProviderConfig,
	model string,
	messages []ChatMessage,
	toolDefs []tools.Definition,
	onDelta func(string) error,
) (*StreamChatToolsResult, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("provider API key is not configured")
	}

	temperatures := []*float64{openAIRequestTemperature(model)}
	if temperatures[0] != nil {
		temperatures = append(temperatures, nil)
	}

	var lastStatus int
	var lastErrBody []byte
	for attemptIdx, temperature := range temperatures {
		result, status, errBody, err := p.streamChatToolsAttempt(ctx, cfg, model, messages, toolDefs, temperature, onDelta)
		if err != nil {
			return nil, err
		}
		if status == 0 {
			return result, nil
		}
		lastStatus = status
		lastErrBody = errBody
		if attemptIdx < len(temperatures)-1 && isUnsupportedOpenAITemperature(status, errBody) {
			continue
		}
		return nil, fmt.Errorf("provider error (%d): %s", status, string(errBody))
	}
	if lastStatus != 0 {
		return nil, fmt.Errorf("provider error (%d): %s", lastStatus, string(lastErrBody))
	}
	return nil, errors.New("provider request failed")
}

type openAIToolsRequest struct {
	Model         string               `json:"model"`
	Messages      []ChatMessage        `json:"messages"`
	Stream        bool                 `json:"stream"`
	Temperature   *float64             `json:"temperature,omitempty"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
	Tools         []tools.Definition   `json:"tools,omitempty"`
	ToolChoice    string               `json:"tool_choice,omitempty"`
}

type openAIToolsChunk struct {
	Choices []struct {
		Delta struct {
			Content   string `json:"content"`
			ToolCalls []struct {
				Index    int    `json:"index"`
				ID       string `json:"id"`
				Type     string `json:"type"`
				Function struct {
					Name      string `json:"name"`
					Arguments string `json:"arguments"`
				} `json:"function"`
			} `json:"tool_calls"`
		} `json:"delta"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (p *openAIProvider) streamChatToolsAttempt(
	ctx context.Context,
	cfg ProviderConfig,
	model string,
	messages []ChatMessage,
	toolDefs []tools.Definition,
	temperature *float64,
	onDelta func(string) error,
) (*StreamChatToolsResult, int, []byte, error) {
	payload := openAIToolsRequest{
		Model:         model,
		Messages:      messages,
		Stream:        true,
		Temperature:   temperature,
		StreamOptions: &openAIStreamOptions{IncludeUsage: true},
	}
	if len(toolDefs) > 0 {
		payload.Tools = toolDefs
		payload.ToolChoice = "auto"
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("marshal provider request: %w", err)
	}

	primaryBase := p.baseURL(cfg)
	bases := []string{primaryBase}
	v1Fallback := ensureOpenAIV1Base(primaryBase)
	if v1Fallback != primaryBase {
		bases = append(bases, v1Fallback)
	}

	var lastStatus int
	var lastErrBody []byte

	for idx, base := range bases {
		endpoint := base + "/chat/completions"
		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
		if reqErr != nil {
			return nil, 0, nil, fmt.Errorf("create provider request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

		resp, doErr := p.client.Do(req)
		if doErr != nil {
			return nil, 0, nil, fmt.Errorf("provider request failed: %w", doErr)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastStatus = resp.StatusCode
			lastErrBody = errBody
			if idx < len(bases)-1 && shouldRetryOpenAIV1(resp.StatusCode, errBody) {
				continue
			}
			return nil, resp.StatusCode, errBody, nil
		}

		result := StreamChatToolsResult{
			ChatResult: ChatResult{ModelParameters: openAIModelParameters(temperature)},
		}
		toolCalls := map[int]*ToolCallEmit{}
		var contentBuilder strings.Builder

		scanner := bufio.NewScanner(resp.Body)
		scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				resp.Body.Close()
				result.Content = contentBuilder.String()
				result.ToolCalls = collectToolCalls(toolCalls)
				return &result, 0, nil, nil
			}
			var chunk openAIToolsChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			if chunk.Usage != nil {
				result.InputTokens = chunk.Usage.PromptTokens
				result.OutputTokens = chunk.Usage.CompletionTokens
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content != "" {
					contentBuilder.WriteString(c.Delta.Content)
					if err := onDelta(c.Delta.Content); err != nil {
						resp.Body.Close()
						return nil, 0, nil, err
					}
				}
				for _, tc := range c.Delta.ToolCalls {
					entry, ok := toolCalls[tc.Index]
					if !ok {
						entry = &ToolCallEmit{Type: "function"}
						toolCalls[tc.Index] = entry
					}
					if tc.ID != "" {
						entry.ID = tc.ID
					}
					if tc.Type != "" {
						entry.Type = tc.Type
					}
					if tc.Function.Name != "" {
						entry.Function.Name = tc.Function.Name
					}
					if tc.Function.Arguments != "" {
						entry.Function.Arguments += tc.Function.Arguments
					}
				}
				if c.FinishReason != "" {
					result.FinishReason = c.FinishReason
				}
			}
		}
		if err := scanner.Err(); err != nil {
			resp.Body.Close()
			return nil, 0, nil, fmt.Errorf("read provider stream: %w", err)
		}
		resp.Body.Close()
		result.Content = contentBuilder.String()
		result.ToolCalls = collectToolCalls(toolCalls)
		return &result, 0, nil, nil
	}

	if lastStatus != 0 {
		return nil, lastStatus, lastErrBody, nil
	}
	return nil, 0, nil, errors.New("provider request failed")
}

func collectToolCalls(m map[int]*ToolCallEmit) []ToolCallEmit {
	if len(m) == 0 {
		return nil
	}
	max := -1
	for k := range m {
		if k > max {
			max = k
		}
	}
	out := make([]ToolCallEmit, 0, len(m))
	for i := 0; i <= max; i++ {
		if v, ok := m[i]; ok && v != nil && v.Function.Name != "" {
			out = append(out, *v)
		}
	}
	return out
}
