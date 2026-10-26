// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package governance

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/accesshistory"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/rbac"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const (
	syncTickInterval = 5 * time.Minute
	queryTimeout     = 60 * time.Second
	staleDuration    = 10 * time.Minute
	// A background sync gets this long per connection, all phases included.
	connectionSyncTimeout = 5 * time.Minute
	// A phase that keeps failing is retried after 10m, 20m, 40m ... and at
	// most every 6h, until a sync succeeds.
	failureBackoffBase = 10 * time.Minute
	failureBackoffMax  = 6 * time.Hour
)

// allSyncTypes lists the sync phases in the order they run.
var allSyncTypes = []SyncType{SyncMetadata, SyncQueryLog, SyncAccess}

// Syncer orchestrates ClickHouse → SQLite governance synchronisation.
// It runs periodic background syncs and supports on-demand sync for
// individual connections.
type Syncer struct {
	store       *Store
	db          *database.DB
	gateway     *tunnel.Gateway
	secret      string
	pro         func() bool
	activeSyncs sync.Map // connectionID → bool (prevents concurrent syncs per connection)
	// govUsers keeps the ClickHouse user of each connection's last
	// background sync, so a borrowed session account stays the same.
	govUsers sync.Map // connectionID → string
	// credIssues holds the credential problem last logged per connection,
	// so it is logged once per change rather than every tick.
	credIssues sync.Map // connectionID → string
	// inflight tracks background syncs started by backgroundTick.
	inflight sync.WaitGroup
	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
}

// NewSyncer creates a new governance Syncer.
//
// pro reports whether Pro work may run now (see config.ProGate). While it
// returns false, or is nil, the background loop keeps running but syncs and
// prunes nothing, and resumes by itself once a license is active. On-demand
// SyncConnection is not gated here: its callers sit behind RequirePro.
func NewSyncer(store *Store, db *database.DB, gw *tunnel.Gateway, secret string, pro func() bool) *Syncer {
	return &Syncer{
		store:   store,
		db:      db,
		gateway: gw,
		secret:  secret,
		pro:     pro,
	}
}

// GetStore returns the underlying governance store.
func (s *Syncer) GetStore() *Store {
	return s.store
}

// StartBackground launches the background goroutine that ticks every 5 minutes
// to sync governance data for all connected tunnels. Idempotent: a second call
// while already running is a no-op.
func (s *Syncer) StartBackground() {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return
	}
	s.stopCh = make(chan struct{})
	s.running = true
	stopCh := s.stopCh
	s.mu.Unlock()

	go func() {
		defer safe.Recover("governance-syncer")
		slog.Info("Governance syncer started", "interval", syncTickInterval)

		if s.proAllowed() {
			if connections, err := s.db.GetConnections(); err == nil {
				s.pruneRetention(connections)
			}
		}

		ticker := time.NewTicker(syncTickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-stopCh:
				slog.Info("Governance syncer stopped")
				return
			case <-ticker.C:
				s.backgroundTick()
			}
		}
	}()
}

// Stop signals the background goroutine to stop. Safe to call when the syncer
// is not running — a no-op in that case.
func (s *Syncer) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.running {
		return
	}
	close(s.stopCh)
	s.running = false
}

// IsRunning reports whether the background goroutine is currently active.
func (s *Syncer) IsRunning() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// SyncConnection runs all three governance sync phases (metadata, querylog, access)
// for a single connection. It prevents concurrent syncs per connection.
func (s *Syncer) SyncConnection(ctx context.Context, creds CHCredentials) (*SyncResult, error) {
	return s.syncPhases(ctx, creds, allSyncTypes)
}

// syncPhases runs the given sync phases for a connection, in their usual
// order. Access history is recorded along with the access phase.
func (s *Syncer) syncPhases(ctx context.Context, creds CHCredentials, phases []SyncType) (*SyncResult, error) {
	// Prevent concurrent syncs for the same connection
	if _, loaded := s.activeSyncs.LoadOrStore(creds.ConnectionID, true); loaded {
		return nil, fmt.Errorf("sync already in progress for connection %s", creds.ConnectionID)
	}
	defer s.activeSyncs.Delete(creds.ConnectionID)

	run := make(map[SyncType]bool, len(phases))
	for _, p := range phases {
		run[p] = true
	}
	result := &SyncResult{}

	// Phase 1: Metadata
	if run[SyncMetadata] {
		metaResult, err := s.syncMetadata(ctx, creds)
		if err != nil {
			result.MetadataError = err.Error()
			slog.Error("Metadata sync failed", "connection", creds.ConnectionID, "error", err)
		} else {
			result.MetadataResult = metaResult
		}
	}

	// Phase 2: Query log
	if run[SyncQueryLog] {
		qlResult, err := s.syncQueryLog(ctx, creds)
		if err != nil {
			result.QueryLogError = err.Error()
			slog.Error("Query log sync failed", "connection", creds.ConnectionID, "error", err)
		} else {
			result.QueryLogResult = qlResult
		}
	}

	// Phase 3: Access
	if run[SyncAccess] {
		accessResult, err := s.syncAccess(ctx, creds)
		if err != nil {
			result.AccessError = err.Error()
			slog.Error("Access sync failed", "connection", creds.ConnectionID, "error", err)
		} else {
			result.AccessResult = accessResult
		}

		// Phase 4: Access history, read with the governance account so drift
		// is judged on one steady view.
		if s.proAllowed() && s.db.GovernanceSyncEnabled() {
			s.recordAccessHistory(ctx, creds)
		}
	}

	return result, nil
}

// syncExecutor runs access snapshot queries through executeQuery.
type syncExecutor struct {
	s     *Syncer
	creds CHCredentials
}

func (e syncExecutor) Query(ctx context.Context, sql string, _ map[string]string) ([]map[string]any, error) {
	return e.s.executeQuery(ctx, e.creds, sql)
}

// recordAccessHistory stores the access control state for access history.
// Failures are logged; they never fail the sync.
func (s *Syncer) recordAccessHistory(ctx context.Context, creds CHCredentials) {
	snap, err := rbac.ReadSnapshot(ctx, syncExecutor{s: s, creds: creds})
	if err != nil {
		slog.Warn("Access history snapshot failed", "connection", creds.ConnectionID, "error", err)
		return
	}
	if _, _, err := accesshistory.Record(s.db, accesshistory.Capture{
		ConnectionID: creds.ConnectionID, Source: accesshistory.SourceSync, Actor: "governance sync", CHUser: creds.User, Snapshot: snap,
	}); err != nil {
		slog.Warn("Access history record failed", "connection", creds.ConnectionID, "error", err)
	}
}

// SyncSingle runs a single sync phase for a connection.
func (s *Syncer) SyncSingle(ctx context.Context, creds CHCredentials, syncType SyncType) error {
	switch syncType {
	case SyncMetadata:
		_, err := s.syncMetadata(ctx, creds)
		return err
	case SyncQueryLog:
		_, err := s.syncQueryLog(ctx, creds)
		return err
	case SyncAccess:
		_, err := s.syncAccess(ctx, creds)
		return err
	default:
		return fmt.Errorf("unknown sync type: %s", syncType)
	}
}

// proAllowed reports whether background Pro work may run now.
func (s *Syncer) proAllowed() bool {
	return s.pro != nil && s.pro()
}

// backgroundTick iterates over all connections, checks tunnel status and
// which sync phases are due, resolves background credentials, and starts a
// sync per connection in its own goroutine. It does not wait for them: a
// slow connection must not hold back the others' next tick, and activeSyncs
// already keeps one sync per connection.
func (s *Syncer) backgroundTick() {
	if !s.proAllowed() {
		return
	}
	connections, err := s.db.GetConnections()
	if err != nil {
		slog.Error("Governance sync: failed to load connections", "error", err)
		return
	}

	s.pruneRetention(connections)

	now := time.Now()
	for _, conn := range connections {
		connID := conn.ID

		// Skip if tunnel is offline
		if !s.gateway.IsTunnelOnline(connID) {
			continue
		}

		// Skip if a sync is already running for this connection
		if _, loaded := s.activeSyncs.Load(connID); loaded {
			continue
		}

		// Only the phases that are stale, or past their failure backoff
		phases := s.duePhases(connID, now)
		if len(phases) == 0 {
			continue
		}

		// Resolve the account configured for this worker
		creds, err := s.findCredentials(connID)
		if err != nil {
			s.recordCredentialProblem(connID, phases, err)
			continue
		}
		if _, had := s.credIssues.LoadAndDelete(connID); had {
			slog.Info("Governance sync: account available again", "connection", connID)
		}

		s.inflight.Add(1)
		go func(c CHCredentials, phases []SyncType) {
			defer s.inflight.Done()
			ctx, cancel := context.WithTimeout(context.Background(), connectionSyncTimeout)
			defer cancel()

			s.govUsers.Store(c.ConnectionID, c.User)
			result, err := s.syncPhases(ctx, c, phases)
			if err != nil {
				slog.Error("Governance background sync failed",
					"connection", c.ConnectionID, "error", err)
				return
			}

			slog.Info("Governance background sync completed",
				"connection", c.ConnectionID,
				"metadata", result.MetadataResult != nil,
				"querylog", result.QueryLogResult != nil,
				"access", result.AccessResult != nil,
			)
		}(creds, phases)
	}
}

// recordCredentialProblem stores why no governance account could be resolved
// as the error of each due phase, so the sync status shows it and the
// failure backoff applies. It is logged once per connection per change.
func (s *Syncer) recordCredentialProblem(connID string, phases []SyncType, err error) {
	msg := credentialProblemMessage(err)
	for _, p := range phases {
		if uerr := s.store.UpsertSyncState(connID, string(p), "error", nil, &msg, 0); uerr != nil {
			slog.Error("Failed to update sync state for missing governance account", "connection", connID, "error", uerr)
		}
	}
	if prev, ok := s.credIssues.Swap(connID, msg); !ok || prev.(string) != msg {
		slog.Warn("Governance sync: no account for connection", "connection", connID, "reason", msg, "error", err)
	}
}

func credentialProblemMessage(err error) string {
	switch {
	case errors.Is(err, database.ErrNoBorrowableSession):
		return "No governance account: no service account is set for governance in this connection's background accounts, and no live CH-UI session can lend its ClickHouse credentials. Set a governance account or sign in."
	case errors.Is(err, database.ErrBackgroundDisabled):
		return "Governance background sync is disabled for this connection in its background accounts."
	default:
		return "Governance account unavailable: " + err.Error()
	}
}

// pruneRetention deletes query log and violation rows older than their
// configured retention windows (database.RetentionConfig, 0 keeps them) for
// every known connection, bounding SQLite growth on busy ClickHouse instances.
func (s *Syncer) pruneRetention(connections []database.Connection) {
	cfg, err := s.db.GetRetentionConfig()
	if err != nil {
		slog.Warn("Governance retention: failed to load config, using defaults", "error", err)
		cfg = database.DefaultRetentionConfig()
	}
	now := time.Now().UTC()
	cutoff := func(days int) string { return now.AddDate(0, 0, -days).Format("2006-01-02 15:04:05") }
	for _, conn := range connections {
		if cfg.GovQueryLog > 0 {
			before := cutoff(cfg.GovQueryLog)
			if n, err := s.store.CleanupOldQueryLogs(conn.ID, before); err != nil {
				slog.Warn("Governance retention prune (query_log) failed",
					"connection", conn.ID, "error", err)
			} else if n > 0 {
				slog.Info("Governance retention pruned query_log",
					"connection", conn.ID, "rows", n, "older_than", before)
			}
		}
		if cfg.GovPolicyViolations > 0 {
			before := cutoff(cfg.GovPolicyViolations)
			if n, err := s.store.CleanupOldViolations(conn.ID, before); err != nil {
				slog.Warn("Governance retention prune (violations) failed",
					"connection", conn.ID, "error", err)
			} else if n > 0 {
				slog.Info("Governance retention pruned violations",
					"connection", conn.ID, "rows", n, "older_than", before)
			}
		}
	}
	if n, err := s.db.PruneRBACSnapshots(time.Now()); err != nil {
		slog.Warn("Access history retention prune failed", "error", err)
	} else if n > 0 {
		slog.Info("Access history retention pruned snapshots", "rows", n)
	}
}

// duePhases returns the sync phases of a connection that should run now.
func (s *Syncer) duePhases(connectionID string, now time.Time) []SyncType {
	var due []SyncType
	for _, st := range allSyncTypes {
		state, err := s.store.GetSyncState(connectionID, string(st))
		if err != nil || syncDue(state, now) {
			due = append(due, st)
		}
	}
	return due
}

// syncDue reports whether a phase should run: never synced, last success
// older than staleDuration, or, after failures, past the failure backoff.
func syncDue(state *SyncState, now time.Time) bool {
	if state == nil {
		return true // no state yet → needs sync
	}
	if state.ConsecutiveFailures > 0 {
		lastAttempt, ok := parseSyncTime(state.LastAttemptAt)
		return !ok || now.Sub(lastAttempt) >= failureBackoff(state.ConsecutiveFailures)
	}
	lastSync, ok := parseSyncTime(state.LastSyncedAt)
	return !ok || now.Sub(lastSync) > staleDuration
}

func parseSyncTime(v *string) (time.Time, bool) {
	if v == nil {
		return time.Time{}, false
	}
	t, err := time.Parse(time.RFC3339, *v)
	return t, err == nil
}

// failureBackoff is the wait before retrying a phase that failed n times in a row.
func failureBackoff(n int) time.Duration {
	d := failureBackoffBase
	for i := 1; i < n && d < failureBackoffMax; i++ {
		d *= 2
	}
	return min(d, failureBackoffMax)
}

// findCredentials resolves the configured background account or session
// fallback. A borrowed session keeps the ClickHouse user of the previous
// background sync while it has a live session, so results do not flip
// between accounts that see different things.
func (s *Syncer) findCredentials(connectionID string) (CHCredentials, error) {
	prefer, _ := s.govUsers.Load(connectionID)
	preferUser, _ := prefer.(string)
	user, password, err := s.db.StableBackgroundCredentials(connectionID, "governance", s.secret, preferUser)
	if err != nil {
		return CHCredentials{}, err
	}
	return CHCredentials{ConnectionID: connectionID, User: user, Password: password}, nil
}

// executeQuery sends a SQL query through the tunnel and returns parsed rows.
// The ClickHouse JSON format returns data as an array of objects:
//
//	{"data": [{"col1": "val1", "col2": "val2"}, ...], "meta": [...], ...}
//
// The tunnel's QueryResult.Data contains this "data" array as json.RawMessage.
// We first try to unmarshal as []map[string]interface{} (JSON format).
// If that fails we fall back to [][]interface{} (JSONCompact) and combine with meta.
func (s *Syncer) executeQuery(ctx context.Context, creds CHCredentials, sql string) ([]map[string]interface{}, error) {
	result, err := s.gateway.ExecuteQueryWithSettingsCtx(ctx, creds.ConnectionID, sql, creds.User, creds.Password,
		map[string]string{"log_comment": GovernanceLogComment}, queryTimeout)
	if err != nil {
		return nil, fmt.Errorf("query execution failed: %w", err)
	}
	if result == nil || len(result.Data) == 0 {
		return nil, nil
	}

	// Try JSON format: array of objects
	var rows []map[string]interface{}
	if err := json.Unmarshal(result.Data, &rows); err == nil {
		return rows, nil
	}

	// Fallback: JSONCompact format — array of arrays + meta
	var arrays [][]interface{}
	if err := json.Unmarshal(result.Data, &arrays); err != nil {
		return nil, fmt.Errorf("failed to parse query result data: %w", err)
	}

	// Parse meta for column names
	type metaCol struct {
		Name string `json:"name"`
		Type string `json:"type"`
	}
	var meta []metaCol
	if result.Meta != nil {
		if err := json.Unmarshal(result.Meta, &meta); err != nil {
			return nil, fmt.Errorf("failed to parse query result meta: %w", err)
		}
	}

	if len(meta) == 0 && len(arrays) > 0 {
		// Generate placeholder column names
		for i := range arrays[0] {
			meta = append(meta, metaCol{Name: fmt.Sprintf("col%d", i), Type: "String"})
		}
	}

	rows = make([]map[string]interface{}, 0, len(arrays))
	for _, row := range arrays {
		m := make(map[string]interface{}, len(meta))
		for i, col := range meta {
			if i < len(row) {
				m[col.Name] = row[i]
			}
		}
		rows = append(rows, m)
	}

	return rows, nil
}
