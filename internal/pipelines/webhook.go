package pipelines

import (
	"bytes"
	"context"
	"crypto/subtle"
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

// Validate checks webhook configuration. auth_token is optional: without it
// the webhook accepts every request. New webhook nodes get one by default.
func (w *WebhookSource) Validate(cfg ConnectorConfig) error {
	if boolField(cfg.Fields, "auth_enabled", false) && stringField(cfg.Fields, "auth_token", "") == "" {
		return fmt.Errorf("authentication is enabled but no token is set; regenerate the token")
	}
	return validateCommon(cfg.Fields)
}

// Start blocks until the context is cancelled, forwarding received batches to out.
func (w *WebhookSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	pipelineID, _ := cfg.Fields["pipeline_id"].(string)
	authToken, _ := cfg.Fields["auth_token"].(string)
	batchSize := max(intField(cfg.Fields, "batch_size", 100), 1)
	batchTimeoutMs := max(intField(cfg.Fields, "batch_timeout_ms", 2000), 100)

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
	// enqueueMu makes the capacity check and the enqueue of one request
	// atomic with respect to other requests, so a request is accepted whole
	// or not at all.
	enqueueMu sync.Mutex
}

// enqueue adds all records or none. It reports false when the buffer lacks
// room for the whole request.
func (r *webhookReceiver) enqueue(records []Record) bool {
	r.enqueueMu.Lock()
	defer r.enqueueMu.Unlock()
	if cap(r.incoming)-len(r.incoming) < len(records) {
		return false
	}
	// Only the batching loop removes from the channel, so the room checked
	// above cannot shrink: these sends never block.
	for _, rec := range records {
		r.incoming <- rec
	}
	return true
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
	if recv.authToken != "" && !webhookTokenMatches(r, recv.authToken) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
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

	if len(records) > cap(recv.incoming) {
		http.Error(w, fmt.Sprintf("too many records in one request (max %d)", cap(recv.incoming)), http.StatusRequestEntityTooLarge)
		return
	}
	// All or nothing: a partial accept followed by 429 would make the sender
	// retry records that were already queued.
	if !recv.enqueue(records) {
		w.Header().Set("Retry-After", "1")
		http.Error(w, "pipeline buffer full, try again later", http.StatusTooManyRequests)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"accepted":%d}`, len(records))
}

// webhookTokenMatches reports whether the request carries the pipeline's
// token. The Authorization header ("Bearer <token>") is preferred; ?token= is
// still accepted for existing senders, though query strings end up in proxy
// and access logs. The comparison is constant time.
func webhookTokenMatches(r *http.Request, want string) bool {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		token = r.URL.Query().Get("token")
	}
	if token == "" || want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(token), []byte(want)) == 1
}

// parseWebhookBody parses a JSON object, a JSON array of objects, or
// newline-delimited JSON into records. The whole body is tried as JSON first,
// so a pretty-printed object or array works; NDJSON is the fallback.
func parseWebhookBody(body []byte, contentType string) ([]Record, error) {
	trimmed := bytes.TrimSpace(body)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty body")
	}

	var whole interface{}
	if err := json.Unmarshal(trimmed, &whole); err == nil {
		switch whole.(type) {
		case map[string]interface{}:
			rec, err := webhookRecord(trimmed)
			if err != nil {
				return nil, err
			}
			return []Record{rec}, nil
		case []interface{}:
			var arr []json.RawMessage
			if err := json.Unmarshal(trimmed, &arr); err != nil {
				return nil, fmt.Errorf("invalid JSON array: %w", err)
			}
			records := make([]Record, 0, len(arr))
			for _, raw := range arr {
				rec, err := webhookRecord(raw)
				if err != nil {
					return nil, fmt.Errorf("invalid JSON object in array: %w", err)
				}
				records = append(records, rec)
			}
			return records, nil
		default:
			return nil, fmt.Errorf("body must be a JSON object, an array of objects, or NDJSON")
		}
	}
	if trimmed[0] == '[' {
		return nil, fmt.Errorf("invalid JSON array")
	}

	// NDJSON
	var records []Record
	for _, line := range bytes.Split(trimmed, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if len(line) == 0 {
			continue
		}
		rec, err := webhookRecord(line)
		if err != nil {
			return nil, fmt.Errorf("invalid JSON line: %w", err)
		}
		records = append(records, rec)
	}
	return records, nil
}

// webhookRecord parses one JSON object and keeps it compacted on one line,
// as JSONEachRow needs.
func webhookRecord(raw []byte) (Record, error) {
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return Record{}, err
	}
	if data == nil {
		return Record{}, fmt.Errorf("expected a JSON object")
	}
	var compact bytes.Buffer
	if err := json.Compact(&compact, raw); err != nil {
		return Record{}, err
	}
	return Record{Data: data, RawJSON: compact.Bytes()}, nil
}
