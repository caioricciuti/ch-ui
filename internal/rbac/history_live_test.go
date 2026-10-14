// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
)

// Access history against a real ClickHouse: store a state, change it
// outside the plan path, diff the two, then revert only the test's own
// entities with PlanRevert and expect them back as they were. Every entity
// is named chui_rbac_hist_* and dropped in cleanup.
func TestHistoryRevertLive(t *testing.T) {
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

	prefix := fmt.Sprintf("chui_rbac_hist_%d_", time.Now().UnixNano())
	base, extra := prefix+"base", prefix+"extra"
	t.Cleanup(func() {
		for _, sql := range []string{"DROP ROLE IF EXISTS " + extra, "DROP ROLE IF EXISTS " + base} {
			if _, err := ex.Query(ctx, sql, nil); err != nil {
				t.Error(err)
			}
		}
		rows, err := ex.Query(ctx, "SELECT count() AS n FROM system.roles WHERE startsWith(name, '"+prefix+"')", nil)
		if err != nil {
			t.Error(err)
		} else if n := u64(rows[0], "n"); n != 0 {
			t.Errorf("%d test roles left behind", n)
		}
	})

	must("CREATE ROLE " + base)
	must("GRANT SELECT ON system.one TO " + base)
	before, err := ReadSnapshot(ctx, ex)
	if err != nil {
		t.Fatal(err)
	}
	stored := FromSnapshot(before)

	// Outside changes: a new role granted to base, and a wider grant.
	must("CREATE ROLE " + extra)
	must("GRANT SELECT ON system.numbers TO " + extra)
	must("GRANT " + extra + " TO " + base)
	must("REVOKE SELECT ON system.one FROM " + base)
	after, err := ReadSnapshot(ctx, ex)
	if err != nil {
		t.Fatal(err)
	}
	only := map[string]bool{base: true, extra: true}
	d := DiffStates(FilterState(stored, only), FilterState(FromSnapshot(after), only))
	for _, want := range []struct{ kind, change, grantee string }{
		{ChangeEntity, "added", extra},
		{ChangeGrant, "added", extra},
		{ChangeRoleGrant, "added", base},
		{ChangeGrant, "removed", base},
	} {
		if !findChange(d.Changes, want.kind, want.change, want.grantee, "") {
			t.Errorf("missing %+v in %+v", want, d.Changes)
		}
	}

	p := PlanRevert(after, stored, only)
	if len(p.Skipped) != 0 || len(p.Remaining) != 0 || len(p.Ops) == 0 {
		t.Fatalf("revert plan: %+v", p)
	}
	plan, err := Compose(p.Ops, after, Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range Apply(ctx, ex, plan, nil) {
		if r.Status != StatusApplied {
			t.Fatalf("%s: %s %s", r.SQL, r.Status, r.Error)
		}
	}
	back, err := ReadSnapshot(ctx, ex)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := FilterState(FromSnapshot(back), only).Hash(), FilterState(stored, only).Hash(); got != want {
		t.Fatalf("revert did not restore\n got %s\nwant %s\nops:\n%s", stateJSON(FilterState(FromSnapshot(back), only)), stateJSON(FilterState(stored, only)), renderOps(p.Ops))
	}
}
