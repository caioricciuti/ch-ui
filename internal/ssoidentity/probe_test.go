// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package ssoidentity

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const deniedErr = "Code: 497. DB::Exception: verify_prov: Not enough privileges. (ACCESS_DENIED)"

// probeResponder answers the probe's statements like a ClickHouse with a
// correctly scoped provisioner would: the two "outside" creates are denied
// unless outsideAllowed / roleAllowed say the grant is too broad.
type probeResponder struct {
	address        string
	outsideAllowed bool
	roleAllowed    bool
	failSQL        string
	failErr        string
}

func (r probeResponder) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	deny := &tunnel.AgentMessage{Type: "query_error", Error: deniedErr}
	if r.failSQL != "" && strings.Contains(msg.SQL, r.failSQL) {
		return &tunnel.AgentMessage{Type: "query_error", Error: r.failErr}
	}
	data := "[]"
	switch {
	case strings.Contains(msg.SQL, "currentUser()"):
		data = `[{"u":"` + msg.User + `"}]`
	case strings.Contains(msg.SQL, "system.users"):
		data = `[{"n":0}]`
	case strings.Contains(msg.SQL, "system.processes"):
		data = `[{"address":"` + r.address + `"}]`
	case strings.HasPrefix(msg.SQL, "CREATE USER `__chui_probe_outside_") && !r.outsideAllowed:
		return deny
	case strings.HasPrefix(msg.SQL, "CREATE ROLE ") && !r.roleAllowed:
		return deny
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data), Meta: json.RawMessage("[]")}
}

// probeSetup runs Probe with the account "verify_prov" (not the saved one,
// which is chui_prov) against an agent answering with r. It returns the
// connection, a getter for the SQL the agent received, and the checks.
func probeSetup(t *testing.T, cfg database.SSOIdentityConfig, r probeResponder) (string, func() []string, []Check) {
	t.Helper()
	_, conn, agent, p := setupWith(t, cfg, r.respond)
	checks := p.Probe(context.Background(), conn, cfg, "verify_prov", "verify-pw")
	for _, m := range queries(agent, 0) {
		if m.User != "verify_prov" || m.Password != "verify-pw" {
			t.Errorf("statement %q ran as %s, want the account passed to Probe", m.SQL, m.User)
		}
	}
	return conn, func() []string { return sqls(queries(agent, 0)) }, checks
}

func names(checks []Check) []string {
	out := make([]string, len(checks))
	for i, c := range checks {
		out[i] = c.Name
	}
	return out
}

func find(t *testing.T, checks []Check, name string) Check {
	t.Helper()
	for _, c := range checks {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no check %q in %v", name, names(checks))
	return Check{}
}

var probeUserRe = regexp.MustCompile("`sso:__chui_probe_([0-9a-f]{8})`")

func TestProbeAllOK(t *testing.T) {
	cfg := perPersonConfig()
	cfg.SettingsProfile = "sso_profile"
	_, got, checks := probeSetup(t, cfg, probeResponder{address: "::ffff:10.1.2.3"})
	want := []string{checkSignIn, checkHost, checkShowUsers, checkCreate, "Grants role admin", "Grants role analyst", checkOutside, checkRole, checkDrop}
	if strings.Join(names(checks), "|") != strings.Join(want, "|") {
		t.Fatalf("checks = %v\nwant %v", names(checks), want)
	}
	if !AllOK(checks) {
		t.Fatalf("checks not all ok: %+v", checks)
	}
	if c := find(t, checks, checkSignIn); c.Detail != "signed in as verify_prov" {
		t.Errorf("sign-in detail = %q", c.Detail)
	}
	if c := find(t, checks, checkHost); c.Detail != "users will be pinned to 10.1.2.3" {
		t.Errorf("host detail = %q", c.Detail)
	}
	if c := find(t, checks, checkCreate); !strings.Contains(c.Detail, "settings profile sso_profile") {
		t.Errorf("create detail = %q", c.Detail)
	}

	stmts := got()
	if len(stmts) != 9 {
		t.Fatalf("got %d statements:\n%s", len(stmts), strings.Join(stmts, "\n"))
	}
	m := probeUserRe.FindStringSubmatch(stmts[3])
	if m == nil {
		t.Fatalf("no probe user in %q", stmts[3])
	}
	probe := "`sso:__chui_probe_" + m[1] + "`"
	h := hashRe.FindStringSubmatch(stmts[3])
	if h == nil {
		t.Fatalf("no sha256 hash in %q", stmts[3])
	}
	outsideHash := hashRe.FindStringSubmatch(stmts[6])
	if outsideHash == nil || outsideHash[1] == h[1] {
		t.Fatalf("outside create %q must carry its own hash", stmts[6])
	}
	wantSQL := []string{
		"SELECT currentUser() AS u",
		"SELECT toString(address) AS address FROM system.processes WHERE query_id = queryID()",
		"SELECT count() AS n FROM system.users WHERE name = 'sso:__chui_probe_" + m[1] + "'",
		"CREATE USER " + probe + " IDENTIFIED WITH sha256_hash BY '" + h[1] + "' HOST NONE SETTINGS PROFILE 'sso_profile'",
		"GRANT `admin` TO " + probe,
		"GRANT `analyst` TO " + probe,
		"CREATE USER `__chui_probe_outside_" + m[1] + "` IDENTIFIED WITH sha256_hash BY '" + outsideHash[1] + "' HOST NONE",
		"CREATE ROLE `__chui_probe_role_" + m[1] + "`",
		"DROP USER IF EXISTS " + probe,
	}
	for i := range wantSQL {
		if stmts[i] != wantSQL[i] {
			t.Errorf("statement %d\n got %q\nwant %q", i, stmts[i], wantSQL[i])
		}
	}
}

func TestProbeGrantRoleSetting(t *testing.T) {
	_, conn, agent, p := setupWith(t, perPersonConfig(), probeResponder{address: "10.0.0.1"}.respond)
	p.Probe(context.Background(), conn, perPersonConfig(), "verify_prov", "verify-pw")
	for _, m := range queries(agent, 0) {
		role := m.Settings["role"]
		switch {
		case strings.HasPrefix(m.SQL, "GRANT `admin`") && role != "admin",
			strings.HasPrefix(m.SQL, "GRANT `analyst`") && role != "analyst",
			!strings.HasPrefix(m.SQL, "GRANT") && role != "":
			t.Errorf("%q ran with role setting %q", m.SQL, role)
		}
	}
}

func TestProbeHostAnySkipsDetection(t *testing.T) {
	cfg := perPersonConfig()
	cfg.Host = database.SSOHostAny
	_, got, checks := probeSetup(t, cfg, probeResponder{})
	for _, n := range names(checks) {
		if n == checkHost {
			t.Fatal("host any must not report a host check")
		}
	}
	for _, sql := range got() {
		if strings.Contains(sql, "system.processes") {
			t.Fatal("host any must not detect the address")
		}
	}
	if !AllOK(checks) {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestProbeOutsideCreateTooBroad(t *testing.T) {
	_, got, checks := probeSetup(t, perPersonConfig(), probeResponder{address: "10.0.0.1", outsideAllowed: true})
	if AllOK(checks) {
		t.Fatal("a too broad grant passed")
	}
	c := find(t, checks, checkOutside)
	if c.OK || !strings.Contains(c.Detail, "provisioner can create users outside the prefix: its CREATE USER grant is too broad") || !strings.Contains(c.Detail, "was dropped again") {
		t.Fatalf("outside check = %+v", c)
	}
	stmts := got()
	var droppedOutside bool
	for _, sql := range stmts {
		if strings.HasPrefix(sql, "DROP USER IF EXISTS `__chui_probe_outside_") {
			droppedOutside = true
		}
	}
	if !droppedOutside {
		t.Fatalf("outside user not dropped:\n%s", strings.Join(stmts, "\n"))
	}
	if last := stmts[len(stmts)-1]; !strings.HasPrefix(last, "DROP USER IF EXISTS `sso:__chui_probe_") {
		t.Fatalf("probe user not dropped last: %q", last)
	}
	for _, n := range []string{checkSignIn, checkCreate, checkRole, checkDrop} {
		if !find(t, checks, n).OK {
			t.Errorf("%s failed: %+v", n, find(t, checks, n))
		}
	}
}

func TestProbeCreateRoleTooBroad(t *testing.T) {
	_, got, checks := probeSetup(t, perPersonConfig(), probeResponder{address: "10.0.0.1", roleAllowed: true})
	c := find(t, checks, checkRole)
	if c.OK || !strings.Contains(c.Detail, "should not hold CREATE ROLE") {
		t.Fatalf("role check = %+v", c)
	}
	var dropped bool
	for _, sql := range got() {
		if strings.HasPrefix(sql, "DROP ROLE IF EXISTS `__chui_probe_role_") {
			dropped = true
		}
	}
	if !dropped {
		t.Fatal("probe role not dropped")
	}
}

func TestProbeSignInFailsSkipsEverything(t *testing.T) {
	_, got, checks := probeSetup(t, perPersonConfig(), probeResponder{
		failSQL: "currentUser()",
		failErr: "Code: 516. DB::Exception: verify_prov: Authentication failed. (AUTHENTICATION_FAILED)",
	})
	if len(got()) != 1 {
		t.Fatalf("statements after a failed sign-in: %q", got())
	}
	if len(checks) != 9 {
		t.Fatalf("checks = %v", names(checks))
	}
	if c := checks[0]; c.OK || !strings.Contains(c.Detail, "AUTHENTICATION_FAILED") {
		t.Fatalf("sign-in check = %+v", c)
	}
	for _, c := range checks[1:] {
		if c.OK || c.Detail != skippedDetail {
			t.Errorf("%s = %+v, want skipped", c.Name, c)
		}
	}
}

func TestProbeCreateFailsSkipsGrantsAndDrop(t *testing.T) {
	_, got, checks := probeSetup(t, perPersonConfig(), probeResponder{
		address: "10.0.0.1",
		failSQL: "CREATE USER `sso:__chui_probe_",
		failErr: deniedErr,
	})
	c := find(t, checks, checkCreate)
	if c.OK || !strings.Contains(c.Detail, "GRANT CREATE USER, ALTER USER, DROP USER ON `sso:`* TO `verify_prov`") {
		t.Fatalf("create check = %+v", c)
	}
	for _, n := range []string{"Grants role admin", "Grants role analyst", checkDrop} {
		if c := find(t, checks, n); c.OK || c.Detail != skippedDetail {
			t.Errorf("%s = %+v, want skipped", n, c)
		}
	}
	for _, n := range []string{checkOutside, checkRole} {
		if !find(t, checks, n).OK {
			t.Errorf("%s should still run and pass: %+v", n, find(t, checks, n))
		}
	}
	stmts := got()
	if len(stmts) != 6 {
		t.Fatalf("got %d statements:\n%s", len(stmts), strings.Join(stmts, "\n"))
	}
	for _, sql := range stmts {
		if strings.HasPrefix(sql, "GRANT") || strings.HasPrefix(sql, "DROP USER") {
			t.Errorf("ran %q without a probe user", sql)
		}
	}
}

func TestProbeGrantFailsNamesAdminOption(t *testing.T) {
	_, got, checks := probeSetup(t, perPersonConfig(), probeResponder{
		address: "10.0.0.1",
		failSQL: "GRANT `admin`",
		failErr: "Code: 512. DB::Exception: Role should be granted to set as a current. (SET_NON_GRANTED_ROLE)",
	})
	c := find(t, checks, "Grants role admin")
	if c.OK || !strings.Contains(c.Detail, "GRANT `admin` TO `verify_prov` WITH ADMIN OPTION") || !strings.Contains(c.Detail, "SET_NON_GRANTED_ROLE") {
		t.Fatalf("grant check = %+v", c)
	}
	if !find(t, checks, "Grants role analyst").OK || !find(t, checks, checkDrop).OK {
		t.Fatalf("checks = %+v", checks)
	}
	if last := got()[len(got())-1]; !strings.HasPrefix(last, "DROP USER IF EXISTS `sso:__chui_probe_") {
		t.Fatalf("probe user not dropped after a failed grant: %q", last)
	}
}

func TestProbeShowUsersDenied(t *testing.T) {
	_, _, checks := probeSetup(t, perPersonConfig(), probeResponder{
		address: "10.0.0.1",
		failSQL: "system.users",
		failErr: deniedErr,
	})
	c := find(t, checks, checkShowUsers)
	if c.OK || !strings.Contains(c.Detail, "GRANT SHOW USERS ON *.* TO `verify_prov`") {
		t.Fatalf("show users check = %+v", c)
	}
	// Not a dependency: the probe user is still created and dropped.
	if !find(t, checks, checkCreate).OK || !find(t, checks, checkDrop).OK {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestProbeHostDetectionDenied(t *testing.T) {
	_, _, checks := probeSetup(t, perPersonConfig(), probeResponder{
		failSQL: "system.processes",
		failErr: deniedErr,
	})
	c := find(t, checks, checkHost)
	if c.OK || !strings.Contains(c.Detail, "SELECT(address, query_id) ON system.processes") {
		t.Fatalf("host check = %+v", c)
	}
	if !find(t, checks, checkCreate).OK {
		t.Fatalf("checks = %+v", checks)
	}
}

func TestAllOK(t *testing.T) {
	if !AllOK(nil) || !AllOK([]Check{{OK: true}}) || AllOK([]Check{{OK: true}, {OK: false}}) {
		t.Fatal("AllOK is wrong")
	}
}
