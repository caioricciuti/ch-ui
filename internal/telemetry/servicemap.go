// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package telemetry

import "fmt"

// ServiceMapParams bounds the service map query.
type ServiceMapParams struct {
	From, To string
}

// serviceMapScanLimit is the span budget of one service map. Above it the
// map samples whole traces (see ServiceMapSampleRate) instead of cutting
// each side of the parent/child join on its own, which used to drop the
// edges whose parent and child landed on different sides of the cut.
const serviceMapScanLimit = 500000

// ServiceMapCountSQL counts the spans in range so the handler can pick a
// sample rate before running the map.
func ServiceMapCountSQL(src *Source, p ServiceMapParams) (string, error) {
	m := src.Traces
	where, err := traceRangeWhere(m, p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("SELECT count() AS c FROM %s.%s WHERE %s %s", Quote(src.Database), Quote(src.Table), where, QuerySettings), nil
}

// ServiceMapSampleRate returns N such that keeping the traces with
// cityHash64(TraceId) % N = 0 stays near the span budget. 1 means no
// sampling.
func ServiceMapSampleRate(spans int64) int64 {
	if spans <= serviceMapScanLimit {
		return 1
	}
	return (spans + serviceMapScanLimit - 1) / serviceMapScanLimit
}

// sampleCond keeps whole traces: the parent and child spans of a call
// share a TraceId, so both sides of the join keep or drop them together.
func sampleCond(m *TracesMapping, rate int64) string {
	if rate <= 1 {
		return ""
	}
	return fmt.Sprintf(" AND cityHash64(%s) %% %d = 0", Quote(m.TraceID), rate)
}

// scaled multiplies a sampled count back to an estimate of the whole.
func scaled(expr string, rate int64) string {
	if rate <= 1 {
		return expr
	}
	return fmt.Sprintf("%s * %d", expr, rate)
}

// durationToMs renders an expression converting the mapped Duration column
// into milliseconds according to the source's duration unit.
func durationToMs(m *TracesMapping) string {
	col := Quote(m.Duration)
	switch m.DurationUnit {
	case "ms":
		return fmt.Sprintf("toFloat64(%s)", col)
	case "us":
		return fmt.Sprintf("toFloat64(%s) / 1000", col)
	case "s":
		return fmt.Sprintf("toFloat64(%s) * 1000", col)
	default:
		return fmt.Sprintf("toFloat64(%s) / 1000000", col)
	}
}

func traceRangeWhere(m *TracesMapping, p ServiceMapParams) (string, error) {
	lo, err := TimeBound(m.Timestamp, ">=", p.From)
	if err != nil {
		return "", err
	}
	hi, err := TimeBound(m.Timestamp, "<=", p.To)
	if err != nil {
		return "", err
	}
	return lo + " AND " + hi, nil
}

func errorCond(m *TracesMapping) string {
	if m.StatusCode == "" {
		return "0"
	}
	return ErrorStatusCond(Quote(m.StatusCode))
}

// ServiceMapNodesSQL aggregates spans per service over the range. rate is
// the trace sample rate from ServiceMapSampleRate; counts are scaled back.
func ServiceMapNodesSQL(src *Source, p ServiceMapParams, rate int64) (string, error) {
	m := src.Traces
	where, err := traceRangeWhere(m, p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		`SELECT service, %s AS spans, %s AS errors,
       quantile(0.5)(duration_ms) AS p50_ms, quantile(0.95)(duration_ms) AS p95_ms
FROM (
  SELECT %s AS service, %s AS is_error, %s AS duration_ms
  FROM %s.%s
  WHERE %s%s
)
GROUP BY service
ORDER BY spans DESC
%s`,
		scaled("count()", rate), scaled("countIf(is_error)", rate),
		Quote(m.Service), errorCond(m), durationToMs(m),
		Quote(src.Database), Quote(src.Table), where, sampleCond(m, rate), QuerySettings,
	), nil
}

// ServiceMapEdgesSQL joins child spans to their parent spans across service
// boundaries. Both sides are range-filtered subqueries so ClickHouse prunes
// partitions before the join, and both apply the same trace sample.
func ServiceMapEdgesSQL(src *Source, p ServiceMapParams, rate int64) (string, error) {
	m := src.Traces
	where, err := traceRangeWhere(m, p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		`SELECT p.service AS from_service, c.service AS to_service,
       %[12]s AS calls, %[13]s AS errors,
       quantile(0.5)(c.duration_ms) AS p50_ms, quantile(0.95)(c.duration_ms) AS p95_ms
FROM (
  SELECT %[1]s AS trace_id, %[2]s AS parent_span_id, %[3]s AS service, %[4]s AS is_error, %[5]s AS duration_ms
  FROM %[6]s.%[7]s
  WHERE %[8]s AND %[2]s != ''%[9]s
) AS c
INNER JOIN (
  SELECT %[1]s AS trace_id, %[10]s AS span_id, %[3]s AS service
  FROM %[6]s.%[7]s
  WHERE %[8]s%[9]s
) AS p ON c.trace_id = p.trace_id AND c.parent_span_id = p.span_id
WHERE c.service != p.service
GROUP BY from_service, to_service
ORDER BY calls DESC
%[11]s`,
		Quote(m.TraceID), Quote(m.ParentSpanID), Quote(m.Service), errorCond(m), durationToMs(m),
		Quote(src.Database), Quote(src.Table), where, sampleCond(m, rate), Quote(m.SpanID), QuerySettings,
		scaled("count()", rate), scaled("countIf(c.is_error)", rate),
	), nil
}

// ShapeServiceMap turns ServiceMapNodesSQL and ServiceMapEdgesSQL rows into
// the service map contract: nodes per service, edges per caller and callee.
func ShapeServiceMap(nodeRows, edgeRows []map[string]interface{}) (nodes, edges []map[string]interface{}) {
	nodes = make([]map[string]interface{}, 0, len(nodeRows))
	for _, row := range nodeRows {
		nodes = append(nodes, map[string]interface{}{
			"service": Str(row["service"]),
			"spans":   Int64(row["spans"]),
			"errors":  Int64(row["errors"]),
			"p50_ms":  roundMs(row["p50_ms"]),
			"p95_ms":  roundMs(row["p95_ms"]),
		})
	}
	edges = make([]map[string]interface{}, 0, len(edgeRows))
	for _, row := range edgeRows {
		edges = append(edges, map[string]interface{}{
			"from":   Str(row["from_service"]),
			"to":     Str(row["to_service"]),
			"calls":  Int64(row["calls"]),
			"errors": Int64(row["errors"]),
			"p50_ms": roundMs(row["p50_ms"]),
			"p95_ms": roundMs(row["p95_ms"]),
		})
	}
	return nodes, edges
}

// roundMs reads a millisecond cell with one decimal; NaN and absent are 0.
func roundMs(v interface{}) float64 {
	f := Float(v)
	return float64(int64(f*10+0.5)) / 10
}
