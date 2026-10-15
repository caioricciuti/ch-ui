// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
)

// Create a user through import against a real ClickHouse with a salted
// sha256 hash made the way the browser makes it (hex(sha256(password ||
// salt))), sign in as it with the password, check the apply is not drift
// and that creating it again is blocked, then roll back and see the user
// gone. Every entity is named chui_rbac_cu_* and dropped in cleanup.
func TestCreateUserLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	admin, adminPassword := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if admin == "" {
		admin = "default"
	}
	client := connector.NewCHClient(endpoint, false)
	ex := liveExecutor{client: client, user: admin, password: adminPassword}
	ctx := context.Background()
	must := func(sql string) {
		t.Helper()
		if _, err := ex.Query(ctx, sql, nil); err != nil {
			t.Fatal(err)
		}
	}

	prefix := fmt.Sprintf("chui_rbac_cu_%d_", time.Now().UnixNano())
	db, roleName, userName := prefix+"db", prefix+"reader", prefix+"user"
	t.Cleanup(func() {
		for _, sql := range []string{"DROP USER IF EXISTS " + userName, "DROP ROLE IF EXISTS " + roleName, "DROP DATABASE IF EXISTS " + db} {
			if _, err := ex.Query(ctx, sql, nil); err != nil {
				t.Error(err)
			}
		}
	})
	must("CREATE DATABASE " + db)
	must("CREATE TABLE " + db + ".t (id UInt64) ENGINE = MergeTree ORDER BY id")
	must("INSERT INTO " + db + ".t VALUES (1), (2)")
	must("CREATE ROLE " + roleName)
	must("GRANT SELECT ON " + db + ".* TO " + roleName)

	pre, err := ReadSnapshot(ctx, ex)
	if err != nil {
		t.Fatal(err)
	}
	st := FromSnapshot(pre)
	st.Users = append(st.Users, StateUser{Name: userName, DefaultRoles: []string{roleName}})
	st.RoleGrants = append(st.RoleGrants, StateRoleGrant{Type: "user", Name: userName, Role: roleName})
	imp, err := PlanImport(pre, st, ImportOpts{CreateUsers: []string{userName}})
	if err != nil {
		t.Fatal(err)
	}

	password, salt := randomHex(t, 12), randomHex(t, 16)
	sum := sha256.Sum256([]byte(password + salt))
	for i := range imp.Ops {
		if imp.Ops[i].Kind == OpCreateUser {
			imp.Ops[i].Auth = &UserAuth{Method: AuthSHA256Hash, Hash: hex.EncodeToString(sum[:]), Salt: salt}
		}
	}
	for _, op := range imp.Ops {
		for _, n := range OpEntities(op) {
			if !strings.HasPrefix(n, prefix) {
				t.Fatalf("import touches %q, not a test entity: %+v", n, op)
			}
		}
	}
	plan, _, err := PlanLive(ctx, ex, imp.Ops, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	if plan.Blocked() {
		t.Fatalf("blocked: %+v", plan.Warnings)
	}
	results := Apply(ctx, ex, plan, nil)
	for _, r := range results {
		if r.Status != StatusApplied {
			t.Fatalf("apply: %+v", r)
		}
		if strings.Contains(r.SQL, salt) {
			t.Fatalf("result shows the salt: %s", r.SQL)
		}
	}

	// Sign in as the new user; its default role reads the table.
	as := liveExecutor{client: client, user: userName, password: password}
	rows, err := as.Query(ctx, "SELECT currentUser() AS u, (SELECT count() FROM "+db+".t) AS n", nil)
	if err != nil {
		t.Fatalf("sign in as the new user: %v", err)
	}
	if str(rows[0], "u") != userName || u64(rows[0], "n") != 2 {
		t.Fatalf("signed in as %v", rows)
	}
	if _, err := (liveExecutor{client: client, user: userName, password: password + "x"}).Query(ctx, "SELECT 1", nil); err == nil {
		t.Fatal("a wrong password signed in")
	}

	post, err := ReadSnapshot(ctx, ex)
	if err != nil {
		t.Fatal(err)
	}
	if extra := UnexplainedChanges(pre, post, RedactOps(AppliedOps(imp.Ops, results))); len(extra) != 0 {
		t.Fatalf("the apply looks like drift: %+v", extra)
	}
	if l := post.UserLogins[userName]; l.Auth != "sha256_password" || len(l.Hosts) != 1 || l.Hosts[0].Kind != HostAny {
		t.Fatalf("login = %+v", l)
	}
	again, err := Compose(imp.Ops[:2], post, Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	if codes(again.Warnings)[CodeUserExists] != SeverityBlock {
		t.Fatalf("creating it again is not blocked: %+v", again.Warnings)
	}

	// Roll back: the user is dropped.
	rb := RollbackOps(plan, results)
	rplan, _, err := PlanLive(ctx, ex, rb, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	if rplan.Blocked() || codes(rplan.Warnings)[CodeDropUser] != SeverityAck {
		t.Fatalf("rollback warnings: %+v", rplan.Warnings)
	}
	for _, r := range Apply(ctx, ex, rplan, nil) {
		if r.Status != StatusApplied {
			t.Fatalf("rollback: %+v", r)
		}
	}
	if _, err := as.Query(ctx, "SELECT 1", nil); err == nil {
		t.Fatal("the user still signs in after the rollback")
	}
}

func randomHex(t *testing.T, n int) string {
	t.Helper()
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(b)
}
