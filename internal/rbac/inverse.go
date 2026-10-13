// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import "sort"

// Inverse returns the ops that undo op. snap must be the state right before
// op runs: Compose passes the snapshot with every earlier op of the plan
// applied. The inverse restores what snap holds, not the mirror of op, so a
// grant of something already held undoes to nothing and a revoke restores
// exactly the rows it removed.
func Inverse(op Op, snap *Snapshot) []Op {
	switch op.Kind {
	case OpCreateRole:
		return []Op{{Kind: OpDropRole, Name: op.Name}}
	case OpDropRole:
		return dropRoleInverse(op.Name, snap)
	case OpSetRoleProfile:
		return []Op{{Kind: OpSetRoleProfile, Name: op.Name, Profile: firstOf(snap.RoleProfiles[op.Name])}}
	case OpGrantPrivilege, OpRevokePrivilege:
		return privilegeInverse(op, snap)
	case OpGrantRole:
		t := *op.Target
		var revoke, revokeAdmin []string
		for _, r := range op.Roles {
			cur := snap.roleGrant(t, r)
			switch {
			case cur == nil:
				revoke = append(revoke, r)
			case op.AdminOption && !cur.AdminOption:
				revokeAdmin = append(revokeAdmin, r)
			}
		}
		var out []Op
		if len(revoke) > 0 {
			out = append(out, Op{Kind: OpRevokeRole, Target: &t, Roles: revoke})
		}
		if len(revokeAdmin) > 0 {
			out = append(out, Op{Kind: OpRevokeRole, Target: &t, Roles: revokeAdmin, AdminOption: true})
		}
		return out
	case OpRevokeRole:
		t := *op.Target
		var plain, admin []string
		for _, r := range op.Roles {
			cur := snap.roleGrant(t, r)
			switch {
			case cur == nil:
			case cur.AdminOption:
				admin = append(admin, r)
			case !op.AdminOption:
				plain = append(plain, r)
			}
		}
		var out []Op
		if len(plain) > 0 {
			out = append(out, Op{Kind: OpGrantRole, Target: &t, Roles: plain})
		}
		if len(admin) > 0 {
			out = append(out, Op{Kind: OpGrantRole, Target: &t, Roles: admin, AdminOption: true})
		}
		// Revoking a role also takes it out of the user's default roles.
		if !op.AdminOption && t.Type == "user" && len(out) > 0 {
			if u := snap.user(t.Name); u != nil && mentionsAny(u, op.Roles) {
				out = append(out, defaultRolesOp(u))
			}
		}
		return out
	case OpSetDefaultRoles:
		if u := snap.user(op.Target.Name); u != nil {
			return []Op{defaultRolesOp(u)}
		}
		return nil
	case OpCreateRowPolicy:
		return []Op{{Kind: OpDropRowPolicy, RowPolicy: &RowPolicySpec{Name: op.RowPolicy.Name, Database: op.RowPolicy.Database, Table: op.RowPolicy.Table}}}
	case OpAlterRowPolicy, OpDropRowPolicy:
		p := snap.rowPolicy(op.RowPolicy.Name, op.RowPolicy.Database, op.RowPolicy.Table)
		if p == nil {
			return nil
		}
		spec := p.RowPolicySpec
		kind := OpAlterRowPolicy
		if op.Kind == OpDropRowPolicy {
			kind = OpCreateRowPolicy
		}
		return []Op{{Kind: kind, RowPolicy: &spec}}
	case OpCreateQuota:
		return []Op{{Kind: OpDropQuota, Name: op.Quota.Name}}
	case OpAlterQuota, OpDropQuota:
		name := op.Name
		if op.Quota != nil {
			name = op.Quota.Name
		}
		q := snap.quota(name)
		if q == nil {
			return nil
		}
		spec := q.QuotaSpec
		kind := OpAlterQuota
		if op.Kind == OpDropQuota {
			kind = OpCreateQuota
		}
		return []Op{{Kind: kind, Quota: &spec}}
	case OpCreateSettingsProfile:
		return []Op{{Kind: OpDropSettingsProfile, Name: op.SettingsProfile.Name}}
	case OpAlterSettingsProfile, OpDropSettingsProfile:
		name := op.Name
		if op.SettingsProfile != nil {
			name = op.SettingsProfile.Name
		}
		p := snap.profile(name)
		if p == nil {
			return nil
		}
		spec := p.ProfileSpec
		kind := OpAlterSettingsProfile
		if op.Kind == OpDropSettingsProfile {
			kind = OpCreateSettingsProfile
		}
		return []Op{{Kind: kind, SettingsProfile: &spec}}
	}
	return nil
}

func firstOf(list []string) string {
	if len(list) == 0 {
		return ""
	}
	return list[0]
}

func (s *Snapshot) roleGrant(t Target, role string) *RoleGrant {
	for i := range s.RoleGrants {
		if s.RoleGrants[i].Grantee() == t && s.RoleGrants[i].GrantedRole == role {
			return &s.RoleGrants[i]
		}
	}
	return nil
}

// defaultRolesOp sets u's default roles back to what they are in u.
func defaultRolesOp(u *UserEntity) Op {
	t := Target{Type: "user", Name: u.Name}
	op := Op{Kind: OpSetDefaultRoles, Target: &t, DefaultAll: u.DefaultAll}
	if u.DefaultAll {
		op.DefaultExcept = append([]string{}, u.DefaultExcpt...)
	} else {
		op.Roles = append([]string{}, u.DefaultRoles...)
	}
	return op
}

// mentionsAny reports whether u's default role setting names one of roles.
func mentionsAny(u *UserEntity, roles []string) bool {
	for _, r := range roles {
		if contains(u.DefaultRoles, r) || contains(u.DefaultExcpt, r) {
			return true
		}
	}
	return false
}

func assigneesMention(a Assignees, name string) bool {
	return contains(a.Names, name) || contains(a.Except, name)
}

// dropRoleInverse recreates a dropped role with everything ClickHouse removes
// along with it: its privileges and partial revokes, the roles granted to
// it, its members, the default roles of users that named it, and its place
// in row policy, quota and settings profile TO lists.
func dropRoleInverse(name string, snap *Snapshot) []Op {
	if snap.role(name) == nil {
		// DROP ROLE IF EXISTS of a missing role changes nothing.
		return nil
	}
	inv := []Op{{Kind: OpCreateRole, Name: name, Profile: firstOf(snap.RoleProfiles[name])}}
	self := Target{Type: "role", Name: name}
	inv = append(inv, grantsAsOps(snap, self)...)
	for _, g := range snap.RoleGrants {
		if g.Grantee() == self {
			inv = append(inv, Op{Kind: OpGrantRole, Target: &Target{Type: "role", Name: name}, Roles: []string{g.GrantedRole}, AdminOption: g.AdminOption})
		}
	}
	for _, g := range snap.RoleGrants {
		if g.GrantedRole == name {
			t := g.Grantee()
			inv = append(inv, Op{Kind: OpGrantRole, Target: &t, Roles: []string{name}, AdminOption: g.AdminOption})
		}
	}
	for i := range snap.Users {
		if mentionsAny(&snap.Users[i], []string{name}) {
			inv = append(inv, defaultRolesOp(&snap.Users[i]))
		}
	}
	for _, p := range snap.RowPolicies {
		if assigneesMention(p.To, name) {
			spec := p.RowPolicySpec
			inv = append(inv, Op{Kind: OpAlterRowPolicy, RowPolicy: &spec})
		}
	}
	for _, q := range snap.Quotas {
		if assigneesMention(q.To, name) {
			spec := q.QuotaSpec
			inv = append(inv, Op{Kind: OpAlterQuota, Quota: &spec})
		}
	}
	for _, p := range snap.SettingsProfiles {
		if assigneesMention(p.To, name) {
			spec := p.ProfileSpec
			inv = append(inv, Op{Kind: OpAlterSettingsProfile, SettingsProfile: &spec})
		}
	}
	return inv
}

// grantsAsOps turns the current grants of t into grant_privilege ops, one
// per object, followed by its partial revokes, so a dropped role can be
// recreated with its privileges.
func grantsAsOps(snap *Snapshot, t Target) []Op {
	type key struct {
		db, table, column  string
		grantOption, revok bool
	}
	groups := map[key][]string{}
	var order []key
	for _, g := range snap.Grants {
		if g.Grantee() != t {
			continue
		}
		k := key{g.Database, g.Table, g.Column, g.GrantOption, g.PartialRevoke}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], g.Access)
	}
	// Grants first, partial revokes after, each by scope from broad to
	// narrow, so every revoke lands on a grant that already exists.
	sort.SliceStable(order, func(a, b int) bool {
		if order[a].revok != order[b].revok {
			return !order[a].revok
		}
		return scope{order[a].db, order[a].table, order[a].column}.depth() < scope{order[b].db, order[b].table, order[b].column}.depth()
	})
	var ops []Op
	for _, k := range order {
		ops = append(ops, privOp(t, groups[k], scope{k.db, k.table, k.column}, k.revok, k.grantOption))
	}
	return ops
}

// scope is the object a privilege row applies to; "" means any.
type scope struct{ db, table, column string }

func scopeOf(g GrantRow) scope { return scope{g.Database, g.Table, g.Column} }

func (s scope) depth() int {
	switch {
	case s.db == "":
		return 0
	case s.table == "":
		return 1
	case s.column == "":
		return 2
	}
	return 3
}

// within reports whether s is outer or lies inside it.
func (s scope) within(outer scope) bool {
	if outer.db == "" {
		return true
	}
	if s.db != outer.db {
		return false
	}
	if outer.table == "" {
		return true
	}
	if s.table != outer.table {
		return false
	}
	return outer.column == "" || s.column == outer.column
}

// privOp builds a grant (revoke false) or revoke of access on sc. On a
// revoke, grantOption means REVOKE GRANT OPTION FOR.
func privOp(t Target, access []string, sc scope, revoke, grantOption bool) Op {
	kind := OpGrantPrivilege
	if revoke {
		kind = OpRevokePrivilege
	}
	op := Op{Kind: kind, Target: &t, Access: append([]string{}, access...), Database: sc.db, Table: sc.table, GrantOption: grantOption}
	if sc.column != "" {
		op.Columns = []string{sc.column}
	}
	return op
}

// heldAt reports whether t holds access on sc in s, and with grant option,
// reading the rows that cover sc from the broadest to the most specific.
func heldAt(s *Snapshot, t Target, access string, sc scope) (has, grantOption bool) {
	var rows []GrantRow
	for _, g := range s.Grants {
		if g.Grantee() == t && (g.Access == access || g.Access == "ALL") && sc.within(scopeOf(g)) {
			rows = append(rows, g)
		}
	}
	sort.SliceStable(rows, func(a, b int) bool { return scopeOf(rows[a]).depth() < scopeOf(rows[b]).depth() })
	for _, g := range rows {
		switch {
		case !g.PartialRevoke:
			has, grantOption = true, grantOption || g.GrantOption
		case g.GrantOption:
			grantOption = false
		default:
			has, grantOption = false, false
		}
	}
	return has, grantOption
}

// Which rows restoreRows puts back: none, those carrying grant option, or all.
const (
	restoreNone = iota
	restoreGrantOption
	restoreAll
)

type invEntry struct {
	depth int
	op    Op
}

// restoreRows returns ops that put back t's rows for access strictly inside
// sc as they are in s. grants and partials pick which grants and which
// partial revokes are restored.
func restoreRows(s *Snapshot, t Target, access string, sc scope, grants, partials int) []invEntry {
	var out []invEntry
	for _, g := range s.Grants {
		gs := scopeOf(g)
		if g.Grantee() != t || gs == sc || !gs.within(sc) || (g.Access != access && access != "ALL") {
			continue
		}
		mode := grants
		if g.PartialRevoke {
			mode = partials
		}
		if mode == restoreNone || (mode == restoreGrantOption && !g.GrantOption) {
			continue
		}
		out = append(out, invEntry{gs.depth(), privOp(t, []string{g.Access}, gs, g.PartialRevoke, g.GrantOption)})
	}
	return out
}

// privilegeInverse undoes a grant or revoke of privileges from the rows t
// holds before it. A grant on an object also clears the partial revokes
// inside it, and a revoke on an object removes every narrower grant, so
// besides the object itself the narrower rows that changed are put back.
func privilegeInverse(op Op, s *Snapshot) []Op {
	t := *op.Target
	var entries []invEntry
	add := func(sc scope, access string, revoke, grantOption bool) {
		entries = append(entries, invEntry{sc.depth(), privOp(t, []string{access}, sc, revoke, grantOption)})
	}
	for _, a := range op.Access {
		for _, col := range columnsOrBlank(op.Columns) {
			sc := scope{normDB(op.Database), normDB(op.Table), col}
			has, gopt := heldAt(s, t, a, sc)
			switch {
			case op.Kind == OpGrantPrivilege && !has:
				add(sc, a, true, false)
				entries = append(entries, restoreRows(s, t, a, sc, restoreAll, restoreAll)...)
			case op.Kind == OpGrantPrivilege && op.GrantOption && !gopt:
				add(sc, a, true, true)
				entries = append(entries, restoreRows(s, t, a, sc, restoreGrantOption, restoreAll)...)
			case op.Kind == OpGrantPrivilege:
				entries = append(entries, restoreRows(s, t, a, sc, restoreNone, restoreAll)...)
			case op.GrantOption && gopt:
				add(sc, a, false, true)
				entries = append(entries, restoreRows(s, t, a, sc, restoreGrantOption, restoreAll)...)
			case op.GrantOption:
				entries = append(entries, restoreRows(s, t, a, sc, restoreGrantOption, restoreGrantOption)...)
			default:
				if has {
					add(sc, a, false, gopt)
				}
				entries = append(entries, restoreRows(s, t, a, sc, restoreAll, restoreAll)...)
			}
		}
	}
	return mergeEntries(entries)
}

// mergeEntries orders entries from broad to narrow scope and folds ops that
// differ only in access into one statement. Ops at the same depth touch
// different objects, so their relative order does not matter.
func mergeEntries(entries []invEntry) []Op {
	sort.SliceStable(entries, func(a, b int) bool { return entries[a].depth < entries[b].depth })
	var out []Op
	last := -1
	for _, e := range entries {
		if last >= 0 && sameShape(out[last], e.op) {
			for _, a := range e.op.Access {
				if !contains(out[last].Access, a) {
					out[last].Access = append(out[last].Access, a)
				}
			}
			continue
		}
		out = append(out, e.op)
		last = len(out) - 1
	}
	return out
}

func sameShape(a, b Op) bool {
	return a.Kind == b.Kind && *a.Target == *b.Target && a.Database == b.Database && a.Table == b.Table &&
		a.GrantOption == b.GrantOption && firstOf(a.Columns) == firstOf(b.Columns) && len(a.Columns) == len(b.Columns)
}
