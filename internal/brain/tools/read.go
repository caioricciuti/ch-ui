package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

func RegisterRead(r *Registry) {
	r.Register(listTables)
	r.Register(describeTable)
	r.Register(runQuery)
}

var listTables = Tool{
	Name:        "list_tables",
	Description: "List tables in a ClickHouse database. If `database` is omitted, lists databases instead. Use this before describe_table when the user mentions a table you don't know about.",
	Parameters: mustJSON(map[string]any{
		"type": "object",
		"properties": map[string]any{
			"database": map[string]any{
				"type":        "string",
				"description": "Database name. Omit to list databases.",
			},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			Database string `json:"database"`
		}
		_ = json.Unmarshal(args, &input)
		input.Database = strings.TrimSpace(input.Database)

		if input.Database == "" {
			sql := "SELECT name FROM system.databases WHERE name NOT IN ('system','INFORMATION_SCHEMA','information_schema') ORDER BY name"
			rows, err := runSelect(tctx, sql, 20*time.Second)
			if err != nil {
				return nil, err
			}
			return map[string]any{"databases": flatten(rows, "name")}, nil
		}

		sql := fmt.Sprintf(
			"SELECT name, engine, total_rows, total_bytes FROM system.tables WHERE database = '%s' ORDER BY name",
			sqlEscape(input.Database),
		)
		rows, err := runSelect(tctx, sql, 20*time.Second)
		if err != nil {
			return nil, err
		}
		return map[string]any{"database": input.Database, "tables": rows}, nil
	},
}

var describeTable = Tool{
	Name:        "describe_table",
	Description: "Get a table's column list (name + type), partition + sorting keys, total row count, and a sample of up to 5 rows. Use this whenever you need to understand a table before writing SQL.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"database", "table"},
		"properties": map[string]any{
			"database": map[string]any{"type": "string"},
			"table":    map[string]any{"type": "string"},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			Database string `json:"database"`
			Table    string `json:"table"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		input.Database = strings.TrimSpace(input.Database)
		input.Table = strings.TrimSpace(input.Table)
		if input.Database == "" || input.Table == "" {
			return nil, errors.New("database and table are required")
		}

		colsSQL := fmt.Sprintf(
			"SELECT name, type, default_kind, is_in_partition_key, is_in_sorting_key FROM system.columns WHERE database = '%s' AND table = '%s' ORDER BY position",
			sqlEscape(input.Database), sqlEscape(input.Table),
		)
		cols, err := runSelect(tctx, colsSQL, 15*time.Second)
		if err != nil {
			return nil, err
		}
		if len(cols) == 0 {
			return nil, fmt.Errorf("table %s.%s not found", input.Database, input.Table)
		}

		metaSQL := fmt.Sprintf(
			"SELECT engine, total_rows, total_bytes, partition_key, sorting_key, primary_key FROM system.tables WHERE database = '%s' AND name = '%s'",
			sqlEscape(input.Database), sqlEscape(input.Table),
		)
		metaRows, err := runSelect(tctx, metaSQL, 10*time.Second)
		if err != nil {
			return nil, err
		}
		var meta map[string]any
		if len(metaRows) > 0 {
			meta = metaRows[0]
		}

		sampleSQL := fmt.Sprintf("SELECT * FROM `%s`.`%s` LIMIT 5",
			sqlEscapeBacktick(input.Database), sqlEscapeBacktick(input.Table))
		sample, err := runSelect(tctx, sampleSQL, 15*time.Second)
		if err != nil {
			sample = nil
		}

		return map[string]any{
			"database": input.Database,
			"table":    input.Table,
			"columns":  cols,
			"meta":     meta,
			"sample":   sample,
		}, nil
	},
}

var runQuery = Tool{
	Name:        "run_query",
	Description: "Execute a read-only ClickHouse SQL query (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN). The result is shown to the user as an artifact and returned to you so you can reason about it. A LIMIT clause is auto-injected (default 100, max 1000). Mutating statements are rejected.",
	Parameters: mustJSON(map[string]any{
		"type":     "object",
		"required": []string{"sql"},
		"properties": map[string]any{
			"sql": map[string]any{
				"type":        "string",
				"description": "The SQL to execute. Must be read-only.",
			},
			"limit": map[string]any{
				"type":        "integer",
				"description": "Max rows to return. Default 100, max 1000.",
				"minimum":     1,
				"maximum":     1000,
			},
		},
	}),
	Handler: func(tctx Context, args json.RawMessage) (any, error) {
		var input struct {
			SQL   string `json:"sql"`
			Limit int    `json:"limit"`
		}
		if err := json.Unmarshal(args, &input); err != nil {
			return nil, fmt.Errorf("invalid args: %w", err)
		}
		sql := strings.TrimSpace(input.SQL)
		if sql == "" {
			return nil, errors.New("sql is required")
		}
		if !isReadOnly(sql) {
			return nil, errors.New("only read-only queries are allowed (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN)")
		}
		limit := input.Limit
		if limit <= 0 {
			limit = 100
		}
		if limit > 1000 {
			limit = 1000
		}
		sql = injectLimit(sql, limit)

		result, err := tctx.Gateway.ExecuteQuery(tctx.ConnectionID, sql, tctx.CHUser, tctx.CHPassword, 30*time.Second)
		if err != nil {
			return nil, err
		}

		var artifactID string
		if tctx.DB != nil && tctx.ChatID != "" && tctx.MessageID != "" {
			payload, _ := json.Marshal(map[string]any{
				"query":      sql,
				"data":       json.RawMessage(result.Data),
				"meta":       json.RawMessage(result.Meta),
				"statistics": json.RawMessage(result.Stats),
			})
			title := "Query result"
			if id, aErr := tctx.DB.CreateBrainArtifact(tctx.ChatID, tctx.MessageID, "query_result", title, string(payload), tctx.Username); aErr == nil {
				artifactID = id
			}
		}

		out := map[string]any{
			"sql":   sql,
			"data":  json.RawMessage(result.Data),
			"meta":  json.RawMessage(result.Meta),
			"stats": json.RawMessage(result.Stats),
		}
		if artifactID != "" {
			out["artifact_id"] = artifactID
		}
		return out, nil
	},
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return b
}

func sqlEscape(s string) string {
	return strings.ReplaceAll(s, "'", "''")
}

func sqlEscapeBacktick(s string) string {
	return strings.ReplaceAll(s, "`", "``")
}

var readOnlyRE = regexp.MustCompile(`(?is)^\s*(SELECT|WITH|SHOW|DESC|DESCRIBE|EXPLAIN)\b`)

func isReadOnly(sql string) bool {
	return readOnlyRE.MatchString(sql)
}

var hasLimitRE = regexp.MustCompile(`(?is)\blimit\s+\d+`)

func injectLimit(sql string, limit int) string {
	trimmed := strings.TrimRight(sql, "; \t\n")
	if hasLimitRE.MatchString(trimmed) {
		return trimmed
	}
	return fmt.Sprintf("%s LIMIT %d", trimmed, limit)
}

func runSelect(tctx Context, sql string, timeout time.Duration) ([]map[string]any, error) {
	result, err := tctx.Gateway.ExecuteQuery(tctx.ConnectionID, sql, tctx.CHUser, tctx.CHPassword, timeout)
	if err != nil {
		return nil, err
	}

	var rows []map[string]any
	if err := json.Unmarshal(result.Data, &rows); err == nil {
		return rows, nil
	}

	var meta []struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(result.Meta, &meta); err != nil {
		return nil, fmt.Errorf("decode meta: %w", err)
	}
	var compact [][]any
	if err := json.Unmarshal(result.Data, &compact); err != nil {
		return nil, fmt.Errorf("decode rows: %w", err)
	}
	out := make([]map[string]any, 0, len(compact))
	for _, row := range compact {
		obj := make(map[string]any, len(meta))
		for i, col := range meta {
			if i < len(row) {
				obj[col.Name] = row[i]
			}
		}
		out = append(out, obj)
	}
	return out, nil
}

func flatten(rows []map[string]any, key string) []any {
	out := make([]any, 0, len(rows))
	for _, r := range rows {
		if v, ok := r[key]; ok {
			out = append(out, v)
		}
	}
	return out
}
