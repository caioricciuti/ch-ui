package telemetry

import "fmt"

// ServiceMapParams bounds the service map query.
type ServiceMapParams struct {
	From, To string
}

// serviceMapScanLimit caps each range-filtered span scan feeding the map.
const serviceMapScanLimit = 500000

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
	return fmt.Sprintf("%s = 'Error'", Quote(m.StatusCode))
}

// ServiceMapNodesSQL aggregates spans per service over the range.
func ServiceMapNodesSQL(src *Source, p ServiceMapParams) (string, error) {
	m := src.Traces
	where, err := traceRangeWhere(m, p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		`SELECT service, count() AS spans, countIf(is_error) AS errors,
       quantile(0.5)(duration_ms) AS p50_ms, quantile(0.95)(duration_ms) AS p95_ms
FROM (
  SELECT %s AS service, %s AS is_error, %s AS duration_ms
  FROM %s.%s
  WHERE %s
  LIMIT %d
)
GROUP BY service
ORDER BY spans DESC
%s`,
		Quote(m.Service), errorCond(m), durationToMs(m),
		Quote(src.Database), Quote(src.Table), where, serviceMapScanLimit, QuerySettings,
	), nil
}

// ServiceMapEdgesSQL joins child spans to their parent spans across service
// boundaries. Both sides are range-filtered subqueries so ClickHouse prunes
// partitions before the join.
func ServiceMapEdgesSQL(src *Source, p ServiceMapParams) (string, error) {
	m := src.Traces
	where, err := traceRangeWhere(m, p)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		`SELECT p.service AS from_service, c.service AS to_service,
       count() AS calls, countIf(c.is_error) AS errors,
       quantile(0.5)(c.duration_ms) AS p50_ms, quantile(0.95)(c.duration_ms) AS p95_ms
FROM (
  SELECT %[1]s AS trace_id, %[2]s AS parent_span_id, %[3]s AS service, %[4]s AS is_error, %[5]s AS duration_ms
  FROM %[6]s.%[7]s
  WHERE %[8]s AND %[2]s != ''
  LIMIT %[9]d
) AS c
INNER JOIN (
  SELECT %[1]s AS trace_id, %[10]s AS span_id, %[3]s AS service
  FROM %[6]s.%[7]s
  WHERE %[8]s
  LIMIT %[9]d
) AS p ON c.trace_id = p.trace_id AND c.parent_span_id = p.span_id
WHERE c.service != p.service
GROUP BY from_service, to_service
ORDER BY calls DESC
%[11]s`,
		Quote(m.TraceID), Quote(m.ParentSpanID), Quote(m.Service), errorCond(m), durationToMs(m),
		Quote(src.Database), Quote(src.Table), where, serviceMapScanLimit, Quote(m.SpanID), QuerySettings,
	), nil
}
