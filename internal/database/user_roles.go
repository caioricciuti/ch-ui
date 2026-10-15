package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
)

// UserRole represents a CH-UI role assignment.
//
// An override for a ClickHouse user (password login) applies on one
// connection only: the same username on another server is a different
// account. An override for an SSO person ("sso:<email>") applies on every
// connection, since the IdP verified who they are; its ConnectionID is "".
type UserRole struct {
	Username       string `json:"username"`
	ConnectionID   string `json:"connection_id"`
	ConnectionName string `json:"connection_name,omitempty"`
	Role           string `json:"role"`
	CreatedAt      string `json:"created_at"`
}

// SSORoleKeyPrefix namespaces role overrides for people who sign in with SSO.
// SSO people share one ClickHouse service account, so their overrides are keyed
// on their email instead, and the prefix keeps an email from ever matching a
// ClickHouse username. Password logins with a username starting with it are
// refused (see IsReservedRoleUsername).
const SSORoleKeyPrefix = "sso:"

// RoleKey returns the user_roles key for a session identity: "sso:<email>" for
// an SSO person, the ClickHouse user for a password login.
func RoleKey(clickhouseUser, authSubject string) string {
	if authSubject != "" {
		return SSORoleKeyPrefix + authSubject
	}
	return clickhouseUser
}

// IsSSORoleKey reports whether a role key names an SSO person.
func IsSSORoleKey(key string) bool {
	return strings.HasPrefix(key, SSORoleKeyPrefix)
}

// IsReservedRoleUsername reports whether a ClickHouse username could be
// mistaken for an SSO role key. Such names cannot sign in with a password.
func IsReservedRoleUsername(username string) bool {
	return strings.HasPrefix(strings.ToLower(username), SSORoleKeyPrefix)
}

// CapLoginURLRole is the most a connection created from the login page can
// grant: anyone can point one at a server they run, where they hold every
// ClickHouse privilege, so it never yields CH-UI admin.
func CapLoginURLRole(role string) string {
	if role == "admin" {
		return "analyst"
	}
	return role
}

var (
	// ErrRoleConnectionRequired is returned when a ClickHouse user's
	// override is written without a connection.
	ErrRoleConnectionRequired = errors.New("a connection is required for a ClickHouse user's role override")
	// ErrRoleLoginURLConnection is returned when an override targets a
	// connection created from the login page.
	ErrRoleLoginURLConnection = errors.New("connections created from the login page cannot carry role overrides")
	// ErrRoleConnectionNotFound is returned when the target connection does
	// not exist.
	ErrRoleConnectionNotFound = errors.New("connection not found")
)

// roleScope returns the connection_id a key's override is stored under: ""
// for an SSO person, else connectionID.
func roleScope(roleKey, connectionID string) string {
	if IsSSORoleKey(roleKey) {
		return ""
	}
	return connectionID
}

// SSOServiceAccountRoleOverrides returns role overrides stored under a
// connection's SSO service account name on that connection. Before overrides
// were keyed per person, such a row applied to every SSO person on that
// connection; it now applies only to password logins as that ClickHouse user.
func (db *DB) SSOServiceAccountRoleOverrides() ([]UserRole, error) {
	rows, err := db.conn.Query(`
		SELECT ur.username, ur.connection_id, c.name, ur.role, ur.created_at
		FROM user_roles ur
		JOIN connections c ON c.id = ur.connection_id
		WHERE c.sso_ch_user IS NOT NULL AND c.sso_ch_user != '' AND ur.username = c.sso_ch_user
		ORDER BY ur.username ASC, c.name ASC`)
	if err != nil {
		return nil, fmt.Errorf("get sso service account role overrides: %w", err)
	}
	return scanUserRoles(rows)
}

// GetUserRole retrieves the CH-UI role override for a role key on a
// connection. An SSO key matches its global override; a ClickHouse user
// matches only an override on that connection. Returns "" when none is set
// (meaning auto-detect from ClickHouse).
func (db *DB) GetUserRole(roleKey, connectionID string) (string, error) {
	scope := roleScope(roleKey, connectionID)
	if scope == "" && !IsSSORoleKey(roleKey) {
		return "", nil
	}
	var role string
	err := db.conn.QueryRow("SELECT role FROM user_roles WHERE username = ? AND connection_id = ?", roleKey, scope).Scan(&role)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get user role: %w", err)
	}
	return role, nil
}

// checkRoleConnection validates the connection a ClickHouse user's override
// is written for.
func (db *DB) checkRoleConnection(connectionID string) error {
	if connectionID == "" {
		return ErrRoleConnectionRequired
	}
	var n int
	if err := db.conn.QueryRow("SELECT COUNT(*) FROM connections WHERE id = ?", connectionID).Scan(&n); err != nil {
		return fmt.Errorf("check role connection: %w", err)
	}
	if n == 0 {
		return ErrRoleConnectionNotFound
	}
	loginURL, err := db.IsLoginURLConnection(connectionID)
	if err != nil {
		return fmt.Errorf("check role connection: %w", err)
	}
	if loginURL {
		return ErrRoleLoginURLConnection
	}
	return nil
}

// SetUserRole sets or updates a CH-UI role override (upsert). connectionID
// is ignored for an SSO key and required for a ClickHouse user.
func (db *DB) SetUserRole(roleKey, connectionID, role string) error {
	scope := roleScope(roleKey, connectionID)
	if !IsSSORoleKey(roleKey) {
		if err := db.checkRoleConnection(scope); err != nil {
			return err
		}
	}
	_, err := db.conn.Exec(
		`INSERT INTO user_roles (username, connection_id, role) VALUES (?, ?, ?)
		 ON CONFLICT(username, connection_id) DO UPDATE SET role = excluded.role`,
		roleKey, scope, role,
	)
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	return nil
}

// BootstrapFirstAdmin makes a ClickHouse user the CH-UI admin on a connection
// if, and only if, nobody holds an admin override anywhere. The check and the
// write are one statement, so two first logins cannot both win. The caller
// must not pass a connection created from the login page.
func (db *DB) BootstrapFirstAdmin(username, connectionID string) (bool, error) {
	if IsSSORoleKey(username) || connectionID == "" {
		return false, ErrRoleConnectionRequired
	}
	res, err := db.conn.Exec(
		`INSERT INTO user_roles (username, connection_id, role)
		 SELECT ?, ?, 'admin'
		 WHERE NOT EXISTS (SELECT 1 FROM user_roles WHERE role = 'admin')`,
		username, connectionID,
	)
	if err != nil {
		return false, fmt.Errorf("bootstrap first admin: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("bootstrap first admin: %w", err)
	}
	return n == 1, nil
}

// DeleteUserRole removes a CH-UI role override (reverts to auto-detect).
// connectionID is ignored for an SSO key.
func (db *DB) DeleteUserRole(roleKey, connectionID string) error {
	_, err := db.conn.Exec("DELETE FROM user_roles WHERE username = ? AND connection_id = ?", roleKey, roleScope(roleKey, connectionID))
	if err != nil {
		return fmt.Errorf("delete user role: %w", err)
	}
	return nil
}

// GetAllUserRoles retrieves all CH-UI role overrides, with the connection
// name for ClickHouse user overrides.
func (db *DB) GetAllUserRoles() ([]UserRole, error) {
	rows, err := db.conn.Query(`
		SELECT ur.username, ur.connection_id, COALESCE(c.name, ''), ur.role, ur.created_at
		FROM user_roles ur
		LEFT JOIN connections c ON c.id = ur.connection_id
		ORDER BY ur.username ASC, COALESCE(c.name, '') ASC`)
	if err != nil {
		return nil, fmt.Errorf("get all user roles: %w", err)
	}
	return scanUserRoles(rows)
}

func scanUserRoles(rows *sql.Rows) ([]UserRole, error) {
	defer rows.Close()
	var roles []UserRole
	for rows.Next() {
		var r UserRole
		var createdAt sql.NullString
		if err := rows.Scan(&r.Username, &r.ConnectionID, &r.ConnectionName, &r.Role, &createdAt); err != nil {
			return nil, fmt.Errorf("scan user role: %w", err)
		}
		r.CreatedAt = createdAt.String
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user role rows: %w", err)
	}
	return roles, nil
}

// CountUsersWithRole returns the number of overrides with a given CH-UI role,
// across every connection.
func (db *DB) CountUsersWithRole(role string) (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM user_roles WHERE role = ?", role).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count users with role: %w", err)
	}
	return count, nil
}

// IsUserRole returns true if the role key has the given explicit role on the
// connection (globally for an SSO key).
func (db *DB) IsUserRole(roleKey, connectionID, role string) (bool, error) {
	var exists int
	err := db.conn.QueryRow(
		"SELECT 1 FROM user_roles WHERE username = ? AND connection_id = ? AND role = ? LIMIT 1",
		roleKey, roleScope(roleKey, connectionID), role,
	).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("is user role: %w", err)
	}
	return exists == 1, nil
}

// SessionRole returns the app role a session acts with: the override for its
// identity on its connection, else the role cached at sign-in, else viewer.
// On a connection created from the login page the result is capped by
// CapLoginURLRole. On error it returns viewer.
func (db *DB) SessionRole(s *Session) (string, error) {
	authSubject := ""
	if s.AuthSubject != nil {
		authSubject = *s.AuthSubject
	}
	if authSubject == "" && IsReservedRoleUsername(s.ClickhouseUser) {
		// A password session from before such names were refused: its key
		// would read an SSO person's override.
		return "viewer", nil
	}
	override, err := db.GetUserRole(RoleKey(s.ClickhouseUser, authSubject), s.ConnectionID)
	if err != nil {
		return "viewer", err
	}
	role := "viewer"
	if override != "" {
		role = override
	} else if s.UserRole != nil && *s.UserRole != "" {
		role = *s.UserRole
	}
	loginURL, err := db.IsLoginURLConnection(s.ConnectionID)
	if loginURL {
		role = CapLoginURLRole(role)
	}
	return role, err
}

// ActorRole returns the app role of a person outside a request, such as the
// author of a scheduled report: their override, else the role cached on
// their newest session, else viewer. actor is what a session acts as (the
// SSO email or the ClickHouse user); connectionID is where the work runs.
// On a connection created from the login page the result is capped by
// CapLoginURLRole. On error it returns viewer.
func (db *DB) ActorRole(actor, connectionID string) (string, error) {
	if actor == "" {
		return "viewer", nil
	}
	role, err := db.GetUserRole(SSORoleKeyPrefix+actor, connectionID)
	if err != nil {
		return "viewer", err
	}
	if role == "" && !IsReservedRoleUsername(actor) {
		if role, err = db.GetUserRole(actor, connectionID); err != nil {
			return "viewer", err
		}
	}
	if role == "" {
		var cached sql.NullString
		err := db.conn.QueryRow(`SELECT user_role FROM sessions
			WHERE auth_subject = ? OR (COALESCE(auth_subject, '') = '' AND clickhouse_user = ? AND connection_id = ?)
			ORDER BY created_at DESC LIMIT 1`, actor, actor, connectionID).Scan(&cached)
		if err != nil && err != sql.ErrNoRows {
			return "viewer", fmt.Errorf("get cached role: %w", err)
		}
		role = cached.String
	}
	if role == "" {
		role = "viewer"
	}
	loginURL, err := db.IsLoginURLConnection(connectionID)
	if err != nil {
		return "viewer", err
	}
	if loginURL {
		role = CapLoginURLRole(role)
	}
	return role, nil
}

// RevokeAllAdmins removes every explicit admin assignment and drops the cached
// admin role from those people's sessions, so they fall back to viewer until
// they sign in again. The next ClickHouse admin who signs in then becomes the
// CH-UI admin, as on a fresh install. Returns the overrides that were revoked.
func (db *DB) RevokeAllAdmins() ([]UserRole, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, fmt.Errorf("revoke admins: %w", err)
	}
	defer tx.Rollback()

	rows, err := tx.Query(`
		SELECT ur.username, ur.connection_id, COALESCE(c.name, ''), ur.role, ur.created_at
		FROM user_roles ur
		LEFT JOIN connections c ON c.id = ur.connection_id
		WHERE ur.role = 'admin'
		ORDER BY ur.username ASC, COALESCE(c.name, '') ASC`)
	if err != nil {
		return nil, fmt.Errorf("revoke admins: %w", err)
	}
	revoked, err := scanUserRoles(rows)
	if err != nil {
		return nil, fmt.Errorf("revoke admins: %w", err)
	}

	if _, err := tx.Exec("DELETE FROM user_roles WHERE role = 'admin'"); err != nil {
		return nil, fmt.Errorf("revoke admins: %w", err)
	}
	if _, err := tx.Exec("UPDATE sessions SET user_role = 'viewer' WHERE user_role = 'admin'"); err != nil {
		return nil, fmt.Errorf("revoke admins: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("revoke admins: %w", err)
	}
	return revoked, nil
}

// String renders an override for logs and the CLI: the key, and the
// connection it applies on for a ClickHouse user.
func (r UserRole) String() string {
	if r.ConnectionID == "" {
		return r.Username
	}
	name := r.ConnectionName
	if name == "" {
		name = r.ConnectionID
	}
	return r.Username + " on " + name
}
