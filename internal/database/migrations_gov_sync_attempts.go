package database

import "fmt"

// migrateGovSyncAttempts splits the governance sync state into the last
// successful sync (last_synced_at) and the last attempt (last_attempt_at),
// counts consecutive failures for the background backoff, and records the
// ClickHouse user each sync ran as (ch_user) so the syncer keeps using the
// same account and its own queries stay out of the query log harvest.
//
// Rows written before this change get their last attempt from
// last_synced_at, which used to mean "last attempt".
//
// Idempotent: safe to run on every start.
func (db *DB) migrateGovSyncAttempts() error {
	if err := db.ensureColumn("gov_sync_state", "last_attempt_at", "TEXT"); err != nil {
		return fmt.Errorf("gov sync attempts migration: %w", err)
	}
	if err := db.ensureColumn("gov_sync_state", "consecutive_failures", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return fmt.Errorf("gov sync attempts migration: %w", err)
	}
	if err := db.ensureColumn("gov_sync_state", "ch_user", "TEXT"); err != nil {
		return fmt.Errorf("gov sync attempts migration: %w", err)
	}
	if _, err := db.conn.Exec(
		`UPDATE gov_sync_state SET last_attempt_at = last_synced_at WHERE last_attempt_at IS NULL`,
	); err != nil {
		return fmt.Errorf("gov sync attempts backfill: %w", err)
	}
	return nil
}
