// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"testing"
)

// grantsOf renders t's privilege rows in s, sorted.
func grantsOf(s *Snapshot, t Target) string {
	var lines []string
	for _, g := range s.Grants {
		if g.Grantee() == t {
			lines = append(lines, renderGrantRow(g))
		}
	}
	sort.Strings(lines)
	return strings.Join(lines, "; ")
}

// The simulator follows ClickHouse: a change covers the objects inside it
// and the privileges a group covers, and rows are what system.grants shows.
func TestApplyGrantFollowsClickHouse(t *testing.T) {
	u := Target{Type: "user", Name: "u"}
	row := func(access string, sc scope, partial, opt bool) GrantRow {
		return GrantRow{UserName: "u", Access: access, Database: sc.db, Table: sc.table, Column: sc.column, PartialRevoke: partial, GrantOption: opt}
	}
	global, db, tbl, col := scope{}, scope{db: "d"}, scope{db: "d", table: "t"}, scope{db: "d", table: "t", column: "c"}
	cases := []struct {
		name string
		rows []GrantRow
		op   Op
		want string
	}{
		{"revoke inside a grant leaves a partial revoke", []GrantRow{row("SELECT", global, false, false)},
			privOp(u, []string{"SELECT"}, db, true, false), "GRANT SELECT ON *.* TO `u`; REVOKE SELECT ON `d`.* FROM `u`"},
		{"grant clears the partial revoke", []GrantRow{row("SELECT", global, false, false), row("SELECT", db, true, false)},
			privOp(u, []string{"SELECT"}, db, false, false), "GRANT SELECT ON *.* TO `u`"},
		{"revoke removes narrower grants", []GrantRow{row("SELECT", tbl, false, false), row("SELECT", col, false, true)},
			privOp(u, []string{"SELECT"}, global, true, false), ""},
		{"revoke of nothing held adds nothing", nil,
			privOp(u, []string{"SELECT"}, db, true, false), ""},
		{"revoke grant option inside", []GrantRow{row("SELECT", global, false, true)},
			privOp(u, []string{"SELECT"}, db, true, true), "GRANT SELECT ON *.* TO `u` WITH GRANT OPTION; REVOKE SELECT ON `d`.* FROM `u`"},
		{"grant option given back", []GrantRow{row("SELECT", global, false, true), row("SELECT", db, true, true)},
			privOp(u, []string{"SELECT"}, db, false, true), "GRANT SELECT ON *.* TO `u` WITH GRANT OPTION"},
		{"broader grant absorbs a narrower one", []GrantRow{row("SELECT", tbl, false, false)},
			privOp(u, []string{"SELECT"}, db, false, false), "GRANT SELECT ON `d`.* TO `u`"},
		{"covered grant adds nothing", []GrantRow{row("SELECT", db, false, false)},
			privOp(u, []string{"SELECT"}, col, false, false), "GRANT SELECT ON `d`.* TO `u`"},
		{"grant option inside a grant", []GrantRow{row("SELECT", global, false, false)},
			privOp(u, []string{"SELECT"}, db, false, true), "GRANT SELECT ON *.* TO `u`; GRANT SELECT ON `d`.* TO `u` WITH GRANT OPTION"},
		{"revoke a member of ALL", []GrantRow{row("ALL", global, false, false)},
			privOp(u, []string{"SELECT"}, db, true, false), "GRANT ALL ON *.* TO `u`; REVOKE SELECT ON `d`.* FROM `u`"},
		{"revoke ALL takes its members", []GrantRow{row("ALL", global, false, false), row("SELECT", db, true, false), row("INSERT", tbl, false, true)},
			privOp(u, []string{"ALL"}, global, true, false), ""},
		{"grant a group clears member revokes", []GrantRow{row("ALTER", global, false, false), row("ALTER UPDATE", db, true, false)},
			privOp(u, []string{"ALTER"}, db, false, false), "GRANT ALTER ON *.* TO `u`"},
		{"grant ALL makes member grants redundant", []GrantRow{row("SELECT", global, false, false), row("SELECT", db, true, false)},
			privOp(u, []string{"ALL"}, global, false, false), "GRANT ALL ON *.* TO `u`"},
		// A group row on an object inside the change does not shield its
		// members from it.
		{"grant reaches inside a narrower group row", []GrantRow{row("ALL", db, false, false)},
			privOp(u, []string{"ALTER"}, global, false, true), "GRANT ALL ON `d`.* TO `u`; GRANT ALTER ON *.* TO `u` WITH GRANT OPTION; GRANT ALTER ON `d`.* TO `u` WITH GRANT OPTION"},
		{"revoke reaches inside a narrower group row", []GrantRow{row("SELECT", global, false, false), row("ALL", db, false, false)},
			privOp(u, []string{"SELECT"}, global, true, false), "GRANT ALL ON `d`.* TO `u`; REVOKE SELECT ON `d`.* FROM `u`"},
	}
	for _, c := range cases {
		s := &Snapshot{RoleProfiles: map[string][]string{}, Grants: append([]GrantRow{}, c.rows...)}
		applyOp(s, c.op)
		if got := grantsOf(s, u); got != c.want {
			t.Errorf("%s:\n got %s\nwant %s", c.name, got, c.want)
		}
	}
}

// On one object the narrowest group decides: ALTER revoked under ALL
// takes ALTER UPDATE with it.
func TestNarrowestGroupDecides(t *testing.T) {
	u := Target{Type: "user", Name: "u"}
	s := &Snapshot{Grants: []GrantRow{
		{UserName: "u", Access: "ALL", Database: "d", GrantOption: true},
		{UserName: "u", Access: "ALTER", Database: "d", PartialRevoke: true},
	}}
	for _, parents := range []map[string]string{nil, testParents} {
		tree := privTree{parents: parents}
		if got := privilegeAt(s, tree, u, "ALTER UPDATE", scope{db: "d", table: "t"}); got != privNone {
			t.Errorf("parents %v: ALTER UPDATE level %d, want none", parents != nil, got)
		}
		if got := privilegeAt(s, tree, u, "SELECT", scope{db: "d"}); got != privWithGrantOption {
			t.Errorf("parents %v: SELECT level %d, want grant option", parents != nil, got)
		}
	}
}

// Undoing a group grant puts back the members held from a broader object,
// which REVOKE of the group alone would take.
func TestInverseRestoresCoveredMembers(t *testing.T) {
	u := Target{Type: "user", Name: "u"}
	cases := []struct {
		name string
		rows []GrantRow
		op   Op
	}{
		{"ALL on a table under SHOW TABLES on its database",
			[]GrantRow{{UserName: "u", Access: "SHOW TABLES", Database: "d"}},
			privOp(u, []string{"ALL"}, scope{db: "d", table: "t"}, false, false)},
		{"ALTER over a narrower member",
			[]GrantRow{{UserName: "u", Access: "ALTER UPDATE", Database: "d", Table: "t"}},
			privOp(u, []string{"ALTER"}, scope{}, false, true)},
		{"revoke of a group with a member held on the object",
			[]GrantRow{{UserName: "u", Access: "ALTER UPDATE", GrantOption: true}, {UserName: "u", Access: "ALTER"}},
			privOp(u, []string{"ALTER"}, scope{}, true, false)},
	}
	for _, c := range cases {
		for _, parents := range []map[string]string{nil, testParents} {
			before := &Snapshot{RoleProfiles: map[string][]string{}, Grants: c.rows, PrivilegeParents: parents}
			after := applyAll(before, []Op{c.op})
			back := applyAll(after, Inverse(c.op, before))
			if got, want := stateJSON(FromSnapshot(back)), stateJSON(FromSnapshot(before)); got != want {
				t.Errorf("%s (parents %v): inverse %s\n got %s\nwant %s", c.name, parents != nil, renderOps(Inverse(c.op, before)), got, want)
			}
		}
	}
}

// reconcileBase is what every generated snapshot shares: users (users are
// never created or dropped) and entities stored in users.xml.
func reconcileBase() *Snapshot {
	return &Snapshot{
		RoleProfiles: map[string][]string{}, RoleSettings: map[string][]ProfileElement{}, UserSettings: map[string][]ProfileElement{},
		Users: []UserEntity{
			{Name: "alice", Storage: "local_directory"}, {Name: "bob", Storage: "local_directory"},
			{Name: "carol", Storage: "replicated"}, {Name: "default", Storage: "users_xml", DefaultAll: true},
		},
		Roles: []Entity{{Name: "xml_role", Storage: "users_xml"}},
		Grants: []GrantRow{
			{UserName: "default", Access: "ALL", GrantOption: true},
			{RoleName: "xml_role", Access: "SELECT", Database: "system"},
		},
		RoleGrants: []RoleGrant{{UserName: "default", GrantedRole: "xml_role"}},
		SettingsProfiles: []SettingsProfile{
			{Storage: "users_xml", ProfileSpec: ProfileSpec{Name: "default", Settings: []ProfileSetting{{Name: "max_threads", Value: sp("8")}}}},
		},
		Quotas: []Quota{{Storage: "users_xml", QuotaSpec: QuotaSpec{Name: "default", Intervals: []QuotaInterval{{Duration: 1, Unit: "hour"}}, To: Assignees{Names: []string{"default"}}}}},
		RowPolicies: []RowPolicy{{FullName: "xml ON system.one", Storage: "users_xml", RowPolicySpec: RowPolicySpec{
			Name: "xml", Database: "system", Table: "one", Using: "1", To: Assignees{Names: []string{"default"}},
		}}},
		Capabilities: Capabilities{User: "admin_session"},
	}
}

var (
	genRoles    = []string{"r1", "r2", "r3", "r4"}
	genProfiles = []string{"p1", "p2", "p3"}
	genUsers    = []string{"alice", "bob", "carol"}
	genAccess   = []string{"ALL", "SELECT", "INSERT", "ALTER", "ALTER UPDATE", "SHOW TABLES"}
	genScopes   = []scope{{}, {db: "d1"}, {db: "d1", table: "t1"}, {db: "d1", table: "t1", column: "c1"}, {db: "d2"}, {db: "d2", table: "t2"}}
	genUsing    = []string{"1", "x = 1", "tenant = 'a'"}
	genSettings = []ProfileSetting{
		{Name: "max_threads", Value: sp("4")},
		{Name: "max_memory_usage", Value: sp("1000"), Max: sp("2000")},
		{Name: "readonly", Writability: "CHANGEABLE_IN_READONLY"},
		{Name: "load_balancing", Value: sp("random"), Writability: "CONST"},
	}
)

func pick[T any](rng *rand.Rand, list []T) T { return list[rng.Intn(len(list))] }

func localRoles(s *Snapshot) []string {
	var out []string
	for _, r := range s.Roles {
		if r.Storage != "users_xml" {
			out = append(out, r.Name)
		}
	}
	return out
}

func randomAssignees(rng *rand.Rand, s *Snapshot) Assignees {
	pool := append(append([]string{}, genUsers...), localRoles(s)...)
	var names []string
	for _, n := range pool {
		if rng.Intn(3) == 0 {
			names = append(names, n)
		}
	}
	switch rng.Intn(4) {
	case 0:
		return Assignees{All: true, Except: names}
	case 1:
		return Assignees{}
	}
	return Assignees{Names: names}
}

// randomize applies n random ops to a copy of base through the simulator, so
// the result is a state the server could be in, then drops dangling
// references the way ClickHouse would not let them exist.
func randomize(base *Snapshot, rng *rand.Rand, n int) *Snapshot {
	s := cloneSnapshot(base)
	for i := 0; i < n; i++ {
		roles := localRoles(s)
		var op Op
		switch rng.Intn(14) {
		case 0, 1:
			name := pick(rng, genRoles)
			if s.role(name) != nil {
				continue
			}
			op = Op{Kind: OpCreateRole, Name: name}
			if rng.Intn(2) == 0 && len(s.SettingsProfiles) > 0 {
				op.Profile = pick(rng, s.SettingsProfiles).Name
			}
		case 2:
			if len(roles) == 0 {
				continue
			}
			op = Op{Kind: OpDropRole, Name: pick(rng, roles)}
		case 3:
			if len(roles) == 0 {
				continue
			}
			op = Op{Kind: OpSetRoleProfile, Name: pick(rng, roles)}
			if rng.Intn(3) > 0 {
				op.Profile = pick(rng, s.SettingsProfiles).Name
			}
		case 4, 5, 6:
			grantees := []Target{}
			for _, u := range genUsers {
				grantees = append(grantees, Target{Type: "user", Name: u})
			}
			for _, r := range roles {
				grantees = append(grantees, Target{Type: "role", Name: r})
			}
			access := []string{pick(rng, genAccess)}
			if rng.Intn(4) == 0 {
				access = append(access, pick(rng, genAccess))
			}
			op = privOp(pick(rng, grantees), access, pick(rng, genScopes), rng.Intn(3) == 0, rng.Intn(3) == 0)
		case 7:
			// Roles go to users, and to roles later in the list, so role
			// grants never form a cycle.
			granted := append([]string{"xml_role"}, roles...)
			role := pick(rng, granted)
			var grantee Target
			if rng.Intn(2) == 0 || len(roles) == 0 {
				grantee = Target{Type: "user", Name: pick(rng, genUsers)}
			} else {
				grantee = Target{Type: "role", Name: pick(rng, roles)}
				if grantee.Name <= role && role != "xml_role" {
					continue
				}
			}
			kind := OpGrantRole
			if rng.Intn(3) == 0 {
				kind = OpRevokeRole
			}
			op = Op{Kind: kind, Target: &grantee, Roles: []string{role}, AdminOption: rng.Intn(3) == 0}
		case 8:
			u := s.user(pick(rng, genUsers))
			t := Target{Type: "user", Name: u.Name}
			var held []string
			for _, g := range s.RoleGrants {
				if g.Grantee() == t {
					held = append(held, g.GrantedRole)
				}
			}
			op = Op{Kind: OpSetDefaultRoles, Target: &t}
			switch rng.Intn(3) {
			case 0:
				op.DefaultAll = true
				if len(held) > 0 && rng.Intn(2) == 0 {
					op.DefaultExcept = []string{pick(rng, held)}
				}
			case 1:
				for _, h := range held {
					if rng.Intn(2) == 0 {
						op.Roles = append(op.Roles, h)
					}
				}
			}
		case 9:
			p := RowPolicySpec{Name: pick(rng, []string{"pol1", "pol2"}), Database: "d1", Table: pick(rng, []string{"t1", "t2"}),
				Using: pick(rng, genUsing), Restrictive: rng.Intn(3) == 0, To: randomAssignees(rng, s)}
			if rng.Intn(4) == 0 {
				op = Op{Kind: OpDropRowPolicy, RowPolicy: &p}
			} else {
				op = Op{Kind: OpCreateRowPolicy, RowPolicy: &p}
			}
		case 10:
			q := QuotaSpec{Name: pick(rng, []string{"q1", "q2"}), KeyedBy: pick(rng, QuotaKeys), To: randomAssignees(rng, s)}
			for _, unit := range []string{"minute", "hour", "day"} {
				if rng.Intn(2) == 0 {
					iv := QuotaInterval{Duration: uint64(1 + rng.Intn(3)), Unit: unit, Randomized: rng.Intn(4) == 0, Limits: map[string]uint64{}}
					for _, res := range []string{"queries", "errors", "read_rows"} {
						if rng.Intn(2) == 0 {
							iv.Limits[res] = uint64(1 + rng.Intn(100))
						}
					}
					q.Intervals = append(q.Intervals, iv)
				}
			}
			op = Op{Kind: OpCreateQuota, Quota: &q}
		case 11:
			op = Op{Kind: OpDropQuota, Name: pick(rng, []string{"q1", "q2"})}
		case 12:
			// A profile inherits only default or profiles before it, so
			// inheritance never cycles.
			idx := rng.Intn(len(genProfiles))
			p := ProfileSpec{Name: genProfiles[idx], To: randomAssignees(rng, s)}
			for _, st := range genSettings {
				if rng.Intn(2) == 0 {
					p.Settings = append(p.Settings, st)
				}
			}
			parents := append([]string{"default"}, genProfiles[:idx]...)
			if rng.Intn(2) == 0 {
				p.Inherit = []string{pick(rng, parents)}
			}
			op = Op{Kind: OpCreateSettingsProfile, SettingsProfile: &p}
		case 13:
			op = Op{Kind: OpDropSettingsProfile, Name: pick(rng, genProfiles)}
		}
		applyOp(s, op)
	}
	sanitize(s)
	return s
}

func sanitize(s *Snapshot) {
	roleOK := func(n string) bool { return s.role(n) != nil }
	assigneeOK := func(n string) bool { return roleOK(n) || s.user(n) != nil }
	keep := func(list []string, ok func(string) bool) []string {
		var out []string
		for _, n := range list {
			if ok(n) {
				out = append(out, n)
			}
		}
		return out
	}
	for i := range s.Users {
		u := &s.Users[i]
		t := Target{Type: "user", Name: u.Name}
		u.DefaultRoles = keep(u.DefaultRoles, func(n string) bool { return s.roleGrant(t, n) != nil })
		u.DefaultExcpt = keep(u.DefaultExcpt, roleOK)
	}
	for role, profiles := range s.RoleProfiles {
		if !roleOK(role) || s.profile(firstOf(profiles)) == nil {
			delete(s.RoleProfiles, role)
		}
	}
	for i := range s.SettingsProfiles {
		p := &s.SettingsProfiles[i]
		p.Inherit = keep(p.Inherit, func(n string) bool { return s.profile(n) != nil })
		p.To.Names, p.To.Except = keep(p.To.Names, assigneeOK), keep(p.To.Except, assigneeOK)
	}
	for i := range s.RowPolicies {
		to := &s.RowPolicies[i].To
		to.Names, to.Except = keep(to.Names, assigneeOK), keep(to.Except, assigneeOK)
	}
	for i := range s.Quotas {
		to := &s.Quotas[i].To
		to.Names, to.Except = keep(to.Names, assigneeOK), keep(to.Except, assigneeOK)
	}
}

func applyAll(s *Snapshot, ops []Op) *Snapshot {
	after := cloneSnapshot(s)
	for _, op := range ops {
		applyOp(after, op)
	}
	return after
}

func stateJSON(st *State) string {
	b, _ := json.MarshalIndent(st.Canonical(), "", " ")
	return string(b)
}

func renderOps(ops []Op) string {
	var out []string
	for _, op := range ops {
		out = append(out, Render(op, "", nil))
	}
	return strings.Join(out, "\n")
}

// checkConverges runs Reconcile with every kind pruned and checks the
// plan reaches desired, is a no-op the second time, passes the order check
// and, when small enough, composes.
func checkConverges(t *testing.T, label string, cur *Snapshot, desired *State) []Op {
	t.Helper()
	ops, skipped := Reconcile(cur, desired, ReconcileOpts{Prune: PruneAll()})
	if len(skipped) > 0 {
		t.Fatalf("%s: unexpected skips %+v", label, skipped)
	}
	after := applyAll(cur, ops)
	if got, want := stateJSON(FromSnapshot(after)), stateJSON(desired); got != want {
		t.Fatalf("%s: did not converge\nops:\n%s\n got %s\nwant %s", label, renderOps(ops), got, want)
	}
	if again, _ := Reconcile(after, desired, ReconcileOpts{Prune: PruneAll()}); len(again) != 0 {
		t.Fatalf("%s: second reconcile not empty:\n%s", label, renderOps(again))
	}
	if err := checkOrder(ops, cur); err != nil {
		t.Fatalf("%s: %v\n%s", label, err, renderOps(ops))
	}
	if len(ops) > 0 && len(ops) <= 200 {
		if _, err := Compose(ops, cur, Catalog{}, "", GuardContext{}); err != nil {
			t.Fatalf("%s: compose: %v\n%s", label, err, renderOps(ops))
		}
	}
	return ops
}

// baseFor rotates between the name-prefix fallback for privilege groups, a
// small hierarchy, and the system.privileges catalog of ClickHouse 25.8.
func baseFor(t *testing.T, seed int64) *Snapshot {
	base := reconcileBase()
	switch seed % 3 {
	case 1:
		base.PrivilegeParents = testParents
	case 2:
		cat := catalogSnapshot(t)
		base.PrivilegeParents, base.PrivilegeLevels = cat.PrivilegeParents, cat.PrivilegeLevels
	}
	return base
}

func TestReconcileConvergesRandom(t *testing.T) {
	total, composed := 0, 0
	for seed := int64(0); seed < 240; seed++ {
		base := baseFor(t, seed)
		rng := rand.New(rand.NewSource(seed))
		cur := randomize(base, rng, 10+rng.Intn(40))
		des := randomize(base, rng, 10+rng.Intn(40))
		ops := checkConverges(t, fmt.Sprintf("seed %d", seed), cur, FromSnapshot(des))
		total += len(ops)
		if len(ops) > 0 && len(ops) <= 200 {
			composed++
		}
		// The way back converges too.
		checkConverges(t, fmt.Sprintf("seed %d back", seed), applyAll(cur, ops), FromSnapshot(cur))
	}
	if total == 0 || composed == 0 {
		t.Fatal("the generator produced no changes")
	}
}

// Rolling back a reconcile plan with the inverses Compose records returns
// the server to where it was.
func TestReconcileRollbackRestores(t *testing.T) {
	for seed := int64(0); seed < 240; seed++ {
		base := baseFor(t, seed)
		rng := rand.New(rand.NewSource(seed))
		cur := randomize(base, rng, 10+rng.Intn(30))
		des := randomize(base, rng, 10+rng.Intn(30))
		ops, _ := Reconcile(cur, FromSnapshot(des), ReconcileOpts{Prune: PruneAll()})
		if len(ops) == 0 || len(ops) > 200 {
			continue
		}
		plan, err := Compose(ops, cur, Catalog{}, "", GuardContext{})
		if err != nil {
			t.Fatal(err)
		}
		results := make([]Result, len(plan.Statements))
		for i, s := range plan.Statements {
			results[i] = Result{Seq: s.Seq, Status: StatusApplied}
		}
		back := applyAll(applyAll(cur, ops), RollbackOps(plan, results))
		if got, want := stateJSON(FromSnapshot(back)), stateJSON(FromSnapshot(cur)); got != want {
			t.Fatalf("seed %d: rollback did not restore\nops:\n%s\nrollback:\n%s\n got %s\nwant %s", seed, renderOps(ops), renderOps(RollbackOps(plan, results)), got, want)
		}
	}
}

func TestReconcileConvergesFixtures(t *testing.T) {
	withGrants := func(rows ...GrantRow) *Snapshot {
		s := reconcileBase()
		s.Roles = append(s.Roles, Entity{Name: "r", Storage: "local_directory"})
		s.Grants = append(s.Grants, rows...)
		return s
	}
	cases := []struct {
		name     string
		cur, des *Snapshot
		want     string // the plan, one statement per line
	}{
		{"partial revoke then regrant inside",
			withGrants(),
			withGrants(GrantRow{RoleName: "r", Access: "SELECT"}, GrantRow{RoleName: "r", Access: "SELECT", Database: "d", PartialRevoke: true}, GrantRow{RoleName: "r", Access: "SELECT", Database: "d", Table: "t"}),
			"GRANT SELECT ON *.* TO `r`\nREVOKE SELECT ON `d`.* FROM `r`\nGRANT SELECT ON `d`.`t` TO `r`"},
		{"narrow to broad",
			withGrants(GrantRow{RoleName: "r", Access: "SELECT", Database: "d", Table: "t"}),
			withGrants(GrantRow{RoleName: "r", Access: "SELECT", Database: "d"}),
			"GRANT SELECT ON `d`.* TO `r`"},
		{"broad to narrow revokes last",
			withGrants(GrantRow{RoleName: "r", Access: "SELECT", Database: "d"}),
			withGrants(GrantRow{RoleName: "r", Access: "SELECT", Database: "d", Table: "t"}),
			"REVOKE SELECT ON `d`.* FROM `r`\nGRANT SELECT ON `d`.`t` TO `r`"},
		{"grant option only",
			withGrants(GrantRow{RoleName: "r", Access: "INSERT", GrantOption: true}),
			withGrants(GrantRow{RoleName: "r", Access: "INSERT"}),
			"REVOKE GRANT OPTION FOR INSERT ON *.* FROM `r`"},
		{"same shape folds into one statement",
			withGrants(),
			withGrants(GrantRow{RoleName: "r", Access: "SELECT", Database: "d"}, GrantRow{RoleName: "r", Access: "INSERT", Database: "d"}, GrantRow{RoleName: "r", Access: "SHOW TABLES", Database: "d"}),
			"GRANT INSERT, SELECT, SHOW TABLES ON `d`.* TO `r`"},
		{"member revoke under ALL",
			withGrants(GrantRow{RoleName: "r", Access: "INSERT", Database: "d"}),
			withGrants(GrantRow{RoleName: "r", Access: "ALL"}, GrantRow{RoleName: "r", Access: "ALTER UPDATE", Database: "d", PartialRevoke: true}),
			"GRANT ALL ON *.* TO `r`\nREVOKE ALTER UPDATE ON `d`.* FROM `r`"},
		{"new role with profile, members and assignments",
			reconcileBase(),
			func() *Snapshot {
				s := reconcileBase()
				s.SettingsProfiles = append(s.SettingsProfiles, SettingsProfile{Storage: "local_directory", ProfileSpec: ProfileSpec{
					Name: "p", Inherit: []string{"default"}, Settings: []ProfileSetting{{Name: "max_threads", Value: sp("2")}}, To: Assignees{Names: []string{"new"}}}})
				s.Roles = append(s.Roles, Entity{Name: "new", Storage: "local_directory"})
				s.RoleProfiles["new"] = []string{"p"}
				s.RoleGrants = append(s.RoleGrants, RoleGrant{UserName: "alice", GrantedRole: "new", AdminOption: true})
				s.Users[0].DefaultRoles = []string{"new"}
				s.RowPolicies = append(s.RowPolicies, RowPolicy{Storage: "local_directory", RowPolicySpec: RowPolicySpec{Name: "pol", Database: "d", Table: "t", Using: "x = 1", To: Assignees{Names: []string{"new"}}}})
				return s
			}(),
			strings.Join([]string{
				"CREATE SETTINGS PROFILE `p` SETTINGS INHERIT 'default', max_threads = 2 TO NONE",
				"CREATE ROLE `new` SETTINGS PROFILE 'p'",
				"GRANT `new` TO `alice` WITH ADMIN OPTION",
				"ALTER USER `alice` DEFAULT ROLE `new`",
				"ALTER SETTINGS PROFILE `p` SETTINGS INHERIT 'default', max_threads = 2 TO `new`",
				"CREATE ROW POLICY `pol` ON `d`.`t` FOR SELECT USING (x = 1) AS PERMISSIVE TO `new`",
			}, "\n")},
		{"everything removed, drops last",
			func() *Snapshot {
				s := withGrants(GrantRow{RoleName: "r", Access: "SELECT"})
				s.RoleGrants = append(s.RoleGrants, RoleGrant{UserName: "bob", GrantedRole: "r"}, RoleGrant{UserName: "bob", GrantedRole: "xml_role", AdminOption: true})
				s.SettingsProfiles = append(s.SettingsProfiles,
					SettingsProfile{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "child", Inherit: []string{"parent"}}},
					SettingsProfile{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "parent"}})
				s.Quotas = append(s.Quotas, Quota{Storage: "local_directory", QuotaSpec: QuotaSpec{Name: "q", To: Assignees{Names: []string{"r"}}}})
				return s
			}(),
			func() *Snapshot {
				s := reconcileBase()
				s.RoleGrants = append(s.RoleGrants, RoleGrant{UserName: "bob", GrantedRole: "xml_role"})
				return s
			}(),
			strings.Join([]string{
				"REVOKE ADMIN OPTION FOR `xml_role` FROM `bob`",
				"DROP QUOTA IF EXISTS `q`",
				"DROP ROLE IF EXISTS `r`",
				"DROP SETTINGS PROFILE IF EXISTS `child`",
				"DROP SETTINGS PROFILE IF EXISTS `parent`",
			}, "\n")},
		{"quota and policy changes alter in place",
			func() *Snapshot {
				s := reconcileBase()
				s.Quotas = append(s.Quotas, Quota{Storage: "local_directory", QuotaSpec: QuotaSpec{Name: "q", Intervals: []QuotaInterval{{Duration: 1, Unit: "hour", Limits: map[string]uint64{"queries": 5}}}}})
				s.RowPolicies = append(s.RowPolicies, RowPolicy{Storage: "local_directory", RowPolicySpec: RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", To: Assignees{All: true}}})
				return s
			}(),
			func() *Snapshot {
				s := reconcileBase()
				s.Quotas = append(s.Quotas, Quota{Storage: "local_directory", QuotaSpec: QuotaSpec{Name: "q", Intervals: []QuotaInterval{{Duration: 60, Unit: "minute", Limits: map[string]uint64{"errors": 1}}}}})
				s.RowPolicies = append(s.RowPolicies, RowPolicy{Storage: "local_directory", RowPolicySpec: RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", Restrictive: true, To: Assignees{All: true, Except: []string{"bob"}}}})
				return s
			}(),
			strings.Join([]string{
				"ALTER ROW POLICY `p` ON `d`.`t` FOR SELECT USING (1) AS RESTRICTIVE TO ALL EXCEPT `bob`",
				"ALTER QUOTA `q` NOT KEYED FOR INTERVAL 1 hour MAX errors = 1 TO NONE",
			}, "\n")},
	}
	for _, c := range cases {
		ops := checkConverges(t, c.name, c.cur, FromSnapshot(c.des))
		if got := renderOps(ops); got != c.want {
			t.Errorf("%s:\n got\n%s\nwant\n%s", c.name, got, c.want)
		}
	}
}

func TestReconcileNoChange(t *testing.T) {
	for _, s := range []*Snapshot{reconcileBase(), richSnapshot(), randomize(reconcileBase(), rand.New(rand.NewSource(7)), 60)} {
		ops, skipped := Reconcile(s, FromSnapshot(s), ReconcileOpts{Prune: PruneAll()})
		if len(ops) != 0 || len(skipped) != 0 {
			t.Fatalf("ops %s skipped %+v", renderOps(ops), skipped)
		}
	}
}

// pruneFixture has one of every prunable thing that desired leaves out.
func pruneFixture() (cur *Snapshot, desired *State) {
	cur = reconcileBase()
	cur.Roles = append(cur.Roles, Entity{Name: "kept", Storage: "local_directory"}, Entity{Name: "extra", Storage: "local_directory"})
	cur.Grants = append(cur.Grants,
		GrantRow{RoleName: "kept", Access: "SELECT"},
		GrantRow{RoleName: "kept", Access: "INSERT", Database: "d", GrantOption: true})
	cur.RoleGrants = append(cur.RoleGrants, RoleGrant{RoleName: "kept", GrantedRole: "extra"}, RoleGrant{UserName: "alice", GrantedRole: "kept", AdminOption: true})
	cur.RowPolicies = append(cur.RowPolicies, RowPolicy{Storage: "local_directory", RowPolicySpec: RowPolicySpec{Name: "pol", Database: "d", Table: "t", Using: "1"}})
	cur.Quotas = append(cur.Quotas, Quota{Storage: "local_directory", QuotaSpec: QuotaSpec{Name: "q"}})
	cur.SettingsProfiles = append(cur.SettingsProfiles, SettingsProfile{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "prof"}})

	des := reconcileBase()
	des.Roles = append(des.Roles, Entity{Name: "kept", Storage: "local_directory"})
	des.Grants = append(des.Grants, GrantRow{RoleName: "kept", Access: "INSERT", Database: "d"})
	des.RoleGrants = append(des.RoleGrants, RoleGrant{UserName: "alice", GrantedRole: "kept"})
	return cur, FromSnapshot(des)
}

func TestReconcilePrunePerKind(t *testing.T) {
	cases := []struct {
		name  string
		prune Prune
		want  []string
	}{
		{"nothing pruned", Prune{}, nil},
		{"roles", Prune{Roles: true}, []string{"DROP ROLE IF EXISTS `extra`"}},
		{"grants", Prune{Grants: true}, []string{"REVOKE SELECT ON *.* FROM `kept`", "REVOKE GRANT OPTION FOR INSERT ON `d`.* FROM `kept`"}},
		{"role grants", Prune{RoleGrants: true}, []string{"REVOKE `extra` FROM `kept`", "REVOKE ADMIN OPTION FOR `kept` FROM `alice`"}},
		{"row policies", Prune{RowPolicies: true}, []string{"DROP ROW POLICY IF EXISTS `pol` ON `d`.`t`"}},
		{"quotas", Prune{Quotas: true}, []string{"DROP QUOTA IF EXISTS `q`"}},
		{"profiles", Prune{Profiles: true}, []string{"DROP SETTINGS PROFILE IF EXISTS `prof`"}},
		// A role about to be dropped is not revoked first.
		{"roles and role grants", Prune{Roles: true, RoleGrants: true}, []string{"REVOKE ADMIN OPTION FOR `kept` FROM `alice`", "DROP ROLE IF EXISTS `extra`"}},
	}
	for _, c := range cases {
		cur, desired := pruneFixture()
		ops, skipped := Reconcile(cur, desired, ReconcileOpts{Prune: c.prune})
		if len(skipped) != 0 {
			t.Errorf("%s: skipped %+v", c.name, skipped)
		}
		if got, want := renderOps(ops), strings.Join(c.want, "\n"); got != want {
			t.Errorf("%s:\n got\n%s\nwant\n%s", c.name, got, want)
		}
		// Whatever is pruned or not, what desired lists holds afterwards
		// and a second run changes nothing.
		after := applyAll(cur, ops)
		if again, _ := Reconcile(after, desired, ReconcileOpts{Prune: c.prune}); len(again) != 0 {
			t.Errorf("%s: second run:\n%s", c.name, renderOps(again))
		}
		if after.role("kept") == nil || after.roleGrant(Target{Type: "user", Name: "alice"}, "kept") == nil {
			t.Errorf("%s: lost what desired lists", c.name)
		}
	}
}

// Without pruning, a listed grant never lowers what is held, while a listed
// partial revoke is applied.
func TestReconcileNoPruneGrants(t *testing.T) {
	cur := reconcileBase()
	cur.Roles = append(cur.Roles, Entity{Name: "r", Storage: "local_directory"})
	cur.Grants = append(cur.Grants, GrantRow{RoleName: "r", Access: "SELECT", GrantOption: true}, GrantRow{RoleName: "r", Access: "INSERT", Database: "x"})
	des := reconcileBase()
	des.Roles = append(des.Roles, Entity{Name: "r", Storage: "local_directory"})
	des.Grants = append(des.Grants,
		GrantRow{RoleName: "r", Access: "SELECT"},
		GrantRow{RoleName: "r", Access: "SELECT", Database: "secret", PartialRevoke: true},
		GrantRow{RoleName: "r", Access: "INSERT", Database: "y"})
	ops, _ := Reconcile(cur, FromSnapshot(des), ReconcileOpts{})
	want := "GRANT INSERT ON `y`.* TO `r`\nREVOKE SELECT ON `secret`.* FROM `r`"
	if got := renderOps(ops); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestReconcileSkipsReadOnly(t *testing.T) {
	cur := reconcileBase()
	des := reconcileBase()
	des.Grants[0].GrantOption = false                                                 // default user's ALL
	des.Grants = append(des.Grants, GrantRow{RoleName: "xml_role", Access: "INSERT"}) // xml role privilege
	des.RoleGrants = nil                                                              // default user's role grant
	des.Users[3].DefaultAll = false                                                   // default user's default roles
	des.SettingsProfiles[0].Settings = nil                                            // users.xml profile
	des.Quotas[0].KeyedBy = "user_name"                                               // users.xml quota
	des.RowPolicies[0].Using = "0"                                                    // users.xml policy
	des.RoleProfiles["xml_role"] = []string{"default"}                                // users.xml role profile
	ops, skipped := Reconcile(cur, FromSnapshot(des), ReconcileOpts{Prune: PruneAll()})
	if len(ops) != 0 {
		t.Fatalf("ops on read-only entities:\n%s", renderOps(ops))
	}
	got := map[string]bool{}
	for _, s := range skipped {
		if !strings.Contains(s.Reason, "users.xml") {
			t.Errorf("reason %q", s.Reason)
		}
		got[s.Kind+":"+s.Name] = true
	}
	for _, want := range []string{"user:default", "role:xml_role", "settings_profile:default", "quota:default", "row_policy:xml ON system.one"} {
		if !got[want] {
			t.Errorf("no skip for %s in %+v", want, skipped)
		}
	}

	// Pruning never drops what users.xml defines.
	ops, skipped = Reconcile(cur, &State{Version: StateVersion}, ReconcileOpts{Prune: PruneAll()})
	for _, op := range ops {
		if op.Kind == OpDropRole || op.Kind == OpDropQuota || op.Kind == OpDropSettingsProfile || op.Kind == OpDropRowPolicy {
			t.Errorf("dropped %s", Render(op, "", nil))
		}
	}
	if len(skipped) == 0 {
		t.Fatal("no skips for users.xml entities left out of the desired state")
	}
}

func TestReconcileMissingUser(t *testing.T) {
	cur := reconcileBase()
	desired := FromSnapshot(cur)
	desired.Users = append(desired.Users, StateUser{Name: "ghost", DefaultRoles: []string{"xml_role"}})
	desired.Grants = append(desired.Grants, GranteeGrants{Type: "user", Name: "ghost", Privileges: []StateGrant{{Access: []string{"SELECT"}}}})
	desired.RoleGrants = append(desired.RoleGrants, StateRoleGrant{Type: "user", Name: "ghost", Role: "xml_role"})
	ops, skipped := Reconcile(cur, desired, ReconcileOpts{Prune: PruneAll()})
	if len(ops) != 0 {
		t.Fatalf("ops for a missing user:\n%s", renderOps(ops))
	}
	if len(skipped) != 1 || skipped[0].Name != "ghost" || !strings.HasPrefix(skipped[0].Reason, "user not found") {
		t.Fatalf("skipped = %+v", skipped)
	}
}

func TestReconcileUnrepresentable(t *testing.T) {
	cur := reconcileBase()
	cur.Roles = append(cur.Roles, Entity{Name: "direct", Storage: "local_directory"})
	cur.RoleSettings["direct"] = []ProfileElement{{ProfileSetting: ProfileSetting{Name: "max_threads", Value: sp("1")}}}
	desired := FromSnapshot(cur)
	desired.Roles = append(desired.Roles,
		StateRole{Name: "two", Profiles: []string{"default", "default2"}},
		StateRole{Name: "dangling", Profiles: []string{"nope"}})
	desired.Roles[0].Profiles = []string{"default"} // "direct" has settings set on the role
	desired.RowPolicies = append(desired.RowPolicies, RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", To: Assignees{Names: []string{"nobody"}}})
	desired.SettingsProfiles = append(desired.SettingsProfiles, ProfileSpec{Name: "orphan", Inherit: []string{"missing"}})
	desired.Grants = append(desired.Grants, GranteeGrants{Type: "role", Name: "undeclared", Privileges: []StateGrant{{Access: []string{"SELECT"}}}})
	ops, skipped := Reconcile(cur, desired, ReconcileOpts{Prune: PruneAll()})
	want := "CREATE ROLE `dangling`\nCREATE ROLE `two`"
	if got := renderOps(ops); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
	reasons := map[string]string{}
	for _, s := range skipped {
		reasons[s.Kind+":"+s.Name] = s.Reason
	}
	for key, part := range map[string]string{
		"role:direct":             "set on the role itself",
		"role:two":                "only one can be set",
		"role:dangling":           `"nope" does not exist`,
		"row_policy:p ON d.t":     `"nobody"`,
		"settings_profile:orphan": `"missing"`,
		"role:undeclared":         "not one of its roles",
	} {
		if !strings.Contains(reasons[key], part) {
			t.Errorf("%s: reason %q, want it to mention %s", key, reasons[key], part)
		}
	}
}

func TestReconcileEmptyDesiredDropsAll(t *testing.T) {
	cur := randomize(reconcileBase(), rand.New(rand.NewSource(3)), 80)
	ops, _ := Reconcile(cur, &State{Version: StateVersion}, ReconcileOpts{Prune: PruneAll()})
	after := applyAll(cur, ops)
	if len(localRoles(after)) != 0 {
		t.Errorf("roles left: %v", localRoles(after))
	}
	for _, q := range after.Quotas {
		if q.Storage != "users_xml" {
			t.Errorf("quota %s left", q.Name)
		}
	}
	for _, p := range after.SettingsProfiles {
		if p.Storage != "users_xml" {
			t.Errorf("profile %s left", p.Name)
		}
	}
	for _, p := range after.RowPolicies {
		if p.Storage != "users_xml" {
			t.Errorf("policy %s left", p.FullName)
		}
	}
	// Users are not in the desired state, so their grants stay.
	if grantsOf(after, Target{Type: "user", Name: "alice"}) != grantsOf(cur, Target{Type: "user", Name: "alice"}) {
		t.Error("an undeclared user's grants changed")
	}
}

func TestReconcileRejectsNewerState(t *testing.T) {
	ops, skipped := Reconcile(reconcileBase(), &State{Version: StateVersion + 1}, ReconcileOpts{Prune: PruneAll()})
	if len(ops) != 0 || len(skipped) != 1 || skipped[0].Kind != "state" {
		t.Fatalf("ops %v skipped %+v", ops, skipped)
	}
}
