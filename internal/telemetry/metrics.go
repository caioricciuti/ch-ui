package telemetry

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/telemetry/query"
)

// MetricType is one of the exporter's per-type tables.
type MetricType string

const (
	MetricGauge                MetricType = "gauge"
	MetricSum                  MetricType = "sum"
	MetricHistogram            MetricType = "histogram"
	MetricExponentialHistogram MetricType = "exponential_histogram"
	MetricSummary              MetricType = "summary"
)

// AllMetricTypes lists the types in display order.
var AllMetricTypes = []MetricType{MetricGauge, MetricSum, MetricHistogram, MetricExponentialHistogram, MetricSummary}

// TableFor returns the configured table for a metric type ("" when absent).
func (t *MetricsTables) TableFor(typ MetricType) string {
	if t == nil {
		return ""
	}
	switch typ {
	case MetricGauge:
		return t.Gauge
	case MetricSum:
		return t.Sum
	case MetricHistogram:
		return t.Histogram
	case MetricExponentialHistogram:
		return t.ExponentialHistogram
	case MetricSummary:
		return t.Summary
	}
	return ""
}

// metricAggregations lists what each type supports.
var metricAggregations = map[MetricType][]string{
	MetricGauge:                {"avg", "sum", "min", "max", "last", "count"},
	MetricSum:                  {"avg", "sum", "min", "max", "last", "count", "rate"},
	MetricHistogram:            {"avg", "sum", "min", "max", "count", "p50", "p95", "p99"},
	MetricExponentialHistogram: {"count", "avg"},
	MetricSummary:              {"count", "sum", "avg"},
}

// SupportedAggregation reports whether an aggregation applies to a type.
func SupportedAggregation(typ MetricType, agg string) bool {
	for _, a := range metricAggregations[typ] {
		if a == agg {
			return true
		}
	}
	return false
}

// Attribute keys must be plain (they are quoted as literals inside the map
// index, but a sane allowlist keeps SQL readable and blocks nonsense).
func validAttributeKey(k string) bool {
	if k == "" || len(k) > 128 {
		return false
	}
	for _, r := range k {
		if !(r == '.' || r == '_' || r == '-' || r == '/' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
}

// freeTextSentinel makes the compiler's bare-word output detectable so it
// can be refused: metrics have no text column to search.
const freeTextSentinel = "__metrics_free_text__"

// MetricsSchema is the search schema shared by the metric tables.
func MetricsSchema() query.Schema {
	return query.Schema{
		TextExpr: freeTextSentinel,
		Roles: map[string]string{
			"service": Quote("ServiceName"),
			"metric":  Quote("MetricName"),
			"scope":   Quote("ScopeName"),
		},
		Aliases:       map[string]string{"service_name": "service", "servicename": "service", "metricname": "metric", "name": "metric"},
		AttributeMaps: []string{Quote("Attributes"), Quote("ResourceAttributes")},
	}
}

// compileMetricsQuery compiles q and refuses free text.
func compileMetricsQuery(q string) (string, error) {
	where, err := query.Compile(q, MetricsSchema())
	if err != nil {
		return "", err
	}
	if strings.Contains(where, freeTextSentinel) {
		return "", fmt.Errorf("use field:value on metrics (for example service:api or http.method:GET)")
	}
	return where, nil
}

// MetricsParams are the inputs of /metrics/query.
type MetricsParams struct {
	From, To      string
	Q             string
	Metric        string
	Type          MetricType
	Aggregation   string
	GroupBy       []string
	BucketSeconds int
	LimitSeries   int
}

func metricsTable(src *Source, typ MetricType) (string, error) {
	t := src.Tables.TableFor(typ)
	if t == "" {
		return "", fmt.Errorf("this source has no %s table", typ)
	}
	if !ValidIdent(src.Database) || !ValidIdent(t) {
		return "", fmt.Errorf("invalid table name")
	}
	return Quote(src.Database) + "." + Quote(t), nil
}

// metricsWhere builds the range + metric + search filter.
func metricsWhere(from, to, metric, q string) (string, error) {
	var conds []string
	if from != "" {
		c, err := TimeBound("TimeUnix", ">=", from)
		if err != nil {
			return "", err
		}
		conds = append(conds, c)
	}
	if to != "" {
		c, err := TimeBound("TimeUnix", "<=", to)
		if err != nil {
			return "", err
		}
		conds = append(conds, c)
	}
	if metric != "" {
		conds = append(conds, Quote("MetricName")+" = "+Literal(metric))
	}
	if strings.TrimSpace(q) != "" {
		w, err := compileMetricsQuery(q)
		if err != nil {
			return "", err
		}
		conds = append(conds, "("+w+")")
	}
	if len(conds) == 0 {
		return "1", nil
	}
	return strings.Join(conds, " AND "), nil
}

// CatalogSQL lists every metric across the present tables.
func CatalogSQL(src *Source, from, to string) (string, error) {
	if src.Tables == nil {
		return "", fmt.Errorf("metrics source has no tables")
	}
	where, err := metricsWhere(from, to, "", "")
	if err != nil {
		return "", err
	}
	var parts []string
	for _, typ := range AllMetricTypes {
		t := src.Tables.TableFor(typ)
		if t == "" {
			continue
		}
		table, err := metricsTable(src, typ)
		if err != nil {
			return "", err
		}
		parts = append(parts, fmt.Sprintf(
			"SELECT %s AS name, %s AS type, any(%s) AS unit, any(%s) AS description, uniq(%s) AS services, uniq(%s) AS series FROM %s WHERE %s GROUP BY name",
			Quote("MetricName"), Literal(string(typ)), Quote("MetricUnit"), Quote("MetricDescription"), Quote("ServiceName"), Quote("Attributes"), table, where))
	}
	if len(parts) == 0 {
		return "", fmt.Errorf("metrics source has no tables")
	}
	return "SELECT * FROM (" + strings.Join(parts, " UNION ALL ") + ") ORDER BY name, type " + QuerySettings, nil
}

// MetricBucketSeconds picks the bucket so a range renders in at most ~360 points.
func MetricBucketSeconds(from, to string, requested int) int {
	if requested > 0 {
		return requested
	}
	f, err1 := time.Parse(time.RFC3339Nano, from)
	t, err2 := time.Parse(time.RFC3339Nano, to)
	if err1 != nil || err2 != nil || !t.After(f) {
		return 60
	}
	secs := int(t.Sub(f).Seconds())
	for _, b := range []int{15, 30, 60, 120, 300, 600, 900, 1800, 3600, 7200, 21600, 43200, 86400} {
		if secs/b <= 360 {
			return b
		}
	}
	return 86400
}

// labelExprs renders the group-by columns: g0, g1 with 'other' for missing.
func labelExprs(keys []string) ([]string, error) {
	out := make([]string, 0, len(keys))
	for i, k := range keys {
		if !validAttributeKey(k) {
			return nil, fmt.Errorf("invalid group_by key %q", k)
		}
		access := fmt.Sprintf("%s[%s]", Quote("Attributes"), Literal(k))
		out = append(out, fmt.Sprintf("if(%s = '', 'other', %s) AS g%d", access, access, i))
	}
	return out, nil
}

// MetricQuerySQL builds the bucketed series query. Rows carry: t (unix
// seconds), g0..gN labels, n (points), unit, and the aggregation columns
// read by the handler (v for plain aggregations; sid + v for rate; counts,
// bounds, cnt, total for histogram quantiles).
func MetricQuerySQL(src *Source, p MetricsParams) (string, error) {
	if src.Tables == nil {
		return "", fmt.Errorf("metrics source has no tables")
	}
	if p.Metric == "" {
		return "", fmt.Errorf("metric is required")
	}
	if !SupportedAggregation(p.Type, p.Aggregation) {
		if p.Type == MetricExponentialHistogram {
			return "", fmt.Errorf("%s is not supported for exponential histograms", p.Aggregation)
		}
		return "", fmt.Errorf("%s is not supported for %s metrics", p.Aggregation, p.Type)
	}
	if len(p.GroupBy) > 2 {
		return "", fmt.Errorf("group_by accepts at most 2 keys")
	}
	table, err := metricsTable(src, p.Type)
	if err != nil {
		return "", err
	}
	where, err := metricsWhere(p.From, p.To, p.Metric, p.Q)
	if err != nil {
		return "", err
	}
	labels, err := labelExprs(p.GroupBy)
	if err != nil {
		return "", err
	}
	bucket := p.BucketSeconds
	if bucket <= 0 {
		bucket = 60
	}
	selects := []string{fmt.Sprintf("toUnixTimestamp(toStartOfInterval(%s, INTERVAL %d second)) AS t", Quote("TimeUnix"), bucket)}
	selects = append(selects, labels...)
	groups := []string{"t"}
	for i := range labels {
		groups = append(groups, fmt.Sprintf("g%d", i))
	}

	switch p.Type {
	case MetricGauge, MetricSum:
		if p.Aggregation == "rate" {
			// One row per raw series (attribute set) so counter resets and
			// deltas are computed per series, then summed per label group.
			selects = append(selects, fmt.Sprintf("cityHash64(%s, %s) AS sid", Quote("ServiceName"), Quote("Attributes")))
			selects = append(selects, fmt.Sprintf("max(%s) AS v", Quote("Value")))
			groups = append(groups, "sid")
		} else {
			selects = append(selects, plainAggregation(p.Aggregation)+" AS v")
		}
	case MetricHistogram:
		switch p.Aggregation {
		case "p50", "p95", "p99":
			selects = append(selects,
				fmt.Sprintf("sumForEach(%s) AS counts", Quote("BucketCounts")),
				fmt.Sprintf("any(%s) AS bounds", Quote("ExplicitBounds")),
				fmt.Sprintf("sum(%s) AS cnt", Quote("Count")))
		case "avg":
			selects = append(selects, fmt.Sprintf("sum(%s) / nullIf(sum(%s), 0) AS v", Quote("Sum"), Quote("Count")))
		case "sum":
			selects = append(selects, fmt.Sprintf("sum(%s) AS v", Quote("Sum")))
		case "count":
			selects = append(selects, fmt.Sprintf("sum(%s) AS v", Quote("Count")))
		case "min":
			selects = append(selects, fmt.Sprintf("min(%s) AS v", Quote("Min")))
		case "max":
			selects = append(selects, fmt.Sprintf("max(%s) AS v", Quote("Max")))
		}
	case MetricExponentialHistogram, MetricSummary:
		switch p.Aggregation {
		case "count":
			selects = append(selects, fmt.Sprintf("sum(%s) AS v", Quote("Count")))
		case "sum":
			selects = append(selects, fmt.Sprintf("sum(%s) AS v", Quote("Sum")))
		case "avg":
			selects = append(selects, fmt.Sprintf("sum(%s) / nullIf(sum(%s), 0) AS v", Quote("Sum"), Quote("Count")))
		}
	}
	selects = append(selects, "count() AS n", fmt.Sprintf("any(%s) AS unit", Quote("MetricUnit")))
	return fmt.Sprintf("SELECT %s FROM %s WHERE %s GROUP BY %s ORDER BY %s %s",
		strings.Join(selects, ", "), table, where, strings.Join(groups, ", "), strings.Join(groups, ", "), QuerySettings), nil
}

func plainAggregation(agg string) string {
	v := Quote("Value")
	switch agg {
	case "avg":
		return "avg(" + v + ")"
	case "sum":
		return "sum(" + v + ")"
	case "min":
		return "min(" + v + ")"
	case "max":
		return "max(" + v + ")"
	case "last":
		return "argMax(" + v + ", " + Quote("TimeUnix") + ")"
	case "count":
		return "count()"
	}
	return "avg(" + v + ")"
}

// AttributesSQL returns (key, value, count) triples over the range for a
// metric, capped; the handler ranks keys and values.
func AttributesSQL(src *Source, from, to, metric string, typ MetricType) (string, error) {
	if src.Tables == nil {
		return "", fmt.Errorf("metrics source has no tables")
	}
	table, err := metricsTable(src, typ)
	if err != nil {
		return "", err
	}
	where, err := metricsWhere(from, to, metric, "")
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"SELECT kv.1 AS k, kv.2 AS v, count() AS c FROM (SELECT arrayJoin(arrayZip(mapKeys(%s), mapValues(%s))) AS kv FROM %s WHERE %s LIMIT 200000) GROUP BY k, v ORDER BY c DESC LIMIT 5000 %s",
		Quote("Attributes"), Quote("Attributes"), table, where, QuerySettings), nil
}

// ── Post-processing in Go ──────────────────────────────────────────

// RatePoint is one bucket max of one raw counter series.
type RatePoint struct {
	T   int64
	SID string
	Max float64
}

// ComputeRate turns per-series bucket maxima into a per-bucket rate
// (units per second), summed across the raw series of a label group.
// A drop between consecutive buckets is a counter reset and yields 0 for
// that bucket. The first bucket of every series has no rate (nil).
func ComputeRate(points []RatePoint, bucketSeconds int) map[int64]float64 {
	bySeries := map[string][]RatePoint{}
	for _, p := range points {
		bySeries[p.SID] = append(bySeries[p.SID], p)
	}
	out := map[int64]float64{}
	for _, pts := range bySeries {
		sort.Slice(pts, func(i, j int) bool { return pts[i].T < pts[j].T })
		for i := 1; i < len(pts); i++ {
			dt := float64(pts[i].T - pts[i-1].T)
			if dt <= 0 {
				dt = float64(bucketSeconds)
			}
			delta := pts[i].Max - pts[i-1].Max
			if delta < 0 {
				delta = 0 // counter reset
			}
			out[pts[i].T] += delta / dt
		}
	}
	return out
}

// HistogramQuantile interpolates a quantile (0..1) from cumulative bucket
// counts and their explicit upper bounds. counts has len(bounds)+1 entries
// (the last is the +Inf bucket). Returns NaN when there are no samples.
func HistogramQuantile(bounds []float64, counts []float64, q float64) float64 {
	if len(counts) == 0 {
		return math.NaN()
	}
	total := 0.0
	for _, c := range counts {
		total += c
	}
	if total <= 0 {
		return math.NaN()
	}
	rank := q * total
	cum := 0.0
	for i, c := range counts {
		prev := cum
		cum += c
		if cum < rank || c == 0 {
			continue
		}
		var lo, hi float64
		switch {
		case i == 0:
			lo, hi = 0, boundAt(bounds, 0)
		case i >= len(bounds):
			// +Inf bucket: report the last finite bound.
			return boundAt(bounds, len(bounds)-1)
		default:
			lo, hi = bounds[i-1], bounds[i]
		}
		if c == 0 {
			return hi
		}
		frac := (rank - prev) / c
		if frac < 0 {
			frac = 0
		}
		if frac > 1 {
			frac = 1
		}
		return lo + (hi-lo)*frac
	}
	return boundAt(bounds, len(bounds)-1)
}

func boundAt(bounds []float64, i int) float64 {
	if len(bounds) == 0 || i < 0 {
		return 0
	}
	if i >= len(bounds) {
		i = len(bounds) - 1
	}
	return bounds[i]
}

// FillAxis returns the bucket starts covering [from, to].
func FillAxis(from, to string, bucketSeconds int) []int64 {
	f, err1 := time.Parse(time.RFC3339Nano, from)
	t, err2 := time.Parse(time.RFC3339Nano, to)
	if err1 != nil || err2 != nil || bucketSeconds <= 0 || !t.After(f) {
		return nil
	}
	b := int64(bucketSeconds)
	start := (f.Unix() / b) * b
	end := t.Unix()
	var axis []int64
	for x := start; x <= end && len(axis) < 100000; x += b {
		axis = append(axis, x)
	}
	return axis
}
