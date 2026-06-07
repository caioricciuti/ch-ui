package clusterhealth

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
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

	mu        sync.Mutex
	running   bool
	stopCh    chan struct{}
	lastPoll  map[string]time.Time
	lastPrune time.Time
}

// NewHarvester creates a cluster-health Harvester.
func NewHarvester(store *Store, db *database.DB, gw *tunnel.Gateway, secret string) *Harvester {
	return &Harvester{
		store:    store,
		db:       db,
		gateway:  gw,
		secret:   secret,
		lastPoll: make(map[string]time.Time),
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
		if !settings.Enabled {
			continue
		}
		if !h.gateway.IsTunnelOnline(conn.ID) {
			continue
		}
		if !h.due(conn.ID, settings.PollIntervalSeconds, now) {
			continue
		}
		h.pollConnection(conn.ID, settings, now)
	}

	if now.Sub(h.lastPrune) >= pruneInterval {
		h.pruneRetention(connections)
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
	creds, err := h.findCredentials(connID)
	if err != nil {
		slog.Debug("Cluster health: no credentials for connection", "connection", connID, "error", err)
		return
	}
	h.markPolled(connID, now)

	exec := ExecFunc(func(sql string) ([]map[string]interface{}, error) { return h.executeQuery(creds, sql) })
	cluster := h.resolveCluster(exec)
	capturedAt := now.UTC().Format(time.RFC3339)

	samples, _, err := CollectSamples(exec, cluster, settings.LongQueryThresholdSecs, capturedAt)
	if err != nil && cluster != "" {
		// Cluster-wide replication agg failed; retry scoped to the local node.
		if s2, _, err2 := CollectSamples(exec, "", settings.LongQueryThresholdSecs, capturedAt); err2 == nil {
			samples, cluster = s2, ""
		}
	}
	if len(samples) == 0 {
		return
	}
	if err := h.store.InsertSamples(connID, cluster, samples); err != nil {
		slog.Warn("Cluster health: failed to store samples", "connection", connID, "error", err)
		return
	}
	slog.Debug("Cluster health sampled", "connection", connID, "cluster", cluster, "nodes", len(samples))
}

func (h *Harvester) resolveCluster(exec ExecFunc) string {
	rows, err := exec(ResolveClusterQuery)
	if err != nil || len(rows) == 0 {
		return ""
	}
	name, _ := rows[0]["cluster"].(string)
	if IsValidClusterName(name) {
		return name
	}
	return ""
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

// findCredentials borrows credentials from an active session for the connection.
func (h *Harvester) findCredentials(connectionID string) (CHCredentials, error) {
	sessions, err := h.db.GetActiveSessionsByConnection(connectionID, 3)
	if err != nil {
		return CHCredentials{}, fmt.Errorf("failed to load sessions: %w", err)
	}
	for _, sess := range sessions {
		password, err := crypto.Decrypt(sess.EncryptedPassword, h.secret)
		if err != nil {
			continue
		}
		return CHCredentials{ConnectionID: connectionID, User: sess.ClickhouseUser, Password: password}, nil
	}
	return CHCredentials{}, fmt.Errorf("no active sessions with valid credentials for connection %s", connectionID)
}

// executeQuery runs a SQL statement through the tunnel and parses the JSON rows.
func (h *Harvester) executeQuery(creds CHCredentials, sql string) ([]map[string]interface{}, error) {
	result, err := h.gateway.ExecuteQuery(creds.ConnectionID, sql, creds.User, creds.Password, harvestTimeout)
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
