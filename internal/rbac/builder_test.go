// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"strings"
	"testing"
)

func sp(s string) *string { return &s }

func role(name string) *Target { return &Target{Type: "role", Name: name} }
func user(name string) *Target { return &Target{Type: "user", Name: name} }

func TestRenderGolden(t *testing.T) {
	cases := []struct {
		name    string
		op      Op
		cluster string
		want    string
	}{
		{"create role", Op{Kind: OpCreateRole, Name: "analyst"}, "", "CREATE ROLE `analyst`"},
		{"create role backtick", Op{Kind: OpCreateRole, Name: "we`ird"}, "", "CREATE ROLE `we``ird`"},
		{"create role backslash", Op{Kind: OpCreateRole, Name: `tail\`}, "", "CREATE ROLE `tail\\\\`"},
		{"create role unicode", Op{Kind: OpCreateRole, Name: "análise"}, "", "CREATE ROLE `análise`"},
		{"create role dotted", Op{Kind: OpCreateRole, Name: "team.read", Profile: "it's"}, "", "CREATE ROLE `team.read` SETTINGS PROFILE 'it''s'"},
		{"create role on cluster", Op{Kind: OpCreateRole, Name: "r"}, "prod", "CREATE ROLE `r` ON CLUSTER `prod`"},
		{"drop role", Op{Kind: OpDropRole, Name: "r"}, "", "DROP ROLE IF EXISTS `r`"},
		{"role profile", Op{Kind: OpSetRoleProfile, Name: "r", Profile: "p"}, "", "ALTER ROLE `r` SETTINGS PROFILE 'p'"},
		{"role profile clear", Op{Kind: OpSetRoleProfile, Name: "r"}, "c1", "ALTER ROLE `r` ON CLUSTER `c1` SETTINGS NONE"},
		{"grant global", Op{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"SELECT", "SHOW TABLES"}}, "", "GRANT SELECT, SHOW TABLES ON *.* TO `r`"},
		{"grant db", Op{Kind: OpGrantPrivilege, Target: user("u"), Access: []string{"INSERT"}, Database: "my.db", GrantOption: true}, "", "GRANT INSERT ON `my.db`.* TO `u` WITH GRANT OPTION"},
		{"grant columns", Op{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"SELECT"}, Database: "d", Table: "t`x", Columns: []string{"a", "b c"}}, "c", "GRANT ON CLUSTER `c` SELECT(`a`, `b c`) ON `d`.`t``x` TO `r`"},
		{"revoke option", Op{Kind: OpRevokePrivilege, Target: role("r"), Access: []string{"SELECT"}, Database: "d", Table: "t", GrantOption: true}, "", "REVOKE GRANT OPTION FOR SELECT ON `d`.`t` FROM `r`"},
		{"grant roles", Op{Kind: OpGrantRole, Target: user("u"), Roles: []string{"a", "b"}, AdminOption: true}, "", "GRANT `a`, `b` TO `u` WITH ADMIN OPTION"},
		{"revoke admin", Op{Kind: OpRevokeRole, Target: user("u"), Roles: []string{"a"}, AdminOption: true}, "", "REVOKE ADMIN OPTION FOR `a` FROM `u`"},
		{"default all", Op{Kind: OpSetDefaultRoles, Target: user("u"), DefaultAll: true}, "", "ALTER USER `u` DEFAULT ROLE ALL"},
		{"default none", Op{Kind: OpSetDefaultRoles, Target: user("u")}, "", "ALTER USER `u` DEFAULT ROLE NONE"},
		{"default list", Op{Kind: OpSetDefaultRoles, Target: user("u"), Roles: []string{"a"}}, "k", "ALTER USER `u` ON CLUSTER `k` DEFAULT ROLE `a`"},
		{"policy", Op{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "tenant = 'a''b'", To: Assignees{Names: []string{"r"}}}}, "", "CREATE ROW POLICY `p` ON `d`.`t` FOR SELECT USING (tenant = 'a''b') AS PERMISSIVE TO `r`"},
		{"policy restrictive all except", Op{Kind: OpAlterRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", Restrictive: true, To: Assignees{All: true, Except: []string{"admin"}}}}, "c", "ALTER ROW POLICY `p` ON CLUSTER `c` ON `d`.`t` FOR SELECT USING (1) AS RESTRICTIVE TO ALL EXCEPT `admin`"},
		{"policy drop", Op{Kind: OpDropRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t"}}, "c", "DROP ROW POLICY IF EXISTS `p` ON `d`.`t` ON CLUSTER `c`"},
		{"quota", Op{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "q", KeyedBy: "user_name", Intervals: []QuotaInterval{
			{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 100, "read_rows": 5}},
			{Duration: 1, Unit: "day", Randomized: true, Limits: map[string]uint64{}},
		}, To: Assignees{Names: []string{"u"}}}}, "", "CREATE QUOTA `q` KEYED BY user_name FOR INTERVAL 1 hour MAX queries = 100, MAX read_rows = 5, FOR RANDOMIZED INTERVAL 1 day TRACKING ONLY TO `u`"},
		{"quota not keyed", Op{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "q"}}, "", "CREATE QUOTA `q` NOT KEYED TO NONE"},
		{"drop quota", Op{Kind: OpDropQuota, Name: "q"}, "", "DROP QUOTA IF EXISTS `q`"},
		{"profile", Op{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "p", Inherit: []string{"default"}, Settings: []ProfileSetting{
			{Name: "max_threads", Value: sp("4"), Min: sp("1"), Max: sp("8"), Writability: "CONST"},
			{Name: "load_balancing", Value: sp("random'; DROP")},
			{Name: "readonly", Writability: "CHANGEABLE_IN_READONLY"},
		}, To: Assignees{All: true}}}, "", "CREATE SETTINGS PROFILE `p` SETTINGS INHERIT 'default', max_threads = 4 MIN 1 MAX 8 CONST, load_balancing = 'random''; DROP', readonly CHANGEABLE_IN_READONLY TO ALL"},
		{"profile alter empty", Op{Kind: OpAlterSettingsProfile, SettingsProfile: &ProfileSpec{Name: "p"}}, "", "ALTER SETTINGS PROFILE `p` SETTINGS NONE TO NONE"},
		{"profile value backslash", Op{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "p", Settings: []ProfileSetting{{Name: "format_csv_delimiter", Value: sp(`\`)}}}}, "", `CREATE SETTINGS PROFILE ` + "`p`" + ` SETTINGS format_csv_delimiter = '\\' TO NONE`},
		{"drop profile", Op{Kind: OpDropSettingsProfile, Name: "p"}, "", "DROP SETTINGS PROFILE IF EXISTS `p`"},
	}
	for _, c := range cases {
		if got := Render(c.op, c.cluster, nil); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

func TestAlterQuotaClearsRemovedLimits(t *testing.T) {
	snap := &Snapshot{Quotas: []Quota{{Storage: "local_directory", QuotaSpec: QuotaSpec{Name: "q", Intervals: []QuotaInterval{
		{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 10, "errors": 2}},
		{Duration: 1, Unit: "day", Limits: map[string]uint64{"queries": 100}},
	}}}}}
	op := Op{Kind: OpAlterQuota, Quota: &QuotaSpec{Name: "q", Intervals: []QuotaInterval{{Duration: 60, Unit: "minute", Limits: map[string]uint64{"queries": 20}}}}}
	got := Render(op, "", snap)
	want := "ALTER QUOTA `q` NOT KEYED FOR INTERVAL 60 minute MAX queries = 20, MAX errors = 0, FOR INTERVAL 1 day NO LIMITS TO NONE"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestComposeOrdersStatements(t *testing.T) {
	snap := &Snapshot{RoleProfiles: map[string][]string{}, Roles: []Entity{{Name: "old", Storage: "local_directory"}}, Quotas: []Quota{{QuotaSpec: QuotaSpec{Name: "oldq"}}}}
	ops := []Op{
		{Kind: OpDropRole, Name: "old"},
		{Kind: OpRevokePrivilege, Target: role("r"), Access: []string{"INSERT"}},
		{Kind: OpGrantRole, Target: user("u"), Roles: []string{"r"}},
		{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"SELECT"}},
		{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1"}},
		{Kind: OpDropQuota, Name: "oldq"},
		{Kind: OpCreateRole, Name: "r"},
		{Kind: OpSetDefaultRoles, Target: user("u"), Roles: []string{"r"}},
		{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "p"}},
		{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "q"}},
		{Kind: OpSetRoleProfile, Name: "r", Profile: "p"},
	}
	plan, err := Compose(ops, snap, Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for i, s := range plan.Statements {
		if s.Seq != i+1 {
			t.Fatalf("seq %d at %d", s.Seq, i)
		}
		kinds = append(kinds, ops[s.OpIndex].Kind)
	}
	want := []string{OpCreateSettingsProfile, OpCreateQuota, OpCreateRole, OpSetRoleProfile, OpGrantPrivilege, OpGrantRole, OpCreateRowPolicy, OpSetDefaultRoles, OpRevokePrivilege, OpDropQuota, OpDropRole}
	if strings.Join(kinds, ",") != strings.Join(want, ",") {
		t.Fatalf("order\n got %v\nwant %v", kinds, want)
	}
}

func TestInverses(t *testing.T) {
	snap := &Snapshot{
		RoleProfiles:     map[string][]string{"old": {"prof"}},
		Roles:            []Entity{{Name: "old"}},
		Users:            []UserEntity{{Name: "u", DefaultRoles: []string{"a"}}},
		Grants:           []GrantRow{{RoleName: "old", Access: "SELECT", Database: "d"}},
		RoleGrants:       []RoleGrant{{UserName: "u", GrantedRole: "old", AdminOption: true}},
		RowPolicies:      []RowPolicy{{FullName: "p ON d.t", RowPolicySpec: RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "x = 1", To: Assignees{Names: []string{"r"}}}}},
		Quotas:           []Quota{{QuotaSpec: QuotaSpec{Name: "q", KeyedBy: "user_name", Intervals: []QuotaInterval{{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 5}}}}}},
		SettingsProfiles: []SettingsProfile{{ProfileSpec: ProfileSpec{Name: "prof", Settings: []ProfileSetting{{Name: "max_threads", Value: sp("2")}}}}},
	}
	render := func(ops []Op) string {
		var out []string
		for _, o := range ops {
			out = append(out, Render(o, "", snap))
		}
		return strings.Join(out, "; ")
	}
	cases := []struct {
		op   Op
		want string
	}{
		{Op{Kind: OpCreateRole, Name: "n"}, "DROP ROLE IF EXISTS `n`"},
		{Op{Kind: OpDropRole, Name: "old"}, "CREATE ROLE `old` SETTINGS PROFILE 'prof'; GRANT SELECT ON `d`.* TO `old`; GRANT `old` TO `u` WITH ADMIN OPTION"},
		{Op{Kind: OpSetRoleProfile, Name: "old"}, "ALTER ROLE `old` SETTINGS PROFILE 'prof'"},
		{Op{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"SELECT"}, GrantOption: true}, "REVOKE SELECT ON *.* FROM `r`"},
		{Op{Kind: OpRevokePrivilege, Target: role("r"), Access: []string{"SELECT"}, GrantOption: true}, "GRANT SELECT ON *.* TO `r` WITH GRANT OPTION"},
		{Op{Kind: OpGrantRole, Target: user("u"), Roles: []string{"x"}}, "REVOKE `x` FROM `u`"},
		{Op{Kind: OpRevokeRole, Target: user("u"), Roles: []string{"old"}}, "GRANT `old` TO `u` WITH ADMIN OPTION"},
		{Op{Kind: OpSetDefaultRoles, Target: user("u"), DefaultAll: true}, "ALTER USER `u` DEFAULT ROLE `a`"},
		{Op{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "n", Database: "d", Table: "t", Using: "1"}}, "DROP ROW POLICY IF EXISTS `n` ON `d`.`t`"},
		{Op{Kind: OpAlterRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "2"}}, "ALTER ROW POLICY `p` ON `d`.`t` FOR SELECT USING (x = 1) AS PERMISSIVE TO `r`"},
		{Op{Kind: OpDropRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t"}}, "CREATE ROW POLICY `p` ON `d`.`t` FOR SELECT USING (x = 1) AS PERMISSIVE TO `r`"},
		{Op{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "n"}}, "DROP QUOTA IF EXISTS `n`"},
		{Op{Kind: OpDropQuota, Name: "q"}, "CREATE QUOTA `q` KEYED BY user_name FOR INTERVAL 1 hour MAX queries = 5 TO NONE"},
		{Op{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "n"}}, "DROP SETTINGS PROFILE IF EXISTS `n`"},
		{Op{Kind: OpAlterSettingsProfile, SettingsProfile: &ProfileSpec{Name: "prof"}}, "ALTER SETTINGS PROFILE `prof` SETTINGS max_threads = 2 TO NONE"},
	}
	for _, c := range cases {
		if got := render(Inverse(c.op, snap)); got != c.want {
			t.Errorf("inverse of %s:\n got %s\nwant %s", c.op.Kind, got, c.want)
		}
	}
}

func TestRollbackOpsOnlyApplied(t *testing.T) {
	snap := &Snapshot{RoleProfiles: map[string][]string{}}
	ops := []Op{{Kind: OpCreateRole, Name: "a"}, {Kind: OpGrantPrivilege, Target: role("a"), Access: []string{"SELECT"}}, {Kind: OpCreateRole, Name: "b"}}
	plan, err := Compose(ops, snap, Catalog{}, "", GuardContext{})
	if err != nil {
		t.Fatal(err)
	}
	// Statements: create a, create b, grant. Only the first two applied.
	results := []Result{{Seq: 1, Status: StatusApplied}, {Seq: 2, Status: StatusApplied}, {Seq: 3, Status: StatusFailed}}
	rb := RollbackOps(plan, results)
	if len(rb) != 2 || rb[0].Kind != OpDropRole || rb[0].Name != "b" || rb[1].Name != "a" {
		t.Fatalf("rollback = %+v", rb)
	}
}

func TestPlanHashTracksSnapshot(t *testing.T) {
	ops := []Op{{Kind: OpCreateRole, Name: "a"}}
	s1 := &Snapshot{RoleProfiles: map[string][]string{}}
	s2 := &Snapshot{RoleProfiles: map[string][]string{}, Roles: []Entity{{Name: "other"}}}
	p1, _ := Compose(ops, s1, Catalog{}, "", GuardContext{})
	p1b, _ := Compose(ops, s1, Catalog{}, "", GuardContext{})
	p2, _ := Compose(ops, s2, Catalog{}, "", GuardContext{})
	p3, _ := Compose(ops, s1, Catalog{}, "c", GuardContext{})
	if p1.PlanHash != p1b.PlanHash {
		t.Fatal("hash is not stable")
	}
	if p1.PlanHash == p2.PlanHash || p1.PlanHash == p3.PlanHash {
		t.Fatal("hash ignores snapshot or statements")
	}
}

func TestComposeRejectsBadInput(t *testing.T) {
	snap := &Snapshot{RoleProfiles: map[string][]string{}}
	cat := Catalog{Privileges: map[string]bool{"SELECT": true}, Settings: map[string]bool{"max_threads": true}}
	bad := []Op{
		{Kind: "raw_sql", Name: "x"},
		{Kind: OpCreateRole, Name: ""},
		{Kind: OpCreateRole, Name: "a\nb"},
		{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"SELECT; DROP"}},
		{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"INSERT"}},
		{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"select"}},
		{Kind: OpGrantPrivilege, Target: &Target{Type: "group", Name: "x"}, Access: []string{"SELECT"}},
		{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"SELECT"}, Columns: []string{"a"}},
		{Kind: OpSetDefaultRoles, Target: role("r")},
		{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1); DROP TABLE x; --"}},
		{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "q", KeyedBy: "user_name; DROP"}},
		{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "q", Intervals: []QuotaInterval{{Duration: 1, Unit: "fortnight"}}}},
		{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "q", Intervals: []QuotaInterval{{Duration: 1, Unit: "hour", Limits: map[string]uint64{"bogus": 1}}}}},
		{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "p", Settings: []ProfileSetting{{Name: "max threads", Value: sp("1")}}}},
		{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "p", Settings: []ProfileSetting{{Name: "unknown_setting", Value: sp("1")}}}},
		{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "p", Settings: []ProfileSetting{{Name: "max_threads", Value: sp("1"), Writability: "READONLY; DROP"}}}},
	}
	for _, op := range bad {
		if _, err := Compose([]Op{op}, snap, cat, "", GuardContext{}); err == nil {
			t.Errorf("accepted %+v", op)
		}
	}
	if _, err := Compose([]Op{{Kind: OpCreateRole, Name: "a"}}, snap, cat, "bad cluster`", GuardContext{}); err == nil {
		t.Error("accepted an invalid cluster name")
	}
}

func TestDiffsCanonical(t *testing.T) {
	snap := &Snapshot{
		RoleProfiles: map[string][]string{},
		Roles:        []Entity{{Name: "r"}},
		Grants:       []GrantRow{{RoleName: "r", Access: "SELECT", Database: "d"}},
	}
	ops := []Op{
		{Kind: OpGrantPrivilege, Target: role("r"), Access: []string{"INSERT"}, Database: "d", Table: "t"},
		{Kind: OpRevokePrivilege, Target: role("r"), Access: []string{"SELECT"}, Database: "d"},
		{Kind: OpCreateRole, Name: "new"},
	}
	diffs := Diffs(ops, snap)
	if len(diffs) != 2 {
		t.Fatalf("diffs = %+v", diffs)
	}
	if diffs[0].Current != "CREATE ROLE `r`\nGRANT SELECT ON `d`.* TO `r`" || diffs[0].Proposed != "CREATE ROLE `r`\nGRANT INSERT ON `d`.`t` TO `r`" {
		t.Fatalf("role diff = %+v", diffs[0])
	}
	if diffs[1].Current != "" || diffs[1].Proposed != "CREATE ROLE `new`" {
		t.Fatalf("new role diff = %+v", diffs[1])
	}
}
