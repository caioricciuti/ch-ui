// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const (
	// tickInterval is the harvester's base cadence. Per-connection poll intervals
	// (>= MinPollIntervalSeconds) are honoured on top of this by tracking the last
	// poll time per connection.
	tickInterval = time.Duration(MinPollIntervalSeconds) * time.Second
	// pruneInterval bounds how often retention pruning runs, independent of polling.
	pruneInterval  = 30 * time.Minute
	harvestTimeout = 20 * time.Second
)

// Harvester periodically polls each connected cluster for aggregated health
// samples and stores them as time-series, pruning per the configured retention.
// It mirrors the governance Syncer's lifecycle and credential-borrowing model.
type Harvester struct {
	store   *Store
	db      *database.DB
	gateway *tunnel.Gateway
	secret  string
	pro     func() bool

	mu        sync.Mutex
	running   bool
	stopCh    chan struct{}
	lastPoll  map[string]time.Time
	lastPrune time.Time
	// lastHistory remembers when each health check node last wrote an
	// evaluation row, for the hourly steady-state sample.
	lastHistory map[string]time.Time
	// startedAt starts the grace window in which an offline tunnel is not
	// counted against the connection check: agents reconnect after a
	// restart.
	startedAt time.Time
}

// NewHarvester creates a cluster-health Harvester. pro reports whether Pro
// work may run now (see config.ProGate); while it returns false, or is nil,
// ticks poll and prune nothing, and polling resumes once a license is active.
func NewHarvester(store *Store, db *database.DB, gw *tunnel.Gateway, secret string, pro func() bool) *Harvester {
	return &Harvester{
		store:     store,
		db:        db,
		gateway:   gw,
		secret:    secret,
		pro:       pro,
		lastPoll:  make(map[string]time.Time),
		startedAt: time.Now(),
	}
}

// GetStore returns the underlying store.
func (h *Harvester) GetStore() *Store {
	return h.store
}

// StartBackground launches the background polling goroutine. Idempotent.
func (h *Harvester) StartBackground() {
	h.mu.Lock()
	if h.running {
		h.mu.Unlock()
		return
	}
	h.stopCh = make(chan struct{})
	h.running = true
	stopCh := h.stopCh
	h.mu.Unlock()

	go func() {
		defer safe.Recover("clusterhealth-harvester")
		slog.Info("Cluster health harvester started", "tick", tickInterval)
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				slog.Info("Cluster health harvester stopped")
				return
			case <-ticker.C:
				h.tick()
			}
		}
	}()
}

// Stop signals the background goroutine to stop. Safe when not running.
func (h *Harvester) Stop() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.running {
		return
	}
	close(h.stopCh)
	h.running = false
}

// IsRunning reports whether the harvester goroutine is active.
func (h *Harvester) IsRunning() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.running
}

func (h *Harvester) tick() {
	if h.pro == nil || !h.pro() {
		return
	}
	connections, err := h.db.GetConnections()
	if err != nil {
		slog.Error("Cluster health: failed to load connections", "error", err)
		return
	}

	now := time.Now()
	for _, conn := range connections {
		settings, err := h.store.GetSettings(conn.ID)
		if err != nil {
			slog.Warn("Cluster health: failed to load settings", "connection", conn.ID, "error", err)
			continue
		}
		// Sampling or a check turned off leaves incidents nothing would
		// evaluate again: close them, however the setting changed.
		if err := CloseOrphanedIncidents(h.db, conn.ID, settings.Enabled, now); err != nil {
			slog.Warn("Health alerts: failed to close orphaned incidents", "connection", conn.ID, "error", err)
		}
		if !h.due(conn.ID, settings.PollIntervalSeconds, now) {
			continue
		}
		// The connection check runs even with sampling off.
		probe := h.connectionCheckEnabled(conn.ID)
		if !settings.Enabled && !probe {
			continue
		}
		if !h.gateway.IsTunnelOnline(conn.ID) {
			if probe && now.Sub(h.startedAt) >= reachabilityGrace {
				h.markPolled(conn.ID, now)
				h.evaluateReachability(conn.ID, reachability{breach: true, detail: "tunnel offline"}, now)
			}
			continue
		}
		if settings.Enabled {
			h.pollConnection(conn.ID, settings, now)
		} else {
			h.probeOnly(conn.ID, now)
		}
	}

	if now.Sub(h.lastPrune) >= pruneInterval {
		h.pruneRetention(connections)
		h.pruneEvaluations(now)
		h.lastPrune = now
	}
}

// due reports whether a connection is ready to be polled again given its
// configured interval.
func (h *Harvester) due(connID string, intervalSeconds int, now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	last, ok := h.lastPoll[connID]
	if !ok {
		return true
	}
	return now.Sub(last) >= time.Duration(intervalSeconds)*time.Second
}

func (h *Harvester) markPolled(connID string, now time.Time) {
	h.mu.Lock()
	h.lastPoll[connID] = now
	h.mu.Unlock()
}

func (h *Harvester) pollConnection(connID string, settings Settings, now time.Time) {
	probe := h.connectionCheckEnabled(connID)
	creds, err := h.findCredentials(connID)
	if err != nil {
		slog.Debug("Cluster health: no credentials for connection", "connection", connID, "error", err)
		if probe {
			h.markPolled(connID, now)
			h.evaluateReachability(connID, reachability{unknown: true, detail: "no credentials: " + err.Error()}, now)
		}
		return
	}
	h.markPolled(connID, now)

	exec := ExecFunc(func(sql string) ([]map[string]interface{}, error) { return h.executeQuery(creds, sql) })
	cluster, clusterKnown := ResolveCluster(exec)
	capturedAt := now.UTC().Format(time.RFC3339)

	col, err := CollectSamples(exec, cluster, settings.LongQueryThresholdSecs, capturedAt)
	if err != nil && cluster != "" {
		// The fan-out failed outright; retry scoped to the local node. Other
		// nodes are not stored: their state is unknown, not down.
		if c2, err2 := CollectSamples(exec, "", settings.LongQueryThresholdSecs, capturedAt); err2 == nil {
			col, cluster = c2, ""
			col.Complete = false
		}
	}
	// Without knowing whether there is a cluster, a local-only collection
	// says nothing about the other nodes.
	membersKnown := clusterKnown && col.Complete
	samples := col.Samples
	if len(col.Errors) > 0 {
		slog.Debug("Cluster health: some metrics failed", "connection", connID, "errors", col.Errors)
	}
	if len(samples) == 0 {
		// Nothing came back: whether the connection answers at all decides
		// the connection check.
		if probe {
			h.evaluateReachability(connID, h.selectOne(creds), now)
		}
		return
	}
	if probe {
		h.evaluateReachability(connID, reachability{}, now)
	}
	if err := h.store.InsertSamples(connID, cluster, samples); err != nil {
		slog.Warn("Cluster health: failed to store samples", "connection", connID, "error", err)
	} else {
		slog.Debug("Cluster health sampled", "connection", connID, "cluster", cluster, "nodes", len(samples))
	}
	h.evaluateHealthAlerts(connID, cluster, samples, membersKnown, exec, now)
}

// ResolveCluster returns the cluster the connection belongs to, "" for a
// single node. ok is false when the lookup failed.
func ResolveCluster(exec ExecFunc) (name string, ok bool) {
	rows, err := exec(ResolveClusterQuery)
	if err != nil {
		return "", false
	}
	if len(rows) == 0 {
		return "", true
	}
	name, _ = rows[0]["cluster"].(string)
	if IsValidClusterName(name) {
		return name, true
	}
	return "", true
}

// pruneRetention deletes samples older than each connection's retention window.
func (h *Harvester) pruneRetention(connections []database.Connection) {
	for _, conn := range connections {
		settings, err := h.store.GetSettings(conn.ID)
		if err != nil {
			continue
		}
		cutoff := time.Now().UTC().AddDate(0, 0, -settings.RetentionDays).Format(time.RFC3339)
		if n, err := h.store.CleanupOldSamples(conn.ID, cutoff); err != nil {
			slog.Warn("Cluster health retention prune failed", "connection", conn.ID, "error", err)
		} else if n > 0 {
			slog.Info("Cluster health retention pruned", "connection", conn.ID, "rows", n, "older_than", cutoff)
		}
	}
}

// findCredentials resolves the configured background account or session fallback.
func (h *Harvester) findCredentials(connectionID string) (CHCredentials, error) {
	user, password, err := h.db.BackgroundCredentials(connectionID, "cluster_health", h.secret)
	if err != nil {
		return CHCredentials{}, err
	}
	return CHCredentials{ConnectionID: connectionID, User: user, Password: password}, nil
}

// executeQuery runs a SQL statement through the tunnel and parses the JSON rows.
func (h *Harvester) executeQuery(creds CHCredentials, sql string) ([]map[string]interface{}, error) {
	result, err := h.gateway.ExecuteQueryWithSettings(creds.ConnectionID, sql, creds.User, creds.Password, QuerySettings(), harvestTimeout)
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Data) == 0 {
		return nil, nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(result.Data, &rows); err != nil {
		return nil, fmt.Errorf("parse query result: %w", err)
	}
	return rows, nil
}
