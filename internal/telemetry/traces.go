package telemetry

import (
	"fmt"
	"sort"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/telemetry/query"
)

// TracesParams are the common filter inputs for the traces endpoints.
type TracesParams struct {
	From, To string
	Q        string
}

const tracesFacetSampleLimit = 200000

// DurationToNs returns the multiplier that converts the mapping's duration
// unit into nanoseconds.
func (m *TracesMapping) DurationToNs() float64 {
	switch m.DurationUnit {
	case "us":
		return 1e3
	case "ms":
		return 1e6
	case "s":
		return 1e9
	default:
		return 1
	}
}

func fmtScale(f float64) string {
	if f == float64(int64(f)) {
		return fmt.Sprintf("%d", int64(f))
	}
	return fmt.Sprintf("%g", f)
}

// spanBase renders the per-span subquery every traces query builds on:
// ts_ns (start in ns), dur_ns, plus the roles the aggregates need.
func spanBase(src *Source, where string) string {
	m := src.Traces
	parent := "''"
	if m.ParentSpanID != "" {
		parent = Quote(m.ParentSpanID)
	}
	status := "''"
	if m.StatusCode != "" {
		status = Quote(m.StatusCode)
	}
	kind := "''"
	if m.SpanKind != "" {
		kind = Quote(m.SpanKind)
	}
	return fmt.Sprintf(
		"(SELECT %s AS trace_id, %s AS span_name, %s AS service, %s AS parent_span_id, %s AS span_status, %s AS kind, "+
			"toUnixTimestamp64Nano(toDateTime64(%s, 9)) AS ts_ns, toUInt64(%s * %s) AS dur_ns FROM %s.%s WHERE %s)",
		Quote(m.TraceID), Quote(m.SpanName), Quote(m.Service), parent, status, kind,
		Quote(m.Timestamp), Quote(m.Duration), fmtScale(m.DurationToNs()), Quote(src.Database), Quote(src.Table), where)
}

// tracesRangeWhere renders only the time bounds.
func tracesRangeWhere(src *Source, p TracesParams) (string, error) {
	m := src.Traces
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
	if len(conds) == 0 {
		return "1", nil
	}
	return strings.Join(conds, " AND "), nil
}

// TracesWhere builds the span-level WHERE clause: time range plus the
// compiled search.
func TracesWhere(src *Source, columns map[string]string, p TracesParams) (string, error) {
	where, err := tracesRangeWhere(src, p)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(p.Q) != "" {
		w, err := query.Compile(p.Q, src.Traces.QuerySchema(columns))
		if err != nil {
			return "", err
		}
		if w != "1" {
			if where == "1" {
				where = "(" + w + ")"
			} else {
				where += " AND (" + w + ")"
			}
		}
	}
	return where, nil
}

// TraceSearchSQL builds /traces/search: one row per trace. When a search
// is given, a trace qualifies if any span in range matches, and the
// aggregates then cover every span of that trace in range.
func TraceSearchSQL(src *Source, columns map[string]string, p TracesParams, limit int, cursor *Cursor) (string, error) {
	rangeWhere, err := tracesRangeWhere(src, p)
	if err != nil {
		return "", err
	}
	where, err := TracesWhere(src, columns, p)
	if err != nil {
		return "", err
	}
	m := src.Traces
	outerWhere := rangeWhere
	if where != rangeWhere {
		outerWhere = fmt.Sprintf("%s AND %s IN (SELECT %s FROM %s.%s WHERE %s)",
			rangeWhere, Quote(m.TraceID), Quote(m.TraceID), Quote(src.Database), Quote(src.Table), where)
	}
	having := ""
	if cursor != nil {
		having = fmt.Sprintf(" HAVING (start_ns, trace_id) < (%s, %s)", cursor.TimestampNs, Literal(cursor.TraceID))
	}
	return fmt.Sprintf(
		"SELECT trace_id, min(ts_ns) AS start_ns, max(ts_ns + dur_ns) AS end_ns, "+
			"formatDateTime(fromUnixTimestamp64Nano(min(ts_ns)), '%%Y-%%m-%%dT%%H:%%i:%%S.%%fZ', 'UTC') AS start, "+
			"if(countIf(parent_span_id = '') > 0, argMinIf(span_name, ts_ns, parent_span_id = ''), argMin(span_name, ts_ns)) AS root_span, "+
			"if(countIf(parent_span_id = '') > 0, argMinIf(service, ts_ns, parent_span_id = ''), argMin(service, ts_ns)) AS root_service, "+
			"count() AS span_count, countIf(span_status = 'Error') AS error_count, groupUniqArray(service) AS services, "+
			"if(countIf(span_status = 'Error') > 0, 'Error', if(countIf(span_status = 'Ok') > 0, 'Ok', 'Unset')) AS status "+
			"FROM %s GROUP BY trace_id%s ORDER BY start_ns DESC, trace_id DESC LIMIT %d %s",
		spanBase(src, outerWhere), having, limit, QuerySettings), nil
}

// TraceHistogramSQL builds /traces/histogram over root spans.
func TraceHistogramSQL(src *Source, columns map[string]string, p TracesParams, bucketSeconds int) (string, error) {
	where, err := TracesWhere(src, columns, p)
	if err != nil {
		return "", err
	}
	m := src.Traces
	rootCond := "1"
	if m.ParentSpanID != "" {
		rootCond = fmt.Sprintf("%s = ''", Quote(m.ParentSpanID))
	}
	return fmt.Sprintf(
		"SELECT formatDateTime(toStartOfInterval(fromUnixTimestamp64Nano(ts_ns), INTERVAL %d second, 'UTC'), '%%Y-%%m-%%dT%%H:%%i:%%SZ', 'UTC') AS t, "+
			"count() AS c, countIf(span_status = 'Error') AS errors, quantile(0.5)(dur_ns / 1e6) AS p50, quantile(0.95)(dur_ns / 1e6) AS p95 "+
			"FROM %s GROUP BY t ORDER BY t %s",
		bucketSeconds, spanBase(src, where+" AND "+rootCond), QuerySettings), nil
}

// TraceFacetsSQL holds the facet queries for the traces explorer.
type TraceFacetsSQL struct {
	Services, SpanNames, Status, Kinds string
}

// BuildTraceFacetsSQL builds the four facet queries over a LIMITed sample.
func BuildTraceFacetsSQL(src *Source, columns map[string]string, p TracesParams) (TraceFacetsSQL, error) {
	where, err := TracesWhere(src, columns, p)
	if err != nil {
		return TraceFacetsSQL{}, err
	}
	m := src.Traces
	facet := func(col string) string {
		if col == "" {
			return ""
		}
		return fmt.Sprintf("SELECT %s AS value, count() AS c FROM (SELECT %s FROM %s.%s WHERE %s LIMIT %d) GROUP BY value ORDER BY c DESC LIMIT 20 %s",
			Quote(col), Quote(col), Quote(src.Database), Quote(src.Table), where, tracesFacetSampleLimit, QuerySettings)
	}
	return TraceFacetsSQL{
		Services:  facet(m.Service),
		SpanNames: facet(m.SpanName),
		Status:    facet(m.StatusCode),
		Kinds:     facet(m.SpanKind),
	}, nil
}

// TraceSpansSQL builds the query that returns every span of one trace.
func TraceSpansSQL(src *Source, traceID string) string {
	m := src.Traces
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
	arr := func(prefix, sub, alias string) string {
		if prefix == "" {
			return "[] AS " + alias
		}
		return Quote(prefix+"."+sub) + " AS " + alias
	}
	parts := []string{
		col(m.SpanID, "span_id"),
		col(m.ParentSpanID, "parent_span_id"),
		col(m.SpanName, "name"),
		col(m.SpanKind, "kind"),
		col(m.Service, "service"),
		fmt.Sprintf("formatDateTime(%s, '%%Y-%%m-%%dT%%H:%%i:%%S.%%fZ', 'UTC') AS start", Quote(m.Timestamp)),
		fmt.Sprintf("toString(toUnixTimestamp64Nano(toDateTime64(%s, 9))) AS start_ns", Quote(m.Timestamp)),
		fmt.Sprintf("toFloat64(%s) * %s / 1e6 AS duration_ms", Quote(m.Duration), fmtScale(m.DurationToNs())),
		col(m.StatusCode, "status"),
		col(m.StatusMessage, "status_message"),
		mapCol(m.SpanAttributes, "attributes"),
		mapCol(m.ResourceAttributes, "resource"),
		arr(m.Events, "Name", "ev_name"),
		arr(m.Events, "Attributes", "ev_attrs"),
		arr(m.Links, "TraceId", "lk_trace"),
		arr(m.Links, "SpanId", "lk_span"),
		arr(m.Links, "Attributes", "lk_attrs"),
	}
	if m.Events == "" {
		parts = append(parts, "[] AS ev_time")
	} else {
		parts = append(parts, fmt.Sprintf("arrayMap(x -> formatDateTime(x, '%%Y-%%m-%%dT%%H:%%i:%%S.%%fZ', 'UTC'), %s) AS ev_time", Quote(m.Events+".Timestamp")))
	}
	return fmt.Sprintf("SELECT %s FROM %s.%s WHERE %s = %s ORDER BY %s ASC LIMIT 5000 %s",
		strings.Join(parts, ", "), Quote(src.Database), Quote(src.Table), Quote(m.TraceID), Literal(traceID), Quote(m.Timestamp), QuerySettings)
}

// SpanEvent is one span event in the contract shape.
type SpanEvent struct {
	Time       string            `json:"time"`
	Name       string            `json:"name"`
	Attributes map[string]string `json:"attributes"`
}

// SpanLink is one span link in the contract shape.
type SpanLink struct {
	TraceID    string            `json:"trace_id"`
	SpanID     string            `json:"span_id"`
	Attributes map[string]string `json:"attributes"`
}

// Span is one span in the contract shape.
type Span struct {
	SpanID        string            `json:"span_id"`
	ParentSpanID  string            `json:"parent_span_id"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	Service       string            `json:"service"`
	Start         string            `json:"start"`
	StartNs       string            `json:"start_ns"`
	DurationMs    float64           `json:"duration_ms"`
	Status        string            `json:"status"`
	StatusMessage string            `json:"status_message"`
	Attributes    map[string]string `json:"attributes"`
	Resource      map[string]string `json:"resource"`
	Events        []SpanEvent       `json:"events"`
	Links         []SpanLink        `json:"links"`
	Depth         int               `json:"depth"`
	Order         int               `json:"order"`

	startNs int64
}

// ZipEvents pairs the parallel Nested arrays into events.
func ZipEvents(times, names []string, attrs []map[string]string) []SpanEvent {
	n := len(names)
	out := make([]SpanEvent, 0, n)
	for i := 0; i < n; i++ {
		e := SpanEvent{Name: names[i], Attributes: map[string]string{}}
		if i < len(times) {
			e.Time = times[i]
		}
		if i < len(attrs) && attrs[i] != nil {
			e.Attributes = attrs[i]
		}
		out = append(out, e)
	}
	return out
}

// ZipLinks pairs the parallel Nested arrays into links.
func ZipLinks(traceIDs, spanIDs []string, attrs []map[string]string) []SpanLink {
	n := len(traceIDs)
	out := make([]SpanLink, 0, n)
	for i := 0; i < n; i++ {
		l := SpanLink{TraceID: traceIDs[i], Attributes: map[string]string{}}
		if i < len(spanIDs) {
			l.SpanID = spanIDs[i]
		}
		if i < len(attrs) && attrs[i] != nil {
			l.Attributes = attrs[i]
		}
		out = append(out, l)
	}
	return out
}

// BuildSpanTree orders spans as a pre-order walk of the parent/child
// tree, filling Depth and Order. Children are sorted by start time.
// Spans whose parent is missing become roots after the real roots.
func BuildSpanTree(spans []Span) []Span {
	if len(spans) == 0 {
		return spans
	}
	byID := make(map[string]int, len(spans))
	for i := range spans {
		spans[i].startNs = parseInt64(spans[i].StartNs)
		if spans[i].SpanID != "" {
			byID[spans[i].SpanID] = i
		}
	}
	children := map[string][]int{}
	var roots, orphans []int
	for i, s := range spans {
		switch {
		case s.ParentSpanID == "":
			roots = append(roots, i)
		default:
			if _, ok := byID[s.ParentSpanID]; ok {
				children[s.ParentSpanID] = append(children[s.ParentSpanID], i)
			} else {
				orphans = append(orphans, i)
			}
		}
	}
	byStart := func(idx []int) {
		sort.SliceStable(idx, func(a, b int) bool { return spans[idx[a]].startNs < spans[idx[b]].startNs })
	}
	byStart(roots)
	byStart(orphans)
	for _, c := range children {
		byStart(c)
	}
	out := make([]Span, 0, len(spans))
	visited := make(map[int]bool, len(spans))
	var walk func(i, depth int)
	walk = func(i, depth int) {
		if visited[i] {
			return
		}
		visited[i] = true
		s := spans[i]
		s.Depth = depth
		s.Order = len(out)
		out = append(out, s)
		for _, c := range children[s.SpanID] {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	for _, o := range orphans {
		walk(o, 0)
	}
	// Cycles or duplicates: append anything unvisited so nothing is lost.
	for i := range spans {
		if !visited[i] {
			walk(i, 0)
		}
	}
	return out
}

func parseInt64(s string) int64 {
	var n int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0
		}
		n = n*10 + int64(r-'0')
	}
	return n
}
