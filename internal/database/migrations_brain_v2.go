package database

import "fmt"

// migrateBrainV2 adds the Brain token and cost ledger: usage columns on
// brain_messages and a brain_usage table that records every provider call
// (chat turns and Ask AI). Idempotent: safe to run on every start.
func (db *DB) migrateBrainV2() error {
	columns := []struct{ name, def string }{
		{"input_tokens", "INTEGER NOT NULL DEFAULT 0"},
		{"output_tokens", "INTEGER NOT NULL DEFAULT 0"},
		{"cache_read_tokens", "INTEGER NOT NULL DEFAULT 0"},
		{"cache_write_tokens", "INTEGER NOT NULL DEFAULT 0"},
		{"model", "TEXT"},
		{"provider", "TEXT"},
	}
	for _, c := range columns {
		if err := db.ensureColumn("brain_messages", c.name, c.def); err != nil {
			return fmt.Errorf("brain v2: %w", err)
		}
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS brain_usage (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL,
			provider TEXT NOT NULL DEFAULT '',
			model TEXT NOT NULL DEFAULT '',
			purpose TEXT NOT NULL,
			chat_id TEXT,
			message_id TEXT,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			cache_read_tokens INTEGER NOT NULL DEFAULT 0,
			cache_write_tokens INTEGER NOT NULL DEFAULT 0,
			created_at TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_usage_time ON brain_usage(created_at)`,
		`CREATE INDEX IF NOT EXISTS idx_brain_usage_user_time ON brain_usage(username, created_at)`,
	}
	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("brain v2: %w", err)
		}
	}
	return nil
}
