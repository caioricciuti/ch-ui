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
	AllowedDatabases string  `json:"allowed_databases"`
	CreatedBy        string  `json:"created_by"`
	CreatedAt        string  `json:"created_at"`
	LastUsedAt       *string `json:"last_used_at"`
	RevokedAt        *string `json:"revoked_at"`
}

func (db *DB) CreateMCPKey(name, keyHash, keyPrefix, connectionID, chUser, chPasswordEnc, scopes, allowedDatabases, createdBy string) (*MCPKey, error) {
	if scopes != "read_write" {
		scopes = "read"
	}
	k := &MCPKey{
		ID:               uuid.New().String(),
		Name:             name,
		KeyHash:          keyHash,
		KeyPrefix:        keyPrefix,
		ConnectionID:     connectionID,
		CHUser:           chUser,
		CHPasswordEnc:    chPasswordEnc,
		Scopes:           scopes,
		AllowedDatabases: allowedDatabases,
		CreatedBy:        createdBy,
		CreatedAt:        time.Now().UTC().Format(time.RFC3339),
	}
	_, err := db.conn.Exec(
		`INSERT INTO mcp_keys (id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		k.ID, k.Name, k.KeyHash, k.KeyPrefix, k.ConnectionID, k.CHUser, k.CHPasswordEnc, k.Scopes, k.AllowedDatabases, k.CreatedBy, k.CreatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("create mcp key: %w", err)
	}
	return k, nil
}

// GetMCPKeyByHash returns the active (non-revoked) key matching the hash, or
// nil when no such key exists.
func (db *DB) GetMCPKeyByHash(keyHash string) (*MCPKey, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at, last_used_at, revoked_at
		 FROM mcp_keys WHERE key_hash = ? AND revoked_at IS NULL`, keyHash,
	)
	return scanMCPKey(row)
}

func (db *DB) ListMCPKeys() ([]*MCPKey, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, key_hash, key_prefix, connection_id, ch_user, ch_password_enc, scopes, allowed_databases, created_by, created_at, last_used_at, revoked_at
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
		&k.CHUser, &k.CHPasswordEnc, &k.Scopes, &k.AllowedDatabases, &k.CreatedBy, &k.CreatedAt, &k.LastUsedAt, &k.RevokedAt)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("scan mcp key: %w", err)
	}
	return &k, nil
}
