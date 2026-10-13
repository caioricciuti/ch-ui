package database

import "fmt"

// migrateAlertsV2 adds the tables behind built-in health checks and SQL
// threshold alerts. Idempotent: safe to run on every start.
func (db *DB) migrateAlertsV2() error {
	stmts := []struct{ name, sql string }{
		// Per-connection overrides of the built-in health check thresholds.
		// A missing row means the check runs with its defaults.
		{"health_alert_settings", `CREATE TABLE IF NOT EXISTS health_alert_settings (
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			check_kind TEXT NOT NULL,
			enabled INTEGER NOT NULL DEFAULT 1,
			warn_threshold REAL,
			crit_threshold REAL,
			for_evaluations INTEGER NOT NULL DEFAULT 2,
			resolve_evaluations INTEGER NOT NULL DEFAULT 2,
			updated_at TEXT,
			PRIMARY KEY (connection_id, check_kind)
		)`},
		// The state machine of each (connection, check, node).
		{"health_alert_state", `CREATE TABLE IF NOT EXISTS health_alert_state (
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			check_kind TEXT NOT NULL,
			node TEXT NOT NULL,
			state TEXT NOT NULL DEFAULT 'ok',
			severity TEXT,
			breach_count INTEGER NOT NULL DEFAULT 0,
			ok_count INTEGER NOT NULL DEFAULT 0,
			last_value REAL,
			since TEXT,
			last_event_id TEXT,
			updated_at TEXT,
			PRIMARY KEY (connection_id, check_kind, node)
		)`},
		{"sql_alerts", `CREATE TABLE IF NOT EXISTS sql_alerts (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			name TEXT NOT NULL,
			sql TEXT NOT NULL,
			mode TEXT NOT NULL DEFAULT 'value',
			comparator TEXT NOT NULL,
			threshold REAL NOT NULL DEFAULT 0,
			severity TEXT NOT NULL DEFAULT 'warn',
			interval_seconds INTEGER NOT NULL DEFAULT 300,
			timeout_seconds INTEGER NOT NULL DEFAULT 30,
			for_evaluations INTEGER NOT NULL DEFAULT 1,
			resolve_evaluations INTEGER NOT NULL DEFAULT 1,
			enabled INTEGER NOT NULL DEFAULT 1,
			state TEXT NOT NULL DEFAULT 'ok',
			breach_count INTEGER NOT NULL DEFAULT 0,
			ok_count INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0,
			last_run_at TEXT,
			last_value REAL,
			last_error TEXT,
			created_by TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`},
		{"idx_sql_alerts_conn", `CREATE INDEX IF NOT EXISTS idx_sql_alerts_conn ON sql_alerts(connection_id)`},
		// Evaluation history: state changes, errors and an hourly sample.
		{"alert_evaluations", `CREATE TABLE IF NOT EXISTS alert_evaluations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			source TEXT NOT NULL,
			source_id TEXT NOT NULL,
			connection_id TEXT NOT NULL,
			node TEXT NOT NULL DEFAULT '',
			value REAL,
			state TEXT NOT NULL,
			error TEXT NOT NULL DEFAULT '',
			duration_ms INTEGER NOT NULL DEFAULT 0,
			evaluated_at TEXT NOT NULL
		)`},
		{"idx_alert_eval_src", `CREATE INDEX IF NOT EXISTS idx_alert_eval_src ON alert_evaluations(source, source_id, evaluated_at)`},
	}
	for _, s := range stmts {
		if _, err := db.conn.Exec(s.sql); err != nil {
			return fmt.Errorf("create %s: %w", s.name, err)
		}
	}
	// Disk only: critical when the emptiest disk has less free space.
	if err := db.ensureColumn("health_alert_settings", "free_bytes_floor", "REAL"); err != nil {
		return err
	}
	return nil
}
