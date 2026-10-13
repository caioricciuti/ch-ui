// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"strings"
	"testing"
)

func TestValidateRowPolicyExpr(t *testing.T) {
	ok := []string{
		"tenant_id = 42",
		"tenant = 'a;b'",
		"tenant = 'it''s'",
		`tenant = 'back\'slash'`,
		"has(groups, currentUser())",
		"`we;ird` = 1",
		"x IN (1, 2, (3))",
		"note = '-- not a comment'",
	}
	for _, e := range ok {
		if err := ValidateRowPolicyExpr(e); err != nil {
			t.Errorf("rejected %q: %v", e, err)
		}
	}
	bad := []string{
		"", "   ", "1; DROP TABLE t", "1 -- tail", "1 /* x */", "1 */", "# x", "a = 'open", "(a = 1", "a = 1)",
		"1) OR (1", "`open", "\"open", "a = 1\x00",
	}
	for _, e := range bad {
		if err := ValidateRowPolicyExpr(e); err == nil {
			t.Errorf("accepted %q", e)
		}
	}
}

// FuzzValidateRowPolicyExpr: anything the lexer accepts, wrapped in
// USING ( ... ), must keep the statement in one piece: no separator,
// comment or unbalanced quote outside literals.
func FuzzValidateRowPolicyExpr(f *testing.F) {
	for _, seed := range []string{"a = 1", "x = 'y'", "(a)", "a;b", "a -- b", "'", "`x`", `'\''`, "a = ')'"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, expr string) {
		if ValidateRowPolicyExpr(expr) != nil {
			return
		}
		wrapped := "(" + expr + ")"
		depth := 0
		for i := 0; i < len(wrapped); i++ {
			c := wrapped[i]
			switch c {
			case ';', '#':
				t.Fatalf("accepted %q with %q outside a literal", expr, c)
			case '-':
				if i+1 < len(wrapped) && wrapped[i+1] == '-' {
					t.Fatalf("accepted a line comment in %q", expr)
				}
			case '/':
				if i+1 < len(wrapped) && wrapped[i+1] == '*' {
					t.Fatalf("accepted a block comment in %q", expr)
				}
			case '(':
				depth++
			case ')':
				depth--
				if depth == 0 && i != len(wrapped)-1 {
					t.Fatalf("expression %q closes USING early", expr)
				}
			case '\'', '"', '`':
				end, err := skipQuoted(wrapped, i)
				if err != nil {
					t.Fatalf("accepted unbalanced quote in %q", expr)
				}
				i = end
			}
		}
		if depth != 0 {
			t.Fatalf("unbalanced %q", expr)
		}
	})
}

func TestValidatePrivilegeAndSetting(t *testing.T) {
	known := map[string]bool{"SELECT": true, "SHOW TABLES": true}
	if err := ValidatePrivilege("SHOW TABLES", known); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"SHOW  TABLES", "SELECT ", "select", "SELECT(a)", "ALTER UPDATE", ""} {
		if ValidatePrivilege(p, known) == nil {
			t.Errorf("accepted privilege %q", p)
		}
	}
	if err := ValidateSettingName("max_threads", map[string]bool{"max_threads": true}); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"max-threads", "a b", "x;", ""} {
		if ValidateSettingName(s, nil) == nil {
			t.Errorf("accepted setting %q", s)
		}
	}
}

func TestValidateName(t *testing.T) {
	for _, n := range []string{"a", "team.read", "análise", "we`ird", `tail\`} {
		if err := ValidateName("role", n); err != nil {
			t.Errorf("rejected %q: %v", n, err)
		}
	}
	for _, n := range []string{"", " ", "a\nb", "a\x00", strings.Repeat("x", 300), "\xff"} {
		if ValidateName("role", n) == nil {
			t.Errorf("accepted %q", n)
		}
	}
}

func TestApplyCapabilities(t *testing.T) {
	var c Capabilities
	applyCapabilities(&c, []string{
		"GRANT SELECT ON *.* TO u",
		"GRANT CREATE ROLE, ALTER ROLE, DROP ROLE, ROLE ADMIN ON * TO u WITH GRANT OPTION",
		"GRANT SHOW ACCESS ON *.* TO u",
		"GRANT CREATE QUOTA ON db.* TO u",
	})
	if c.AccessManagement || !c.ManageRoles || !c.RoleAdmin || !c.ShowAccess || c.ManageQuotas || !c.GrantOption || c.Cluster {
		t.Fatalf("caps = %+v", c)
	}
	var all Capabilities
	applyCapabilities(&all, []string{"GRANT ALL ON *.* TO default WITH GRANT OPTION"})
	if !all.AccessManagement || !all.ManageQuotas || !all.Cluster {
		t.Fatalf("all caps = %+v", all)
	}
	// The docker image's default user: every access privilege listed one by
	// one, never ACCESS MANAGEMENT by name.
	var expanded Capabilities
	applyCapabilities(&expanded, []string{
		"GRANT SHOW, SELECT, ROLE ADMIN, CREATE ROW POLICY, ALTER ROW POLICY, DROP ROW POLICY, CREATE QUOTA, ALTER QUOTA, DROP QUOTA, CREATE SETTINGS PROFILE, ALTER SETTINGS PROFILE, DROP SETTINGS PROFILE, SHOW ACCESS, CLUSTER ON *.* TO default WITH GRANT OPTION",
		"GRANT CREATE USER, ALTER USER, DROP USER, CREATE ROLE, ALTER ROLE, DROP ROLE ON * TO default WITH GRANT OPTION",
	})
	if !expanded.AccessManagement || !expanded.ManageRoles || !expanded.ManageProfiles {
		t.Fatalf("expanded caps = %+v", expanded)
	}
}

func TestHostResults(t *testing.T) {
	rows := []map[string]any{
		{"host": "a", "port": "9000", "status": "0", "error": "", "num_hosts_remaining": "1"},
		{"host": "b", "port": float64(9000), "status": float64(493), "error": "Code: 493. DB::Exception: exists (version 25.8)", "num_hosts_remaining": "0"},
		{"host": "c", "port": "9000", "status": nil, "num_hosts_remaining": "0"},
	}
	hosts := hostResults(rows)
	if len(hosts) != 3 || hosts[0].Status != StatusApplied || hosts[1].Status != StatusFailed || hosts[1].Error != "exists" || hosts[2].Status != StatusPending {
		t.Fatalf("hosts = %+v", hosts)
	}
}
