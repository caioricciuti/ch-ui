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
		writeError(w, http.StatusUnauthorized, err.Error())
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

func spanFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case json.Number:
		f, _ := t.Float64()
		return f
	case int64:
		return float64(t)
	case int:
		return float64(t)
	case string:
		f, _ := json.Number(t).Float64()
		return f
	default:
		return 0
	}
}

func stringSlice(v interface{}) []string {
	arr, ok := v.([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(arr))
	for _, x := range arr {
		out = append(out, str(x))
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
		out = append(out, stringMap(x))
	}
	return out
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
		last := rows[len(rows)-1]
		next = telemetry.EncodeCursor(telemetry.Cursor{TimestampNs: str(last["start_ns"]), TraceID: str(last["trace_id"])})
	}
	traces := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		startNs := toInt64(row["start_ns"])
		endNs := toInt64(row["end_ns"])
		services := stringSlice(row["services"])
		if services == nil {
			services = []string{}
		}
		traces = append(traces, map[string]interface{}{
			"trace_id":     str(row["trace_id"]),
			"root_span":    str(row["root_span"]),
			"root_service": str(row["root_service"]),
			"start":        str(row["start"]),
			"start_ns":     str(row["start_ns"]),
			"duration_ms":  float64(endNs-startNs) / 1e6,
			"span_count":   toInt64(row["span_count"]),
			"error_count":  toInt64(row["error_count"]),
			"services":     services,
			"status":       str(row["status"]),
		})
	}
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
			"p50_ms": spanFloat(row["p50"]),
			"p95_ms": spanFloat(row["p95"]),
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
	type facet struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	run := func(sqlText string) []facet {
		out := []facet{}
		if sqlText == "" {
			return out
		}
		rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
		if err != nil {
			slog.Warn("Telemetry trace facet query failed", "error", err)
			return out
		}
		for _, row := range rows {
			out = append(out, facet{Value: str(row["value"]), Count: toInt64(row["c"])})
		}
		return out
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":    true,
		"services":   run(queries.Services),
		"span_names": run(queries.SpanNames),
		"status":     run(queries.Status),
		"kinds":      run(queries.Kinds),
	})
}

// correlatedLogsSource picks the logs source for a traces source: the
// configured correlation, else the first enabled logs source.
func (h *TelemetryHandler) correlatedLogsSource(src *telemetry.Source) *telemetry.Source {
	if src.CorrelatedLogs != "" {
		if logs, err := h.DB.GetTelemetrySource(src.CorrelatedLogs); err == nil && logs != nil &&
			logs.ConnectionID == src.ConnectionID && logs.Kind == telemetry.KindLogs && logs.Logs != nil {
			return logs
		}
	}
	all, err := h.DB.ListTelemetrySources(src.ConnectionID)
	if err != nil {
		return nil
	}
	for i := range all {
		if all[i].Kind == telemetry.KindLogs && all[i].Enabled && all[i].Logs != nil {
			return &all[i]
		}
	}
	return nil
}

// GetTrace returns every span of a trace as a pre-ordered tree, a service
// summary, and the correlated log lines.
func (h *TelemetryHandler) GetTrace(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
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
	rows, err := h.exec(c, telemetry.TraceSpansSQL(src, traceID), telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Telemetry trace load failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	if len(rows) == 0 {
		writeError(w, http.StatusNotFound, "Trace not found")
		return
	}
	spans := make([]telemetry.Span, 0, len(rows))
	for _, row := range rows {
		spans = append(spans, telemetry.Span{
			SpanID:        str(row["span_id"]),
			ParentSpanID:  str(row["parent_span_id"]),
			Name:          str(row["name"]),
			Kind:          str(row["kind"]),
			Service:       str(row["service"]),
			Start:         str(row["start"]),
			StartNs:       str(row["start_ns"]),
			DurationMs:    spanFloat(row["duration_ms"]),
			Status:        str(row["status"]),
			StatusMessage: str(row["status_message"]),
			Attributes:    stringMap(row["attributes"]),
			Resource:      stringMap(row["resource"]),
			Events:        telemetry.ZipEvents(stringSlice(row["ev_time"]), stringSlice(row["ev_name"]), mapSlice(row["ev_attrs"])),
			Links:         telemetry.ZipLinks(stringSlice(row["lk_trace"]), stringSlice(row["lk_span"]), mapSlice(row["lk_attrs"])),
		})
	}
	spans = telemetry.BuildSpanTree(spans)

	// Trace summary: earliest start to latest end, services in first-seen order.
	var startNs, endNs int64
	errors := 0
	type svcAgg struct {
		spans, errors int
		duration      float64
	}
	svcOrder := []string{}
	svcs := map[string]*svcAgg{}
	for _, s := range spans {
		st := toInt64(s.StartNs)
		en := st + int64(s.DurationMs*1e6)
		if startNs == 0 || st < startNs {
			startNs = st
		}
		if en > endNs {
			endNs = en
		}
		a := svcs[s.Service]
		if a == nil {
			a = &svcAgg{}
			svcs[s.Service] = a
			svcOrder = append(svcOrder, s.Service)
		}
		a.spans++
		a.duration += s.DurationMs
		if s.Status == "Error" {
			a.errors++
			errors++
		}
	}
	services := make([]map[string]interface{}, 0, len(svcOrder))
	for _, name := range svcOrder {
		a := svcs[name]
		services = append(services, map[string]interface{}{
			"name": name, "spans": a.spans, "errors": a.errors, "duration_ms": a.duration,
		})
	}
	start := ""
	if len(spans) > 0 {
		start = spans[0].Start
		for _, s := range spans {
			if toInt64(s.StartNs) == startNs {
				start = s.Start
				break
			}
		}
	}

	logs := []map[string]interface{}{}
	if logsSrc := h.correlatedLogsSource(src); logsSrc != nil {
		if sqlText, err := telemetry.ByTraceSQL(logsSrc, traceID); err == nil {
			if lrows, err := h.exec(c, sqlText, telemetrySearchTimeout); err == nil {
				logs = shapeLogRows(lrows)
			} else {
				slog.Warn("Telemetry trace logs failed", "error", err)
			}
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"trace": map[string]interface{}{
			"trace_id":    traceID,
			"start":       start,
			"duration_ms": float64(endNs-startNs) / 1e6,
			"span_count":  len(spans),
			"error_count": errors,
			"services":    services,
		},
		"spans": spans,
		"logs":  logs,
	})
}
