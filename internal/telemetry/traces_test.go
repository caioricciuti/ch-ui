package telemetry

import (
	"strings"
	"testing"
)

func tracesSrcLocal() *Source {
	m := DefaultTracesMapping()
	return &Source{ID: "t1", Kind: KindTraces, Database: "default", Table: "otel_traces", Traces: &m}
}

func TestTraceSearchSQL(t *testing.T) {
	src := tracesSrcLocal()
	sql, err := TraceSearchSQL(src, nil, TracesParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T01:00:00Z", Q: "service:checkout duration:>200"}, 101, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	for _, want := range []string{
		"GROUP BY trace_id",
		"`Timestamp` < parseDateTime64BestEffort('2026-09-11T01:00:00Z')",
		"ORDER BY start_ns DESC, trace_id DESC LIMIT 101",
		"`TraceId` IN (SELECT `TraceId` FROM `default`.`otel_traces` WHERE",
		"`ServiceName` = 'checkout'",
		"`Duration` > 200000000",
		"toUInt64(`Duration` * 1) AS dur_ns",
		"argMinIf(span_name, ts_ns, parent_span_id = '')",
		"countIf(span_status = 'Error') AS error_count",
		QuerySettings,
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in:\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "HAVING") {
		t.Errorf("no cursor should mean no HAVING")
	}

	// Without a search there is no IN subquery.
	sql, err = TraceSearchSQL(src, nil, TracesParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T01:00:00Z"}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(sql, " IN (SELECT") {
		t.Errorf("range-only search must not add the IN subquery:\n%s", sql)
	}

	// Cursor becomes a HAVING on (start_ns, trace_id).
	sql, err = TraceSearchSQL(src, nil, TracesParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T01:00:00Z"}, 10, &Cursor{TimestampNs: "1757548800000000000", TraceID: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "HAVING (start_ns, trace_id) < (1757548800000000000, 'abc')") {
		t.Errorf("cursor not rendered:\n%s", sql)
	}

	// Bad query -> parser error, not SQL.
	if _, err := TraceSearchSQL(src, nil, TracesParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T01:00:00Z", Q: "(a"}, 10, nil); err == nil {
		t.Errorf("expected a compile error")
	}
	// Bad time -> error.
	if _, err := TraceSearchSQL(src, nil, TracesParams{From: "yesterday", To: "now"}, 10, nil); err == nil {
		t.Errorf("expected a time error")
	}
}

func TestTraceSearchSQLDurationUnit(t *testing.T) {
	src := tracesSrcLocal()
	src.Traces.DurationUnit = "ms"
	sql, err := TraceSearchSQL(src, nil, TracesParams{Q: "duration:>=1.5"}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "`Duration` >= 1.5") {
		t.Errorf("ms unit should not scale the literal:\n%s", sql)
	}
	if !strings.Contains(sql, "toUInt64(`Duration` * 1000000) AS dur_ns") {
		t.Errorf("ms unit must scale to ns for aggregates:\n%s", sql)
	}
}

func TestTraceHistogramSQL(t *testing.T) {
	src := tracesSrcLocal()
	sql, err := TraceHistogramSQL(src, nil, TracesParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T06:00:00Z"}, BucketSeconds("2026-09-11T00:00:00Z", "2026-09-11T06:00:00Z"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"INTERVAL 60 second, 'UTC'", "`ParentSpanId` = ''", "quantile(0.5)(dur_ns / 1e6) AS p50", "quantile(0.95)(dur_ns / 1e6) AS p95", "GROUP BY t ORDER BY t"} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in:\n%s", want, sql)
		}
	}
}

func TestBuildTraceFacetsSQL(t *testing.T) {
	src := tracesSrcLocal()
	f, err := BuildTraceFacetsSQL(src, nil, TracesParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T01:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Services, "`ServiceName` AS value") || !strings.Contains(f.SpanNames, "`SpanName` AS value") ||
		!strings.Contains(f.Status, "`StatusCode` AS value") || !strings.Contains(f.Kinds, "`SpanKind` AS value") {
		t.Errorf("facet columns wrong: %+v", f)
	}
	src.Traces.SpanKind = ""
	f, _ = BuildTraceFacetsSQL(src, nil, TracesParams{})
	if f.Kinds != "" {
		t.Errorf("unmapped kind must produce no query")
	}
}

func TestTraceSpansSQL(t *testing.T) {
	src := tracesSrcLocal()
	sql := TraceSpansSQL(src, "abc'def")
	for _, want := range []string{
		"WHERE `TraceId` = 'abc\\'def'",
		"`Events.Name` AS ev_name",
		"`Events.Attributes` AS ev_attrs",
		"`Links.TraceId` AS lk_trace",
		"arrayMap(x -> formatDateTime(x, '%Y-%m-%dT%H:%i:%S.%fZ', 'UTC'), `Events.Timestamp`) AS ev_time",
		"toFloat64(`Duration`) * 1 / 1e6 AS duration_ms",
		"ORDER BY `Timestamp` ASC LIMIT 5000",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in:\n%s", want, sql)
		}
	}
	src.Traces.Events = ""
	src.Traces.Links = ""
	sql = TraceSpansSQL(src, "x")
	if !strings.Contains(sql, "[] AS ev_name") || !strings.Contains(sql, "[] AS ev_time") || !strings.Contains(sql, "[] AS lk_trace") {
		t.Errorf("unmapped nested columns must render empty arrays:\n%s", sql)
	}
}

func TestZipEventsAndLinks(t *testing.T) {
	ev := ZipEvents([]string{"t1", "t2"}, []string{"exception", "retry", "extra"}, []map[string]string{{"exception.type": "E"}})
	if len(ev) != 3 || ev[0].Time != "t1" || ev[0].Attributes["exception.type"] != "E" || ev[2].Time != "" || ev[2].Attributes == nil {
		t.Fatalf("unexpected events: %+v", ev)
	}
	lk := ZipLinks([]string{"a", "b"}, []string{"s1"}, nil)
	if len(lk) != 2 || lk[0].SpanID != "s1" || lk[1].SpanID != "" || lk[1].Attributes == nil {
		t.Fatalf("unexpected links: %+v", lk)
	}
}

func TestBuildSpanTree(t *testing.T) {
	spans := []Span{
		{SpanID: "c2", ParentSpanID: "root", StartNs: "300"},
		{SpanID: "orphan", ParentSpanID: "missing", StartNs: "150"},
		{SpanID: "gc", ParentSpanID: "c1", StartNs: "250"},
		{SpanID: "root", ParentSpanID: "", StartNs: "100"},
		{SpanID: "c1", ParentSpanID: "root", StartNs: "200"},
	}
	out := BuildSpanTree(spans)
	if len(out) != 5 {
		t.Fatalf("expected 5 spans, got %d", len(out))
	}
	ids := make([]string, len(out))
	for i, s := range out {
		ids[i] = s.SpanID
		if s.Order != i {
			t.Errorf("span %s order %d at index %d", s.SpanID, s.Order, i)
		}
	}
	want := []string{"root", "c1", "gc", "c2", "orphan"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("pre-order wrong: %v", ids)
	}
	depths := map[string]int{"root": 0, "c1": 1, "gc": 2, "c2": 1, "orphan": 0}
	for _, s := range out {
		if s.Depth != depths[s.SpanID] {
			t.Errorf("span %s depth %d, want %d", s.SpanID, s.Depth, depths[s.SpanID])
		}
	}
	// A cycle must not hang and must not drop spans.
	cyc := BuildSpanTree([]Span{{SpanID: "a", ParentSpanID: "b", StartNs: "1"}, {SpanID: "b", ParentSpanID: "a", StartNs: "2"}})
	if len(cyc) != 2 {
		t.Fatalf("cycle lost spans: %+v", cyc)
	}
	if BuildSpanTree(nil) != nil {
		t.Fatalf("nil in, nil out")
	}
}
