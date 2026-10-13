// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

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

	nodes, err := ServiceMapNodesSQL(tracesSource(), p, 1)
	if err != nil {
		t.Fatalf("nodes: %v", err)
	}
	for _, want := range []string{
		"`ServiceName` AS service", "lower(`StatusCode`) IN ('error', 'status_code_error') AS is_error", "toFloat64(`Duration`) / 1000000 AS duration_ms",
		"FROM `default`.`otel_traces`", "parseDateTime64BestEffort('2026-09-11T00:00:00Z')", "count() AS spans", "GROUP BY service", QuerySettings,
	} {
		if !strings.Contains(nodes, want) {
			t.Errorf("nodes SQL missing %q:\n%s", want, nodes)
		}
	}

	edges, err := ServiceMapEdgesSQL(tracesSource(), p, 1)
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
	sql, _ := ServiceMapNodesSQL(src, p, 1)
	if !strings.Contains(sql, "toFloat64(`Duration`) AS duration_ms") {
		t.Errorf("ms unit should not divide:\n%s", sql)
	}

	// Bad bounds are rejected before any SQL is produced.
	if _, err := ServiceMapNodesSQL(tracesSource(), ServiceMapParams{From: "yesterday", To: p.To}, 1); err == nil {
		t.Errorf("expected an error for a non-RFC3339 bound")
	}

	// A source without a status column still compiles.
	src = tracesSource()
	src.Traces.StatusCode = ""
	sql, _ = ServiceMapNodesSQL(src, p, 1)
	if !strings.Contains(sql, "0 AS is_error") {
		t.Errorf("missing status column should render a constant:\n%s", sql)
	}
}

func TestServiceMapSamplesBothSidesTogether(t *testing.T) {
	if ServiceMapSampleRate(500000) != 1 || ServiceMapSampleRate(10) != 1 {
		t.Fatalf("no sampling at or under the budget")
	}
	if got := ServiceMapSampleRate(1_200_000); got != 3 {
		t.Fatalf("rate for 1.2M spans: %d", got)
	}
	p := ServiceMapParams{From: "2026-09-11T00:00:00Z", To: "2026-09-11T01:00:00Z"}
	edges, err := ServiceMapEdgesSQL(tracesSource(), p, 3)
	if err != nil {
		t.Fatal(err)
	}
	// Both join sides keep the same traces, and neither is cut on its own.
	if strings.Count(edges, "cityHash64(`TraceId`) % 3 = 0") != 2 {
		t.Errorf("both sides must apply the same trace sample:\n%s", edges)
	}
	if strings.Contains(edges, "LIMIT") {
		t.Errorf("a per-side LIMIT drops edges:\n%s", edges)
	}
	if !strings.Contains(edges, "count() * 3 AS calls") {
		t.Errorf("sampled counts must be scaled back:\n%s", edges)
	}
	nodes, _ := ServiceMapNodesSQL(tracesSource(), p, 3)
	if !strings.Contains(nodes, "cityHash64(`TraceId`) % 3 = 0") || !strings.Contains(nodes, "count() * 3 AS spans") {
		t.Errorf("nodes must use the same sample:\n%s", nodes)
	}
	plain, _ := ServiceMapEdgesSQL(tracesSource(), p, 1)
	if strings.Contains(plain, "cityHash64") {
		t.Errorf("rate 1 must not sample:\n%s", plain)
	}
}
