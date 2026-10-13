// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"errors"
	"strings"
	"testing"
)

var testParents = map[string]string{
	"SELECT": "ALL", "INSERT": "ALL", "ALTER": "ALL", "ALTER TABLE": "ALTER", "ALTER UPDATE": "ALTER TABLE",
	"SHOW": "ALL", "SHOW TABLES": "SHOW", "CREATE": "ALL", "CREATE TABLE": "CREATE",
}

// snap builds a snapshot with full visibility.
func snap() *Snapshot {
	return &Snapshot{
		RoleProfiles: map[string][]string{}, UserSettings: map[string][]ProfileElement{}, RoleSettings: map[string][]ProfileElement{},
		PrivilegeParents: testParents, Capabilities: Capabilities{ShowAccess: true}, Host: "node-1",
	}
}

func (s *Snapshot) withUser(name string, defaults ...string) *Snapshot {
	s.Users = append(s.Users, UserEntity{Name: name, Storage: "local_directory", DefaultRoles: defaults, DefaultAll: len(defaults) == 1 && defaults[0] == "ALL"})
	if len(defaults) == 1 && defaults[0] == "ALL" {
		s.Users[len(s.Users)-1].DefaultRoles = nil
	}
	return s
}

func (s *Snapshot) withRoles(names ...string) *Snapshot {
	for _, n := range names {
		s.Roles = append(s.Roles, Entity{Name: n, Storage: "local_directory"})
	}
	return s
}

func (s *Snapshot) userRole(user, role string) *Snapshot {
	s.RoleGrants = append(s.RoleGrants, RoleGrant{UserName: user, GrantedRole: role})
	return s
}

func (s *Snapshot) roleRole(role, granted string) *Snapshot {
	s.RoleGrants = append(s.RoleGrants, RoleGrant{RoleName: role, GrantedRole: granted})
	return s
}

func (s *Snapshot) grant(g GrantRow) *Snapshot {
	s.Grants = append(s.Grants, g)
	return s
}

func TestRBACEffective(t *testing.T) {
	type want struct {
		def, all    string
		defOnly     []string
		defExcept   []string
		grantOption bool
		viaContains string
		incomplete  bool
	}
	cases := []struct {
		name string
		s    *Snapshot
		q    ExplainQuery
		want want
	}{
		{
			name: "nested roles",
			s: snap().withUser("u", "r1").withRoles("r1", "r2", "r3").userRole("u", "r1").roleRole("r1", "r2").roleRole("r2", "r3").
				grant(GrantRow{RoleName: "r3", Access: "SELECT", Database: "d", Table: "t"}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictAllowed, all: VerdictAllowed, viaContains: "user u > role r1 > role r2 > role r3"},
		},
		{
			name: "cyclic roles terminate",
			s: snap().withUser("u", "r1").withRoles("r1", "r2").userRole("u", "r1").roleRole("r1", "r2").roleRole("r2", "r1").
				grant(GrantRow{RoleName: "r2", Access: "SELECT", Database: "d"}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictAllowed, all: VerdictAllowed, viaContains: "role r2"},
		},
		{
			name: "non-default role only after SET ROLE",
			s: snap().withUser("u", "r1").withRoles("r1", "r2").userRole("u", "r1").userRole("u", "r2").
				grant(GrantRow{RoleName: "r2", Access: "INSERT", Database: "d", Table: "t"}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "INSERT"},
			want: want{def: VerdictDenied, all: VerdictAllowed},
		},
		{
			name: "default role all except",
			s: func() *Snapshot {
				s := snap().withRoles("r1", "r2").userRole("u", "r1").userRole("u", "r2").
					grant(GrantRow{RoleName: "r2", Access: "SELECT", Database: "d"})
				s.Users = []UserEntity{{Name: "u", DefaultAll: true, DefaultExcpt: []string{"r2"}}}
				return s
			}(),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictDenied, all: VerdictAllowed},
		},
		{
			name: "partial revoke in one role while another grants",
			s: snap().withUser("u", "ALL").withRoles("r1", "r3").userRole("u", "r1").userRole("u", "r3").
				grant(GrantRow{RoleName: "r1", Access: "SELECT", Database: "d"}).
				grant(GrantRow{RoleName: "r1", Access: "SELECT", Database: "d", Table: "t", Column: "s", PartialRevoke: true}).
				grant(GrantRow{RoleName: "r3", Access: "SELECT", Database: "d", Table: "t", Column: "s"}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t", Column: "s"},
			want: want{def: VerdictAllowed, all: VerdictAllowed, viaContains: "role r3"},
		},
		{
			name: "partial revoke without another grant leaves the rest",
			s: snap().withUser("u", "ALL").withRoles("r1").userRole("u", "r1").
				grant(GrantRow{RoleName: "r1", Access: "SELECT", Database: "d"}).
				grant(GrantRow{RoleName: "r1", Access: "SELECT", Database: "d", Table: "t", Column: "s", PartialRevoke: true}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictPartial, all: VerdictPartial, defExcept: []string{"s"}},
		},
		{
			name: "partial revoke on the same grantee removes access",
			s: snap().withUser("u").
				grant(GrantRow{UserName: "u", Access: "SELECT"}).
				grant(GrantRow{UserName: "u", Access: "SELECT", Database: "d", PartialRevoke: true}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictDenied, all: VerdictDenied},
		},
		{
			name: "column grants",
			s: snap().withUser("u").
				grant(GrantRow{UserName: "u", Access: "SELECT", Database: "d", Table: "t", Column: "a"}).
				grant(GrantRow{UserName: "u", Access: "SELECT", Database: "d", Table: "t", Column: "b"}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictPartial, all: VerdictPartial, defOnly: []string{"a", "b"}},
		},
		{
			name: "ALL covers SELECT with grant option",
			s:    snap().withUser("u").grant(GrantRow{UserName: "u", Access: "ALL", GrantOption: true}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictAllowed, all: VerdictAllowed, grantOption: true, viaContains: "user u"},
		},
		{
			name: "group covers child through parent_group",
			s:    snap().withUser("u").grant(GrantRow{UserName: "u", Access: "ALTER", Database: "d"}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "ALTER UPDATE"},
			want: want{def: VerdictAllowed, all: VerdictAllowed},
		},
		{
			name: "child does not cover group",
			s:    snap().withUser("u").grant(GrantRow{UserName: "u", Access: "ALTER UPDATE", Database: "d"}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "ALTER"},
			want: want{def: VerdictDenied, all: VerdictDenied},
		},
		{
			name: "revoke grant option keeps access",
			s: snap().withUser("u").
				grant(GrantRow{UserName: "u", Access: "SELECT", GrantOption: true}).
				grant(GrantRow{UserName: "u", Access: "SELECT", Database: "d", PartialRevoke: true, GrantOption: true}),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictAllowed, all: VerdictAllowed, grantOption: false},
		},
		{
			name: "readonly profile through a role blocks INSERT",
			s: func() *Snapshot {
				one := "1"
				s := snap().withUser("u", "r1").withRoles("r1").userRole("u", "r1").
					grant(GrantRow{UserName: "u", Access: "INSERT", Database: "d"})
				s.SettingsProfiles = []SettingsProfile{{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "ro", Settings: []ProfileSetting{{Name: "readonly", Value: &one}}}}}
				s.RoleSettings["r1"] = []ProfileElement{{Inherit: "ro"}}
				return s
			}(),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "INSERT"},
			want: want{def: VerdictBlocked, all: VerdictBlocked},
		},
		{
			name: "incomplete snapshot never says no access",
			s: func() *Snapshot {
				s := snap().withUser("u")
				s.Capabilities.ShowAccess = false
				return s
			}(),
			q:    ExplainQuery{User: "u", Database: "d", Table: "t"},
			want: want{def: VerdictUnknown, all: VerdictUnknown, incomplete: true},
		},
		{
			name: "always readable system table",
			s:    snap().withUser("u"),
			q:    ExplainQuery{User: "u", Database: "system", Table: "one"},
			want: want{def: VerdictAllowed, all: VerdictAllowed},
		},
		{
			name: "other system tables depend on the server setting",
			s:    snap().withUser("u"),
			q:    ExplainQuery{User: "u", Database: "system", Table: "parts"},
			want: want{def: VerdictConditional, all: VerdictConditional},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if err := c.q.Validate(true, nil); err != nil {
				t.Fatal(err)
			}
			e, err := Explain(c.s, c.q)
			if err != nil {
				t.Fatal(err)
			}
			d, a := e.Verdict.Default, e.Verdict.AllRoles
			if d.Status != c.want.def || a.Status != c.want.all {
				t.Fatalf("verdict = %s / %s, want %s / %s (%s | %s)", d.Status, a.Status, c.want.def, c.want.all, d.Summary, a.Summary)
			}
			if c.want.defOnly != nil && strings.Join(d.Only, ",") != strings.Join(c.want.defOnly, ",") {
				t.Fatalf("only = %v", d.Only)
			}
			if c.want.defExcept != nil && strings.Join(d.Except, ",") != strings.Join(c.want.defExcept, ",") {
				t.Fatalf("except = %v", d.Except)
			}
			if a.GrantOption != c.want.grantOption {
				t.Fatalf("grant option = %v", a.GrantOption)
			}
			if c.want.viaContains != "" && !strings.Contains(strings.Join(a.Via, "|"), c.want.viaContains) {
				t.Fatalf("via = %v", a.Via)
			}
			if e.Incomplete != c.want.incomplete {
				t.Fatalf("incomplete = %v", e.Incomplete)
			}
			for i, ev := range e.Evidence {
				if ev.Step != i+1 || ev.Effect == "" || ev.Statement == "" {
					t.Fatalf("evidence %d = %+v", i, ev)
				}
			}
		})
	}
}

func TestRBACEffectiveEvidence(t *testing.T) {
	s := snap().withUser("u", "r1").withRoles("r1", "r2").userRole("u", "r1").userRole("u", "r2").
		grant(GrantRow{RoleName: "r1", Access: "SELECT", Database: "d"}).
		grant(GrantRow{RoleName: "r1", Access: "SELECT", Database: "d", Table: "t", PartialRevoke: true}).
		grant(GrantRow{RoleName: "r2", Access: "SELECT", Database: "d", Table: "t", GrantOption: true})
	e, err := Explain(s, ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "SELECT"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Verdict.Default.Status != VerdictDenied || e.Verdict.AllRoles.Status != VerdictAllowed || !e.Verdict.AllRoles.GrantOption {
		t.Fatalf("verdict = %+v", e.Verdict)
	}
	effects := []string{}
	for _, ev := range e.Evidence {
		effects = append(effects, ev.Mode+":"+ev.Effect)
	}
	if strings.Join(effects, ",") != "both:allows,both:removes,all_roles:allows" {
		t.Fatalf("evidence = %v", effects)
	}
	if e.Evidence[2].Statement != "GRANT SELECT ON `d`.`t` TO `r2` WITH GRANT OPTION" {
		t.Fatalf("statement = %q", e.Evidence[2].Statement)
	}
}

func TestRBACEffectiveRowPolicies(t *testing.T) {
	s := snap().withUser("u", "r1").withUser("v").withRoles("r1").userRole("u", "r1").
		grant(GrantRow{UserName: "u", Access: "SELECT", Database: "d"}).
		grant(GrantRow{UserName: "v", Access: "SELECT", Database: "d"})
	s.RowPolicies = []RowPolicy{
		{FullName: "p1 ON d.t", Storage: "local_directory", RowPolicySpec: RowPolicySpec{Name: "p1", Database: "d", Table: "t", Using: "a = 1", To: Assignees{Names: []string{"r1"}}}},
		{FullName: "p2 ON d.t", Storage: "local_directory", RowPolicySpec: RowPolicySpec{Name: "p2", Database: "d", Table: "t", Using: "b = 2", Restrictive: true, To: Assignees{All: true}}},
		{FullName: "p3 ON d.t", Storage: "local_directory", RowPolicySpec: RowPolicySpec{Name: "p3", Database: "d", Table: "t", Using: "c = 3", To: Assignees{All: true, Except: []string{"r1", "v"}}}},
	}
	e, err := Explain(s, ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "SELECT"})
	if err != nil {
		t.Fatal(err)
	}
	rp := e.RowPolicies.Default
	if rp.Filter != "((a = 1)) AND (b = 2)" || rp.NoPermissive || !e.Verdict.Default.RowFiltered {
		t.Fatalf("u policies = %+v", rp)
	}
	e, _ = Explain(s, ExplainQuery{User: "v", Database: "d", Table: "t", Privilege: "SELECT"})
	rp = e.RowPolicies.Default
	if !rp.NoPermissive || rp.IfSettingOn != "Rows matching (b = 2)" || rp.IfSettingOff != "No rows" || !rp.SettingDependent {
		t.Fatalf("v policies = %+v", rp)
	}
	// INSERT is not filtered by row policies.
	e, _ = Explain(s, ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "INSERT"})
	if e.RowPolicies.Applicable {
		t.Fatal("row policies applied to INSERT")
	}
}

func TestRBACEffectiveProfileAndQuota(t *testing.T) {
	zero, four := "0", "4"
	s := snap().withUser("u", "r1").withRoles("r1").userRole("u", "r1").
		grant(GrantRow{UserName: "u", Access: "CREATE TABLE", Database: "d"})
	s.SettingsProfiles = []SettingsProfile{{ProfileSpec: ProfileSpec{Name: "p", Settings: []ProfileSetting{{Name: "max_threads", Value: &four, Writability: "CONST"}}, To: Assignees{Names: []string{"r1"}}}}}
	s.UserSettings["u"] = []ProfileElement{{ProfileSetting: ProfileSetting{Name: "allow_ddl", Value: &zero}}}
	s.Quotas = []Quota{{Storage: "replicated", QuotaSpec: QuotaSpec{Name: "q", KeyedBy: "user_name", To: Assignees{Names: []string{"u"}},
		Intervals: []QuotaInterval{{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 10}}}}}}
	s.QuotaUsage = []QuotaUsage{{Quota: "q", Key: "u", Duration: 3600, Used: map[string]float64{"queries": 3}}}
	s.MultiNode = true
	e, err := Explain(s, ExplainQuery{User: "u", Database: "d", Table: "t", Privilege: "CREATE TABLE"})
	if err != nil {
		t.Fatal(err)
	}
	if e.Verdict.Default.Status != VerdictBlocked || e.Profile.AllowDDL || strings.Join(e.Profile.Const, ",") != "max_threads" {
		t.Fatalf("profile = %+v verdict = %+v", e.Profile, e.Verdict.Default)
	}
	blocked := false
	for _, ev := range e.Evidence {
		if ev.Effect == EffectBlocks && ev.Statement == "SETTINGS allow_ddl = 0" && ev.Source == "user u" {
			blocked = true
		}
		if ev.Source == "quota q" && ev.Statement != "1 hour: queries 3 of 10" {
			t.Fatalf("quota evidence = %+v", ev)
		}
	}
	if !blocked {
		t.Fatalf("no blocking evidence: %+v", e.Evidence)
	}
	if len(e.Quotas) != 1 || e.Quotas[0].Intervals[0].Used["queries"] != 3 || e.Quotas[0].Local {
		t.Fatalf("quotas = %+v", e.Quotas)
	}
	// The user and role are local while a cluster exists; the quota is not.
	if strings.Join(e.Node.LocalEntities, ",") != "user u,role r1" {
		t.Fatalf("local = %v", e.Node.LocalEntities)
	}
}

func TestRBACEffectiveUnknownUser(t *testing.T) {
	if _, err := Explain(snap(), ExplainQuery{User: "ghost", Privilege: "SELECT"}); !errors.Is(err, ErrUnknownUser) {
		t.Fatalf("err = %v", err)
	}
	q := ExplainQuery{User: "u", Table: "t"}
	if err := q.Validate(true, nil); err == nil {
		t.Fatal("table without database accepted")
	}
	q = ExplainQuery{User: "u", Privilege: "select; drop"}
	if err := q.Validate(true, nil); err == nil {
		t.Fatal("bad privilege accepted")
	}
}

func TestRBACWhoCan(t *testing.T) {
	s := snap().withUser("a", "r1").withUser("b").withUser("c", "r1").withRoles("r1", "r2").
		userRole("a", "r1").userRole("b", "r2").
		grant(GrantRow{RoleName: "r1", Access: "SELECT", Database: "d"}).
		grant(GrantRow{RoleName: "r2", Access: "SELECT", Database: "d", Table: "t", Column: "x"})
	res := WhoCan(s, ExplainQuery{Database: "d", Table: "t", Privilege: "SELECT"})
	got := []string{}
	for _, u := range res.Users {
		got = append(got, u.User+":"+u.Default.Status+"/"+u.AllRoles.Status+":"+strings.Join(u.Columns, ","))
	}
	if strings.Join(got, " ") != "a:allowed/allowed: b:denied/partial:x" {
		t.Fatalf("who can = %v", got)
	}
	if res.Users[0].Via[0] != "user a > role r1" {
		t.Fatalf("via = %v", res.Users[0].Via)
	}
}

func TestRBACParseGrantLines(t *testing.T) {
	rows := ParseGrantLines([]string{
		"GRANT SHOW DATABASES, SELECT(id, `we ird`), INSERT ON `my db`.events TO u WITH GRANT OPTION",
		"REVOKE SELECT ON system.* FROM u",
		"REVOKE GRANT OPTION FOR INSERT ON d.* FROM u",
		"GRANT r1, r2 TO u",
		"GRANT TABLE ENGINE ON * TO u",
		"REVOKE TABLE ENGINE ON Kafka FROM u",
		"GRANT SELECT ON d.pre* TO u",
	})
	var got []string
	for _, r := range rows {
		got = append(got, strings.Join([]string{r.Access, r.Database, r.Table, r.Column, b(r.PartialRevoke), b(r.GrantOption)}, "|"))
	}
	want := []string{
		"SHOW DATABASES|my db|events||0|1",
		"SELECT|my db|events|id|0|1",
		"SELECT|my db|events|we ird|0|1",
		"INSERT|my db|events||0|1",
		"SELECT|system|||1|0",
		"INSERT|d|||1|1",
		"TABLE ENGINE||||0|0",
		"SELECT|d|pre*||0|0",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows:\n%s", strings.Join(got, "\n"))
	}
}

func b(v bool) string {
	if v {
		return "1"
	}
	return "0"
}

func TestRBACVerify(t *testing.T) {
	s := snap().withUser("u").grant(GrantRow{UserName: "u", Access: "SELECT", Database: "d"})
	q := ExplainQuery{User: "u", Database: "d", Table: "prefix_t", Privilege: "SELECT"}

	e, _ := Explain(s, q)
	Verify(e, s, "SHOW GRANTS FOR `u` WITH IMPLICIT FINAL", []string{"GRANT SELECT ON d.* TO u"}, true)
	if e.Verification.Status != "match" || len(e.Warnings) != 0 {
		t.Fatalf("match: %+v %v", e.Verification, e.Warnings)
	}

	// ClickHouse disagrees (a wildcard revoke system.grants cannot show):
	// warn and let ClickHouse win.
	e, _ = Explain(s, q)
	Verify(e, s, "SHOW GRANTS FOR `u` WITH IMPLICIT FINAL", []string{"GRANT SELECT ON d.* TO u", "REVOKE SELECT ON d.prefix* FROM u"}, true)
	if e.Verification.Status != "mismatch" || len(e.Warnings) != 1 || !strings.Contains(e.Warnings[0], "ClickHouse wins") || e.Verdict.AllRoles.Status != VerdictDenied {
		t.Fatalf("mismatch: %+v %v %+v", e.Verification, e.Warnings, e.Verdict.AllRoles)
	}

	// The implicit grants resolve the system table rule.
	sys := ExplainQuery{User: "u", Database: "system", Table: "parts", Privilege: "SELECT"}
	e, _ = Explain(s, sys)
	Verify(e, s, "SHOW GRANTS FOR `u` WITH IMPLICIT FINAL", []string{"GRANT SELECT ON system.one TO u"}, true)
	if e.Verdict.Default.Status != VerdictDenied || e.Verification.Status != "match" {
		t.Fatalf("system: %+v %+v", e.Verdict.Default, e.Verification)
	}
}

func TestRBACFingerprintIgnoresExplainerFacts(t *testing.T) {
	a := snap()
	b := snap()
	b.Host, b.MultiNode = "other", true
	b.PrivilegeParents = map[string]string{"X": "ALL"}
	b.QuotaUsage = []QuotaUsage{{Quota: "q"}}
	b.UserSettings["u"] = []ProfileElement{{Inherit: "p"}}
	if a.Fingerprint() != b.Fingerprint() {
		t.Fatal("explainer facts changed the fingerprint")
	}
}
