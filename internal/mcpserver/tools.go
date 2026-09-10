package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
)

const (
	metaTimeout  = 30 * time.Second
	queryTimeout = 60 * time.Second

	defaultMaxRows = 100
	hardMaxRows    = 2000

	defaultPageSize = 50
	hardMaxPageSize = 500

	// maxResultBytes caps the serialized rows returned to the client; MCP
	// results land in an LLM context window, not a data pipeline.
	maxResultBytes = 200 * 1024

	logComment = "ch-ui:mcp"

	maxHistoryQueryLen = 16 * 1024
)

// leadingCommentsRe strips leading whitespace, -- line comments and /* */
// block comments so the statement kind can be detected.
var leadingCommentsRe = regexp.MustCompile(`(?s)^(?:\s|--[^\n]*\n|/\*.*?\*/)*`)

// readOnlyRe matches the statement kinds the MCP server will run. Everything
// else is rejected before reaching ClickHouse; readonly=2 is enforced there as
// the real guard.
var readOnlyRe = regexp.MustCompile(`(?is)^(?:SELECT|WITH|SHOW|DESC|DESCRIBE|EXISTS|EXPLAIN)\b|^\(`)

// intoOutfileRe blocks SELECT ... INTO OUTFILE, which writes a file on the
// agent host. ClickHouse's readonly mode also rejects it; defense in depth.
var intoOutfileRe = regexp.MustCompile(`(?is)\bINTO\s+OUTFILE\b`)

// selectOnlyRe matches statements explain_query accepts.
var selectOnlyRe = regexp.MustCompile(`(?is)^(?:SELECT|WITH)\b|^\(`)

// trailingFormatRe detects an explicit FORMAT clause, which would break the
// JSON decoding of results; the MCP server always returns JSON.
var trailingFormatRe = regexp.MustCompile(`(?is)\bFORMAT\s+\w+\s*;?\s*$`)

// isReadOnlyStatement reports whether q is a statement the MCP server accepts.
func isReadOnlyStatement(q string) bool {
	head := leadingCommentsRe.ReplaceAllString(q, "")
	return readOnlyRe.MatchString(head) && !intoOutfileRe.MatchString(q)
}

// credentialFuncRe flags table functions that can carry credentials in string
// literals, mirroring the editor's history sanitizer.
var credentialFuncRe = regexp.MustCompile(`(?i)\b(s3|s3Cluster|remote|remoteSecure|mysql|postgresql|mongodb|azureBlobStorage|deltaLake|iceberg|hudi|url|jdbc|odbc)\s*\(`)
var stringLiteralRe = regexp.MustCompile(`'(?:[^'\\]|\\.)*'`)
var identifiedByRe = regexp.MustCompile(`(?i)(IDENTIFIED\s+(?:WITH\s+\w+\s+)?BY\s+)('(?:[^'\\]|\\.)*'|\S+)`)

func sanitizeForHistory(q string) string {
	s := identifiedByRe.ReplaceAllString(q, "${1}'[REDACTED]'")
	if credentialFuncRe.MatchString(s) {
		s = stringLiteralRe.ReplaceAllString(s, "'[REDACTED]'")
	}
	if len(s) > maxHistoryQueryLen {
		s = s[:maxHistoryQueryLen]
	}
	return s
}

// readOnlyTool returns the metadata for a tool that never modifies anything:
// title plus the spec's readOnlyHint/idempotentHint/openWorldHint. Clients use
// these to skip confirmation prompts, and connector directories require them.
func readOnlyTool(title string) (string, *mcp.ToolAnnotations) {
	closed := false
	return title, &mcp.ToolAnnotations{
		Title:          title,
		ReadOnlyHint:   true,
		IdempotentHint: true,
		OpenWorldHint:  &closed,
	}
}

// additiveTool returns the metadata for a tool that creates CH-UI entities but
// never deletes or overwrites: readOnlyHint=false, destructiveHint=false.
func additiveTool(title string) (string, *mcp.ToolAnnotations) {
	f := false
	return title, &mcp.ToolAnnotations{
		Title:           title,
		ReadOnlyHint:    false,
		DestructiveHint: &f,
		IdempotentHint:  false,
		OpenWorldHint:   &f,
	}
}

// errResult renders a tool-level error (IsError, not a protocol error) so the
// model can read it and correct course.
func errResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		IsError: true,
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
	}
}

func jsonResult(v any) *mcp.CallToolResult {
	b, err := json.Marshal(v)
	if err != nil {
		return errResult("failed to serialize result: %v", err)
	}
	if len(b) > maxResultBytes {
		return errResult("result too large (%d KB, cap %d KB): use a smaller page_size, select fewer columns, add filters, or aggregate", len(b)/1024, maxResultBytes/1024)
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// ---- pagination ----
//
// Every list tool returns at most page_size items and a next_cursor when more
// exist. Cursors are opaque to the client: base64url of either "name:<last>"
// (keyset, for system tables) or "off:<n>" (offset, for CH-UI's own lists).

func encodeCursor(kind, v string) string {
	return base64.RawURLEncoding.EncodeToString([]byte(kind + ":" + v))
}

func decodeCursor(c string) (kind, v string, err error) {
	if c == "" {
		return "", "", nil
	}
	b, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil {
		return "", "", fmt.Errorf("invalid cursor")
	}
	kind, v, ok := strings.Cut(string(b), ":")
	if !ok {
		return "", "", fmt.Errorf("invalid cursor")
	}
	return kind, v, nil
}

func clampPageSize(n int) int {
	if n <= 0 {
		return defaultPageSize
	}
	if n > hardMaxPageSize {
		return hardMaxPageSize
	}
	return n
}

// pageOf slices an in-memory list by an offset cursor and returns the page
// plus the next cursor ("" when this is the last page).
func pageOf(items []map[string]any, cursor string, pageSize int) ([]map[string]any, string, error) {
	kind, v, err := decodeCursor(cursor)
	if err != nil {
		return nil, "", err
	}
	offset := 0
	if kind == "off" {
		if offset, err = strconv.Atoi(v); err != nil || offset < 0 {
			return nil, "", fmt.Errorf("invalid cursor")
		}
	} else if kind != "" {
		return nil, "", fmt.Errorf("invalid cursor")
	}
	if offset >= len(items) {
		return []map[string]any{}, "", nil
	}
	end := offset + pageSize
	next := ""
	if end < len(items) {
		next = encodeCursor("off", strconv.Itoa(end))
	} else {
		end = len(items)
	}
	return items[offset:end], next, nil
}

// chColumn is one entry of ClickHouse's JSON "meta" array, which preserves
// column order (rows decode into maps and lose it).
type chColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// chStats is ClickHouse's JSON "statistics" block.
type chStats struct {
	Elapsed   float64 `json:"elapsed"`
	RowsRead  int64   `json:"rows_read"`
	BytesRead int64   `json:"bytes_read"`
}

// chResult is a decoded gateway result with column order and server stats.
type chResult struct {
	Rows    []map[string]any
	Columns []chColumn
	Stats   *chStats
}

// toCSV renders rows as RFC 4180 CSV with a header, in ClickHouse column
// order. Roughly half the tokens of the JSON envelope for wide results.
func toCSV(cols []chColumn, rows []map[string]any) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	header := make([]string, len(cols))
	for i, c := range cols {
		header[i] = c.Name
	}
	w.Write(header)
	rec := make([]string, len(cols))
	for _, r := range rows {
		for i, c := range cols {
			rec[i] = cellString(r[c.Name])
		}
		w.Write(rec)
	}
	w.Flush()
	return buf.String()
}

func cellString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case bool:
		return strconv.FormatBool(x)
	default:
		b, _ := json.Marshal(x)
		return string(b)
	}
}

// runCH executes sql through the tunnel gateway with the key's credentials and
// the given extra settings, returning decoded rows.
func runCH(ctx context.Context, deps Deps, ak *authedKey, sql string, extra map[string]string, timeout time.Duration) ([]map[string]any, error) {
	res, err := runCHFull(ctx, deps, ak, sql, extra, timeout)
	if err != nil {
		return nil, err
	}
	return res.Rows, nil
}

// runCHFull is runCH keeping ClickHouse's column metadata and statistics.
func runCHFull(ctx context.Context, deps Deps, ak *authedKey, sql string, extra map[string]string, timeout time.Duration) (*chResult, error) {
	if !deps.Gateway.IsTunnelOnline(ak.key.ConnectionID) {
		return nil, fmt.Errorf("connection %q is offline: its agent/tunnel is not connected to CH-UI", ak.key.ConnectionID)
	}
	settings := map[string]string{
		"readonly":             "2",
		"result_overflow_mode": "break",
		"log_comment":          logComment,
	}
	for k, v := range extra {
		settings[k] = v
	}
	result, err := deps.Gateway.ExecuteQueryWithSettingsCtx(ctx, ak.key.ConnectionID, sql, ak.key.CHUser, ak.chPassword, settings, timeout)
	if err != nil {
		return nil, err
	}
	out := &chResult{}
	if len(result.Data) > 0 {
		if err := json.Unmarshal(result.Data, &out.Rows); err != nil {
			return nil, fmt.Errorf("failed to decode result rows: %w", err)
		}
	}
	if len(result.Meta) > 0 {
		json.Unmarshal(result.Meta, &out.Columns) // best effort; rows still usable
	}
	if len(result.Stats) > 0 {
		var st chStats
		if json.Unmarshal(result.Stats, &st) == nil {
			out.Stats = &st
		}
	}
	return out, nil
}

// allowedDBs returns the key's database allowlist as a set; nil means
// unrestricted.
func allowedDBs(k *database.MCPKey) map[string]bool {
	raw := strings.TrimSpace(k.AllowedDatabases)
	if raw == "" {
		return nil
	}
	set := make(map[string]bool)
	for _, d := range strings.Split(raw, ",") {
		if d = strings.TrimSpace(d); d != "" {
			set[d] = true
		}
	}
	return set
}

func dbAllowed(k *database.MCPKey, db string) bool {
	set := allowedDBs(k)
	return set == nil || set[db]
}

// recordQuery writes the query into history (source=mcp) and the audit log,
// asynchronously — observability must never slow the query path.
func recordQuery(deps Deps, ak *authedKey, sql, status, errMsg string, elapsed time.Duration, rows int64) {
	go func() {
		deps.DB.CreateQueryHistoryEntry(database.CreateQueryHistoryParams{
			ConnectionID: ak.key.ConnectionID,
			User:         ak.key.CHUser,
			QueryText:    sanitizeForHistory(sql),
			Status:       status,
			ErrorMessage: errMsg,
			ElapsedMS:    elapsed.Milliseconds(),
			RowsReturned: rows,
			Source:       "mcp",
		})
		user := ak.key.CHUser
		connID := ak.key.ConnectionID
		details := "mcp key: " + ak.key.Name
		deps.DB.CreateAuditLog(database.AuditLogParams{
			Action:       "mcp.query.execute",
			Username:     &user,
			ConnectionID: &connID,
			Details:      &details,
		})
	}()
}

// checkGuardrails runs governance policies when available (Pro). A nil service
// or non-Pro license means no policy evaluation.
func checkGuardrails(deps Deps, ak *authedKey, sql, endpoint string) *mcp.CallToolResult {
	if deps.Guardrails == nil || deps.Config == nil || !deps.Config.IsPro() {
		return nil
	}
	decision, err := deps.Guardrails.EvaluateQuery(ak.key.ConnectionID, ak.key.CHUser, sql, endpoint)
	if err != nil || decision.Allowed {
		return nil // guardrails soft-fail open, same as the editor
	}
	b := decision.Block
	return errResult("query blocked by governance policy %q (severity %s): %s", b.PolicyName, b.Severity, b.Detail)
}

// ---- tool registration ----

type listTablesArgs struct {
	Database string `json:"database" jsonschema:"the database to list tables from"`
	Like     string `json:"like,omitempty" jsonschema:"optional SQL LIKE pattern on the table name, e.g. %events%"`
	PageSize int    `json:"page_size,omitempty" jsonschema:"tables per page (default 50, max 500)"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"opaque cursor from a previous next_cursor to fetch the next page"`
}

// pageArgs is the shared pagination input of CH-UI's own list tools.
type pageArgs struct {
	PageSize int    `json:"page_size,omitempty" jsonschema:"items per page (default 50, max 500)"`
	Cursor   string `json:"cursor,omitempty" jsonschema:"opaque cursor from a previous next_cursor to fetch the next page"`
}

type describeTableArgs struct {
	Database string `json:"database" jsonschema:"the database the table lives in"`
	Table    string `json:"table" jsonschema:"the table to describe"`
}

type runSelectArgs struct {
	SQL     string `json:"sql" jsonschema:"the read-only SQL statement to run (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN)"`
	MaxRows int    `json:"max_rows,omitempty" jsonschema:"maximum rows to return (default 100, max 2000)"`
	Format  string `json:"format,omitempty" jsonschema:"result format: json (default, rows as objects) or csv (header + rows, about half the tokens for wide results)"`
}

type explainArgs struct {
	SQL string `json:"sql" jsonschema:"the SELECT statement to explain"`
}

func registerFreeTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := readOnlyTool("List databases")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_databases",
		Title:       title,
		Annotations: ann,
		Description: "List the databases visible to this connection (filtered by the key's database allowlist).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		rows, err := runCH(ctx, deps, ak,
			"SELECT name, engine FROM system.databases WHERE name NOT IN ('system', 'INFORMATION_SCHEMA', 'information_schema') ORDER BY name FORMAT JSON",
			nil, metaTimeout)
		if err != nil {
			return errResult("list_databases failed: %v", err), nil, nil
		}
		set := allowedDBs(ak.key)
		out := rows[:0]
		for _, r := range rows {
			name, _ := r["name"].(string)
			if set == nil || set[name] {
				out = append(out, r)
			}
		}
		return jsonResult(map[string]any{"databases": out}), nil, nil
	})

	title, ann = readOnlyTool("List tables")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tables",
		Title:       title,
		Annotations: ann,
		Description: "List tables in a database with engine, row count and size on disk.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listTablesArgs) (*mcp.CallToolResult, any, error) {
		if args.Database == "" {
			return errResult("database is required"), nil, nil
		}
		if !dbAllowed(ak.key, args.Database) {
			return errResult("database %q is not in this key's allowlist", args.Database), nil, nil
		}
		kind, after, err := decodeCursor(args.Cursor)
		if err != nil || (kind != "" && kind != "name") {
			return errResult("invalid cursor; pass the next_cursor value from the previous page"), nil, nil
		}
		pageSize := clampPageSize(args.PageSize)
		like := strings.TrimSpace(args.Like)
		if like == "" {
			like = "%"
		}
		// Keyset pagination on name (unique per database); fetch one extra row
		// to know whether a next page exists.
		rows, err := runCH(ctx, deps, ak,
			"SELECT name, engine, total_rows, total_bytes, comment FROM system.tables WHERE database = {db:String} AND name > {after:String} AND name LIKE {like:String} ORDER BY name LIMIT {lim:UInt32} FORMAT JSON",
			map[string]string{"param_db": args.Database, "param_after": after, "param_like": like, "param_lim": strconv.Itoa(pageSize + 1)}, metaTimeout)
		if err != nil {
			return errResult("list_tables failed: %v", err), nil, nil
		}
		out := map[string]any{"database": args.Database}
		if len(rows) > pageSize {
			rows = rows[:pageSize]
			last, _ := rows[len(rows)-1]["name"].(string)
			out["next_cursor"] = encodeCursor("name", last)
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		out["tables"] = rows
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Describe table")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "describe_table",
		Title:       title,
		Annotations: ann,
		Description: "Describe a table: columns, types, comments, and the CREATE statement's sorting/partition keys.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args describeTableArgs) (*mcp.CallToolResult, any, error) {
		if args.Database == "" || args.Table == "" {
			return errResult("database and table are required"), nil, nil
		}
		if !dbAllowed(ak.key, args.Database) {
			return errResult("database %q is not in this key's allowlist", args.Database), nil, nil
		}
		cols, err := runCH(ctx, deps, ak,
			"SELECT name, type, default_kind, default_expression, comment, is_in_primary_key, is_in_sorting_key, is_in_partition_key FROM system.columns WHERE database = {db:String} AND table = {tbl:String} ORDER BY position FORMAT JSON",
			map[string]string{"param_db": args.Database, "param_tbl": args.Table}, metaTimeout)
		if err != nil {
			return errResult("describe_table failed: %v", err), nil, nil
		}
		if len(cols) == 0 {
			return errResult("table %s.%s not found", args.Database, args.Table), nil, nil
		}
		meta, err := runCH(ctx, deps, ak,
			"SELECT engine, partition_key, sorting_key, primary_key, total_rows, total_bytes FROM system.tables WHERE database = {db:String} AND name = {tbl:String} FORMAT JSON",
			map[string]string{"param_db": args.Database, "param_tbl": args.Table}, metaTimeout)
		out := map[string]any{"database": args.Database, "table": args.Table, "columns": cols}
		if err == nil && len(meta) == 1 {
			out["table_info"] = meta[0]
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Run read-only SQL")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_select",
		Title:       title,
		Annotations: ann,
		Description: "Run a read-only SQL statement (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN) and return the rows as JSON. Row-capped and time-limited; write statements are rejected and the session runs with readonly enforced. Every call is recorded in CH-UI query history and the audit log.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args runSelectArgs) (*mcp.CallToolResult, any, error) {
		sql := strings.TrimSpace(args.SQL)
		if sql == "" {
			return errResult("sql is required"), nil, nil
		}
		if !isReadOnlyStatement(sql) {
			return errResult("only read-only statements are allowed (SELECT, WITH, SHOW, DESCRIBE, EXPLAIN); INTO OUTFILE is rejected"), nil, nil
		}
		if trailingFormatRe.MatchString(sql) {
			return errResult("omit the FORMAT clause; results are always returned as JSON"), nil, nil
		}
		if blocked := checkGuardrails(deps, ak, sql, "/mcp/run_select"); blocked != nil {
			return blocked, nil, nil
		}

		maxRows := args.MaxRows
		if maxRows <= 0 {
			maxRows = defaultMaxRows
		}
		if maxRows > hardMaxRows {
			maxRows = hardMaxRows
		}
		settings := map[string]string{
			"max_result_rows":    strconv.Itoa(maxRows),
			"max_execution_time": strconv.Itoa(int(queryTimeout.Seconds())),
		}

		format := strings.ToLower(strings.TrimSpace(args.Format))
		if format == "" {
			format = "json"
		}
		if format != "json" && format != "csv" {
			return errResult("format must be json or csv"), nil, nil
		}

		start := time.Now()
		res, err := runCHFull(ctx, deps, ak, sql, settings, queryTimeout)
		elapsed := time.Since(start)
		if err != nil {
			recordQuery(deps, ak, sql, "error", err.Error(), elapsed, 0)
			return errResult("query failed: %v", err), nil, nil
		}
		rows := res.Rows
		// result_overflow_mode=break stops at block granularity, so ClickHouse
		// may hand back more than max_result_rows; trim so the cap is exact.
		truncated := len(rows) >= maxRows
		if len(rows) > maxRows {
			rows = rows[:maxRows]
		}
		recordQuery(deps, ak, sql, "success", "", elapsed, int64(len(rows)))

		meta := map[string]any{
			"row_count":  len(rows),
			"elapsed_ms": elapsed.Milliseconds(),
		}
		if len(res.Columns) > 0 {
			meta["columns"] = res.Columns
		}
		if res.Stats != nil {
			meta["stats"] = map[string]any{"rows_read": res.Stats.RowsRead, "bytes_read": res.Stats.BytesRead}
		}
		if truncated {
			meta["truncated"] = true
			meta["note"] = fmt.Sprintf("result capped at %d rows; add filters, aggregate, or raise max_rows (up to %d)", maxRows, hardMaxRows)
		}

		if format == "csv" {
			body := toCSV(res.Columns, rows)
			if len(body) > maxResultBytes {
				return errResult("result too large (%d KB, cap %d KB): select fewer columns, add filters, or aggregate", len(body)/1024, maxResultBytes/1024), nil, nil
			}
			mb, _ := json.Marshal(meta)
			return &mcp.CallToolResult{Content: []mcp.Content{
				&mcp.TextContent{Text: body},
				&mcp.TextContent{Text: string(mb)},
			}}, nil, nil
		}
		if rows == nil {
			rows = []map[string]any{}
		}
		meta["rows"] = rows
		return jsonResult(meta), nil, nil
	})

	title, ann = readOnlyTool("Explain query plan")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "explain_query",
		Title:       title,
		Annotations: ann,
		Description: "Show ClickHouse's execution plan for a SELECT (EXPLAIN indexes = 1), useful to understand index usage and full scans before running an expensive query.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args explainArgs) (*mcp.CallToolResult, any, error) {
		sql := strings.TrimSpace(args.SQL)
		if sql == "" {
			return errResult("sql is required"), nil, nil
		}
		head := leadingCommentsRe.ReplaceAllString(sql, "")
		if !selectOnlyRe.MatchString(head) {
			return errResult("explain_query accepts SELECT / WITH statements only"), nil, nil
		}
		if trailingFormatRe.MatchString(sql) {
			return errResult("omit the FORMAT clause"), nil, nil
		}
		rows, err := runCH(ctx, deps, ak, "EXPLAIN indexes = 1\n"+sql+"\nFORMAT JSON", nil, metaTimeout)
		if err != nil {
			return errResult("explain failed: %v", err), nil, nil
		}
		return jsonResult(map[string]any{"plan": rows}), nil, nil
	})
}
