// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package ssoidentity

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const testSecret = "test-secret"

// responder answers the provisioner's statements like ClickHouse would:
// usersCount for the hijack check, address for host detection, no rows for
// everything else. failSQL makes every statement containing it fail with
// failErr.
type responder struct {
	usersCount int
	address    string
	failSQL    string
	failErr    string
}

func (r responder) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	if r.failSQL != "" && strings.Contains(msg.SQL, r.failSQL) {
		return &tunnel.AgentMessage{Type: "query_error", Error: r.failErr}
	}
	data := "[]"
	switch {
	case strings.Contains(msg.SQL, "system.users"):
		data = `[{"n":` + jsonInt(r.usersCount) + `}]`
	case strings.Contains(msg.SQL, "system.processes"):
		data = `[{"address":"` + r.address + `"}]`
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data), Meta: json.RawMessage("[]")}
}

func jsonInt(n int) string {
	b, _ := json.Marshal(n)
	return string(b)
}

func perPersonConfig() database.SSOIdentityConfig {
	return database.SSOIdentityConfig{
		Mode:       database.SSOModePerPerson,
		UserPrefix: "sso:",
		Host:       database.SSOHostAuto,
		RoleMappings: []database.SSORoleMapping{
			{Group: "analysts", Roles: []string{"analyst"}},
			{Group: "admins", Roles: []string{"admin", "analyst"}},
		},
	}
}

// setup gives a worker DB with a provisioning account and cfg saved on the
// connection, a fake agent answering with r, and the provisioner.
func setup(t *testing.T, cfg database.SSOIdentityConfig, r responder) (*database.DB, string, *testutil.Agent, *Provisioner) {
	t.Helper()
	return setupWith(t, cfg, r.respond)
}

// setupWith is setup with any agent responder.
func setupWith(t *testing.T, cfg database.SSOIdentityConfig, respond func(tunnel.GatewayMessage) *tunnel.AgentMessage) (*database.DB, string, *testutil.Agent, *Provisioner) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	enc, err := crypto.Encrypt("prov-pw", testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(conn, "chui_prov", enc); err != nil {
		t.Fatal(err)
	}
	if err := db.SetSSOIdentityConfig(conn, cfg); err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "worker-token", respond)
	return db, conn, agent, &Provisioner{DB: db, Gateway: agent.Gateway, Secret: testSecret}
}

// queries returns the SQL statements the agent received since index from.
func queries(agent *testutil.Agent, from int) []tunnel.GatewayMessage {
	var out []tunnel.GatewayMessage
	for _, m := range agent.Messages() {
		if m.Type == "query" {
			out = append(out, m)
		}
	}
	if from > len(out) {
		return nil
	}
	return out[from:]
}

func sqls(msgs []tunnel.GatewayMessage) []string {
	out := make([]string, len(msgs))
	for i, m := range msgs {
		out[i] = m.SQL
	}
	return out
}

var hashRe = regexp.MustCompile(`BY '([0-9a-f]{64})'`)

func ana() Person {
	return Person{Issuer: "https://idp.example", Subject: "sub-ana", Email: "Ana@X.com", Name: "Ana", Groups: []string{"analysts"}}
}

func TestSignInSharedMode(t *testing.T) {
	_, conn, _, p := setup(t, database.DefaultSSOIdentityConfig(), responder{})
	_, err := p.SignIn(context.Background(), conn, ana())
	if !errors.Is(err, ErrSharedMode) {
		t.Fatalf("err = %v, want ErrSharedMode", err)
	}
}

func TestSignInNoRoles(t *testing.T) {
	_, conn, agent, p := setup(t, perPersonConfig(), responder{})
	person := ana()
	person.Groups = []string{"nobody"}
	_, err := p.SignIn(context.Background(), conn, person)
	var nre *NoRolesError
	if !errors.As(err, &nre) || nre.ConnectionID != conn {
		t.Fatalf("err = %v, want *NoRolesError for %s", err, conn)
	}
	if n := len(queries(agent, 0)); n != 0 {
		t.Fatalf("%d statements sent, want none", n)
	}
}

func TestSignInProvisionsUser(t *testing.T) {
	db, conn, agent, p := setup(t, perPersonConfig(), responder{address: "::ffff:10.1.2.3"})
	res, err := p.SignIn(context.Background(), conn, ana())
	if err != nil {
		t.Fatal(err)
	}
	if !res.Created || res.CHUser != "sso:ana@x.com" || strings.Join(res.Roles, ",") != "analyst" || res.IdentityID == "" {
		t.Fatalf("result = %+v", res)
	}

	msgs := queries(agent, 0)
	got := sqls(msgs)
	if len(got) != 6 {
		t.Fatalf("got %d statements:\n%s", len(got), strings.Join(got, "\n"))
	}
	m := hashRe.FindStringSubmatch(got[2])
	if m == nil {
		t.Fatalf("no sha256 hash in %q", got[2])
	}
	hash := m[1]
	want := []string{
		"SELECT count() AS n FROM system.users WHERE name = 'sso:ana@x.com'",
		"SELECT toString(address) AS address FROM system.processes WHERE query_id = queryID()",
		"CREATE USER IF NOT EXISTS `sso:ana@x.com` IDENTIFIED WITH sha256_hash BY '" + hash + "' HOST IP '10.1.2.3'",
		"ALTER USER `sso:ana@x.com` IDENTIFIED WITH sha256_hash BY '" + hash + "' HOST IP '10.1.2.3'",
		"GRANT `analyst` TO `sso:ana@x.com`",
		"ALTER USER `sso:ana@x.com` DEFAULT ROLE ALL",
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("statement %d\n got %q\nwant %q", i, got[i], want[i])
		}
	}
	for i, msg := range msgs {
		if msg.User != "chui_prov" || msg.Password != "prov-pw" {
			t.Errorf("statement %d ran as %s, want the provisioner", i, msg.User)
		}
		role := msg.Settings["role"]
		if i == 4 && role != "analyst" {
			t.Errorf("GRANT ran with role setting %q, want analyst", role)
		}
		if i != 4 && role != "" {
			t.Errorf("statement %d ran with role setting %q, want none", i, role)
		}
	}

	// The saved password hashes to what went into ClickHouse and never
	// appears in plain text in any statement.
	row, err := db.GetSSOClickHouseUser(res.IdentityID, conn)
	if err != nil || row == nil {
		t.Fatalf("row = %v, err = %v", row, err)
	}
	if row.EncryptedPassword != res.EncryptedPassword || row.CHUser != res.CHUser || strings.Join(row.Roles, ",") != "analyst" {
		t.Fatalf("saved row = %+v", row)
	}
	password, err := crypto.Decrypt(row.EncryptedPassword, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if len(password) != 64 {
		t.Fatalf("password has %d chars, want 64 hex", len(password))
	}
	sum := sha256.Sum256([]byte(password))
	if hex.EncodeToString(sum[:]) != hash {
		t.Fatal("sha256 of the saved password differs from the hash sent to ClickHouse")
	}
	for _, sql := range got {
		if strings.Contains(sql, password) {
			t.Fatal("plaintext password appears in a statement")
		}
	}

	ident, err := db.GetSSOIdentity(res.IdentityID)
	if err != nil || ident == nil || ident.Email != "ana@x.com" || ident.Subject != "sub-ana" {
		t.Fatalf("identity = %+v, err = %v", ident, err)
	}

	logs, err := db.GetAuditLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, l := range logs {
		if l.Action == "sso.user_provisioned" {
			found = true
			if l.Username == nil || *l.Username != "ana@x.com" || l.ChUser == nil || *l.ChUser != "sso:ana@x.com" || l.ConnectionID == nil || *l.ConnectionID != conn {
				t.Errorf("audit row = %+v", l)
			}
			var details map[string]any
			if l.Details == nil || json.Unmarshal([]byte(*l.Details), &details) != nil {
				t.Fatalf("audit details = %v", l.Details)
			}
			if details["created"] != true || len(details["roles_before"].([]any)) != 0 || len(details["roles_after"].([]any)) != 1 {
				t.Errorf("audit details = %v", details)
			}
		}
	}
	if !found {
		t.Fatal("no sso.user_provisioned audit entry")
	}
}

func TestSignInSyncsRoles(t *testing.T) {
	db, conn, agent, p := setup(t, perPersonConfig(), responder{address: "10.1.2.3"})
	ctx := context.Background()
	if _, err := p.SignIn(ctx, conn, ana()); err != nil {
		t.Fatal(err)
	}
	first := len(queries(agent, 0))

	// Promoted: gains admin, keeps analyst, nothing revoked. The address is
	// cached, so system.processes is not asked again, and the user exists
	// in our table, so system.users is not checked again.
	admin := ana()
	admin.Groups = []string{"admins"}
	res, err := p.SignIn(ctx, conn, admin)
	if err != nil {
		t.Fatal(err)
	}
	if res.Created || strings.Join(res.Roles, ",") != "admin,analyst" {
		t.Fatalf("result = %+v", res)
	}
	got := queries(agent, first)
	want := []string{
		"CREATE USER IF NOT EXISTS `sso:ana@x.com` IDENTIFIED WITH sha256_hash BY '%s' HOST IP '10.1.2.3'",
		"ALTER USER `sso:ana@x.com` IDENTIFIED WITH sha256_hash BY '%s' HOST IP '10.1.2.3'",
		"GRANT `admin` TO `sso:ana@x.com`",
		"GRANT `analyst` TO `sso:ana@x.com`",
		"ALTER USER `sso:ana@x.com` DEFAULT ROLE ALL",
	}
	assertStatements(t, got, want)
	if got[2].Settings["role"] != "admin" || got[3].Settings["role"] != "analyst" {
		t.Errorf("GRANT role settings = %q, %q", got[2].Settings["role"], got[3].Settings["role"])
	}
	second := first + len(got)

	// Demoted: admin is managed and no longer mapped, so it is revoked with
	// admin active; analyst is re-granted.
	res, err = p.SignIn(ctx, conn, ana())
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Roles, ",") != "analyst" {
		t.Fatalf("result = %+v", res)
	}
	got = queries(agent, second)
	want = []string{
		"CREATE USER IF NOT EXISTS `sso:ana@x.com` IDENTIFIED WITH sha256_hash BY '%s' HOST IP '10.1.2.3'",
		"ALTER USER `sso:ana@x.com` IDENTIFIED WITH sha256_hash BY '%s' HOST IP '10.1.2.3'",
		"REVOKE `admin` FROM `sso:ana@x.com`",
		"GRANT `analyst` TO `sso:ana@x.com`",
		"ALTER USER `sso:ana@x.com` DEFAULT ROLE ALL",
	}
	assertStatements(t, got, want)
	if got[2].Settings["role"] != "admin" {
		t.Errorf("REVOKE ran with role setting %q, want admin", got[2].Settings["role"])
	}
	third := second + len(got)

	// A role CH-UI did not hand out is never revoked, even when the person
	// loses it in our table: only managed roles are touched.
	row, err := db.GetSSOClickHouseUser(res.IdentityID, conn)
	if err != nil {
		t.Fatal(err)
	}
	row.Roles = []string{"analyst", "legacy_dba"}
	if err := db.SaveSSOClickHouseUser(*row); err != nil {
		t.Fatal(err)
	}
	if _, err := p.SignIn(ctx, conn, ana()); err != nil {
		t.Fatal(err)
	}
	for _, sql := range sqls(queries(agent, third)) {
		if strings.HasPrefix(sql, "REVOKE") {
			t.Errorf("unmanaged role revoked: %s", sql)
		}
	}

	logs, err := db.GetAuditLogs(20)
	if err != nil {
		t.Fatal(err)
	}
	var synced int
	for _, l := range logs {
		if l.Action == "sso.user_synced" {
			synced++
		}
	}
	if synced != 3 {
		t.Errorf("%d sso.user_synced audit entries, want 3", synced)
	}
}

func assertStatements(t *testing.T, got []tunnel.GatewayMessage, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %d statements, want %d:\n%s", len(got), len(want), strings.Join(sqls(got), "\n"))
	}
	for i := range want {
		w := want[i]
		if strings.Contains(w, "%s") {
			m := hashRe.FindStringSubmatch(got[i].SQL)
			if m == nil {
				t.Fatalf("statement %d has no sha256 hash: %q", i, got[i].SQL)
			}
			w = strings.Replace(w, "%s", m[1], 1)
		}
		if got[i].SQL != w {
			t.Errorf("statement %d\n got %q\nwant %q", i, got[i].SQL, w)
		}
	}
}

func TestSignInRefusesExistingUser(t *testing.T) {
	db, conn, agent, p := setup(t, perPersonConfig(), responder{usersCount: 1, address: "10.1.2.3"})
	_, err := p.SignIn(context.Background(), conn, ana())
	var uee *UserExistsError
	if !errors.As(err, &uee) || uee.CHUser != "sso:ana@x.com" {
		t.Fatalf("err = %v, want *UserExistsError", err)
	}
	got := sqls(queries(agent, 0))
	if len(got) != 1 || !strings.HasPrefix(got[0], "SELECT count() AS n FROM system.users") {
		t.Fatalf("statements after refusal: %q", got)
	}
	users, err := db.ListSSOClickHouseUsers(conn)
	if err != nil || len(users) != 0 {
		t.Fatalf("users = %v, err = %v", users, err)
	}
}

func TestSignInRefusesUserOfAnotherIdentity(t *testing.T) {
	_, conn, agent, p := setup(t, perPersonConfig(), responder{address: "10.1.2.3"})
	ctx := context.Background()
	if _, err := p.SignIn(ctx, conn, ana()); err != nil {
		t.Fatal(err)
	}
	before := len(queries(agent, 0))
	// Same email from another issuer: a different person as far as the IdP
	// is concerned, who must not get Ana's user.
	twin := ana()
	twin.Issuer = "https://other-idp.example"
	_, err := p.SignIn(ctx, conn, twin)
	var uee *UserExistsError
	if !errors.As(err, &uee) {
		t.Fatalf("err = %v, want *UserExistsError", err)
	}
	if n := len(queries(agent, before)); n != 0 {
		t.Fatalf("%d statements sent for the refused twin, want none", n)
	}
}

func TestSignInNeedsShowUsers(t *testing.T) {
	_, conn, _, p := setup(t, perPersonConfig(), responder{
		failSQL: "system.users",
		failErr: "Code: 497. DB::Exception: chui_prov: Not enough privileges. (ACCESS_DENIED)",
	})
	_, err := p.SignIn(context.Background(), conn, ana())
	if err == nil || !strings.Contains(err.Error(), "GRANT SHOW USERS ON *.*") {
		t.Fatalf("err = %v, want the SHOW USERS hint", err)
	}
}

func TestSignInHostModes(t *testing.T) {
	t.Run("any", func(t *testing.T) {
		cfg := perPersonConfig()
		cfg.Host = database.SSOHostAny
		cfg.SettingsProfile = "sso_profile"
		_, conn, agent, p := setup(t, cfg, responder{})
		if _, err := p.SignIn(context.Background(), conn, ana()); err != nil {
			t.Fatal(err)
		}
		got := sqls(queries(agent, 0))
		for _, sql := range got {
			if strings.Contains(sql, "system.processes") {
				t.Error("host any must not detect the address")
			}
		}
		create := got[1]
		if !strings.HasSuffix(create, "' HOST ANY SETTINGS PROFILE 'sso_profile'") || !strings.HasPrefix(create, "CREATE USER IF NOT EXISTS `sso:ana@x.com` IDENTIFIED WITH sha256_hash BY '") {
			t.Errorf("create = %q", create)
		}
		if alter := got[2]; !strings.HasSuffix(alter, "' HOST ANY SETTINGS PROFILE 'sso_profile'") {
			t.Errorf("alter = %q", alter)
		}
	})
	t.Run("ip list", func(t *testing.T) {
		cfg := perPersonConfig()
		cfg.Host = "10.0.0.1, 10.0.0.2"
		_, conn, agent, p := setup(t, cfg, responder{})
		if _, err := p.SignIn(context.Background(), conn, ana()); err != nil {
			t.Fatal(err)
		}
		if create := sqls(queries(agent, 0))[1]; !strings.HasSuffix(create, "' HOST IP '10.0.0.1', IP '10.0.0.2'") {
			t.Errorf("create = %q", create)
		}
	})
	t.Run("auto without address", func(t *testing.T) {
		_, conn, _, p := setup(t, perPersonConfig(), responder{
			failSQL: "system.processes",
			failErr: "Code: 497. DB::Exception: Not enough privileges. (ACCESS_DENIED)",
		})
		_, err := p.SignIn(context.Background(), conn, ana())
		if err == nil || !strings.Contains(err.Error(), "SELECT(address, query_id) ON system.processes") {
			t.Fatalf("err = %v, want the system.processes hint", err)
		}
	})
}

func TestSignInStopsAtFirstFailure(t *testing.T) {
	db, conn, agent, p := setup(t, perPersonConfig(), responder{
		address: "10.1.2.3",
		failSQL: "GRANT",
		failErr: "Code: 511. DB::Exception: Not enough privileges. (ACCESS_DENIED)",
	})
	res, err := p.SignIn(context.Background(), conn, ana())
	if err == nil || res != nil || !strings.Contains(err.Error(), "grant role analyst") {
		t.Fatalf("res = %v, err = %v", res, err)
	}
	got := sqls(queries(agent, 0))
	if len(got) != 5 || !strings.HasPrefix(got[4], "GRANT") {
		t.Fatalf("statements: %q", got)
	}
	users, err := db.ListSSOClickHouseUsers(conn)
	if err != nil || len(users) != 0 {
		t.Fatalf("a failed sign-in saved a user: %v %v", users, err)
	}
}

func TestSignInIdentity(t *testing.T) {
	db, conn, _, p := setup(t, perPersonConfig(), responder{address: "10.1.2.3"})
	ctx := context.Background()
	if _, err := p.SignInIdentity(ctx, conn, "missing"); err == nil || !strings.Contains(err.Error(), "unknown identity") {
		t.Fatalf("err = %v", err)
	}
	ident, err := db.UpsertSSOIdentity("https://idp.example", "sub-bob", "bob@x.com", "Bob", []string{"admins"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := p.SignInIdentity(ctx, conn, ident.ID)
	if err != nil {
		t.Fatal(err)
	}
	if res.IdentityID != ident.ID || res.CHUser != "sso:bob@x.com" || strings.Join(res.Roles, ",") != "admin,analyst" {
		t.Fatalf("result = %+v", res)
	}
}

func TestLockAndDrop(t *testing.T) {
	db, conn, agent, p := setup(t, perPersonConfig(), responder{address: "10.1.2.3"})
	ctx := context.Background()
	if _, err := p.SignIn(ctx, conn, ana()); err != nil {
		t.Fatal(err)
	}
	users, err := db.ListSSOClickHouseUsers(conn)
	if err != nil || len(users) != 1 {
		t.Fatalf("users = %v, err = %v", users, err)
	}
	u := users[0]

	before := len(queries(agent, 0))
	if err := p.Lock(ctx, u, "admin@x.com"); err != nil {
		t.Fatal(err)
	}
	got := sqls(queries(agent, before))
	if len(got) != 1 || got[0] != "ALTER USER `sso:ana@x.com` HOST NONE" {
		t.Fatalf("lock statements: %q", got)
	}
	row, err := db.GetSSOClickHouseUser(u.IdentityID, conn)
	if err != nil || row == nil || !row.Locked() {
		t.Fatalf("row after lock = %+v, err = %v", row, err)
	}

	// Signing in again re-opens the user: HOST is set again and the row is
	// unlocked.
	before = len(queries(agent, 0))
	if _, err := p.SignIn(ctx, conn, ana()); err != nil {
		t.Fatal(err)
	}
	if alter := sqls(queries(agent, before))[1]; !strings.HasSuffix(alter, "HOST IP '10.1.2.3'") {
		t.Errorf("alter after lock = %q", alter)
	}
	row, err = db.GetSSOClickHouseUser(u.IdentityID, conn)
	if err != nil || row == nil || row.Locked() {
		t.Fatalf("row after re-sign-in = %+v, err = %v", row, err)
	}

	before = len(queries(agent, 0))
	if err := p.Drop(ctx, u, "system"); err != nil {
		t.Fatal(err)
	}
	got = sqls(queries(agent, before))
	if len(got) != 1 || got[0] != "DROP USER IF EXISTS `sso:ana@x.com`" {
		t.Fatalf("drop statements: %q", got)
	}
	row, err = db.GetSSOClickHouseUser(u.IdentityID, conn)
	if err != nil || row != nil {
		t.Fatalf("row after drop = %+v, err = %v", row, err)
	}

	logs, err := db.GetAuditLogs(20)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]database.AuditLog{}
	for _, l := range logs {
		seen[l.Action] = l
	}
	lock, ok := seen["sso.user_locked"]
	if !ok || lock.Username == nil || *lock.Username != "admin@x.com" || lock.ChUser == nil || *lock.ChUser != "sso:ana@x.com" || lock.Details == nil || !strings.Contains(*lock.Details, "ana@x.com") {
		t.Errorf("lock audit = %+v", lock)
	}
	drop, ok := seen["sso.user_dropped"]
	if !ok || drop.Username == nil || *drop.Username != "system" || drop.ChUser == nil || *drop.ChUser != "sso:ana@x.com" {
		t.Errorf("drop audit = %+v", drop)
	}
}

func TestLockSharedMode(t *testing.T) {
	_, conn, _, p := setup(t, database.DefaultSSOIdentityConfig(), responder{})
	u := database.SSOPersonUser{SSOClickHouseUser: database.SSOClickHouseUser{IdentityID: "i", ConnectionID: conn, CHUser: "sso:x"}}
	if err := p.Lock(context.Background(), u, "system"); !errors.Is(err, ErrSharedMode) {
		t.Fatalf("err = %v, want ErrSharedMode", err)
	}
}

func TestSetupSQL(t *testing.T) {
	cfg := perPersonConfig()
	cfg.SettingsProfile = "sso_profile"
	sql := SetupSQL(cfg, "chui_prov")
	for _, want := range []string{
		"CREATE ROLE IF NOT EXISTS `admin`;",
		"CREATE ROLE IF NOT EXISTS `analyst`;",
		"CREATE SETTINGS PROFILE IF NOT EXISTS `sso_profile` SETTINGS max_memory_usage = 4000000000, max_execution_time = 300;",
		"CREATE USER IF NOT EXISTS `chui_prov` IDENTIFIED WITH sha256_password BY '<choose a password>';",
		"GRANT CREATE USER, ALTER USER, DROP USER ON `sso:`* TO `chui_prov`;",
		"GRANT `admin`, `analyst` TO `chui_prov` WITH ADMIN OPTION;",
		"GRANT SHOW USERS ON *.* TO `chui_prov`;",
		"GRANT SELECT(address, query_id) ON system.processes TO `chui_prov`;",
		"SET DEFAULT ROLE NONE TO `chui_prov`;",
		"system.processes, system.query_log",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("setup SQL lacks %q:\n%s", want, sql)
		}
	}
	if strings.Contains(SetupSQL(perPersonConfig(), "chui_prov"), "CREATE SETTINGS PROFILE") {
		t.Error("setup SQL creates a profile when none is configured")
	}
}

func TestNormalizeAddress(t *testing.T) {
	for in, want := range map[string]string{
		"::ffff:10.1.2.3":        "10.1.2.3",
		"::ffff:10.1.2.3:51234":  "10.1.2.3",
		"10.1.2.3":               "10.1.2.3",
		"10.1.2.3:9000":          "10.1.2.3",
		"[::1]:9000":             "::1",
		"::1":                    "::1",
		"2001:db8::1":            "2001:db8::1",
		" [::ffff:10.1.2.3]:80 ": "10.1.2.3",
		"":                       "",
	} {
		if got := normalizeAddress(in); got != want {
			t.Errorf("normalizeAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
