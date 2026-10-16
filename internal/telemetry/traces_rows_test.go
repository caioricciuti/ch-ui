// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package telemetry

import (
	"encoding/json"
	"testing"
	"time"
)

func decodeRowsJSON(t *testing.T, raw string) []map[string]interface{} {
	t.Helper()
	var rows []map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func TestShapeTraceRowsAndCursor(t *testing.T) {
	rows := decodeRowsJSON(t, `[{"trace_id":"t1","root_span":"GET /","root_service":"web","start":"2026-10-16T08:00:00Z",
		"start_ns":"1760601600000000000","end_ns":"1760601600250000000","span_count":"4","error_count":1,"services":["web","db"],"status":"Error"}]`)
	out := ShapeTraceRows(rows)
	if len(out) != 1 {
		t.Fatalf("rows: %d", len(out))
	}
	r := out[0]
	if r["duration_ms"] != 250.0 || r["span_count"] != int64(4) || r["error_count"] != int64(1) || r["start_ns"] != "1760601600000000000" {
		t.Errorf("shape: %v", r)
	}
	if s := r["services"].([]string); len(s) != 2 || s[1] != "db" {
		t.Errorf("services: %v", r["services"])
	}
	cur, err := DecodeCursor(TraceCursor(rows[0]))
	if err != nil || cur.TimestampNs != "1760601600000000000" || cur.TraceID != "t1" {
		t.Errorf("cursor: %+v %v", cur, err)
	}
	if empty := ShapeTraceRows(decodeRowsJSON(t, `[{"trace_id":"t2"}]`)); len(empty[0]["services"].([]string)) != 0 {
		t.Error("missing services must be an empty list")
	}
}

func TestTraceBoundsWindow(t *testing.T) {
	if TraceBoundsWindow(nil) != nil || TraceBoundsWindow(decodeRowsJSON(t, `[{"n":"0","s":"0","e":"0"}]`)) != nil {
		t.Error("no index entry: nil window")
	}
	w := TraceBoundsWindow(decodeRowsJSON(t, `[{"n":"3","s":"1760601600","e":"1760601610"}]`))
	if w == nil || w.FromNs != time.Unix(1760601600, 0).Add(-TraceIndexMargin).UnixNano() || w.ToNs != time.Unix(1760601610, 0).Add(TraceIndexMargin).UnixNano() {
		t.Errorf("window: %+v", w)
	}
}

func TestSpansFromRowsAndSummary(t *testing.T) {
	rows := decodeRowsJSON(t, `[
	 {"span_id":"b","parent_span_id":"a","name":"query","kind":"Client","service":"db","start":"T1","start_ns":"1000000","duration_ms":2,"status":"Error",
	  "attributes":{"db.system":"clickhouse"},"resource":"{\"host\":\"h\"}","ev_time":["T1"],"ev_name":["retry"],"ev_attrs":[{"n":"1"}],"lk_trace":[],"lk_span":[],"lk_attrs":[]},
	 {"span_id":"a","parent_span_id":"","name":"GET /","service":"web","start":"T0","start_ns":"0500000","duration_ms":"5.5","status":"Ok"}
	]`)
	spans := BuildSpanTree(SpansFromRows(rows))
	if len(spans) != 2 || spans[0].SpanID != "a" || spans[1].Depth != 1 {
		t.Fatalf("tree: %+v", spans)
	}
	db := spans[1]
	if db.Attributes["db.system"] != "clickhouse" || db.Resource["host"] != "h" || len(db.Events) != 1 || db.Events[0].Attributes["n"] != "1" || len(db.Links) != 0 {
		t.Errorf("span fields: %+v", db)
	}
	sum := SummarizeTrace("t1", spans)
	if sum.TraceID != "t1" || sum.SpanCount != 2 || sum.ErrorCount != 1 || sum.Start != "T0" {
		t.Errorf("summary: %+v", sum)
	}
	// 0.5 ms start to max(0.5+5.5, 1+2) ms end.
	if sum.StartNs != 500000 || sum.EndNs != 6000000 || sum.DurationMs != 5.5 {
		t.Errorf("extent: %+v", sum)
	}
	if len(sum.Services) != 2 || sum.Services[0].Name != "web" || sum.Services[1].Errors != 1 {
		t.Errorf("services: %+v", sum.Services)
	}
	w := sum.LogsWindow()
	if w == nil || w.FromNs != 500000-int64(TraceIndexMargin) || w.ToNs != 6000000+int64(TraceIndexMargin) {
		t.Errorf("logs window: %+v", w)
	}
	if (TraceSummary{}).LogsWindow() != nil {
		t.Error("empty summary: nil logs window")
	}
}

func TestCorrelatedLogsSource(t *testing.T) {
	lm := DefaultLogsMapping()
	traces := &Source{ID: "tr", ConnectionID: "c1", Kind: KindTraces, CorrelatedLogs: "l2"}
	sources := []Source{
		{ID: "l1", ConnectionID: "c1", Kind: KindLogs, Logs: &lm, Enabled: true},
		{ID: "l2", ConnectionID: "c1", Kind: KindLogs, Logs: &lm, Enabled: false},
		{ID: "l3", ConnectionID: "c2", Kind: KindLogs, Logs: &lm, Enabled: true},
	}
	if got := CorrelatedLogsSource(traces, sources); got == nil || got.ID != "l2" {
		t.Errorf("configured correlation wins, even disabled: %+v", got)
	}
	traces.CorrelatedLogs = "l3" // another connection: ignored
	if got := CorrelatedLogsSource(traces, sources); got == nil || got.ID != "l1" {
		t.Errorf("fallback to the first enabled logs source: %+v", got)
	}
	if got := CorrelatedLogsSource(&Source{ConnectionID: "c9"}, sources); got != nil {
		t.Errorf("no logs source on the connection: %+v", got)
	}
}

func TestShapeServiceMap(t *testing.T) {
	nodes, edges := ShapeServiceMap(
		decodeRowsJSON(t, `[{"service":"web","spans":"10","errors":1,"p50_ms":1.26,"p95_ms":"nan"}]`),
		decodeRowsJSON(t, `[{"from_service":"web","to_service":"db","calls":4,"errors":"0","p50_ms":"2.04","p95_ms":null}]`),
	)
	if len(nodes) != 1 || nodes[0]["spans"] != int64(10) || nodes[0]["p50_ms"] != 1.3 || nodes[0]["p95_ms"] != 0.0 {
		t.Errorf("nodes: %v", nodes)
	}
	if len(edges) != 1 || edges[0]["from"] != "web" || edges[0]["to"] != "db" || edges[0]["calls"] != int64(4) || edges[0]["p50_ms"] != 2.0 {
		t.Errorf("edges: %v", edges)
	}
}
