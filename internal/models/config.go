package models

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Incremental strategies.
const (
	StrategyAppend       = "append"
	StrategyDeleteInsert = "delete_insert"
)

// maxExpressionLen bounds every user expression embedded in DDL.
const maxExpressionLen = 2000

// identPattern is a plain ClickHouse identifier (a column name).
var identPattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// BuildsTable reports whether the materialization creates a table (and so
// uses the engine, ORDER BY, PARTITION BY and engine arguments).
func BuildsTable(materialization string) bool {
	switch materialization {
	case MaterializationTable, MaterializationIncremental, MaterializationMaterializedView:
		return true
	}
	return false
}

// ValidateExpression checks that s can be embedded in DDL as one expression:
// no statement separator, no comment (which could swallow the rest of the
// statement), balanced parentheses and closed quotes. ClickHouse parses it
// later (EXPLAIN SYNTAX) before anything is built.
func ValidateExpression(label, s string) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	if len(s) > maxExpressionLen {
		return fmt.Errorf("%s is too long (max %d characters)", label, maxExpressionLen)
	}
	depth := 0
	for _, seg := range splitSQL(s) {
		switch seg.kind {
		case segComment:
			return fmt.Errorf("%s must not contain comments", label)
		case segQuoted:
			q := seg.text[0]
			if len(seg.text) < 2 || seg.text[len(seg.text)-1] != q {
				return fmt.Errorf("%s has an unterminated quote", label)
			}
		case segCode:
			for _, c := range seg.text {
				switch c {
				case ';':
					return fmt.Errorf("%s must be a single expression (no ';')", label)
				case '(':
					depth++
				case ')':
					depth--
					if depth < 0 {
						return fmt.Errorf("%s has unbalanced parentheses", label)
					}
				}
			}
		}
	}
	if depth != 0 {
		return fmt.Errorf("%s has unbalanced parentheses", label)
	}
	return nil
}

// splitColumns parses a comma-separated list of plain column names.
func splitColumns(label, s string) ([]string, error) {
	var out []string
	for _, part := range strings.Split(s, ",") {
		c := strings.TrimSpace(part)
		if c == "" {
			continue
		}
		if !identPattern.MatchString(c) {
			return nil, fmt.Errorf("%s: %q is not a column name", label, c)
		}
		out = append(out, c)
	}
	return out, nil
}

// NormalizeModel validates a model's materialization and the options that
// apply to it, and fills the table defaults. Options that do not apply to the
// materialization are kept as they are (switching back restores them) and
// ignored by runs. Call it before storing a model from any source.
func NormalizeModel(m *database.Model) error {
	m.Materialization = strings.TrimSpace(m.Materialization)
	if m.Materialization == "" {
		m.Materialization = MaterializationView
	}
	if err := ValidateMaterialization(m.Materialization); err != nil {
		return err
	}
	m.PartitionBy = strings.TrimSpace(m.PartitionBy)
	m.EngineArgs = strings.TrimSpace(m.EngineArgs)
	m.IncrementalStrategy = strings.TrimSpace(m.IncrementalStrategy)
	m.UniqueKey = strings.TrimSpace(m.UniqueKey)
	m.WatermarkColumn = strings.TrimSpace(m.WatermarkColumn)

	if !BuildsTable(m.Materialization) {
		return nil
	}
	m.TableEngine, m.OrderBy = TableDefaults(m.TableEngine, m.OrderBy)
	if !engineTakesOrderBy(m.TableEngine) {
		m.OrderBy = ""
		if m.PartitionBy != "" {
			return fmt.Errorf("PARTITION BY needs a MergeTree-family engine (got %s)", m.TableEngine)
		}
	}
	if m.EngineArgs != "" && strings.Contains(m.TableEngine, "(") {
		return fmt.Errorf("put the engine arguments either in the engine (%s) or in engine_args, not both", m.TableEngine)
	}
	for _, e := range []struct{ label, value string }{
		{"table engine", m.TableEngine},
		{"ORDER BY", m.OrderBy},
		{"PARTITION BY", m.PartitionBy},
		{"engine arguments", m.EngineArgs},
	} {
		if err := ValidateExpression(e.label, e.value); err != nil {
			return err
		}
	}

	if m.Materialization != MaterializationIncremental {
		return nil
	}
	if m.IncrementalStrategy == "" {
		m.IncrementalStrategy = StrategyAppend
	}
	switch m.IncrementalStrategy {
	case StrategyAppend:
	case StrategyDeleteInsert:
		keys, err := splitColumns("unique_key", m.UniqueKey)
		if err != nil {
			return err
		}
		if len(keys) == 0 {
			return fmt.Errorf("the delete_insert strategy needs a unique_key")
		}
		m.UniqueKey = strings.Join(keys, ", ")
	default:
		return fmt.Errorf("incremental_strategy must be 'append' or 'delete_insert' (got %q)", m.IncrementalStrategy)
	}
	if m.WatermarkColumn != "" && !identPattern.MatchString(m.WatermarkColumn) {
		return fmt.Errorf("watermark_column: %q is not a column name", m.WatermarkColumn)
	}
	return nil
}
