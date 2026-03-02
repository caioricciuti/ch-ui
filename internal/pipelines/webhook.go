package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// WebhookSource receives data via HTTP POST requests.
// When started, it registers itself in a global registry so that an
// external HTTP handler can route incoming requests to the right pipeline.
type WebhookSource struct{}

func (w *WebhookSource) Type() string { return "source_webhook" }

// Validate checks webhook configuration.
func (w *WebhookSource) Validate(cfg ConnectorConfig) error {
	// auth_token is optional — when empty, the webhook accepts all requests
	return nil
}

// Start blocks until the context is cancelled, forwarding received batches to out.
func (w *WebhookSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	pipelineID, _ := cfg.Fields["pipeline_id"].(string)
	authToken, _ := cfg.Fields["auth_token"].(string)
	batchSize := intField(cfg.Fields, "batch_size", 100)
	batchTimeoutMs := intField(cfg.Fields, "batch_timeout_ms", 5000)

	// Create a receiver for this pipeline
	recv := &webhookReceiver{
		authToken: authToken,
		incoming:  make(chan Record, 1000),
	}

	// Register in global registry
	webhookRegistry.Store(pipelineID, recv)
	defer webhookRegistry.Delete(pipelineID)

	slog.Info("Webhook source started", "pipeline", pipelineID)

	// Batch accumulation loop
	var buf []Record
	ticker := time.NewTicker(time.Duration(batchTimeoutMs) * time.Millisecond)
	defer ticker.Stop()

	flush := func() {
		if len(buf) == 0 {
			return
		}
		batch := Batch{
			Records:  buf,
			SourceTS: time.Now(),
		}
		select {
		case out <- batch:
		case <-ctx.Done():
			return
		}
		buf = nil
	}

	for {
		select {
		case <-ctx.Done():
			flush()
			return nil
		case rec := <-recv.incoming:
			buf = append(buf, rec)
			if len(buf) >= batchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// ── Webhook HTTP integration ───────────────────────────────────────

// webhookRegistry maps pipeline IDs to active webhook receivers.
var webhookRegistry sync.Map

// webhookReceiver holds the channel for a single pipeline's webhook endpoint.
type webhookReceiver struct {
	authToken string
	incoming  chan Record
}

// HandleWebhook is an HTTP handler that routes incoming webhook POSTs to the
// correct running pipeline. Mount at: POST /api/pipelines/webhook/{pipelineID}
func HandleWebhook(w http.ResponseWriter, r *http.Request) {
	// Extract pipeline ID from URL path (last segment)
	parts := strings.Split(strings.TrimRight(r.URL.Path, "/"), "/")
	if len(parts) == 0 {
		http.Error(w, "missing pipeline ID", http.StatusBadRequest)
		return
	}
	pipelineID := parts[len(parts)-1]

	val, ok := webhookRegistry.Load(pipelineID)
	if !ok {
		http.Error(w, "pipeline not running or not a webhook pipeline", http.StatusNotFound)
		return
	}
	recv := val.(*webhookReceiver)

	// Authenticate (skip if no auth token configured)
	if recv.authToken != "" {
		token := r.Header.Get("Authorization")
		token = strings.TrimPrefix(token, "Bearer ")
		if token == "" {
			token = r.URL.Query().Get("token")
		}
		if token != recv.authToken {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
	}

	// Read body
	body, err := io.ReadAll(io.LimitReader(r.Body, 10*1024*1024)) // 10MB limit
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}

	contentType := r.Header.Get("Content-Type")
	records, parseErr := parseWebhookBody(body, contentType)
	if parseErr != nil {
		http.Error(w, fmt.Sprintf("parse error: %v", parseErr), http.StatusBadRequest)
		return
	}

	// Send records to pipeline (non-blocking with backpressure)
	accepted := 0
	for _, rec := range records {
		select {
		case recv.incoming <- rec:
			accepted++
		default:
			// Channel full, apply backpressure
			http.Error(w, "pipeline buffer full, try again later", http.StatusTooManyRequests)
			return
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"accepted":%d}`, accepted)
}

// parseWebhookBody parses JSON or NDJSON into records.
func parseWebhookBody(body []byte, contentType string) ([]Record, error) {
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return nil, fmt.Errorf("empty body")
	}

	// Try to detect if it's an array or single object
	if strings.HasPrefix(trimmed, "[") {
		// JSON array
		var arr []json.RawMessage
		if err := json.Unmarshal(body, &arr); err != nil {
			return nil, fmt.Errorf("invalid JSON array: %w", err)
		}
		var records []Record
		for _, raw := range arr {
			var data map[string]interface{}
			if err := json.Unmarshal(raw, &data); err != nil {
				return nil, fmt.Errorf("invalid JSON object in array: %w", err)
			}
			records = append(records, Record{
				Data:    data,
				RawJSON: raw,
			})
		}
		return records, nil
	}

	// NDJSON or single object
	lines := strings.Split(trimmed, "\n")
	var records []Record
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var data map[string]interface{}
		if err := json.Unmarshal([]byte(line), &data); err != nil {
			return nil, fmt.Errorf("invalid JSON line: %w", err)
		}
		records = append(records, Record{
			Data:    data,
			RawJSON: []byte(line),
		})
	}

	return records, nil
}
