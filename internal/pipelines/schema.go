package pipelines

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// SchemaColumn is one column of the table the sink auto-creates.
type SchemaColumn struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// InferredSchema is the column types inferred from sample records plus
// suggested table keys.
type InferredSchema struct {
	Columns     []SchemaColumn `json:"columns"`
	OrderBy     string         `json:"order_by"`
	PartitionBy string         `json:"partition_by"`
	// JSONType says whether the server has the JSON type (25.3 and later),
	// which objects and the wide events attributes use.
	JSONType bool `json:"json_type"`
}

// maxSchemaColumns bounds a saved column list.
const maxSchemaColumns = 1000

// typeShape is a type name with an optional parenthesised argument list.
var typeShape = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*(\(.*\))?$`)

// validateColumnType accepts one ClickHouse data type such as
// Nullable(DateTime64(3)) or Enum8('a' = 1), and nothing that could add a
// DEFAULT, CODEC or another column to the generated CREATE TABLE.
func validateColumnType(t string) error {
	t = strings.TrimSpace(t)
	if t == "" {
		return fmt.Errorf("column type is empty")
	}
	if !typeShape.MatchString(t) {
		return fmt.Errorf("column type %q is not a single ClickHouse type", t)
	}
	return validateTypeExpr(t)
}

// validateTypeExpr is validateExpr without the clause keyword check, which
// would refuse legitimate named tuple fields. Brackets, quotes, comments and
// separators are still checked; the shape regexp keeps it one type.
func validateTypeExpr(t string) error {
	if len(t) > 500 {
		return fmt.Errorf("column type is too long")
	}
	depth := 0
	var quote rune
	escaped := false
	prev := rune(0)
	for _, c := range t {
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth < 0 {
				return fmt.Errorf("column type %q has unbalanced brackets", t)
			}
		case ';', '#', '\n', '\r':
			return fmt.Errorf("column type %q contains a forbidden character", t)
		case '-':
			if prev == '-' {
				return fmt.Errorf("column type %q contains a comment", t)
			}
		case '*':
			if prev == '/' {
				return fmt.Errorf("column type %q contains a comment", t)
			}
		}
		prev = c
	}
	if quote != 0 || depth != 0 {
		return fmt.Errorf("column type %q has unbalanced quotes or brackets", t)
	}
	// With brackets balanced, the top-level argument list must span from
	// the first '(' to the final ')': "String) DEFAULT (1" opens a second one.
	if i := strings.IndexByte(t, '('); i >= 0 && !closesAtEnd(t[i:]) {
		return fmt.Errorf("column type %q is not a single ClickHouse type", t)
	}
	return nil
}

// closesAtEnd reports whether the bracket opened at s[0] closes at the last
// byte of s, ignoring brackets inside quotes.
func closesAtEnd(s string) bool {
	depth := 0
	var quote byte
	escaped := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if quote != 0 {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == quote:
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"', '`':
			quote = c
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i == len(s)-1
			}
		}
	}
	return false
}

// schemaColumns reads and validates create_table_columns from a sink config.
// Missing or empty means "infer from the first batch".
func schemaColumns(fields map[string]interface{}) ([]SchemaColumn, error) {
	v, ok := fields["create_table_columns"]
	if !ok || v == nil {
		return nil, nil
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("create_table_columns: %w", err)
	}
	var cols []SchemaColumn
	if err := json.Unmarshal(raw, &cols); err != nil {
		return nil, fmt.Errorf("create_table_columns must be a list of {name, type}")
	}
	if len(cols) > maxSchemaColumns {
		return nil, fmt.Errorf("create_table_columns has more than %d columns", maxSchemaColumns)
	}
	seen := map[string]bool{}
	for i := range cols {
		cols[i].Name = strings.TrimSpace(cols[i].Name)
		cols[i].Type = strings.TrimSpace(cols[i].Type)
		if cols[i].Name == "" {
			return nil, fmt.Errorf("column %d has no name", i+1)
		}
		if seen[cols[i].Name] {
			return nil, fmt.Errorf("column %q appears twice", cols[i].Name)
		}
		seen[cols[i].Name] = true
		if err := validateColumnType(cols[i].Type); err != nil {
			return nil, err
		}
	}
	return cols, nil
}

var (
	timeName = regexp.MustCompile(`(?i)^(ts|time|timestamp|date|datetime|event_time|event_date|created|created_at|updated_at|inserted_at|occurred_at|_timestamp)$|(_at|_time|_ts|_date)$`)
	idName   = regexp.MustCompile(`(?i)^(id|uuid|key)$|(_id|_uuid|_key)$`)
)

// suggestKeys proposes ORDER BY (a timestamp-like and an id-like column) and
// PARTITION BY toYYYYMM(<timestamp>) when a date or time column exists.
// Nullable columns cannot be keys and are skipped.
func suggestKeys(cols []SchemaColumn) (orderBy, partitionBy string) {
	// The first date or time column, preferring one whose name looks like
	// a timestamp, and the first id-like scalar column.
	var ts, id string
	namedTS := false
	for _, c := range cols {
		if strings.Contains(c.Type, "Nullable(") {
			continue
		}
		if strings.HasPrefix(c.Type, "Date") && (ts == "" || !namedTS && timeName.MatchString(c.Name)) {
			ts, namedTS = c.Name, timeName.MatchString(c.Name)
		}
		if id == "" && idName.MatchString(c.Name) && isScalarType(c.Type) {
			id = c.Name
		}
	}
	var keys []string
	if ts != "" {
		keys = append(keys, quoteIdent(ts))
		partitionBy = "toYYYYMM(" + quoteIdent(ts) + ")"
	}
	if id != "" {
		keys = append(keys, quoteIdent(id))
	}
	switch len(keys) {
	case 0:
		orderBy = "tuple()"
	case 1:
		orderBy = keys[0]
	default:
		orderBy = "(" + strings.Join(keys, ", ") + ")"
	}
	return orderBy, partitionBy
}

// isScalarType reports whether a type can be a plain sorting key column.
func isScalarType(t string) bool {
	for _, p := range []string{"Array", "Map", "Tuple", "JSON", "Object", "Nested", "Variant", "Dynamic"} {
		if strings.HasPrefix(t, p) {
			return false
		}
	}
	return true
}
