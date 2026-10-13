package telemetry

import (
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/telemetry/query"
)

// MaxResultRows is the row cap every telemetry query runs under. With
// result_overflow_mode = 'break' ClickHouse stops at the cap and returns
// what it has without an error, so callers that aggregate rows in Go must
// compare the row count with ResultCapped to know the answer is partial.
const MaxResultRows = 100000

// QuerySettings is appended to every telemetry query.
const QuerySettings = "SETTINGS max_execution_time = 25, max_result_rows = 100000, result_overflow_mode = 'break'"

// ResultCapped reports whether a result of n rows may have been cut by
// max_result_rows. ClickHouse stops at a block boundary, so a cut result
// has at least MaxResultRows rows.
func ResultCapped(n int) bool { return n >= MaxResultRows }

// TimeWindow bounds a lookup to [FromNs, ToNs] in unix nanoseconds.
type TimeWindow struct {
	FromNs, ToNs int64
}

// cond renders the window against a timestamp column.
func (w TimeWindow) cond(col string) string {
	return fmt.Sprintf("%s >= fromUnixTimestamp64Nano(toInt64(%d)) AND %s <= fromUnixTimestamp64Nano(toInt64(%d))",
		Quote(col), w.FromNs, Quote(col), w.ToNs)
}

// Around returns the window [t-before, t+after].
func Around(t time.Time, before, after time.Duration) *TimeWindow {
	return &TimeWindow{FromNs: t.Add(-before).UnixNano(), ToNs: t.Add(after).UnixNano()}
}

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
	// ExcludeField drops that field's top-level terms from Q (facets).
	ExcludeField string
}

// Cursor is the keyset position for /logs/search paging. RowHash is the
// row's content hash (row_id): timestamp, trace id and span id alone are
// not unique (untraced logs, second-precision timestamps), and a cursor
// that is not unique skips the rows tied with the last row of a page.
type Cursor struct {
	TimestampNs string
	TraceID     string
	SpanID      string
	RowHash     string
}

// EncodeCursor serializes a cursor as an opaque token. Each part is
// base64 on its own so ids containing '|' survive.
func EncodeCursor(c Cursor) string {
	enc := base64.RawURLEncoding.EncodeToString
	out := enc([]byte(c.TimestampNs)) + "." + enc([]byte(c.TraceID)) + "." + enc([]byte(c.SpanID))
	if c.RowHash != "" {
		out += "." + enc([]byte(c.RowHash))
	}
	return out
}

func allDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// DecodeCursor parses a token from EncodeCursor. A three-part token from
// before the row hash existed still decodes, without a hash.
func DecodeCursor(s string) (Cursor, error) {
	parts := strings.Split(s, ".")
	if len(parts) != 3 && len(parts) != 4 {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	dec := make([]string, 4)
	for i, p := range parts {
		raw, err := base64.RawURLEncoding.DecodeString(p)
		if err != nil {
			return Cursor{}, fmt.Errorf("invalid cursor")
		}
		dec[i] = string(raw)
	}
	if dec[0] == "" || len(dec[0]) > 20 || !allDigits(dec[0]) {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	if len(dec[3]) > 20 || !allDigits(dec[3]) || (len(parts) == 4 && dec[3] == "") {
		return Cursor{}, fmt.Errorf("invalid cursor")
	}
	return Cursor{TimestampNs: dec[0], TraceID: dec[1], SpanID: dec[2], RowHash: dec[3]}, nil
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
		c, err := TimeBound(m.Timestamp, "<", p.To)
		if err != nil {
			return "", err
		}
		conds = append(conds, c)
	}
	if strings.TrimSpace(p.Q) != "" {
		w, err := query.CompileExcluding(p.Q, m.QuerySchema(columns), p.ExcludeField)
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
		rowHashExpr(m) + " AS row_hash",
		"toString(row_hash) AS row_id",
	}
	return strings.Join(parts, ", ")
}

// rowHashExpr hashes the content of a log row: with the timestamp it is
// the row's identity for paging, context anchors and links. Maps and JSON
// columns go through toString because cityHash64 refuses JSON.
func rowHashExpr(m *LogsMapping) string {
	var parts []string
	for _, c := range []string{m.Body, m.Service, m.Severity, m.TraceID, m.SpanID, m.LogAttributes, m.ResourceAttributes, m.ScopeAttributes, m.ScopeName, m.EventName} {
		if c != "" {
			parts = append(parts, "toString("+Quote(c)+")")
		}
	}
	return "cityHash64(" + strings.Join(parts, ", ") + ")"
}

// logColumns lists the mapped columns once each, quoted, for subqueries
// that feed logRowSelect.
func logColumns(m *LogsMapping) string {
	seen := map[string]bool{}
	var out []string
	for _, c := range []string{m.Timestamp, m.Severity, m.SeverityNumber, m.Service, m.Body, m.TraceID, m.SpanID,
		m.ResourceAttributes, m.ScopeAttributes, m.LogAttributes, m.ScopeName, m.EventName} {
		if c != "" && !seen[c] {
			seen[c] = true
			out = append(out, Quote(c))
		}
	}
	return strings.Join(out, ", ")
}

func tsNsExpr(col string) string {
	return fmt.Sprintf("toUnixTimestamp64Nano(toDateTime64(%s, 9))", Quote(col))
}

func orZero(role string) string {
	if role == "" {
		return "0"
	}
	return Quote(role)
}

// SearchSQL builds /logs/search. order is "asc" or "desc".
//
// The keyset is (timestamp, trace id, span id, row hash). Hashing every
// row in range just to sort would read Body and every attribute map for
// the whole range, so the hash is only computed where it is cheap:
//
//   - the main part sorts by (timestamp, trace id, span id) and takes the
//     page LIMIT ... WITH TIES, so a tie group at the page edge comes back
//     whole; the outer query hashes only those rows and cuts the page;
//   - on the next page, a second part fetches the rest of the tie group
//     the cursor sits in (same timestamp, smaller key), a tiny set.
func SearchSQL(src *Source, columns map[string]string, p LogsParams, order string, limit int, cursor *Cursor) (string, error) {
	where, err := LogsWhere(src, columns, p)
	if err != nil {
		return "", err
	}
	m := src.Logs
	dir, op := "DESC", "<"
	if order == "asc" {
		dir, op = "ASC", ">"
	}
	tid, sid := "''", "''"
	if m.TraceID != "" {
		tid = Quote(m.TraceID)
	}
	if m.SpanID != "" {
		sid = Quote(m.SpanID)
	}
	key := fmt.Sprintf("%s %s", Quote(m.Timestamp), dir)
	if m.TraceID != "" {
		key += fmt.Sprintf(", %s %s", tid, dir)
	}
	if m.SpanID != "" {
		key += fmt.Sprintf(", %s %s", sid, dir)
	}
	table := fmt.Sprintf("%s.%s", Quote(src.Database), Quote(src.Table))
	cols := logColumns(m)
	ts := tsNsExpr(m.Timestamp)

	mainWhere := where
	var parts []string
	if cursor != nil {
		mainWhere += fmt.Sprintf(" AND %s %s %s", ts, op, cursor.TimestampNs)
		tie := fmt.Sprintf("(%s, %s) %s (%s, %s)", tid, sid, op, Literal(cursor.TraceID), Literal(cursor.SpanID))
		if cursor.RowHash != "" {
			tie = fmt.Sprintf("(%s, %s, %s) %s (%s, %s, %s)", tid, sid, rowHashExpr(m), op,
				Literal(cursor.TraceID), Literal(cursor.SpanID), cursor.RowHash)
		}
		parts = append(parts, fmt.Sprintf("(SELECT %s FROM %s WHERE %s AND %s = %s AND %s)",
			cols, table, where, ts, cursor.TimestampNs, tie))
	}
	parts = append(parts, fmt.Sprintf("(SELECT %s FROM %s WHERE %s ORDER BY %s LIMIT %d WITH TIES)",
		cols, table, mainWhere, key, limit))
	return fmt.Sprintf("SELECT %s FROM (%s) ORDER BY %s, row_hash %s LIMIT %d %s",
		logRowSelect(m), strings.Join(parts, " UNION ALL "), key, dir, limit, QuerySettings), nil
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
		"SELECT formatDateTime(toStartOfInterval(%s, INTERVAL %d second, 'UTC'), '%%Y-%%m-%%dT%%H:%%i:%%SZ', 'UTC') AS t, %s AS severity, count() AS c FROM %s.%s WHERE %s GROUP BY t, severity ORDER BY t %s",
		Quote(m.Timestamp), bucketSeconds, Quote(m.Severity), Quote(src.Database), Quote(src.Table), where, QuerySettings), nil
}

// FacetsSQL holds the facet queries: severity, services, attribute keys,
// and top values per requested key.
//
// Every query aggregates a LIMITed sample of the matching rows, so counts
// are exact only below facetSampleLimit rows. Each value query also
// returns `total`, the number of sampled rows it aggregated: total above
// facetSampleLimit means the sample was cut and the counts are approximate.
//
// Each facet drops its own filter and keeps the others (Kibana style): the
// severity counts ignore the selected severities, the values of one
// attribute ignore that attribute's top-level terms in the query. Without
// that, picking a value hides every other value of the same facet.
type FacetsSQL struct {
	Severity string
	Services string
	Keys     string
	Values   map[string]string
}

const facetSampleLimit = 200000

// FacetSampled reports whether a facet total means the sample was cut.
func FacetSampled(total int64) bool { return total > facetSampleLimit }

// FacetRowsScanned caps a facet total at the sample size.
func FacetRowsScanned(total int64) int64 {
	if total > facetSampleLimit {
		return facetSampleLimit
	}
	return total
}

// facetValueSQL counts expr over a sample of where, keeping the sample size
// as total. filterEmpty drops empty values after the total is taken.
func facetValueSQL(table, cols, where, expr string, top int, filterEmpty bool) string {
	sub := fmt.Sprintf("(SELECT %s FROM %s WHERE %s LIMIT %d)", cols, table, where, facetSampleLimit+1)
	having := ""
	if filterEmpty {
		having = " WHERE value != ''"
	}
	return fmt.Sprintf("SELECT value, c, total FROM (SELECT %s AS value, count() AS c, sum(count()) OVER () AS total FROM %s GROUP BY value)%s ORDER BY c DESC LIMIT %d %s",
		expr, sub, having, top, QuerySettings)
}

// BuildFacetsSQL builds the facet queries over LIMITed subqueries.
func BuildFacetsSQL(src *Source, columns map[string]string, p LogsParams, keys []string) (FacetsSQL, error) {
	where, err := LogsWhere(src, columns, p)
	if err != nil {
		return FacetsSQL{}, err
	}
	noSeverity := p
	noSeverity.Severity = nil
	sevWhere, err := LogsWhere(src, columns, noSeverity)
	if err != nil {
		return FacetsSQL{}, err
	}
	noServices := p
	noServices.Services = nil
	svcWhere, err := LogsWhere(src, columns, noServices)
	if err != nil {
		return FacetsSQL{}, err
	}
	m := src.Logs
	table := fmt.Sprintf("%s.%s", Quote(src.Database), Quote(src.Table))
	sub := func(cols string) string {
		return fmt.Sprintf("(SELECT %s FROM %s WHERE %s LIMIT %d)", cols, table, where, facetSampleLimit+1)
	}
	out := FacetsSQL{Values: map[string]string{}}
	out.Severity = facetValueSQL(table, Quote(m.Severity), sevWhere, Quote(m.Severity), 20, false)
	out.Services = facetValueSQL(table, Quote(m.Service), svcWhere, Quote(m.Service), 50, false)

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
			o := make([]string, len(valueExprs))
			for i, v := range valueExprs {
				o[i] = "nullIf(" + v + ", '')"
			}
			expr = "coalesce(" + strings.Join(o, ", ") + ", '')"
		}
		own := p
		own.ExcludeField = k
		keyWhere, err := LogsWhere(src, columns, own)
		if err != nil {
			return FacetsSQL{}, err
		}
		out.Values[k] = facetValueSQL(table, strings.Join(mapCols, ", "), keyWhere, expr, 10, true)
	}
	return out, nil
}

// ContextWindow bounds the context lookup on each side of the anchor.
const ContextWindow = time.Hour

// ContextSQL builds the two halves of /logs/context around an anchor
// (unix ns). Both halves are bounded to ContextWindow so a sparse service
// does not scan the whole table, and order by the row hash after the
// timestamp so rows tied with the anchor come back in a stable order the
// handler can find the anchor in.
func ContextSQL(src *Source, anchorNs int64, service string, before, after int) (beforeSQL, afterSQL string) {
	m := src.Logs
	table := fmt.Sprintf("%s.%s", Quote(src.Database), Quote(src.Table))
	ts := tsNsExpr(m.Timestamp)
	svc := ""
	if service != "" {
		svc = fmt.Sprintf(" AND %s = %s", Quote(m.Service), Literal(service))
	}
	w := int64(ContextWindow)
	beforeWin := TimeWindow{FromNs: anchorNs - w, ToNs: anchorNs}
	afterWin := TimeWindow{FromNs: anchorNs, ToNs: anchorNs + w}
	beforeSQL = fmt.Sprintf("SELECT %s FROM %s WHERE %s AND %s < %d%s ORDER BY %s DESC, row_hash DESC LIMIT %d %s",
		logRowSelect(m), table, beforeWin.cond(m.Timestamp), ts, anchorNs, svc, Quote(m.Timestamp), before, QuerySettings)
	afterSQL = fmt.Sprintf("SELECT %s FROM %s WHERE %s AND %s >= %d%s ORDER BY %s ASC, row_hash ASC LIMIT %d %s",
		logRowSelect(m), table, afterWin.cond(m.Timestamp), ts, anchorNs, svc, Quote(m.Timestamp), after+1, QuerySettings)
	return
}

// ByTraceSQL builds /logs/by-trace. A window (from the trace itself or a
// time hint) keeps the lookup off the rest of the table; nil scans it all.
func ByTraceSQL(src *Source, traceID string, win *TimeWindow) (string, error) {
	m := src.Logs
	if m.TraceID == "" {
		return "", fmt.Errorf("this logs source has no trace_id column")
	}
	bound := ""
	if win != nil {
		bound = " AND " + win.cond(m.Timestamp)
	}
	return fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s = %s%s ORDER BY %s ASC LIMIT 1000 %s",
		logRowSelect(m), Quote(src.Database), Quote(src.Table), Quote(m.TraceID), Literal(traceID), bound, Quote(m.Timestamp), QuerySettings), nil
}
