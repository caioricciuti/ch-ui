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
	"net/url"
	"strings"
	"time"
)

// ErrToolsUnsupported is returned by CallWithTools when the provider does
// not implement the toolsCapable interface (e.g. Ollama).
var ErrToolsUnsupported = errors.New("provider does not support tool calling")

// Message represents one chat message for provider calls.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ProviderConfig defines provider runtime configuration.
type ProviderConfig struct {
	Kind    string
	BaseURL string
	APIKey  string
	// MaxTokens caps the output of one request where the API needs a cap
	// (Anthropic). Zero means the per-model default.
	MaxTokens int
}

// ChatResult holds optional metadata returned after streaming completes.
type ChatResult struct {
	// InputTokens counts input not read from the prompt cache; cached input
	// is in CacheReadTokens and input written to the cache in
	// CacheWriteTokens, so the three never overlap.
	InputTokens      int
	OutputTokens     int
	CacheReadTokens  int
	CacheWriteTokens int
	// StopReason is "length" when the answer hit the output cap and
	// "refusal" when the model declined, where the provider reports it.
	StopReason      string
	ModelParameters map[string]interface{}
}

// Add sums another call's token counts into r.
func (r *ChatResult) Add(o *ChatResult) {
	if r == nil || o == nil {
		return
	}
	r.InputTokens += o.InputTokens
	r.OutputTokens += o.OutputTokens
	r.CacheReadTokens += o.CacheReadTokens
	r.CacheWriteTokens += o.CacheWriteTokens
}

// Tokens is the total the ledger counts toward a budget: input, output and
// cache writes. Cache reads are left out; they cost a tenth of input or
// less and an agent turn re-reads its prefix on every step.
func (r ChatResult) Tokens() int {
	return r.InputTokens + r.OutputTokens + r.CacheWriteTokens
}

// Provider handles streaming chat and model discovery.
type Provider interface {
	StreamChat(ctx context.Context, cfg ProviderConfig, model string, messages []Message, onDelta func(string) error) (*ChatResult, error)
	ListModels(ctx context.Context, cfg ProviderConfig) ([]string, error)
}

var sharedHTTPClient = &http.Client{
	Timeout: 5 * time.Minute,
	Transport: &http.Transport{
		MaxIdleConns:        20,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	},
}

func NewProvider(kind string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai", "openai_compatible":
		return &openAIProvider{client: sharedHTTPClient}, nil
	case "ollama":
		return &ollamaProvider{client: sharedHTTPClient}, nil
	case "anthropic":
		return newAnthropicProvider(sharedHTTPClient), nil
	default:
		return nil, fmt.Errorf("unsupported provider kind: %s", kind)
	}
}

// -------- OpenAI provider --------

type openAIProvider struct {
	client *http.Client
}

type openAIRequest struct {
	Model         string               `json:"model"`
	Messages      []Message            `json:"messages"`
	Stream        bool                 `json:"stream"`
	Temperature   *float64             `json:"temperature,omitempty"`
	StreamOptions *openAIStreamOptions `json:"stream_options,omitempty"`
}

type openAIStreamOptions struct {
	IncludeUsage bool `json:"include_usage"`
}

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *openAIUsage `json:"usage"`
}

// openAIUsage is the usage object of an OpenAI-style stream. Cached prompt
// tokens are part of prompt_tokens there; they are split out here.
type openAIUsage struct {
	PromptTokens        int `json:"prompt_tokens"`
	CompletionTokens    int `json:"completion_tokens"`
	PromptTokensDetails *struct {
		CachedTokens int `json:"cached_tokens"`
	} `json:"prompt_tokens_details"`
}

func (u *openAIUsage) applyTo(r *ChatResult) {
	if u == nil {
		return
	}
	cached := 0
	if u.PromptTokensDetails != nil {
		cached = u.PromptTokensDetails.CachedTokens
	}
	if cached > u.PromptTokens {
		cached = u.PromptTokens
	}
	r.InputTokens = u.PromptTokens - cached
	r.CacheReadTokens = cached
	r.OutputTokens = u.CompletionTokens
}

const openAIDefaultTemperature = 0.1

func ensureOpenAIV1Base(rawBase string) string {
	trimmed := strings.TrimRight(strings.TrimSpace(rawBase), "/")
	if trimmed == "" {
		return "https://api.openai.com/v1"
	}
	parsed, err := url.Parse(trimmed)
	if err != nil {
		if strings.HasSuffix(trimmed, "/v1") {
			return trimmed
		}
		return trimmed + "/v1"
	}
	path := strings.TrimRight(strings.TrimSpace(parsed.Path), "/")
	if path == "/v1" || strings.HasSuffix(path, "/v1") {
		return strings.TrimRight(parsed.String(), "/")
	}
	if path == "" || path == "/" {
		parsed.Path = "/v1"
	} else {
		parsed.Path = path + "/v1"
	}
	return strings.TrimRight(parsed.String(), "/")
}

func shouldRetryOpenAIV1(status int, body []byte) bool {
	if status != http.StatusNotFound {
		return false
	}
	msg := strings.ToLower(string(body))
	return strings.Contains(msg, "invalid url") || strings.Contains(msg, "/models") || strings.Contains(msg, "/chat/completions")
}

func (p *openAIProvider) baseURL(cfg ProviderConfig) string {
	raw := strings.TrimSpace(cfg.BaseURL)
	if raw == "" {
		return "https://api.openai.com/v1"
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return strings.TrimRight(raw, "/")
	}

	base := strings.TrimRight(parsed.String(), "/")
	path := strings.TrimSpace(parsed.Path)
	// For OpenAI-style APIs, a root URL should target /v1 endpoints.
	if path == "" || path == "/" {
		return strings.TrimRight(base, "/") + "/v1"
	}
	return base
}

// DefaultModelParameters returns the default provider request parameters for one model.
func DefaultModelParameters(kind, model string) map[string]interface{} {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai", "openai_compatible":
		return openAIModelParameters(openAIRequestTemperature(model))
	default:
		return nil
	}
}

func openAIRequestTemperature(model string) *float64 {
	if openAIModelRequiresDefaultTemperature(model) {
		return nil
	}
	temperature := openAIDefaultTemperature
	return &temperature
}

func openAIModelRequiresDefaultTemperature(model string) bool {
	name := strings.ToLower(strings.TrimSpace(model))
	if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	// Current Claude models reject a non-default temperature, also through
	// OpenAI-compatible gateways, so none is sent.
	return strings.HasPrefix(name, "o1") || strings.HasPrefix(name, "o3") || strings.HasPrefix(name, "o4") ||
		strings.HasPrefix(name, "claude") || strings.Contains(name, ".claude") || strings.HasPrefix(name, "anthropic")
}

// checkAPIKey requires a key for OpenAI itself. OpenAI-compatible servers
// (local vLLM, LM Studio, llama.cpp, an internal gateway) often run keyless.
func checkAPIKey(cfg ProviderConfig) error {
	if strings.TrimSpace(cfg.APIKey) != "" {
		return nil
	}
	if strings.EqualFold(strings.TrimSpace(cfg.Kind), "openai_compatible") {
		return nil
	}
	return errors.New("provider API key is not configured")
}

// setAuthHeader sends the bearer key, or nothing for a keyless provider.
func setAuthHeader(req *http.Request, cfg ProviderConfig) {
	if key := strings.TrimSpace(cfg.APIKey); key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
}

func openAIModelParameters(temperature *float64) map[string]interface{} {
	params := map[string]interface{}{}
	if temperature != nil {
		params["temperature"] = *temperature
	}
	return params
}

func isUnsupportedOpenAITemperature(status int, body []byte) bool {
	if status != http.StatusBadRequest {
		return false
	}
	msg := strings.ToLower(string(body))
	return strings.Contains(msg, "temperature") &&
		(strings.Contains(msg, "unsupported value") ||
			strings.Contains(msg, "does not support") ||
			strings.Contains(msg, "only the default"))
}

func (p *openAIProvider) StreamChat(ctx context.Context, cfg ProviderConfig, model string, messages []Message, onDelta func(string) error) (*ChatResult, error) {
	if err := checkAPIKey(cfg); err != nil {
		return nil, err
	}

	temperatures := []*float64{openAIRequestTemperature(model)}
	if temperatures[0] != nil {
		temperatures = append(temperatures, nil)
	}

	var lastStatus int
	var lastErrBody []byte
	for attemptIdx, temperature := range temperatures {
		result, status, errBody, err := p.streamChatAttempt(ctx, cfg, model, messages, temperature, onDelta)
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

func (p *openAIProvider) streamChatAttempt(ctx context.Context, cfg ProviderConfig, model string, messages []Message, temperature *float64, onDelta func(string) error) (*ChatResult, int, []byte, error) {
	payload := openAIRequest{
		Model:         model,
		Messages:      messages,
		Stream:        true,
		Temperature:   temperature,
		StreamOptions: &openAIStreamOptions{IncludeUsage: true},
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
		setAuthHeader(req, cfg)

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

		result := ChatResult{ModelParameters: openAIModelParameters(temperature)}
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				resp.Body.Close()
				return &result, 0, nil, nil
			}

			var chunk openAIChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			chunk.Usage.applyTo(&result)
			for _, c := range chunk.Choices {
				if c.Delta.Content == "" {
					continue
				}
				if err := onDelta(c.Delta.Content); err != nil {
					resp.Body.Close()
					return nil, 0, nil, err
				}
			}
		}
		if err := scanner.Err(); err != nil {
			resp.Body.Close()
			return nil, 0, nil, fmt.Errorf("read provider stream: %w", err)
		}
		resp.Body.Close()
		return &result, 0, nil, nil
	}

	if lastStatus != 0 {
		return nil, lastStatus, lastErrBody, nil
	}
	return nil, 0, nil, errors.New("provider request failed")
}

func (p *openAIProvider) ListModels(ctx context.Context, cfg ProviderConfig) ([]string, error) {
	if err := checkAPIKey(cfg); err != nil {
		return nil, err
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
		endpoint := base + "/models"
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
		if err != nil {
			return nil, fmt.Errorf("create provider request: %w", err)
		}
		setAuthHeader(req, cfg)

		resp, err := p.client.Do(req)
		if err != nil {
			return nil, fmt.Errorf("provider request failed: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastStatus = resp.StatusCode
			lastErrBody = errBody
			if idx < len(bases)-1 && shouldRetryOpenAIV1(resp.StatusCode, errBody) {
				continue
			}
			return nil, fmt.Errorf("provider error (%d): %s", resp.StatusCode, string(errBody))
		}

		var parsed struct {
			Data []struct {
				ID string `json:"id"`
			} `json:"data"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("decode models response: %w", err)
		}
		resp.Body.Close()

		models := make([]string, 0, len(parsed.Data))
		for _, item := range parsed.Data {
			if strings.TrimSpace(item.ID) == "" {
				continue
			}
			models = append(models, item.ID)
		}
		return models, nil
	}

	if lastStatus != 0 {
		return nil, fmt.Errorf("provider error (%d): %s", lastStatus, string(lastErrBody))
	}
	return nil, errors.New("provider request failed")
}

// -------- Ollama provider --------

type ollamaProvider struct {
	client *http.Client
}

func (p *ollamaProvider) baseURL(cfg ProviderConfig) string {
	if strings.TrimSpace(cfg.BaseURL) != "" {
		base := strings.TrimRight(cfg.BaseURL, "/")
		base = strings.TrimSuffix(base, "/v1")
		return base
	}
	return "http://localhost:11434"
}

func (p *ollamaProvider) StreamChat(ctx context.Context, cfg ProviderConfig, model string, messages []Message, onDelta func(string) error) (*ChatResult, error) {
	payload := map[string]interface{}{
		"model":    model,
		"stream":   true,
		"messages": messages,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal provider request: %w", err)
	}

	url := p.baseURL(cfg) + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider error (%d): %s", resp.StatusCode, string(errBody))
	}

	var result ChatResult
	scanner := bufio.NewScanner(resp.Body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var chunk struct {
			Done    bool `json:"done"`
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
			Error           string `json:"error"`
			PromptEvalCount int    `json:"prompt_eval_count"`
			EvalCount       int    `json:"eval_count"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Error != "" {
			return nil, errors.New(chunk.Error)
		}
		if chunk.Message.Content != "" {
			if err := onDelta(chunk.Message.Content); err != nil {
				return nil, err
			}
		}
		if chunk.Done {
			result.InputTokens = chunk.PromptEvalCount
			result.OutputTokens = chunk.EvalCount
			return &result, nil
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read provider stream: %w", err)
	}
	return &result, nil
}

func (p *ollamaProvider) ListModels(ctx context.Context, cfg ProviderConfig) ([]string, error) {
	url := p.baseURL(cfg) + "/api/tags"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create provider request: %w", err)
	}

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("provider error (%d): %s", resp.StatusCode, string(errBody))
	}

	var parsed struct {
		Models []struct {
			Name string `json:"name"`
		} `json:"models"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("decode models response: %w", err)
	}

	models := make([]string, 0, len(parsed.Models))
	for _, item := range parsed.Models {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		models = append(models, item.Name)
	}
	return models, nil
}
