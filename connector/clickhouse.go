package connector

import (
	"bufio"
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// CHClient handles ClickHouse query execution
type CHClient struct {
	baseURL    string
	sessionID  string
	transport  *http.Transport
	httpClient *http.Client
}

// NewCHClient creates a new ClickHouse HTTP client.
// A stable routing key is generated per client instance and sent as the
// X-CH-UI-Session header so that ClickHouse-aware load balancers (chproxy,
// HAProxy, etc.) can pin all requests from the same agent to the same node.
// We intentionally do NOT use ClickHouse's session_id URL parameter because it
// creates a server-side mutex that blocks concurrent queries.
func NewCHClient(baseURL string, insecureSkipVerify bool) *CHClient {
	transport := &http.Transport{
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		TLSClientConfig:       &tls.Config{InsecureSkipVerify: insecureSkipVerify},
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   10,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		ResponseHeaderTimeout: 5 * time.Minute,
		DisableKeepAlives:     false,
		ForceAttemptHTTP2:     true,
	}

	return &CHClient{
		baseURL:   strings.TrimSuffix(baseURL, "/"),
		sessionID: generateSessionID(),
		transport: transport,
		httpClient: &http.Client{
			Transport: transport,
			Timeout:   5 * time.Minute,
		},
	}
}

func generateSessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return "ch-ui-" + hex.EncodeToString(b)
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

// isTransientError checks if an error is a transient connection error that
// should be retried (e.g. server closed an idle keep-alive connection).
func isTransientError(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	if strings.Contains(s, "unexpected EOF") ||
		strings.Contains(s, "connection reset by peer") ||
		strings.Contains(s, "transport connection broken") ||
		strings.Contains(s, "use of closed network connection") {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return false // real timeouts should not be retried
	}
	return false
}

// doWithRetry executes an HTTP request, retrying once on transient connection errors.
func (c *CHClient) doWithRetry(req *http.Request, client *http.Client) (*http.Response, error) {
	resp, err := client.Do(req)
	if err != nil && isTransientError(err) {
		// Close any idle connections that may be stale, then retry once.
		c.transport.CloseIdleConnections()

		// Clone the request for retry (the body must be re-readable).
		retryReq := req.Clone(req.Context())
		if req.GetBody != nil {
			body, bodyErr := req.GetBody()
			if bodyErr != nil {
				return nil, err // return original error
			}
			retryReq.Body = body
		}
		return client.Do(retryReq)
	}
	return resp, err
}

// Execute runs a query against ClickHouse
func (c *CHClient) Execute(ctx context.Context, query, user, password string, settings map[string]string) (*QueryResult, error) {
	// Determine if this is a read or write query
	isWrite := isWriteQuery(query)
	hasFormat := hasFormatClause(query)

	// Build URL with parameters
	params := url.Values{}
	params.Set("default_format", "JSON")
	// Forward query settings and bind parameters (e.g. param_<name>) as URL params.
	for k, v := range settings {
		params.Set(k, v)
	}

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
	req.Header.Set("X-CH-UI-Session", c.sessionID)

	// GetBody allows doWithRetry to re-create the body on retry
	bodyStr := finalQuery
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(bodyStr)), nil
	}

	// Execute with retry on transient connection errors
	resp, err := c.doWithRetry(req, c.httpClient)
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
func (c *CHClient) ExecuteRaw(ctx context.Context, query, user, password, format string, settings map[string]string) (json.RawMessage, error) {
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
	// Forward query settings and bind parameters (e.g. param_<name>) as URL params.
	for k, v := range settings {
		params.Set(k, v)
	}
	fullURL := c.baseURL + "/?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-CH-UI-Session", c.sessionID)

	// GetBody allows doWithRetry to re-create the body on retry
	bodyStr := finalQuery
	req.GetBody = func() (io.ReadCloser, error) {
		return io.NopCloser(strings.NewReader(bodyStr)), nil
	}

	resp, err := c.doWithRetry(req, c.httpClient)
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
	Seq  int             `json:"seq"`
	Data json.RawMessage `json:"data"` // JSON array of arrays: [[v1,v2],[v3,v4],...]
}

// QueryProgress is a point-in-time snapshot of a running query, sampled from
// system.processes while the query streams. Zero values mean "not reported":
// total_rows_approx is 0 for sources ClickHouse cannot size up front.
type QueryProgress struct {
	ReadRows    uint64  `json:"read_rows"`
	ReadBytes   uint64  `json:"read_bytes"`
	TotalRows   uint64  `json:"total_rows"`
	MemoryUsage int64   `json:"memory_usage"`
	Elapsed     float64 `json:"elapsed"`
}

// progressPollInterval is how often system.processes is sampled for a running
// query. Each sample is a sub-millisecond system-table read.
const progressPollInterval = 300 * time.Millisecond

// isSafeQueryID reports whether an ID can be embedded in SQL as-is. Gateway IDs
// are UUIDs; anything else is refused rather than escaped, so the progress query
// can never carry a payload.
func isSafeQueryID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return false
		}
	}
	return true
}

// watchProgress samples a running query's progress until ctx is cancelled,
// calling onProgress for every snapshot that differs from the previous one.
// Progress is best-effort: a user without access to system.processes, or a
// balancer that routes the sample to another node, simply gets no updates.
func (c *CHClient) watchProgress(ctx context.Context, queryID, user, password string, onProgress func(QueryProgress)) {
	ticker := time.NewTicker(progressPollInterval)
	defer ticker.Stop()

	var last QueryProgress
	seen := false
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}

		p, ok, denied := c.fetchProgress(ctx, queryID, user, password)
		if denied {
			// The user may not read system.processes, and that will not change
			// while this query runs. Stop sampling instead of failing three
			// times a second for the rest of the query.
			return
		}
		if !ok {
			// The row is gone the moment the query finishes; keep sampling in
			// case this was a transient failure and the query is still running.
			continue
		}
		if seen && p == last {
			continue
		}
		last, seen = p, true
		onProgress(p)
	}
}

// fetchProgress reads one progress snapshot for queryID from system.processes.
// The second return value is false when no snapshot is available (the query is
// not running any more, or the sample failed). The third is true when the
// server refused the read, which no amount of retrying will fix.
func (c *CHClient) fetchProgress(ctx context.Context, queryID, user, password string) (QueryProgress, bool, bool) {
	var p QueryProgress

	sampleCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	sql := "SELECT read_rows, read_bytes, total_rows_approx, memory_usage, elapsed" +
		" FROM system.processes WHERE query_id = '" + queryID + "'"

	// log_queries=0 keeps the samples out of system.query_log so progress
	// polling does not pollute the user's own query history or insights.
	raw, err := c.ExecuteRaw(sampleCtx, sql, user, password, "JSONCompact", map[string]string{"log_queries": "0"})
	if err != nil {
		return p, false, isProgressDenied(err)
	}

	var resp struct {
		Data [][]json.RawMessage `json:"data"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil || len(resp.Data) == 0 || len(resp.Data[0]) < 5 {
		return p, false, false
	}

	row := resp.Data[0]
	p.ReadRows = jsonUint(row[0])
	p.ReadBytes = jsonUint(row[1])
	p.TotalRows = jsonUint(row[2])
	p.MemoryUsage = int64(jsonUint(row[3]))
	p.Elapsed = jsonFloat(row[4])
	return p, true, false
}

// isProgressDenied reports whether ClickHouse refused the progress read for a
// reason that will not change mid-query: the user lacks the grant on
// system.processes, or the table is not exposed at all. Anything else (a
// dropped connection, a timeout) is treated as transient and retried.
func isProgressDenied(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, marker := range []string{
		"ACCESS_DENIED",
		"Not enough privileges",
		"NOT_ENOUGH_PRIVILEGES",
		"UNKNOWN_TABLE",
		"UNKNOWN_DATABASE",
		"readonly mode",
		"READONLY",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// jsonUint parses a JSON number that ClickHouse may have quoted (UInt64 values
// are emitted as strings to survive JSON's 53-bit integer range).
func jsonUint(raw json.RawMessage) uint64 {
	n, err := strconv.ParseUint(strings.Trim(string(raw), `"`), 10, 64)
	if err != nil {
		return 0
	}
	return n
}

func jsonFloat(raw json.RawMessage) float64 {
	f, err := strconv.ParseFloat(strings.Trim(string(raw), `"`), 64)
	if err != nil {
		return 0
	}
	return f
}

// KillQuery asks ClickHouse to stop a running query. Closing the HTTP request
// is not enough on its own: a query that produces no output until it finishes
// never writes to the socket, so it never notices the client is gone.
func (c *CHClient) KillQuery(ctx context.Context, queryID, user, password string) error {
	if !isSafeQueryID(queryID) {
		return fmt.Errorf("unsafe query id")
	}

	killCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	sql := "KILL QUERY WHERE query_id = '" + queryID + "' ASYNC"
	_, err := c.ExecuteRaw(killCtx, sql, user, password, "JSONCompact", map[string]string{"log_queries": "0"})
	return err
}

// QuerySummary is ClickHouse's own accounting for a query, read from the
// X-ClickHouse-Summary response header. The header is written when the response
// headers are flushed, so it is exact for queries that produce their output at
// the end (aggregations) and a partial snapshot for queries that stream rows.
type QuerySummary struct {
	ReadRows  uint64
	ReadBytes uint64
	ElapsedNS uint64
}

// parseSummary reads X-ClickHouse-Summary from a response header set.
func parseSummary(h http.Header) (*QuerySummary, bool) {
	raw := h.Get("X-ClickHouse-Summary")
	if raw == "" {
		return nil, false
	}
	var payload struct {
		ReadRows  json.RawMessage `json:"read_rows"`
		ReadBytes json.RawMessage `json:"read_bytes"`
		ElapsedNS json.RawMessage `json:"elapsed_ns"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, false
	}
	return &QuerySummary{
		ReadRows:  jsonUint(payload.ReadRows),
		ReadBytes: jsonUint(payload.ReadBytes),
		ElapsedNS: jsonUint(payload.ElapsedNS),
	}, true
}

// ExecuteStreaming runs a query using JSONCompactEachRow format, reading the response
// line-by-line without buffering the entire result. It calls onMeta with column metadata,
// then onChunk for each batch of chunkSize rows, and returns ClickHouse's own
// summary of the run (nil when the server did not report one).
// When queryID is set and onProgress is non-nil, the query is tagged with that
// query_id and onProgress receives live progress sampled from system.processes.
func (c *CHClient) ExecuteStreaming(
	ctx context.Context,
	queryID string,
	query, user, password string,
	chunkSize int,
	settings map[string]string,
	onMeta func(meta json.RawMessage) error,
	onChunk func(seq int, data json.RawMessage) error,
	onProgress func(p QueryProgress),
) (*QuerySummary, int64, error) {
	isWrite := isWriteQuery(query)
	hasFormat := hasFormatClause(query)

	if chunkSize <= 0 {
		chunkSize = 5000
	}

	// Get column metadata via a LIMIT 0 query with JSONCompact, or send empty meta for writes
	if !isWrite && !hasFormat {
		metaResult, err := c.ExecuteRaw(ctx, buildMetaQuery(query), user, password, "JSONCompact", settings)
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
		// Use a newline so trailing -- line comments don't swallow the FORMAT clause
		finalQuery = strings.TrimRight(query, "; \n\t") + "\nFORMAT JSONCompactEachRow"
	}

	// Extract max_result_rows for precise client-side enforcement in the scanner loop.
	var maxRows int64
	if v, ok := settings["max_result_rows"]; ok {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
			maxRows = n
		}
	}

	params := url.Values{}
	params.Set("default_format", "JSON")
	params.Set("send_progress_in_http_headers", "0")
	// Tagging the query lets us sample its progress from system.processes.
	// ClickHouse reports progress as repeated HTTP header updates, which a Go
	// client cannot read until the response ends, so sampling is what works.
	trackProgress := onProgress != nil && isSafeQueryID(queryID)
	if trackProgress {
		params.Set("query_id", queryID)
	}
	// Pass settings as ClickHouse HTTP URL params for coarse server-side abort.
	// max_result_rows + result_overflow_mode=break causes ClickHouse to stop at block
	// granularity (~65k rows), preventing the server from doing unbounded work.
	// The scanner loop below enforces the exact row count on top of this.
	for k, v := range settings {
		params.Set(k, v)
	}
	fullURL := c.baseURL + "/?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return nil, 0, fmt.Errorf("failed to create request: %w", err)
	}
	if user != "" {
		req.SetBasicAuth(user, password)
	}
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-CH-UI-Session", c.sessionID)

	// Use a client without timeout for streaming (context controls cancellation)
	// but share the configured transport for proper TLS and connection management.
	streamClient := &http.Client{Transport: c.transport}

	if trackProgress {
		sampleCtx, stopSampling := context.WithCancel(ctx)
		defer stopSampling()
		go c.watchProgress(sampleCtx, queryID, user, password, onProgress)
	}

	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return nil, 0, fmt.Errorf("ClickHouse error: %s", string(body))
	}

	// ClickHouse reports its accounting in a response header, so it is available
	// before the body is read. Rows that stream out make it a partial snapshot;
	// the caller reconciles it with the sampled progress.
	summary, _ := parseSummary(resp.Header)

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

		// Enforce max_result_rows limit: break early (closes body, aborts ClickHouse query)
		if maxRows > 0 && totalRows >= maxRows {
			break
		}

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

	return summary, totalRows, nil
}

// TestConnection verifies connectivity and returns the ClickHouse version
func (c *CHClient) TestConnection(ctx context.Context, user, password string) (string, error) {
	query := "SELECT version() as version FORMAT JSON"

	result, err := c.Execute(ctx, query, user, password, nil)
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

// metaLimitRe matches an existing LIMIT clause, including negative limits/offsets
// (LIMIT -10 means "all but the last 10 rows" since ClickHouse 25.x).
var metaLimitRe = regexp.MustCompile(`(?i)\bLIMIT\s+-?\d+(\s*,\s*-?\d+)?(\s+OFFSET\s+-?\d+)?`)

// metaWrapRe matches statement kinds that can safely live inside an outer
// SELECT * FROM (...): SELECT, WITH, and parenthesized queries.
// The caller passes the query with leading whitespace/comments already stripped.
var metaWrapRe = regexp.MustCompile(`(?is)^\s*(?:SELECT|WITH)\b|^\s*\(`)

// metaSkipRe strips leading whitespace and whole-line -- comments so the
// statement kind can be detected regardless of how the query was formatted.
var metaSkipRe = regexp.MustCompile(`(?s)^(?:\s|--[^\n]*\n)*`)

// buildMetaQuery rewrites a query to return zero rows so column metadata can be
// fetched cheaply before streaming the real result.
//
// SELECT / WITH / parenthesized statements are wrapped in an outer subquery
// (SELECT * FROM (<query>) LIMIT 0). Wrapping is immune to the whole tail of
// textual cases a LIMIT rewrite keeps missing — LIMIT with an expression like
// 10*2, WITH TIES, negative limits/offsets, odd whitespace and comment
// placement — and ClickHouse short-circuits the outer LIMIT 0 to just the
// header anyway.
//
// Statements that cannot live inside a subquery (SHOW, DESCRIBE, INSERT, ...)
// keep the textual rewrite path.
func buildMetaQuery(query string) string {
	trimmed := strings.TrimRight(query, "; \n\t")
	head := metaSkipRe.ReplaceAllString(trimmed, "")
	if metaWrapRe.MatchString(head) {
		return "SELECT * FROM (\n" + trimmed + "\n) LIMIT 0"
	}
	if metaLimitRe.MatchString(trimmed) {
		return metaLimitRe.ReplaceAllString(trimmed, "LIMIT 0")
	}
	// Use a newline so that trailing -- line comments don't swallow the injected clause
	return trimmed + "\nLIMIT 0"
}

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
