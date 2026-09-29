package database

import (
	"database/sql"
	"fmt"
)

// UserRole represents a CH-UI role assignment for a ClickHouse user.
type UserRole struct {
	Username  string `json:"username"`
	Role      string `json:"role"`
	CreatedAt string `json:"created_at"`
}

// SSORoleKeyPrefix namespaces role overrides for people who sign in with SSO.
// SSO people share one ClickHouse service account, so their overrides are keyed
// on their email instead, and the prefix keeps an email from ever matching a
// ClickHouse username.
const SSORoleKeyPrefix = "sso:"

// RoleKey returns the user_roles key for a session identity: "sso:<email>" for
// an SSO person, the ClickHouse user for a password login.
func RoleKey(clickhouseUser, authSubject string) string {
	if authSubject != "" {
		return SSORoleKeyPrefix + authSubject
	}
	return clickhouseUser
}

// SSOServiceAccountRoleOverrides returns role overrides stored under a
// connection's SSO service account name. Before overrides were keyed per
// person, such a row applied to every SSO person on that connection; it now
// applies only to password logins as that ClickHouse user.
func (db *DB) SSOServiceAccountRoleOverrides() ([]UserRole, error) {
	rows, err := db.conn.Query(`
		SELECT ur.username, ur.role, ur.created_at
		FROM user_roles ur
		WHERE ur.username IN (SELECT sso_ch_user FROM connections WHERE sso_ch_user IS NOT NULL AND sso_ch_user != '')
		ORDER BY ur.username ASC`)
	if err != nil {
		return nil, fmt.Errorf("get sso service account role overrides: %w", err)
	}
	defer rows.Close()

	var roles []UserRole
	for rows.Next() {
		var r UserRole
		if err := rows.Scan(&r.Username, &r.Role, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user role: %w", err)
		}
		roles = append(roles, r)
	}
	return roles, rows.Err()
}

// GetUserRole retrieves the CH-UI role for a user.
// Returns empty string if not set (meaning auto-detect from ClickHouse).
func (db *DB) GetUserRole(username string) (string, error) {
	var role string
	err := db.conn.QueryRow("SELECT role FROM user_roles WHERE username = ?", username).Scan(&role)
	if err != nil {
		if err.Error() == "sql: no rows in result set" {
			return "", nil
		}
		return "", fmt.Errorf("get user role: %w", err)
	}
	return role, nil
}

// SetUserRole sets or updates the CH-UI role for a user (upsert).
func (db *DB) SetUserRole(username, role string) error {
	_, err := db.conn.Exec(
		`INSERT INTO user_roles (username, role) VALUES (?, ?)
		 ON CONFLICT(username) DO UPDATE SET role = excluded.role`,
		username, role,
	)
	if err != nil {
		return fmt.Errorf("set user role: %w", err)
	}
	return nil
}

// DeleteUserRole removes the CH-UI role assignment for a user (reverts to auto-detect).
func (db *DB) DeleteUserRole(username string) error {
	_, err := db.conn.Exec("DELETE FROM user_roles WHERE username = ?", username)
	if err != nil {
		return fmt.Errorf("delete user role: %w", err)
	}
	return nil
}

// GetAllUserRoles retrieves all CH-UI role assignments.
func (db *DB) GetAllUserRoles() ([]UserRole, error) {
	rows, err := db.conn.Query("SELECT username, role, created_at FROM user_roles ORDER BY username ASC")
	if err != nil {
		return nil, fmt.Errorf("get all user roles: %w", err)
	}
	defer rows.Close()

	var roles []UserRole
	for rows.Next() {
		var r UserRole
		if err := rows.Scan(&r.Username, &r.Role, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan user role: %w", err)
		}
		roles = append(roles, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user role rows: %w", err)
	}
	return roles, nil
}

// CountUsersWithRole returns the number of users currently assigned a given CH-UI role.
func (db *DB) CountUsersWithRole(role string) (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM user_roles WHERE role = ?", role).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count users with role: %w", err)
	}
	return count, nil
}

// IsUserRole returns true if username currently has the given explicit role in CH-UI.
func (db *DB) IsUserRole(username, role string) (bool, error) {
	var exists int
	err := db.conn.QueryRow(
		"SELECT 1 FROM user_roles WHERE username = ? AND role = ? LIMIT 1",
		username, role,
	).Scan(&exists)
	if err != nil {
		if err == sql.ErrNoRows {
			return false, nil
		}
		return false, fmt.Errorf("is user role: %w", err)
	}
	return exists == 1, nil
}
