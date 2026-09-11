package telemetry

import (
	"strings"
	"testing"
)

func tracesSource() *Source {
	m := DefaultTracesMapping()
	return &Source{Kind: KindTraces, Database: "default", Table: "otel_traces", Traces: &m}
}

func TestServiceMapSQL(t *testing.T) {
	p := ServiceMapParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T01:00:00Z"}

	nodes, err := ServiceMapNodesSQL(tracesSource(), p)
	if err != nil {
		t.Fatalf("nodes: %v", err)
	}
	for _, want := range []string{
		"`ServiceName` AS service", "`StatusCode` = 'Error' AS is_error", "toFloat64(`Duration`) / 1000000 AS duration_ms",
		"FROM `default`.`otel_traces`", "parseDateTime64BestEffort('2026-09-11T00:00:00Z')", "LIMIT 500000", "GROUP BY service", QuerySettings,
	} {
		if !strings.Contains(nodes, want) {
			t.Errorf("nodes SQL missing %q:\n%s", want, nodes)
		}
	}

	edges, err := ServiceMapEdgesSQL(tracesSource(), p)
	if err != nil {
		t.Fatalf("edges: %v", err)
	}
	for _, want := range []string{
		") AS c", "INNER JOIN (", ") AS p ON c.trace_id = p.trace_id AND c.parent_span_id = p.span_id",
		"WHERE c.service != p.service", "`ParentSpanId` != ''", "GROUP BY from_service, to_service",
	} {
		if !strings.Contains(edges, want) {
			t.Errorf("edges SQL missing %q:\n%s", want, edges)
		}
	}
	if strings.Count(edges, "parseDateTime64BestEffort('2026-09-11T00:00:00Z')") != 2 {
		t.Errorf("both join sides must be range-filtered:\n%s", edges)
	}

	// Duration unit drives the ms conversion.
	src := tracesSource()
	src.Traces.DurationUnit = "ms"
	sql, _ := ServiceMapNodesSQL(src, p)
	if !strings.Contains(sql, "toFloat64(`Duration`) AS duration_ms") {
		t.Errorf("ms unit should not divide:\n%s", sql)
	}

	// Bad bounds are rejected before any SQL is produced.
	if _, err := ServiceMapNodesSQL(tracesSource(), ServiceMapParams{From: "yesterday", To: p.To}); err == nil {
		t.Errorf("expected an error for a non-RFC3339 bound")
	}

	// A source without a status column still compiles.
	src = tracesSource()
	src.Traces.StatusCode = ""
	sql, _ = ServiceMapNodesSQL(src, p)
	if !strings.Contains(sql, "0 AS is_error") {
		t.Errorf("missing status column should render a constant:\n%s", sql)
	}
}
