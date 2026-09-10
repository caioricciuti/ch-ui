package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// MCPKey is a bearer credential for the embedded MCP server. The plaintext key
// is shown once at creation; only its SHA-256 hex digest is stored.
type MCPKey struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	KeyHash      string `json:"-"`
	KeyPrefix    string `json:"key_prefix"`
	ConnectionID string `json:"connection_id"`
	// CHUser / CHPasswordEnc are the ClickHouse credentials the key executes
	// with. The password is AES-encrypted with the app secret key; the
	// ClickHouse user's grants are the real permission boundary.
	CHUser        string `json:"ch_user"`
	CHPasswordEnc string `json:"-"`
	// Scopes is 'read' or 'read_write'. Write tools (create saved queries,
	// dashboards, models, pipelines) are registered only for read_write keys.
	Scopes string `json:"scopes"`
	// AllowedDatabases is a comma-separated list of database names the key may
	// touch. Empty means all databases the ClickHouse user can see.
	AllowedDatabases string `json:"allowed_databases"`
	CreatedBy        string `json:"created_by"`
	CreatedAt        string `json:"created_at"`
	// ExpiresAt is an RFC 3339 instant after which the key is rejected; nil
	// means the key never expires.
	ExpiresAt  *string `json:"expires_at"`
	LastUsedAt *string `json:"last_used_at"`
	RevokedAt  *string `json:"revoked_at"`
	// Kind is "api" for admin-created keys and "oauth" for access tokens
	// minted by the OAuth flow; Subject is the person who granted an OAuth
	// token, ClientID the OAuth client it was issued to.
	Kind     string `json:"kind"`
	Subject  string `json:"subject"`
	ClientID string `json:"client_id"`
}

// Expired reports whether the key's expiry, if any, has passed.
func (k *MCPKey) Expired(now time.Time) bool {
	if k.ExpiresAt == nil || *k.ExpiresAt == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, *k.ExpiresAt)
	if err != nil {
		return true // unparseable expiry fails closed
	}
	return !now.Before(t)
}

// CreateMCPKeyParams is the input of CreateMCPKey.
type CreateMCPKeyParams struct {
	Name             string
	KeyHash          string
	KeyPrefix        string
	ConnectionID     string
	CHUser           string
	CHPasswordEnc    string
	Scopes           string // "read" or "read_write"
	AllowedDatabases string
	CreatedBy        string
	ExpiresAt        *string // RFC 3339, nil = never
	Kind             string  // "" or "api" (default), or "oauth"
	Subject          string
	ClientID         string
}

func (db *DB) CreateMCPKey(p CreateMCPKeyParams) (*MCPKey, error) {
	if p.Scopes != "read_write" {
		p.Scopes = "read"
	}
	if p.Kind == "" {
		p.Kind = "api"
	}
	k := &MCPKey{
		ID:               uuid.New().String(),
		Name:             p.Name,
		KeyHash:          p.KeyHash,
		KeyPrefix:        p.KeyPrefix,
		ConnectionID:     p.ConnectionID,
		CHUser:           p.CHUser,
		CHPasswordEnc:    p.CHPasswordEnc,
		Scopes:           p.Scopes,
		AllowedDatabases: p.AllowedDatabases,
		CreatedBy:        p.CreatedBy,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
		ExpiresAt:        p.ExpiresAt,
		Kind:             p.Kind,
		Subject:          p.Subject,
		ClientID:         p.ClientID,
	}
	_, err := db.conn.Exec(
		`INSERT INTO mcp_keys (id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at, expires_at, kind, subject, client_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.KeyHash, k.KeyPrefix, k.ConnectionID, k.CHUser, k.CHPasswordEnc, k.Scopes, k.AllowedDatabases, k.CreatedBy, k.CreatedAt, k.ExpiresAt, k.Kind, k.Subject, k.ClientID,
	)
	if err != nil {
		return nil, fmt.Errorf("create mcp key: %w", err)
	}
	return k, nil
}

// RotateMCPKey issues a new key with the same binding (name, connection,
// ClickHouse credentials, scope, allowlist, expiry) and revokes the old one,
// atomically. Clients switch to the new secret; the old one stops working
// the moment this returns.
func (db *DB) RotateMCPKey(id, newHash, newPrefix, rotatedBy string) (*MCPKey, error) {
	old, err := db.GetMCPKeyByID(id)
	if err != nil {
		return nil, err
	}
	if old == nil || old.RevokedAt != nil {
		return nil, fmt.Errorf("mcp key not found or already revoked")
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("rotate mcp key: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)
	k := &MCPKey{
		ID:               uuid.New().String(),
		Name:             old.Name,
		KeyHash:          newHash,
		KeyPrefix:        newPrefix,
		ConnectionID:     old.ConnectionID,
		CHUser:           old.CHUser,
		CHPasswordEnc:    old.CHPasswordEnc,
		Scopes:           old.Scopes,
		AllowedDatabases: old.AllowedDatabases,
		CreatedBy:        rotatedBy,
		CreatedAt:        now,
		ExpiresAt:        old.ExpiresAt,
		Kind:             old.Kind,
		Subject:          old.Subject,
		ClientID:         old.ClientID,
	}
	if _, err := tx.Exec(
		`INSERT INTO mcp_keys (id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at, expires_at, kind, subject, client_id)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.KeyHash, k.KeyPrefix, k.ConnectionID, k.CHUser, k.CHPasswordEnc, k.Scopes, k.AllowedDatabases, k.CreatedBy, k.CreatedAt, k.ExpiresAt, k.Kind, k.Subject, k.ClientID,
	); err != nil {
		return nil, fmt.Errorf("rotate mcp key: insert: %w", err)
	}
	if _, err := tx.Exec(`UPDATE mcp_keys SET revoked_at = ? WHERE id = ?`, now, id); err != nil {
		return nil, fmt.Errorf("rotate mcp key: revoke old: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("rotate mcp key: %w", err)
	}
	return k, nil
}

// GetMCPKeyByID returns a key by id regardless of revocation, or nil.
func (db *DB) GetMCPKeyByID(id string) (*MCPKey, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at, expires_at, last_used_at, revoked_at, kind, subject, client_id
		 FROM mcp_keys WHERE id = ?`, id,
	)
	return scanMCPKey(row)
}

// GetMCPKeyByHash returns the active (non-revoked) key matching the hash, or
// nil when no such key exists.
func (db *DB) GetMCPKeyByHash(keyHash string) (*MCPKey, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at, expires_at, last_used_at, revoked_at, kind, subject, client_id
		 FROM mcp_keys WHERE key_hash = ? AND revoked_at IS NULL`, keyHash,
	)
	return scanMCPKey(row)
}

func (db *DB) ListMCPKeys() ([]*MCPKey, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at, expires_at, last_used_at, revoked_at, kind, subject, client_id
		 FROM mcp_keys ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list mcp keys: %w", err)
	}
	defer rows.Close()

	var keys []*MCPKey
	for rows.Next() {
		k, err := scanMCPKey(rows)
		if err != nil {
			return nil, err
		}
		if k != nil {
			keys = append(keys, k)
		}
	}
	return keys, rows.Err()
}

func (db *DB) RevokeMCPKey(id string) error {
	res, err := db.conn.Exec(
		`UPDATE mcp_keys SET revoked_at = ? WHERE id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("revoke mcp key: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("mcp key not found or already revoked")
	}
	return nil
}

// TouchMCPKey records key usage. Best-effort: errors are ignored by callers.
func (db *DB) TouchMCPKey(id string) error {
	_, err := db.conn.Exec(
		`UPDATE mcp_keys SET last_used_at = ? WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	return err
}

type rowScanner interface{ Scan(dest ...any) error }

func scanMCPKey(row rowScanner) (*MCPKey, error) {
	var k MCPKey
	err := row.Scan(&k.ID, &k.Name, &k.KeyHash, &k.KeyPrefix, &k.ConnectionID,
		&k.CHUser, &k.CHPasswordEnc, &k.Scopes, &k.AllowedDatabases, &k.CreatedBy, &k.CreatedAt, &k.ExpiresAt, &k.LastUsedAt, &k.RevokedAt, &k.Kind, &k.Subject, &k.ClientID)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan mcp key: %w", err)
	}
	return &k, nil
}
