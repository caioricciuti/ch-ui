package governance

import (
	"context"
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
	syncTickInterval = 5 * time.Minute
	queryTimeout     = 60 * time.Second
	staleDuration    = 10 * time.Minute
)

// Syncer orchestrates ClickHouse → SQLite governance synchronisation.
// It runs periodic background syncs and supports on-demand sync for
// individual connections.
type Syncer struct {
	store       *Store
	db          *database.DB
	gateway     *tunnel.Gateway
	secret      string
	activeSyncs sync.Map // connectionID → bool (prevents concurrent syncs per connection)
	stopCh      chan struct{}
}

// NewSyncer creates a new governance Syncer.
func NewSyncer(store *Store, db *database.DB, gw *tunnel.Gateway, secret string) *Syncer {
	return &Syncer{
		store:   store,
		db:      db,
		gateway: gw,
		secret:  secret,
		stopCh:  make(chan struct{}),
	}
}

// GetStore returns the underlying governance store.
func (s *Syncer) GetStore() *Store {
	return s.store
}

// StartBackground launches the background goroutine that ticks every 5 minutes
// to sync governance data for all connected tunnels.
func (s *Syncer) StartBackground() {
	go func() {
		slog.Info("Governance syncer started", "interval", syncTickInterval)
		ticker := time.NewTicker(syncTickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				slog.Info("Governance syncer stopped")
				return
			case <-ticker.C:
				s.backgroundTick()
			}
		}
	}()
}

// Stop signals the background goroutine to stop.
func (s *Syncer) Stop() {
	close(s.stopCh)
}

// SyncConnection runs all three governance sync phases (metadata, querylog, access)
// for a single connection. It prevents concurrent syncs per connection.
func (s *Syncer) SyncConnection(ctx context.Context, creds CHCredentials) (*SyncResult, error) {
	// Prevent concurrent syncs for the same connection
	if _, loaded := s.activeSyncs.LoadOrStore(creds.ConnectionID, true); loaded {
		return nil, fmt.Errorf("sync already in progress for connection %s", creds.ConnectionID)
	}
	defer s.activeSyncs.Delete(creds.ConnectionID)

	result := &SyncResult{}

	// Phase 1: Metadata
	metaResult, err := s.syncMetadata(ctx, creds)
	if err != nil {
		result.MetadataError = err.Error()
		slog.Error("Metadata sync failed", "connection", creds.ConnectionID, "error", err)
	} else {
		result.MetadataResult = metaResult
	}

	// Phase 2: Query log
	qlResult, err := s.syncQueryLog(ctx, creds)
	if err != nil {
		result.QueryLogError = err.Error()
		slog.Error("Query log sync failed", "connection", creds.ConnectionID, "error", err)
	} else {
		result.QueryLogResult = qlResult
	}

	// Phase 3: Access
	accessResult, err := s.syncAccess(ctx, creds)
	if err != nil {
		result.AccessError = err.Error()
		slog.Error("Access sync failed", "connection", creds.ConnectionID, "error", err)
	} else {
		result.AccessResult = accessResult
	}

	return result, nil
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

// backgroundTick iterates over all connections, checks tunnel status and
// sync staleness, borrows credentials from active sessions, and triggers
// SyncConnection in goroutines.
func (s *Syncer) backgroundTick() {
	connections, err := s.db.GetConnections()
	if err != nil {
		slog.Error("Governance sync: failed to load connections", "error", err)
		return
	}

	var wg sync.WaitGroup
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

		// Check if any sync type is stale
		if !s.isSyncStale(connID) {
			continue
		}

		// Borrow credentials from an active session
		creds, err := s.findCredentials(connID)
		if err != nil {
			slog.Debug("Governance sync: no credentials for connection",
				"connection", connID, "error", err)
			continue
		}

		wg.Add(1)
		go func(c CHCredentials) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()

			result, err := s.SyncConnection(ctx, c)
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
		}(creds)
	}

	wg.Wait()
}

// isSyncStale returns true if any sync type for the connection is older than staleDuration.
func (s *Syncer) isSyncStale(connectionID string) bool {
	syncTypes := []SyncType{SyncMetadata, SyncQueryLog, SyncAccess}
	for _, st := range syncTypes {
		state, err := s.store.GetSyncState(connectionID, string(st))
		if err != nil || state == nil {
			return true // no state yet → needs sync
		}
		if state.LastSyncedAt == nil {
			return true
		}
		lastSync, err := time.Parse(time.RFC3339, *state.LastSyncedAt)
		if err != nil {
			return true
		}
		if time.Since(lastSync) > staleDuration {
			return true
		}
	}
	return false
}

// findCredentials borrows credentials from an active session for the given connection.
// It tries up to 3 recent sessions and returns the first one with a valid password.
func (s *Syncer) findCredentials(connectionID string) (CHCredentials, error) {
	sessions, err := s.db.GetActiveSessionsByConnection(connectionID, 3)
	if err != nil {
		return CHCredentials{}, fmt.Errorf("failed to load sessions: %w", err)
	}

	for _, sess := range sessions {
		password, err := crypto.Decrypt(sess.EncryptedPassword, s.secret)
		if err != nil {
			continue
		}
		return CHCredentials{
			ConnectionID: connectionID,
			User:         sess.ClickhouseUser,
			Password:     password,
		}, nil
	}

	return CHCredentials{}, fmt.Errorf("no active sessions with valid credentials for connection %s", connectionID)
}

// executeQuery sends a SQL query through the tunnel and returns parsed rows.
// The ClickHouse JSON format returns data as an array of objects:
//
//	{"data": [{"col1": "val1", "col2": "val2"}, ...], "meta": [...], ...}
//
// The tunnel's QueryResult.Data contains this "data" array as json.RawMessage.
// We first try to unmarshal as []map[string]interface{} (JSON format).
// If that fails we fall back to [][]interface{} (JSONCompact) and combine with meta.
func (s *Syncer) executeQuery(creds CHCredentials, sql string) ([]map[string]interface{}, error) {
	result, err := s.gateway.ExecuteQuery(creds.ConnectionID, sql, creds.User, creds.Password, queryTimeout)
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
