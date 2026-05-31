package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func RegisterInsights(r *Registry) {
	r.Register(getInsights)
}

var getInsights = Tool{
	Name:        "get_insights",
	Description: "Profile a table in one pass: total row count plus per-column null count, approximate distinct count (uniqHLL12), min, and max. Use this when the user asks 'what's interesting about this table?' or wants an overview before writing queries. Caps at the first 25 columns.",
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
		db := strings.TrimSpace(input.Database)
		tbl := strings.TrimSpace(input.Table)
		if db == "" || tbl == "" {
			return nil, errors.New("database and table are required")
		}

		colsSQL := fmt.Sprintf(
			"SELECT name, type FROM system.columns WHERE database = '%s' AND table = '%s' ORDER BY position LIMIT 25",
			sqlEscape(db), sqlEscape(tbl),
		)
		cols, err := runSelect(tctx, colsSQL, 15*time.Second)
		if err != nil {
			return nil, err
		}
		if len(cols) == 0 {
			return nil, fmt.Errorf("table %s.%s not found", db, tbl)
		}

		var sb strings.Builder
		sb.WriteString("SELECT count() AS _row_count")
		for _, c := range cols {
			name, _ := c["name"].(string)
			typ, _ := c["type"].(string)
			esc := sqlEscapeBacktick(name)
			alias := safeAlias(name)
			sb.WriteString(fmt.Sprintf(", countIf(`%s` IS NULL) AS `%s__nulls`", esc, alias))
			sb.WriteString(fmt.Sprintf(", uniqHLL12(`%s`) AS `%s__distinct`", esc, alias))
			if supportsMinMax(typ) {
				sb.WriteString(fmt.Sprintf(", toString(min(`%s`)) AS `%s__min`", esc, alias))
				sb.WriteString(fmt.Sprintf(", toString(max(`%s`)) AS `%s__max`", esc, alias))
			}
		}
		sb.WriteString(fmt.Sprintf(" FROM `%s`.`%s`", sqlEscapeBacktick(db), sqlEscapeBacktick(tbl)))

		statsRows, err := runSelect(tctx, sb.String(), 60*time.Second)
		if err != nil {
			return nil, fmt.Errorf("compute insights: %w", err)
		}
		if len(statsRows) == 0 {
			return nil, errors.New("insights query returned no rows")
		}
		stats := statsRows[0]

		var rowCount any
		if v, ok := stats["_row_count"]; ok {
			rowCount = v
		}

		report := make([]map[string]any, 0, len(cols))
		for _, c := range cols {
			name, _ := c["name"].(string)
			typ, _ := c["type"].(string)
			alias := safeAlias(name)
			col := map[string]any{
				"name":            name,
				"type":            typ,
				"null_count":      stats[alias+"__nulls"],
				"distinct_approx": stats[alias+"__distinct"],
			}
			if supportsMinMax(typ) {
				col["min"] = stats[alias+"__min"]
				col["max"] = stats[alias+"__max"]
			}
			report = append(report, col)
		}

		return map[string]any{
			"database":  db,
			"table":     tbl,
			"row_count": rowCount,
			"columns":   report,
			"note":      "Distinct counts are approximate (uniqHLL12). Top-N values not included — ask run_query for distributions.",
		}, nil
	},
}

func supportsMinMax(typ string) bool {
	t := strings.ToLower(strings.TrimSpace(typ))
	if strings.HasPrefix(t, "map(") || strings.HasPrefix(t, "array(") || strings.HasPrefix(t, "tuple(") || strings.HasPrefix(t, "nested(") {
		return false
	}
	return true
}

func safeAlias(s string) string {
	var b strings.Builder
	for i, r := range s {
		if i >= 48 {
			break
		}
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "col"
	}
	return b.String()
}
