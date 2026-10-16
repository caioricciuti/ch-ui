// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// ── Metrics (phase 3) ───────────────────────────────────────────────

// metricsSetup resolves credentials and a metrics source from the request.
func (h *TelemetryHandler) metricsSetup(w http.ResponseWriter, r *http.Request, sourceID string) (chCreds, *telemetry.Source, bool) {
	c, session, err := h.creds(r)
	if err != nil {
		writeCredsError(w, err)
		return chCreds{}, nil, false
	}
	if sourceID == "" {
		writeError(w, http.StatusBadRequest, "source_id is required")
		return chCreds{}, nil, false
	}
	src := h.loadSource(w, sourceID, session.ConnectionID)
	if src == nil {
		return chCreds{}, nil, false
	}
	if src.Kind != telemetry.KindMetrics || src.Tables == nil {
		writeError(w, http.StatusBadRequest, "Source is not a metrics source")
		return chCreds{}, nil, false
	}
	return c, src, true
}

// MetricsCatalog lists the metrics present in the range across the tables.
func (h *TelemetryHandler) MetricsCatalog(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, src, ok := h.metricsSetup(w, r, q.Get("source_id"))
	if !ok {
		return
	}
	from, to := q.Get("from"), q.Get("to")
	if from == "" || to == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	sqlText, err := telemetry.CatalogSQL(src, from, to)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
	if err != nil {
		slog.Warn("Metrics catalog failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	metrics := telemetry.ShapeCatalogRows(rows)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "metrics": metrics})
}

type metricsQueryRequest struct {
	SourceID      string   `json:"source_id"`
	From          string   `json:"from"`
	To            string   `json:"to"`
	Metric        string   `json:"metric"`
	Type          string   `json:"type"`
	Aggregation   string   `json:"aggregation"`
	GroupBy       []string `json:"group_by"`
	Q             string   `json:"q"`
	BucketSeconds int      `json:"bucket_seconds"`
	LimitSeries   int      `json:"limit_series"`
}

// MetricsQuery returns bucketed series for one metric.
func (h *TelemetryHandler) MetricsQuery(w http.ResponseWriter, r *http.Request) {
	var body metricsQueryRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	c, src, ok := h.metricsSetup(w, r, body.SourceID)
	if !ok {
		return
	}
	if body.From == "" || body.To == "" {
		writeError(w, http.StatusBadRequest, "from and to are required")
		return
	}
	typ := telemetry.MetricType(strings.ToLower(strings.TrimSpace(body.Type)))
	agg := strings.ToLower(strings.TrimSpace(body.Aggregation))
	if agg == "" {
		agg = "avg"
	}
	bucket := telemetry.MetricBucketSeconds(body.From, body.To, body.BucketSeconds)
	limitSeries := body.LimitSeries
	if limitSeries <= 0 {
		limitSeries = 20
	}
	if limitSeries > 100 {
		limitSeries = 100
	}
	params := telemetry.MetricsParams{
		From: body.From, To: body.To, Q: body.Q, Metric: body.Metric, Type: typ,
		Aggregation: agg, GroupBy: body.GroupBy, BucketSeconds: bucket, LimitSeries: limitSeries,
	}
	sqlText, err := telemetry.MetricQuerySQL(src, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	started := time.Now()
	rows, err := h.exec(c, sqlText, telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Metrics query failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	res, err := telemetry.BuildSeries(rows, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	resp := map[string]interface{}{
		"success":        true,
		"bucket_seconds": res.BucketSeconds,
		"unit":           res.Unit,
		"series":         res.Series,
		"took_ms":        time.Since(started).Milliseconds(),
	}
	if res.Truncated {
		resp["truncated"] = true
	}
	if res.RowsTruncated {
		resp["rows_truncated"] = true
	}
	writeJSON(w, http.StatusOK, resp)
}

// MetricsAttributes lists attribute keys and top values for a metric.
func (h *TelemetryHandler) MetricsAttributes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	c, src, ok := h.metricsSetup(w, r, q.Get("source_id"))
	if !ok {
		return
	}
	from, to, metric := q.Get("from"), q.Get("to"), q.Get("metric")
	typ := telemetry.MetricType(strings.ToLower(q.Get("type")))
	if from == "" || to == "" || metric == "" {
		writeError(w, http.StatusBadRequest, "from, to and metric are required")
		return
	}
	sqlText, err := telemetry.AttributesSQL(src, from, to, metric, typ)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	rows, err := h.exec(c, sqlText, telemetrySummaryTimeout)
	if err != nil {
		slog.Warn("Metrics attributes failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	type valueCount struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	values := map[string][]valueCount{}
	totals := map[string]int64{}
	var keyOrder []string
	for _, row := range rows {
		k, v, n := str(row["k"]), str(row["v"]), toInt64(row["c"])
		if k == "" {
			continue
		}
		if _, seen := totals[k]; !seen {
			keyOrder = append(keyOrder, k)
		}
		totals[k] += n
		if len(values[k]) < 10 {
			values[k] = append(values[k], valueCount{Value: v, Count: n})
		}
	}
	sort.SliceStable(keyOrder, func(i, j int) bool { return totals[keyOrder[i]] > totals[keyOrder[j]] })
	if len(keyOrder) > 20 {
		keyOrder = keyOrder[:20]
	}
	keys := make([]map[string]interface{}, 0, len(keyOrder))
	for _, k := range keyOrder {
		keys = append(keys, map[string]interface{}{"key": k, "values": values[k]})
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "keys": keys})
}

// ── Helpers ─────────────────────────────────────────────────────────

// toFloat reads a gateway cell as float64; 0 for nil/NaN/non-numbers.
func toFloat(v interface{}) float64 { return telemetry.Float(v) }
