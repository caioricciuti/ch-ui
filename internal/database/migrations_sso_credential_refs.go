package database

import "fmt"

// migrateSSOCredentialRefs lets dashboard shares, OAuth codes and the MCP
// keys minted from them reference the per-person ClickHouse user of an SSO
// person instead of copying their password.
//
// Per-person passwords rotate at every sign-in, so a copy goes stale the
// next time the person logs in. A row with sso_identity_id set keeps its
// encrypted password column empty and resolves the live
// sso_clickhouse_users row at query time; when that row is locked or gone
// the share or token stops working with it.
//
// Idempotent: safe to run on every start.
func (db *DB) migrateSSOCredentialRefs() error {
	for _, table := range []string{"dashboard_shares", "oauth_codes", "mcp_keys"} {
		if err := db.ensureColumn(table, "sso_identity_id", "TEXT"); err != nil {
			return fmt.Errorf("sso credential refs migration: %w", err)
		}
	}
	return nil
}
