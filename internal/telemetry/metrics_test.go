package telemetry

import (
	"math"
	"strings"
	"testing"
)

func metricsSource() *Source {
	t := DefaultMetricsTables()
	t.ExponentialHistogram = ""
	t.Summary = ""
	return &Source{ID: "m1", Kind: KindMetrics, Name: "Metrics", Database: "default", Tables: &t, Enabled: true}
}

func TestCatalogSQL(t *testing.T) {
	sql, err := CatalogSQL(metricsSource(), "2026-09-10T10:00:00Z", "2026-09-10T11:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"`default`.`otel_metrics_gauge`", "`default`.`otel_metrics_sum`", "`default`.`otel_metrics_histogram`", "UNION ALL", "'gauge' AS type", "uniq(`Attributes`) AS series", QuerySettings} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in\n%s", want, sql)
		}
	}
	if strings.Contains(sql, "exponential") {
		t.Errorf("absent tables must not be queried")
	}
}

func TestMetricQuerySQL(t *testing.T) {
	src := metricsSource()
	base := MetricsParams{From: "2026-09-10T10:00:00Z", To: "2026-09-10T11:00:00Z", Metric: "http.server.requests", BucketSeconds: 60}

	p := base
	p.Type, p.Aggregation, p.GroupBy, p.Q = MetricSum, "rate", []string{"http.method"}, "service:api http.status_code:200"
	sql, err := MetricQuerySQL(src, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"`MetricName` = 'http.server.requests'",
		"`ServiceName` = 'api'",
		"`Attributes`['http.status_code'] = '200'",
		"if(`Attributes`['http.method'] = '', 'other', `Attributes`['http.method']) AS g0",
		"cityHash64(`ServiceName`, `Attributes`) AS sid",
		"max(`Value`) AS v",
		"toStartOfInterval(`TimeUnix`, INTERVAL 60 second)",
		"GROUP BY t, g0, sid",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in\n%s", want, sql)
		}
	}

	p = base
	p.Type, p.Aggregation = MetricHistogram, "p95"
	sql, err = MetricQuerySQL(src, p)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"sumForEach(`BucketCounts`) AS counts", "any(`ExplicitBounds`) AS bounds", "sum(`Count`) AS cnt"} {
		if !strings.Contains(sql, want) {
			t.Errorf("missing %q in\n%s", want, sql)
		}
	}

	p = base
	p.Type, p.Aggregation = MetricGauge, "last"
	sql, _ = MetricQuerySQL(src, p)
	if !strings.Contains(sql, "argMax(`Value`, `TimeUnix`) AS v") {
		t.Errorf("last aggregation: %s", sql)
	}

	// Refusals.
	for _, bad := range []MetricsParams{
		{From: base.From, To: base.To, Metric: "x", Type: MetricGauge, Aggregation: "rate"},
		{From: base.From, To: base.To, Metric: "x", Type: MetricExponentialHistogram, Aggregation: "p95"},
		{From: base.From, To: base.To, Metric: "x", Type: MetricGauge, Aggregation: "avg", GroupBy: []string{"a", "b", "c"}},
		{From: base.From, To: base.To, Metric: "x", Type: MetricGauge, Aggregation: "avg", GroupBy: []string{"bad key"}},
		{From: base.From, To: base.To, Metric: "x", Type: MetricGauge, Aggregation: "avg", Q: "freetext"},
		{From: base.From, To: base.To, Metric: "", Type: MetricGauge, Aggregation: "avg"},
		{From: base.From, To: base.To, Metric: "x", Type: MetricSummary, Aggregation: "avg"}, // no summary table on this source
	} {
		if _, err := MetricQuerySQL(src, bad); err == nil {
			t.Errorf("expected error for %+v", bad)
		}
	}
	if _, err := compileMetricsQuery("service:api and"); err == nil {
		t.Errorf("dangling AND must fail")
	}
	if w, err := compileMetricsQuery(`http.method:GET`); err != nil || !strings.Contains(w, "`Attributes`['http.method'] = 'GET'") {
		t.Errorf("attribute filter: %s %v", w, err)
	}
}

func TestAttributesSQL(t *testing.T) {
	sql, err := AttributesSQL(metricsSource(), "2026-09-10T10:00:00Z", "2026-09-10T11:00:00Z", "cpu", MetricGauge)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sql, "arrayZip(mapKeys(`Attributes`), mapValues(`Attributes`))") || !strings.Contains(sql, "`MetricName` = 'cpu'") {
		t.Errorf("unexpected: %s", sql)
	}
}

func TestComputeRate(t *testing.T) {
	pts := []RatePoint{
		{T: 0, SID: "a", Max: 100}, {T: 60, SID: "a", Max: 160}, {T: 120, SID: "a", Max: 10}, {T: 180, SID: "a", Max: 70},
		{T: 0, SID: "b", Max: 0}, {T: 60, SID: "b", Max: 30}, {T: 120, SID: "b", Max: 30}, {T: 180, SID: "b", Max: 90},
	}
	got := ComputeRate(pts, 60)
	if _, ok := got[0]; ok {
		t.Errorf("first bucket has no rate")
	}
	if math.Abs(got[60]-1.5) > 1e-9 { // (60 + 30) / 60
		t.Errorf("t=60: want 1.5, got %v", got[60])
	}
	if math.Abs(got[120]-0) > 1e-9 { // reset on a -> 0, b flat -> 0
		t.Errorf("t=120: want 0, got %v", got[120])
	}
	if math.Abs(got[180]-2.0) > 1e-9 { // (60 + 60) / 60
		t.Errorf("t=180: want 2, got %v", got[180])
	}
}

func TestHistogramQuantile(t *testing.T) {
	bounds := []float64{5, 10, 25, 50, 100}
	counts := []float64{10, 10, 20, 40, 10, 10} // total 100, last is +Inf
	if v := HistogramQuantile(bounds, counts, 0.5); math.Abs(v-31.25) > 1e-9 {
		// rank 50: cum 10,20,40,80 -> bucket (25,50], frac (50-40)/40 = 0.25 -> 31.25
		t.Errorf("p50: want 31.25, got %v", v)
	}
	if v := HistogramQuantile(bounds, counts, 0.1); math.Abs(v-5) > 1e-9 {
		t.Errorf("p10: want 5, got %v", v)
	}
	if v := HistogramQuantile(bounds, counts, 0.99); v != 100 {
		t.Errorf("p99 in +Inf bucket reports last bound: got %v", v)
	}
	if v := HistogramQuantile(bounds, []float64{0, 0, 0, 0, 0, 0}, 0.5); !math.IsNaN(v) {
		t.Errorf("empty histogram must be NaN")
	}
}

func TestFillAxisAndBucket(t *testing.T) {
	axis := FillAxis("2026-09-10T10:00:10Z", "2026-09-10T10:05:00Z", 60)
	if len(axis) != 6 || axis[0]%60 != 0 {
		t.Errorf("axis: %v", axis)
	}
	if b := MetricBucketSeconds("2026-09-10T00:00:00Z", "2026-09-11T00:00:00Z", 0); b != 300 {
		t.Errorf("24h bucket: want 300, got %d", b)
	}
	if b := MetricBucketSeconds("2026-09-10T00:00:00Z", "2026-09-11T00:00:00Z", 7); b != 7 {
		t.Errorf("requested bucket must win")
	}
}
