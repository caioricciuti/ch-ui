package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ConnectionReset counts what RepointConnectionServer cleared from a
// connection that now points at a different ClickHouse server.
type ConnectionReset struct {
	Sessions           int64 `json:"sessions"`
	RoleOverrides      int64 `json:"role_overrides"`
	SSOUsers           int64 `json:"sso_users"`
	SSOAccount         bool  `json:"sso_account"`
	BackgroundAccounts int64 `json:"background_accounts"`
	MCPKeys            int64 `json:"mcp_keys"`
	DashboardShares    int64 `json:"dashboard_shares"`
}

// Summary is the audit line for the reset.
func (r ConnectionReset) Summary() string {
	parts := []string{
		fmt.Sprintf("%d session(s) signed out", r.Sessions),
		fmt.Sprintf("%d role override(s) removed", r.RoleOverrides),
		fmt.Sprintf("%d per-person SSO user record(s) forgotten", r.SSOUsers),
		fmt.Sprintf("%d background account(s) disabled", r.BackgroundAccounts),
		fmt.Sprintf("%d MCP key(s) and OAuth token(s) revoked", r.MCPKeys),
		fmt.Sprintf("%d dashboard share link(s) deleted", r.DashboardShares),
	}
	if r.SSOAccount {
		parts = append(parts, "SSO account cleared")
	}
	return strings.Join(parts, ", ")
}

// RepointConnectionServer sets a connection's ClickHouse URL to a different
// server and, in the same transaction, clears everything that belongs to
// the old server: sessions (their stored passwords), ClickHouse user role
// overrides, per-person SSO user records, the SSO account, background
// service accounts, MCP keys and OAuth tokens, and dashboard share links.
// Nothing is sent to either server: per-person SSO users are forgotten, not
// dropped, since the sweep must never act on the new server for them.
//
// Background service accounts become "disabled" rather than deleted, so a
// worker an admin moved off borrowed sessions does not fall back to them.
// The SSO identity settings (mode, prefix, group map) are kept; they hold
// no credential.
func (db *DB) RepointConnectionServer(id, url string) (*ConnectionReset, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("repoint connection: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	res, err := tx.Exec("UPDATE connections SET clickhouse_url = ? WHERE id = ?", url, id)
	if err != nil {
		return nil, fmt.Errorf("repoint connection: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return nil, sql.ErrNoRows
	}

	reset := &ConnectionReset{}
	steps := []struct {
		name  string
		query string
		args  []any
		count *int64
	}{
		{"sessions", "DELETE FROM sessions WHERE connection_id = ?", []any{id}, &reset.Sessions},
		{"role overrides", "DELETE FROM user_roles WHERE connection_id = ?", []any{id}, &reset.RoleOverrides},
		{"sso users", "DELETE FROM sso_clickhouse_users WHERE connection_id = ?", []any{id}, &reset.SSOUsers},
		{"background accounts", `UPDATE background_credentials SET mode = 'disabled', username = '', encrypted_password = '', updated_at = ?
			WHERE connection_id = ? AND mode = 'service_account'`, []any{now, id}, &reset.BackgroundAccounts},
		{"refresh tokens", `UPDATE oauth_refresh_tokens SET revoked_at = ?
			WHERE revoked_at IS NULL AND key_id IN (SELECT id FROM mcp_keys WHERE connection_id = ?)`, []any{now, id}, nil},
		{"mcp keys", "UPDATE mcp_keys SET revoked_at = ? WHERE connection_id = ? AND revoked_at IS NULL", []any{now, id}, &reset.MCPKeys},
		{"oauth codes", "DELETE FROM oauth_codes WHERE connection_id = ?", []any{id}, nil},
		{"dashboard shares", "DELETE FROM dashboard_shares WHERE connection_id = ?", []any{id}, &reset.DashboardShares},
	}
	for _, s := range steps {
		res, err := tx.Exec(s.query, s.args...)
		if err != nil {
			return nil, fmt.Errorf("repoint connection: clear %s: %w", s.name, err)
		}
		if s.count != nil {
			*s.count, _ = res.RowsAffected()
		}
	}

	var account sql.NullString
	if err := tx.QueryRow("SELECT sso_ch_user FROM connections WHERE id = ?", id).Scan(&account); err != nil {
		return nil, fmt.Errorf("repoint connection: read sso account: %w", err)
	}
	if account.String != "" {
		if _, err := tx.Exec("UPDATE connections SET sso_ch_user = NULL, sso_ch_password_encrypted = NULL WHERE id = ?", id); err != nil {
			return nil, fmt.Errorf("repoint connection: clear sso account: %w", err)
		}
		reset.SSOAccount = true
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("repoint connection: %w", err)
	}
	return reset, nil
}
