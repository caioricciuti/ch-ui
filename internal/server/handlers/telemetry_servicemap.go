package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

// ServiceMap aggregates spans into services (nodes) and cross-service
// parent→child calls (edges) over a time range.
func (h *TelemetryHandler) ServiceMap(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
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

	nodesSQL, err := telemetry.ServiceMapNodesSQL(src, params)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	edgesSQL, err := telemetry.ServiceMapEdgesSQL(src, params)
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

	nodes := make([]map[string]interface{}, 0, len(nodeRows))
	for _, row := range nodeRows {
		nodes = append(nodes, map[string]interface{}{
			"service": str(row["service"]),
			"spans":   toInt64(row["spans"]),
			"errors":  toInt64(row["errors"]),
			"p50_ms":  roundMs(row["p50_ms"]),
			"p95_ms":  roundMs(row["p95_ms"]),
		})
	}
	edges := make([]map[string]interface{}, 0, len(edgeRows))
	for _, row := range edgeRows {
		edges = append(edges, map[string]interface{}{
			"from":   str(row["from_service"]),
			"to":     str(row["to_service"]),
			"calls":  toInt64(row["calls"]),
			"errors": toInt64(row["errors"]),
			"p50_ms": roundMs(row["p50_ms"]),
			"p95_ms": roundMs(row["p95_ms"]),
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"nodes":   nodes,
		"edges":   edges,
	})
}

// roundMs turns a gateway numeric (float64, json.Number or string) into a
// millisecond value with one decimal; NaN/absent become 0.
func roundMs(v interface{}) float64 {
	var f float64
	switch t := v.(type) {
	case float64:
		f = t
	case json.Number:
		f, _ = t.Float64()
	case string:
		f, _ = strconv.ParseFloat(t, 64)
	case int64:
		f = float64(t)
	case int:
		f = float64(t)
	}
	if f != f { // NaN
		return 0
	}
	return float64(int64(f*10+0.5)) / 10
}
