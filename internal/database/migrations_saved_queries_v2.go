package database

import "fmt"

// migrateSavedQueriesV2 adds folders, tags, visibility and version history to
// saved queries. It is idempotent: every statement checks before it changes
// anything. Existing rows get visibility 'shared', so they stay visible to
// everyone exactly as before.
func (db *DB) migrateSavedQueriesV2() error {
	if _, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS saved_query_folders (
		id TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		parent_id TEXT REFERENCES saved_query_folders(id) ON DELETE CASCADE,
		created_by TEXT,
		created_at TEXT NOT NULL,
		updated_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("saved queries v2 migration: %w", err)
	}
	// SQLite accepts a REFERENCES clause on ADD COLUMN when the default is
	// NULL. DeleteSavedQueryFolder also clears folder_id in code, so a
	// database opened without foreign keys behaves the same.
	if err := db.ensureColumn("saved_queries", "folder_id", "TEXT REFERENCES saved_query_folders(id) ON DELETE SET NULL"); err != nil {
		return err
	}
	if err := db.ensureColumn("saved_queries", "tags", "TEXT NOT NULL DEFAULT '[]'"); err != nil {
		return err
	}
	if err := db.ensureColumn("saved_queries", "visibility", "TEXT NOT NULL DEFAULT 'shared'"); err != nil {
		return err
	}
	stmts := []string{
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_query_folders_parent_name
			ON saved_query_folders(COALESCE(parent_id, ''), name)`,
		`CREATE TABLE IF NOT EXISTS saved_query_versions (
			id TEXT PRIMARY KEY,
			saved_query_id TEXT NOT NULL REFERENCES saved_queries(id) ON DELETE CASCADE,
			version INTEGER NOT NULL,
			snapshot TEXT NOT NULL,
			created_by TEXT,
			created_at TEXT NOT NULL,
			note TEXT NOT NULL DEFAULT ''
		)`,
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_saved_query_versions_query_version ON saved_query_versions(saved_query_id, version)`,
		`CREATE INDEX IF NOT EXISTS idx_saved_queries_folder ON saved_queries(folder_id)`,
		`CREATE INDEX IF NOT EXISTS idx_saved_queries_created_by ON saved_queries(created_by)`,
	}
	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("saved queries v2 migration: %w", err)
		}
	}
	return nil
}
