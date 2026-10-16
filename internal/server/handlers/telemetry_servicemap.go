// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// ServiceMap aggregates spans into services (nodes) and cross-service
// parent→child calls (edges) over a time range.
func (h *TelemetryHandler) ServiceMap(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeCredsError(w, err)
		return
	}
	var body struct {
		SourceID string `json:"source_id"`
		From     string `json:"from"`
		To       string `json:"to"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if body.SourceID == "" || body.From == "" || body.To == "" {
		writeError(w, http.StatusBadRequest, "source_id, from and to are required")
		return
	}
	src := h.loadSource(w, body.SourceID, session.ConnectionID)
	if src == nil {
		return
	}
	if src.Kind != telemetry.KindTraces || src.Traces == nil {
		writeError(w, http.StatusBadRequest, "Source is not a traces source")
		return
	}
	params := telemetry.ServiceMapParams{From: body.From, To: body.To}

	// Count first so a busy range samples whole traces on both sides of
	// the parent/child join instead of cutting each side on its own.
	countSQL, err := telemetry.ServiceMapCountSQL(src, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	countRows, err := h.exec(c, countSQL, telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Telemetry service map count failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	var spans int64
	if len(countRows) > 0 {
		spans = toInt64(countRows[0]["c"])
	}
	rate := telemetry.ServiceMapSampleRate(spans)

	nodesSQL, err := telemetry.ServiceMapNodesSQL(src, params, rate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	edgesSQL, err := telemetry.ServiceMapEdgesSQL(src, params, rate)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	nodeRows, err := h.exec(c, nodesSQL, telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Telemetry service map nodes query failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	edgeRows, err := h.exec(c, edgesSQL, telemetrySearchTimeout)
	if err != nil {
		slog.Warn("Telemetry service map edges query failed", "error", err)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	nodes, edges := telemetry.ShapeServiceMap(nodeRows, edgeRows)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":     true,
		"nodes":       nodes,
		"edges":       edges,
		"sampled":     rate > 1,
		"sample_rate": rate,
		"total_spans": spans,
	})
}
