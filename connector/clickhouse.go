package connector

import (
	"bufio"
	"bytes"
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

// setAuth authenticates a request with ClickHouse's own headers rather than
// HTTP Basic auth. Basic auth splits user and password at the first colon
// (RFC 7617), so a user named "sso:ana@example.com" could never sign in that
// way; the headers carry any name. ClickHouse, ClickHouse Cloud and chproxy
// all accept them.
func setAuth(req *http.Request, user, password string) {
	if user == "" {
		return
	}
	req.Header.Set("X-ClickHouse-User", user)
	req.Header.Set("X-ClickHouse-Key", password)
}

// Execute runs a query against ClickHouse
func (c *CHClient) Execute(ctx context.Context, query, user, password string, settings map[string]string) (*QueryResult, error) {
	// Determine if this is a read or write query
	isWrite := isWriteQuery(query)
	hasFormat := hasFormatClause(query) || rejectsFormat(query)

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
		// Use a newline so a trailing -- line comment cannot swallow the clause
		finalQuery = trimTrailingNoise(query) + "\nFORMAT JSON"
	}

	fullURL := c.baseURL + "/?" + params.Encode()

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set auth if provided
	setAuth(req, user, password)

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
		// DDL with ON CLUSTER answers with one status row per host in the
		// default JSON format. Keep those rows; any other write body is
		// ignored as before.
		if isWrite && len(body) > 0 && body[0] == '{' {
			var hosts QueryResult
			if json.Unmarshal(body, &hosts) == nil && len(hosts.Data) > 0 {
				return &hosts, nil
			}
		}
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
	hasFormat := hasFormatClause(query) || rejectsFormat(query)

	finalQuery := query
	if !isWrite && !hasFormat {
		if format == "" {
			format = "JSON"
		}
		finalQuery = trimTrailingNoise(query) + "\nFORMAT " + format
	}

	params := url.Values{}
	params.Set("default_format", "JSON")
	if rejectsFormat(query) && format != "" {
		params.Set("default_format", format)
	}
	// Forward query settings and bind parameters (e.g. param_<name>) as URL params.
	for k, v := range settings {
		params.Set(k, v)
	}
	fullURL := c.baseURL + "/?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	setAuth(req, user, password)
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
	ReadRows     uint64
	ReadBytes    uint64
	WrittenRows  uint64
	WrittenBytes uint64
	ElapsedNS    uint64
}

// parseSummary reads X-ClickHouse-Summary from a response header set.
func parseSummary(h http.Header) (*QuerySummary, bool) {
	raw := h.Get("X-ClickHouse-Summary")
	if raw == "" {
		return nil, false
	}
	var payload struct {
		ReadRows     json.RawMessage `json:"read_rows"`
		ReadBytes    json.RawMessage `json:"read_bytes"`
		WrittenRows  json.RawMessage `json:"written_rows"`
		WrittenBytes json.RawMessage `json:"written_bytes"`
		ElapsedNS    json.RawMessage `json:"elapsed_ns"`
	}
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return nil, false
	}
	return &QuerySummary{
		ReadRows:     jsonUint(payload.ReadRows),
		ReadBytes:    jsonUint(payload.ReadBytes),
		WrittenRows:  jsonUint(payload.WrittenRows),
		WrittenBytes: jsonUint(payload.WrittenBytes),
		ElapsedNS:    jsonUint(payload.ElapsedNS),
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
	hasFormat := hasFormatClause(query)
	probe := probeNone
	if !hasFormat {
		probe = metaProbeFor(query)
	}
	// A statement that is neither probed nor a known write runs once with a
	// format that carries its own header (CHECK TABLE, an unknown statement).
	headerInBody := probe == probeNone && !hasFormat && !isWriteQuery(query)

	if chunkSize <= 0 {
		chunkSize = 5000
	}

	// Get column metadata via a LIMIT 0 query with JSONCompact. Only
	// statements that certainly just read are probed: anything else would run
	// twice, and the probe of a statement that writes (REPLACE TABLE ... AS
	// SELECT, CREATE ... AS SELECT) would itself write.
	switch {
	case probe != probeNone:
		metaResult, err := c.ExecuteRaw(ctx, buildMetaQuery(query), user, password, "JSONCompact", settings)
		if err != nil && probe == probeIntrospect {
			// Some statements (SHOW CREATE, DESCRIBE, EXISTS) reject a LIMIT clause.
			// They are cheap, so read the header from the statement itself.
			metaResult, err = c.ExecuteRaw(ctx, query, user, password, "JSONCompact", settings)
		}
		if err != nil {
			return nil, 0, metaProbeError(err, probe == probeWrap)
		}
		var compact struct {
			Meta json.RawMessage `json:"meta"`
		}
		if err := json.Unmarshal(metaResult, &compact); err == nil && len(compact.Meta) > 0 {
			if err := onMeta(compact.Meta); err != nil {
				return nil, 0, err
			}
		}
	case !headerInBody:
		// Write queries: send empty meta so consumers always get exactly one meta message
		if err := onMeta(json.RawMessage("[]")); err != nil {
			return nil, 0, err
		}
	}

	// Now execute the actual query with JSONCompactEachRow for streaming
	finalQuery := query
	if probe != probeNone {
		// Trailing comments and semicolons go first, so the clause is neither
		// swallowed by a -- comment nor a second statement after a ';'.
		finalQuery = trimTrailingNoise(query) + "\nFORMAT JSONCompactEachRow"
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
	if headerInBody {
		params.Set("default_format", "JSONCompactEachRowWithNamesAndTypes")
	}
	params.Set("send_progress_in_http_headers", "0")
	// Tagging the query lets us sample its progress from system.processes.
	// ClickHouse reports progress as repeated HTTP header updates, which a Go
	// client cannot read until the response ends, so sampling is what works.
	trackProgress := onProgress != nil && isSafeQueryID(queryID)
	if trackProgress {
		// The same id lets the server find the run in system.query_log
		// afterwards for the inline profile.
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
	setAuth(req, user, password)
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
	// With the header in the body, the first two lines are names and types.
	var header [][]byte

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
		if headerInBody && len(header) < 2 {
			header = append(header, append([]byte(nil), line...))
			if len(header) == 2 {
				if err := onMeta(headerMeta(header[0], header[1])); err != nil {
					return nil, 0, err
				}
			}
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
	if headerInBody && len(header) < 2 {
		// The statement returned no result set.
		if err := onMeta(json.RawMessage("[]")); err != nil {
			return nil, totalRows, err
		}
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

// headerMeta turns the names and types lines of JSONCompactEachRowWithNamesAndTypes
// into the column list JSONCompact reports as meta.
func headerMeta(names, types []byte) json.RawMessage {
	var n, t []string
	if json.Unmarshal(names, &n) != nil || json.Unmarshal(types, &t) != nil || len(n) != len(t) {
		return json.RawMessage("[]")
	}
	meta := make([]ColumnMeta, len(n))
	for i := range n {
		meta[i] = ColumnMeta{Name: n[i], Type: t[i]}
	}
	out, _ := json.Marshal(meta)
	return out
}

// Export chunking. Output is forwarded in exportChunkSize pieces, and the last
// exportHoldback bytes are held until the response ends: ClickHouse reports an
// error that happens after the first bytes were sent by appending an
// "__exception__" block to the body (the status is already 200), so the tail
// is checked for it before it is forwarded.
const (
	exportChunkSize = 256 << 10
	exportHoldback  = 64 << 10
)

var (
	exportFormatRe     = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9]*$`)
	exportExceptionTag = []byte("__exception__")
	// exportFramingLineRe matches the "<size> <tag>" line newer servers write
	// after the exception message.
	exportFramingLineRe = regexp.MustCompile(`^\d+ \S+$`)
)

// ExecuteExport runs query with FORMAT format and passes the raw output to
// onChunk as it arrives. It returns the number of bytes forwarded. The query
// must not carry its own FORMAT clause. When queryID is safe it tags the query
// so a cancel can KILL it.
func (c *CHClient) ExecuteExport(
	ctx context.Context,
	queryID, query, user, password, format string,
	settings map[string]string,
	onChunk func(chunk []byte) error,
) (int64, error) {
	if !exportFormatRe.MatchString(format) {
		return 0, fmt.Errorf("invalid export format %q", format)
	}
	if hasFormatClause(query) {
		return 0, fmt.Errorf("the statement already has a FORMAT clause; remove it to export")
	}
	// A newline keeps a trailing -- comment from swallowing the clause.
	finalQuery := trimTrailingNoise(query) + "\nFORMAT " + format

	params := url.Values{}
	if isSafeQueryID(queryID) {
		params.Set("query_id", queryID)
	}
	for k, v := range settings {
		params.Set(k, v)
	}
	fullURL := c.baseURL + "/?" + params.Encode()

	req, err := http.NewRequestWithContext(ctx, "POST", fullURL, strings.NewReader(finalQuery))
	if err != nil {
		return 0, fmt.Errorf("failed to create request: %w", err)
	}
	setAuth(req, user, password)
	req.Header.Set("Content-Type", "text/plain")
	req.Header.Set("X-CH-UI-Session", c.sessionID)

	// No client timeout: an export can run long, the context cancels it.
	resp, err := (&http.Client{Transport: c.transport}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf("ClickHouse error: %s", strings.TrimSpace(string(body)))
	}

	var sent int64
	buf := make([]byte, 0, exportChunkSize+exportHoldback)
	read := make([]byte, 64<<10)
	for {
		n, readErr := resp.Body.Read(read)
		buf = append(buf, read[:n]...)
		if len(buf) >= exportChunkSize+exportHoldback {
			out := len(buf) - exportHoldback
			if err := onChunk(buf[:out]); err != nil {
				return sent, err
			}
			sent += int64(out)
			buf = append(buf[:0], buf[out:]...)
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			// ClickHouse may end the chunked body abruptly after writing the
			// exception block; report the exception rather than the cut.
			if msg, ok := exportTrailingException(buf); ok {
				return sent, fmt.Errorf("ClickHouse error: %s", msg)
			}
			return sent, fmt.Errorf("export read error: %w", readErr)
		}
	}

	if msg, ok := exportTrailingException(buf); ok {
		return sent, fmt.Errorf("ClickHouse error: %s", msg)
	}
	if len(buf) > 0 {
		if err := onChunk(buf); err != nil {
			return sent, err
		}
		sent += int64(len(buf))
	}
	return sent, nil
}

// exportTrailingException finds the exception block ClickHouse appends to a
// response that failed after it started, and returns its message.
func exportTrailingException(tail []byte) (string, bool) {
	for from := 0; ; {
		j := bytes.Index(tail[from:], exportExceptionTag)
		if j < 0 {
			return "", false
		}
		i := from + j
		from = i + len(exportExceptionTag)
		if i > 0 && tail[i-1] != '\n' {
			continue
		}
		// Newer servers put a tag line before the message and repeat the
		// marker after it; older ones write the message straight after.
		rest := string(tail[from:])
		if end := strings.Index(rest, string(exportExceptionTag)); end >= 0 {
			rest = rest[:end]
		}
		if idx := strings.Index(rest, "Code: "); idx >= 0 {
			msg := strings.TrimSpace(rest[idx:])
			// Drop the trailing "<size> <tag>" line of the newer framing.
			if nl := strings.LastIndex(msg, "\n"); nl >= 0 && exportFramingLineRe.MatchString(strings.TrimSpace(msg[nl:])) {
				msg = strings.TrimSpace(msg[:nl])
			}
			return msg, true
		}
	}
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
	// writeQueryPattern matches statements that return no result set. BACKUP,
	// RESTORE and CHECK do return one; ExecuteStreaming still never probes them.
	writeQueryPattern = regexp.MustCompile(`(?i)^\s*(INSERT|DELETE|UPDATE|CREATE|REPLACE|DROP|UNDROP|ALTER|TRUNCATE|RENAME|EXCHANGE|MOVE|ATTACH|DETACH|OPTIMIZE|GRANT|REVOKE|KILL|SYSTEM|SET|USE)\b`)
	// formatPattern matches a trailing FORMAT clause, optionally followed by a
	// SETTINGS clause (ClickHouse accepts both orders).
	formatPattern = regexp.MustCompile(`(?is)\bFORMAT\s+\w+(\s+SETTINGS\s+[^()]*)?$`)
	// noFormatPattern matches statements that take no FORMAT clause.
	noFormatPattern = regexp.MustCompile(`(?i)^\s*CHECK\s+GRANT\b`)
	// trailingSettingsRe matches a trailing SETTINGS clause (no parentheses after it).
	trailingSettingsRe = regexp.MustCompile(`(?is)\s+SETTINGS\s+[\w.]+\s*=[^()]*$`)
)

// metaLimitRe matches an existing LIMIT clause, including negative limits/offsets
// (LIMIT -10 means "all but the last 10 rows" since ClickHouse 25.x).
var metaLimitRe = regexp.MustCompile(`(?i)\bLIMIT\s+-?\d+(\s*,\s*-?\d+)?(\s+OFFSET\s+-?\d+)?`)

// buildMetaQuery rewrites a probed query (see metaProbeFor) to return zero
// rows so column metadata can be fetched cheaply before streaming the real
// result.
//
// SELECT / WITH / parenthesized statements are wrapped in an outer subquery
// (SELECT * FROM (<query>) LIMIT 0). Wrapping is immune to the whole tail of
// textual cases a LIMIT rewrite keeps missing (LIMIT with an expression like
// 10*2, WITH TIES, negative limits/offsets, odd whitespace and comment
// placement, a trailing SETTINGS clause) and ClickHouse short-circuits the
// outer LIMIT 0 to just the header anyway.
//
// Introspection statements (SHOW, DESCRIBE, ...) cannot live inside a
// subquery and keep the textual rewrite; LIMIT 0 goes before a trailing
// SETTINGS clause.
func buildMetaQuery(query string) string {
	trimmed := trimTrailingNoise(query)
	if metaProbeFor(trimmed) == probeWrap {
		return metaWrapPrefix + trimmed + "\n) LIMIT 0"
	}
	if metaLimitRe.MatchString(trimmed) {
		return metaLimitRe.ReplaceAllString(trimmed, "LIMIT 0")
	}
	if loc := trailingSettingsRe.FindStringIndex(trimmed); loc != nil {
		return trimmed[:loc[0]] + "\nLIMIT 0" + trimmed[loc[0]:]
	}
	// Use a newline so that trailing -- line comments don't swallow the injected clause
	return trimmed + "\nLIMIT 0"
}

// isCheapIntrospection reports whether a statement only reads metadata and
// is safe to run twice.
func isCheapIntrospection(query string) bool {
	return metaProbeFor(query) == probeIntrospect
}

func isWriteQuery(query string) bool {
	return writeQueryPattern.MatchString(stripLeadingComments(query))
}

func hasFormatClause(query string) bool {
	return formatPattern.MatchString(trimTrailingNoise(query))
}

// rejectsFormat reports statements ClickHouse refuses a FORMAT clause on,
// such as CHECK GRANT. They still return rows, shaped by default_format.
func rejectsFormat(query string) bool {
	return noFormatPattern.MatchString(stripLeadingComments(query))
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
