package database

import "fmt"

// migrateDashboardsV2 adds dashboard variables and version history. It is
// idempotent: every statement checks before it changes anything.
func (db *DB) migrateDashboardsV2() error {
	// Variables are a JSON array of variable definitions; '[]' means none.
	if err := db.ensureColumn("dashboards", "variables", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS dashboard_versions (
			id TEXT PRIMARY KEY,
			dashboard_id TEXT NOT NULL REFERENCES dashboards(id) ON DELETE CASCADE,
			version INTEGER NOT NULL,
			snapshot TEXT NOT NULL,
			created_by TEXT,
			created_at TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_dashboard_versions_dash_version ON dashboard_versions(dashboard_id, version)`,
	}
	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("dashboards v2 migration: %w", err)
		}
	}
	return nil
}
