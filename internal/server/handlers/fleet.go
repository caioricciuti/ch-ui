// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
package handlers

import (
	"net/http"
	"time"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

// FleetHandler uses retained instance monitoring data, never another user's credentials.
type FleetHandler struct {
	DB               *database.DB
	Gateway          *tunnel.Gateway
	Config           *config.Config
	RegressionCounts func(connectionID string) (int, error)
}

type fleetEntry struct {
	ID                  string                 `json:"id"`
	Name                string                 `json:"name"`
	Online              bool                   `json:"online"`
	MonitoringEnabled   bool                   `json:"monitoring_enabled"`
	Status              string                 `json:"status"`
	CapturedAt          string                 `json:"captured_at,omitempty"`
	StaleAfterSeconds   int                    `json:"stale_after_seconds"`
	Cluster             string                 `json:"cluster"`
	Nodes               []clusterhealth.Sample `json:"nodes"`
	MaxReplicationDelay float64                `json:"max_replication_delay"`
	ReplicationQueue    int64                  `json:"replication_queue"`
	ReadonlyReplicas    int64                  `json:"readonly_replicas"`
	PartsPressurePct    float64                `json:"parts_pressure_pct"`
	PendingMutations    int64                  `json:"pending_mutations"`
	LongQueries         int64                  `json:"long_queries"`
	OpenIncidents       int                    `json:"open_incidents"`
	Regressions         *int                   `json:"regressions,omitempty"`
}

func (h *FleetHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequireAdmin(h.DB))
	r.Get("/", h.List)
	return r
}

func fleetStatus(e fleetEntry, now time.Time) string {
	if !e.Online {
		return "offline"
	}
	if !e.MonitoringEnabled {
		return "disabled"
	}
	if len(e.Nodes) == 0 {
		return "missing"
	}
	captured, err := time.Parse(time.RFC3339, e.CapturedAt)
	if err != nil || now.Sub(captured) > time.Duration(e.StaleAfterSeconds)*time.Second || captured.After(now.Add(time.Minute)) {
		return "stale"
	}
	if e.ReadonlyReplicas > 0 || e.MaxReplicationDelay >= 60 || e.PartsPressurePct >= 80 {
		return "critical"
	}
	if e.MaxReplicationDelay >= 10 || e.ReplicationQueue >= 10 || e.PendingMutations > 0 || e.LongQueries > 0 || e.OpenIncidents > 0 || (e.Regressions != nil && *e.Regressions > 0) {
		return "warning"
	}
	return "healthy"
}

func (h *FleetHandler) List(w http.ResponseWriter, r *http.Request) {
	connections, err := h.DB.GetConnections()
	if err != nil {
		writeError(w, 500, "Failed to load connections")
		return
	}
	now := time.Now().UTC()
	entries := make([]fleetEntry, 0, len(connections))
	store := clusterhealth.NewStore(h.DB)
	for _, conn := range connections {
		settings, err := store.GetSettings(conn.ID)
		if err != nil {
			writeError(w, 500, "Failed to load monitoring settings")
			return
		}
		e := fleetEntry{ID: conn.ID, Name: conn.Name, Online: h.Gateway != nil && h.Gateway.IsTunnelOnline(conn.ID), MonitoringEnabled: settings.Enabled, Nodes: []clusterhealth.Sample{}, StaleAfterSeconds: max(180, settings.PollIntervalSeconds*3)}
		// Restrict every node to the same most recent poll: retaining one latest row
		// per node would silently present nodes removed from the cluster as current.
		rows, err := h.DB.Conn().QueryContext(r.Context(), `SELECT cluster, node, captured_at, replication_max_delay, replication_queue_total, replicas_readonly, merges_running, mutations_pending, parts_max_active, parts_pressure_pct, long_queries FROM ch_health_samples WHERE connection_id = ? AND captured_at = (SELECT MAX(captured_at) FROM ch_health_samples WHERE connection_id = ?) ORDER BY node`, conn.ID, conn.ID)
		if err != nil {
			writeError(w, 500, "Failed to load retained cluster health")
			return
		}
		for rows.Next() {
			var sample clusterhealth.Sample
			if err = rows.Scan(&e.Cluster, &sample.Node, &sample.CapturedAt, &sample.ReplicationMaxDelay, &sample.ReplicationQueueTotal, &sample.ReplicasReadonly, &sample.MergesRunning, &sample.MutationsPending, &sample.PartsMaxActive, &sample.PartsPressurePct, &sample.LongQueries); err != nil {
				break
			}
			e.Nodes = append(e.Nodes, sample)
			e.CapturedAt = sample.CapturedAt
			e.MaxReplicationDelay = max(e.MaxReplicationDelay, sample.ReplicationMaxDelay)
			e.ReplicationQueue += sample.ReplicationQueueTotal
			e.ReadonlyReplicas += sample.ReplicasReadonly
			e.PartsPressurePct = max(e.PartsPressurePct, sample.PartsPressurePct)
			e.PendingMutations += sample.MutationsPending
			e.LongQueries += sample.LongQueries
		}
		rowsErr := rows.Err()
		rows.Close()
		if err != nil || rowsErr != nil {
			writeError(w, 500, "Failed to read retained cluster health")
			return
		}
		if err := h.DB.Conn().QueryRowContext(r.Context(), `SELECT COUNT(*) FROM gov_incidents WHERE connection_id = ? AND status <> 'resolved'`, conn.ID).Scan(&e.OpenIncidents); err != nil {
			writeError(w, 500, "Failed to load incident counts")
			return
		}
		if h.RegressionCounts != nil {
			count, err := h.RegressionCounts(conn.ID)
			if err == nil {
				e.Regressions = &count
			}
		}
		e.Status = fleetStatus(e, now)
		entries = append(entries, e)
	}
	writeJSON(w, 200, map[string]any{"captured_at": now.Format(time.RFC3339), "connections": entries})
}
