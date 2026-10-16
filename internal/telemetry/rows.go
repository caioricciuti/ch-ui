package telemetry

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
)

// Gateway rows arrive as decoded JSON: numbers as float64 or json.Number,
// 64-bit integers as strings, Map and JSON columns as objects or as a JSON
// string of one. The helpers below read cells of any of those shapes, so the
// HTTP handlers and the MCP tools shape rows the same way.

// Str reads a cell as a string; whole numbers lose no digits.
func Str(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case json.Number:
		return t.String()
	case float64:
		return fmt.Sprintf("%.0f", t)
	default:
		return fmt.Sprintf("%v", t)
	}
}

// Int64 reads a cell as an int64; anything unreadable is 0.
func Int64(v interface{}) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case string:
		n, _ := strconv.ParseInt(t, 10, 64)
		return n
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		n, _ := strconv.ParseInt(fmt.Sprint(v), 10, 64)
		return n
	}
}

// Numeric reads a cell as a float64; false for nil, NaN, Inf and non-numbers.
func Numeric(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, false
		}
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	}
	return 0, false
}

// Float is Numeric with 0 for anything unreadable.
func Float(v interface{}) float64 {
	f, _ := Numeric(v)
	return f
}

// StringMap accepts a JSON object (Map column or the exporter's JSON
// column) or a JSON string of one. Nested objects, which is how a JSON
// column stores dotted OTel keys, are flattened back to dotted keys.
func StringMap(v interface{}) map[string]string {
	out := map[string]string{}
	switch t := v.(type) {
	case map[string]interface{}:
		flattenInto(out, "", t)
	case string:
		var m map[string]interface{}
		if json.Unmarshal([]byte(t), &m) == nil {
			flattenInto(out, "", m)
		}
	}
	return out
}

func flattenInto(out map[string]string, prefix string, m map[string]interface{}) {
	for k, val := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		if nested, ok := val.(map[string]interface{}); ok {
			flattenInto(out, key, nested)
			continue
		}
		out[key] = Str(val)
	}
}

// ShapeLogRows normalizes rows selected by SearchSQL, ContextSQL or
// ByTraceSQL into the log row contract.
func ShapeLogRows(rows []map[string]interface{}) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		out = append(out, map[string]interface{}{
			"timestamp":       Str(row["timestamp"]),
			"timestamp_ns":    Str(row["timestamp_ns"]),
			"severity":        Str(row["severity"]),
			"severity_number": Int64(row["severity_number"]),
			"service":         Str(row["service"]),
			"body":            Str(row["body"]),
			"trace_id":        Str(row["trace_id"]),
			"span_id":         Str(row["span_id"]),
			"resource":        StringMap(row["resource"]),
			"scope":           StringMap(row["scope"]),
			"attributes":      StringMap(row["attributes"]),
			"scope_name":      Str(row["scope_name"]),
			"event_name":      Str(row["event_name"]),
			"row_id":          Str(row["row_id"]),
		})
	}
	return out
}

// LogCursor is the keyset position of one SearchSQL row.
func LogCursor(row map[string]interface{}) string {
	return EncodeCursor(Cursor{TimestampNs: Str(row["timestamp_ns"]), TraceID: Str(row["trace_id"]),
		SpanID: Str(row["span_id"]), RowHash: Str(row["row_id"])})
}

// DescribeSQL is the DESCRIBE statement for a source table.
func DescribeSQL(db, table string) (string, error) {
	if !ValidIdent(db) || !ValidIdent(table) {
		return "", fmt.Errorf("invalid table name")
	}
	return fmt.Sprintf("DESCRIBE TABLE %s.%s", Quote(db), Quote(table)), nil
}

// ColumnTypes reads DESCRIBE rows into column name -> type.
func ColumnTypes(rows []map[string]interface{}) map[string]string {
	cols := make(map[string]string, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		typ, _ := row["type"].(string)
		if name != "" {
			cols[name] = typ
		}
	}
	return cols
}
