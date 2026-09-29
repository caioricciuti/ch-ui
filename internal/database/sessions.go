package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Session represents an authenticated session.
type Session struct {
	ID                string  `json:"id"`
	ConnectionID      string  `json:"connection_id"`
	ClickhouseUser    string  `json:"clickhouse_user"`
	EncryptedPassword string  `json:"encrypted_password"`
	Token             string  `json:"token"`
	ExpiresAt         string  `json:"expires_at"`
	UserRole          *string `json:"user_role"`
	// AuthSubject is the human identity behind the session (the OIDC email) when
	// it differs from ClickhouseUser. Empty for password logins, where the
	// ClickHouse user is the identity.
	AuthSubject *string `json:"auth_subject,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

// CreateSessionParams holds parameters for creating a session.
type CreateSessionParams struct {
	ConnectionID      string
	ClickhouseUser    string
	EncryptedPassword string
	Token             string
	ExpiresAt         string
	UserRole          string // defaults to "viewer" if empty
	AuthSubject       string // OIDC identity; empty for password logins
}

// SessionUser represents an aggregated user from sessions.
type SessionUser struct {
	// Username is the RoleKey: "sso:<email>" for an SSO person, else the
	// ClickHouse user. Role overrides are set and removed by this key.
	Username string `json:"username"`
	// DisplayName is the email for an SSO person, else the ClickHouse user.
	DisplayName string `json:"display_name"`
	// ClickhouseUser is the account the person queries as (the shared service
	// account for SSO).
	ClickhouseUser string `json:"clickhouse_user"`
	ViaSSO         bool   `json:"via_sso"`
	UserRole       string `json:"user_role"`
	LastLogin      string `json:"last_login"`
	SessionCount   int    `json:"session_count"`
}

// GetSession retrieves a session by token. Deletes and returns nil if expired.
func (db *DB) GetSession(token string) (*Session, error) {
	row := db.conn.QueryRow(
		"SELECT id, connection_id, clickhouse_user, encrypted_password, token, expires_at, user_role, auth_subject, created_at FROM sessions WHERE token = ?",
		token,
	)

	var s Session
	var userRole, authSubject sql.NullString

	err := row.Scan(
		&s.ID, &s.ConnectionID,
		&s.ClickhouseUser, &s.EncryptedPassword, &s.Token,
		&s.ExpiresAt, &userRole, &authSubject, &s.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get session: %w", err)
	}

	s.UserRole = nullStringToPtr(userRole)
	s.AuthSubject = nullStringToPtr(authSubject)

	// Check if session has expired
	expiresAt, err := time.Parse(time.RFC3339, s.ExpiresAt)
	if err != nil {
		expiresAt, err = time.Parse("2006-01-02T15:04:05.000Z", s.ExpiresAt)
		if err != nil {
			db.conn.Exec("DELETE FROM sessions WHERE id = ?", s.ID)
			return nil, nil
		}
	}

	if time.Now().UTC().After(expiresAt) {
		db.conn.Exec("DELETE FROM sessions WHERE id = ?", s.ID)
		return nil, nil
	}

	return &s, nil
}

// CreateSession creates a new session and returns its ID.
func (db *DB) CreateSession(params CreateSessionParams) (string, error) {
	id := uuid.NewString()

	userRole := params.UserRole
	if userRole == "" {
		userRole = "viewer"
	}

	var authSubject any
	if params.AuthSubject != "" {
		authSubject = params.AuthSubject
	}

	_, err := db.conn.Exec(
		`INSERT INTO sessions (id, connection_id, clickhouse_user, encrypted_password, token, expires_at, user_role, auth_subject)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, params.ConnectionID,
		params.ClickhouseUser, params.EncryptedPassword,
		params.Token, params.ExpiresAt, userRole, authSubject,
	)
	if err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return id, nil
}

// DeleteSession deletes a session by its token.
func (db *DB) DeleteSession(token string) error {
	_, err := db.conn.Exec("DELETE FROM sessions WHERE token = ?", token)
	if err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

// SetSessionsUserRole updates the cached app role for all sessions of the user
// identified by a RoleKey: the sessions of one SSO person for "sso:<email>",
// otherwise the password sessions of that ClickHouse user. A password user
// never touches SSO sessions that share its ClickHouse account.
func (db *DB) SetSessionsUserRole(roleKey, role string) error {
	if role == "" {
		role = "viewer"
	}
	var err error
	if subject, ok := strings.CutPrefix(roleKey, SSORoleKeyPrefix); ok {
		_, err = db.conn.Exec("UPDATE sessions SET user_role = ? WHERE auth_subject = ?", role, subject)
	} else {
		_, err = db.conn.Exec("UPDATE sessions SET user_role = ? WHERE clickhouse_user = ? AND (auth_subject IS NULL OR auth_subject = '')", role, roleKey)
	}
	if err != nil {
		return fmt.Errorf("set sessions user role: %w", err)
	}
	return nil
}

// GetUsers returns aggregated user data from sessions.
func (db *DB) GetUsers() ([]SessionUser, error) {
	// One row per person: SSO sessions group by email, password sessions by
	// ClickHouse user, so SSO people sharing a service account stay separate.
	rows, err := db.conn.Query(`
		SELECT
			CASE WHEN COALESCE(auth_subject, '') != '' THEN ? || auth_subject ELSE clickhouse_user END AS user_key,
			CASE WHEN COALESCE(auth_subject, '') != '' THEN auth_subject ELSE clickhouse_user END AS display_name,
			MAX(clickhouse_user),
			MAX(COALESCE(auth_subject, '') != ''),
			user_role,
			MAX(created_at) as last_login,
			COUNT(*) as session_count
		FROM sessions
		GROUP BY user_key
		ORDER BY last_login DESC
	`, SSORoleKeyPrefix)
	if err != nil {
		return nil, fmt.Errorf("get users: %w", err)
	}
	defer rows.Close()

	var users []SessionUser
	for rows.Next() {
		var u SessionUser
		var userRole sql.NullString
		if err := rows.Scan(&u.Username, &u.DisplayName, &u.ClickhouseUser, &u.ViaSSO, &userRole, &u.LastLogin, &u.SessionCount); err != nil {
			return nil, fmt.Errorf("scan user: %w", err)
		}
		if userRole.Valid {
			u.UserRole = userRole.String
		} else {
			u.UserRole = "viewer"
		}
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user rows: %w", err)
	}
	return users, nil
}

// GetActiveSessionsByConnection returns up to limit active sessions for a connection,
// ordered by most recently created first.
func (db *DB) GetActiveSessionsByConnection(connectionID string, limit int) ([]Session, error) {
	if limit <= 0 {
		limit = 5
	}
	now := time.Now().UTC().Format(time.RFC3339)

	rows, err := db.conn.Query(
		`SELECT id, connection_id, clickhouse_user, encrypted_password, token, expires_at, user_role, created_at
		 FROM sessions
		 WHERE connection_id = ? AND expires_at > ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		connectionID, now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get active sessions by connection: %w", err)
	}
	defer rows.Close()

	sessions := make([]Session, 0, limit)
	for rows.Next() {
		var s Session
		var role sql.NullString
		if err := rows.Scan(
			&s.ID, &s.ConnectionID,
			&s.ClickhouseUser, &s.EncryptedPassword, &s.Token,
			&s.ExpiresAt, &role, &s.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan active session: %w", err)
		}
		s.UserRole = nullStringToPtr(role)
		sessions = append(sessions, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active sessions: %w", err)
	}
	return sessions, nil
}
