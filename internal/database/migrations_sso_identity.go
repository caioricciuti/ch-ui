package database

import "fmt"

// migrateSSOIdentity adds per-person ClickHouse users for SSO sign-ins.
//
// sso_identities is the person as the identity provider knows them, keyed by
// (issuer, subject) so an email change at the IdP does not create a second
// person. sso_clickhouse_users is the ClickHouse user CH-UI created for that
// person on one connection, with the password it generated, encrypted with
// the app secret key, and the roles it granted. connections gets the
// per-connection JSON config (mode, prefix, group to role mapping); sessions
// learn which identity they belong to so a connection switch can provision
// the person on the target without a fresh ID token.
//
// Idempotent: safe to run on every start.
func (db *DB) migrateSSOIdentity() error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS sso_identities (
			id TEXT PRIMARY KEY,
			issuer TEXT NOT NULL,
			subject TEXT NOT NULL,
			email TEXT NOT NULL,
			display_name TEXT NOT NULL DEFAULT '',
			groups TEXT NOT NULL DEFAULT '[]',
			created_at TEXT NOT NULL,
			last_login_at TEXT NOT NULL,
			UNIQUE (issuer, subject)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sso_identities_email ON sso_identities(email)`,
		`CREATE TABLE IF NOT EXISTS sso_clickhouse_users (
			identity_id TEXT NOT NULL REFERENCES sso_identities(id) ON DELETE CASCADE,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			ch_user TEXT NOT NULL,
			encrypted_password TEXT NOT NULL,
			roles TEXT NOT NULL DEFAULT '[]',
			last_sync_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			locked_at TEXT,
			PRIMARY KEY (identity_id, connection_id),
			UNIQUE (connection_id, ch_user)
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.conn.Exec(stmt); err != nil {
			return fmt.Errorf("sso identity migration: %w", err)
		}
	}
	if err := db.ensureColumn("connections", "sso_identity_config", "TEXT"); err != nil {
		return fmt.Errorf("sso identity migration: %w", err)
	}
	if err := db.ensureColumn("sessions", "sso_identity_id", "TEXT"); err != nil {
		return fmt.Errorf("sso identity migration: %w", err)
	}
	return nil
}
