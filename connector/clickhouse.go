package connector

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

// CHClient handles ClickHouse query execution
type CHClient struct {
	baseURL    string
	httpClient *http.Client
}

// NewCHClient creates a new ClickHouse HTTP client
func NewCHClient(baseURL string) *CHClient {
	return &CHClient{
		baseURL: strings.TrimSuffix(baseURL, "/"),
		httpClient: &http.Client{
			Timeout: 5 * time.Minute, // Long timeout for large queries
		},
	}
}

// QueryResult holds the result of a query execution
type QueryResult struct {
	Data       []map[string]interface{} `json:"data"`
	Meta       []ColumnMeta             `json:"meta"`
	Rows       int                      `json:"rows"`
	Statistics struct {
		Elapsed   float64 `json:"elapsed"`
		RowsRead  uint64  `json:"rows_read"`
		BytesRead uint64  `json:"bytes_read"`
	} `json:"statistics"`
}

// ColumnMeta describes a column in the result
type ColumnMeta struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Execute runs a query against ClickHouse
func (c *CHClient) Execute(ctx context.Context, query, user, password string) (*QueryResult, error) {
	// Determine if this is a read or write query
	isWrite := isWriteQuery(query)
	hasFormat := hasFormatClause(query)

	// Build URL with parameters
	params := url.Values{}
	params.Set("default_format", "JSON")

	// For read queries without explicit FORMAT, add FORMAT JSON
	finalQuery := query
	if !isWrite && !hasFormat {
		finalQuery = strings.TrimRight(query, "; \n\t") + " FORMAT JSON"
	}

	fullURL := c.baseURL + "/?" + params.Encode()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set auth if provided
	if user != "" {
		req.SetBasicAuth(user, password)
	}

	req.Header.Set("Content-Type", "text/plain")

	// Execute
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for errors
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClickHouse error: %s", string(body))
	}

	// For write queries or queries with explicit format, we may get empty body
	if len(body) == 0 || (isWrite && !hasFormat) {
		return &QueryResult{
			Data: []map[string]interface{}{},
			Meta: []ColumnMeta{},
			Rows: 0,
		}, nil
	}

	// Parse JSON response
	var result QueryResult
	if err := json.Unmarshal(body, &result); err != nil {
		// If JSON parse fails but status was OK, treat as DDL success
		if isWrite {
			return &QueryResult{
				Data: []map[string]interface{}{},
				Meta: []ColumnMeta{},
				Rows: 0,
			}, nil
		}
		return nil, fmt.Errorf("failed to parse response: %w (body: %s)", err, truncate(string(body), 200))
	}

	return &result, nil
}

// ExecuteRaw runs a query and returns the raw ClickHouse response bytes without intermediate parsing.
// The format parameter controls the FORMAT clause appended to read queries (e.g. "JSONCompact").
func (c *CHClient) ExecuteRaw(ctx context.Context, query, user, password, format string) (json.RawMessage, error) {
	isWrite := isWriteQuery(query)
	hasFormat := hasFormatClause(query)

	finalQuery := query
	if !isWrite && !hasFormat {
		if format == "" {
			format = "JSON"
		}
		finalQuery = strings.TrimRight(query, "; \n\t") + " FORMAT " + format
	}

	params := url.Values{}
	params.Set("default_format", "JSON")
	fullURL := c.baseURL + "/?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	req.Header.Set("Content-Type", "text/plain")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("ClickHouse error: %s", string(body))
	}

	if len(body) == 0 || (isWrite && !hasFormat) {
		return json.RawMessage(`{"data":[],"meta":[],"rows":0}`), nil
	}

	return json.RawMessage(body), nil
}

// StreamChunk holds a batch of rows for streaming execution.
type StreamChunk struct {
	Seq  int               `json:"seq"`
	Data json.RawMessage   `json:"data"` // JSON array of arrays: [[v1,v2],[v3,v4],...]
}

// ExecuteStreaming runs a query using JSONCompactEachRow format, reading the response
// line-by-line without buffering the entire result. It calls onMeta with column metadata,
// then onChunk for each batch of chunkSize rows, and returns final statistics.
func (c *CHClient) ExecuteStreaming(
	ctx context.Context,
	query, user, password string,
	chunkSize int,
	onMeta func(meta json.RawMessage) error,
	onChunk func(seq int, data json.RawMessage) error,
) (*json.RawMessage, int64, error) {
	isWrite := isWriteQuery(query)
	hasFormat := hasFormatClause(query)

	if chunkSize <= 0 {
		chunkSize = 5000
	}

	// Get column metadata via a LIMIT 0 query with JSONCompact, or send empty meta for writes
	if !isWrite && !hasFormat {
		trimmed := strings.TrimRight(query, "; \n\t")
		var metaQuery string
		if limitRe := regexp.MustCompile(`(?i)\bLIMIT\s+\d+(\s*,\s*\d+)?(\s+OFFSET\s+\d+)?`); limitRe.MatchString(trimmed) {
			metaQuery = limitRe.ReplaceAllString(trimmed, "LIMIT 0")
		} else {
			metaQuery = trimmed + " LIMIT 0"
		}
		metaResult, err := c.ExecuteRaw(ctx, metaQuery, user, password, "JSONCompact")
		if err != nil {
			return nil, 0, fmt.Errorf("metadata query failed: %w", err)
		}
		var compact struct {
			Meta json.RawMessage `json:"meta"`
		}
		if err := json.Unmarshal(metaResult, &compact); err == nil && len(compact.Meta) > 0 {
			if err := onMeta(compact.Meta); err != nil {
				return nil, 0, err
			}
		}
	} else {
		// Write queries: send empty meta so consumers always get exactly one meta message
		if err := onMeta(json.RawMessage("[]")); err != nil {
			return nil, 0, err
		}
	}

	// Now execute the actual query with JSONCompactEachRow for streaming
	finalQuery := query
	if !isWrite && !hasFormat {
		finalQuery = strings.TrimRight(query, "; \n\t") + " FORMAT JSONCompactEachRow"
	}

	params := url.Values{}
	params.Set("default_format", "JSON")
	// Request stats in HTTP header so we get them without parsing JSON
	params.Set("send_progress_in_http_headers", "0")
	fullURL := c.baseURL + "/?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	req.Header.Set("Content-Type", "text/plain")

	// Use a client without timeout for streaming (context controls cancellation)
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, 0, fmt.Errorf("ClickHouse error: %s", string(body))
	}

	// Read line by line, accumulate chunks
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024) // 10MB max line

	var batch []json.RawMessage
	seq := 0
	var totalRows int64

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return nil, totalRows, ctx.Err()
		default:
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		// Each line is a JSON array: [v1, v2, v3]
		row := make(json.RawMessage, len(line))
		copy(row, line)
		batch = append(batch, row)
		totalRows++

		if len(batch) >= chunkSize {
			chunkData, _ := json.Marshal(batch)
			if err := onChunk(seq, chunkData); err != nil {
				return nil, totalRows, err
			}
			batch = batch[:0]
			seq++
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, totalRows, fmt.Errorf("stream read error: %w", err)
	}

	// Flush remaining rows
	if len(batch) > 0 {
		chunkData, _ := json.Marshal(batch)
		if err := onChunk(seq, chunkData); err != nil {
			return nil, totalRows, err
		}
	}

	// We don't get statistics from JSONCompactEachRow format directly.
	// Return nil stats — the server can compute elapsed time itself.
	return nil, totalRows, nil
}

// TestConnection verifies connectivity and returns the ClickHouse version
func (c *CHClient) TestConnection(ctx context.Context, user, password string) (string, error) {
	query := "SELECT version() as version FORMAT JSON"

	result, err := c.Execute(ctx, query, user, password)
	if err != nil {
		return "", err
	}

	if len(result.Data) > 0 {
		if v, ok := result.Data[0]["version"]; ok {
			return fmt.Sprintf("%v", v), nil
		}
	}

	return "unknown", nil
}

// Query patterns
var (
	writeQueryPattern = regexp.MustCompile(`(?i)^\s*(INSERT|CREATE|DROP|ALTER|TRUNCATE|RENAME|ATTACH|DETACH|OPTIMIZE|GRANT|REVOKE|KILL|SYSTEM|SET|USE)`)
	formatPattern     = regexp.MustCompile(`(?i)\bFORMAT\s+\w+\s*$`)
	commentPattern    = regexp.MustCompile(`(?m)^\s*--.*$`)
)

func isWriteQuery(query string) bool {
	// Strip leading comments
	stripped := commentPattern.ReplaceAllString(query, "")
	stripped = strings.TrimSpace(stripped)
	return writeQueryPattern.MatchString(stripped)
}

func hasFormatClause(query string) bool {
	return formatPattern.MatchString(strings.TrimSpace(query))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
