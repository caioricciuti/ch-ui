package database

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
)

// userRolesV2Table is the scoped user_roles schema. A ClickHouse user's
// override applies on one connection; an SSO person's ("sso:<email>") on
// every connection, stored with an empty connection_id. The CHECK keeps the two
// apart, so a ClickHouse user can never hold a global override.
const userRolesV2Table = `CREATE TABLE IF NOT EXISTS user_roles (
	username TEXT NOT NULL,
	connection_id TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL DEFAULT 'viewer',
	created_at TEXT DEFAULT CURRENT_TIMESTAMP,
	PRIMARY KEY (username, connection_id),
	CHECK ((substr(username, 1, 4) = 'sso:') = (connection_id = ''))
)`

// userRolesConnectionTrigger drops a connection's overrides with it.
// connection_id has no foreign key because SSO rows store it empty.
const userRolesConnectionTrigger = `CREATE TRIGGER IF NOT EXISTS user_roles_connection_deleted
	AFTER DELETE ON connections
	BEGIN
		DELETE FROM user_roles WHERE connection_id = OLD.id;
	END`

// migrateUserRolesV2 scopes role overrides for ClickHouse users to a
// connection. Before, an override was keyed on the username alone, so the
// same username on any connection, including one created from the login page
// against a server anyone can run, got it.
//
// Each existing ClickHouse user override is copied to every connection that
// was not created from the login page, so nobody loses access on upgrade.
// SSO overrides stay global. Cached admin roles on login page connections
// drop to analyst, and password sessions whose username could pass for an SSO
// key are signed out. Idempotent: a no-op once connection_id exists, and the
// whole rebuild is one transaction.
func (db *DB) migrateUserRolesV2() error {
	return db.inImmediateTx(func(ctx context.Context, q sqlRunner) error {
		scoped, err := columnExistsOn(ctx, q, "user_roles", "connection_id")
		if err != nil {
			return err
		}
		if !scoped {
			if err := rebuildUserRolesV2(ctx, q); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx, userRolesConnectionTrigger); err != nil {
			return fmt.Errorf("user roles v2: create trigger: %w", err)
		}
		return nil
	})
}

// rebuildUserRolesV2 moves the username-keyed table to the scoped schema.
func rebuildUserRolesV2(ctx context.Context, q sqlRunner) error {
	// The trigger names user_roles, which blocks the rename; it is
	// created again after the rebuild.
	if _, err := q.ExecContext(ctx, "DROP TRIGGER IF EXISTS user_roles_connection_deleted"); err != nil {
		return fmt.Errorf("user roles v2: drop trigger: %w", err)
	}

	var raw string
	if err := q.QueryRowContext(ctx, "SELECT COALESCE((SELECT value FROM settings WHERE key = ?), '')", SettingLoginURLConnectionIDs).Scan(&raw); err != nil {
		return fmt.Errorf("user roles v2: read login url connections: %w", err)
	}
	loginURLIDs, err := parseLoginURLConnectionIDs(raw)
	copyClickHouseOverrides := err == nil
	if err != nil {
		// Which connections are safe is unknown, so copy no ClickHouse
		// user override rather than guess. "ch-ui reset-admin --user"
		// restores an admin.
		slog.Warn("Login URL connection list is unreadable; ClickHouse user role overrides are not carried over", "error", err)
	}

	slog.Info("Migrating user_roles to per-connection role overrides")

	if _, err := q.ExecContext(ctx, "ALTER TABLE user_roles RENAME TO user_roles_old"); err != nil {
		return fmt.Errorf("user roles v2: rename old table: %w", err)
	}
	if _, err := q.ExecContext(ctx, userRolesV2Table); err != nil {
		return fmt.Errorf("user roles v2: create table: %w", err)
	}
	if _, err := q.ExecContext(ctx, `INSERT INTO user_roles (username, connection_id, role, created_at)
		SELECT username, '', role, created_at FROM user_roles_old
		WHERE substr(username, 1, 4) = 'sso:'`); err != nil {
		return fmt.Errorf("user roles v2: copy sso overrides: %w", err)
	}

	exclude := ""
	args := make([]any, 0, len(loginURLIDs))
	if len(loginURLIDs) > 0 {
		exclude = " AND c.id NOT IN (?" + strings.Repeat(",?", len(loginURLIDs)-1) + ")"
		for _, id := range loginURLIDs {
			args = append(args, id)
		}
	}
	if copyClickHouseOverrides {
		if _, err := q.ExecContext(ctx, `INSERT INTO user_roles (username, connection_id, role, created_at)
			SELECT o.username, c.id, o.role, o.created_at
			FROM user_roles_old o CROSS JOIN connections c
			WHERE substr(o.username, 1, 4) != 'sso:'`+exclude, args...); err != nil {
			return fmt.Errorf("user roles v2: copy clickhouse user overrides: %w", err)
		}
	}
	if err := failpoint("user_roles: copied"); err != nil {
		return err
	}
	if _, err := q.ExecContext(ctx, "DROP TABLE user_roles_old"); err != nil {
		return fmt.Errorf("user roles v2: drop old table: %w", err)
	}

	if len(loginURLIDs) > 0 {
		in := "(?" + strings.Repeat(",?", len(loginURLIDs)-1) + ")"
		if _, err := q.ExecContext(ctx, "UPDATE sessions SET user_role = 'analyst' WHERE user_role = 'admin' AND connection_id IN "+in, args...); err != nil {
			return fmt.Errorf("user roles v2: cap login url sessions: %w", err)
		}
	}
	if _, err := q.ExecContext(ctx, `DELETE FROM sessions
		WHERE COALESCE(auth_subject, '') = '' AND lower(substr(clickhouse_user, 1, 4)) = 'sso:'`); err != nil {
		return fmt.Errorf("user roles v2: drop reserved name sessions: %w", err)
	}

	slog.Info("user_roles migration complete")
	return nil
}
