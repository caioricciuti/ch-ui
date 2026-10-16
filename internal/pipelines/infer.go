package pipelines

import (
	"encoding/json"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// InferSampleLimit is how many records column types are inferred from.
const InferSampleLimit = 100

// LowCardinality(String) is suggested for a string column with at most
// lowCardRatio distinct values per non-null value, when the sample has at
// least lowCardMinRows records.
const (
	lowCardRatio   = 0.1
	lowCardMinRows = 20
	// distinctCap bounds the distinct strings kept per column.
	distinctCap = 1000
)

// isoDateTime matches ISO 8601 / RFC 3339 date-times (a space or T between
// date and time, optional seconds, fraction and zone).
var isoDateTime = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}[T ]\d{2}:\d{2}(:\d{2}(\.\d{1,9})?)?(Z|[+-]\d{2}:?\d{2})?$`)

// Value kinds seen for a column.
const (
	kindInt = 1 << iota
	kindBigInt
	kindFloat
	kindBool
	kindString
	kindDateTime
	kindArray
	kindObject
)

// columnStats accumulates what a column's sampled values look like.
type columnStats struct {
	kinds    int
	nulls    int
	values   int // non-null values
	distinct map[string]struct{}
	// elems are the kinds of every non-null array element; nested marks
	// an element that is an array or object.
	elems  int
	nested bool
	// objStrings stays true while every object value seen is a string.
	objStrings bool
}

// valueKind classifies a decoded JSON value (json.Number or float64 for
// numbers). It returns 0 for nil.
func valueKind(v interface{}) int {
	switch x := v.(type) {
	case nil:
		return 0
	case bool:
		return kindBool
	case string:
		if isoDateTime.MatchString(x) {
			return kindDateTime
		}
		return kindString
	case json.Number:
		s := x.String()
		if strings.ContainsAny(s, ".eE") {
			return kindFloat
		}
		if _, err := strconv.ParseInt(s, 10, 64); err == nil {
			return kindInt
		}
		return kindBigInt
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return kindInt
		}
		return kindFloat
	case []interface{}:
		return kindArray
	case map[string]interface{}:
		return kindObject
	default:
		return kindString
	}
}

func (c *columnStats) add(v interface{}) {
	k := valueKind(v)
	if k == 0 {
		c.nulls++
		return
	}
	c.values++
	c.kinds |= k
	switch x := v.(type) {
	case string:
		if c.distinct == nil {
			c.distinct = map[string]struct{}{}
		}
		if len(c.distinct) <= distinctCap {
			c.distinct[x] = struct{}{}
		}
	case []interface{}:
		for _, e := range x {
			ek := valueKind(e)
			if ek == kindArray || ek == kindObject {
				c.nested = true
			}
			c.elems |= ek
		}
	case map[string]interface{}:
		for _, e := range x {
			if _, ok := e.(string); !ok {
				c.objStrings = false
			}
		}
	}
}

// scalarType maps a set of scalar kinds to one ClickHouse type, or "" when
// they do not share one.
func scalarType(kinds int) string {
	switch kinds {
	case kindInt:
		return "Int64"
	case kindFloat, kindInt | kindFloat:
		return "Float64"
	case kindBool:
		return "Bool"
	case kindDateTime:
		return "DateTime64(3)"
	case kindString, kindString | kindDateTime, kindBigInt, kindInt | kindBigInt:
		return "String"
	}
	return ""
}

// columnType picks the column's type from its stats over rows sampled
// records. jsonType says whether the server has the JSON type.
func (c *columnStats) columnType(rows int, jsonType bool) string {
	if c.values == 0 {
		return "Nullable(String)"
	}
	var t string
	switch c.kinds {
	case kindArray:
		elem := "String"
		if c.elems != 0 {
			elem = scalarType(c.elems)
		}
		if c.nested || elem == "" {
			return "String" // stored as JSON text
		}
		if elem == "DateTime64(3)" {
			elem = "String"
		}
		return "Array(" + elem + ")"
	case kindObject:
		switch {
		case jsonType:
			return "JSON"
		case c.objStrings:
			return "Map(String, String)"
		}
		return "String" // stored as JSON text
	default:
		t = scalarType(c.kinds)
		if t == "" {
			t = "String" // mixed types
		}
	}
	lowCard := t == "String" && c.kinds&(kindBigInt|kindInt|kindFloat|kindBool|kindArray|kindObject) == 0 &&
		rows >= lowCardMinRows && len(c.distinct) <= distinctCap &&
		float64(len(c.distinct)) <= lowCardRatio*float64(c.values)
	if c.nulls > 0 {
		t = "Nullable(" + t + ")"
	}
	if lowCard {
		t = "LowCardinality(" + t + ")"
	}
	return t
}

// InferColumns infers top-level column types from up to InferSampleLimit
// records, in name order:
//
//   - integers that fit Int64 are Int64, any float makes Float64, bools Bool
//   - ISO 8601 / RFC 3339 strings are DateTime64(3)
//   - arrays of one scalar type are Array(T), other arrays String
//   - objects are JSON (Map(String, String) with only string values, or
//     String, before ClickHouse 25.3)
//   - mixed types are String
//
// Nullable is added only when nulls appear next to values (a column with
// only nulls is Nullable(String)), and string columns with few distinct
// values are LowCardinality.
func InferColumns(records []map[string]interface{}, jsonType bool) []SchemaColumn {
	if len(records) > InferSampleLimit {
		records = records[:InferSampleLimit]
	}
	stats := map[string]*columnStats{}
	for _, rec := range records {
		for k, v := range rec {
			if k == "" {
				continue
			}
			s := stats[k]
			if s == nil {
				s = &columnStats{objStrings: true}
				stats[k] = s
			}
			s.add(v)
		}
	}
	names := make([]string, 0, len(stats))
	for k := range stats {
		names = append(names, k)
	}
	sort.Strings(names)
	cols := make([]SchemaColumn, 0, len(names))
	for _, n := range names {
		cols = append(cols, SchemaColumn{Name: n, Type: stats[n].columnType(len(records), jsonType)})
	}
	return cols
}

// InferSchemaFrom is InferColumns plus suggested ORDER BY and PARTITION BY.
func InferSchemaFrom(records []map[string]interface{}, jsonType bool) InferredSchema {
	s := InferredSchema{Columns: InferColumns(records, jsonType), JSONType: jsonType}
	s.OrderBy, s.PartitionBy = suggestKeys(s.Columns)
	return s
}
