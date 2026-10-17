// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package ssoidentity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Check is one step of Probe: what was tried, whether it went as expected
// and a detail for the admin (the value found, or what grant is missing).
type Check struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail"`
}

// AllOK reports whether every check passed.
func AllOK(checks []Check) bool {
	for _, c := range checks {
		if !c.OK {
			return false
		}
	}
	return true
}

// Check names, in the order Probe reports them.
const (
	checkSignIn    = "Provisioner signs in"
	checkHost      = "Host detection"
	checkShowUsers = "Can list users (SHOW USERS)"
	checkCreate    = "Creates a user under the prefix"
	checkOutside   = "Cannot create users outside the prefix"
	checkRole      = "Cannot create roles"
	checkDrop      = "Drops the probe user"
)

func checkGrant(role string) string { return "Grants role " + role }

const skippedDetail = "skipped"

// Probe verifies a provisioning account against cfg without touching any
// real user: it signs in, creates one throw-away user under the prefix
// (HOST NONE, so it can never log in), grants it every managed role, checks
// the account is refused outside the prefix and for roles, and drops the
// probe user again. The account is passed explicitly because the admin may
// be verifying credentials before saving them. Each step is one Check;
// steps that depend on a failed one are reported as skipped.
func (p *Provisioner) Probe(ctx context.Context, connID string, cfg database.SSOIdentityConfig, user, password string) (checks []Check) {
	cfg.Normalize()
	acct := account{user: user, password: password}
	ctx = tunnel.WithTag(ctx, tunnel.QueryTag{Surface: Surface})
	suffix := randomHex(8)
	probe := cfg.UserPrefix + "__chui_probe_" + suffix
	outside := "__chui_probe_outside_" + suffix
	if strings.HasPrefix(outside, cfg.UserPrefix) {
		outside = "zz" + outside
	}
	probeRole := "__chui_probe_role_" + suffix
	roles := cfg.ManagedRoles()

	add := func(name string, ok bool, detail string) {
		checks = append(checks, Check{Name: name, OK: ok, Detail: detail})
	}
	skip := func(names ...string) {
		for _, n := range names {
			add(n, false, skippedDetail)
		}
	}
	grantNames := make([]string, len(roles))
	for i, r := range roles {
		grantNames[i] = checkGrant(r)
	}

	// 1. Sign in. Nothing else can run without it.
	res, err := p.exec(ctx, connID, acct, "SELECT currentUser() AS u", nil)
	if err != nil {
		add(checkSignIn, false, err.Error())
		if cfg.Host == database.SSOHostAuto {
			skip(checkHost)
		}
		skip(checkShowUsers, checkCreate)
		skip(grantNames...)
		skip(checkOutside, checkRole, checkDrop)
		return checks
	}
	who := user
	if rows, err := decodeRows(res); err == nil && len(rows) > 0 {
		if u, ok := rows[0]["u"].(string); ok && u != "" {
			who = u
		}
	}
	add(checkSignIn, true, "signed in as "+who)

	// 2. Host detection, only when the config relies on it. The probe user
	// is HOST NONE regardless, so a failure here blocks nothing else.
	if cfg.Host == database.SSOHostAuto {
		if addr, err := p.lookupHost(ctx, connID, acct); err != nil {
			add(checkHost, false, err.Error())
		} else {
			add(checkHost, true, "users will be pinned to "+addr)
		}
	}

	// 3. SHOW USERS, needed to refuse a sign-in whose name is taken.
	res, err = p.exec(ctx, connID, acct, "SELECT count() AS n FROM system.users WHERE name = "+safe.QuotedLiteral(probe), nil)
	switch {
	case err != nil && isAccessDenied(err):
		add(checkShowUsers, false, fmt.Sprintf("the provisioning account cannot read system.users; it needs GRANT SHOW USERS ON *.* TO %s: %v", safe.Identifier(user), err))
	case err != nil:
		add(checkShowUsers, false, err.Error())
	default:
		detail := "system.users is readable"
		if rows, derr := decodeRows(res); derr == nil && len(rows) > 0 && toInt(rows[0]["n"]) > 0 {
			detail = "system.users is readable; a user named " + probe + " already exists and is left alone"
		}
		add(checkShowUsers, true, detail)
	}

	// 4. Create the probe user under the prefix. HOST NONE: it never logs in.
	_, hash, err := newPassword()
	if err != nil {
		add(checkCreate, false, err.Error())
		skip(grantNames...)
		skip(checkOutside, checkRole, checkDrop)
		return checks
	}
	probeIdent := safe.Identifier(probe)
	create := "CREATE USER " + probeIdent + " IDENTIFIED WITH sha256_hash BY '" + hash + "' HOST NONE"
	detail := "created " + probe
	if cfg.SettingsProfile != "" {
		create += " SETTINGS PROFILE " + safe.QuotedLiteral(cfg.SettingsProfile)
		detail += " with settings profile " + cfg.SettingsProfile
	}
	created := false
	if _, err := p.exec(ctx, connID, acct, create, nil); err != nil {
		msg := strings.ReplaceAll(err.Error(), hash, "<hash>")
		if isAccessDenied(err) {
			msg = fmt.Sprintf("the provisioning account needs GRANT CREATE USER, ALTER USER, DROP USER ON %s* TO %s: %s", safe.Identifier(cfg.UserPrefix), safe.Identifier(user), msg)
		}
		add(checkCreate, false, msg)
		skip(grantNames...)
	} else {
		created = true
		add(checkCreate, true, detail)
		// Never leave the probe user behind, whatever happens below.
		defer func() {
			if _, err := p.exec(ctx, connID, acct, "DROP USER IF EXISTS "+probeIdent, nil); err != nil {
				add(checkDrop, false, fmt.Sprintf("could not drop %s; drop it as an admin: %v", probe, err))
				return
			}
			add(checkDrop, true, "dropped "+probe)
		}()

		// 5. Every managed role, activated for the statement like SignIn does.
		for _, role := range roles {
			sql := "GRANT " + safe.Identifier(role) + " TO " + probeIdent
			if _, err := p.exec(ctx, connID, acct, sql, map[string]string{"role": role}); err != nil {
				add(checkGrant(role), false, fmt.Sprintf("the role must exist and the provisioning account needs GRANT %s TO %s WITH ADMIN OPTION: %v", safe.Identifier(role), safe.Identifier(user), err))
				continue
			}
			add(checkGrant(role), true, "granted")
		}
	}

	// 6. The CREATE USER grant must stop at the prefix. ACCESS_DENIED is the
	// pass; a success means the grant is too broad, and the user goes away.
	_, outsideHash, err := newPassword()
	if err != nil {
		add(checkOutside, false, err.Error())
	} else {
		outsideIdent := safe.Identifier(outside)
		_, err := p.exec(ctx, connID, acct, "CREATE USER "+outsideIdent+" IDENTIFIED WITH sha256_hash BY '"+outsideHash+"' HOST NONE", nil)
		switch {
		case err == nil:
			msg := "provisioner can create users outside the prefix: its CREATE USER grant is too broad"
			if _, derr := p.exec(ctx, connID, acct, "DROP USER IF EXISTS "+outsideIdent, nil); derr != nil {
				msg += fmt.Sprintf("; could not drop %s, drop it as an admin: %v", outside, derr)
			} else {
				msg += "; " + outside + " was dropped again"
			}
			add(checkOutside, false, msg)
		case isAccessDenied(err):
			add(checkOutside, true, "denied, as it should be")
		default:
			add(checkOutside, false, strings.ReplaceAll(err.Error(), outsideHash, "<hash>"))
		}
	}

	// 7. No CREATE ROLE either.
	roleIdent := safe.Identifier(probeRole)
	_, err = p.exec(ctx, connID, acct, "CREATE ROLE "+roleIdent, nil)
	switch {
	case err == nil:
		msg := "provisioner can create roles: it should not hold CREATE ROLE"
		if _, derr := p.exec(ctx, connID, acct, "DROP ROLE IF EXISTS "+roleIdent, nil); derr != nil {
			msg += fmt.Sprintf("; could not drop role %s, drop it as an admin: %v", probeRole, derr)
		} else {
			msg += "; " + probeRole + " was dropped again"
		}
		add(checkRole, false, msg)
	case isAccessDenied(err):
		add(checkRole, true, "denied, as it should be")
	default:
		add(checkRole, false, err.Error())
	}

	// 8. The deferred drop reports itself when the probe user was created.
	if !created {
		skip(checkDrop)
	}
	return checks
}

// randomHex returns n random bytes as hex, for throw-away names.
func randomHex(n int) string {
	raw := make([]byte, n)
	if _, err := rand.Read(raw); err != nil {
		return "00000000"
	}
	return hex.EncodeToString(raw)[:n]
}
