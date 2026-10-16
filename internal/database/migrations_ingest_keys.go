package database

import "fmt"

// migrateIngestKeys adds the keys shippers send to /ingest. Each key belongs
// to one pipeline and goes with it. Only the SHA-256 of a key is stored.
// Idempotent: safe to run on every start.
func (db *DB) migrateIngestKeys() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS ingest_keys (
			id TEXT PRIMARY KEY,
			pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			key_hash TEXT NOT NULL UNIQUE,
			key_prefix TEXT NOT NULL,
			created_by TEXT NOT NULL DEFAULT '',
			created_at TEXT NOT NULL,
			last_used_at TEXT,
			revoked_at TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_ingest_keys_pipeline ON ingest_keys(pipeline_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("ingest keys migration: %w", err)
		}
	}
	return nil
}
