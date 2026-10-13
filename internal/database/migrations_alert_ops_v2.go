package database

import "fmt"

// migrateAlertOpsV2 adds alert silences, incident acknowledgements, the
// suppression marker on events and connection-scoped rules. Idempotent: safe
// to run on every start.
func (db *DB) migrateAlertOpsV2() error {
	stmts := []struct{ name, sql string }{
		// A silence holds back the events its matchers select while its
		// window is open. Every matcher that is set must match.
		{"alert_silences", `CREATE TABLE IF NOT EXISTS alert_silences (
			id TEXT PRIMARY KEY,
			reason TEXT NOT NULL,
			connection_id TEXT REFERENCES connections(id) ON DELETE CASCADE,
			event_type TEXT,
			source_ref TEXT,
			node TEXT,
			starts_at TEXT NOT NULL,
			ends_at TEXT NOT NULL,
			created_by TEXT,
			created_at TEXT NOT NULL,
			expired_at TEXT,
			expired_by TEXT
		)`},
		{"idx_alert_silences_window", `CREATE INDEX IF NOT EXISTS idx_alert_silences_window ON alert_silences(ends_at, starts_at)`},
		// An acknowledgement mutes the rest of one incident of one alert.
		{"alert_acks", `CREATE TABLE IF NOT EXISTS alert_acks (
			id TEXT PRIMARY KEY,
			alert_key TEXT NOT NULL,
			incident_id TEXT,
			event_id TEXT REFERENCES alert_events(id) ON DELETE SET NULL,
			connection_id TEXT,
			severity TEXT,
			comment TEXT,
			acked_by TEXT,
			created_at TEXT NOT NULL,
			expires_at TEXT,
			cleared_at TEXT,
			cleared_reason TEXT
		)`},
		{"idx_alert_acks_open", `CREATE UNIQUE INDEX IF NOT EXISTS idx_alert_acks_open ON alert_acks(alert_key) WHERE cleared_at IS NULL`},
	}
	for _, s := range stmts {
		if _, err := db.conn.Exec(s.sql); err != nil {
			return fmt.Errorf("create %s: %w", s.name, err)
		}
	}
	// 'silence:<id>' or 'ack:<id>' on an event that was held back.
	if err := db.ensureColumn("alert_events", "suppressed_by", "TEXT"); err != nil {
		return err
	}
	// JSON array of connection ids a rule is limited to; NULL means all.
	if err := db.ensureColumn("alert_rules", "connection_ids", "TEXT"); err != nil {
		return err
	}
	return nil
}
