// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// Pro observe tools mirror the Pro Telemetry pages: traces, the service map,
// metrics and monitors. Registered only when a Pro license is active, the
// same as the RequirePro routes behind those pages.

const (
	defaultTraceSpans = 200
	maxTraceLogs      = 50

	defaultMetricSeries = 10
	maxMetricSeries     = 100
)

type searchTracesArgs struct {
	SourceID string `json:"source_id,omitempty" jsonschema:"traces source id from telemetry_sources; default the first enabled traces source"`
	rangeArgs
	Query         string  `json:"query,omitempty" jsonschema:"search over spans: words match the span name; field:value matches service, span_name, status, kind, trace_id or any attribute key; duration:>500 compares milliseconds; AND, OR, NOT"`
	Service       string  `json:"service,omitempty" jsonschema:"keep traces with a span of this service"`
	Operation     string  `json:"operation,omitempty" jsonschema:"keep traces with a span of this name"`
	MinDurationMs float64 `json:"min_duration_ms,omitempty" jsonschema:"keep traces with a span at least this long, in milliseconds"`
	ErrorsOnly    bool    `json:"errors_only,omitempty" jsonschema:"keep traces with an error span"`
	Limit         int     `json:"limit,omitempty" jsonschema:"traces to return (default 50, max 500)"`
	Cursor        string  `json:"cursor,omitempty" jsonschema:"next_cursor from a previous call, to page on with the same filters"`
}

type getTraceArgs struct {
	SourceID    string `json:"source_id,omitempty" jsonschema:"traces source id; default the first enabled traces source"`
	TraceID     string `json:"trace_id" jsonschema:"the trace id"`
	Around      string `json:"around,omitempty" jsonschema:"RFC 3339 time near the trace (e.g. its start from search_traces); bounds the lookup to an hour either side when the table has no trace-id index"`
	MaxSpans    int    `json:"max_spans,omitempty" jsonschema:"spans to return in tree order (default 200, max 500)"`
	IncludeLogs *bool  `json:"include_logs,omitempty" jsonschema:"include up to 50 correlated log lines (default true)"`
}

type serviceMapArgs struct {
	SourceID string `json:"source_id,omitempty" jsonschema:"traces source id; default the first enabled traces source"`
	rangeArgs
}

type listMetricsArgs struct {
	SourceID string `json:"source_id,omitempty" jsonschema:"metrics source id; default the first enabled metrics source"`
	rangeArgs
	Name  string `json:"name,omitempty" jsonschema:"case-insensitive substring of the metric name"`
	Limit int    `json:"limit,omitempty" jsonschema:"metrics to return (default 50, max 500)"`
}

type queryMetricsArgs struct {
	SourceID string `json:"source_id,omitempty" jsonschema:"metrics source id; default the first enabled metrics source"`
	rangeArgs
	Metric        string   `json:"metric" jsonschema:"metric name, from list_metrics"`
	Type          string   `json:"type" jsonschema:"metric type from list_metrics: gauge, sum, histogram, exponential_histogram or summary"`
	Aggregation   string   `json:"aggregation,omitempty" jsonschema:"avg (default), sum, min, max, last, count; rate for cumulative sums; p50, p95, p99 for histograms"`
	GroupBy       []string `json:"group_by,omitempty" jsonschema:"up to 2 attribute keys to split series by, e.g. [\"service.name\"]"`
	Query         string   `json:"query,omitempty" jsonschema:"filter as field:value terms, e.g. service:api http.method:GET"`
	BucketSeconds int      `json:"bucket_seconds,omitempty" jsonschema:"bucket width in seconds; default fits the range in about 360 points"`
	LimitSeries   int      `json:"limit_series,omitempty" jsonschema:"series to return, busiest first (default 10, max 100)"`
}

func registerObserveProTools(srv *mcp.Server, deps Deps, ak *authedKey) {
	title, ann := readOnlyTool("Search traces")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "search_traces",
		Title:       title,
		Annotations: ann,
		Description: "List traces of a traces source in a time range (default the last hour), newest first: root span and service, duration, span and error counts, services. Filter by service, operation, minimum duration, errors or a search; then get_trace for the spans.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args searchTracesArgs) (*mcp.CallToolResult, any, error) {
		src, errRes := observeSource(deps, ak, args.SourceID, telemetry.KindTraces)
		if errRes != nil {
			return errRes, nil, nil
		}
		if src.Traces == nil {
			return errResult("source %q has no traces mapping", src.ID), nil, nil
		}
		from, to, err := args.resolve(time.Now())
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		q, err := traceSearchQuery(args)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		var cursor *telemetry.Cursor
		if args.Cursor != "" {
			cur, err := telemetry.DecodeCursor(args.Cursor)
			if err != nil {
				return errResult("invalid cursor; pass the next_cursor value from the previous call"), nil, nil
			}
			cursor = &cur
		}
		cols, err := sourceColumns(ctx, deps, ak, src.Database, src.Table)
		if err != nil {
			return errResult("search_traces failed: %v", err), nil, nil
		}
		src.Traces.ResolveNested(cols)
		limit := clampLimit(args.Limit, defaultObserveLimit, maxObserveLimit)
		sql, err := telemetry.TraceSearchSQL(src, cols, telemetry.TracesParams{From: from, To: to, Q: q}, limit+1, cursor)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		rows, err := runTelemetry(ctx, deps, ak, sql)
		if err != nil {
			return errResult("search_traces failed: %v", err), nil, nil
		}
		out := map[string]any{"source": sourceRef(src), "from": from, "to": to}
		if q != "" {
			out["query"] = q
		}
		if len(rows) > limit {
			rows = rows[:limit]
			out["next_cursor"] = telemetry.TraceCursor(rows[len(rows)-1])
		}
		out["traces"] = telemetry.ShapeTraceRows(rows)
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Get trace")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "get_trace",
		Title:       title,
		Annotations: ann,
		Description: "One trace by id: a summary (duration, services, errors), its spans in tree order with depth, attributes, events and status, and the correlated log lines. Use after search_traces or a trace_id from search_logs.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args getTraceArgs) (*mcp.CallToolResult, any, error) {
		traceID := strings.TrimSpace(args.TraceID)
		if traceID == "" || len(traceID) > 64 {
			return errResult("trace_id is required"), nil, nil
		}
		src, errRes := observeSource(deps, ak, args.SourceID, telemetry.KindTraces)
		if errRes != nil {
			return errRes, nil, nil
		}
		if src.Traces == nil {
			return errResult("source %q has no traces mapping", src.ID), nil, nil
		}
		cols, err := sourceColumns(ctx, deps, ak, src.Database, src.Table)
		if err != nil {
			return errResult("get_trace failed: %v", err), nil, nil
		}
		src.Traces.ResolveNested(cols)
		win, err := traceWindow(ctx, deps, ak, src, traceID, args.Around)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		rows, err := runTelemetry(ctx, deps, ak, telemetry.TraceSpansSQL(src, traceID, win))
		if err != nil {
			return errResult("get_trace failed: %v", err), nil, nil
		}
		if len(rows) == 0 {
			return errResult("trace %q not found in %s.%s (pass around if the trace is old and the table has no trace-id index)", traceID, src.Database, src.Table), nil, nil
		}
		spans := telemetry.BuildSpanTree(telemetry.SpansFromRows(rows))
		summary := telemetry.SummarizeTrace(traceID, spans)
		out := map[string]any{"source": sourceRef(src), "trace": summary}
		maxSpans := clampLimit(args.MaxSpans, defaultTraceSpans, maxObserveLimit)
		if len(spans) > maxSpans {
			spans = spans[:maxSpans]
			out["spans_truncated"] = true
			out["note"] = fmt.Sprintf("showing the first %d of %d spans in tree order; raise max_spans (up to %d)", maxSpans, summary.SpanCount, maxObserveLimit)
		}
		out["spans"] = spans
		if args.IncludeLogs == nil || *args.IncludeLogs {
			out["logs"] = traceLogs(ctx, deps, ak, src, traceID, summary)
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Service map")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "service_map",
		Title:       title,
		Annotations: ann,
		Description: "Services and the calls between them from a traces source over a time range (default the last hour): spans, errors and p50/p95 latency per service and per caller-callee edge. Busy ranges are sampled by whole traces and scaled.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args serviceMapArgs) (*mcp.CallToolResult, any, error) {
		src, errRes := observeSource(deps, ak, args.SourceID, telemetry.KindTraces)
		if errRes != nil {
			return errRes, nil, nil
		}
		if src.Traces == nil {
			return errResult("source %q has no traces mapping", src.ID), nil, nil
		}
		from, to, err := args.resolve(time.Now())
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		params := telemetry.ServiceMapParams{From: from, To: to}
		countSQL, err := telemetry.ServiceMapCountSQL(src, params)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		countRows, err := runTelemetry(ctx, deps, ak, countSQL)
		if err != nil {
			return errResult("service_map failed: %v", err), nil, nil
		}
		var spans int64
		if len(countRows) > 0 {
			spans = telemetry.Int64(countRows[0]["c"])
		}
		rate := telemetry.ServiceMapSampleRate(spans)
		nodesSQL, err := telemetry.ServiceMapNodesSQL(src, params, rate)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		edgesSQL, err := telemetry.ServiceMapEdgesSQL(src, params, rate)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		nodeRows, err := runTelemetry(ctx, deps, ak, nodesSQL)
		if err != nil {
			return errResult("service_map failed: %v", err), nil, nil
		}
		edgeRows, err := runTelemetry(ctx, deps, ak, edgesSQL)
		if err != nil {
			return errResult("service_map failed: %v", err), nil, nil
		}
		nodes, edges := telemetry.ShapeServiceMap(nodeRows, edgeRows)
		return jsonResult(map[string]any{
			"source": sourceRef(src), "from": from, "to": to,
			"services": nodes, "edges": edges,
			"total_spans": spans, "sampled": rate > 1, "sample_rate": rate,
		}), nil, nil
	})

	title, ann = readOnlyTool("List metrics")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_metrics",
		Title:       title,
		Annotations: ann,
		Description: "Metrics present in a metrics source over a time range (default the last hour): name, type, unit, description, and how many services and series report it. Use it to find the metric name and type for query_metrics.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args listMetricsArgs) (*mcp.CallToolResult, any, error) {
		src, errRes := observeSource(deps, ak, args.SourceID, telemetry.KindMetrics)
		if errRes != nil {
			return errRes, nil, nil
		}
		from, to, err := args.resolve(time.Now())
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		sql, err := telemetry.CatalogSQL(src, from, to)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		rows, err := runTelemetry(ctx, deps, ak, sql)
		if err != nil {
			return errResult("list_metrics failed: %v", err), nil, nil
		}
		metrics := telemetry.ShapeCatalogRows(rows)
		if name := strings.ToLower(strings.TrimSpace(args.Name)); name != "" {
			metrics = filterRows(metrics, func(m map[string]any) bool {
				n, _ := m["name"].(string)
				return strings.Contains(strings.ToLower(n), name)
			})
		}
		out := map[string]any{"source": sourceRef(src), "from": from, "to": to, "total": len(metrics)}
		if limit := clampLimit(args.Limit, defaultObserveLimit, maxObserveLimit); len(metrics) > limit {
			metrics = metrics[:limit]
			out["truncated"] = true
		}
		out["metrics"] = metrics
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("Query metric")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "query_metrics",
		Title:       title,
		Annotations: ann,
		Description: "Bucketed series of one metric over a time range (default the last hour) with an aggregation (avg, sum, min, max, last, count, rate, p50/p95/p99), optionally split by up to 2 attributes. Each series has its points plus last, min, max and avg.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args queryMetricsArgs) (*mcp.CallToolResult, any, error) {
		src, errRes := observeSource(deps, ak, args.SourceID, telemetry.KindMetrics)
		if errRes != nil {
			return errRes, nil, nil
		}
		if strings.TrimSpace(args.Metric) == "" || strings.TrimSpace(args.Type) == "" {
			return errResult("metric and type are required; list_metrics gives both"), nil, nil
		}
		from, to, err := args.resolve(time.Now())
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		agg := strings.ToLower(strings.TrimSpace(args.Aggregation))
		if agg == "" {
			agg = "avg"
		}
		params := telemetry.MetricsParams{
			From: from, To: to, Q: args.Query, Metric: strings.TrimSpace(args.Metric),
			Type:        telemetry.MetricType(strings.ToLower(strings.TrimSpace(args.Type))),
			Aggregation: agg, GroupBy: args.GroupBy,
			BucketSeconds: telemetry.MetricBucketSeconds(from, to, args.BucketSeconds),
			LimitSeries:   clampLimit(args.LimitSeries, defaultMetricSeries, maxMetricSeries),
		}
		sql, err := telemetry.MetricQuerySQL(src, params)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		rows, err := runTelemetry(ctx, deps, ak, sql)
		if err != nil {
			return errResult("query_metrics failed: %v", err), nil, nil
		}
		res, err := telemetry.BuildSeries(rows, params)
		if err != nil {
			return errResult("%v", err), nil, nil
		}
		out := map[string]any{
			"source": sourceRef(src), "from": from, "to": to,
			"metric": params.Metric, "type": params.Type, "aggregation": agg,
			"bucket_seconds": res.BucketSeconds, "unit": res.Unit, "series": res.Series,
		}
		if res.Truncated {
			out["truncated"] = true
			out["note"] = fmt.Sprintf("only the %d busiest series; raise limit_series or filter with query", params.LimitSeries)
		}
		if res.RowsTruncated {
			out["rows_truncated"] = true
		}
		return jsonResult(out), nil, nil
	})

	title, ann = readOnlyTool("List telemetry monitors")
	mcp.AddTool(srv, &mcp.Tool{
		Name:        "list_monitors",
		Title:       title,
		Annotations: ann,
		Description: "Telemetry monitors on this connection with their current state: what each counts, threshold, window, last value, ok or firing, last error. Use it to see which alerts on logs, traces or metrics are firing.",
	}, func(ctx context.Context, req *mcp.CallToolRequest, args pageArgs) (*mcp.CallToolResult, any, error) {
		monitors, err := deps.DB.ListTelemetryMonitors(ak.key.ConnectionID)
		if err != nil {
			return errResult("failed to list monitors: %v", err), nil, nil
		}
		sources, err := deps.DB.ListTelemetrySources(ak.key.ConnectionID)
		if err != nil {
			return errResult("failed to list telemetry sources: %v", err), nil, nil
		}
		sourceDB := make(map[string]string, len(sources))
		for _, s := range sources {
			sourceDB[s.ID] = s.Database
		}
		items := make([]map[string]any, 0, len(monitors))
		for _, m := range monitors {
			db, ok := sourceDB[m.SourceID]
			if m.ConnectionID != ak.key.ConnectionID || (!ok && allowedDBs(ak.key) != nil) || (ok && !dbAllowed(ak.key, db)) {
				continue
			}
			items = append(items, map[string]any{
				"id": m.ID, "name": m.Name, "kind": m.Kind, "source_id": m.SourceID, "query": m.Query,
				"window_seconds": m.WindowSeconds, "interval_seconds": m.IntervalSeconds,
				"comparator": m.Comparator, "threshold": m.Threshold, "severity": m.Severity, "enabled": m.Enabled,
				"state": m.LastState, "last_value": m.LastValue, "last_run_at": m.LastRunAt, "last_error": m.LastError,
			})
		}
		return pagedResult("monitors", items, args), nil, nil
	})
}

// traceSearchQuery combines the search with the convenience filters, written
// in the same search language so the compiler quotes every value.
func traceSearchQuery(args searchTracesArgs) (string, error) {
	var terms []string
	if q := strings.TrimSpace(args.Query); q != "" {
		terms = append(terms, "("+q+")")
	}
	if s := strings.TrimSpace(args.Service); s != "" {
		terms = append(terms, "service:"+searchPhrase(s))
	}
	if op := strings.TrimSpace(args.Operation); op != "" {
		terms = append(terms, "span_name:"+searchPhrase(op))
	}
	if args.MinDurationMs < 0 {
		return "", fmt.Errorf("min_duration_ms must not be negative")
	}
	if args.MinDurationMs > 0 {
		terms = append(terms, "duration:>="+strconv.FormatFloat(args.MinDurationMs, 'f', -1, 64))
	}
	if args.ErrorsOnly {
		// The exporter writes Error, other pipelines STATUS_CODE_ERROR.
		terms = append(terms, "(status:error OR status:status_code_error)")
	}
	return strings.Join(terms, " AND "), nil
}

// searchPhrase quotes a value for the search language.
func searchPhrase(s string) string {
	return `"` + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) + `"`
}

// traceWindow bounds a trace lookup like the trace page: the exporter's
// trace-id index when the table has one, else an hour either side of
// around, else no bound.
func traceWindow(ctx context.Context, deps Deps, ak *authedKey, src *telemetry.Source, traceID, around string) (*telemetry.TimeWindow, error) {
	if telemetry.ValidIdent(telemetry.TraceIDTsTable(src)) {
		if rows, err := runTelemetry(ctx, deps, ak, telemetry.TraceIDTsLookupSQL(src)); err == nil && len(rows) > 0 {
			if b, err := runTelemetry(ctx, deps, ak, telemetry.TraceBoundsSQL(src, traceID)); err == nil {
				if win := telemetry.TraceBoundsWindow(b); win != nil {
					return win, nil
				}
			}
		}
	}
	around = strings.TrimSpace(around)
	if around == "" {
		return nil, nil
	}
	t, err := time.Parse(time.RFC3339Nano, around)
	if err != nil {
		return nil, fmt.Errorf("around must be RFC 3339, e.g. 2026-10-16T08:00:00Z")
	}
	return telemetry.Around(t, time.Hour, time.Hour), nil
}

// traceLogs returns up to maxTraceLogs log lines of a trace from the
// correlated logs source, when that source's database is allowed.
func traceLogs(ctx context.Context, deps Deps, ak *authedKey, src *telemetry.Source, traceID string, summary telemetry.TraceSummary) []map[string]any {
	logs := []map[string]any{}
	all, err := deps.DB.ListTelemetrySources(ak.key.ConnectionID)
	if err != nil {
		return logs
	}
	logsSrc := telemetry.CorrelatedLogsSource(src, all)
	if logsSrc == nil || !dbAllowed(ak.key, logsSrc.Database) {
		return logs
	}
	sql, err := telemetry.ByTraceSQL(logsSrc, traceID, summary.LogsWindow())
	if err != nil {
		return logs
	}
	rows, err := runTelemetry(ctx, deps, ak, sql)
	if err != nil {
		return logs
	}
	if len(rows) > maxTraceLogs {
		rows = rows[:maxTraceLogs]
	}
	return telemetry.ShapeLogRows(rows)
}
