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
		"ORDER BY `Timestamp` DESC, `TraceId` DESC, `SpanId` DESC LIMIT 200 WITH TIES",
		"ORDER BY `Timestamp` DESC, `TraceId` DESC, `SpanId` DESC, row_hash DESC LIMIT 200",
		"AS row_id",
		QuerySettings,
		"AS timestamp_ns",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in\n%s", want, sql)
		}
	}
	cur := Cursor{TimestampNs: "1757500000000000000", TraceID: "t", SpanID: "s", RowHash: "42"}
	sql, err = SearchSQL(src, nil, p, "asc", 10, &cur)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		// Rows after the cursor's timestamp...
		"toUnixTimestamp64Nano(toDateTime64(`Timestamp`, 9)) > 1757500000000000000 ORDER BY",
		// ...plus the rest of the cursor's tie group, keyed by the row hash.
		"toUnixTimestamp64Nano(toDateTime64(`Timestamp`, 9)) = 1757500000000000000 AND (`TraceId`, `SpanId`, cityHash64(",
		") > ('t', 's', 42)",
		" UNION ALL ",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("cursor keyset missing %q: %s", want, sql)
		}
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
	for _, c := range []Cursor{{TimestampNs: "1", TraceID: "a|b", SpanID: "c"}, {TimestampNs: "1", RowHash: "18446744073709551615"}} {
		got, err := DecodeCursor(EncodeCursor(c))
		if err != nil || got != c {
			t.Fatalf("round trip: %+v %v", got, err)
		}
	}
	for _, bad := range []string{"", "!!!", EncodeCursor(Cursor{TimestampNs: "x", TraceID: "", SpanID: ""}), EncodeCursor(Cursor{TimestampNs: "1", RowHash: "1 OR 1"})} {
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
	if !strings.Contains(f.Services, "LIMIT 200001") || !strings.Contains(f.Services, "sum(count()) OVER () AS total") {
		t.Errorf("facet sample limit / total missing: %s", f.Services)
	}
}

func TestFacetsDropTheirOwnFilter(t *testing.T) {
	src := testSource()
	p := LogsParams{From: "2026-09-10T10:00:00Z", To: "2026-09-10T11:00:00Z", Q: "http.method:GET timeout",
		Severity: []string{"ERROR"}, Services: []string{"api"}}
	f, err := BuildFacetsSQL(src, nil, p, []string{"http.method", "user.id"})
	if err != nil {
		t.Fatal(err)
	}
	sev := "`SeverityText` IN ('ERROR')"
	svc := "`ServiceName` IN ('api')"
	method := "`LogAttributes`['http.method'] = 'GET'"
	if strings.Contains(f.Severity, sev) || !strings.Contains(f.Severity, svc) || !strings.Contains(f.Severity, method) {
		t.Errorf("severity facet must drop only the severity filter: %s", f.Severity)
	}
	if strings.Contains(f.Services, svc) || !strings.Contains(f.Services, sev) {
		t.Errorf("services facet must drop only the services filter: %s", f.Services)
	}
	if strings.Contains(f.Values["http.method"], method) || !strings.Contains(f.Values["http.method"], "hasTokenCaseInsensitive(`Body`, 'timeout')") {
		t.Errorf("http.method values must drop the http.method term only: %s", f.Values["http.method"])
	}
	if !strings.Contains(f.Values["user.id"], method) || !strings.Contains(f.Keys, method) {
		t.Errorf("other facets keep the http.method filter")
	}
	if FacetSampled(200000) || !FacetSampled(200001) || FacetRowsScanned(200001) != 200000 {
		t.Errorf("sample flag")
	}
}

func TestContextAndByTraceSQL(t *testing.T) {
	src := testSource()
	b, a := ContextSQL(src, 1757500000000000000, "api", 50, 50)
	if !strings.Contains(b, "< 1757500000000000000 AND `ServiceName` = 'api' ORDER BY `Timestamp` DESC, row_hash DESC LIMIT 50") {
		t.Errorf("before: %s", b)
	}
	if !strings.Contains(b, "`Timestamp` >= fromUnixTimestamp64Nano(toInt64(1757496400000000000))") {
		t.Errorf("before must be bounded to an hour: %s", b)
	}
	if !strings.Contains(a, ">= 1757500000000000000 AND `ServiceName` = 'api' ORDER BY `Timestamp` ASC, row_hash ASC LIMIT 51") {
		t.Errorf("after: %s", a)
	}
	if !strings.Contains(a, "`Timestamp` <= fromUnixTimestamp64Nano(toInt64(1757503600000000000))") {
		t.Errorf("after must be bounded to an hour: %s", a)
	}
	s, err := ByTraceSQL(src, "abc'", nil)
	if err != nil || !strings.Contains(s, "`TraceId` = 'abc\\''") || strings.Contains(s, "fromUnixTimestamp64Nano") {
		t.Errorf("by trace: %s %v", s, err)
	}
	s, _ = ByTraceSQL(src, "abc", &TimeWindow{FromNs: 1, ToNs: 2})
	if !strings.Contains(s, "`Timestamp` >= fromUnixTimestamp64Nano(toInt64(1)) AND `Timestamp` <= fromUnixTimestamp64Nano(toInt64(2))") {
		t.Errorf("by trace window: %s", s)
	}
	src.Logs.TraceID = ""
	if _, err := ByTraceSQL(src, "x", nil); err == nil {
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
