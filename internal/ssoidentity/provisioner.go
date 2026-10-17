// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package ssoidentity gives every SSO person their own ClickHouse user.
//
// A connection in per-person mode (database.SSOModePerPerson) names a
// provisioning account: a ClickHouse user that may create, alter and drop
// users under the connection's prefix and hand out exactly the roles the
// group mapping names. At every sign-in SignIn creates or updates the
// person's user, rotates its password, syncs its roles from the IdP groups
// and returns the credentials the session queries with. Lock and Drop retire
// users that stopped signing in.
package ssoidentity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Surface is the log_comment surface every provisioning statement is tagged
// with.
const Surface = "sso_provisioning"

const (
	statementTimeout = 30 * time.Second
	hostCacheTTL     = 10 * time.Minute
)

// Person is a verified SSO sign-in: the claims SignIn needs.
type Person struct {
	Issuer  string
	Subject string
	Email   string
	Name    string
	Groups  []string
}

// Provisioner creates and keeps per-person ClickHouse users in sync. Secret
// is the app secret key the stored passwords are encrypted with. The zero
// value with the three fields set is ready to use.
type Provisioner struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Secret  string

	// hosts caches the address ClickHouse sees the provisioner from, per
	// connection id, for host mode "auto".
	hosts sync.Map
}

type cachedHost struct {
	address string
	at      time.Time
}

// Result is what a sign-in gets: the person's ClickHouse user and the
// encrypted password to put on the session.
type Result struct {
	IdentityID        string
	CHUser            string
	EncryptedPassword string
	Roles             []string
	// Created is true the first time the person gets a user on this
	// connection.
	Created bool
}

// ErrSharedMode is returned when the connection is not in per-person mode:
// the caller uses the shared service account as before.
var ErrSharedMode = errors.New("connection uses the shared SSO account")

// NoRolesError means the person's IdP groups map to no ClickHouse role on the
// connection and no fallback role is configured, so they get no user.
type NoRolesError struct {
	ConnectionID string
}

func (e *NoRolesError) Error() string {
	return fmt.Sprintf("no ClickHouse role is mapped for this person's groups on connection %s", e.ConnectionID)
}

// UserExistsError means ClickHouse already has a user with the name CH-UI
// would create and CH-UI did not create it. It is never taken over: someone
// may have pre-created it with broader grants.
type UserExistsError struct {
	CHUser string
}

func (e *UserExistsError) Error() string {
	return fmt.Sprintf("ClickHouse user %q already exists and was not created by CH-UI", e.CHUser)
}

// account is the provisioning account of one connection, decrypted.
type account struct {
	user     string
	password string
}

// SignIn provisions or re-syncs the person's ClickHouse user on connID from
// a verified ID token and returns the credentials for the session. It
// returns ErrSharedMode when the connection is not in per-person mode.
func (p *Provisioner) SignIn(ctx context.Context, connID string, person Person) (*Result, error) {
	cfg, err := p.DB.GetSSOIdentityConfig(connID)
	if err != nil {
		return nil, err
	}
	if !cfg.PerPerson() {
		return nil, ErrSharedMode
	}
	roles := cfg.RolesForGroups(person.Groups)
	if roles == nil {
		return nil, &NoRolesError{ConnectionID: connID}
	}
	acct, err := p.account(connID)
	if err != nil {
		return nil, err
	}
	ident, err := p.DB.UpsertSSOIdentity(person.Issuer, person.Subject, person.Email, person.Name, person.Groups)
	if err != nil {
		return nil, err
	}
	ctx = tunnel.WithTag(ctx, tunnel.QueryTag{Surface: Surface, Person: ident.Email})

	existing, err := p.DB.GetSSOClickHouseUser(ident.ID, connID)
	if err != nil {
		return nil, err
	}
	chUser := cfg.UserName(ident.Email)
	var rolesBefore []string
	if existing != nil {
		chUser = existing.CHUser
		rolesBefore = existing.Roles
	} else {
		if err := p.refuseForeignUser(ctx, connID, acct, ident.ID, chUser); err != nil {
			return nil, err
		}
	}

	host, err := p.hostClause(ctx, connID, acct, cfg)
	if err != nil {
		return nil, err
	}

	password, hash, err := newPassword()
	if err != nil {
		return nil, err
	}
	userIdent := safe.Identifier(chUser)
	identified := " IDENTIFIED WITH sha256_hash BY '" + hash + "' " + host
	if cfg.SettingsProfile != "" {
		identified += " SETTINGS PROFILE " + safe.QuotedLiteral(cfg.SettingsProfile)
	}
	redact := func(err error) error {
		return errors.New(strings.ReplaceAll(err.Error(), hash, "<hash>"))
	}

	if _, err := p.exec(ctx, connID, acct, "CREATE USER IF NOT EXISTS "+userIdent+identified, nil); err != nil {
		return nil, fmt.Errorf("create user %s: %w", chUser, redact(err))
	}
	if _, err := p.exec(ctx, connID, acct, "ALTER USER "+userIdent+identified, nil); err != nil {
		return nil, fmt.Errorf("rotate password of %s: %w", chUser, redact(err))
	}
	for _, role := range revokable(rolesBefore, roles, cfg.ManagedRoles()) {
		sql := "REVOKE " + safe.Identifier(role) + " FROM " + userIdent
		if _, err := p.exec(ctx, connID, acct, sql, map[string]string{"role": role}); err != nil {
			return nil, fmt.Errorf("revoke role %s from %s: %w", role, chUser, err)
		}
	}
	for _, role := range roles {
		sql := "GRANT " + safe.Identifier(role) + " TO " + userIdent
		if _, err := p.exec(ctx, connID, acct, sql, map[string]string{"role": role}); err != nil {
			return nil, fmt.Errorf("grant role %s to %s: %w", role, chUser, err)
		}
	}
	if _, err := p.exec(ctx, connID, acct, "ALTER USER "+userIdent+" DEFAULT ROLE ALL", nil); err != nil {
		return nil, fmt.Errorf("set default roles of %s: %w", chUser, err)
	}

	enc, err := crypto.Encrypt(password, p.Secret)
	if err != nil {
		return nil, fmt.Errorf("encrypt password: %w", err)
	}
	if err := p.DB.SaveSSOClickHouseUser(database.SSOClickHouseUser{
		IdentityID:        ident.ID,
		ConnectionID:      connID,
		CHUser:            chUser,
		EncryptedPassword: enc,
		Roles:             roles,
	}); err != nil {
		return nil, err
	}

	created := existing == nil
	action := "sso.user_synced"
	if created {
		action = "sso.user_provisioned"
	}
	p.audit(action, ident.Email, chUser, connID, map[string]any{
		"roles_before": nonNil(rolesBefore),
		"roles_after":  nonNil(roles),
		"created":      created,
	})

	return &Result{
		IdentityID:        ident.ID,
		CHUser:            chUser,
		EncryptedPassword: enc,
		Roles:             roles,
		Created:           created,
	}, nil
}

// SignInIdentity is SignIn for a connection switch: the person is already
// known (a stored identity with the groups of their last verified token) and
// gets provisioned on connID without a fresh ID token.
func (p *Provisioner) SignInIdentity(ctx context.Context, connID, identityID string) (*Result, error) {
	ident, err := p.DB.GetSSOIdentity(identityID)
	if err != nil {
		return nil, err
	}
	if ident == nil {
		return nil, errors.New("unknown identity")
	}
	return p.SignIn(ctx, connID, Person{
		Issuer:  ident.Issuer,
		Subject: ident.Subject,
		Email:   ident.Email,
		Name:    ident.DisplayName,
		Groups:  ident.Groups,
	})
}

// Lock keeps the user but refuses every login (HOST NONE), so query_log
// history stays attributable. actor names who asked, "system" for the sweep.
func (p *Provisioner) Lock(ctx context.Context, u database.SSOPersonUser, actor string) error {
	acct, err := p.perPersonAccount(u.ConnectionID)
	if err != nil {
		return err
	}
	ctx = tunnel.WithTag(ctx, tunnel.QueryTag{Surface: Surface, Person: u.Email})
	if _, err := p.exec(ctx, u.ConnectionID, acct, "ALTER USER "+safe.Identifier(u.CHUser)+" HOST NONE", nil); err != nil {
		return fmt.Errorf("lock user %s: %w", u.CHUser, err)
	}
	if err := p.DB.SetSSOClickHouseUserLocked(u.IdentityID, u.ConnectionID, true); err != nil {
		return err
	}
	p.audit("sso.user_locked", actor, u.CHUser, u.ConnectionID, map[string]any{
		"email":     u.Email,
		"last_seen": u.LastSeenAt,
	})
	return nil
}

// Drop removes the user from ClickHouse and forgets it.
func (p *Provisioner) Drop(ctx context.Context, u database.SSOPersonUser, actor string) error {
	acct, err := p.perPersonAccount(u.ConnectionID)
	if err != nil {
		return err
	}
	ctx = tunnel.WithTag(ctx, tunnel.QueryTag{Surface: Surface, Person: u.Email})
	if _, err := p.exec(ctx, u.ConnectionID, acct, "DROP USER IF EXISTS "+safe.Identifier(u.CHUser), nil); err != nil {
		return fmt.Errorf("drop user %s: %w", u.CHUser, err)
	}
	if err := p.DB.DeleteSSOClickHouseUser(u.IdentityID, u.ConnectionID); err != nil {
		return err
	}
	p.audit("sso.user_dropped", actor, u.CHUser, u.ConnectionID, map[string]any{
		"email":     u.Email,
		"last_seen": u.LastSeenAt,
	})
	return nil
}

// perPersonAccount is account for Lock and Drop: the connection must still be
// in per-person mode, otherwise the stored account is the shared one.
func (p *Provisioner) perPersonAccount(connID string) (account, error) {
	cfg, err := p.DB.GetSSOIdentityConfig(connID)
	if err != nil {
		return account{}, err
	}
	if !cfg.PerPerson() {
		return account{}, ErrSharedMode
	}
	return p.account(connID)
}

// account returns the connection's provisioning account, decrypted.
func (p *Provisioner) account(connID string) (account, error) {
	user, enc, err := p.DB.GetConnectionSSOAccount(connID)
	if err != nil {
		return account{}, err
	}
	if user == "" || enc == "" {
		return account{}, errors.New("no provisioning account on connection")
	}
	password, err := crypto.Decrypt(enc, p.Secret)
	if err != nil {
		return account{}, fmt.Errorf("decrypt provisioning account password: %w", err)
	}
	return account{user: user, password: password}, nil
}

// exec runs one statement through the gateway as the provisioning account.
func (p *Provisioner) exec(ctx context.Context, connID string, acct account, sql string, settings map[string]string) (*tunnel.QueryResult, error) {
	return p.Gateway.ExecuteQueryWithSettingsCtx(ctx, connID, sql, acct.user, acct.password, settings, statementTimeout)
}

// refuseForeignUser fails when chUser already exists in ClickHouse without
// CH-UI having created it, or when CH-UI created it for another identity.
func (p *Provisioner) refuseForeignUser(ctx context.Context, connID string, acct account, identityID, chUser string) error {
	owner, err := p.DB.SSOClickHouseUserOwner(connID, chUser)
	if err != nil {
		return err
	}
	if owner != "" && owner != identityID {
		return &UserExistsError{CHUser: chUser}
	}
	res, err := p.exec(ctx, connID, acct, "SELECT count() AS n FROM system.users WHERE name = "+safe.QuotedLiteral(chUser), nil)
	if err != nil {
		if isAccessDenied(err) {
			return fmt.Errorf("the provisioning account %s cannot read system.users; it needs GRANT SHOW USERS ON *.* to check for pre-existing users: %w", acct.user, err)
		}
		return fmt.Errorf("check for existing user %s: %w", chUser, err)
	}
	rows, err := decodeRows(res)
	if err != nil {
		return fmt.Errorf("check for existing user %s: %w", chUser, err)
	}
	if len(rows) > 0 && toInt(rows[0]["n"]) > 0 {
		return &UserExistsError{CHUser: chUser}
	}
	return nil
}

// hostClause renders the HOST clause for cfg.Host, detecting the address for
// mode "auto".
func (p *Provisioner) hostClause(ctx context.Context, connID string, acct account, cfg database.SSOIdentityConfig) (string, error) {
	switch cfg.Host {
	case database.SSOHostAny:
		return "HOST ANY", nil
	case database.SSOHostAuto:
		addr, err := p.detectHost(ctx, connID, acct)
		if err != nil {
			return "", err
		}
		return "HOST IP " + safe.QuotedLiteral(addr), nil
	}
	parts := strings.Split(cfg.Host, ",")
	ips := make([]string, 0, len(parts))
	for _, ip := range parts {
		if ip = strings.TrimSpace(ip); ip != "" {
			ips = append(ips, "IP "+safe.QuotedLiteral(ip))
		}
	}
	if len(ips) == 0 {
		return "", fmt.Errorf("connection SSO host %q names no IP address", cfg.Host)
	}
	return "HOST " + strings.Join(ips, ", "), nil
}

// detectHost returns the address ClickHouse sees the provisioner connect
// from, cached per connection for hostCacheTTL.
func (p *Provisioner) detectHost(ctx context.Context, connID string, acct account) (string, error) {
	if v, ok := p.hosts.Load(connID); ok {
		if c := v.(cachedHost); time.Since(c.at) < hostCacheTTL {
			return c.address, nil
		}
	}
	addr, err := p.lookupHost(ctx, connID, acct)
	if err != nil {
		return "", err
	}
	p.hosts.Store(connID, cachedHost{address: addr, at: time.Now()})
	return addr, nil
}

// lookupHost asks ClickHouse which address it sees acct connect from, with
// no cache: the probe uses it for an account that may not be saved yet.
func (p *Provisioner) lookupHost(ctx context.Context, connID string, acct account) (string, error) {
	res, err := p.exec(ctx, connID, acct, "SELECT toString(address) AS address FROM system.processes WHERE query_id = queryID()", nil)
	var addr string
	if err == nil {
		var rows []map[string]any
		rows, err = decodeRows(res)
		if err == nil && len(rows) > 0 {
			addr, _ = rows[0]["address"].(string)
		}
	}
	addr = normalizeAddress(addr)
	if err != nil || addr == "" {
		if err == nil {
			err = errors.New("system.processes returned no address for the current query")
		}
		return "", fmt.Errorf("cannot detect the address ClickHouse sees the provisioning account %s from; grant it SELECT(address, query_id) ON system.processes or set the connection's SSO host explicitly: %w", acct.user, err)
	}
	return addr, nil
}

// normalizeAddress strips an IPv4-mapped IPv6 prefix and any port.
func normalizeAddress(addr string) string {
	addr = strings.TrimSpace(addr)
	if strings.HasPrefix(addr, "[") {
		if j := strings.Index(addr, "]"); j > 0 {
			addr = addr[1:j] // [ipv6]:port
		}
	}
	addr = strings.TrimPrefix(addr, "::ffff:")
	if i := strings.LastIndex(addr, ":"); i > 0 && strings.Count(addr, ":") == 1 {
		addr = addr[:i] // ipv4:port
	}
	return addr
}

// newPassword returns a fresh random password and its sha256 hex digest.
func newPassword() (password, hash string, err error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("generate password: %w", err)
	}
	password = hex.EncodeToString(raw)
	sum := sha256.Sum256([]byte(password))
	return password, hex.EncodeToString(sum[:]), nil
}

// revokable is every role in before that CH-UI manages and that after no
// longer names. Roles granted outside CH-UI are never touched.
func revokable(before, after, managed []string) []string {
	keep := make(map[string]bool, len(after))
	for _, r := range after {
		keep[r] = true
	}
	isManaged := make(map[string]bool, len(managed))
	for _, r := range managed {
		isManaged[r] = true
	}
	var out []string
	for _, r := range before {
		if isManaged[r] && !keep[r] {
			out = append(out, r)
		}
	}
	return out
}

func (p *Provisioner) audit(action, actor, chUser, connID string, details map[string]any) {
	raw, err := json.Marshal(details)
	if err != nil {
		return
	}
	d := string(raw)
	_ = p.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       &actor,
		ClickhouseUser: &chUser,
		ConnectionID:   &connID,
		Details:        &d,
	})
}

func isAccessDenied(err error) bool {
	msg := err.Error()
	return strings.Contains(msg, "ACCESS_DENIED") || strings.Contains(msg, "Not enough privileges")
}

func decodeRows(res *tunnel.QueryResult) ([]map[string]any, error) {
	if res == nil || len(res.Data) == 0 {
		return nil, nil
	}
	var rows []map[string]any
	if err := json.Unmarshal(res.Data, &rows); err != nil {
		return nil, fmt.Errorf("decode rows: %w", err)
	}
	return rows, nil
}

// toInt reads a ClickHouse JSON number, which arrives as a string for 64-bit
// integers and as a float64 otherwise.
func toInt(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	case json.Number:
		i, _ := n.Int64()
		return i
	}
	return 0
}

func nonNil(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// SetupSQL renders the one-time admin setup for a connection in per-person
// mode: the managed roles, the optional settings profile, the provisioning
// account and exactly the grants SignIn, Lock and Drop need.
func SetupSQL(cfg database.SSOIdentityConfig, provisionerUser string) string {
	cfg.Normalize()
	prov := safe.Identifier(provisionerUser)
	roles := cfg.ManagedRoles()
	var b strings.Builder
	b.WriteString("-- CH-UI per-person SSO setup. Run once as a ClickHouse admin.\n")
	b.WriteString("-- Every SSO person gets their own user named " + cfg.UserPrefix + "<email>, created and\n")
	b.WriteString("-- kept in sync by the provisioning account " + provisionerUser + ".\n\n")

	b.WriteString("-- 1. The roles the group mapping hands out. Give them the grants the people need.\n")
	b.WriteString("--    Include SELECT ON system.processes and system.query_log so query progress\n")
	b.WriteString("--    and Query Profile work for them.\n")
	if len(roles) == 0 {
		b.WriteString("--    (no roles mapped yet)\n")
	}
	for _, r := range roles {
		b.WriteString("CREATE ROLE IF NOT EXISTS " + safe.Identifier(r) + ";\n")
	}
	b.WriteString("--    e.g. GRANT SELECT ON system.processes, system.query_log TO <role>;\n")
	b.WriteString("\n")

	if cfg.SettingsProfile != "" {
		b.WriteString("-- 2. The settings profile every created user gets. Adjust the limits.\n")
		b.WriteString("CREATE SETTINGS PROFILE IF NOT EXISTS " + safe.Identifier(cfg.SettingsProfile) +
			" SETTINGS max_memory_usage = 4000000000, max_execution_time = 300;\n\n")
	} else {
		b.WriteString("-- 2. No settings profile configured; set one on the connection to cap the people's queries.\n\n")
	}

	b.WriteString("-- 3. The provisioning account. Choose a strong password and store it on the connection\n")
	b.WriteString("--    as the SSO account; it only ever creates, alters and drops users under the prefix.\n")
	b.WriteString("CREATE USER IF NOT EXISTS " + prov + " IDENTIFIED WITH sha256_password BY '<choose a password>';\n")
	b.WriteString("-- The * outside the backticks is the only form ClickHouse treats as a name wildcard.\n")
	b.WriteString("GRANT CREATE USER, ALTER USER, DROP USER ON " + safe.Identifier(cfg.UserPrefix) + "* TO " + prov + ";\n")
	if len(roles) > 0 {
		quoted := make([]string, len(roles))
		for i, r := range roles {
			quoted[i] = safe.Identifier(r)
		}
		b.WriteString("GRANT " + strings.Join(quoted, ", ") + " TO " + prov + " WITH ADMIN OPTION;\n")
	}
	b.WriteString("-- Needed to refuse a sign-in when the user name is already taken by someone else.\n")
	b.WriteString("GRANT SHOW USERS ON *.* TO " + prov + ";\n")
	b.WriteString("-- Only needed when the connection's SSO host is \"auto\": the provisioner looks up the\n")
	b.WriteString("-- address ClickHouse sees it connect from and pins the created users to it.\n")
	b.WriteString("GRANT SELECT(address, query_id) ON system.processes TO " + prov + ";\n")
	b.WriteString("-- The provisioner activates each role only for the statement that grants it.\n")
	b.WriteString("SET DEFAULT ROLE NONE TO " + prov + ";\n")
	return b.String()
}
