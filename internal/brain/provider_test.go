package brain

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestOpenAIProviderStreamChatOmitsTemperatureForReasoningModels(t *testing.T) {
	t.Parallel()

	var attempts int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		attempts++

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if _, ok := payload["temperature"]; ok {
			t.Fatalf("temperature should be omitted for o4-mini payloads")
		}

		writeOpenAIStreamResponse(w, "hello", 3, 1)
	}))
	defer server.Close()

	provider := &openAIProvider{client: server.Client()}
	var built strings.Builder
	result, err := provider.StreamChat(
		context.Background(),
		ProviderConfig{Kind: "openai", BaseURL: server.URL, APIKey: "test-key"},
		"o4-mini",
		[]Message{{Role: "user", Content: "help"}},
		func(delta string) error {
			built.WriteString(delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if built.String() != "hello" {
		t.Fatalf("unexpected streamed content: %q", built.String())
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt, got %d", attempts)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if result.InputTokens != 3 || result.OutputTokens != 1 {
		t.Fatalf("unexpected usage: %+v", result)
	}
	if len(result.ModelParameters) != 0 {
		t.Fatalf("expected no model parameters, got %+v", result.ModelParameters)
	}
}

func TestOpenAIProviderStreamChatRetriesWithoutTemperatureOnUnsupportedValue(t *testing.T) {
	t.Parallel()

	var (
		mu       sync.Mutex
		attempts []bool
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}

		var payload map[string]interface{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request: %v", err)
		}

		_, hasTemperature := payload["temperature"]
		mu.Lock()
		attempts = append(attempts, hasTemperature)
		mu.Unlock()

		if hasTemperature {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusBadRequest)
			_, _ = fmt.Fprint(w, `{"error":{"message":"Unsupported value: 'temperature' does not support 0.1 with this model. Only the default (1) value is supported.","type":"invalid_request_error","param":"temperature","code":"unsupported_value"}}`)
			return
		}

		writeOpenAIStreamResponse(w, "fixed", 5, 2)
	}))
	defer server.Close()

	provider := &openAIProvider{client: server.Client()}
	var built strings.Builder
	result, err := provider.StreamChat(
		context.Background(),
		ProviderConfig{Kind: "openai", BaseURL: server.URL, APIKey: "test-key"},
		"gpt-4o",
		[]Message{{Role: "user", Content: "help"}},
		func(delta string) error {
			built.WriteString(delta)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("StreamChat returned error: %v", err)
	}
	if built.String() != "fixed" {
		t.Fatalf("unexpected streamed content: %q", built.String())
	}
	mu.Lock()
	gotAttempts := append([]bool(nil), attempts...)
	mu.Unlock()
	if len(gotAttempts) != 2 {
		t.Fatalf("expected 2 attempts, got %d", len(gotAttempts))
	}
	if !gotAttempts[0] || gotAttempts[1] {
		t.Fatalf("expected retry sequence [true false], got %v", gotAttempts)
	}
	if result == nil {
		t.Fatalf("expected result")
	}
	if result.InputTokens != 5 || result.OutputTokens != 2 {
		t.Fatalf("unexpected usage: %+v", result)
	}
	if len(result.ModelParameters) != 0 {
		t.Fatalf("expected no model parameters after retry, got %+v", result.ModelParameters)
	}
}

func writeOpenAIStreamResponse(w http.ResponseWriter, content string, inputTokens, outputTokens int) {
	w.Header().Set("Content-Type", "text/event-stream")
	_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", content)
	_, _ = fmt.Fprintf(w, "data: {\"usage\":{\"prompt_tokens\":%d,\"completion_tokens\":%d}}\n\n", inputTokens, outputTokens)
	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
}
