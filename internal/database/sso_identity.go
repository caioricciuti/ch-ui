// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// SSO identity modes of a connection: who ClickHouse sees for an SSO person.
const (
	// SSOModeShared runs every SSO session through the connection's one
	// service account (the original behaviour).
	SSOModeShared = "shared"
	// SSOModePerPerson gives every person their own ClickHouse user, created
	// and kept in sync by a provisioning account.
	SSOModePerPerson = "per_person"
)

// SSO host modes, for the HOST clause of the users CH-UI creates.
const (
	// SSOHostAuto pins the user to the address ClickHouse sees the
	// provisioner connect from, detected at sign-in.
	SSOHostAuto = "auto"
	// SSOHostAny creates the user with HOST ANY.
	SSOHostAny = "any"
)

// Default lifecycle of a per-person user that stops signing in.
const (
	DefaultSSOLockAfterDays = 30
	DefaultSSODropAfterDays = 90
	DefaultSSOUserPrefix    = "sso:"
)

// SSORoleMapping gives the ClickHouse roles members of one IdP group get.
type SSORoleMapping struct {
	Group string   `json:"group"`
	Roles []string `json:"roles"`
}

// SSOIdentityConfig is a connection's per-person identity setup, stored as
// JSON in connections.sso_identity_config. The provisioning account itself is
// the connection's SSO account (GetConnectionSSOAccount): in shared mode it is
// the account everyone queries as, in per-person mode the account that
// creates users.
type SSOIdentityConfig struct {
	Mode string `json:"mode"`
	// UserPrefix starts every ClickHouse user name CH-UI creates; the rest is
	// the lower-cased email. The provisioner's grants are scoped to it.
	UserPrefix string `json:"user_prefix"`
	// SettingsProfile is assigned to every created user when set.
	SettingsProfile string `json:"settings_profile,omitempty"`
	// Host is SSOHostAuto, SSOHostAny, or a comma-separated list of IP
	// addresses the users may connect from.
	Host string `json:"host"`
	// RoleMappings map IdP groups to ClickHouse roles. A person gets the
	// union of the roles of every group they are in.
	RoleMappings []SSORoleMapping `json:"role_mappings"`
	// FallbackRoles are granted when no group matches. Empty refuses the
	// sign-in on this connection.
	FallbackRoles []string `json:"fallback_roles,omitempty"`
	// LockAfterDays locks (HOST NONE) a user not seen for that long; 0 never
	// locks. DropAfterDays drops a user not seen for that long; 0 never drops.
	LockAfterDays int `json:"lock_after_days"`
	DropAfterDays int `json:"drop_after_days"`
}

// DefaultSSOIdentityConfig is a connection with no config saved: shared mode.
func DefaultSSOIdentityConfig() SSOIdentityConfig {
	return SSOIdentityConfig{
		Mode:          SSOModeShared,
		UserPrefix:    DefaultSSOUserPrefix,
		Host:          SSOHostAuto,
		LockAfterDays: DefaultSSOLockAfterDays,
		DropAfterDays: DefaultSSODropAfterDays,
	}
}

// PerPerson reports whether SSO people get their own ClickHouse users.
func (c SSOIdentityConfig) PerPerson() bool { return c.Mode == SSOModePerPerson }

var ssoPrefixRe = regexp.MustCompile(`^[A-Za-z0-9_.:-]{1,32}$`)

// Normalize trims every field and fills the defaults for empty ones.
func (c *SSOIdentityConfig) Normalize() {
	c.Mode = strings.TrimSpace(strings.ToLower(c.Mode))
	if c.Mode == "" {
		c.Mode = SSOModeShared
	}
	c.UserPrefix = strings.TrimSpace(c.UserPrefix)
	if c.UserPrefix == "" {
		c.UserPrefix = DefaultSSOUserPrefix
	}
	c.SettingsProfile = strings.TrimSpace(c.SettingsProfile)
	c.Host = strings.TrimSpace(strings.ToLower(c.Host))
	if c.Host == "" {
		c.Host = SSOHostAuto
	}
	if c.Host != SSOHostAuto && c.Host != SSOHostAny {
		c.Host = strings.Join(splitTrimmed(c.Host), ",")
	}
	mappings := c.RoleMappings[:0]
	for _, m := range c.RoleMappings {
		m.Group = strings.TrimSpace(m.Group)
		m.Roles = uniqueTrimmed(m.Roles)
		if m.Group == "" && len(m.Roles) == 0 {
			continue
		}
		mappings = append(mappings, m)
	}
	c.RoleMappings = mappings
	c.FallbackRoles = uniqueTrimmed(c.FallbackRoles)
}

// Validate reports the first problem with the config, after Normalize.
func (c SSOIdentityConfig) Validate() error {
	switch c.Mode {
	case SSOModeShared, SSOModePerPerson:
	default:
		return fmt.Errorf("mode must be %q or %q", SSOModeShared, SSOModePerPerson)
	}
	if !ssoPrefixRe.MatchString(c.UserPrefix) {
		return errors.New("user_prefix must be 1 to 32 letters, digits, '_', '.', ':' or '-'")
	}
	if len(c.SettingsProfile) > 128 {
		return errors.New("settings_profile is too long")
	}
	if c.Host != SSOHostAuto && c.Host != SSOHostAny {
		for _, h := range strings.Split(c.Host, ",") {
			if net.ParseIP(h) == nil {
				return fmt.Errorf("host must be %q, %q or a comma-separated list of IP addresses; %q is not an IP", SSOHostAuto, SSOHostAny, h)
			}
		}
	}
	for i, m := range c.RoleMappings {
		if m.Group == "" {
			return fmt.Errorf("role mapping %d has no group", i+1)
		}
		if len(m.Roles) == 0 {
			return fmt.Errorf("role mapping for group %q has no roles", m.Group)
		}
		for _, r := range m.Roles {
			if err := validateSSORoleName(r); err != nil {
				return fmt.Errorf("group %q: %w", m.Group, err)
			}
		}
	}
	for _, r := range c.FallbackRoles {
		if err := validateSSORoleName(r); err != nil {
			return fmt.Errorf("fallback roles: %w", err)
		}
	}
	if c.PerPerson() && len(c.RoleMappings) == 0 && len(c.FallbackRoles) == 0 {
		return errors.New("per-person mode needs at least one group to role mapping or a fallback role")
	}
	if c.LockAfterDays < 0 || c.DropAfterDays < 0 {
		return errors.New("lock_after_days and drop_after_days cannot be negative")
	}
	if c.LockAfterDays > 0 && c.DropAfterDays > 0 && c.DropAfterDays < c.LockAfterDays {
		return errors.New("drop_after_days must not be smaller than lock_after_days")
	}
	return nil
}

func validateSSORoleName(r string) error {
	if r == "" {
		return errors.New("empty role name")
	}
	if len(r) > 128 || strings.ContainsAny(r, "\n\r\t`") {
		return fmt.Errorf("invalid role name %q", r)
	}
	return nil
}

// RolesForGroups returns the ClickHouse roles a person with these IdP groups
// gets: the union over matching mappings (case-insensitive group match),
// sorted; the fallback roles when nothing matches; nil when they get none.
func (c SSOIdentityConfig) RolesForGroups(groups []string) []string {
	have := make(map[string]bool, len(groups))
	for _, g := range groups {
		have[strings.ToLower(strings.TrimSpace(g))] = true
	}
	set := map[string]bool{}
	for _, m := range c.RoleMappings {
		if have[strings.ToLower(m.Group)] {
			for _, r := range m.Roles {
				set[r] = true
			}
		}
	}
	if len(set) == 0 {
		if len(c.FallbackRoles) == 0 {
			return nil
		}
		return append([]string(nil), c.FallbackRoles...)
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// ManagedRoles is every role the config can grant, sorted: the set the
// provisioner must hold WITH ADMIN OPTION and the only roles CH-UI revokes.
func (c SSOIdentityConfig) ManagedRoles() []string {
	set := map[string]bool{}
	for _, m := range c.RoleMappings {
		for _, r := range m.Roles {
			set[r] = true
		}
	}
	for _, r := range c.FallbackRoles {
		set[r] = true
	}
	out := make([]string, 0, len(set))
	for r := range set {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

// UserName is the ClickHouse user CH-UI creates for an email on this
// connection.
func (c SSOIdentityConfig) UserName(email string) string {
	return c.UserPrefix + strings.ToLower(strings.TrimSpace(email))
}

func splitTrimmed(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func uniqueTrimmed(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		s = strings.TrimSpace(s)
		if s == "" || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// GetSSOIdentityConfig returns the connection's identity config, the default
// (shared mode) when none was saved. ErrNotFound-style: an unknown connection
// also gets the default, since the caller already resolved it.
func (db *DB) GetSSOIdentityConfig(connID string) (SSOIdentityConfig, error) {
	cfg := DefaultSSOIdentityConfig()
	var raw sql.NullString
	err := db.conn.QueryRow("SELECT sso_identity_config FROM connections WHERE id = ?", connID).Scan(&raw)
	if err == sql.ErrNoRows {
		return cfg, nil
	}
	if err != nil {
		return cfg, fmt.Errorf("get sso identity config: %w", err)
	}
	if !raw.Valid || strings.TrimSpace(raw.String) == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw.String), &cfg); err != nil {
		return DefaultSSOIdentityConfig(), fmt.Errorf("get sso identity config: %w", err)
	}
	cfg.Normalize()
	return cfg, nil
}

// SetSSOIdentityConfig validates and stores the connection's identity config.
func (db *DB) SetSSOIdentityConfig(connID string, cfg SSOIdentityConfig) error {
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("set sso identity config: %w", err)
	}
	res, err := db.conn.Exec("UPDATE connections SET sso_identity_config = ? WHERE id = ?", string(raw), connID)
	if err != nil {
		return fmt.Errorf("set sso identity config: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("connection not found: %s", connID)
	}
	return nil
}

// SSOIdentity is a person as the identity provider knows them.
type SSOIdentity struct {
	ID          string   `json:"id"`
	Issuer      string   `json:"issuer"`
	Subject     string   `json:"subject"`
	Email       string   `json:"email"`
	DisplayName string   `json:"display_name"`
	Groups      []string `json:"groups"`
	CreatedAt   string   `json:"created_at"`
	LastLoginAt string   `json:"last_login_at"`
}

// UpsertSSOIdentity records a verified sign-in: the person keyed by (issuer,
// subject) gets their current email, name and groups, and a new row when
// first seen. Email is stored lower-cased.
func (db *DB) UpsertSSOIdentity(issuer, subject, email, displayName string, groups []string) (*SSOIdentity, error) {
	issuer, subject = strings.TrimSpace(issuer), strings.TrimSpace(subject)
	email = strings.ToLower(strings.TrimSpace(email))
	if issuer == "" || subject == "" || email == "" {
		return nil, errors.New("upsert sso identity: issuer, subject and email are required")
	}
	if groups == nil {
		groups = []string{}
	}
	groupsJSON, err := json.Marshal(groups)
	if err != nil {
		return nil, fmt.Errorf("upsert sso identity: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	id := uuid.NewString()
	_, err = db.conn.Exec(`INSERT INTO sso_identities (id, issuer, subject, email, display_name, groups, created_at, last_login_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(issuer, subject) DO UPDATE SET
			email = excluded.email, display_name = excluded.display_name,
			groups = excluded.groups, last_login_at = excluded.last_login_at`,
		id, issuer, subject, email, strings.TrimSpace(displayName), string(groupsJSON), now, now)
	if err != nil {
		return nil, fmt.Errorf("upsert sso identity: %w", err)
	}
	return db.getSSOIdentityWhere("issuer = ? AND subject = ?", issuer, subject)
}

// GetSSOIdentity returns the identity by id, or nil.
func (db *DB) GetSSOIdentity(id string) (*SSOIdentity, error) {
	return db.getSSOIdentityWhere("id = ?", id)
}

func (db *DB) getSSOIdentityWhere(where string, args ...any) (*SSOIdentity, error) {
	var ident SSOIdentity
	var groups string
	err := db.conn.QueryRow("SELECT id, issuer, subject, email, display_name, groups, created_at, last_login_at FROM sso_identities WHERE "+where, args...).
		Scan(&ident.ID, &ident.Issuer, &ident.Subject, &ident.Email, &ident.DisplayName, &groups, &ident.CreatedAt, &ident.LastLoginAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sso identity: %w", err)
	}
	if err := json.Unmarshal([]byte(groups), &ident.Groups); err != nil || ident.Groups == nil {
		ident.Groups = []string{}
	}
	return &ident, nil
}

// SSOClickHouseUser is the ClickHouse user CH-UI created for one person on
// one connection.
type SSOClickHouseUser struct {
	IdentityID   string `json:"identity_id"`
	ConnectionID string `json:"connection_id"`
	CHUser       string `json:"ch_user"`
	// EncryptedPassword is the generated password, encrypted with the app
	// secret key. Never serialized.
	EncryptedPassword string   `json:"-"`
	Roles             []string `json:"roles"`
	LastSyncAt        string   `json:"last_sync_at"`
	LastSeenAt        string   `json:"last_seen_at"`
	LockedAt          *string  `json:"locked_at,omitempty"`
}

// Locked reports whether the user is locked (HOST NONE) pending drop.
func (u SSOClickHouseUser) Locked() bool { return u.LockedAt != nil && *u.LockedAt != "" }

const ssoClickHouseUserColumns = "identity_id, connection_id, ch_user, encrypted_password, roles, last_sync_at, last_seen_at, locked_at"

func scanSSOClickHouseUser(scan func(dest ...any) error) (*SSOClickHouseUser, error) {
	var u SSOClickHouseUser
	var roles string
	var locked sql.NullString
	if err := scan(&u.IdentityID, &u.ConnectionID, &u.CHUser, &u.EncryptedPassword, &roles, &u.LastSyncAt, &u.LastSeenAt, &locked); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(roles), &u.Roles); err != nil || u.Roles == nil {
		u.Roles = []string{}
	}
	u.LockedAt = nullStringToPtr(locked)
	return &u, nil
}

// GetSSOClickHouseUser returns the person's user on the connection, or nil.
func (db *DB) GetSSOClickHouseUser(identityID, connID string) (*SSOClickHouseUser, error) {
	u, err := scanSSOClickHouseUser(db.conn.QueryRow(
		"SELECT "+ssoClickHouseUserColumns+" FROM sso_clickhouse_users WHERE identity_id = ? AND connection_id = ?", identityID, connID).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sso clickhouse user: %w", err)
	}
	return u, nil
}

// ListSSOClickHouseUsersForIdentity returns the person's users on every
// connection, locked ones included.
func (db *DB) ListSSOClickHouseUsersForIdentity(identityID string) ([]SSOClickHouseUser, error) {
	rows, err := db.conn.Query(
		"SELECT "+ssoClickHouseUserColumns+" FROM sso_clickhouse_users WHERE identity_id = ? ORDER BY connection_id", identityID)
	if err != nil {
		return nil, fmt.Errorf("list sso clickhouse users for identity: %w", err)
	}
	defer rows.Close()
	var out []SSOClickHouseUser
	for rows.Next() {
		u, err := scanSSOClickHouseUser(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("list sso clickhouse users for identity: %w", err)
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

// SaveSSOClickHouseUser records a provisioned or re-synced user: the current
// password, roles and sync time, unlocked, seen now.
func (db *DB) SaveSSOClickHouseUser(u SSOClickHouseUser) error {
	if u.IdentityID == "" || u.ConnectionID == "" || u.CHUser == "" || u.EncryptedPassword == "" {
		return errors.New("save sso clickhouse user: identity, connection, user and password are required")
	}
	roles := u.Roles
	if roles == nil {
		roles = []string{}
	}
	rolesJSON, err := json.Marshal(roles)
	if err != nil {
		return fmt.Errorf("save sso clickhouse user: %w", err)
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err = db.conn.Exec(`INSERT INTO sso_clickhouse_users (`+ssoClickHouseUserColumns+`)
		VALUES (?, ?, ?, ?, ?, ?, ?, NULL)
		ON CONFLICT(identity_id, connection_id) DO UPDATE SET
			ch_user = excluded.ch_user, encrypted_password = excluded.encrypted_password,
			roles = excluded.roles, last_sync_at = excluded.last_sync_at,
			last_seen_at = excluded.last_seen_at, locked_at = NULL`,
		u.IdentityID, u.ConnectionID, u.CHUser, u.EncryptedPassword, string(rolesJSON), now, now)
	if err != nil {
		return fmt.Errorf("save sso clickhouse user: %w", err)
	}
	return nil
}

// TouchSSOClickHouseUser marks the person as seen on the connection now.
func (db *DB) TouchSSOClickHouseUser(identityID, connID string) error {
	_, err := db.conn.Exec("UPDATE sso_clickhouse_users SET last_seen_at = ? WHERE identity_id = ? AND connection_id = ?",
		time.Now().UTC().Format(time.RFC3339), identityID, connID)
	if err != nil {
		return fmt.Errorf("touch sso clickhouse user: %w", err)
	}
	return nil
}

// SetSSOClickHouseUserLocked records that the user was locked (HOST NONE) or
// unlocked in ClickHouse.
func (db *DB) SetSSOClickHouseUserLocked(identityID, connID string, locked bool) error {
	var at any
	if locked {
		at = time.Now().UTC().Format(time.RFC3339)
	}
	_, err := db.conn.Exec("UPDATE sso_clickhouse_users SET locked_at = ? WHERE identity_id = ? AND connection_id = ?", at, identityID, connID)
	if err != nil {
		return fmt.Errorf("lock sso clickhouse user: %w", err)
	}
	return nil
}

// DeleteSSOClickHouseUser forgets the person's user on the connection, after
// it was dropped in ClickHouse.
func (db *DB) DeleteSSOClickHouseUser(identityID, connID string) error {
	_, err := db.conn.Exec("DELETE FROM sso_clickhouse_users WHERE identity_id = ? AND connection_id = ?", identityID, connID)
	if err != nil {
		return fmt.Errorf("delete sso clickhouse user: %w", err)
	}
	return nil
}

// SSOClickHouseUserOwner returns the identity that owns chUser on the
// connection, or "" when CH-UI did not create that user.
func (db *DB) SSOClickHouseUserOwner(connID, chUser string) (string, error) {
	var id string
	err := db.conn.QueryRow("SELECT identity_id FROM sso_clickhouse_users WHERE connection_id = ? AND ch_user = ?", connID, chUser).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("sso clickhouse user owner: %w", err)
	}
	return id, nil
}

// SSOPersonUser is a per-person user with the person behind it, for the
// admin screens and the sweep.
type SSOPersonUser struct {
	SSOClickHouseUser
	Email       string `json:"email"`
	DisplayName string `json:"display_name"`
}

// ListSSOClickHouseUsers returns every per-person user on the connection
// (every connection when connID is empty), oldest sign-in first.
func (db *DB) ListSSOClickHouseUsers(connID string) ([]SSOPersonUser, error) {
	q := `SELECT u.identity_id, u.connection_id, u.ch_user, u.encrypted_password, u.roles, u.last_sync_at, u.last_seen_at, u.locked_at, i.email, i.display_name
		FROM sso_clickhouse_users u JOIN sso_identities i ON i.id = u.identity_id`
	var args []any
	if connID != "" {
		q += " WHERE u.connection_id = ?"
		args = append(args, connID)
	}
	q += " ORDER BY u.last_seen_at ASC, u.ch_user ASC"
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list sso clickhouse users: %w", err)
	}
	defer rows.Close()
	var out []SSOPersonUser
	for rows.Next() {
		var p SSOPersonUser
		var roles string
		var locked sql.NullString
		if err := rows.Scan(&p.IdentityID, &p.ConnectionID, &p.CHUser, &p.EncryptedPassword, &roles, &p.LastSyncAt, &p.LastSeenAt, &locked, &p.Email, &p.DisplayName); err != nil {
			return nil, fmt.Errorf("list sso clickhouse users: %w", err)
		}
		if err := json.Unmarshal([]byte(roles), &p.Roles); err != nil || p.Roles == nil {
			p.Roles = []string{}
		}
		p.LockedAt = nullStringToPtr(locked)
		out = append(out, p)
	}
	return out, rows.Err()
}
