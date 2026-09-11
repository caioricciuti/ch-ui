package telemetry

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/telemetry/query"
)

// QuerySettings is appended to every telemetry query.
const QuerySettings = "SETTINGS max_execution_time = 25, max_result_rows = 100000, result_overflow_mode = 'break'"

// Literal escapes a value for a single-quoted ClickHouse literal.
func Literal(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\x00", "")
	return "'" + s + "'"
}

// TimeBound renders an RFC3339 bound against a DateTime/DateTime64 column.
func TimeBound(col, op, value string) (string, error) {
	if _, err := time.Parse(time.RFC3339Nano, value); err != nil {
		return "", fmt.Errorf("invalid timestamp %q: expected RFC3339", value)
	}
	return fmt.Sprintf("%s %s parseDateTime64BestEffort(%s)", Quote(col), op, Literal(value)), nil
}

// LogsParams are the common filter inputs for the logs endpoints.
type LogsParams struct {
	From, To string
	Q        string
	Severity []string
	Services []string
}

// Cursor is the keyset position for /logs/search paging.
type Cursor struct {
	TimestampNs string
	TraceID     string
	SpanID      string
}

// EncodeCursor serializes a cursor as an opaque token. Each part is
// base64 on its own so ids containing '|' survive.
func EncodeCursor(c Cursor) string {
	enc := base64.RawURLEncoding.EncodeToString
	return enc([]byte(c.TimestampNs)) + "." + enc([]byte(c.TraceID)) + "." + enc([]byte(c.SpanID))
}

// DecodeCursor parses a token from EncodeCursor.
func DecodeCursor(s string) (Cursor, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	dec := make([]string, 3)
	for i, p := range parts {
		raw, err := base64.RawURLEncoding.DecodeString(p)
		if err != nil {
			return Cursor{}, fmt.Errorf("invalid cursor")
		}
		dec[i] = string(raw)
	}
	if dec[0] == "" || len(dec[0]) > 20 {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	for _, r := range dec[0] {
		if r < '0' || r > '9' {
			return Cursor{}, fmt.Errorf("invalid cursor")
		}
	}
	return Cursor{TimestampNs: dec[0], TraceID: dec[1], SpanID: dec[2]}, nil
}

// inList renders a quoted IN list.
func inList(values []string) string {
	q := make([]string, len(values))
	for i, v := range values {
		q[i] = Literal(v)
	}
	return strings.Join(q, ", ")
}

// LogsWhere builds the WHERE clause (without the keyword) shared by the
// logs endpoints: time range, search, severity and service filters.
func LogsWhere(src *Source, columns map[string]string, p LogsParams) (string, error) {
	m := src.Logs
	var conds []string
	if p.From != "" {
		c, err := TimeBound(m.Timestamp, ">=", p.From)
		if err != nil {
			return "", err
		}
		conds = append(conds, c)
	}
	if p.To != "" {
		c, err := TimeBound(m.Timestamp, "<=", p.To)
		if err != nil {
			return "", err
		}
		conds = append(conds, c)
	}
	if strings.TrimSpace(p.Q) != "" {
		w, err := query.Compile(p.Q, m.QuerySchema(columns))
		if err != nil {
			return "", err
		}
		if w != "1" {
			conds = append(conds, "("+w+")")
		}
	}
	if len(p.Severity) > 0 {
		conds = append(conds, fmt.Sprintf("%s IN (%s)", Quote(m.Severity), inList(p.Severity)))
	}
	if len(p.Services) > 0 {
		conds = append(conds, fmt.Sprintf("%s IN (%s)", Quote(m.Service), inList(p.Services)))
	}
	if len(conds) == 0 {
		return "1", nil
	}
	return strings.Join(conds, " AND "), nil
}

// logRowSelect renders the SELECT list for a log row in the contract shape.
func logRowSelect(m *LogsMapping) string {
	col := func(role, alias string) string {
		if role == "" {
			return "'' AS " + alias
		}
		return Quote(role) + " AS " + alias
	}
	mapCol := func(role, alias string) string {
		if role == "" {
			return "map() AS " + alias
		}
		return Quote(role) + " AS " + alias
	}
	parts := []string{
		fmt.Sprintf("formatDateTime(%s, '%%Y-%%m-%%dT%%H:%%i:%%S.%%fZ', 'UTC') AS timestamp", Quote(m.Timestamp)),
		fmt.Sprintf("toString(toUnixTimestamp64Nano(toDateTime64(%s, 9))) AS timestamp_ns", Quote(m.Timestamp)),
		col(m.Severity, "severity"),
		"toUInt8(" + orZero(m.SeverityNumber) + ") AS severity_number",
		col(m.Service, "service"),
		col(m.Body, "body"),
		col(m.TraceID, "trace_id"),
		col(m.SpanID, "span_id"),
		mapCol(m.ResourceAttributes, "resource"),
		mapCol(m.ScopeAttributes, "scope"),
		mapCol(m.LogAttributes, "attributes"),
		col(m.ScopeName, "scope_name"),
		col(m.EventName, "event_name"),
	}
	return strings.Join(parts, ", ")
}

func orZero(role string) string {
	if role == "" {
		return "0"
	}
	return Quote(role)
}

// SearchSQL builds /logs/search. order is "asc" or "desc".
func SearchSQL(src *Source, columns map[string]string, p LogsParams, order string, limit int, cursor *Cursor) (string, error) {
	where, err := LogsWhere(src, columns, p)
	if err != nil {
		return "", err
	}
	m := src.Logs
	if order != "asc" {
		order = "desc"
	}
	if cursor != nil {
		op := "<"
		if order == "asc" {
			op = ">"
		}
		ts := Quote(m.Timestamp)
		tid, sid := "''", "''"
		if m.TraceID != "" {
			tid = Quote(m.TraceID)
		}
		if m.SpanID != "" {
			sid = Quote(m.SpanID)
		}
		where += fmt.Sprintf(" AND (toUnixTimestamp64Nano(toDateTime64(%s, 9)), %s, %s) %s (%s, %s, %s)",
			ts, tid, sid, op, cursor.TimestampNs, Literal(cursor.TraceID), Literal(cursor.SpanID))
	}
	orderBy := fmt.Sprintf("%s %s", Quote(m.Timestamp), strings.ToUpper(order))
	if m.TraceID != "" {
		orderBy += fmt.Sprintf(", %s %s", Quote(m.TraceID), strings.ToUpper(order))
	}
	if m.SpanID != "" {
		orderBy += fmt.Sprintf(", %s %s", Quote(m.SpanID), strings.ToUpper(order))
	}
	return fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s ORDER BY %s LIMIT %d %s",
		logRowSelect(m), Quote(src.Database), Quote(src.Table), where, orderBy, limit, QuerySettings), nil
}

// BucketSeconds picks the histogram bucket for a range.
func BucketSeconds(from, to string) int {
	f, err1 := time.Parse(time.RFC3339Nano, from)
	t, err2 := time.Parse(time.RFC3339Nano, to)
	if err1 != nil || err2 != nil || !t.After(f) {
		return 60
	}
	secs := int(t.Sub(f).Seconds())
	switch {
	case secs <= 3600:
		return 10
	case secs <= 6*3600:
		return 60
	case secs <= 86400:
		return 300
	case secs <= 7*86400:
		return 3600
	default:
		return 86400
	}
}

// HistogramSQL builds /logs/histogram: one row per (bucket, severity).
func HistogramSQL(src *Source, columns map[string]string, p LogsParams, bucketSeconds int) (string, error) {
	where, err := LogsWhere(src, columns, p)
	if err != nil {
		return "", err
	}
	m := src.Logs
	return fmt.Sprintf(
		"SELECT formatDateTime(toStartOfInterval(%s, INTERVAL %d second), '%%Y-%%m-%%dT%%H:%%i:%%SZ', 'UTC') AS t, %s AS severity, count() AS c FROM %s.%s WHERE %s GROUP BY t, severity ORDER BY t %s",
		Quote(m.Timestamp), bucketSeconds, Quote(m.Severity), Quote(src.Database), Quote(src.Table), where, QuerySettings), nil
}

// FacetsSQL builds the four facet queries: severity, services, attribute
// keys, and top values per requested key.
type FacetsSQL struct {
	Severity string
	Services string
	Keys     string
	Values   map[string]string
}

const facetSampleLimit = 200000

// BuildFacetsSQL builds the facet queries over a LIMITed subquery.
func BuildFacetsSQL(src *Source, columns map[string]string, p LogsParams, keys []string) (FacetsSQL, error) {
	where, err := LogsWhere(src, columns, p)
	if err != nil {
		return FacetsSQL{}, err
	}
	m := src.Logs
	table := fmt.Sprintf("%s.%s", Quote(src.Database), Quote(src.Table))
	sub := func(cols string) string {
		return fmt.Sprintf("(SELECT %s FROM %s WHERE %s LIMIT %d)", cols, table, where, facetSampleLimit)
	}
	out := FacetsSQL{Values: map[string]string{}}
	out.Severity = fmt.Sprintf("SELECT %s AS value, count() AS c FROM %s GROUP BY value ORDER BY c DESC LIMIT 20 %s",
		Quote(m.Severity), sub(Quote(m.Severity)), QuerySettings)
	out.Services = fmt.Sprintf("SELECT %s AS value, count() AS c FROM %s GROUP BY value ORDER BY c DESC LIMIT 50 %s",
		Quote(m.Service), sub(Quote(m.Service)), QuerySettings)

	var keyParts []string
	var mapCols []string
	var mapJSON []bool
	if m.LogAttributes != "" {
		j := IsJSONColumn(columns[m.LogAttributes])
		mapCols = append(mapCols, Quote(m.LogAttributes))
		mapJSON = append(mapJSON, j)
		keyParts = append(keyParts, fmt.Sprintf("SELECT arrayJoin(%s) AS k, 'attributes' AS src FROM %s",
			AttrKeys(Quote(m.LogAttributes), j), sub(Quote(m.LogAttributes))))
	}
	if m.ResourceAttributes != "" {
		j := IsJSONColumn(columns[m.ResourceAttributes])
		mapCols = append(mapCols, Quote(m.ResourceAttributes))
		mapJSON = append(mapJSON, j)
		keyParts = append(keyParts, fmt.Sprintf("SELECT arrayJoin(%s) AS k, 'resource' AS src FROM %s",
			AttrKeys(Quote(m.ResourceAttributes), j), sub(Quote(m.ResourceAttributes))))
	}
	if len(keyParts) > 0 {
		out.Keys = fmt.Sprintf("SELECT k, src, count() AS c FROM (%s) GROUP BY k, src ORDER BY c DESC LIMIT 50 %s",
			strings.Join(keyParts, " UNION ALL "), QuerySettings)
	}
	if len(keys) > 10 {
		keys = keys[:10]
	}
	for _, k := range keys {
		if k == "" || len(k) > 200 {
			continue
		}
		var valueExprs []string
		for i, c := range mapCols {
			valueExprs = append(valueExprs, AttrAccess(c, k, mapJSON[i]))
		}
		if len(valueExprs) == 0 {
			continue
		}
		expr := valueExprs[0]
		if len(valueExprs) > 1 {
			expr = "coalesce(" + strings.Join(func() []string {
				o := make([]string, len(valueExprs))
				for i, v := range valueExprs {
					o[i] = "nullIf(" + v + ", '')"
				}
				return o
			}(), ", ") + ", '')"
		}
		out.Values[k] = fmt.Sprintf("SELECT %s AS value, count() AS c FROM %s WHERE value != '' GROUP BY value ORDER BY c DESC LIMIT 10 %s",
			expr, sub(strings.Join(mapCols, ", ")), QuerySettings)
	}
	return out, nil
}

// ContextSQL builds the two halves of /logs/context around an anchor.
func ContextSQL(src *Source, timestampNs, service string, before, after int) (beforeSQL, afterSQL string) {
	m := src.Logs
	table := fmt.Sprintf("%s.%s", Quote(src.Database), Quote(src.Table))
	ts := fmt.Sprintf("toUnixTimestamp64Nano(toDateTime64(%s, 9))", Quote(m.Timestamp))
	svc := ""
	if service != "" {
		svc = fmt.Sprintf(" AND %s = %s", Quote(m.Service), Literal(service))
	}
	beforeSQL = fmt.Sprintf("SELECT %s FROM %s WHERE %s < %s%s ORDER BY %s DESC LIMIT %d %s",
		logRowSelect(m), table, ts, timestampNs, svc, Quote(m.Timestamp), before, QuerySettings)
	afterSQL = fmt.Sprintf("SELECT %s FROM %s WHERE %s >= %s%s ORDER BY %s ASC LIMIT %d %s",
		logRowSelect(m), table, ts, timestampNs, svc, Quote(m.Timestamp), after+1, QuerySettings)
	return
}

// ByTraceSQL builds /logs/by-trace.
func ByTraceSQL(src *Source, traceID string) (string, error) {
	m := src.Logs
	if m.TraceID == "" {
		return "", fmt.Errorf("this logs source has no trace_id column")
	}
	return fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s = %s ORDER BY %s ASC LIMIT 1000 %s",
		logRowSelect(m), Quote(src.Database), Quote(src.Table), Quote(m.TraceID), Literal(traceID), Quote(m.Timestamp), QuerySettings), nil
}
