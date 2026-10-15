// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"fmt"
	"reflect"
	"sort"
)

// Prune says, per kind, whether what the server has and the desired state
// leaves out is removed. Off, it is left alone and the plan only adds and
// changes: no privilege, grant option or admin option is revoked except the
// partial revokes the desired state lists.
type Prune struct {
	Roles       bool `json:"roles,omitempty" yaml:"roles,omitempty"`
	Grants      bool `json:"grants,omitempty" yaml:"grants,omitempty"`
	RoleGrants  bool `json:"role_grants,omitempty" yaml:"role_grants,omitempty"`
	RowPolicies bool `json:"row_policies,omitempty" yaml:"row_policies,omitempty"`
	Quotas      bool `json:"quotas,omitempty" yaml:"quotas,omitempty"`
	Profiles    bool `json:"profiles,omitempty" yaml:"profiles,omitempty"`
}

// PruneAll prunes every kind, so the plan makes the server match exactly.
func PruneAll() Prune {
	return Prune{Roles: true, Grants: true, RoleGrants: true, RowPolicies: true, Quotas: true, Profiles: true}
}

// ReconcileOpts tunes Reconcile.
type ReconcileOpts struct {
	Prune Prune `json:"prune" yaml:"prune"`
	// CreateUsers names desired users the server lacks that the plan
	// creates. Their create_user ops carry no authentication; the caller
	// adds it. Other missing users are skipped.
	CreateUsers map[string]bool `json:"-" yaml:"-"`
	// Revert words the skips for a revert from access history: users the
	// desired state leaves out are listed, never dropped, and missing users
	// cannot be recreated.
	Revert bool `json:"-" yaml:"-"`
}

// Skip is a difference Reconcile cannot or may not change.
type Skip struct {
	Kind   string `json:"kind"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

const readOnlyReason = "defined in users.xml, which is read-only"

// Reconcile returns the ops that make current look like desired, in an
// order Compose accepts: settings profiles, roles, users, role profiles,
// grants, role grants, default roles, row policies, quotas and profile
// changes, then revokes, then drops. Users are created only when named in
// CreateUsers and never dropped, and entities stored in
// users.xml are never changed; those differences, and any the op model
// cannot express, come back as skipped.
func Reconcile(current *Snapshot, desired *State, opts ReconcileOpts) (ops []Op, skipped []Skip) {
	if desired.Version > StateVersion {
		return []Op{}, []Skip{{Kind: "state", Reason: fmt.Sprintf("state version %d is newer than this server reads (%d)", desired.Version, StateVersion)}}
	}
	r := &reconciler{cur: current, want: desired.CanonicalWith(current), prune: opts.Prune, opts: opts, seen: map[Skip]bool{}}
	r.index()
	r.planProfiles()
	r.planRoles()
	r.planGrants()
	r.planRoleGrants()
	r.planDefaultRoles()
	r.planRowPolicies()
	r.planQuotas()

	ops = []Op{}
	for _, phase := range [][]Op{
		r.createProfiles, r.createRoles, r.createUsers, r.roleProfiles, r.grants, r.addRoleGrants, r.defaults,
		r.alterProfiles, r.alterPolicies, r.alterQuotas, r.revokes,
		r.dropPolicies, r.dropQuotas, r.dropRoles, r.dropProfiles,
	} {
		ops = append(ops, phase...)
	}
	if r.skips == nil {
		r.skips = []Skip{}
	}
	return ops, r.skips
}

type reconciler struct {
	cur   *Snapshot
	want  *State
	prune Prune
	opts  ReconcileOpts
	skips []Skip
	seen  map[Skip]bool

	wantRoles    map[string]StateRole
	wantUsers    map[string]StateUser
	wantProfiles map[string]ProfileSpec
	// managed are the grantees whose grants, role grants and default roles
	// the plan sets: desired roles and desired users that exist.
	managed     map[Target]bool
	dropRole    map[string]bool
	dropProfile map[string]bool
	// newUser are the users the plan creates.
	newUser map[string]bool
	// badProfile are desired profiles that cannot be applied.
	badProfile map[string]bool
	// granted maps a managed user to the roles it holds after the plan.
	granted map[string]map[string]bool

	createProfiles, createRoles, createUsers, roleProfiles, grants, addRoleGrants, defaults []Op
	alterProfiles, alterPolicies, alterQuotas, revokes                                      []Op
	dropPolicies, dropQuotas, dropRoles, dropProfiles                                       []Op
}

func (r *reconciler) skip(kind, name, format string, args ...any) {
	s := Skip{Kind: kind, Name: name, Reason: fmt.Sprintf(format, args...)}
	if !r.seen[s] {
		r.seen[s] = true
		r.skips = append(r.skips, s)
	}
}

// finalRole reports whether role exists once the plan has run.
func (r *reconciler) finalRole(name string) bool {
	if _, ok := r.wantRoles[name]; ok {
		return true
	}
	return r.cur.role(name) != nil && !r.dropRole[name]
}

// finalProfile reports whether a settings profile exists once the plan has
// run.
func (r *reconciler) finalProfile(name string) bool {
	if _, ok := r.wantProfiles[name]; ok && !r.badProfile[name] {
		return true
	}
	return r.cur.profile(name) != nil && !r.dropProfile[name]
}

// finalUser reports whether a user exists once the plan has run.
func (r *reconciler) finalUser(name string) bool {
	return r.newUser[name] || r.cur.user(name) != nil
}

// missingAssignee returns the first name of a TO clause that is neither a
// role nor a user once the plan has run, or "".
func (r *reconciler) missingAssignee(a Assignees) string {
	for _, n := range append(append([]string{}, a.Names...), a.Except...) {
		if !r.finalRole(n) && !r.finalUser(n) {
			return n
		}
	}
	return ""
}

func (r *reconciler) index() {
	r.wantRoles, r.wantUsers, r.wantProfiles = map[string]StateRole{}, map[string]StateUser{}, map[string]ProfileSpec{}
	r.managed, r.dropRole, r.dropProfile, r.badProfile = map[Target]bool{}, map[string]bool{}, map[string]bool{}, map[string]bool{}
	r.granted = map[string]map[string]bool{}
	r.newUser = map[string]bool{}
	for _, role := range r.want.Roles {
		r.wantRoles[role.Name] = role
		r.managed[Target{Type: "role", Name: role.Name}] = true
	}
	for _, u := range r.want.Users {
		r.wantUsers[u.Name] = u
		if r.cur.user(u.Name) == nil {
			switch {
			case r.opts.CreateUsers[u.Name]:
				r.newUser[u.Name] = true
				r.createUsers = append(r.createUsers, Op{Kind: OpCreateUser, Name: u.Name})
				r.managed[Target{Type: "user", Name: u.Name}] = true
			case r.opts.Revert:
				r.skip(KindUser, u.Name, "user no longer exists; a revert cannot recreate it because CH-UI never keeps passwords")
			default:
				r.skip(KindUser, u.Name, "user not found; tick it under New users to create it")
			}
			continue
		}
		r.managed[Target{Type: "user", Name: u.Name}] = true
	}
	for _, p := range r.want.SettingsProfiles {
		r.wantProfiles[p.Name] = p
	}
	for _, role := range r.cur.Roles {
		if _, ok := r.wantRoles[role.Name]; ok || !r.prune.Roles {
			continue
		}
		if role.Storage == "users_xml" {
			r.skip(KindRole, role.Name, "not in the desired state but %s", readOnlyReason)
			continue
		}
		r.dropRole[role.Name] = true
	}
	for _, p := range r.cur.SettingsProfiles {
		if _, ok := r.wantProfiles[p.Name]; ok || !r.prune.Profiles {
			continue
		}
		if p.Storage == "users_xml" {
			r.skip(KindSettingsProfile, p.Name, "not in the desired state but %s", readOnlyReason)
			continue
		}
		r.dropProfile[p.Name] = true
	}
	if r.opts.Revert {
		for _, u := range r.cur.Users {
			if _, ok := r.wantUsers[u.Name]; !ok {
				r.skip(KindUser, u.Name, "created since this snapshot; CH-UI does not drop users on revert, drop it yourself if wanted")
			}
		}
	}
	if r.prune.Grants || r.prune.RoleGrants {
		for _, u := range r.cur.Users {
			if _, ok := r.wantUsers[u.Name]; ok || r.opts.Revert {
				continue
			}
			t := Target{Type: "user", Name: u.Name}
			if r.holdsAnything(t) {
				r.skip(KindUser, u.Name, "not in the desired state; users are never dropped here, so its grants were left unchanged")
			}
		}
	}
	unmanaged := func(t Target) {
		switch {
		case r.managed[t]:
		case t.Type == "role":
			r.skip(KindRole, t.Name, "has grants in the desired state but is not one of its roles")
		case t.Type == "user":
			if _, listed := r.wantUsers[t.Name]; !listed {
				r.skip(KindUser, t.Name, "has grants in the desired state but is not one of its users")
			}
		default:
			r.skip(t.Type, t.Name, "grantee type must be role or user")
		}
	}
	for _, g := range r.want.Grants {
		unmanaged(g.target())
	}
	for _, g := range r.want.RoleGrants {
		unmanaged(g.target())
	}
}

func (r *reconciler) holdsAnything(t Target) bool {
	for _, g := range r.cur.Grants {
		if g.Grantee() == t {
			return true
		}
	}
	for _, g := range r.cur.RoleGrants {
		if g.Grantee() == t {
			return true
		}
	}
	return false
}

func (r *reconciler) storage(t Target) string { return r.cur.storageOf(t.Type, t.Name) }

func (r *reconciler) planProfiles() {
	var order []string
	for {
		// A profile is applied only when what it inherits exists and its
		// TO clause names roles or users; dropping one can break another.
		for changed := true; changed; {
			changed = false
			for _, p := range r.want.SettingsProfiles {
				if r.badProfile[p.Name] {
					continue
				}
				reason := ""
				for _, parent := range p.Inherit {
					if !r.finalProfile(parent) {
						reason = fmt.Sprintf("inherits settings profile %q, which does not exist", parent)
						break
					}
				}
				if n := r.missingAssignee(p.To); reason == "" && n != "" {
					reason = fmt.Sprintf("assigned to %q, which is neither a role nor a user", n)
				}
				if reason != "" {
					r.badProfile[p.Name] = true
					r.skip(KindSettingsProfile, p.Name, "%s", reason)
					changed = true
				}
			}
		}
		// New profiles are created after the new profiles they inherit.
		order = nil
		done := map[string]bool{}
		pending := []ProfileSpec{}
		for _, p := range r.want.SettingsProfiles {
			if r.cur.profile(p.Name) == nil && !r.badProfile[p.Name] {
				pending = append(pending, p)
			}
		}
		for progress := true; progress && len(pending) > 0; {
			progress = false
			rest := pending[:0]
			for _, p := range pending {
				ready := true
				for _, parent := range p.Inherit {
					if r.cur.profile(parent) == nil && !done[parent] {
						ready = false
					}
				}
				if ready {
					done[p.Name], progress = true, true
					order = append(order, p.Name)
				} else {
					rest = append(rest, p)
				}
			}
			pending = rest
		}
		if len(pending) == 0 {
			break
		}
		for _, p := range pending {
			r.badProfile[p.Name] = true
			r.skip(KindSettingsProfile, p.Name, "inherits itself through other profiles")
		}
	}

	for _, name := range order {
		spec := canonProfile(r.wantProfiles[name])
		// The TO clause may name roles the plan creates later; those are
		// assigned by an ALTER once the roles exist.
		existsNow := true
		for _, n := range append(append([]string{}, spec.To.Names...), spec.To.Except...) {
			if r.cur.role(n) == nil && r.cur.user(n) == nil {
				existsNow = false
			}
		}
		if existsNow {
			r.createProfiles = append(r.createProfiles, Op{Kind: OpCreateSettingsProfile, SettingsProfile: &spec})
			continue
		}
		bare := canonProfile(spec)
		bare.To = Assignees{}
		full := canonProfile(spec)
		r.createProfiles = append(r.createProfiles, Op{Kind: OpCreateSettingsProfile, SettingsProfile: &bare})
		r.alterProfiles = append(r.alterProfiles, Op{Kind: OpAlterSettingsProfile, SettingsProfile: &full})
	}
	for _, p := range r.want.SettingsProfiles {
		cur := r.cur.profile(p.Name)
		if cur == nil || r.badProfile[p.Name] || reflect.DeepEqual(canonProfile(cur.ProfileSpec), p) {
			continue
		}
		if cur.Storage == "users_xml" {
			r.skip(KindSettingsProfile, p.Name, "%s", readOnlyReason)
			continue
		}
		spec := canonProfile(p)
		r.alterProfiles = append(r.alterProfiles, Op{Kind: OpAlterSettingsProfile, SettingsProfile: &spec})
	}
	// Drop a profile before the dropped profiles it inherits.
	pending := sortedNames(r.dropProfile)
	for len(pending) > 0 {
		var next []string
		for _, name := range pending {
			inherited := false
			for _, other := range pending {
				if other != name && contains(r.cur.profile(other).Inherit, name) {
					inherited = true
				}
			}
			if inherited {
				next = append(next, name)
			} else {
				r.dropProfiles = append(r.dropProfiles, Op{Kind: OpDropSettingsProfile, Name: name})
			}
		}
		if len(next) == len(pending) {
			// A cycle among dropped profiles: any order works.
			for _, name := range next {
				r.dropProfiles = append(r.dropProfiles, Op{Kind: OpDropSettingsProfile, Name: name})
			}
			break
		}
		pending = next
	}
}

// directSettings reports whether a role has settings set on it directly,
// which ALTER ROLE ... SETTINGS PROFILE would remove.
func (r *reconciler) directSettings(role string) bool {
	for _, el := range r.cur.RoleSettings[role] {
		if el.Inherit == "" && el.Name != "" {
			return true
		}
	}
	return false
}

func (r *reconciler) planRoles() {
	for _, role := range r.want.Roles {
		reason := ""
		switch {
		case len(role.Profiles) > 1:
			reason = fmt.Sprintf("inherits %d settings profiles; only one can be set", len(role.Profiles))
		case len(role.Profiles) == 1 && !r.finalProfile(role.Profiles[0]):
			reason = fmt.Sprintf("settings profile %q does not exist", role.Profiles[0])
		}
		cur := r.cur.role(role.Name)
		if cur == nil {
			op := Op{Kind: OpCreateRole, Name: role.Name}
			if reason == "" {
				op.Profile = firstOf(role.Profiles)
			} else {
				r.skip(KindRole, role.Name, "created without its settings profile: %s", reason)
			}
			r.createRoles = append(r.createRoles, op)
			continue
		}
		if equalStrings(r.cur.RoleProfiles[role.Name], role.Profiles) {
			continue
		}
		switch {
		case cur.Storage == "users_xml":
			r.skip(KindRole, role.Name, "%s", readOnlyReason)
		case reason != "":
			r.skip(KindRole, role.Name, "%s", reason)
		case r.directSettings(role.Name):
			r.skip(KindRole, role.Name, "has settings set on the role itself, which changing its settings profile would remove")
		default:
			r.roleProfiles = append(r.roleProfiles, Op{Kind: OpSetRoleProfile, Name: role.Name, Profile: firstOf(role.Profiles)})
		}
	}
	for _, name := range sortedNames(r.dropRole) {
		r.dropRoles = append(r.dropRoles, Op{Kind: OpDropRole, Name: name})
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// managedTargets returns the managed grantees, roles first, by name.
func (r *reconciler) managedTargets() []Target {
	out := make([]Target, 0, len(r.managed))
	for t := range r.managed {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *reconciler) planGrants() {
	want := map[Target][]GrantRow{}
	for _, g := range r.want.Grants {
		want[g.target()] = append(want[g.target()], g.rows()...)
	}
	scratch := cloneSnapshot(r.cur)
	for _, t := range r.managedTargets() {
		before := scratch.Grants
		scratch.Grants = append([]GrantRow{}, before...)
		early, late := privilegeOps(scratch, t, want[t], r.prune.Grants)
		if len(early)+len(late) == 0 {
			continue
		}
		if r.storage(t) == "users_xml" {
			r.skip(t.Type, t.Name, "privileges differ but the %s is %s", t.Type, readOnlyReason)
			scratch.Grants = before
			continue
		}
		r.grants = append(r.grants, early...)
		r.revokes = append(r.revokes, late...)
	}
}

// privilegeOps returns the grants and revokes that give t the privilege
// rows want, applying them to scratch. Levels are set object by object from
// the broadest to the narrowest, and on one object a group (ALL, ALTER)
// before the privileges it covers, so each op lands on the state the
// previous ones left: a revoke on *.* also takes what a group grants inside
// it. Ops that lower a level, and the ops that do not commute with an
// earlier one of those, come back in late so the plan adds before it
// removes. Without prune, rows want does not list are left alone.
func privilegeOps(scratch *Snapshot, t Target, want []GrantRow, prune bool) (early, late []Op) {
	tree := privTree{parents: scratch.PrivilegeParents}
	// With the catalog, levels are compared privilege by privilege (the
	// leaves of every group), on the objects each applies to, so a group
	// printed as a list and the group itself compare equal.
	if scratch.catalog == nil {
		scratch.catalog = catalogOf(scratch)
	}
	c := scratch.catalog
	var rows []GrantRow
	for _, g := range scratch.Grants {
		if g.Grantee() == t {
			rows = append(rows, g)
		}
	}
	rows = append(rows, want...)
	var accesses []string
	for _, g := range rows {
		names := []string{g.Access}
		if c != nil {
			names = c.leaves(g.Access)
		}
		for _, a := range names {
			if !contains(accesses, a) {
				accesses = append(accesses, a)
			}
		}
	}
	if c != nil {
		// Leaves never cover each other.
		sort.Strings(accesses)
	} else {
		sortAccesses(tree, accesses)
	}
	rank := map[string]int{}
	for i, a := range accesses {
		rank[a] = i
	}
	// Where a level can change: the access's own rows and the rows of the
	// groups covering it, on either side.
	type point struct {
		access string
		sc     scope
	}
	var points []point
	seen := map[point]bool{}
	for _, a := range accesses {
		for _, g := range rows {
			if tree.covers(g.Access, a) && (c == nil || c.applies(a, scopeOf(g).depth())) {
				p := point{a, scopeOf(g)}
				if !seen[p] {
					seen[p] = true
					points = append(points, p)
				}
			}
		}
	}
	sort.Slice(points, func(i, j int) bool {
		if points[i].sc != points[j].sc {
			return scopeLess(points[i].sc, points[j].sc)
		}
		return rank[points[i].access] < rank[points[j].access]
	})
	desired := newGrantLevels(c, tree, t, want)
	held := newGrantLevels(c, tree, t, scratch.Grants)
	listed := map[point]GrantRow{}
	for _, g := range want {
		listed[point{g.Access, scopeOf(g)}] = g
	}
	for _, p := range points {
		goal := desired.at(p.access, p.sc)
		have := held.at(p.access, p.sc)
		if !prune {
			// Without pruning only listed rows count, and a listed grant
			// never lowers what is held.
			switch row, ok := listed[p]; {
			case !ok:
				goal = have
			case !row.PartialRevoke:
				goal = max(goal, have)
			}
		}
		if goal == have {
			continue
		}
		op := levelOp(t, p.access, p.sc, have, goal)
		applyPrivilegeOp(scratch, op)
		held = newGrantLevels(c, tree, t, scratch.Grants)
		lower := goal < have
		for _, l := range late {
			if !lower && privOpsOverlap(tree, l, op) {
				lower = true
			}
		}
		if lower {
			late = append(late, op)
		} else {
			early = append(early, op)
		}
	}
	early, late = mergePrivOps(tree, early), mergePrivOps(tree, late)
	if c != nil {
		normalizeGrantee(scratch, c, t)
		for _, ops := range [][]Op{early, late} {
			for i := range ops {
				set := map[string]bool{}
				for _, a := range ops[i].Access {
					set[a] = true
				}
				ops[i].Access = c.names(set)
			}
		}
	}
	return early, late
}

// privOpsOverlap reports whether two privilege ops may not swap places:
// related privileges on nested objects.
func privOpsOverlap(tree privTree, a, b Op) bool {
	sa := scope{normDB(a.Database), normDB(a.Table), firstOf(a.Columns)}
	sb := scope{normDB(b.Database), normDB(b.Table), firstOf(b.Columns)}
	if !sa.within(sb) && !sb.within(sa) {
		return false
	}
	return privilegesRelated(tree, a.Access, b.Access)
}

func privilegesRelated(tree privTree, a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if tree.covers(x, y) || tree.covers(y, x) {
				return true
			}
		}
	}
	return false
}

// scopeLess orders objects from broad to narrow, then by name.
func scopeLess(a, b scope) bool {
	if a.depth() != b.depth() {
		return a.depth() < b.depth()
	}
	if a.db != b.db {
		return a.db < b.db
	}
	if a.table != b.table {
		return a.table < b.table
	}
	return a.column < b.column
}

// levelOp moves access on sc from level have to level goal.
func levelOp(t Target, access string, sc scope, have, goal int) Op {
	switch {
	case goal == privNone:
		return privOp(t, []string{access}, sc, true, false)
	case goal == privGranted && have == privWithGrantOption:
		return privOp(t, []string{access}, sc, true, true)
	}
	return privOp(t, []string{access}, sc, false, goal == privWithGrantOption)
}

// mergePrivOps folds an op into an earlier one of the same shape when it
// can swap places with every op in between.
func mergePrivOps(tree privTree, ops []Op) []Op {
	var out []Op
	for _, op := range ops {
		merged := false
		for i := len(out) - 1; i >= 0; i-- {
			if sameShape(out[i], op) {
				out[i].Access = append(out[i].Access, op.Access...)
				merged = true
				break
			}
			if privOpsOverlap(tree, out[i], op) {
				break
			}
		}
		if !merged {
			out = append(out, op)
		}
	}
	return out
}

func (r *reconciler) planRoleGrants() {
	want := map[Target]map[string]bool{}
	for _, g := range r.want.RoleGrants {
		t := g.target()
		if !r.managed[t] {
			continue
		}
		if !r.finalRole(g.Role) {
			r.skip(t.Type, t.Name, "is granted role %q, which does not exist", g.Role)
			continue
		}
		if want[t] == nil {
			want[t] = map[string]bool{}
		}
		want[t][g.Role] = g.AdminOption
	}
	for _, t := range r.managedTargets() {
		have := map[string]bool{}
		for _, g := range r.cur.RoleGrants {
			if g.Grantee() == t {
				have[g.GrantedRole] = g.AdminOption
			}
		}
		final := map[string]bool{}
		var grant, grantAdmin, revoke, revokeAdmin []string
		for _, role := range sortedKeys(want[t]) {
			admin := want[t][role]
			final[role] = true
			cur, held := have[role]
			switch {
			case !held && admin, held && admin && !cur:
				grantAdmin = append(grantAdmin, role)
			case !held:
				grant = append(grant, role)
			case !admin && cur && r.prune.RoleGrants:
				revokeAdmin = append(revokeAdmin, role)
			}
		}
		for _, role := range sortedKeys(have) {
			if _, ok := want[t][role]; ok || r.dropRole[role] {
				continue
			}
			if r.prune.RoleGrants {
				revoke = append(revoke, role)
			} else {
				final[role] = true
			}
		}
		if t.Type == "user" {
			r.granted[t.Name] = final
		}
		if len(grant)+len(grantAdmin)+len(revoke)+len(revokeAdmin) == 0 {
			continue
		}
		if r.storage(t) == "users_xml" {
			r.skip(t.Type, t.Name, "role grants differ but the %s is %s", t.Type, readOnlyReason)
			if t.Type == "user" {
				r.granted[t.Name] = have
			}
			continue
		}
		target := t
		if len(grant) > 0 {
			r.addRoleGrants = append(r.addRoleGrants, Op{Kind: OpGrantRole, Target: &target, Roles: grant})
		}
		if len(grantAdmin) > 0 {
			r.addRoleGrants = append(r.addRoleGrants, Op{Kind: OpGrantRole, Target: &target, Roles: grantAdmin, AdminOption: true})
		}
		if len(revokeAdmin) > 0 {
			r.revokes = append(r.revokes, Op{Kind: OpRevokeRole, Target: &target, Roles: revokeAdmin, AdminOption: true})
		}
		if len(revoke) > 0 {
			r.revokes = append(r.revokes, Op{Kind: OpRevokeRole, Target: &target, Roles: revoke})
		}
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (r *reconciler) planDefaultRoles() {
	for _, u := range r.want.Users {
		t := Target{Type: "user", Name: u.Name}
		if !r.managed[t] {
			continue
		}
		cur := r.cur.user(u.Name)
		if cur == nil {
			// Created by this plan, with DEFAULT ROLE ALL.
			cur = &UserEntity{Name: u.Name, DefaultAll: true}
		}
		have := canonUser(StateUser{Name: cur.Name, DefaultAll: cur.DefaultAll, DefaultRoles: cur.DefaultRoles, DefaultExcept: cur.DefaultExcpt})
		if reflect.DeepEqual(have, u) {
			continue
		}
		if cur.Storage == "users_xml" {
			r.skip(KindUser, u.Name, "default roles differ but the user is %s", readOnlyReason)
			continue
		}
		reason := ""
		for _, role := range append(append([]string{}, u.DefaultRoles...), u.DefaultExcept...) {
			if !r.finalRole(role) {
				reason = fmt.Sprintf("default role %q does not exist", role)
				break
			}
		}
		for _, role := range u.DefaultRoles {
			if reason == "" && !r.granted[u.Name][role] {
				reason = fmt.Sprintf("default role %q is not granted to the user", role)
			}
		}
		if reason != "" {
			r.skip(KindUser, u.Name, "default roles left unchanged: %s", reason)
			continue
		}
		r.defaults = append(r.defaults, Op{
			Kind: OpSetDefaultRoles, Target: &t, DefaultAll: u.DefaultAll,
			Roles: cloneStrings(u.DefaultRoles), DefaultExcept: cloneStrings(u.DefaultExcept),
		})
	}
}

func (r *reconciler) planRowPolicies() {
	wanted := map[string]bool{}
	for _, p := range r.want.RowPolicies {
		name := policyFullName(p)
		wanted[name] = true
		if n := r.missingAssignee(p.To); n != "" {
			r.skip(KindRowPolicy, name, "assigned to %q, which is neither a role nor a user", n)
			continue
		}
		spec := canonPolicy(p)
		cur := r.cur.rowPolicy(p.Name, p.Database, p.Table)
		switch {
		case cur == nil:
			r.alterPolicies = append(r.alterPolicies, Op{Kind: OpCreateRowPolicy, RowPolicy: &spec})
		case reflect.DeepEqual(canonPolicy(cur.RowPolicySpec), spec):
		case cur.Storage == "users_xml":
			r.skip(KindRowPolicy, name, "%s", readOnlyReason)
		default:
			r.alterPolicies = append(r.alterPolicies, Op{Kind: OpAlterRowPolicy, RowPolicy: &spec})
		}
	}
	if !r.prune.RowPolicies {
		return
	}
	for _, p := range r.cur.RowPolicies {
		name := policyFullName(p.RowPolicySpec)
		if wanted[name] {
			continue
		}
		if p.Storage == "users_xml" {
			r.skip(KindRowPolicy, name, "not in the desired state but %s", readOnlyReason)
			continue
		}
		r.dropPolicies = append(r.dropPolicies, Op{Kind: OpDropRowPolicy, RowPolicy: &RowPolicySpec{Name: p.Name, Database: p.Database, Table: p.Table}})
	}
}

func (r *reconciler) planQuotas() {
	wanted := map[string]bool{}
	for _, q := range r.want.Quotas {
		wanted[q.Name] = true
		if n := r.missingAssignee(q.To); n != "" {
			r.skip(KindQuota, q.Name, "assigned to %q, which is neither a role nor a user", n)
			continue
		}
		spec := canonQuota(q)
		cur := r.cur.quota(q.Name)
		switch {
		case cur == nil:
			r.alterQuotas = append(r.alterQuotas, Op{Kind: OpCreateQuota, Quota: &spec})
		case reflect.DeepEqual(canonQuota(cur.QuotaSpec), spec):
		case cur.Storage == "users_xml":
			r.skip(KindQuota, q.Name, "%s", readOnlyReason)
		default:
			r.alterQuotas = append(r.alterQuotas, Op{Kind: OpAlterQuota, Quota: &spec})
		}
	}
	if !r.prune.Quotas {
		return
	}
	for _, q := range r.cur.Quotas {
		if wanted[q.Name] {
			continue
		}
		if q.Storage == "users_xml" {
			r.skip(KindQuota, q.Name, "not in the desired state but %s", readOnlyReason)
			continue
		}
		r.dropQuotas = append(r.dropQuotas, Op{Kind: OpDropQuota, Name: q.Name})
	}
}
