package database

import "fmt"

// migrateModelsV2 adds what models v2 needs: table options and incremental
// settings on models, data tests, test outcomes on run results, and named
// pipelines that replace per-anchor schedules. Every step is idempotent, so it
// runs on every start. model_schedules is left in place; its rows are copied
// into model_pipelines by the model scheduler on start (see
// models.MigrateLegacySchedules), which needs the dependency graph.
func (db *DB) migrateModelsV2() error {
	columns := []struct{ table, column, definition string }{
		{"models", "partition_by", "TEXT NOT NULL DEFAULT ''"},
		{"models", "engine_args", "TEXT NOT NULL DEFAULT ''"},
		{"models", "incremental_strategy", "TEXT NOT NULL DEFAULT ''"},
		{"models", "unique_key", "TEXT NOT NULL DEFAULT ''"},
		{"models", "watermark_column", "TEXT NOT NULL DEFAULT ''"},
		{"model_run_results", "tests_passed", "INTEGER NOT NULL DEFAULT 0"},
		{"model_run_results", "tests_failed", "INTEGER NOT NULL DEFAULT 0"},
		{"model_run_results", "test_details", "TEXT"},
		{"model_runs", "selector", "TEXT NOT NULL DEFAULT ''"},
		{"model_runs", "pipeline_id", "TEXT"},
	}
	for _, c := range columns {
		if err := db.ensureColumn(c.table, c.column, c.definition); err != nil {
			return fmt.Errorf("models v2: %w", err)
		}
	}

	stmts := []string{
		`CREATE TABLE IF NOT EXISTS model_tests (
			id TEXT PRIMARY KEY,
			model_id TEXT NOT NULL REFERENCES models(id) ON DELETE CASCADE,
			kind TEXT NOT NULL,
			column_name TEXT NOT NULL DEFAULT '',
			args TEXT NOT NULL DEFAULT '{}',
			severity TEXT NOT NULL DEFAULT 'error',
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_model_tests_model ON model_tests(model_id)`,

		`CREATE TABLE IF NOT EXISTS model_pipelines (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			selector TEXT NOT NULL,
			cron TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 0,
			timezone TEXT NOT NULL DEFAULT 'UTC',
			last_run_at TEXT,
			next_run_at TEXT,
			last_status TEXT,
			last_error TEXT,
			last_run_id TEXT,
			legacy_schedule_id TEXT UNIQUE,
			created_by TEXT,
			created_at TEXT DEFAULT CURRENT_TIMESTAMP,
			updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
			UNIQUE(connection_id, name)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_model_pipelines_conn ON model_pipelines(connection_id)`,
	}
	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("models v2: %w", err)
		}
	}
	return nil
}
