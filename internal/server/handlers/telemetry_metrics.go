package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// ── Metrics (phase 3) ───────────────────────────────────────────────

// metricsSetup resolves credentials and a metrics source from the request.
func (h *TelemetryHandler) metricsSetup(w http.ResponseWriter, r *http.Request, sourceID string) (chCreds, *telemetry.Source, bool) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return chCreds{}, nil, false
	}
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, "source_id is required")
		return chCreds{}, nil, false
	}
	src := h.loadSource(w, sourceID, session.ConnectionID)
	if src == nil {
		return chCreds{}, nil, false
	}
	if src.Kind != telemetry.KindMetrics || src.Tables == nil {
		writeError(w, http.StatusBadRequest, "Source is not a metrics source")
		return chCreds{}, nil, false
	}
	return c, src, true
}

// MetricsCatalog lists the metrics present in the range across the tables.
func (h *TelemetryHandler) MetricsCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, src, ok := h.metricsSetup(w, r, q.Get("source_id"))
	if !ok {
		return
	}
	from, to := q.Get("from"), q.Get("to")
	if from == "" || to == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	sqlText, err := telemetry.CatalogSQL(src, from, to)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
	if err != nil {
		slog.Warn("Metrics catalog failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	metrics := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		metrics = append(metrics, map[string]interface{}{
			"name":        str(row["name"]),
			"type":        str(row["type"]),
			"unit":        str(row["unit"]),
			"description": str(row["description"]),
			"services":    toInt64(row["services"]),
			"series":      toInt64(row["series"]),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "metrics": metrics})
}

type metricsQueryRequest struct {
	SourceID      string   `json:"source_id"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Metric        string   `json:"metric"`
	Type          string   `json:"type"`
	Aggregation   string   `json:"aggregation"`
	GroupBy       []string `json:"group_by"`
	Q             string   `json:"q"`
	BucketSeconds int      `json:"bucket_seconds"`
	LimitSeries   int      `json:"limit_series"`
}

// metricSeries is one output series being assembled.
type metricSeries struct {
	labels map[string]string
	name   string
	values map[int64]float64 // bucket -> value (nil = gap)
	count  int64             // rows contributing (for ranking)
	// histogram quantile accumulators per bucket
	hist map[int64]*histAcc
	// rate accumulators
	rate []telemetry.RatePoint
}

type histAcc struct {
	counts []float64
	bounds []float64
	cnt    float64
}

// MetricsQuery returns bucketed series for one metric.
func (h *TelemetryHandler) MetricsQuery(w http.ResponseWriter, r *http.Request) {
	var body metricsQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	c, src, ok := h.metricsSetup(w, r, body.SourceID)
	if !ok {
		return
	}
	if body.From == "" || body.To == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	typ := telemetry.MetricType(strings.ToLower(strings.TrimSpace(body.Type)))
	agg := strings.ToLower(strings.TrimSpace(body.Aggregation))
	if agg == "" {
		agg = "avg"
	}
	bucket := telemetry.MetricBucketSeconds(body.From, body.To, body.BucketSeconds)
	limitSeries := body.LimitSeries
	if limitSeries <= 0 {
		limitSeries = 20
	}
	if limitSeries > 100 {
		limitSeries = 100
	}
	params := telemetry.MetricsParams{
		From: body.From, To: body.To, Q: body.Q, Metric: body.Metric, Type: typ,
		Aggregation: agg, GroupBy: body.GroupBy, BucketSeconds: bucket, LimitSeries: limitSeries,
	}
	sqlText, err := telemetry.MetricQuerySQL(src, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	rows, err := h.exec(c, sqlText, telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Metrics query failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	// Group rows into series keyed by their labels.
	seriesByKey := map[string]*metricSeries{}
	var order []string
	unit := ""
	quantile := 0.0
	switch agg {
	case "p50":
		quantile = 0.5
	case "p95":
		quantile = 0.95
	case "p99":
		quantile = 0.99
	}
	for _, row := range rows {
		labels := map[string]string{}
		parts := make([]string, 0, len(body.GroupBy))
		for i, k := range body.GroupBy {
			v := str(row[fmt.Sprintf("g%d", i)])
			labels[k] = v
			parts = append(parts, v)
		}
		key := strings.Join(parts, "\x00")
		s, ok := seriesByKey[key]
		if !ok {
			name := strings.Join(parts, " · ")
			if name == "" {
				name = body.Metric
			}
			s = &metricSeries{labels: labels, name: name, values: map[int64]float64{}, hist: map[int64]*histAcc{}}
			seriesByKey[key] = s
			order = append(order, key)
		}
		if unit == "" {
			unit = str(row["unit"])
		}
		t := toInt64(row["t"])
		s.count += toInt64(row["n"])
		switch {
		case agg == "rate":
			s.rate = append(s.rate, telemetry.RatePoint{T: t, SID: str(row["sid"]), Max: toFloat(row["v"])})
		case quantile > 0:
			acc := s.hist[t]
			if acc == nil {
				acc = &histAcc{}
				s.hist[t] = acc
			}
			acc.counts = addVectors(acc.counts, floatSlice(row["counts"]))
			if len(acc.bounds) == 0 {
				acc.bounds = floatSlice(row["bounds"])
			}
			acc.cnt += toFloat(row["cnt"])
		default:
			if v, ok := numeric(row["v"]); ok {
				s.values[t] = v
			}
		}
	}

	// Finish rate and quantile series.
	for _, s := range seriesByKey {
		if agg == "rate" {
			for t, v := range telemetry.ComputeRate(s.rate, bucket) {
				s.values[t] = v
			}
		}
		if quantile > 0 {
			for t, acc := range s.hist {
				if v := telemetry.HistogramQuantile(acc.bounds, acc.counts, quantile); !math.IsNaN(v) {
					s.values[t] = v
				}
			}
		}
	}

	// Rank by contributing rows, cap, keep a stable order.
	sort.SliceStable(order, func(i, j int) bool { return seriesByKey[order[i]].count > seriesByKey[order[j]].count })
	truncated := false
	if len(order) > limitSeries {
		order = order[:limitSeries]
		truncated = true
	}

	axis := telemetry.FillAxis(body.From, body.To, bucket)
	out := make([]map[string]interface{}, 0, len(order))
	for _, key := range order {
		s := seriesByKey[key]
		points := make([][2]interface{}, 0, len(axis))
		var last, min, max, sum interface{}
		n := 0.0
		minV, maxV, sumV := math.Inf(1), math.Inf(-1), 0.0
		for _, t := range axis {
			if v, ok := s.values[t]; ok {
				points = append(points, [2]interface{}{t, v})
				last = v
				if v < minV {
					minV = v
				}
				if v > maxV {
					maxV = v
				}
				sumV += v
				n++
			} else {
				points = append(points, [2]interface{}{t, nil})
			}
		}
		if n > 0 {
			min, max, sum = minV, maxV, sumV/n
		}
		out = append(out, map[string]interface{}{
			"labels": s.labels,
			"name":   s.name,
			"points": points,
			"last":   last,
			"min":    min,
			"max":    max,
			"avg":    sum,
		})
	}
	resp := map[string]interface{}{
		"success":        true,
		"bucket_seconds": bucket,
		"unit":           unit,
		"series":         out,
		"took_ms":        time.Since(started).Milliseconds(),
	}
	if truncated {
		resp["truncated"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// MetricsAttributes lists attribute keys and top values for a metric.
func (h *TelemetryHandler) MetricsAttributes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, src, ok := h.metricsSetup(w, r, q.Get("source_id"))
	if !ok {
		return
	}
	from, to, metric := q.Get("from"), q.Get("to"), q.Get("metric")
	typ := telemetry.MetricType(strings.ToLower(q.Get("type")))
	if from == "" || to == "" || metric == "" {
		writeError(w, http.StatusBadRequest, "from, to and metric are required")
		return
	}
	sqlText, err := telemetry.AttributesSQL(src, from, to, metric, typ)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
	if err != nil {
		slog.Warn("Metrics attributes failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	type valueCount struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	values := map[string][]valueCount{}
	totals := map[string]int64{}
	var keyOrder []string
	for _, row := range rows {
		k, v, n := str(row["k"]), str(row["v"]), toInt64(row["c"])
		if k == "" {
			continue
		}
		if _, seen := totals[k]; !seen {
			keyOrder = append(keyOrder, k)
		}
		totals[k] += n
		if len(values[k]) < 10 {
			values[k] = append(values[k], valueCount{Value: v, Count: n})
		}
	}
	sort.SliceStable(keyOrder, func(i, j int) bool { return totals[keyOrder[i]] > totals[keyOrder[j]] })
	if len(keyOrder) > 20 {
		keyOrder = keyOrder[:20]
	}
	keys := make([]map[string]interface{}, 0, len(keyOrder))
	for _, k := range keyOrder {
		keys = append(keys, map[string]interface{}{"key": k, "values": values[k]})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "keys": keys})
}

// ── Helpers ─────────────────────────────────────────────────────────

// numeric reads a gateway cell as float64; false for nil/NaN/non-numbers.
func numeric(v interface{}) (float64, bool) {
	switch t := v.(type) {
	case nil:
		return 0, false
	case float64:
		if math.IsNaN(t) || math.IsInf(t, 0) {
			return 0, false
		}
		return t, true
	case json.Number:
		f, err := t.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(t, 64)
		if err != nil || math.IsNaN(f) || math.IsInf(f, 0) {
			return 0, false
		}
		return f, true
	case int64:
		return float64(t), true
	case int:
		return float64(t), true
	}
	return 0, false
}

func toFloat(v interface{}) float64 {
	f, _ := numeric(v)
	return f
}

// floatSlice reads an Array column (numbers or numeric strings).
func floatSlice(v interface{}) []float64 {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]float64, len(arr))
	for i, x := range arr {
		out[i] = toFloat(x)
	}
	return out
}

func addVectors(a, b []float64) []float64 {
	if len(b) > len(a) {
		grown := make([]float64, len(b))
		copy(grown, a)
		a = grown
	}
	for i := range b {
		a[i] += b[i]
	}
	return a
}
