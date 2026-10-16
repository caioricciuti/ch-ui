// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package telemetry

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// ShapeCatalogRows turns CatalogSQL rows into the metric catalog contract.
func ShapeCatalogRows(rows []map[string]interface{}) []map[string]interface{} {
	metrics := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		metrics = append(metrics, map[string]interface{}{
			"name":        Str(row["name"]),
			"type":        Str(row["type"]),
			"unit":        Str(row["unit"]),
			"description": Str(row["description"]),
			"services":    Int64(row["services"]),
			"series":      Int64(row["series"]),
		})
	}
	return metrics
}

// MetricsResult is a MetricQuerySQL answer assembled into series.
type MetricsResult struct {
	BucketSeconds int
	Unit          string
	// Series are ranked by contributing points and capped at LimitSeries;
	// each has labels, name, points ([unix seconds, value or nil]) on the
	// full axis of the range, and last, min, max, avg.
	Series []map[string]interface{}
	// Truncated: more series than LimitSeries existed.
	Truncated bool
	// RowsTruncated: max_result_rows cut the rows, so values may be partial.
	RowsTruncated bool
}

// metricSeries is one output series being assembled.
type metricSeries struct {
	labels map[string]string
	name   string
	values map[int64]float64 // bucket -> value (absent = gap)
	count  int64             // rows contributing (for ranking)
	// histogram quantile points per raw series
	hist []HistPoint
	// rate accumulators
	rate []RatePoint
}

// BuildSeries groups MetricQuerySQL rows into series for p. p must be the
// params the SQL was built with, Aggregation and BucketSeconds resolved.
// Rate on a metric that is not a cumulative monotonic counter is an error.
func BuildSeries(rows []map[string]interface{}, p MetricsParams) (MetricsResult, error) {
	agg := p.Aggregation
	if agg == "rate" {
		for _, row := range rows {
			if Int64(row["cum"]) == 0 {
				return MetricsResult{}, fmt.Errorf("rate needs a cumulative monotonic counter; %s has delta or non-monotonic points. Use sum (delta) or avg / last (non-monotonic) instead.", p.Metric)
			}
		}
	}
	// max_result_rows cuts the answer silently; rate and quantiles read one
	// row per raw series and bucket, so a busy metric can hit it.
	res := MetricsResult{BucketSeconds: p.BucketSeconds, RowsTruncated: ResultCapped(len(rows))}

	// Group rows into series keyed by their labels.
	seriesByKey := map[string]*metricSeries{}
	var order []string
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
		parts := make([]string, 0, len(p.GroupBy))
		for i, k := range p.GroupBy {
			v := Str(row[fmt.Sprintf("g%d", i)])
			labels[k] = v
			parts = append(parts, v)
		}
		key := strings.Join(parts, "\x00")
		s, ok := seriesByKey[key]
		if !ok {
			name := strings.Join(parts, " · ")
			if name == "" {
				name = p.Metric
			}
			s = &metricSeries{labels: labels, name: name, values: map[int64]float64{}}
			seriesByKey[key] = s
			order = append(order, key)
		}
		if res.Unit == "" {
			res.Unit = Str(row["unit"])
		}
		t := Int64(row["t"])
		s.count += Int64(row["n"])
		switch {
		case agg == "rate":
			s.rate = append(s.rate, RatePoint{T: t, SID: Str(row["sid"]), Max: Float(row["v"])})
		case quantile > 0:
			s.hist = append(s.hist, HistPoint{
				T: t, SID: Str(row["sid"]), Cumulative: Int64(row["cum"]) == 1,
				Sum: floatSlice(row["counts_sum"]), First: floatSlice(row["counts_first"]), Last: floatSlice(row["counts_last"]),
				Bounds: floatSlice(row["bounds"]),
			})
		default:
			if v, ok := Numeric(row["v"]); ok {
				s.values[t] = v
			}
		}
	}

	// Finish rate and quantile series.
	for _, s := range seriesByKey {
		if agg == "rate" {
			for t, v := range ComputeRate(s.rate, p.BucketSeconds) {
				s.values[t] = v
			}
		}
		if quantile > 0 {
			counts, bounds := HistogramIncrease(s.hist)
			for t, c := range counts {
				if v := HistogramQuantile(bounds[t], c, quantile); !math.IsNaN(v) {
					s.values[t] = v
				}
			}
		}
	}

	// Rank by contributing rows, cap, keep a stable order.
	sort.SliceStable(order, func(i, j int) bool { return seriesByKey[order[i]].count > seriesByKey[order[j]].count })
	if len(order) > p.LimitSeries {
		order = order[:p.LimitSeries]
		res.Truncated = true
	}

	axis := FillAxis(p.From, p.To, p.BucketSeconds)
	res.Series = make([]map[string]interface{}, 0, len(order))
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
		res.Series = append(res.Series, map[string]interface{}{
			"labels": s.labels,
			"name":   s.name,
			"points": points,
			"last":   last,
			"min":    min,
			"max":    max,
			"avg":    sum,
		})
	}
	return res, nil
}

// floatSlice reads an Array column (numbers or numeric strings).
func floatSlice(v interface{}) []float64 {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]float64, len(arr))
	for i, x := range arr {
		out[i] = Float(x)
	}
	return out
}
