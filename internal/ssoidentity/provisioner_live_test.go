// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package ssoidentity

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// TestSSOProvisionLive runs the whole per-person lifecycle against a real
// ClickHouse: the admin setup from SetupSQL, sign-in, login as the created
// user, role convergence, the hijack refusal, lock and drop.
func TestSSOProvisionLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	adminUser, adminPassword := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if adminUser == "" {
		adminUser = "default"
	}
	admin := func(sql string) ([]map[string]any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, adminUser, adminPassword, nil)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(res.Data)
		var rows []map[string]any
		_ = json.Unmarshal(raw, &rows)
		return rows, nil
	}
	userCount := func(name string) int64 {
		rows, err := admin("SELECT count() AS n FROM system.users WHERE name = " + safe.QuotedLiteral(name))
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) == 0 {
			return 0
		}
		return toInt(rows[0]["n"])
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	prefix := "sso" + suffix + ":"
	roleA, roleB := "chui_sso_a_"+suffix, "chui_sso_b_"+suffix
	profile := "chui_sso_profile_" + suffix
	prov := "chui_sso_prov_" + suffix
	anaUser, otherUser := prefix+"ana@x.com", prefix+"other@x.com"
	// Registered before anything is created, so a failure midway still
	// cleans up.
	t.Cleanup(func() {
		for _, sql := range []string{
			"DROP USER IF EXISTS " + safe.Identifier(anaUser),
			"DROP USER IF EXISTS " + safe.Identifier(otherUser),
			"DROP USER IF EXISTS " + safe.Identifier(prov),
			"DROP ROLE IF EXISTS " + safe.Identifier(roleA),
			"DROP ROLE IF EXISTS " + safe.Identifier(roleB),
			"DROP SETTINGS PROFILE IF EXISTS " + safe.Identifier(profile),
		} {
			if _, err := admin(sql); err != nil {
				t.Error(err)
			}
		}
	})

	cfg := database.SSOIdentityConfig{
		Mode:            database.SSOModePerPerson,
		UserPrefix:      prefix,
		SettingsProfile: profile,
		Host:            database.SSOHostAny,
		RoleMappings: []database.SSORoleMapping{
			{Group: "a", Roles: []string{roleA}},
			{Group: "b", Roles: []string{roleB}},
		},
	}
	provPassword := "prov-" + suffix
	for _, sql := range setupStatements(SetupSQL(cfg, prov), provPassword) {
		if _, err := admin(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	db, conn := testutil.WorkerDB(t)
	enc, err := crypto.Encrypt(provPassword, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(conn, prov, enc); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSSOIdentityConfig(conn, cfg); err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewWorkerAgent(t, db, nil)
	p := &Provisioner{DB: db, Gateway: agent.Gateway, Secret: testSecret}
	ctx := context.Background()

	// whoami logs in through the gateway as a created user and returns
	// currentUser() and the sorted currentRoles().
	whoami := func(user, password string) (string, []string, error) {
		qctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		res, err := agent.Gateway.ExecuteQueryWithSettingsCtx(qctx, conn, "SELECT currentUser() AS u, currentRoles() AS r", user, password, nil, 10*time.Second)
		if err != nil {
			return "", nil, err
		}
		rows, err := decodeRows(res)
		if err != nil || len(rows) != 1 {
			return "", nil, fmt.Errorf("rows = %v, err = %v", rows, err)
		}
		u, _ := rows[0]["u"].(string)
		var roles []string
		if list, ok := rows[0]["r"].([]any); ok {
			for _, r := range list {
				roles = append(roles, fmt.Sprint(r))
			}
		}
		sort.Strings(roles)
		return u, roles, nil
	}

	res, err := p.SignIn(ctx, conn, Person{Issuer: "https://idp.example", Subject: "sub-ana", Email: "Ana@X.com", Name: "Ana", Groups: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.CHUser != anaUser || strings.Join(res.Roles, ",") != roleA {
		t.Fatalf("result = %+v", res)
	}
	firstPassword, err := crypto.Decrypt(res.EncryptedPassword, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	u, roles, err := whoami(anaUser, firstPassword)
	if err != nil {
		t.Fatalf("login as %s: %v", anaUser, err)
	}
	if u != anaUser || strings.Join(roles, ",") != roleA {
		t.Fatalf("currentUser() = %q, currentRoles() = %v", u, roles)
	}
	rows, err := admin("SELECT count() AS n FROM system.settings_profile_elements WHERE user_name = " + safe.QuotedLiteral(anaUser) + " AND inherit_profile = " + safe.QuotedLiteral(profile))
	if err != nil || len(rows) != 1 || toInt(rows[0]["n"]) != 1 {
		t.Errorf("profile not assigned: rows = %v, err = %v", rows, err)
	}

	// Group change: roles converge and the old password stops working.
	res, err = p.SignIn(ctx, conn, Person{Issuer: "https://idp.example", Subject: "sub-ana", Email: "ana@x.com", Name: "Ana", Groups: []string{"b"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || strings.Join(res.Roles, ",") != roleB {
		t.Fatalf("result = %+v", res)
	}
	secondPassword, err := crypto.Decrypt(res.EncryptedPassword, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if secondPassword == firstPassword {
		t.Fatal("password was not rotated")
	}
	if _, _, err := whoami(anaUser, firstPassword); err == nil {
		t.Fatal("old password still logs in")
	}
	u, roles, err = whoami(anaUser, secondPassword)
	if err != nil {
		t.Fatalf("login after re-sync: %v", err)
	}
	if u != anaUser || strings.Join(roles, ",") != roleB {
		t.Fatalf("currentUser() = %q, currentRoles() = %v, want only %s", u, roles, roleB)
	}

	// A pre-existing user with the name CH-UI would pick is never taken over.
	if _, err := admin("CREATE USER " + safe.Identifier(otherUser) + " IDENTIFIED BY 'pre-existing' HOST ANY"); err != nil {
		t.Fatal(err)
	}
	_, err = p.SignIn(ctx, conn, Person{Issuer: "https://idp.example", Subject: "sub-other", Email: "other@x.com", Groups: []string{"a"}})
	var uee *UserExistsError
	if !errors.As(err, &uee) || uee.CHUser != otherUser {
		t.Fatalf("err = %v, want *UserExistsError for %s", err, otherUser)
	}
	if _, _, err := whoami(otherUser, "pre-existing"); err != nil {
		t.Fatalf("pre-existing user was altered: %v", err)
	}

	users, err := db.ListSSOClickHouseUsers(conn)
	if err != nil || len(users) != 1 {
		t.Fatalf("users = %v, err = %v", users, err)
	}
	if err := p.Lock(ctx, users[0], "admin@x.com"); err != nil {
		t.Fatal(err)
	}
	if _, _, err := whoami(anaUser, secondPassword); err == nil {
		t.Fatal("locked user still logs in")
	}
	if n := userCount(anaUser); n != 1 {
		t.Fatalf("locked user count = %d, want 1 (kept for query_log attribution)", n)
	}

	if err := p.Drop(ctx, users[0], "system"); err != nil {
		t.Fatal(err)
	}
	if n := userCount(anaUser); n != 0 {
		t.Fatalf("dropped user count = %d, want 0", n)
	}
	if left, err := db.ListSSOClickHouseUsers(conn); err != nil || len(left) != 0 {
		t.Fatalf("users after drop = %v, err = %v", left, err)
	}

	// Host "auto": the provisioner's SELECT(address, query_id) grant from
	// SetupSQL lets it pin the user to the address ClickHouse sees it from,
	// which here is the same address the person connects from.
	cfg.Host = database.SSOHostAuto
	if err := db.SetSSOIdentityConfig(conn, cfg); err != nil {
		t.Fatal(err)
	}
	res, err = p.SignIn(ctx, conn, Person{Issuer: "https://idp.example", Subject: "sub-ana", Email: "ana@x.com", Name: "Ana", Groups: []string{"a"}})
	if err != nil {
		t.Fatal(err)
	}
	autoPassword, err := crypto.Decrypt(res.EncryptedPassword, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if u, _, err := whoami(anaUser, autoPassword); err != nil || u != anaUser {
		t.Fatalf("login with host auto: user = %q, err = %v", u, err)
	}
	rows, err = admin("SELECT host_ip FROM system.users WHERE name = " + safe.QuotedLiteral(anaUser))
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
	if ips, _ := rows[0]["host_ip"].([]any); len(ips) != 1 || strings.Contains(fmt.Sprint(ips[0]), "::ffff:") {
		t.Fatalf("host_ip = %v, want exactly one plain address", rows[0]["host_ip"])
	}
}

// setupStatements turns SetupSQL output into runnable statements: comments
// stripped, the password placeholder filled in.
func setupStatements(script, provPassword string) []string {
	var lines []string
	for _, l := range strings.Split(script, "\n") {
		if t := strings.TrimSpace(l); t != "" && !strings.HasPrefix(t, "--") {
			lines = append(lines, t)
		}
	}
	var out []string
	for _, stmt := range strings.Split(strings.Join(lines, "\n"), ";") {
		stmt = strings.TrimSpace(stmt)
		if stmt == "" {
			continue
		}
		out = append(out, strings.Replace(stmt, "'<choose a password>'", safe.QuotedLiteral(provPassword), 1))
	}
	return out
}

// TestSSOProbeLive runs Probe against a real ClickHouse: a provisioner set up
// exactly by SetupSQL passes every check and leaves nothing behind; one with
// a global CREATE USER grant fails the "outside the prefix" check, and the
// user it managed to create is dropped again.
func TestSSOProbeLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	adminUser, adminPassword := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if adminUser == "" {
		adminUser = "default"
	}
	admin := func(sql string) ([]map[string]any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, adminUser, adminPassword, nil)
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(res.Data)
		var rows []map[string]any
		_ = json.Unmarshal(raw, &rows)
		return rows, nil
	}
	leftovers := func(like string) []string {
		rows, err := admin("SELECT name FROM system.users WHERE name LIKE " + safe.QuotedLiteral(like) + " ORDER BY name")
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, r := range rows {
			names = append(names, fmt.Sprint(r["name"]))
		}
		return names
	}

	suffix := fmt.Sprintf("%d", time.Now().UnixNano())
	prefix := "sso" + suffix + ":"
	roleA, roleB := "chui_sso_a_"+suffix, "chui_sso_b_"+suffix
	profile := "chui_sso_profile_" + suffix
	prov := "chui_sso_prov_" + suffix
	t.Cleanup(func() {
		for _, name := range leftovers("%chui_probe%") {
			t.Errorf("probe left user %s behind", name)
			if _, err := admin("DROP USER IF EXISTS " + safe.Identifier(name)); err != nil {
				t.Error(err)
			}
		}
		for _, sql := range []string{
			"DROP USER IF EXISTS " + safe.Identifier(prov),
			"DROP ROLE IF EXISTS " + safe.Identifier(roleA),
			"DROP ROLE IF EXISTS " + safe.Identifier(roleB),
			"DROP SETTINGS PROFILE IF EXISTS " + safe.Identifier(profile),
		} {
			if _, err := admin(sql); err != nil {
				t.Error(err)
			}
		}
	})

	cfg := database.SSOIdentityConfig{
		Mode:            database.SSOModePerPerson,
		UserPrefix:      prefix,
		SettingsProfile: profile,
		Host:            database.SSOHostAuto,
		RoleMappings: []database.SSORoleMapping{
			{Group: "a", Roles: []string{roleA}},
			{Group: "b", Roles: []string{roleB}},
		},
	}
	provPassword := "prov-" + suffix
	for _, sql := range setupStatements(SetupSQL(cfg, prov), provPassword) {
		if _, err := admin(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	// The account is deliberately not saved on the connection: Probe takes
	// it explicitly, as the verify endpoint does before the admin saves it.
	p := &Provisioner{DB: db, Gateway: agent.Gateway, Secret: testSecret}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	checks := p.Probe(ctx, conn, cfg, prov, provPassword)
	if !AllOK(checks) {
		t.Fatalf("a provisioner set up by SetupSQL failed checks: %+v", checks)
	}
	want := []string{checkSignIn, checkHost, checkShowUsers, checkCreate, checkGrant(roleA), checkGrant(roleB), checkOutside, checkRole, checkDrop}
	if strings.Join(names(checks), "|") != strings.Join(want, "|") {
		t.Fatalf("checks = %v\nwant %v", names(checks), want)
	}
	if c := find(t, checks, checkSignIn); c.Detail != "signed in as "+prov {
		t.Errorf("sign-in detail = %q", c.Detail)
	}
	if c := find(t, checks, checkHost); !strings.HasPrefix(c.Detail, "users will be pinned to ") || strings.Contains(c.Detail, "::ffff:") {
		t.Errorf("host detail = %q", c.Detail)
	}
	if left := leftovers("%chui_probe%"); len(left) != 0 {
		t.Fatalf("probe left users behind: %v", left)
	}

	// Wrong password: only the sign-in runs, everything else is skipped.
	checks = p.Probe(ctx, conn, cfg, prov, "not-the-password")
	if len(checks) != len(want) || checks[0].OK {
		t.Fatalf("checks with a wrong password = %+v", checks)
	}
	for _, c := range checks[1:] {
		if c.OK || c.Detail != skippedDetail {
			t.Errorf("%s = %+v, want skipped", c.Name, c)
		}
	}

	// Too broad: a global CREATE USER (and DROP USER, so the probe can clean
	// up) grant lets the provisioner create users outside the prefix.
	if _, err := admin("GRANT CREATE USER, DROP USER ON *.* TO " + safe.Identifier(prov)); err != nil {
		t.Fatal(err)
	}
	checks = p.Probe(ctx, conn, cfg, prov, provPassword)
	if AllOK(checks) {
		t.Fatalf("a global CREATE USER grant passed: %+v", checks)
	}
	c := find(t, checks, checkOutside)
	if c.OK || !strings.Contains(c.Detail, "too broad") || !strings.Contains(c.Detail, "was dropped again") {
		t.Fatalf("outside check = %+v", c)
	}
	for _, n := range []string{checkSignIn, checkCreate, checkGrant(roleA), checkGrant(roleB), checkRole, checkDrop} {
		if !find(t, checks, n).OK {
			t.Errorf("%s = %+v, want ok", n, find(t, checks, n))
		}
	}
	if left := leftovers("%chui_probe%"); len(left) != 0 {
		t.Fatalf("probe left users behind: %v", left)
	}
}
