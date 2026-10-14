// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
)

type liveExecutor struct {
	client         *connector.CHClient
	user, password string
}

func (e liveExecutor) Query(ctx context.Context, sql string, settings map[string]string) ([]map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	res, err := e.client.Execute(ctx, sql, e.user, e.password, settings)
	if err != nil {
		return nil, err
	}
	return res.Data, nil
}

// Reconcile against a real ClickHouse: build a state with roles, a profile,
// grants with partial revokes and privilege groups (ALL, ALTER, SHOW),
// role grants, default roles, a row policy and a quota, apply the plan, read
// the server back and expect nothing left to do and the same hash; narrow a
// user's grants; then reconcile back to where it started. Every entity is
// named chui_rbac_rec_* and dropped in cleanup.
func TestReconcileLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	admin, adminPassword := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if admin == "" {
		admin = "default"
	}
	ex := liveExecutor{client: connector.NewCHClient(endpoint, false), user: admin, password: adminPassword}
	ctx := context.Background()
	must := func(sql string) {
		t.Helper()
		if _, err := ex.Query(ctx, sql, nil); err != nil {
			t.Fatal(err)
		}
	}

	prefix := fmt.Sprintf("chui_rbac_rec_%d_", time.Now().UnixNano())
	name := func(kind string) string { return prefix + kind }
	db, userName := name("db"), name("user")
	reader, base, wide, profile, policy, quota := name("reader"), name("base"), name("wide"), name("profile"), name("policy"), name("quota")

	must("CREATE DATABASE " + db)
	must("CREATE TABLE " + db + ".events (id UInt64, tenant String) ENGINE = MergeTree ORDER BY id")
	must("CREATE TABLE " + db + ".secret (id UInt64) ENGINE = MergeTree ORDER BY id")
	must("CREATE USER " + userName + " IDENTIFIED WITH plaintext_password BY 'reconcile-test-only'")
	t.Cleanup(func() {
		for _, sql := range []string{
			"DROP ROW POLICY IF EXISTS " + policy + " ON " + db + ".events",
			"DROP QUOTA IF EXISTS " + quota,
			"DROP USER IF EXISTS " + userName,
			"DROP ROLE IF EXISTS " + reader,
			"DROP ROLE IF EXISTS " + base,
			"DROP ROLE IF EXISTS " + wide,
			"DROP SETTINGS PROFILE IF EXISTS " + profile,
			"DROP DATABASE IF EXISTS " + db,
		} {
			if _, err := ex.Query(ctx, sql, nil); err != nil {
				t.Error(err)
			}
		}
	})

	read := func() *Snapshot {
		t.Helper()
		s, err := ReadSnapshot(ctx, ex)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Errors) > 0 {
			t.Skipf("the test login cannot read every access table: %v", s.Errors)
		}
		return s
	}
	// run reconciles toward desired, refusing any op on an entity this
	// test did not create, applies and checks the server converged.
	run := func(label string, desired *State) {
		t.Helper()
		cur := read()
		ops, skipped := Reconcile(cur, desired, ReconcileOpts{Prune: PruneAll()})
		if len(skipped) > 0 {
			t.Fatalf("%s: skipped %+v", label, skipped)
		}
		if len(ops) == 0 {
			t.Fatalf("%s: no ops", label)
		}
		for _, op := range ops {
			names := append([]string{}, op.Roles...)
			for _, r := range touched(op) {
				names = append(names, r[1])
			}
			for _, n := range names {
				if !strings.Contains(n, prefix) {
					t.Fatalf("%s: op touches %q, which this test did not create: %s", label, n, Render(op, "", nil))
				}
			}
		}
		cat, err := LoadCatalog(ctx, ex, ops)
		if err != nil {
			t.Fatal(err)
		}
		plan, err := Compose(ops, cur, cat, "", GuardContext{})
		if err != nil {
			t.Fatalf("%s: %v", label, err)
		}
		t.Logf("%s: %d statements\n%s", label, len(plan.Statements), renderOps(ops))
		for _, r := range Apply(ctx, ex, plan, nil) {
			if r.Status != StatusApplied {
				t.Fatalf("%s: %s: %s %s", label, r.SQL, r.Status, r.Error)
			}
		}
		after := read()
		if again, _ := Reconcile(after, desired, ReconcileOpts{Prune: PruneAll()}); len(again) != 0 {
			t.Fatalf("%s: server did not converge, still to do:\n%s\napplied:\n%s", label, renderOps(again), renderOps(ops))
		}
		// However desired wrote the grants, the server's state hashes the
		// same, so access history sees no drift.
		if got, want := FromSnapshot(after), desired.CanonicalWith(after); got.Hash() != want.Hash() {
			t.Fatalf("%s: hash differs after converging\n got %s\nwant %s", label, stateJSON(got), stateJSON(want))
		}
	}

	start := read()
	desired := FromSnapshot(start)
	desired.SettingsProfiles = append(desired.SettingsProfiles, ProfileSpec{
		Name: profile, Settings: []ProfileSetting{{Name: "max_threads", Value: sp("2")}}, To: Assignees{Names: []string{base}},
	})
	desired.Roles = append(desired.Roles, StateRole{Name: reader, Profiles: []string{profile}}, StateRole{Name: base}, StateRole{Name: wide})
	desired.Grants = append(desired.Grants,
		GranteeGrants{Type: "role", Name: reader, Privileges: []StateGrant{
			{Access: []string{"SELECT", "SHOW TABLES"}, Database: db},
			{Access: []string{"SELECT"}, Database: db, Table: "secret", Revoke: true},
			{Access: []string{"INSERT"}, Database: db, Table: "events", Column: "tenant", GrantOption: true},
		}},
		GranteeGrants{Type: "role", Name: base, Privileges: []StateGrant{
			{Access: []string{"ALTER"}, Database: db},
			{Access: []string{"ALTER UPDATE"}, Database: db, Table: "events", Revoke: true},
		}},
		// ALL on a table under SHOW TABLES on its database, written as
		// groups; the server prints the table-level list.
		GranteeGrants{Type: "role", Name: wide, Privileges: []StateGrant{
			{Access: []string{"SHOW TABLES"}, Database: db},
			{Access: []string{"ALL"}, Database: db, Table: "events"},
			{Access: []string{"ALTER UPDATE"}, Database: db, Table: "events", Revoke: true},
		}},
		GranteeGrants{Type: "user", Name: userName, Privileges: []StateGrant{
			{Access: []string{"ALL"}, Database: db},
			{Access: []string{"SELECT"}, Database: db, Table: "secret", Revoke: true},
		}},
	)
	desired.RoleGrants = append(desired.RoleGrants,
		StateRoleGrant{Type: "user", Name: userName, Role: reader, AdminOption: true},
		StateRoleGrant{Type: "role", Name: reader, Role: base})
	for i := range desired.Users {
		if desired.Users[i].Name == userName {
			desired.Users[i] = StateUser{Name: userName, DefaultRoles: []string{reader}}
		}
	}
	desired.RowPolicies = append(desired.RowPolicies, RowPolicySpec{
		Name: policy, Database: db, Table: "events", Using: "tenant = 'a'", To: Assignees{Names: []string{reader}},
	})
	desired.Quotas = append(desired.Quotas, QuotaSpec{Name: quota, KeyedBy: "user_name", Intervals: []QuotaInterval{
		{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 100}},
	}, To: Assignees{Names: []string{reader}}})
	run("build", desired)

	// Narrow the user's grants: a revoke on the database that also takes
	// what ALL granted there, then a grant inside it.
	narrowed := FromSnapshot(read())
	for i, g := range narrowed.Grants {
		if g.Type == "user" && g.Name == userName {
			narrowed.Grants[i].Privileges = []StateGrant{{Access: []string{"SELECT"}, Database: db, Table: "events"}}
		}
	}
	run("narrow", narrowed)

	run("revert", FromSnapshot(start))
}
