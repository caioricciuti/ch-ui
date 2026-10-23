package mcpserver

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"regexp"
	"strconv"
	"strings"
	"sync"
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

	// Batch limits for describe_table tables and run_select queries.
	maxBatchTables        = 10
	maxBatchQueries       = 5
	batchTableParallelism = 4

	maxHistoryQueryLen = 16 * 1024
)

// leadingCommentsRe strips leading whitespace, -- line comments and /* */
// block comments so the statement kind can be detected.
var leadingCommentsRe = regexp.MustCompile(`(?s)^(?:\s|--[^\n]*\n|/\*.*?\*/)*`)

// readOnlyRe matches the statement kinds the MCP server will run. Everything
// else is rejected before reaching ClickHouse; readonly=1 is enforced there as
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

// chResult is a gateway result with column order and server stats. Rows
// are kept exactly as ClickHouse sent them: decoding into maps would sort
// the columns alphabetically and push every number through float64.
type chResult struct {
	Rows    []json.RawMessage
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
	case json.Number:
		return x.String()
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
	return decodeRows(res.Rows, false)
}

// decodeRows decodes raw result rows into maps. exact keeps numbers as
// json.Number, so a Decimal or Int64 renders with every digit it came with.
func decodeRows(raw []json.RawMessage, exact bool) ([]map[string]any, error) {
	if raw == nil {
		return nil, nil
	}
	rows := make([]map[string]any, len(raw))
	for i, r := range raw {
		dec := json.NewDecoder(bytes.NewReader(r))
		if exact {
			dec.UseNumber()
		}
		if err := dec.Decode(&rows[i]); err != nil {
			return nil, fmt.Errorf("failed to decode result rows: %w", err)
		}
	}
	return rows, nil
}

// runCHFull is runCH keeping ClickHouse's column metadata and statistics.
func runCHFull(ctx context.Context, deps Deps, ak *authedKey, sql string, extra map[string]string, timeout time.Duration) (*chResult, error) {
	if !deps.Gateway.IsTunnelOnline(ak.key.ConnectionID) {
		return nil, fmt.Errorf("connection %q is offline: its agent/tunnel is not connected to CH-UI", ak.key.ConnectionID)
	}
	// readonly=1, not 2: the statement may not change any setting, so the
	// row, time and byte caps below cannot be lifted from inside the SQL.
	// The caps travel as request settings, which ClickHouse applies before
	// readonly takes effect.
	settings := map[string]string{
		"readonly":             "1",
		"result_overflow_mode": "break",
	}
	for k, v := range extra {
		settings[k] = v
	}
	ctx = tunnel.WithTag(ctx, ak.queryTag())
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
	return allowList(k.AllowedDatabases).set()
}

func dbAllowed(k *database.MCPKey, db string) bool {
	set := allowedDBs(k)
	return set == nil || set[db]
}

// actor is the person behind a key, for history and audit: the OAuth
// subject (the person who granted the token) when set, else the ClickHouse
// user the key runs as. Same rule as middleware.Actor for sessions.
func (ak *authedKey) actor() string {
	if s := strings.TrimSpace(ak.key.Subject); s != "" {
		return s
	}
	return ak.key.CHUser
}

// recordQuery writes the query into history (source=mcp) and the audit log,
// asynchronously — observability must never slow the query path.
func recordQuery(deps Deps, ak *authedKey, sql, status, errMsg string, elapsed time.Duration, rows int64) {
	background.Go(func() {
		deps.DB.CreateQueryHistoryEntry(database.CreateQueryHistoryParams{
			ConnectionID: ak.key.ConnectionID,
			User:         ak.key.CHUser,
			Actor:        ak.actor(),
			QueryText:    sanitizeForHistory(sql),
			Status:       status,
			ErrorMessage: errMsg,
			ElapsedMS:    elapsed.Milliseconds(),
			RowsReturned: rows,
			Source:       "mcp",
			MCPKeyID:     ak.key.ID,
		})
		user := ak.actor()
		chUser := ak.key.CHUser
		connID := ak.key.ConnectionID
		details := "mcp key: " + ak.key.Name
		deps.DB.CreateAuditLog(database.AuditLogParams{
			Action:         "mcp.query.execute",
			Username:       &user,
			ClickhouseUser: &chUser,
			ConnectionID:   &connID,
			Details:        &details,
		})
	})
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
	Database   string           `json:"database,omitempty" jsonschema:"the database the table lives in"`
	Table      string           `json:"table,omitempty" jsonschema:"the table to describe"`
	Tables     []describeTarget `json:"tables,omitempty" jsonschema:"describe several tables in one call (max 10) instead of database and table"`
	SampleRows int              `json:"sample_rows,omitempty" jsonschema:"number of sample rows to include per table (default 3, max 20, 0 to skip)"`
}

type describeTarget struct {
	Database string `json:"database"`
	Table    string `json:"table"`
}

type runSelectArgs struct {
	SQL     string   `json:"sql,omitempty" jsonschema:"the read-only SQL statement to run (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN)"`
	Queries []string `json:"queries,omitempty" jsonschema:"up to 5 independent read-only statements to run together instead of sql; each gets the same max_rows, format and max_bytes"`
	MaxRows int      `json:"max_rows,omitempty" jsonschema:"maximum rows to return (default 100, max 2000)"`
	Format  string   `json:"format,omitempty" jsonschema:"result format: json (default, rows as objects) or csv (header + rows, about half the tokens for wide results)"`
	// MaxBytes maps to ClickHouse max_bytes_to_read: the query is aborted
	// once it has read more uncompressed bytes than this. Use estimate_query
	// first to size it.
	MaxBytes int64 `json:"max_bytes,omitempty" jsonschema:"abort if the query reads more than this many uncompressed bytes (0 = no budget); size it with estimate_query"`
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
		Description: "Describe a table: columns, types, comments, and the CREATE statement's sorting/partition keys. Pass tables to describe up to 10 tables in one call.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args describeTableArgs) (*mcp.CallToolResult, any, error) {
		n := args.SampleRows
		if req.Params != nil && !argProvided(req, "sample_rows") {
			n = 3
		}
		if n > 20 {
			n = 20
		}
		if len(args.Tables) == 0 {
			if args.Database == "" || args.Table == "" {
				return errResult("database and table (or tables) are required"), nil, nil
			}
			out, msg := describeTable(ctx, deps, ak, args.Database, args.Table, n)
			if msg != "" {
				return errResult("%s", msg), nil, nil
			}
			return jsonResult(out), nil, nil
		}
		if args.Database != "" || args.Table != "" {
			return errResult("pass either database and table, or tables, not both"), nil, nil
		}
		if len(args.Tables) > maxBatchTables {
			return errResult("at most %d tables per call", maxBatchTables), nil, nil
		}
		// A bounded number of tables at a time: each one is four queries.
		results := make([]map[string]any, len(args.Tables))
		slots := make(chan struct{}, batchTableParallelism)
		var wg sync.WaitGroup
		for i, t := range args.Tables {
			wg.Go(func() {
				slots <- struct{}{}
				defer func() { <-slots }()
				out, msg := describeTable(ctx, deps, ak, t.Database, t.Table, n)
				if msg != "" {
					out = map[string]any{"database": t.Database, "table": t.Table, "error": msg}
				}
				results[i] = out
			})
		}
		wg.Wait()
		return jsonResult(map[string]any{"tables": results}), nil, nil
	})

	title, ann = readOnlyTool("Run read-only SQL")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_select",
		Title:       title,
		Annotations: ann,
		Description: "Run a read-only SQL statement (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN) and return the rows as JSON. Pass queries to run up to 5 independent statements together. Row-capped and time-limited; write statements are rejected and the session runs with readonly enforced. Every call is recorded in CH-UI query history and the audit log.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args runSelectArgs) (*mcp.CallToolResult, any, error) {
		q := readQuery{
			SQL: args.SQL, MaxRows: args.MaxRows, Format: args.Format, MaxBytes: args.MaxBytes,
			Endpoint: "/mcp/run_select",
		}
		if len(args.Queries) == 0 {
			return runReadQuery(ctx, deps, ak, q), nil, nil
		}
		if strings.TrimSpace(args.SQL) != "" {
			return errResult("pass either sql or queries, not both"), nil, nil
		}
		if len(args.Queries) > maxBatchQueries {
			return errResult("at most %d queries per call", maxBatchQueries), nil, nil
		}
		return runReadQueries(ctx, deps, ak, q, args.Queries), nil, nil
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
		if blocked := guardSQL(ctx, deps, ak, sql, nil); blocked != nil {
			return blocked, nil, nil
		}
		rows, err := runCH(ctx, deps, ak, "EXPLAIN indexes = 1\n"+sql+"\nFORMAT JSON", nil, metaTimeout)
		if err != nil {
			return errResult("explain failed: %v", err), nil, nil
		}
		return jsonResult(map[string]any{"plan": rows}), nil, nil
	})
}

// describeTable gathers one table's columns, table metadata, storage and
// sample rows. msg is set, and out nil, when the table cannot be described.
func describeTable(ctx context.Context, deps Deps, ak *authedKey, database, table string, n int) (out map[string]any, msg string) {
	if database == "" || table == "" {
		return nil, "database and table are required"
	}
	if !dbAllowed(ak.key, database) {
		return nil, fmt.Sprintf("database %q is not in this key's allowlist", database)
	}
	params := map[string]string{"param_db": database, "param_tbl": table}
	sampleSQL := fmt.Sprintf("SELECT * FROM %s.%s LIMIT %d FORMAT JSON", quoteIdent(database), quoteIdent(table), n)
	sampleBlocked := n > 0 && checkGuardrails(deps, ak, sampleSQL, "/mcp/describe_table") != nil

	// The four lookups are independent, so they share one round trip
	// through the tunnel instead of four.
	var (
		wg                 sync.WaitGroup
		cols, meta, parts  []map[string]any
		sample             []map[string]any
		colsErr, sampleErr error
	)
	wg.Go(func() {
		cols, colsErr = runCH(ctx, deps, ak,
			"SELECT name, type, default_kind, default_expression, comment, is_in_primary_key, is_in_sorting_key, is_in_partition_key, data_compressed_bytes, data_uncompressed_bytes FROM system.columns WHERE database = {db:String} AND table = {tbl:String} ORDER BY position FORMAT JSON",
			params, metaTimeout)
	})
	wg.Go(func() {
		meta, _ = runCH(ctx, deps, ak,
			"SELECT engine, partition_key, sorting_key, primary_key, total_rows, total_bytes, comment, create_table_query, metadata_modification_time FROM system.tables WHERE database = {db:String} AND name = {tbl:String} FORMAT JSON",
			params, metaTimeout)
	})
	// Parts give freshness and physical size; system.parts may be off
	// limits to a locked-down user, so this is best effort.
	wg.Go(func() {
		parts, _ = runCH(ctx, deps, ak,
			"SELECT count() AS active_parts, uniqExact(partition_id) AS partitions, sum(rows) AS rows, sum(data_compressed_bytes) AS compressed_bytes, sum(data_uncompressed_bytes) AS uncompressed_bytes, max(modification_time) AS last_modified FROM system.parts WHERE active AND database = {db:String} AND table = {tbl:String} FORMAT JSON",
			params, metaTimeout)
	})
	if n > 0 && !sampleBlocked {
		wg.Go(func() {
			sample, sampleErr = runCH(ctx, deps, ak, sampleSQL, map[string]string{"max_result_rows": strconv.Itoa(n), "max_execution_time": "10"}, metaTimeout)
		})
	}
	wg.Wait()

	if colsErr != nil {
		return nil, fmt.Sprintf("describe_table failed: %v", colsErr)
	}
	if len(cols) == 0 {
		return nil, fmt.Sprintf("table %s.%s not found", database, table)
	}
	out = map[string]any{"database": database, "table": table, "columns": cols}
	if len(meta) == 1 {
		out["table_info"] = meta[0]
	}
	if len(parts) == 1 {
		out["storage"] = parts[0]
	}
	switch {
	case n <= 0:
	case sampleBlocked:
		out["sample_note"] = "sample rows withheld by governance policy"
	case sampleErr != nil:
		out["sample_note"] = "sample rows unavailable: " + sampleErr.Error()
	default:
		out["sample_rows"] = sample
	}
	return out, ""
}

// runReadQueries runs independent statements together, each through
// runReadQuery with every guard and its own history entry. Each query's
// content blocks carry its query_index; a failed query reports its error
// in place without failing the others.
func runReadQueries(ctx context.Context, deps Deps, ak *authedKey, base readQuery, sqls []string) *mcp.CallToolResult {
	results := make([]*mcp.CallToolResult, len(sqls))
	var wg sync.WaitGroup
	for i, sql := range sqls {
		wg.Go(func() {
			q := base
			q.SQL = sql
			q.Meta = map[string]any{"query_index": i}
			results[i] = runReadQuery(ctx, deps, ak, q)
		})
	}
	wg.Wait()

	out := &mcp.CallToolResult{IsError: true}
	size := 0
	for i, r := range results {
		if r.IsError {
			out.Content = append(out.Content, &mcp.TextContent{Text: fmt.Sprintf("query %d failed: %s", i, resultText(r))})
		} else {
			out.IsError = false
			out.Content = append(out.Content, r.Content...)
		}
		size += len(resultText(r))
	}
	if size > maxResultBytes {
		return errResult("batch result too large (%d KB, cap %d KB): run fewer queries, lower max_rows, or aggregate", size/1024, maxResultBytes/1024)
	}
	return out
}

// resultText joins a result's text content blocks.
func resultText(r *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

// readQuery is the input of runReadQuery, shared by run_select and
// run_saved_query.
type readQuery struct {
	SQL      string
	MaxRows  int
	Format   string
	MaxBytes int64
	Extra    map[string]string // additional ClickHouse settings, e.g. param_*
	Endpoint string            // guardrail endpoint label
	Meta     map[string]any    // merged into the result envelope
}

// runReadQuery validates, executes and records a read-only statement and
// renders the result envelope. Every guard applies regardless of which tool
// the SQL came from.
func runReadQuery(ctx context.Context, deps Deps, ak *authedKey, q readQuery) *mcp.CallToolResult {
	sql := strings.TrimSpace(q.SQL)
	if sql == "" {
		return errResult("sql is required")
	}
	if !isReadOnlyStatement(sql) {
		return errResult("only read-only statements are allowed (SELECT, WITH, SHOW, DESCRIBE, EXPLAIN); INTO OUTFILE is rejected")
	}
	if trailingFormatRe.MatchString(sql) {
		return errResult("omit the FORMAT clause; results are always returned as JSON")
	}
	if blocked := guardSQL(ctx, deps, ak, sql, q.Extra); blocked != nil {
		return blocked
	}
	if blocked := checkGuardrails(deps, ak, sql, q.Endpoint); blocked != nil {
		return blocked
	}
	format := strings.ToLower(strings.TrimSpace(q.Format))
	if format == "" {
		format = "json"
	}
	if format != "json" && format != "csv" {
		return errResult("format must be json or csv")
	}
	maxRows := q.MaxRows
	if maxRows <= 0 {
		maxRows = defaultMaxRows
	}
	if maxRows > hardMaxRows {
		maxRows = hardMaxRows
	}
	// Ask for one row more than the cap: getting it back is how a result
	// that was cut is told apart from one of exactly maxRows rows.
	settings := map[string]string{
		"max_result_rows":    strconv.Itoa(maxRows + 1),
		"max_execution_time": strconv.Itoa(int(queryTimeout.Seconds())),
	}
	if q.MaxBytes > 0 {
		settings["max_bytes_to_read"] = strconv.FormatInt(q.MaxBytes, 10)
		settings["read_overflow_mode"] = "throw"
	}
	for k, v := range q.Extra {
		settings[k] = v
	}

	start := time.Now()
	res, err := runCHFull(ctx, deps, ak, sql, settings, queryTimeout)
	elapsed := time.Since(start)
	if err != nil {
		recordQuery(deps, ak, sql, "error", err.Error(), elapsed, 0)
		return errResult("query failed: %v", err)
	}
	rows := res.Rows
	// result_overflow_mode=break stops at block granularity, so ClickHouse
	// may hand back more than max_result_rows; trim so the cap is exact.
	truncated := len(rows) > maxRows
	if truncated {
		rows = rows[:maxRows]
	}
	recordQuery(deps, ak, sql, "success", "", elapsed, int64(len(rows)))

	meta := map[string]any{
		"row_count":  len(rows),
		"elapsed_ms": elapsed.Milliseconds(),
	}
	for k, v := range q.Meta {
		meta[k] = v
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
		decoded, err := decodeRows(rows, true)
		if err != nil {
			return errResult("%v", err)
		}
		body := toCSV(res.Columns, decoded)
		if len(body) > maxResultBytes {
			return errResult("result too large (%d KB, cap %d KB): select fewer columns, add filters, or aggregate", len(body)/1024, maxResultBytes/1024)
		}
		mb, _ := json.Marshal(meta)
		return &mcp.CallToolResult{Content: []mcp.Content{
			&mcp.TextContent{Text: body},
			&mcp.TextContent{Text: string(mb)},
		}}
	}
	if rows == nil {
		rows = []json.RawMessage{}
	}
	meta["rows"] = rows
	return jsonResult(meta)
}

// quoteIdent backtick-quotes a ClickHouse identifier. Backslashes are
// escaped first: ClickHouse reads \ escapes inside backticks, so a trailing
// backslash would otherwise escape the closing quote.
func quoteIdent(s string) string {
	return "`" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "`", "\\`") + "`"
}

// guardSQL holds the checks shared by every tool that sends agent-written
// SQL to ClickHouse: no SETTINGS clause of its own, and the key's database
// allow-list (see tablerefs.go). params are the query's param_* bindings.
func guardSQL(ctx context.Context, deps Deps, ak *authedKey, sql string, params map[string]string) *mcp.CallToolResult {
	if settingsClause(sql) {
		return errResult("remove the SETTINGS clause: CH-UI sets the limits for MCP queries and ClickHouse runs them with readonly=1, which refuses setting changes")
	}
	currentDB := func() (string, error) {
		rows, err := runCH(ctx, deps, ak, "SELECT currentDatabase() AS db FORMAT JSON", nil, metaTimeout)
		if err != nil {
			return "", err
		}
		if len(rows) != 1 {
			return "", fmt.Errorf("unexpected currentDatabase() result")
		}
		db, _ := rows[0]["db"].(string)
		return db, nil
	}
	if err := checkAllowedDatabases(currentDB, allowList(ak.key.AllowedDatabases), sql, params); err != nil {
		return errResult("%v", err)
	}
	return nil
}

// argProvided reports whether the raw call carried the named argument, so a
// zero value can be told apart from an omitted one.
func argProvided(req *mcp.CallToolRequest, name string) bool {
	if req == nil || req.Params == nil || len(req.Params.Arguments) == 0 {
		return false
	}
	var m map[string]json.RawMessage
	if json.Unmarshal(req.Params.Arguments, &m) != nil {
		return false
	}
	_, ok := m[name]
	return ok
}

func jsonUnmarshal(text string, v any) error { return json.Unmarshal([]byte(text), v) }
