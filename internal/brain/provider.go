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
)

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
}

// Provider handles streaming chat and model discovery.
type Provider interface {
	StreamChat(ctx context.Context, cfg ProviderConfig, model string, messages []Message, onDelta func(string) error) error
	ListModels(ctx context.Context, cfg ProviderConfig) ([]string, error)
}

func NewProvider(kind string) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "openai", "openai_compatible":
		return &openAIProvider{client: &http.Client{}}, nil
	case "ollama":
		return &ollamaProvider{client: &http.Client{}}, nil
	default:
		return nil, fmt.Errorf("unsupported provider kind: %s", kind)
	}
}

// -------- OpenAI provider --------

type openAIProvider struct {
	client *http.Client
}

type openAIRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream"`
	Temperature float64   `json:"temperature"`
}

type openAIChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
	} `json:"choices"`
}

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

func (p *openAIProvider) StreamChat(ctx context.Context, cfg ProviderConfig, model string, messages []Message, onDelta func(string) error) error {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return errors.New("provider API key is not configured")
	}

	payload := openAIRequest{
		Model:       model,
		Messages:    messages,
		Stream:      true,
		Temperature: 0.1,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal provider request: %w", err)
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
			return fmt.Errorf("create provider request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

		resp, doErr := p.client.Do(req)
		if doErr != nil {
			return fmt.Errorf("provider request failed: %w", doErr)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			errBody, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			lastStatus = resp.StatusCode
			lastErrBody = errBody
			if idx < len(bases)-1 && shouldRetryOpenAIV1(resp.StatusCode, errBody) {
				continue
			}
			return fmt.Errorf("provider error (%d): %s", resp.StatusCode, string(errBody))
		}

		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			data := strings.TrimPrefix(line, "data: ")
			if data == "[DONE]" {
				resp.Body.Close()
				return nil
			}

			var chunk openAIChunk
			if err := json.Unmarshal([]byte(data), &chunk); err != nil {
				continue
			}
			for _, c := range chunk.Choices {
				if c.Delta.Content == "" {
					continue
				}
				if err := onDelta(c.Delta.Content); err != nil {
					resp.Body.Close()
					return err
				}
			}
		}
		if err := scanner.Err(); err != nil {
			resp.Body.Close()
			return fmt.Errorf("read provider stream: %w", err)
		}
		resp.Body.Close()
		return nil
	}

	if lastStatus != 0 {
		return fmt.Errorf("provider error (%d): %s", lastStatus, string(lastErrBody))
	}
	return errors.New("provider request failed")
}

func (p *openAIProvider) ListModels(ctx context.Context, cfg ProviderConfig) ([]string, error) {
	if strings.TrimSpace(cfg.APIKey) == "" {
		return nil, errors.New("provider API key is not configured")
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
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)

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
		return strings.TrimRight(cfg.BaseURL, "/")
	}
	return "http://localhost:11434"
}

func (p *ollamaProvider) StreamChat(ctx context.Context, cfg ProviderConfig, model string, messages []Message, onDelta func(string) error) error {
	payload := map[string]interface{}{
		"model":    model,
		"stream":   true,
		"messages": messages,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("marshal provider request: %w", err)
	}

	url := p.baseURL(cfg) + "/api/chat"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("create provider request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("provider request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		errBody, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("provider error (%d): %s", resp.StatusCode, string(errBody))
	}

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
			Error string `json:"error"`
		}
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			continue
		}
		if chunk.Error != "" {
			return errors.New(chunk.Error)
		}
		if chunk.Message.Content != "" {
			if err := onDelta(chunk.Message.Content); err != nil {
				return err
			}
		}
		if chunk.Done {
			return nil
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read provider stream: %w", err)
	}
	return nil
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
