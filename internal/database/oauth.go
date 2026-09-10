package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// OAuth 2.1 state for the MCP endpoint. CH-UI is its own authorization
// server: clients register (or bring a Client ID Metadata Document), the
// person approves on a consent page inside CH-UI, and the resulting access
// token is an mcp_keys row of kind "oauth" bound to that person's session
// credentials. Everything secret is stored hashed.

// OAuthClient is a registered public client (no secret).
type OAuthClient struct {
	ID           string   `json:"client_id"`
	Name         string   `json:"client_name"`
	RedirectURIs []string `json:"redirect_uris"`
	CreatedAt    string   `json:"created_at"`
}

func (db *DB) CreateOAuthClient(name string, redirectURIs []string) (*OAuthClient, error) {
	c := &OAuthClient{ID: uuid.NewString(), Name: name, RedirectURIs: redirectURIs, CreatedAt: time.Now().UTC().Format(time.RFC3339)}
	uris, _ := json.Marshal(redirectURIs)
	if _, err := db.conn.Exec(`INSERT INTO oauth_clients (id, name, redirect_uris, created_at) VALUES (?, ?, ?, ?)`, c.ID, c.Name, string(uris), c.CreatedAt); err != nil {
		return nil, fmt.Errorf("create oauth client: %w", err)
	}
	return c, nil
}

func (db *DB) GetOAuthClient(id string) (*OAuthClient, error) {
	var c OAuthClient
	var uris string
	err := db.conn.QueryRow(`SELECT id, name, redirect_uris, created_at FROM oauth_clients WHERE id = ?`, id).Scan(&c.ID, &c.Name, &uris, &c.CreatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth client: %w", err)
	}
	_ = json.Unmarshal([]byte(uris), &c.RedirectURIs)
	return &c, nil
}

// OAuthRequest is a validated authorization request waiting for consent.
type OAuthRequest struct {
	ID            string `json:"id"`
	ClientID      string `json:"client_id"`
	ClientName    string `json:"client_name"`
	RedirectURI   string `json:"redirect_uri"`
	Scope         string `json:"scope"`
	State         string `json:"state"`
	CodeChallenge string `json:"-"`
	ExpiresAt     string `json:"expires_at"`
}

func (db *DB) CreateOAuthRequest(r *OAuthRequest) error {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	_, err := db.conn.Exec(`INSERT INTO oauth_requests (id, client_id, client_name, redirect_uri, scope, state, code_challenge, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.ClientID, r.ClientName, r.RedirectURI, r.Scope, r.State, r.CodeChallenge, r.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create oauth request: %w", err)
	}
	return nil
}

func (db *DB) GetOAuthRequest(id string) (*OAuthRequest, error) {
	var r OAuthRequest
	err := db.conn.QueryRow(`SELECT id, client_id, client_name, redirect_uri, scope, state, code_challenge, expires_at FROM oauth_requests WHERE id = ?`, id).
		Scan(&r.ID, &r.ClientID, &r.ClientName, &r.RedirectURI, &r.Scope, &r.State, &r.CodeChallenge, &r.ExpiresAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get oauth request: %w", err)
	}
	return &r, nil
}

func (db *DB) DeleteOAuthRequest(id string) error {
	_, err := db.conn.Exec(`DELETE FROM oauth_requests WHERE id = ?`, id)
	return err
}

// OAuthCode is an issued authorization code with the identity it grants.
type OAuthCode struct {
	CodeHash      string
	ClientID      string
	ClientName    string
	RedirectURI   string
	Scope         string
	CodeChallenge string
	ConnectionID  string
	CHUser        string
	CHPasswordEnc string
	Subject       string
	ExpiresAt     string
	UsedAt        *string
}

func (db *DB) CreateOAuthCode(c *OAuthCode) error {
	_, err := db.conn.Exec(`INSERT INTO oauth_codes (code_hash, client_id, client_name, redirect_uri, scope, code_challenge, connection_id, ch_user, ch_password_enc, subject, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		c.CodeHash, c.ClientID, c.ClientName, c.RedirectURI, c.Scope, c.CodeChallenge, c.ConnectionID, c.CHUser, c.CHPasswordEnc, c.Subject, c.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create oauth code: %w", err)
	}
	return nil
}

// ConsumeOAuthCode marks a code used and returns it. A code that was already
// used returns nil, and the caller must treat that as an attack (RFC 6749
// says to revoke tokens issued from it; we never issue twice, so nothing to
// revoke).
func (db *DB) ConsumeOAuthCode(codeHash string) (*OAuthCode, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var c OAuthCode
	err = tx.QueryRow(`SELECT code_hash, client_id, client_name, redirect_uri, scope, code_challenge, connection_id, ch_user, ch_password_enc, subject, expires_at, used_at FROM oauth_codes WHERE code_hash = ?`, codeHash).
		Scan(&c.CodeHash, &c.ClientID, &c.ClientName, &c.RedirectURI, &c.Scope, &c.CodeChallenge, &c.ConnectionID, &c.CHUser, &c.CHPasswordEnc, &c.Subject, &c.ExpiresAt, &c.UsedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("consume oauth code: %w", err)
	}
	if c.UsedAt != nil {
		return nil, nil
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE oauth_codes SET used_at = ? WHERE code_hash = ?`, now, codeHash); err != nil {
		return nil, fmt.Errorf("consume oauth code: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &c, nil
}

// OAuthRefreshToken links a refresh token to the access-token key it renews.
type OAuthRefreshToken struct {
	TokenHash string
	KeyID     string
	ClientID  string
	Scope     string
	ExpiresAt string
	RevokedAt *string
}

func (db *DB) CreateOAuthRefreshToken(t *OAuthRefreshToken) error {
	_, err := db.conn.Exec(`INSERT INTO oauth_refresh_tokens (token_hash, key_id, client_id, scope, expires_at) VALUES (?, ?, ?, ?, ?)`,
		t.TokenHash, t.KeyID, t.ClientID, t.Scope, t.ExpiresAt)
	if err != nil {
		return fmt.Errorf("create refresh token: %w", err)
	}
	return nil
}

func (db *DB) GetOAuthRefreshToken(tokenHash string) (*OAuthRefreshToken, error) {
	var t OAuthRefreshToken
	err := db.conn.QueryRow(`SELECT token_hash, key_id, client_id, scope, expires_at, revoked_at FROM oauth_refresh_tokens WHERE token_hash = ?`, tokenHash).
		Scan(&t.TokenHash, &t.KeyID, &t.ClientID, &t.Scope, &t.ExpiresAt, &t.RevokedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get refresh token: %w", err)
	}
	return &t, nil
}

func (db *DB) RevokeOAuthRefreshToken(tokenHash string) error {
	_, err := db.conn.Exec(`UPDATE oauth_refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND revoked_at IS NULL`, time.Now().UTC().Format(time.RFC3339), tokenHash)
	return err
}

// PurgeExpiredOAuthState drops stale requests, codes and refresh tokens.
// Cheap enough to run on every token request.
func (db *DB) PurgeExpiredOAuthState() {
	now := time.Now().UTC().Format(time.RFC3339)
	db.conn.Exec(`DELETE FROM oauth_requests WHERE expires_at < ?`, now)
	db.conn.Exec(`DELETE FROM oauth_codes WHERE expires_at < ?`, now)
	db.conn.Exec(`DELETE FROM oauth_refresh_tokens WHERE expires_at < ? OR revoked_at IS NOT NULL AND revoked_at < ?`, now, time.Now().UTC().Add(-24*time.Hour).Format(time.RFC3339))
}
