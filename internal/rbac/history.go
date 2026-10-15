// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"sort"
	"strings"
)

// Change kinds of a StateChange.
const (
	ChangeGrant        = "grant"
	ChangeRoleGrant    = "role_grant"
	ChangeDefaultRoles = "default_roles"
	ChangeEntity       = "entity"
	ChangeSetting      = "setting"
)

// StateChange is one difference between two states. Grantee names the
// entity it belongs to, of kind EntityKind.
type StateChange struct {
	Kind       string `json:"kind"`
	Change     string `json:"change"` // added, removed, changed
	EntityKind string `json:"entity_kind"`
	Grantee    string `json:"grantee"`
	Detail     string `json:"detail"`
}

// EffectiveChange is something a user can do after and not before (gained),
// or the other way round (lost). Via is the role that carries it, "direct"
// for the user's own grants.
type EffectiveChange struct {
	User   string `json:"user"`
	Change string `json:"change"` // gained, lost
	What   string `json:"what"`
	Via    string `json:"via"`
}

// StateDiff is what changed from one state to another: the change list,
// each touched entity rendered before and after, and per-user effective
// gains and losses through role chains.
type StateDiff struct {
	Changes   []StateChange     `json:"changes"`
	Entities  []Diff            `json:"entities"`
	Effective []EffectiveChange `json:"effective"`
}

// DiffStates compares a (before) with b (after). A nil side is an empty
// state.
func DiffStates(a, b *State) *StateDiff {
	if a == nil {
		a = &State{Version: StateVersion}
	}
	if b == nil {
		b = &State{Version: StateVersion}
	}
	a, b = a.Canonical(), b.Canonical()
	sa, sb := snapshotOfState(a), snapshotOfState(b)
	d := &StateDiff{Changes: stateChanges(a, b, sa, sb), Entities: []Diff{}, Effective: effectiveChanges(sa, sb)}
	seen := map[[2]string]bool{}
	for _, c := range d.Changes {
		k := [2]string{c.EntityKind, c.Grantee}
		if seen[k] {
			continue
		}
		seen[k] = true
		before, after := renderEntity(sa, c.EntityKind, c.Grantee), renderEntity(sb, c.EntityKind, c.Grantee)
		if before != after {
			d.Entities = append(d.Entities, Diff{Kind: c.EntityKind, Entity: c.Grantee, Current: before, Proposed: after})
		}
	}
	return d
}

// Counts returns the number of changes per change (added, removed,
// changed) and kind.
func (d *StateDiff) Counts() map[string]map[string]int {
	out := map[string]map[string]int{}
	for _, c := range d.Changes {
		if out[c.Change] == nil {
			out[c.Change] = map[string]int{}
		}
		out[c.Change][c.Kind]++
	}
	return out
}

// EntityKeys returns the entities the changes touch as "kind:name", sorted.
func (d *StateDiff) EntityKeys() []string {
	set := map[string]bool{}
	for _, c := range d.Changes {
		set[c.EntityKind+":"+c.Grantee] = true
	}
	return sortedNames(set)
}

// Simulate returns a copy of s with ops applied the way the plan simulator
// models ClickHouse. s is not changed.
func Simulate(s *Snapshot, ops []Op) *Snapshot {
	after := cloneSnapshot(s)
	for _, op := range ops {
		applyOp(after, op)
	}
	return after
}

// AppliedOps returns the ops whose statement ran. Compose renders one
// statement per op, in order, so statement Seq is the op index plus one.
func AppliedOps(ops []Op, results []Result) []Op {
	applied := map[int]bool{}
	for _, r := range results {
		if r.Status == StatusApplied {
			applied[r.Seq] = true
		}
	}
	out := []Op{}
	for i, op := range ops {
		if applied[i+1] {
			out = append(out, op)
		}
	}
	return out
}

// UnexplainedChanges returns how post differs from pre with the applied
// ops simulated: changes the apply alone does not account for, made by
// someone else at about the same time. Empty means the apply explains post.
func UnexplainedChanges(pre, post *Snapshot, applied []Op) []StateChange {
	expected, actual := FromSnapshot(Simulate(pre, applied)), FromSnapshot(post)
	if expected.Hash() == actual.Hash() {
		return []StateChange{}
	}
	return DiffStates(expected, actual).Changes
}

// FilterState returns st limited to the named entities: roles, users,
// grantees, row policies (by full name or short name), quotas and settings
// profiles whose name is in only.
func FilterState(st *State, only map[string]bool) *State {
	out := &State{Version: st.Version}
	for _, r := range st.Roles {
		if only[r.Name] {
			out.Roles = append(out.Roles, r)
		}
	}
	for _, u := range st.Users {
		if only[u.Name] {
			out.Users = append(out.Users, u)
		}
	}
	for _, g := range st.Grants {
		if only[g.Name] {
			out.Grants = append(out.Grants, g)
		}
	}
	for _, g := range st.RoleGrants {
		if only[g.Name] {
			out.RoleGrants = append(out.RoleGrants, g)
		}
	}
	for _, p := range st.RowPolicies {
		if only[policyFullName(p)] || only[p.Name] {
			out.RowPolicies = append(out.RowPolicies, p)
		}
	}
	for _, q := range st.Quotas {
		if only[q.Name] {
			out.Quotas = append(out.Quotas, q)
		}
	}
	for _, p := range st.SettingsProfiles {
		if only[p.Name] {
			out.SettingsProfiles = append(out.SettingsProfiles, p)
		}
	}
	return out
}

// MergeState returns base with the named entities taken from over instead:
// everything FilterState(base, only) would keep is replaced by
// FilterState(over, only). Reconciling a server to the result changes only
// those entities.
func MergeState(base, over *State, only map[string]bool) *State {
	keep := func(name string) bool { return !only[name] }
	out := &State{Version: base.Version}
	for _, r := range base.Roles {
		if keep(r.Name) {
			out.Roles = append(out.Roles, r)
		}
	}
	for _, u := range base.Users {
		if keep(u.Name) {
			out.Users = append(out.Users, u)
		}
	}
	for _, g := range base.Grants {
		if keep(g.Name) {
			out.Grants = append(out.Grants, g)
		}
	}
	for _, g := range base.RoleGrants {
		if keep(g.Name) {
			out.RoleGrants = append(out.RoleGrants, g)
		}
	}
	for _, p := range base.RowPolicies {
		if keep(policyFullName(p)) && keep(p.Name) {
			out.RowPolicies = append(out.RowPolicies, p)
		}
	}
	for _, q := range base.Quotas {
		if keep(q.Name) {
			out.Quotas = append(out.Quotas, q)
		}
	}
	for _, p := range base.SettingsProfiles {
		if keep(p.Name) {
			out.SettingsProfiles = append(out.SettingsProfiles, p)
		}
	}
	sel := FilterState(over, only)
	out.Roles = append(out.Roles, sel.Roles...)
	out.Users = append(out.Users, sel.Users...)
	out.Grants = append(out.Grants, sel.Grants...)
	out.RoleGrants = append(out.RoleGrants, sel.RoleGrants...)
	out.RowPolicies = append(out.RowPolicies, sel.RowPolicies...)
	out.Quotas = append(out.Quotas, sel.Quotas...)
	out.SettingsProfiles = append(out.SettingsProfiles, sel.SettingsProfiles...)
	return out.Canonical()
}

// OpEntities returns the entity names an op changes, for picking which
// entities a long revert covers.
func OpEntities(op Op) []string {
	var out []string
	for _, t := range touched(op) {
		out = append(out, t[1])
	}
	return out
}

// snapshotOfState builds the snapshot a state describes, without storage,
// so renderEntity and the role closure work on stored states.
func snapshotOfState(st *State) *Snapshot {
	s := &Snapshot{RoleProfiles: map[string][]string{}}
	for _, r := range st.Roles {
		s.Roles = append(s.Roles, Entity{Name: r.Name})
		if len(r.Profiles) > 0 {
			s.RoleProfiles[r.Name] = cloneStrings(r.Profiles)
		}
	}
	for _, u := range st.Users {
		s.Users = append(s.Users, UserEntity{Name: u.Name, DefaultAll: u.DefaultAll, DefaultRoles: cloneStrings(u.DefaultRoles), DefaultExcpt: cloneStrings(u.DefaultExcept)})
	}
	for _, g := range st.Grants {
		s.Grants = append(s.Grants, g.rows()...)
	}
	for _, g := range st.RoleGrants {
		rg := RoleGrant{GrantedRole: g.Role, AdminOption: g.AdminOption}
		if g.Type == "user" {
			rg.UserName = g.Name
		} else {
			rg.RoleName = g.Name
		}
		s.RoleGrants = append(s.RoleGrants, rg)
	}
	for _, p := range st.RowPolicies {
		s.RowPolicies = append(s.RowPolicies, RowPolicy{FullName: policyFullName(p), RowPolicySpec: p})
	}
	for _, q := range st.Quotas {
		s.Quotas = append(s.Quotas, Quota{QuotaSpec: q})
	}
	for _, p := range st.SettingsProfiles {
		s.SettingsProfiles = append(s.SettingsProfiles, SettingsProfile{ProfileSpec: p})
	}
	return s
}

func stateChanges(a, b *State, sa, sb *Snapshot) []StateChange {
	out := []StateChange{}
	add := func(kind, change, entityKind, name, detail string) {
		out = append(out, StateChange{Kind: kind, Change: change, EntityKind: entityKind, Grantee: name, Detail: detail})
	}

	rolesA, rolesB := map[string]StateRole{}, map[string]StateRole{}
	for _, r := range a.Roles {
		rolesA[r.Name] = r
	}
	for _, r := range b.Roles {
		rolesB[r.Name] = r
	}
	for _, name := range unionKeys(rolesA, rolesB) {
		ra, inA := rolesA[name]
		rb, inB := rolesB[name]
		switch {
		case !inA:
			add(ChangeEntity, "added", KindRole, name, "role created")
		case !inB:
			add(ChangeEntity, "removed", KindRole, name, "role dropped")
		case !equalStrings(ra.Profiles, rb.Profiles):
			add(ChangeSetting, "changed", KindRole, name, "settings profile "+listOrNone(ra.Profiles)+" to "+listOrNone(rb.Profiles))
		}
	}

	usersA, usersB := map[string]StateUser{}, map[string]StateUser{}
	for _, u := range a.Users {
		usersA[u.Name] = u
	}
	for _, u := range b.Users {
		usersB[u.Name] = u
	}
	for _, name := range unionKeys(usersA, usersB) {
		ua, inA := usersA[name]
		ub, inB := usersB[name]
		switch {
		case !inA:
			add(ChangeEntity, "added", KindUser, name, "user appeared")
		case !inB:
			add(ChangeEntity, "removed", KindUser, name, "user disappeared")
		default:
			if da, db := defaultRolesText(ua), defaultRolesText(ub); da != db {
				add(ChangeDefaultRoles, "changed", KindUser, name, "default roles "+da+" to "+db)
			}
		}
	}

	grantTexts := func(s *Snapshot) map[Target]map[string]bool {
		out := map[Target]map[string]bool{}
		for _, g := range s.Grants {
			t := g.Grantee()
			if out[t] == nil {
				out[t] = map[string]bool{}
			}
			out[t][renderGrantRow(g)] = true
		}
		return out
	}
	ga, gb := grantTexts(sa), grantTexts(sb)
	targets := map[Target]bool{}
	for t := range ga {
		targets[t] = true
	}
	for t := range gb {
		targets[t] = true
	}
	for _, t := range sortedTargets(targets) {
		for _, line := range sortedNames(ga[t]) {
			if !gb[t][line] {
				add(ChangeGrant, "removed", t.Type, t.Name, line)
			}
		}
		for _, line := range sortedNames(gb[t]) {
			if !ga[t][line] {
				add(ChangeGrant, "added", t.Type, t.Name, line)
			}
		}
	}

	type rgKey struct {
		t    Target
		role string
	}
	rgA, rgB := map[rgKey]bool{}, map[rgKey]bool{}
	var rgKeys []rgKey
	for _, g := range a.RoleGrants {
		k := rgKey{g.target(), g.Role}
		rgA[k] = g.AdminOption
		rgKeys = append(rgKeys, k)
	}
	for _, g := range b.RoleGrants {
		k := rgKey{g.target(), g.Role}
		rgB[k] = g.AdminOption
		if _, ok := rgA[k]; !ok {
			rgKeys = append(rgKeys, k)
		}
	}
	sort.SliceStable(rgKeys, func(i, j int) bool {
		x, y := rgKeys[i], rgKeys[j]
		if x.t != y.t {
			return x.t.Type+"\x00"+x.t.Name < y.t.Type+"\x00"+y.t.Name
		}
		return x.role < y.role
	})
	for _, k := range rgKeys {
		adminA, inA := rgA[k]
		adminB, inB := rgB[k]
		switch {
		case !inA:
			add(ChangeRoleGrant, "added", k.t.Type, k.t.Name, "role "+k.role+adminSuffix(adminB))
		case !inB:
			add(ChangeRoleGrant, "removed", k.t.Type, k.t.Name, "role "+k.role+adminSuffix(adminA))
		case adminA != adminB:
			detail := "role " + k.role + " admin option removed"
			if adminB {
				detail = "role " + k.role + " admin option added"
			}
			add(ChangeRoleGrant, "changed", k.t.Type, k.t.Name, detail)
		}
	}

	polA, polB := map[string]RowPolicySpec{}, map[string]RowPolicySpec{}
	for _, p := range a.RowPolicies {
		polA[policyFullName(p)] = p
	}
	for _, p := range b.RowPolicies {
		polB[policyFullName(p)] = p
	}
	for _, name := range unionKeys(polA, polB) {
		pa, inA := polA[name]
		pb, inB := polB[name]
		switch {
		case !inA:
			add(ChangeEntity, "added", KindRowPolicy, name, "row policy created")
		case !inB:
			add(ChangeEntity, "removed", KindRowPolicy, name, "row policy dropped")
		default:
			var parts []string
			if pa.Using != pb.Using {
				parts = append(parts, "condition "+pa.Using+" to "+pb.Using)
			}
			if pa.Restrictive != pb.Restrictive {
				parts = append(parts, "mode "+policyMode(pa)+" to "+policyMode(pb))
			}
			if ta, tb := assigneesText(pa.To), assigneesText(pb.To); ta != tb {
				parts = append(parts, "applies to "+ta+" to "+tb)
			}
			if len(parts) > 0 {
				add(ChangeEntity, "changed", KindRowPolicy, name, strings.Join(parts, "; "))
			}
		}
	}

	quoA, quoB := map[string]QuotaSpec{}, map[string]QuotaSpec{}
	for _, q := range a.Quotas {
		quoA[q.Name] = q
	}
	for _, q := range b.Quotas {
		quoB[q.Name] = q
	}
	for _, name := range unionKeys(quoA, quoB) {
		qa, inA := quoA[name]
		qb, inB := quoB[name]
		switch {
		case !inA:
			add(ChangeEntity, "added", KindQuota, name, "quota created")
		case !inB:
			add(ChangeEntity, "removed", KindQuota, name, "quota dropped")
		default:
			var parts []string
			if qa.KeyedBy != qb.KeyedBy {
				parts = append(parts, "keyed by "+orNone(qa.KeyedBy)+" to "+orNone(qb.KeyedBy))
			}
			if quotaIntervals(qa, nil) != quotaIntervals(qb, nil) {
				parts = append(parts, "limits changed")
			}
			if ta, tb := assigneesText(qa.To), assigneesText(qb.To); ta != tb {
				parts = append(parts, "applies to "+ta+" to "+tb)
			}
			if len(parts) > 0 {
				add(ChangeEntity, "changed", KindQuota, name, strings.Join(parts, "; "))
			}
		}
	}

	proA, proB := map[string]ProfileSpec{}, map[string]ProfileSpec{}
	for _, p := range a.SettingsProfiles {
		proA[p.Name] = p
	}
	for _, p := range b.SettingsProfiles {
		proB[p.Name] = p
	}
	for _, name := range unionKeys(proA, proB) {
		pa, inA := proA[name]
		pb, inB := proB[name]
		switch {
		case !inA:
			add(ChangeEntity, "added", KindSettingsProfile, name, "settings profile created")
		case !inB:
			add(ChangeEntity, "removed", KindSettingsProfile, name, "settings profile dropped")
		default:
			var parts []string
			if profileElements(ProfileSpec{Settings: pa.Settings}) != profileElements(ProfileSpec{Settings: pb.Settings}) {
				parts = append(parts, "settings changed")
			}
			if !equalStrings(pa.Inherit, pb.Inherit) {
				parts = append(parts, "inherits "+listOrNone(pa.Inherit)+" to "+listOrNone(pb.Inherit))
			}
			if ta, tb := assigneesText(pa.To), assigneesText(pb.To); ta != tb {
				parts = append(parts, "applies to "+ta+" to "+tb)
			}
			if len(parts) > 0 {
				add(ChangeSetting, "changed", KindSettingsProfile, name, strings.Join(parts, "; "))
			}
		}
	}
	return out
}

// effectiveChanges lists, per user, the roles and privileges reachable
// through SET ROLE ALL that one side has and the other does not.
func effectiveChanges(sa, sb *Snapshot) []EffectiveChange {
	users := map[string]bool{}
	for _, u := range sa.Users {
		users[u.Name] = true
	}
	for _, u := range sb.Users {
		users[u.Name] = true
	}
	out := []EffectiveChange{}
	for _, user := range sortedNames(users) {
		ea, eb := effectiveOf(sa, user), effectiveOf(sb, user)
		for _, what := range sortedKeysOf(eb) {
			if _, ok := ea[what]; !ok {
				out = append(out, effectiveEntry(user, what, eb[what], true))
			}
		}
		for _, what := range sortedKeysOf(ea) {
			if _, ok := eb[what]; !ok {
				out = append(out, effectiveEntry(user, what, ea[what], false))
			}
		}
	}
	return out
}

// revokedPrefix marks a partial revoke in an effective set: gaining one is
// losing access.
const revokedPrefix = "\x00"

func effectiveEntry(user, what, via string, gained bool) EffectiveChange {
	if strings.HasPrefix(what, revokedPrefix) {
		what = strings.TrimPrefix(what, revokedPrefix) + " (partial revoke)"
		gained = !gained
	}
	change := "lost"
	if gained {
		change = "gained"
	}
	return EffectiveChange{User: user, Change: change, What: what, Via: via}
}

// effectiveOf maps each role and privilege user holds after SET ROLE ALL
// to the role that carries it ("direct" for the user's own).
func effectiveOf(s *Snapshot, user string) map[string]string {
	out := map[string]string{}
	if s.user(user) == nil {
		return out
	}
	put := func(what, via string) {
		if _, ok := out[what]; !ok {
			out[what] = via
		}
	}
	addGrants := func(t Target, via string) {
		for _, g := range s.Grants {
			if g.Grantee() == t {
				put(effectiveGrantText(g), via)
			}
		}
	}
	addGrants(Target{Type: "user", Name: user}, "direct")
	_, all := userRoles(s, user)
	for _, r := range all.order {
		path := all.paths[r]
		via := "direct"
		if len(path) > 2 {
			via = path[len(path)-2]
		}
		put("role "+r, via)
		addGrants(Target{Type: "role", Name: r}, r)
	}
	return out
}

func effectiveGrantText(g GrantRow) string {
	var cols []string
	if g.Column != "" {
		cols = []string{g.Column}
	}
	s := privilegeList([]string{g.Access}, cols) + " ON " + grantObject(g.Database, g.Table)
	if g.PartialRevoke {
		if g.GrantOption {
			return revokedPrefix + "grant option for " + s
		}
		return revokedPrefix + s
	}
	if g.GrantOption {
		s += " WITH GRANT OPTION"
	}
	return s
}

func defaultRolesText(u StateUser) string {
	switch {
	case u.DefaultAll && len(u.DefaultExcept) > 0:
		return "ALL EXCEPT " + strings.Join(u.DefaultExcept, ", ")
	case u.DefaultAll:
		return "ALL"
	case len(u.DefaultRoles) == 0:
		return "NONE"
	}
	return strings.Join(u.DefaultRoles, ", ")
}

func policyMode(p RowPolicySpec) string {
	if p.Restrictive {
		return "restrictive"
	}
	return "permissive"
}

func adminSuffix(admin bool) string {
	if admin {
		return " with admin option"
	}
	return ""
}

func listOrNone(list []string) string {
	if len(list) == 0 {
		return "none"
	}
	return strings.Join(list, ", ")
}

func orNone(s string) string {
	if s == "" {
		return "none"
	}
	return s
}

func unionKeys[V any](a, b map[string]V) []string {
	set := map[string]bool{}
	for k := range a {
		set[k] = true
	}
	for k := range b {
		set[k] = true
	}
	return sortedNames(set)
}

func sortedKeysOf(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func sortedTargets(set map[Target]bool) []Target {
	out := make([]Target, 0, len(set))
	for t := range set {
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

// RevertPlan is how to bring a server back to a stored state.
type RevertPlan struct {
	Ops []Op `json:"ops"`
	// Skipped are differences Reconcile cannot or may not change.
	Skipped []Skip `json:"skipped"`
	// Remaining is what would still differ from the target after the ops,
	// simulated on the live snapshot.
	Remaining []StateChange `json:"remaining"`
}

// PlanRevert reconciles live to target with every kind pruned. With only
// set, just those entities are reverted and everything else is left as it
// is live. Users are never dropped or recreated: CH-UI does not know who
// created a user, and it never stores passwords. Both come back as skipped.
func PlanRevert(live *Snapshot, target *State, only map[string]bool) RevertPlan {
	desired := target
	if len(only) > 0 {
		desired = MergeState(FromSnapshot(live), target.CanonicalWith(live), only)
	}
	ops, skipped := Reconcile(live, desired, ReconcileOpts{Prune: PruneAll(), Revert: true})
	after := Simulate(live, ops)
	return RevertPlan{Ops: ops, Skipped: skipped, Remaining: DiffStates(FromSnapshot(after), desired.CanonicalWith(after)).Changes}
}
