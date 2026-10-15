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

// Export a real server, import the file into the same server with every
// test entity renamed to a new name and its database to another, apply,
// and expect a second import to plan nothing. Every entity is named
// chui_rbac_aac_* and dropped in cleanup; an op naming anything else stops
// the test before it runs.
func TestImportLive(t *testing.T) {
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

	prefix := fmt.Sprintf("chui_rbac_aac_%d_", time.Now().UnixNano())
	name := func(kind string) string { return prefix + kind }
	srcDB, dstDB, userName := name("src"), name("dst"), name("user")
	srcRole, dstRole, policy := name("src_reader"), name("dst_reader"), name("policy")

	t.Cleanup(func() {
		for _, sql := range []string{
			"DROP ROW POLICY IF EXISTS " + policy + " ON " + srcDB + ".events",
			"DROP ROW POLICY IF EXISTS " + policy + " ON " + dstDB + ".events",
			"DROP USER IF EXISTS " + userName,
			"DROP ROLE IF EXISTS " + srcRole,
			"DROP ROLE IF EXISTS " + dstRole,
			"DROP DATABASE IF EXISTS " + srcDB,
			"DROP DATABASE IF EXISTS " + dstDB,
		} {
			if _, err := ex.Query(ctx, sql, nil); err != nil {
				t.Error(err)
			}
		}
		rows, err := ex.Query(ctx, `SELECT name FROM system.roles WHERE startsWith(name, 'chui_rbac_aac_')
			UNION ALL SELECT name FROM system.users WHERE startsWith(name, 'chui_rbac_aac_')
			UNION ALL SELECT name FROM system.row_policies WHERE startsWith(short_name, 'chui_rbac_aac_')
			UNION ALL SELECT name FROM system.databases WHERE startsWith(name, 'chui_rbac_aac_')`, nil)
		if err != nil {
			t.Error(err)
		} else if left := firstColumn(rows); len(left) > 0 {
			t.Errorf("left behind: %v", left)
		}
	})
	for _, db := range []string{srcDB, dstDB} {
		must("CREATE DATABASE " + db)
		must("CREATE TABLE " + db + ".events (id UInt64, tenant String) ENGINE = MergeTree ORDER BY id")
		must("CREATE TABLE " + db + ".secret (id UInt64) ENGINE = MergeTree ORDER BY id")
	}
	must("CREATE USER " + userName + " IDENTIFIED WITH plaintext_password BY 'import-test-only'")
	must("CREATE ROLE " + srcRole)
	must("GRANT SELECT, SHOW TABLES ON " + srcDB + ".* TO " + srcRole)
	must("REVOKE SELECT ON " + srcDB + ".secret FROM " + srcRole)
	must("GRANT INSERT(tenant) ON " + srcDB + ".events TO " + srcRole + " WITH GRANT OPTION")
	must("GRANT " + srcRole + " TO " + userName)
	must("ALTER USER " + userName + " DEFAULT ROLE " + srcRole)
	must("CREATE ROW POLICY " + policy + " ON " + srcDB + ".events FOR SELECT USING tenant = 'a' TO " + srcRole)

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
	databases := func() map[string]bool {
		rows, err := ex.Query(ctx, "SELECT name FROM system.databases", nil)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]bool{}
		for _, n := range firstColumn(rows) {
			out[n] = true
		}
		return out
	}

	st, _ := ExportState(read())
	file, err := MarshalFileYAML(st, nil, FileSource{Connection: "live"}, ExportHeader(FileSource{}, nil))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(strings.ToLower(string(file)), "import-test-only") {
		t.Fatal("the export holds the test user's password")
	}
	parsed, err := ParseFile(file)
	if err != nil {
		t.Fatal(err)
	}
	opts := func() ImportOpts {
		return ImportOpts{
			Rename:    Rename{Principals: map[string]string{srcRole: dstRole}, Databases: map[string]string{srcDB: dstDB}},
			Databases: databases(),
		}
	}
	cur := read()
	plan, err := PlanImport(cur, parsed.State, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(plan.Ops) == 0 {
		t.Fatal("the import planned nothing")
	}
	for _, op := range plan.Ops {
		names := append([]string{op.Database}, op.Roles...)
		for _, r := range touched(op) {
			names = append(names, r[1])
		}
		if op.RowPolicy != nil {
			names = append(names, op.RowPolicy.Database)
		}
		for _, n := range names {
			if n != "" && !strings.HasPrefix(n, prefix) && !strings.Contains(n, " ON "+prefix) {
				t.Fatalf("op touches %q, which this test did not create: %s", n, Render(op, "", nil))
			}
		}
	}
	cat, err := LoadCatalog(ctx, ex, plan.Ops)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateImportOps(plan.Ops, cat); err != nil {
		t.Fatal(err)
	}
	composed, err := Compose(plan.Ops, cur, cat, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("%d statements\n%s", len(composed.Statements), renderOps(plan.Ops))
	for _, r := range Apply(ctx, ex, composed, nil) {
		if r.Status != StatusApplied {
			t.Fatalf("%s: %s %s", r.SQL, r.Status, r.Error)
		}
	}
	again, err := PlanImport(read(), parsed.State, opts())
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Ops) != 0 {
		t.Fatalf("second import not empty:\n%s", renderOps(again.Ops))
	}
	// The renamed role holds what the source role held, on the new database.
	rows, err := ex.Query(ctx, "SHOW GRANTS FOR "+dstRole, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(firstColumn(rows), "\n"); !strings.Contains(got, dstDB+".secret") || !strings.Contains(got, "INSERT(tenant) ON "+dstDB+".events") {
		t.Fatalf("grants of %s:\n%s", dstRole, got)
	}
}
