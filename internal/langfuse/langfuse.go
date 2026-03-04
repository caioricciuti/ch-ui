package langfuse

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Config holds Langfuse connection settings.
type Config struct {
	PublicKey string
	SecretKey string
	BaseURL   string
}

// Enabled returns true when both keys are set.
func (c Config) Enabled() bool {
	return c.PublicKey != "" && c.SecretKey != ""
}

// NormalizeBaseURL ensures BaseURL has a sensible default and no trailing slash.
func (c *Config) NormalizeBaseURL() {
	c.BaseURL = strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if c.BaseURL == "" {
		c.BaseURL = "https://cloud.langfuse.com"
	}
}

// Usage holds token counts for a generation.
type Usage struct {
	Input  int `json:"input"`
	Output int `json:"output"`
	Total  int `json:"total"`
}

// TraceParams captures trace-level data.
type TraceParams struct {
	ID        string
	Name      string
	UserID    string
	SessionID string
	Input     interface{}
	Output    interface{}
	Release   string
	Metadata  map[string]string
	Tags      []string
}

// GenerationParams captures one LLM generation.
type GenerationParams struct {
	ID              string
	TraceID         string
	Name            string
	Model           string
	ModelParameters map[string]interface{}
	Input           interface{}
	Output          interface{}
	StartTime       time.Time
	EndTime         time.Time
	Usage           *Usage
	Level           string // "DEFAULT" or "ERROR"
}

// ScoreParams captures a score attached to a trace.
type ScoreParams struct {
	TraceID  string
	Name     string
	Value    float64
	Comment  string
	DataType string // "NUMERIC" or "BOOLEAN"
}

// EventParams captures a point-in-time event within a trace.
type EventParams struct {
	TraceID string
	Name    string
	Input   interface{}
	Level   string
}

type event struct {
	ID        string      `json:"id"`
	Type      string      `json:"type"`
	Timestamp string      `json:"timestamp"`
	Body      interface{} `json:"body"`
}

// Client sends observability events to Langfuse asynchronously.
// Always non-nil — inactive when config is not enabled.
type Client struct {
	mu     sync.RWMutex
	cfg    Config
	http   *http.Client
	events chan event
	stopCh chan struct{}
	wg     sync.WaitGroup
}

// New creates a Client. Always returns a valid pointer.
// The client is inactive until Reconfigure is called with valid credentials.
func New() *Client {
	return &Client{
		http:   &http.Client{Timeout: 10 * time.Second},
		events: make(chan event, 256),
		stopCh: make(chan struct{}),
	}
}

// Reconfigure swaps the config at runtime. Safe to call while the client is running.
func (c *Client) Reconfigure(cfg Config) {
	cfg.NormalizeBaseURL()
	c.mu.Lock()
	c.cfg = cfg
	c.mu.Unlock()
	if cfg.Enabled() {
		slog.Info("Langfuse observability configured", "base_url", cfg.BaseURL)
	} else {
		slog.Info("Langfuse observability disabled")
	}
}

// IsEnabled returns true if the client has valid credentials.
func (c *Client) IsEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg.Enabled()
}

func (c *Client) getConfig() Config {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.cfg
}

// Start spawns the background flush goroutine.
func (c *Client) Start() {
	c.wg.Add(1)
	go c.loop()
}

// Stop drains pending events and shuts down.
func (c *Client) Stop() {
	close(c.stopCh)
	c.wg.Wait()
	slog.Info("Langfuse client stopped")
}

func (c *Client) loop() {
	defer c.wg.Done()
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	var buf []event
	for {
		select {
		case e := <-c.events:
			buf = append(buf, e)
			if len(buf) >= 10 {
				c.flush(buf)
				buf = buf[:0]
			}
		case <-ticker.C:
			if len(buf) > 0 {
				c.flush(buf)
				buf = buf[:0]
			}
		case <-c.stopCh:
			for {
				select {
				case e := <-c.events:
					buf = append(buf, e)
				default:
					c.flush(buf)
					return
				}
			}
		}
	}
}

func (c *Client) enqueue(e event) {
	if !c.IsEnabled() {
		return
	}
	select {
	case c.events <- e:
	default:
		slog.Warn("langfuse event dropped, channel full")
	}
}

func now() string {
	return time.Now().UTC().Format(time.RFC3339Nano)
}

func newID() string {
	return uuid.NewString()
}

// LogTrace enqueues a trace-create event.
func (c *Client) LogTrace(p TraceParams) {
	id := p.ID
	if id == "" {
		id = newID()
	}
	body := map[string]interface{}{
		"id":   id,
		"name": p.Name,
	}
	if p.UserID != "" {
		body["userId"] = p.UserID
	}
	if p.SessionID != "" {
		body["sessionId"] = p.SessionID
	}
	if p.Input != nil {
		body["input"] = p.Input
	}
	if p.Output != nil {
		body["output"] = p.Output
	}
	if p.Release != "" {
		body["release"] = p.Release
	}
	if len(p.Metadata) > 0 {
		body["metadata"] = p.Metadata
	}
	if len(p.Tags) > 0 {
		body["tags"] = p.Tags
	}

	c.enqueue(event{
		ID:        id,
		Type:      "trace-create",
		Timestamp: now(),
		Body:      body,
	})
}

// LogGeneration enqueues a generation-create event.
func (c *Client) LogGeneration(p GenerationParams) {
	id := p.ID
	if id == "" {
		id = newID()
	}

	body := map[string]interface{}{
		"id":        id,
		"traceId":   p.TraceID,
		"name":      p.Name,
		"type":      "GENERATION",
		"model":     p.Model,
		"startTime": p.StartTime.UTC().Format(time.RFC3339Nano),
		"endTime":   p.EndTime.UTC().Format(time.RFC3339Nano),
	}
	if len(p.ModelParameters) > 0 {
		body["modelParameters"] = p.ModelParameters
	}
	if p.Input != nil {
		body["input"] = p.Input
	}
	if p.Output != nil {
		body["output"] = p.Output
	}
	if p.Usage != nil {
		body["usage"] = p.Usage
	}
	if p.Level != "" {
		body["level"] = p.Level
	}

	c.enqueue(event{
		ID:        id,
		Type:      "generation-create",
		Timestamp: now(),
		Body:      body,
	})
}

// LogScore enqueues a score-create event.
func (c *Client) LogScore(p ScoreParams) {
	dataType := p.DataType
	if dataType == "" {
		dataType = "NUMERIC"
	}
	body := map[string]interface{}{
		"traceId":  p.TraceID,
		"name":     p.Name,
		"value":    p.Value,
		"dataType": dataType,
	}
	if p.Comment != "" {
		body["comment"] = p.Comment
	}

	c.enqueue(event{
		ID:        newID(),
		Type:      "score-create",
		Timestamp: now(),
		Body:      body,
	})
}

// LogEvent enqueues an event-create for notable occurrences within a trace.
func (c *Client) LogEvent(p EventParams) {
	body := map[string]interface{}{
		"traceId": p.TraceID,
		"name":    p.Name,
	}
	if p.Input != nil {
		body["input"] = p.Input
	}
	if p.Level != "" {
		body["level"] = p.Level
	}

	c.enqueue(event{
		ID:        newID(),
		Type:      "event-create",
		Timestamp: now(),
		Body:      body,
	})
}

func (c *Client) flush(batch []event) {
	if len(batch) == 0 {
		return
	}

	cfg := c.getConfig()
	if !cfg.Enabled() {
		return
	}

	payload := map[string]interface{}{
		"batch": batch,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		slog.Warn("langfuse: failed to marshal batch", "error", err)
		return
	}

	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+"/api/public/ingestion", bytes.NewReader(body))
	if err != nil {
		slog.Warn("langfuse: failed to create request", "error", err)
		return
	}
	req.SetBasicAuth(cfg.PublicKey, cfg.SecretKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		slog.Warn("langfuse: flush failed", "error", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMultiStatus {
		slog.Warn("langfuse: unexpected status", "status", resp.StatusCode)
	}
}

// TestConnection verifies credentials by calling the Langfuse API.
// Returns nil on success, error with details on failure.
func (c *Client) TestConnection(cfg Config) error {
	cfg.NormalizeBaseURL()
	req, err := http.NewRequest(http.MethodGet, cfg.BaseURL+"/api/public/projects", nil)
	if err != nil {
		return err
	}
	req.SetBasicAuth(cfg.PublicKey, cfg.SecretKey)

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK {
		return nil
	}
	return &ConnectionError{StatusCode: resp.StatusCode}
}

// ConnectionError represents a Langfuse API error.
type ConnectionError struct {
	StatusCode int
}

func (e *ConnectionError) Error() string {
	switch e.StatusCode {
	case 401:
		return "invalid credentials"
	case 403:
		return "access denied"
	default:
		return "unexpected status: " + http.StatusText(e.StatusCode)
	}
}
