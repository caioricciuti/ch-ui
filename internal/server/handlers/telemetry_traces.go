// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// ── Traces ─────────────────────────────────────────────────────────

type tracesRequest struct {
	SourceID string `json:"source_id"`
	From     string `json:"from"`
	To       string `json:"to"`
	Q        string `json:"q"`
	Limit    int    `json:"limit"`
	Cursor   string `json:"cursor"`
}

func (r tracesRequest) params() telemetry.TracesParams {
	return telemetry.TracesParams{From: r.From, To: r.To, Q: r.Q}
}

// tracesSource loads a traces source owned by the session and its columns.
func (h *TelemetryHandler) tracesSource(w http.ResponseWriter, c chCreds, id, connID string) (*telemetry.Source, map[string]string, bool) {
	src := h.loadSource(w, id, connID)
	if src == nil {
		return nil, nil, false
	}
	if src.Kind != telemetry.KindTraces || src.Traces == nil {
		writeError(w, http.StatusBadRequest, "Source is not a traces source")
		return nil, nil, false
	}
	cols, err := h.columnsFor(c, src)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return nil, nil, false
	}
	src.Traces.ResolveNested(cols)
	return src, cols, true
}

// tracesSetup decodes the body, loads the source and its columns.
func (h *TelemetryHandler) tracesSetup(w http.ResponseWriter, r *http.Request) (chCreds, *telemetry.Source, map[string]string, tracesRequest, bool) {
	c, session, err := h.creds(r)
	if err != nil {
		writeCredsError(w, err)
		return chCreds{}, nil, nil, tracesRequest{}, false
	}
	var body tracesRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return chCreds{}, nil, nil, tracesRequest{}, false
	}
	if body.SourceID == "" {
		writeError(w, http.StatusBadRequest, "source_id is required")
		return chCreds{}, nil, nil, tracesRequest{}, false
	}
	if body.From == "" || body.To == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return chCreds{}, nil, nil, tracesRequest{}, false
	}
	src, cols, ok := h.tracesSource(w, c, body.SourceID, session.ConnectionID)
	if !ok {
		return chCreds{}, nil, nil, tracesRequest{}, false
	}
	return c, src, cols, body, true
}

// SearchTraces returns one row per trace with keyset paging.
func (h *TelemetryHandler) SearchTraces(w http.ResponseWriter, r *http.Request) {
	c, src, cols, body, ok := h.tracesSetup(w, r)
	if !ok {
		return
	}
	limit := body.Limit
	if limit <= 0 {
		limit = 100
	}
	if limit > 500 {
		limit = 500
	}
	var cursor *telemetry.Cursor
	if body.Cursor != "" {
		cur, err := telemetry.DecodeCursor(body.Cursor)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		cursor = &cur
	}
	sqlText, err := telemetry.TraceSearchSQL(src, cols, body.params(), limit+1, cursor)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	rows, err := h.exec(c, sqlText, telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Telemetry trace search failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var next interface{}
	if len(rows) > limit {
		rows = rows[:limit]
		next = telemetry.TraceCursor(rows[len(rows)-1])
	}
	traces := telemetry.ShapeTraceRows(rows)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"traces":      traces,
		"next_cursor": next,
		"took_ms":     time.Since(started).Milliseconds(),
	})
}

// TracesHistogram returns root-span counts, errors and latency per bucket.
func (h *TelemetryHandler) TracesHistogram(w http.ResponseWriter, r *http.Request) {
	c, src, cols, body, ok := h.tracesSetup(w, r)
	if !ok {
		return
	}
	bucket := telemetry.BucketSeconds(body.From, body.To)
	sqlText, err := telemetry.TraceHistogramSQL(src, cols, body.params(), bucket)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
	if err != nil {
		slog.Warn("Telemetry trace histogram failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	buckets := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		buckets = append(buckets, map[string]interface{}{
			"t":      str(row["t"]),
			"count":  toInt64(row["c"]),
			"errors": toInt64(row["errors"]),
			"p50_ms": telemetry.Float(row["p50"]),
			"p95_ms": telemetry.Float(row["p95"]),
		})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "bucket_seconds": bucket, "buckets": buckets})
}

// TracesFacets returns service, span name, status and kind distributions.
func (h *TelemetryHandler) TracesFacets(w http.ResponseWriter, r *http.Request) {
	c, src, cols, body, ok := h.tracesSetup(w, r)
	if !ok {
		return
	}
	queries, err := telemetry.BuildTraceFacetsSQL(src, cols, body.params())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	var fr facetResult
	writeJSON(w, http.StatusOK, fr.fields(map[string]interface{}{
		"success":    true,
		"services":   fr.run(h, c, queries.Services),
		"span_names": fr.run(h, c, queries.SpanNames),
		"status":     fr.run(h, c, queries.Status),
		"kinds":      fr.run(h, c, queries.Kinds),
	}))
}

// traceWindow bounds a trace lookup. The exporter's <table>_trace_id_ts
// index gives the trace's real start and end; without it the UI's time
// hint (the trace row's start, the log line's time) bounds it to an hour
// either side; with neither the lookup scans the table as before.
func (h *TelemetryHandler) traceWindow(c chCreds, src *telemetry.Source, traceID string, r *http.Request) *telemetry.TimeWindow {
	if telemetry.ValidIdent(telemetry.TraceIDTsTable(src)) {
		if rows, err := h.exec(c, telemetry.TraceIDTsLookupSQL(src), telemetryMetaTimeout); err == nil && len(rows) > 0 {
			b, err := h.exec(c, telemetry.TraceBoundsSQL(src, traceID), telemetryMetaTimeout)
			if err != nil {
				slog.Warn("Telemetry trace index lookup failed", "error", err)
			} else if win := telemetry.TraceBoundsWindow(b); win != nil {
				return win
			}
		}
	}
	return traceHintWindow(r, traceHintAround)
}

// correlatedLogsSource picks the logs source for a traces source: the
// configured correlation, else the first enabled logs source.
func (h *TelemetryHandler) correlatedLogsSource(src *telemetry.Source) *telemetry.Source {
	all, err := h.DB.ListTelemetrySources(src.ConnectionID)
	if err != nil {
		return nil
	}
	return telemetry.CorrelatedLogsSource(src, all)
}

// GetTrace returns every span of a trace as a pre-ordered tree, a service
// summary, and the correlated log lines.
func (h *TelemetryHandler) GetTrace(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeCredsError(w, err)
		return
	}
	traceID := strings.TrimSpace(chi.URLParam(r, "traceId"))
	if traceID == "" || len(traceID) > 64 {
		writeError(w, http.StatusBadRequest, "trace id is required")
		return
	}
	src, _, ok := h.tracesSource(w, c, r.URL.Query().Get("source_id"), session.ConnectionID)
	if !ok {
		return
	}
	rows, err := h.exec(c, telemetry.TraceSpansSQL(src, traceID, h.traceWindow(c, src, traceID, r)), telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Telemetry trace load failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "Trace not found")
		return
	}
	spans := telemetry.BuildSpanTree(telemetry.SpansFromRows(rows))
	summary := telemetry.SummarizeTrace(traceID, spans)

	logs := []map[string]interface{}{}
	if logsSrc := h.correlatedLogsSource(src); logsSrc != nil {
		// The spans give the trace's real extent; its log lines sit inside
		// it, give or take clock skew between services.
		if sqlText, err := telemetry.ByTraceSQL(logsSrc, traceID, summary.LogsWindow()); err == nil {
			if lrows, err := h.exec(c, sqlText, telemetrySearchTimeout); err == nil {
				logs = telemetry.ShapeLogRows(lrows)
			} else {
				slog.Warn("Telemetry trace logs failed", "error", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"trace":   summary,
		"spans":   spans,
		"logs":    logs,
	})
}
