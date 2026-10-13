package telemetry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// liveCH returns an exec function for the disposable ClickHouse named by
// CHUI_TEST_CLICKHOUSE_URL, or skips.
func liveCH(t *testing.T) func(string) ([]map[string]interface{}, error) {
	t.Helper()
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable ClickHouse")
	}
	client := &http.Client{Timeout: 30 * time.Second}
	return func(query string) ([]map[string]interface{}, error) {
		req, err := http.NewRequest(http.MethodPost, endpoint, strings.NewReader(query))
		if err != nil {
			return nil, err
		}
		user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
		if user == "" {
			user = "default"
		}
		req.SetBasicAuth(user, os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"))
		res, err := client.Do(req)
		if err != nil {
			return nil, err
		}
		defer res.Body.Close()
		body, err := io.ReadAll(res.Body)
		if err != nil {
			return nil, err
		}
		if res.StatusCode != 200 {
			return nil, fmt.Errorf("HTTP %d: %s", res.StatusCode, body)
		}
		if len(body) == 0 {
			return nil, nil
		}
		var payload struct {
			Data []map[string]interface{} `json:"data"`
		}
		err = json.Unmarshal(body, &payload)
		return payload.Data, err
	}
}

// otelSchema creates a throwaway database with the OpenTelemetry
// exporter's logs, traces, trace-id index and metric tables.
func otelSchema(t *testing.T, exec func(string) ([]map[string]interface{}, error)) string {
	t.Helper()
	db := fmt.Sprintf("chui_tel_test_%d", time.Now().UnixNano())
	ddl := []string{
		"CREATE DATABASE " + db,
		`CREATE TABLE ` + db + `.otel_logs (
			Timestamp DateTime64(9), TimestampTime DateTime DEFAULT toDateTime(Timestamp),
			TraceId String, SpanId String, TraceFlags UInt8, SeverityText LowCardinality(String), SeverityNumber UInt8,
			ServiceName LowCardinality(String), Body String, ResourceSchemaUrl LowCardinality(String),
			ResourceAttributes Map(LowCardinality(String), String), ScopeSchemaUrl LowCardinality(String), ScopeName String,
			ScopeVersion LowCardinality(String), ScopeAttributes Map(LowCardinality(String), String),
			LogAttributes Map(LowCardinality(String), String), EventName String
		) ENGINE = MergeTree PARTITION BY toDate(TimestampTime) PRIMARY KEY (ServiceName, TimestampTime) ORDER BY (ServiceName, TimestampTime, Timestamp)`,
		`CREATE TABLE ` + db + `.otel_traces (
			Timestamp DateTime64(9), TraceId String, SpanId String, ParentSpanId String, TraceState String,
			SpanName LowCardinality(String), SpanKind LowCardinality(String), ServiceName LowCardinality(String),
			ResourceAttributes Map(LowCardinality(String), String), ScopeName String, ScopeVersion String,
			SpanAttributes Map(LowCardinality(String), String), Duration UInt64, StatusCode LowCardinality(String), StatusMessage String,
			Events Nested(Timestamp DateTime64(9), Name LowCardinality(String), Attributes Map(LowCardinality(String), String)),
			Links Nested(TraceId String, SpanId String, TraceState String, Attributes Map(LowCardinality(String), String))
		) ENGINE = MergeTree PARTITION BY toDate(Timestamp) ORDER BY (ServiceName, SpanName, toDateTime(Timestamp))`,
		`CREATE TABLE ` + db + `.otel_traces_trace_id_ts (TraceId String, Start DateTime, End DateTime)
			ENGINE = MergeTree PARTITION BY toDate(Start) ORDER BY (TraceId, Start)`,
		`CREATE TABLE ` + db + `.otel_metrics_sum (
			ResourceAttributes Map(LowCardinality(String), String), ServiceName LowCardinality(String), MetricName String,
			MetricDescription String, MetricUnit String, Attributes Map(LowCardinality(String), String),
			StartTimeUnix DateTime64(9), TimeUnix DateTime64(9), Value Float64, Flags UInt32,
			AggregationTemporality Int32, IsMonotonic Bool, ScopeName String
		) ENGINE = MergeTree ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Nano(TimeUnix))`,
		`CREATE TABLE ` + db + `.otel_metrics_histogram (
			ResourceAttributes Map(LowCardinality(String), String), ServiceName LowCardinality(String), MetricName String,
			MetricDescription String, MetricUnit String, Attributes Map(LowCardinality(String), String),
			StartTimeUnix DateTime64(9), TimeUnix DateTime64(9), Count UInt64, Sum Float64, BucketCounts Array(UInt64),
			ExplicitBounds Array(Float64), Flags UInt32, Min Float64, Max Float64, AggregationTemporality Int32, ScopeName String
		) ENGINE = MergeTree ORDER BY (ServiceName, MetricName, Attributes, toUnixTimestamp64Nano(TimeUnix))`,
	}
	t.Cleanup(func() {
		if _, err := exec("DROP DATABASE IF EXISTS " + db); err != nil {
			t.Error(err)
		}
	})
	for _, q := range ddl {
		if _, err := exec(q); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	return db
}

func mustRows(t *testing.T, exec func(string) ([]map[string]interface{}, error), name, q string) []map[string]interface{} {
	t.Helper()
	rows, err := exec(q + " FORMAT JSON")
	if err != nil {
		t.Fatalf("%s: %v\n%s", name, err, q)
	}
	return rows
}

func TestTelemetrySQLLive(t *testing.T) {
	exec := liveCH(t)
	db := otelSchema(t, exec)
	base := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

	// ── Logs: ties on (timestamp, trace, span) ────────────────────
	var values []string
	for i := 0; i < 25; i++ {
		ts := base.Add(time.Duration(i/10) * time.Second) // groups of 10 share a second
		sev := []string{"INFO", "WARN", "ERROR"}[i%3]
		values = append(values, fmt.Sprintf("('%s', '', '', '%s', 'api', 'line %d', map('http.method', '%s'))",
			ts.Format("2006-01-02 15:04:05.000000000"), sev, i, []string{"GET", "POST"}[i%2]))
	}
	values = append(values, fmt.Sprintf("('%s', 'trace-1', 'span-1', 'INFO', 'api', 'traced', map())", base.Add(5*time.Minute).Format("2006-01-02 15:04:05")))
	if _, err := exec("INSERT INTO " + db + ".otel_logs (Timestamp, TraceId, SpanId, SeverityText, ServiceName, Body, LogAttributes) VALUES " + strings.Join(values, ", ")); err != nil {
		t.Fatal(err)
	}
	m := DefaultLogsMapping()
	logs := &Source{Kind: KindLogs, Database: db, Table: "otel_logs", Logs: &m}
	p := LogsParams{From: base.Add(-time.Minute).Format(time.RFC3339), To: base.Add(time.Minute).Format(time.RFC3339)}
	for _, order := range []string{"desc", "asc"} {
		seen := map[string]bool{}
		var cursor *Cursor
		pages := 0
		for {
			q, err := SearchSQL(logs, nil, p, order, 4, cursor) // page of 3, one extra
			if err != nil {
				t.Fatal(err)
			}
			rows := mustRows(t, exec, "search "+order, q)
			pages++
			if pages > 20 {
				t.Fatalf("%s: paging does not end", order)
			}
			more := len(rows) > 3
			if more {
				rows = rows[:3]
			}
			for _, r := range rows {
				id := fmt.Sprint(r["timestamp_ns"], "/", r["row_id"])
				if seen[id] {
					t.Fatalf("%s: row %v returned twice", order, r["body"])
				}
				seen[id] = true
			}
			if !more {
				break
			}
			last := rows[len(rows)-1]
			cursor = &Cursor{TimestampNs: fmt.Sprint(last["timestamp_ns"]), TraceID: fmt.Sprint(last["trace_id"]), SpanID: fmt.Sprint(last["span_id"]), RowHash: fmt.Sprint(last["row_id"])}
		}
		if len(seen) != 25 {
			t.Fatalf("%s paging returned %d of 25 rows", order, len(seen))
		}
	}

	// Facets: each drops its own filter and reports its sample size.
	fp := p
	fp.Severity = []string{"ERROR"}
	fp.Q = "http.method:GET"
	facets, err := BuildFacetsSQL(logs, nil, fp, []string{"http.method"})
	if err != nil {
		t.Fatal(err)
	}
	sev := mustRows(t, exec, "severity facet", facets.Severity)
	if len(sev) != 3 {
		t.Fatalf("severity facet must ignore the severity filter: %+v", sev)
	}
	methods := mustRows(t, exec, "method facet", facets.Values["http.method"])
	if len(methods) != 2 {
		t.Fatalf("http.method facet must ignore its own term: %+v", methods)
	}
	if fmt.Sprint(methods[0]["total"]) == "" {
		t.Fatalf("facet total missing: %+v", methods)
	}
	mustRows(t, exec, "keys facet", facets.Keys)
	mustRows(t, exec, "services facet", facets.Services)

	// Context around a tied anchor.
	anchor := base.UnixNano()
	before, after := ContextSQL(logs, anchor, "api", 50, 50)
	if b := mustRows(t, exec, "context before", before); len(b) != 0 {
		t.Fatalf("nothing precedes the first second: %d", len(b))
	}
	if a := mustRows(t, exec, "context after", after); len(a) != 26 { // the 25 lines plus the traced one five minutes later
		t.Fatalf("context after: %d rows", len(a))
	}

	// By-trace with and without a window.
	byTrace, _ := ByTraceSQL(logs, "trace-1", Around(base.Add(5*time.Minute), time.Hour, time.Hour))
	if r := mustRows(t, exec, "by trace", byTrace); len(r) != 1 {
		t.Fatalf("by trace in window: %d", len(r))
	}
	byTrace, _ = ByTraceSQL(logs, "trace-1", Around(base.Add(5*time.Hour), time.Minute, time.Minute))
	if r := mustRows(t, exec, "by trace out of window", byTrace); len(r) != 0 {
		t.Fatalf("by trace out of window: %d", len(r))
	}

	// ── Traces ────────────────────────────────────────────────────
	spans := []string{}
	for i := 0; i < 40; i++ {
		trace := fmt.Sprintf("t%02d", i)
		ts := base.Add(time.Duration(i) * time.Second).Format("2006-01-02 15:04:05")
		status := []string{"Ok", "Error", "STATUS_CODE_ERROR", "Unset"}[i%4]
		spans = append(spans,
			fmt.Sprintf("('%s', '%s', 'root', '', 'GET /', 'Server', 'frontend', 5000000, '%s')", ts, trace, status),
			fmt.Sprintf("('%s', '%s', 'child', 'root', 'SELECT', 'Client', 'db', 1000000, 'Unset')", ts, trace))
	}
	if _, err := exec("INSERT INTO " + db + ".otel_traces (Timestamp, TraceId, SpanId, ParentSpanId, SpanName, SpanKind, ServiceName, Duration, StatusCode) VALUES " + strings.Join(spans, ", ")); err != nil {
		t.Fatal(err)
	}
	if _, err := exec(fmt.Sprintf("INSERT INTO %s.otel_traces_trace_id_ts VALUES ('t03', '%s', '%s')", db,
		base.Add(3*time.Second).Format("2006-01-02 15:04:05"), base.Add(4*time.Second).Format("2006-01-02 15:04:05"))); err != nil {
		t.Fatal(err)
	}
	tm := DefaultTracesMapping()
	traces := &Source{Kind: KindTraces, Database: db, Table: "otel_traces", Traces: &tm}
	if r := mustRows(t, exec, "trace_id_ts lookup", TraceIDTsLookupSQL(traces)); len(r) != 1 {
		t.Fatalf("trace_id_ts table not found: %+v", r)
	}
	bounds := mustRows(t, exec, "trace bounds", TraceBoundsSQL(traces, "t03"))
	if fmt.Sprint(bounds[0]["n"]) != "1" {
		t.Fatalf("trace bounds: %+v", bounds)
	}
	win := Around(base.Add(3*time.Second), time.Minute, time.Minute)
	if r := mustRows(t, exec, "trace spans", TraceSpansSQL(traces, "t03", win)); len(r) != 2 {
		t.Fatalf("trace spans in window: %d", len(r))
	}
	tp := TracesParams{From: base.Add(-time.Minute).Format(time.RFC3339), To: base.Add(time.Minute).Format(time.RFC3339)}
	ts, _ := TraceSearchSQL(traces, nil, tp, 100, nil)
	errorsSeen := 0
	for _, r := range mustRows(t, exec, "trace search", ts) {
		if r["status"] == "Error" {
			errorsSeen++
		}
	}
	if errorsSeen != 20 {
		t.Fatalf("both error spellings must count: %d of 20", errorsSeen)
	}
	tf, err := BuildTraceFacetsSQL(traces, nil, TracesParams{From: tp.From, To: tp.To, Q: "service:db"})
	if err != nil {
		t.Fatal(err)
	}
	if r := mustRows(t, exec, "trace service facet", tf.Services); len(r) != 2 {
		t.Fatalf("service facet must ignore service:db: %+v", r)
	}
	mp := ServiceMapParams{From: tp.From, To: tp.To}
	countSQL, _ := ServiceMapCountSQL(traces, mp)
	if c := mustRows(t, exec, "map count", countSQL); fmt.Sprint(c[0]["c"]) != "80" {
		t.Fatalf("map count: %+v", c)
	}
	for _, rate := range []int64{1, 2} {
		nodesSQL, _ := ServiceMapNodesSQL(traces, mp, rate)
		edgesSQL, _ := ServiceMapEdgesSQL(traces, mp, rate)
		mustRows(t, exec, "map nodes", nodesSQL)
		edges := mustRows(t, exec, "map edges", edgesSQL)
		if len(edges) != 1 || edges[0]["from_service"] != "frontend" {
			t.Fatalf("rate %d edges: %+v", rate, edges)
		}
		if rate == 1 && fmt.Sprint(edges[0]["calls"]) != "40" {
			t.Fatalf("unsampled calls: %+v", edges)
		}
	}

	// ── Metrics ───────────────────────────────────────────────────
	if _, err := exec(fmt.Sprintf(`INSERT INTO %s.otel_metrics_sum (ResourceAttributes, ServiceName, MetricName, Attributes, TimeUnix, Value, AggregationTemporality, IsMonotonic) VALUES
		(map('host', 'a'), 'api', 'requests', map(), '%s', 10, 2, true),
		(map('host', 'b'), 'api', 'requests', map(), '%s', 100, 2, true),
		(map('host', 'a'), 'api', 'requests', map(), '%s', 70, 2, true),
		(map('host', 'b'), 'api', 'requests', map(), '%s', 160, 2, true),
		(map(), 'api', 'deltas', map(), '%s', 5, 1, true)`, db,
		base.Format("2006-01-02 15:04:05"), base.Format("2006-01-02 15:04:05"),
		base.Add(time.Minute).Format("2006-01-02 15:04:05"), base.Add(time.Minute).Format("2006-01-02 15:04:05"),
		base.Format("2006-01-02 15:04:05"))); err != nil {
		t.Fatal(err)
	}
	if _, err := exec(fmt.Sprintf(`INSERT INTO %s.otel_metrics_histogram (ServiceName, MetricName, Attributes, TimeUnix, Count, BucketCounts, ExplicitBounds, AggregationTemporality) VALUES
		('api', 'latency', map(), '%s', 10, [5, 5], [100], 2),
		('api', 'latency', map(), '%s', 20, [15, 5], [100], 2)`, db,
		base.Format("2006-01-02 15:04:05"), base.Add(time.Minute).Format("2006-01-02 15:04:05"))); err != nil {
		t.Fatal(err)
	}
	mt := DefaultMetricsTables()
	metrics := &Source{Kind: KindMetrics, Database: db, Tables: &mt}
	mpar := MetricsParams{From: base.Add(-time.Minute).Format(time.RFC3339), To: base.Add(2 * time.Minute).Format(time.RFC3339),
		Metric: "requests", Type: MetricSum, Aggregation: "rate", BucketSeconds: 60}
	rateSQL, err := MetricQuerySQL(metrics, mpar)
	if err != nil {
		t.Fatal(err)
	}
	rateRows := mustRows(t, exec, "rate", rateSQL)
	var pts []RatePoint
	sids := map[string]bool{}
	for _, r := range rateRows {
		if fmt.Sprint(r["cum"]) != "1" {
			t.Fatalf("cumulative sum flagged as not cumulative: %+v", r)
		}
		sids[fmt.Sprint(r["sid"])] = true
		v, _ := r["v"].(float64)
		tf, _ := r["t"].(float64)
		tt := int64(tf)
		pts = append(pts, RatePoint{T: tt, SID: fmt.Sprint(r["sid"]), Max: v})
	}
	if len(sids) != 2 {
		t.Fatalf("hosts a and b must be two series: %d", len(sids))
	}
	got := ComputeRate(pts, 60)
	if got[base.Add(time.Minute).Unix()] != 2 { // (60 + 60) / 60
		t.Fatalf("rate across two hosts: %+v", got)
	}
	mpar.Metric = "deltas"
	deltaSQL, _ := MetricQuerySQL(metrics, mpar)
	for _, r := range mustRows(t, exec, "delta rate", deltaSQL) {
		if fmt.Sprint(r["cum"]) != "0" {
			t.Fatalf("delta sum must be flagged: %+v", r)
		}
	}
	hpar := MetricsParams{From: mpar.From, To: mpar.To, Metric: "latency", Type: MetricHistogram, Aggregation: "p50", BucketSeconds: 60}
	histSQL, err := MetricQuerySQL(metrics, hpar)
	if err != nil {
		t.Fatal(err)
	}
	if r := mustRows(t, exec, "histogram p50", histSQL); len(r) != 2 {
		t.Fatalf("histogram rows: %+v", r)
	}
}
