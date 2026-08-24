package mcpserver

import (
	"context"
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
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}
}

// runCH executes sql through the tunnel gateway with the key's credentials and
// the given extra settings, returning decoded rows.
func runCH(deps Deps, ak *authedKey, sql string, extra map[string]string, timeout time.Duration) ([]map[string]any, error) {
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
	result, err := deps.Gateway.ExecuteQueryWithSettings(ak.key.ConnectionID, sql, ak.key.CHUser, ak.chPassword, settings, timeout)
	if err != nil {
		return nil, err
	}
	var rows []map[string]any
	if len(result.Data) > 0 {
		if err := json.Unmarshal(result.Data, &rows); err != nil {
			return nil, fmt.Errorf("failed to decode result rows: %w", err)
		}
	}
	return rows, nil
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
}

type describeTableArgs struct {
	Database string `json:"database" jsonschema:"the database the table lives in"`
	Table    string `json:"table" jsonschema:"the table to describe"`
}

type runSelectArgs struct {
	SQL     string `json:"sql" jsonschema:"the read-only SQL statement to run (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN)"`
	MaxRows int    `json:"max_rows,omitempty" jsonschema:"maximum rows to return (default 100, max 2000)"`
}

type explainArgs struct {
	SQL string `json:"sql" jsonschema:"the SELECT statement to explain"`
}

func registerFreeTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_databases",
		Description: "List the databases visible to this connection (filtered by the key's database allowlist).",
	}, func(ctx context.Context, req *mcp.CallToolRequest, _ struct{}) (*mcp.CallToolResult, any, error) {
		rows, err := runCH(deps, ak,
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

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_tables",
		Description: "List tables in a database with engine, row count and size on disk.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listTablesArgs) (*mcp.CallToolResult, any, error) {
		if args.Database == "" {
			return errResult("database is required"), nil, nil
		}
		if !dbAllowed(ak.key, args.Database) {
			return errResult("database %q is not in this key's allowlist", args.Database), nil, nil
		}
		rows, err := runCH(deps, ak,
			"SELECT name, engine, total_rows, total_bytes, comment FROM system.tables WHERE database = {db:String} ORDER BY name FORMAT JSON",
			map[string]string{"param_db": args.Database}, metaTimeout)
		if err != nil {
			return errResult("list_tables failed: %v", err), nil, nil
		}
		return jsonResult(map[string]any{"database": args.Database, "tables": rows}), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "describe_table",
		Description: "Describe a table: columns, types, comments, and the CREATE statement's sorting/partition keys.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args describeTableArgs) (*mcp.CallToolResult, any, error) {
		if args.Database == "" || args.Table == "" {
			return errResult("database and table are required"), nil, nil
		}
		if !dbAllowed(ak.key, args.Database) {
			return errResult("database %q is not in this key's allowlist", args.Database), nil, nil
		}
		cols, err := runCH(deps, ak,
			"SELECT name, type, default_kind, default_expression, comment, is_in_primary_key, is_in_sorting_key, is_in_partition_key FROM system.columns WHERE database = {db:String} AND table = {tbl:String} ORDER BY position FORMAT JSON",
			map[string]string{"param_db": args.Database, "param_tbl": args.Table}, metaTimeout)
		if err != nil {
			return errResult("describe_table failed: %v", err), nil, nil
		}
		if len(cols) == 0 {
			return errResult("table %s.%s not found", args.Database, args.Table), nil, nil
		}
		meta, err := runCH(deps, ak,
			"SELECT engine, partition_key, sorting_key, primary_key, total_rows, total_bytes FROM system.tables WHERE database = {db:String} AND name = {tbl:String} FORMAT JSON",
			map[string]string{"param_db": args.Database, "param_tbl": args.Table}, metaTimeout)
		out := map[string]any{"database": args.Database, "table": args.Table, "columns": cols}
		if err == nil && len(meta) == 1 {
			out["table_info"] = meta[0]
		}
		return jsonResult(out), nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "run_select",
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

		start := time.Now()
		rows, err := runCH(deps, ak, sql, settings, queryTimeout)
		elapsed := time.Since(start)
		if err != nil {
			recordQuery(deps, ak, sql, "error", err.Error(), elapsed, 0)
			return errResult("query failed: %v", err), nil, nil
		}
		recordQuery(deps, ak, sql, "success", "", elapsed, int64(len(rows)))

		payload := map[string]any{
			"rows":       rows,
			"row_count":  len(rows),
			"elapsed_ms": elapsed.Milliseconds(),
		}
		if len(rows) >= maxRows {
			payload["truncated"] = true
			payload["note"] = fmt.Sprintf("result capped at %d rows; add filters or LIMIT for more precision", maxRows)
		}
		b, jerr := json.Marshal(payload)
		if jerr != nil {
			return errResult("failed to serialize result: %v", jerr), nil, nil
		}
		if len(b) > maxResultBytes {
			return errResult("result too large (%d KB, cap %d KB): select fewer columns, add filters, or aggregate", len(b)/1024, maxResultBytes/1024), nil, nil
		}
		return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: string(b)}}}, nil, nil
	})

	mcp.AddTool(srv, &mcp.Tool{
		Name:        "explain_query",
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
		rows, err := runCH(deps, ak, "EXPLAIN indexes = 1\n"+sql+"\nFORMAT JSON", nil, metaTimeout)
		if err != nil {
			return errResult("explain failed: %v", err), nil, nil
		}
		return jsonResult(map[string]any{"plan": rows}), nil, nil
	})
}
