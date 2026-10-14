// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"encoding/json"
	"sort"
	"strings"
)

// Diffs renders every entity the ops touch twice: as it is in the snapshot
// and as it would be after the ops. Both sides use the same canonical
// rendering, one statement per line, so the diff shows only real changes.
func Diffs(ops []Op, snap *Snapshot) []Diff {
	after := cloneSnapshot(snap)
	for _, op := range ops {
		applyOp(after, op)
	}
	type ref struct{ kind, name string }
	var refs []ref
	seen := map[ref]bool{}
	add := func(kind, name string) {
		r := ref{kind, name}
		if name != "" && !seen[r] {
			seen[r] = true
			refs = append(refs, r)
		}
	}
	for _, op := range ops {
		for _, r := range touched(op) {
			add(r[0], r[1])
		}
	}
	out := []Diff{}
	for _, r := range refs {
		cur, next := renderEntity(snap, r.kind, r.name), renderEntity(after, r.kind, r.name)
		out = append(out, Diff{Kind: r.kind, Entity: r.name, Current: cur, Proposed: next})
	}
	return out
}

// touched returns the (kind, name) pairs an op changes.
func touched(op Op) [][2]string {
	switch op.Kind {
	case OpCreateRole, OpDropRole, OpSetRoleProfile:
		return [][2]string{{KindRole, op.Name}}
	case OpGrantPrivilege, OpRevokePrivilege, OpGrantRole, OpRevokeRole, OpSetDefaultRoles:
		if op.Target == nil {
			return nil
		}
		return [][2]string{{op.Target.Type, op.Target.Name}}
	case OpCreateRowPolicy, OpAlterRowPolicy, OpDropRowPolicy:
		if op.RowPolicy == nil {
			return nil
		}
		return [][2]string{{KindRowPolicy, policyFullName(*op.RowPolicy)}}
	case OpCreateQuota, OpAlterQuota:
		return [][2]string{{KindQuota, op.Quota.Name}}
	case OpDropQuota:
		return [][2]string{{KindQuota, op.Name}}
	case OpCreateSettingsProfile, OpAlterSettingsProfile:
		return [][2]string{{KindSettingsProfile, op.SettingsProfile.Name}}
	case OpDropSettingsProfile:
		return [][2]string{{KindSettingsProfile, op.Name}}
	}
	return nil
}

func policyFullName(p RowPolicySpec) string {
	return p.Name + " ON " + p.Database + "." + p.Table
}

func cloneSnapshot(s *Snapshot) *Snapshot {
	b, _ := json.Marshal(s)
	var c Snapshot
	_ = json.Unmarshal(b, &c)
	if c.RoleProfiles == nil {
		c.RoleProfiles = map[string][]string{}
	}
	return &c
}

func columnsOrBlank(cols []string) []string {
	if len(cols) == 0 {
		return []string{""}
	}
	return cols
}

func normDB(s string) string {
	if s == "*" {
		return ""
	}
	return s
}

// applyOp mutates s the way ClickHouse would, closely enough for a diff.
func applyOp(s *Snapshot, op Op) {
	switch op.Kind {
	case OpCreateRole:
		s.Roles = append(s.Roles, Entity{Name: op.Name, Storage: "local_directory"})
		if op.Profile != "" {
			s.RoleProfiles[op.Name] = []string{op.Profile}
		}
	case OpDropRole:
		roles := s.Roles[:0]
		for _, r := range s.Roles {
			if r.Name != op.Name {
				roles = append(roles, r)
			}
		}
		s.Roles = roles
		grants := s.Grants[:0]
		for _, g := range s.Grants {
			if g.RoleName != op.Name {
				grants = append(grants, g)
			}
		}
		s.Grants = grants
		rgs := s.RoleGrants[:0]
		for _, g := range s.RoleGrants {
			if g.GrantedRole != op.Name && g.RoleName != op.Name {
				rgs = append(rgs, g)
			}
		}
		s.RoleGrants = rgs
		delete(s.RoleProfiles, op.Name)
		// ClickHouse also drops the role from default roles and TO lists.
		for i := range s.Users {
			s.Users[i].DefaultRoles = without(s.Users[i].DefaultRoles, op.Name)
			s.Users[i].DefaultExcpt = without(s.Users[i].DefaultExcpt, op.Name)
		}
		for i := range s.RowPolicies {
			s.RowPolicies[i].To = assigneesWithout(s.RowPolicies[i].To, op.Name)
		}
		for i := range s.Quotas {
			s.Quotas[i].To = assigneesWithout(s.Quotas[i].To, op.Name)
		}
		for i := range s.SettingsProfiles {
			s.SettingsProfiles[i].To = assigneesWithout(s.SettingsProfiles[i].To, op.Name)
		}
	case OpSetRoleProfile:
		if op.Profile == "" {
			delete(s.RoleProfiles, op.Name)
		} else {
			s.RoleProfiles[op.Name] = []string{op.Profile}
		}
	case OpGrantPrivilege, OpRevokePrivilege:
		applyPrivilegeOp(s, op)
		// With the privilege catalog, rows read the way ClickHouse prints
		// them, so the preview matches what the server will show.
		if s.catalog == nil {
			s.catalog = catalogOf(s)
		}
		if s.catalog != nil {
			normalizeGrantee(s, s.catalog, *op.Target)
		}
	case OpGrantRole, OpRevokeRole:
		for _, role := range op.Roles {
			found := -1
			for i, g := range s.RoleGrants {
				if g.Grantee() == *op.Target && g.GrantedRole == role {
					found = i
				}
			}
			if op.Kind == OpGrantRole {
				if found >= 0 {
					s.RoleGrants[found].AdminOption = s.RoleGrants[found].AdminOption || op.AdminOption
					continue
				}
				g := RoleGrant{GrantedRole: role, AdminOption: op.AdminOption}
				if op.Target.Type == "user" {
					g.UserName = op.Target.Name
				} else {
					g.RoleName = op.Target.Name
				}
				s.RoleGrants = append(s.RoleGrants, g)
				continue
			}
			if found < 0 {
				continue
			}
			if op.AdminOption {
				s.RoleGrants[found].AdminOption = false
			} else {
				s.RoleGrants = append(s.RoleGrants[:found], s.RoleGrants[found+1:]...)
			}
		}
	case OpSetDefaultRoles:
		if u := s.user(op.Target.Name); u != nil {
			u.DefaultAll = op.DefaultAll
			u.DefaultExcpt = append([]string{}, op.DefaultExcept...)
			u.DefaultRoles = append([]string{}, op.Roles...)
		}
	case OpCreateRowPolicy, OpAlterRowPolicy:
		p := op.RowPolicy
		if cur := s.rowPolicy(p.Name, p.Database, p.Table); cur != nil {
			cur.RowPolicySpec = *p
		} else {
			s.RowPolicies = append(s.RowPolicies, RowPolicy{FullName: policyFullName(*p), Storage: "local_directory", RowPolicySpec: *p})
		}
	case OpDropRowPolicy:
		p := op.RowPolicy
		out := s.RowPolicies[:0]
		for _, r := range s.RowPolicies {
			if !(r.Name == p.Name && r.Database == p.Database && r.Table == p.Table) {
				out = append(out, r)
			}
		}
		s.RowPolicies = out
	case OpCreateQuota, OpAlterQuota:
		if cur := s.quota(op.Quota.Name); cur != nil {
			cur.QuotaSpec = *op.Quota
		} else {
			s.Quotas = append(s.Quotas, Quota{Storage: "local_directory", QuotaSpec: *op.Quota})
		}
	case OpDropQuota:
		out := s.Quotas[:0]
		for _, q := range s.Quotas {
			if q.Name != op.Name {
				out = append(out, q)
			}
		}
		s.Quotas = out
	case OpCreateSettingsProfile, OpAlterSettingsProfile:
		if cur := s.profile(op.SettingsProfile.Name); cur != nil {
			cur.ProfileSpec = *op.SettingsProfile
		} else {
			s.SettingsProfiles = append(s.SettingsProfiles, SettingsProfile{Storage: "local_directory", ProfileSpec: *op.SettingsProfile})
		}
	case OpDropSettingsProfile:
		out := s.SettingsProfiles[:0]
		for _, p := range s.SettingsProfiles {
			if p.Name != op.Name {
				out = append(out, p)
			}
		}
		s.SettingsProfiles = out
	}
}

// applyPrivilegeOp applies a grant or revoke of privileges without
// rewriting the rows in printed form; the levels are the same.
func applyPrivilegeOp(s *Snapshot, op Op) {
	db, tbl := normDB(op.Database), normDB(op.Table)
	for _, a := range op.Access {
		for _, col := range columnsOrBlank(op.Columns) {
			applyGrant(s, *op.Target, GrantRow{Access: a, Database: db, Table: tbl, Column: col}, op.Kind == OpGrantPrivilege, op.GrantOption)
		}
	}
}

// without returns a new list with name removed.
func without(list []string, name string) []string {
	out := make([]string, 0, len(list))
	for _, n := range list {
		if n != name {
			out = append(out, n)
		}
	}
	return out
}

func assigneesWithout(a Assignees, name string) Assignees {
	return Assignees{All: a.All, Names: without(a.Names, name), Except: without(a.Except, name)}
}

// Levels of one privilege on one object, lowest first.
const (
	privNone = iota
	privGranted
	privWithGrantOption
)

// privLevel is the level a system.grants row sets on its object.
func privLevel(g GrantRow) int {
	switch {
	case !g.PartialRevoke && g.GrantOption:
		return privWithGrantOption
	case !g.PartialRevoke, g.GrantOption:
		// A grant row, or a REVOKE GRANT OPTION FOR row that keeps access.
		return privGranted
	}
	return privNone
}

// privSource is one row that may decide a privilege level: own is a row of
// the privilege itself rather than of a group covering it, and narrow ranks
// groups, ALTER over ALL.
type privSource struct {
	depth  int
	own    bool
	narrow int
	level  int
}

// pickLevel returns the level of the deciding source: the one on the most
// specific object, a row of the privilege itself over a group row, the
// narrowest group over a wider one.
func pickLevel(srcs []privSource) int {
	if len(srcs) == 0 {
		return privNone
	}
	best := srcs[0]
	for _, s := range srcs[1:] {
		if better(s, best) {
			best = s
		}
	}
	return best.level
}

// better reports whether a decides a level over b, as pickLevel orders them.
func better(a, b privSource) bool {
	switch {
	case a.depth != b.depth:
		return a.depth > b.depth
	case a.own != b.own:
		return a.own
	case a.narrow != b.narrow:
		return a.narrow > b.narrow
	}
	return a.level > b.level
}

// groupSources returns t's rows of the groups covering access (ALL, ALTER
// for ALTER UPDATE) on sc or above it.
func groupSources(s *Snapshot, tree privTree, t Target, access string, sc scope) []privSource {
	var out []privSource
	for _, g := range s.Grants {
		if g.Grantee() == t && g.Access != access && tree.covers(g.Access, access) && sc.within(scopeOf(g)) {
			out = append(out, privSource{scopeOf(g).depth(), false, tree.narrowness(g.Access), privLevel(g)})
		}
	}
	return out
}

// narrowness ranks a privilege group by how far it sits below ALL; only
// groups covering the same privilege are compared, and those form a chain.
func (t privTree) narrowness(p string) int {
	p = normPriv(p)
	if p == "ALL" {
		return 0
	}
	if len(t.parents) == 0 {
		return len(strings.Fields(p))
	}
	return len(t.ancestors(p))
}

// privilegeAt returns the level of access t holds on sc in s.
func privilegeAt(s *Snapshot, tree privTree, t Target, access string, sc scope) int {
	srcs := groupSources(s, tree, t, access, sc)
	for _, g := range s.Grants {
		if g.Grantee() == t && g.Access == access && sc.within(scopeOf(g)) {
			srcs = append(srcs, privSource{depth: scopeOf(g).depth(), own: true, level: privLevel(g)})
		}
	}
	return pickLevel(srcs)
}

// applyGrant changes access on row's object the way ClickHouse does: the
// change covers every object inside it and every privilege the access
// covers, and the rows are rebuilt as system.grants shows them, one row
// where an object differs from the one above it. A revoke of something not
// held leaves no row; a grant on a database clears partial revokes inside it.
func applyGrant(s *Snapshot, t Target, row GrantRow, grant, grantOptionOnly bool) {
	tree := privTree{parents: s.PrivilegeParents}
	sc := scopeOf(row)
	change := func(level int) int {
		switch {
		case grant && grantOptionOnly:
			return privWithGrantOption
		case grant:
			return max(level, privGranted)
		case grantOptionOnly:
			return min(level, privGranted)
		}
		return privNone
	}
	accesses := []string{row.Access}
	for _, g := range s.Grants {
		if g.Grantee() == t && !contains(accesses, g.Access) && tree.covers(row.Access, g.Access) {
			accesses = append(accesses, g.Access)
		}
	}
	// Groups before their members, so a member reads its group's new rows.
	sortAccesses(tree, accesses)

	// Every level is read before any row changes.
	type levelChange struct {
		access string
		scopes []scope
		levels []int
	}
	var changes []levelChange
	for _, a := range accesses {
		// The change reaches a's own rows and, inside sc, the objects where
		// a group row would otherwise decide a's level.
		scopes := []scope{sc}
		for _, g := range s.Grants {
			own := g.Access == a
			group := !own && tree.covers(g.Access, a) && scopeOf(g).within(sc)
			if g.Grantee() == t && (own || group) && !containsScope(scopes, scopeOf(g)) {
				scopes = append(scopes, scopeOf(g))
			}
		}
		levels := make([]int, len(scopes))
		for i, c := range scopes {
			levels[i] = privilegeAt(s, tree, t, a, c)
			if c.within(sc) {
				levels[i] = change(levels[i])
			}
		}
		changes = append(changes, levelChange{a, scopes, levels})
	}
	for _, ch := range changes {
		kept := make([]GrantRow, 0, len(s.Grants))
		for _, g := range s.Grants {
			if g.Grantee() != t || g.Access != ch.access {
				kept = append(kept, g)
			}
		}
		s.Grants = kept
		order := make([]int, len(ch.scopes))
		for i := range order {
			order[i] = i
		}
		sort.SliceStable(order, func(a, b int) bool { return ch.scopes[order[a]].depth() < ch.scopes[order[b]].depth() })
		for _, i := range order {
			c, level := ch.scopes[i], ch.levels[i]
			srcs := groupSources(s, tree, t, ch.access, c)
			for j, o := range ch.scopes {
				if o != c && c.within(o) {
					srcs = append(srcs, privSource{depth: o.depth(), own: true, level: ch.levels[j]})
				}
			}
			parent := pickLevel(srcs)
			if level == parent {
				continue
			}
			r := GrantRow{Access: ch.access, Database: c.db, Table: c.table, Column: c.column}
			if t.Type == "user" {
				r.UserName = t.Name
			} else {
				r.RoleName = t.Name
			}
			switch {
			case level == privNone:
				r.PartialRevoke = true
			case level == privGranted && parent == privWithGrantOption:
				r.PartialRevoke, r.GrantOption = true, true
			default:
				r.GrantOption = level == privWithGrantOption
			}
			s.Grants = append(s.Grants, r)
		}
	}
}

func containsScope(list []scope, sc scope) bool {
	for _, s := range list {
		if s == sc {
			return true
		}
	}
	return false
}

// sortAccesses orders privileges so a group comes before the privileges it
// covers (ALL, then ALTER, then ALTER UPDATE), by name otherwise.
func sortAccesses(tree privTree, accesses []string) {
	covering := map[string]int{}
	for _, a := range accesses {
		for _, b := range accesses {
			if a != b && tree.covers(b, a) {
				covering[a]++
			}
		}
	}
	sort.SliceStable(accesses, func(i, j int) bool {
		if covering[accesses[i]] != covering[accesses[j]] {
			return covering[accesses[i]] < covering[accesses[j]]
		}
		return accesses[i] < accesses[j]
	})
}

// renderGrantRow renders one system.grants row as a statement.
func renderGrantRow(g GrantRow) string {
	var cols []string
	if g.Column != "" {
		cols = []string{g.Column}
	}
	who := quoteIdent(g.Grantee().Name)
	obj := grantObject(g.Database, g.Table)
	if g.PartialRevoke {
		return "REVOKE " + privilegeList([]string{g.Access}, cols) + " ON " + obj + " FROM " + who
	}
	s := "GRANT " + privilegeList([]string{g.Access}, cols) + " ON " + obj + " TO " + who
	if g.GrantOption {
		s += " WITH GRANT OPTION"
	}
	return s
}

// renderEntity renders one entity canonically, or "" when it does not exist.
func renderEntity(s *Snapshot, kind, name string) string {
	var head string
	switch kind {
	case KindRole:
		if s.role(name) == nil {
			return ""
		}
		head = Render(Op{Kind: OpCreateRole, Name: name, Profile: firstOf(s.RoleProfiles[name])}, "", nil)
	case KindUser:
		u := s.user(name)
		if u == nil {
			return ""
		}
		head = Render(defaultRolesOp(u), "", nil)
	case KindRowPolicy:
		for _, p := range s.RowPolicies {
			if p.FullName == name || policyFullName(p.RowPolicySpec) == name {
				spec := p.RowPolicySpec
				return Render(Op{Kind: OpCreateRowPolicy, RowPolicy: &spec}, "", nil)
			}
		}
		return ""
	case KindQuota:
		q := s.quota(name)
		if q == nil {
			return ""
		}
		spec := q.QuotaSpec
		return Render(Op{Kind: OpCreateQuota, Quota: &spec}, "", nil)
	case KindSettingsProfile:
		p := s.profile(name)
		if p == nil {
			return ""
		}
		spec := p.ProfileSpec
		return Render(Op{Kind: OpCreateSettingsProfile, SettingsProfile: &spec}, "", nil)
	default:
		return ""
	}
	t := Target{Type: kind, Name: name}
	var lines []string
	for _, g := range s.Grants {
		if g.Grantee() == t {
			lines = append(lines, renderGrantRow(g))
		}
	}
	for _, g := range s.RoleGrants {
		if g.Grantee() == t {
			l := "GRANT " + quoteIdent(g.GrantedRole) + " TO " + quoteIdent(name)
			if g.AdminOption {
				l += " WITH ADMIN OPTION"
			}
			lines = append(lines, l)
		}
	}
	sort.Strings(lines)
	return strings.Join(append([]string{head}, lines...), "\n")
}
