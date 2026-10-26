package database

import "fmt"

// migrateSSOAccessRemoved adds sso_clickhouse_users.removed_at. Removing an
// SSO person's access while their connection is offline cannot drop the
// ClickHouse user yet; the row is kept for the drop, and removed_at makes
// every path that resolves it refuse it in the meantime.
//
// Idempotent: safe to run on every start.
func (db *DB) migrateSSOAccessRemoved() error {
	if err := db.ensureColumn("sso_clickhouse_users", "removed_at", "TEXT"); err != nil {
		return fmt.Errorf("sso access removed migration: %w", err)
	}
	return nil
}
