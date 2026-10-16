package pipelines

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
)

// WebhookSource receives data via HTTP POST requests. Accepted requests are
// written to the ingest spool before the sender gets its 200, and the source
// drains the spool, so a crash or a ClickHouse outage loses nothing that was
// acknowledged.
type WebhookSource struct{}

func (w *WebhookSource) Type() string { return "source_webhook" }

// Validate checks webhook configuration. auth_token is optional: without it
// the webhook accepts every request. New webhook nodes get one by default.
// batch_size and batch_timeout_ms are still validated for old configs; the
// spool now decides the batches (one sealed segment per insert).
func (w *WebhookSource) Validate(cfg ConnectorConfig) error {
	if boolField(cfg.Fields, "auth_enabled", false) && stringField(cfg.Fields, "auth_token", "") == "" {
		return fmt.Errorf("authentication is enabled but no token is set; regenerate the token")
	}
	if err := checkMin(cfg.Fields, "stall_after_minutes", 0, "stall_after_minutes"); err != nil {
		return err
	}
	return validateCommon(cfg.Fields)
}

// Start registers the pipeline's endpoint and drains its spool stream until
// the context is cancelled.
func (w *WebhookSource) Start(ctx context.Context, cfg ConnectorConfig, out chan<- Batch) error {
	if !cfg.Hub.Available() {
		return ErrNoSpool
	}
	pipelineID, _ := cfg.Fields["pipeline_id"].(string)
	authToken, _ := cfg.Fields["auth_token"].(string)

	recv := &webhookReceiver{authToken: authToken, hub: cfg.Hub}
	webhookRegistry.Store(pipelineID, recv)
	defer webhookRegistry.Delete(pipelineID)
	ep := &IngestEndpoint{PipelineID: pipelineID, Signal: "webhook"}
	cfg.Hub.Register(ep)
	defer cfg.Hub.Unregister(ep)

	slog.Info("Webhook source started", "pipeline", pipelineID)
	return drainSpool(ctx, cfg, out, true)
}

// ── Webhook HTTP integration ───────────────────────────────────────

// webhookRegistry maps pipeline IDs to active webhook receivers.
var webhookRegistry sync.Map

// webhookReceiver is a running webhook pipeline's endpoint.
type webhookReceiver struct {
	authToken string
	hub       *Hub

	// recent keeps the last webhookRecentMax accepted records for Test.
	recentMu sync.Mutex
	recent   []Record
}

// webhookRecentMax is how many received records a running webhook keeps
// for the Test preview.
const webhookRecentMax = 20

// webhookMaxBody bounds one webhook request body.
const webhookMaxBody = 10 << 20

func (r *webhookReceiver) remember(records []Record) {
	if over := len(records) - webhookRecentMax; over > 0 {
		records = records[over:]
	}
	r.recentMu.Lock()
	defer r.recentMu.Unlock()
	r.recent = append(r.recent, records...)
	if over := len(r.recent) - webhookRecentMax; over > 0 {
		r.recent = append([]Record(nil), r.recent[over:]...)
	}
}

func (r *webhookReceiver) recentRecords() []Record {
	r.recentMu.Lock()
	defer r.recentMu.Unlock()
	return append([]Record(nil), r.recent...)
}

// Sample returns the last records received while the pipeline runs. A
// webhook cannot be polled, so a stopped pipeline returns instructions.
func (w *WebhookSource) Sample(_ context.Context, cfg ConnectorConfig, limit int) (SampleResult, error) {
	pipelineID := stringField(cfg.Fields, "pipeline_id", "")
	val, ok := webhookRegistry.Load(pipelineID)
	if !ok {
		return SampleResult{Note: "A webhook has no data to read until it receives some. Run the pipeline, POST a few events to /api/pipelines/webhook/" + pipelineID + ", then test again to see the last " + fmt.Sprint(webhookRecentMax) + " received records."}, nil
	}
	recs := val.(*webhookReceiver).recentRecords()
	if len(recs) > limit {
		recs = recs[len(recs)-limit:]
	}
	out := make([]map[string]interface{}, 0, len(recs))
	for _, r := range recs {
		out = append(out, r.Data)
	}
	note := fmt.Sprintf("Last %d records received since the pipeline started.", len(out))
	if len(out) == 0 {
		note = "The pipeline is running but has received nothing yet. POST events to /api/pipelines/webhook/" + pipelineID + " and test again."
	}
	return SampleResult{Records: out, Note: note}, nil
}

// HandleWebhook is an HTTP handler that routes incoming webhook POSTs to the
// correct running pipeline. Mount at: POST /api/pipelines/webhook/{pipelineID}
//
// The request is accepted whole or not at all: 200 means every record is in
// the spool on disk.
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
	stats := recv.hub.Stats(pipelineID)

	// Authenticate (skip if no auth token configured)
	if recv.authToken != "" && !webhookTokenMatches(r, recv.authToken) {
		stats.RejectedAuth.Add(1)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	body, err := io.ReadAll(io.LimitReader(r.Body, webhookMaxBody+1))
	if err != nil {
		http.Error(w, "failed to read body", http.StatusBadRequest)
		return
	}
	if len(body) > webhookMaxBody {
		stats.RejectedTooLarge.Add(1)
		http.Error(w, "request body too large (max 10 MB)", http.StatusRequestEntityTooLarge)
		return
	}

	contentType := r.Header.Get("Content-Type")
	records, parseErr := parseWebhookBody(body, contentType)
	if parseErr != nil {
		stats.RejectedMalformed.Add(1)
		http.Error(w, fmt.Sprintf("parse error: %v", parseErr), http.StatusBadRequest)
		return
	}

	if status, msg := SpoolAppendStatus(recv.hub.Append(pipelineID, encodeRows(records)), stats); status != http.StatusOK {
		if status == http.StatusServiceUnavailable {
			w.Header().Set("Retry-After", "5")
		}
		http.Error(w, msg, status)
		return
	}
	stats.Accepted(int64(len(records)), int64(len(body)), time.Now())
	recv.remember(records)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	fmt.Fprintf(w, `{"accepted":%d}`, len(records))
}

// SpoolAppendStatus maps the result of Hub.Append onto an HTTP status and a
// message, counting the rejection. 200 means the data is durable.
func SpoolAppendStatus(err error, stats *IngestStats) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, spool.ErrFull):
		stats.RejectedSpoolFull.Add(1)
		return http.StatusServiceUnavailable, "ingest spool is full, try again later"
	case errors.Is(err, spool.ErrTooLarge):
		stats.RejectedTooLarge.Add(1)
		return http.StatusRequestEntityTooLarge, "request too large"
	default:
		slog.Warn("Ingest spool append failed", "error", err)
		return http.StatusServiceUnavailable, "ingest is unavailable, try again later"
	}
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

// EventsBlock parses an events body (a JSON object, an array of objects or
// NDJSON, as the webhook takes) into a JSONEachRow block for the spool, and
// returns the number of rows.
func EventsBlock(body []byte) ([]byte, int, error) {
	records, err := parseWebhookBody(body, "")
	if err != nil {
		return nil, 0, err
	}
	return encodeRows(records), len(records), nil
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
