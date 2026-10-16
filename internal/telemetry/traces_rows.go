// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package telemetry

import "time"

// TraceIndexMargin pads the window read from the exporter's trace-id
// index, which stores whole seconds, and the window a trace's log lines
// are looked up in.
const TraceIndexMargin = time.Minute

// ShapeTraceRows turns TraceSearchSQL rows into the trace list contract.
func ShapeTraceRows(rows []map[string]interface{}) []map[string]interface{} {
	traces := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		startNs := Int64(row["start_ns"])
		endNs := Int64(row["end_ns"])
		services := stringSlice(row["services"])
		if services == nil {
			services = []string{}
		}
		traces = append(traces, map[string]interface{}{
			"trace_id":     Str(row["trace_id"]),
			"root_span":    Str(row["root_span"]),
			"root_service": Str(row["root_service"]),
			"start":        Str(row["start"]),
			"start_ns":     Str(row["start_ns"]),
			"duration_ms":  float64(endNs-startNs) / 1e6,
			"span_count":   Int64(row["span_count"]),
			"error_count":  Int64(row["error_count"]),
			"services":     services,
			"status":       Str(row["status"]),
		})
	}
	return traces
}

// TraceCursor is the keyset position of one TraceSearchSQL row.
func TraceCursor(row map[string]interface{}) string {
	return EncodeCursor(Cursor{TimestampNs: Str(row["start_ns"]), TraceID: Str(row["trace_id"])})
}

// TraceBoundsWindow reads a TraceBoundsSQL result into the window a trace's
// spans sit in, padded by TraceIndexMargin; nil when the index has no entry.
func TraceBoundsWindow(rows []map[string]interface{}) *TimeWindow {
	if len(rows) == 0 || Int64(rows[0]["n"]) <= 0 {
		return nil
	}
	start := time.Unix(Int64(rows[0]["s"]), 0)
	end := time.Unix(Int64(rows[0]["e"]), 0)
	return &TimeWindow{FromNs: start.Add(-TraceIndexMargin).UnixNano(), ToNs: end.Add(TraceIndexMargin).UnixNano()}
}

// SpansFromRows reads TraceSpansSQL rows into spans, in row order. Pass the
// result to BuildSpanTree for the display order.
func SpansFromRows(rows []map[string]interface{}) []Span {
	spans := make([]Span, 0, len(rows))
	for _, row := range rows {
		spans = append(spans, Span{
			SpanID:        Str(row["span_id"]),
			ParentSpanID:  Str(row["parent_span_id"]),
			Name:          Str(row["name"]),
			Kind:          Str(row["kind"]),
			Service:       Str(row["service"]),
			Start:         Str(row["start"]),
			StartNs:       Str(row["start_ns"]),
			DurationMs:    Float(row["duration_ms"]),
			Status:        Str(row["status"]),
			StatusMessage: Str(row["status_message"]),
			Attributes:    StringMap(row["attributes"]),
			Resource:      StringMap(row["resource"]),
			Events:        ZipEvents(stringSlice(row["ev_time"]), stringSlice(row["ev_name"]), mapSlice(row["ev_attrs"])),
			Links:         ZipLinks(stringSlice(row["lk_trace"]), stringSlice(row["lk_span"]), mapSlice(row["lk_attrs"])),
		})
	}
	return spans
}

// TraceService is one service's share of a trace.
type TraceService struct {
	Name       string  `json:"name"`
	Spans      int     `json:"spans"`
	Errors     int     `json:"errors"`
	DurationMs float64 `json:"duration_ms"`
}

// TraceSummary is the header of a trace: earliest start to latest end and
// its services in first-seen order.
type TraceSummary struct {
	TraceID    string         `json:"trace_id"`
	Start      string         `json:"start"`
	DurationMs float64        `json:"duration_ms"`
	SpanCount  int            `json:"span_count"`
	ErrorCount int            `json:"error_count"`
	Services   []TraceService `json:"services"`

	StartNs, EndNs int64 `json:"-"`
}

// SummarizeTrace aggregates the spans of one trace.
func SummarizeTrace(traceID string, spans []Span) TraceSummary {
	sum := TraceSummary{TraceID: traceID, SpanCount: len(spans), Services: []TraceService{}}
	at := map[string]int{}
	for _, s := range spans {
		st := Int64(s.StartNs)
		en := st + int64(s.DurationMs*1e6)
		if sum.StartNs == 0 || st < sum.StartNs {
			sum.StartNs = st
		}
		if en > sum.EndNs {
			sum.EndNs = en
		}
		i, ok := at[s.Service]
		if !ok {
			i = len(sum.Services)
			at[s.Service] = i
			sum.Services = append(sum.Services, TraceService{Name: s.Service})
		}
		sum.Services[i].Spans++
		sum.Services[i].DurationMs += s.DurationMs
		if IsErrorStatus(s.Status) {
			sum.Services[i].Errors++
			sum.ErrorCount++
		}
	}
	if len(spans) > 0 {
		sum.Start = spans[0].Start
		for _, s := range spans {
			if Int64(s.StartNs) == sum.StartNs {
				sum.Start = s.Start
				break
			}
		}
	}
	sum.DurationMs = float64(sum.EndNs-sum.StartNs) / 1e6
	return sum
}

// LogsWindow is where the trace's log lines sit: its real extent, give or
// take clock skew between services. nil when the spans gave no extent.
func (s TraceSummary) LogsWindow() *TimeWindow {
	if s.StartNs <= 0 || s.EndNs < s.StartNs {
		return nil
	}
	return &TimeWindow{FromNs: s.StartNs - int64(TraceIndexMargin), ToNs: s.EndNs + int64(TraceIndexMargin)}
}

// CorrelatedLogsSource picks the logs source for a traces source among the
// sources of its connection: the configured correlation, else the first
// enabled logs source.
func CorrelatedLogsSource(src *Source, sources []Source) *Source {
	if src.CorrelatedLogs != "" {
		for i := range sources {
			s := &sources[i]
			if s.ID == src.CorrelatedLogs && s.ConnectionID == src.ConnectionID && s.Kind == KindLogs && s.Logs != nil {
				return s
			}
		}
	}
	for i := range sources {
		s := &sources[i]
		if s.ConnectionID == src.ConnectionID && s.Kind == KindLogs && s.Enabled && s.Logs != nil {
			return s
		}
	}
	return nil
}

func stringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		out = append(out, Str(x))
	}
	return out
}

func mapSlice(v interface{}) []map[string]string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]map[string]string, 0, len(arr))
	for _, x := range arr {
		out = append(out, StringMap(x))
	}
	return out
}
