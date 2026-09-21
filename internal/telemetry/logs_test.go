package telemetry

import (
	"strings"
	"testing"
)

func testSource() *Source {
	m := DefaultLogsMapping()
	return &Source{ID: "s1", Kind: KindLogs, Name: "Logs", Database: "default", Table: "otel_logs", Logs: &m, Enabled: true}
}

func TestSearchSQL(t *testing.T) {
	src := testSource()
	p := LogsParams{From: "2026-09-10T10:00:00Z", To: "2026-09-10T11:00:00Z", Q: "timeout service:api", Severity: []string{"ERROR", "WARN"}, Services: []string{"a'b"}}
	sql, err := SearchSQL(src, nil, p, "desc", 200, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"FROM `default`.`otel_logs`",
		"`Timestamp` >= parseDateTime64BestEffort('2026-09-10T10:00:00Z')",
		"`Timestamp` < parseDateTime64BestEffort('2026-09-10T11:00:00Z')",
		"hasTokenCaseInsensitive(`Body`, 'timeout')",
		"`ServiceName` = 'api'",
		"`SeverityText` IN ('ERROR', 'WARN')",
		"`ServiceName` IN ('a\\'b')",
		"ORDER BY `Timestamp` DESC, `TraceId` DESC, `SpanId` DESC LIMIT 200",
		QuerySettings,
		"AS timestamp_ns",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in\n%s", want, sql)
		}
	}
	cur := Cursor{TimestampNs: "1757500000000000000", TraceID: "t", SpanID: "s"}
	sql, err = SearchSQL(src, nil, p, "asc", 10, &cur)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, ", `TraceId`, `SpanId`) > (1757500000000000000, 't', 's')") {
		t.Errorf("cursor keyset missing: %s", sql)
	}
	if !strings.Contains(sql, "ORDER BY `Timestamp` ASC") {
		t.Errorf("asc order missing: %s", sql)
	}
	if _, err := SearchSQL(src, nil, LogsParams{Q: "(broken"}, "desc", 10, nil); err == nil {
		t.Errorf("bad query should fail")
	}
	if _, err := SearchSQL(src, nil, LogsParams{From: "yesterday"}, "desc", 10, nil); err == nil {
		t.Errorf("bad timestamp should fail")
	}
}

func TestCursorRoundTrip(t *testing.T) {
	c := Cursor{TimestampNs: "1", TraceID: "a|b", SpanID: "c"}
	got, err := DecodeCursor(EncodeCursor(c))
	if err != nil || got != c {
		t.Fatalf("round trip: %+v %v", got, err)
	}
	for _, bad := range []string{"", "!!!", EncodeCursor(Cursor{TimestampNs: "x", TraceID: "", SpanID: ""})} {
		if _, err := DecodeCursor(bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}

func TestBucketSeconds(t *testing.T) {
	cases := map[[2]string]int{
		{"2026-09-10T10:00:00Z", "2026-09-10T10:30:00Z"}: 10,
		{"2026-09-10T10:00:00Z", "2026-09-10T14:00:00Z"}: 60,
		{"2026-09-10T00:00:00Z", "2026-09-11T00:00:00Z"}: 300,
		{"2026-09-04T00:00:00Z", "2026-09-11T00:00:00Z"}: 3600,
		{"2026-08-01T00:00:00Z", "2026-09-11T00:00:00Z"}: 86400,
		{"bad", "worse"}: 60,
	}
	for in, want := range cases {
		if got := BucketSeconds(in[0], in[1]); got != want {
			t.Errorf("%v: got %d want %d", in, got, want)
		}
	}
}

func TestHistogramAndFacetsSQL(t *testing.T) {
	src := testSource()
	p := LogsParams{From: "2026-09-10T10:00:00Z", To: "2026-09-10T11:00:00Z"}
	h, err := HistogramSQL(src, nil, p, 60)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(h, "toStartOfInterval(`Timestamp`, INTERVAL 60 second, 'UTC')") || !strings.Contains(h, "GROUP BY t, severity") {
		t.Errorf("histogram: %s", h)
	}
	f, err := BuildFacetsSQL(src, nil, p, []string{"http.method", "bad'key"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Keys, "arrayJoin(mapKeys(`LogAttributes`))") || !strings.Contains(f.Keys, "'resource' AS src") {
		t.Errorf("keys: %s", f.Keys)
	}
	if v, ok := f.Values["http.method"]; !ok || !strings.Contains(v, "`LogAttributes`['http.method']") {
		t.Errorf("values: %v", f.Values)
	}
	if v := f.Values["bad'key"]; !strings.Contains(v, `'bad\'key'`) {
		t.Errorf("key not escaped: %s", v)
	}
	if !strings.Contains(f.Services, "LIMIT 200000") {
		t.Errorf("facet sample limit missing: %s", f.Services)
	}
}

func TestContextAndByTraceSQL(t *testing.T) {
	src := testSource()
	b, a := ContextSQL(src, "1757500000000000000", "api", 50, 50)
	if !strings.Contains(b, "< 1757500000000000000 AND `ServiceName` = 'api' ORDER BY `Timestamp` DESC LIMIT 50") {
		t.Errorf("before: %s", b)
	}
	if !strings.Contains(a, ">= 1757500000000000000 AND `ServiceName` = 'api' ORDER BY `Timestamp` ASC LIMIT 51") {
		t.Errorf("after: %s", a)
	}
	s, err := ByTraceSQL(src, "abc'")
	if err != nil || !strings.Contains(s, "`TraceId` = 'abc\\''") {
		t.Errorf("by trace: %s %v", s, err)
	}
	src.Logs.TraceID = ""
	if _, err := ByTraceSQL(src, "x"); err == nil {
		t.Errorf("no trace column should fail")
	}
}

func TestFacetsSQLOverJSONColumns(t *testing.T) {
	src := testSource()
	cols := map[string]string{"LogAttributes": "JSON", "ResourceAttributes": "Map(LowCardinality(String), String)"}
	p := LogsParams{From: "2026-09-10T10:00:00Z", To: "2026-09-10T11:00:00Z"}
	f, err := BuildFacetsSQL(src, cols, p, []string{"http.method"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(f.Keys, "arrayJoin(JSONAllPaths(`LogAttributes`))") || !strings.Contains(f.Keys, "arrayJoin(mapKeys(`ResourceAttributes`))") {
		t.Errorf("keys: %s", f.Keys)
	}
	v := f.Values["http.method"]
	if !strings.Contains(v, "toString(`LogAttributes`.`http`.`method`)") || !strings.Contains(v, "`ResourceAttributes`['http.method']") {
		t.Errorf("values: %s", v)
	}
	if !IsJSONColumn("JSON(max_dynamic_paths=64)") || !IsJSONColumn("Nullable(JSON)") || IsJSONColumn("Map(String, String)") {
		t.Errorf("IsJSONColumn misclassifies")
	}
	// The Map path is untouched when no column is JSON.
	plain, _ := BuildFacetsSQL(src, nil, p, []string{"http.method"})
	if !strings.Contains(plain.Values["http.method"], "`LogAttributes`['http.method']") {
		t.Errorf("map path changed: %s", plain.Values["http.method"])
	}
}
