// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// StateVersion is the State format this build writes and reads.
const StateVersion = 1

// State is the access-control state of a connection in a canonical,
// comparable form: what Reconcile can make a server look like. It leaves
// out storage, capabilities, usage and the privilege catalog. Users carry
// only their default roles; they are never created or dropped.
type State struct {
	Version          int              `json:"version" yaml:"version"`
	Roles            []StateRole      `json:"roles,omitempty" yaml:"roles,omitempty"`
	Users            []StateUser      `json:"users,omitempty" yaml:"users,omitempty"`
	Grants           []GranteeGrants  `json:"grants,omitempty" yaml:"grants,omitempty"`
	RoleGrants       []StateRoleGrant `json:"role_grants,omitempty" yaml:"role_grants,omitempty"`
	RowPolicies      []RowPolicySpec  `json:"row_policies,omitempty" yaml:"row_policies,omitempty"`
	Quotas           []QuotaSpec      `json:"quotas,omitempty" yaml:"quotas,omitempty"`
	SettingsProfiles []ProfileSpec    `json:"settings_profiles,omitempty" yaml:"settings_profiles,omitempty"`
}

// StateRole is a role and the settings profiles it inherits, in order.
type StateRole struct {
	Name     string   `json:"name" yaml:"name"`
	Profiles []string `json:"profiles,omitempty" yaml:"profiles,omitempty"`
}

// StateUser is an existing user and its default roles: DefaultAll with
// DefaultExcept, or the DefaultRoles list (empty is DEFAULT ROLE NONE).
type StateUser struct {
	Name          string   `json:"name" yaml:"name"`
	DefaultAll    bool     `json:"default_roles_all,omitempty" yaml:"default_roles_all,omitempty"`
	DefaultRoles  []string `json:"default_roles,omitempty" yaml:"default_roles,omitempty"`
	DefaultExcept []string `json:"default_roles_except,omitempty" yaml:"default_roles_except,omitempty"`
}

// GranteeGrants holds the privilege rows of one role or user.
type GranteeGrants struct {
	Type       string       `json:"type" yaml:"type"` // "role" or "user"
	Name       string       `json:"name" yaml:"name"`
	Privileges []StateGrant `json:"privileges" yaml:"privileges"`
}

// StateGrant is a set of system.grants rows that differ only in access.
// Revoke marks partial revokes; with GrantOption it is REVOKE GRANT OPTION
// FOR, which keeps the access.
type StateGrant struct {
	Access      []string `json:"access" yaml:"access"`
	Database    string   `json:"database,omitempty" yaml:"database,omitempty"`
	Table       string   `json:"table,omitempty" yaml:"table,omitempty"`
	Column      string   `json:"column,omitempty" yaml:"column,omitempty"`
	Revoke      bool     `json:"revoke,omitempty" yaml:"revoke,omitempty"`
	GrantOption bool     `json:"grant_option,omitempty" yaml:"grant_option,omitempty"`
}

// StateRoleGrant is a role granted to a role or user.
type StateRoleGrant struct {
	Type        string `json:"type" yaml:"type"` // grantee: "role" or "user"
	Name        string `json:"name" yaml:"name"`
	Role        string `json:"role" yaml:"role"`
	AdminOption bool   `json:"admin_option,omitempty" yaml:"admin_option,omitempty"`
}

func (g GranteeGrants) target() Target { return Target{Type: g.Type, Name: g.Name} }

func (g StateRoleGrant) target() Target { return Target{Type: g.Type, Name: g.Name} }

// FromSnapshot returns the canonical state of s.
func FromSnapshot(s *Snapshot) *State {
	st := &State{Version: StateVersion}
	for _, r := range s.Roles {
		st.Roles = append(st.Roles, StateRole{Name: r.Name, Profiles: s.RoleProfiles[r.Name]})
	}
	for _, u := range s.Users {
		st.Users = append(st.Users, StateUser{Name: u.Name, DefaultAll: u.DefaultAll, DefaultRoles: u.DefaultRoles, DefaultExcept: u.DefaultExcpt})
	}
	byGrantee := map[Target]int{}
	for _, g := range s.Grants {
		t := g.Grantee()
		i, ok := byGrantee[t]
		if !ok {
			i = len(st.Grants)
			byGrantee[t] = i
			st.Grants = append(st.Grants, GranteeGrants{Type: t.Type, Name: t.Name})
		}
		st.Grants[i].Privileges = append(st.Grants[i].Privileges, StateGrant{
			Access: []string{g.Access}, Database: g.Database, Table: g.Table, Column: g.Column, Revoke: g.PartialRevoke, GrantOption: g.GrantOption,
		})
	}
	for _, g := range s.RoleGrants {
		t := g.Grantee()
		st.RoleGrants = append(st.RoleGrants, StateRoleGrant{Type: t.Type, Name: t.Name, Role: g.GrantedRole, AdminOption: g.AdminOption})
	}
	for _, p := range s.RowPolicies {
		st.RowPolicies = append(st.RowPolicies, p.RowPolicySpec)
	}
	for _, q := range s.Quotas {
		st.Quotas = append(st.Quotas, q.QuotaSpec)
	}
	for _, p := range s.SettingsProfiles {
		st.SettingsProfiles = append(st.SettingsProfiles, p.ProfileSpec)
	}
	return st.CanonicalWith(s)
}

// CanonicalWith is Canonical with every grantee's privileges rewritten from
// what they amount to, using the privilege catalog (system.privileges) s
// carries, so grants that mean the same are written the same however they
// were printed: "ALL ON db.* minus SELECT" as a group with a partial revoke
// or as the list SHOW GRANTS prints. The form is ClickHouse's: per object,
// the privileges that differ from the object above, complete groups named
// by the group. Without a catalog it is Canonical.
func (st *State) CanonicalWith(s *Snapshot) *State {
	out := st.Canonical()
	c := catalogOf(s)
	if c == nil {
		return out
	}
	for i, g := range out.Grants {
		norm := GranteeGrants{Type: g.Type, Name: g.Name}
		for _, r := range normalizeRows(c, g.target(), g.rows()) {
			norm.Privileges = append(norm.Privileges, StateGrant{
				Access: []string{r.Access}, Database: r.Database, Table: r.Table, Column: r.Column, Revoke: r.PartialRevoke, GrantOption: r.GrantOption,
			})
		}
		out.Grants[i] = norm
	}
	out.Grants = canonGrants(out.Grants)
	return out
}

// Canonical returns a sorted, deduplicated copy of st that shares no memory
// with it. Two states that mean the same compare equal once canonical,
// except that grants are only rewritten by CanonicalWith. Lists whose order
// matters to ClickHouse (inherited profiles, profile settings) keep their
// order.
func (st *State) Canonical() *State {
	out := &State{Version: st.Version}
	seenRole := map[string]bool{}
	for _, r := range st.Roles {
		if !seenRole[r.Name] {
			seenRole[r.Name] = true
			out.Roles = append(out.Roles, StateRole{Name: r.Name, Profiles: cloneStrings(r.Profiles)})
		}
	}
	sort.Slice(out.Roles, func(i, j int) bool { return out.Roles[i].Name < out.Roles[j].Name })

	seenUser := map[string]bool{}
	for _, u := range st.Users {
		if !seenUser[u.Name] {
			seenUser[u.Name] = true
			out.Users = append(out.Users, canonUser(u))
		}
	}
	sort.Slice(out.Users, func(i, j int) bool { return out.Users[i].Name < out.Users[j].Name })

	out.Grants = canonGrants(st.Grants)

	seenRG := map[StateRoleGrant]bool{}
	for _, g := range st.RoleGrants {
		// The admin option of a duplicate wins, as a second GRANT would.
		plain := g
		plain.AdminOption = false
		seenRG[plain] = seenRG[plain] || g.AdminOption
	}
	for g, admin := range seenRG {
		g.AdminOption = admin
		out.RoleGrants = append(out.RoleGrants, g)
	}
	sort.Slice(out.RoleGrants, func(i, j int) bool {
		a, b := out.RoleGrants[i], out.RoleGrants[j]
		if a.Type != b.Type {
			return a.Type < b.Type
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Role < b.Role
	})

	seenPolicy := map[string]bool{}
	for _, p := range st.RowPolicies {
		if k := policyFullName(p); !seenPolicy[k] {
			seenPolicy[k] = true
			out.RowPolicies = append(out.RowPolicies, canonPolicy(p))
		}
	}
	sort.Slice(out.RowPolicies, func(i, j int) bool {
		a, b := out.RowPolicies[i], out.RowPolicies[j]
		if a.Database != b.Database {
			return a.Database < b.Database
		}
		if a.Table != b.Table {
			return a.Table < b.Table
		}
		return a.Name < b.Name
	})

	seenQuota := map[string]bool{}
	for _, q := range st.Quotas {
		if !seenQuota[q.Name] {
			seenQuota[q.Name] = true
			out.Quotas = append(out.Quotas, canonQuota(q))
		}
	}
	sort.Slice(out.Quotas, func(i, j int) bool { return out.Quotas[i].Name < out.Quotas[j].Name })

	seenProfile := map[string]bool{}
	for _, p := range st.SettingsProfiles {
		if !seenProfile[p.Name] {
			seenProfile[p.Name] = true
			out.SettingsProfiles = append(out.SettingsProfiles, canonProfile(p))
		}
	}
	sort.Slice(out.SettingsProfiles, func(i, j int) bool { return out.SettingsProfiles[i].Name < out.SettingsProfiles[j].Name })
	return out
}

// Hash is the sha256 of the canonical state's JSON, stable across list and
// map ordering.
func (st *State) Hash() string {
	b, _ := json.Marshal(st.Canonical())
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// rows returns the system.grants rows of a grantee entry.
func (g GranteeGrants) rows() []GrantRow {
	var out []GrantRow
	for _, p := range g.Privileges {
		for _, a := range p.Access {
			r := GrantRow{Access: a, Database: normDB(p.Database), Table: normDB(p.Table), Column: p.Column, PartialRevoke: p.Revoke, GrantOption: p.GrantOption}
			if g.Type == "user" {
				r.UserName = g.Name
			} else {
				r.RoleName = g.Name
			}
			out = append(out, r)
		}
	}
	return out
}

func canonGrants(in []GranteeGrants) []GranteeGrants {
	type key struct {
		sc          scope
		revoke, opt bool
	}
	byGrantee := map[Target]map[key]map[string]bool{}
	for _, g := range in {
		t := g.target()
		if byGrantee[t] == nil {
			byGrantee[t] = map[key]map[string]bool{}
		}
		for _, r := range g.rows() {
			k := key{scopeOf(r), r.PartialRevoke, r.GrantOption}
			if byGrantee[t][k] == nil {
				byGrantee[t][k] = map[string]bool{}
			}
			byGrantee[t][k][r.Access] = true
		}
	}
	var out []GranteeGrants
	for t, groups := range byGrantee {
		if len(groups) == 0 {
			continue
		}
		gg := GranteeGrants{Type: t.Type, Name: t.Name, Privileges: []StateGrant{}}
		for k, access := range groups {
			gg.Privileges = append(gg.Privileges, StateGrant{
				Access: sortedNames(access), Database: k.sc.db, Table: k.sc.table, Column: k.sc.column, Revoke: k.revoke, GrantOption: k.opt,
			})
		}
		sort.Slice(gg.Privileges, func(i, j int) bool {
			a, b := gg.Privileges[i], gg.Privileges[j]
			sa, sb := scope{a.Database, a.Table, a.Column}, scope{b.Database, b.Table, b.Column}
			switch {
			case sa.depth() != sb.depth():
				return sa.depth() < sb.depth()
			case sa != sb:
				return sa.db+"\x00"+sa.table+"\x00"+sa.column < sb.db+"\x00"+sb.table+"\x00"+sb.column
			case a.Revoke != b.Revoke:
				return !a.Revoke
			}
			return !a.GrantOption && b.GrantOption
		})
		out = append(out, gg)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type != out[j].Type {
			return out[i].Type < out[j].Type
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func canonUser(u StateUser) StateUser {
	out := StateUser{Name: u.Name, DefaultAll: u.DefaultAll}
	if u.DefaultAll {
		out.DefaultExcept = sortedUnique(u.DefaultExcept)
	} else {
		out.DefaultRoles = sortedUnique(u.DefaultRoles)
	}
	return out
}

func canonAssignees(a Assignees) Assignees {
	if a.All {
		return Assignees{All: true, Except: sortedUnique(a.Except)}
	}
	return Assignees{Names: sortedUnique(a.Names)}
}

func canonPolicy(p RowPolicySpec) RowPolicySpec {
	p.To = canonAssignees(p.To)
	return p
}

// canonQuota sorts intervals by length, writes each length in its largest
// whole unit and drops zero (unlimited) limits.
func canonQuota(q QuotaSpec) QuotaSpec {
	out := QuotaSpec{Name: q.Name, KeyedBy: q.KeyedBy, To: canonAssignees(q.To)}
	for _, iv := range q.Intervals {
		n, unit := splitDuration(intervalSeconds(iv))
		c := QuotaInterval{Duration: n, Unit: unit, Randomized: iv.Randomized}
		for res, v := range iv.Limits {
			if v > 0 {
				if c.Limits == nil {
					c.Limits = map[string]uint64{}
				}
				c.Limits[res] = v
			}
		}
		out.Intervals = append(out.Intervals, c)
	}
	sort.SliceStable(out.Intervals, func(i, j int) bool {
		return intervalSeconds(out.Intervals[i]) < intervalSeconds(out.Intervals[j])
	})
	return out
}

func canonProfile(p ProfileSpec) ProfileSpec {
	out := ProfileSpec{Name: p.Name, Inherit: cloneStrings(p.Inherit), To: canonAssignees(p.To)}
	for _, s := range p.Settings {
		out.Settings = append(out.Settings, ProfileSetting{
			Name: s.Name, Value: cloneStr(s.Value), Min: cloneStr(s.Min), Max: cloneStr(s.Max), Writability: s.Writability,
		})
	}
	return out
}

func cloneStr(p *string) *string {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

// cloneStrings copies list, keeping its order; empty becomes nil.
func cloneStrings(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	return append([]string{}, list...)
}

// sortedUnique returns list sorted without duplicates; empty becomes nil.
func sortedUnique(list []string) []string {
	if len(list) == 0 {
		return nil
	}
	set := map[string]bool{}
	for _, n := range list {
		set[n] = true
	}
	return sortedNames(set)
}

// privCatalog is system.privileges: the group each privilege belongs to and
// the object level it applies at.
type privCatalog struct {
	tree     privTree
	levels   map[string]string
	children map[string][]string
	roots    []string
	memo     map[string][]string
	sets     map[string]map[string]bool
}

// catalogOf returns the privilege catalog of s, or nil when s has none.
func catalogOf(s *Snapshot) *privCatalog {
	if s == nil || len(s.PrivilegeParents) == 0 {
		return nil
	}
	c := &privCatalog{tree: privTree{parents: s.PrivilegeParents}, levels: s.PrivilegeLevels, children: map[string][]string{}, memo: map[string][]string{}, sets: map[string]map[string]bool{}}
	names := map[string]bool{}
	for child, parent := range s.PrivilegeParents {
		c.children[parent] = append(c.children[parent], child)
		names[child], names[parent] = true, true
	}
	for _, kids := range c.children {
		sort.Strings(kids)
	}
	for _, n := range sortedNames(names) {
		if _, ok := s.PrivilegeParents[n]; !ok && n != "NONE" {
			c.roots = append(c.roots, n)
		}
	}
	return c
}

// leaves returns the privileges p stands for that have no members, sorted.
// A name the catalog does not know stands for itself; a known one with no
// level and no members (SHOW FILESYSTEM CACHES in 25.8) stands for nothing.
func (c *privCatalog) leaves(p string) []string {
	p = normPriv(p)
	if out, ok := c.memo[p]; ok {
		return out
	}
	var out []string
	_, known := c.tree.parents[p]
	_, leveled := c.levels[p]
	if kids := c.children[p]; len(kids) == 0 {
		if !known || leveled || len(c.levels) == 0 {
			out = []string{p}
		}
	} else {
		set := map[string]bool{}
		for _, k := range kids {
			for _, l := range c.leaves(k) {
				set[l] = true
			}
		}
		out = sortedNames(set)
	}
	c.memo[p] = out
	return out
}

// leafSet is leaves as a set.
func (c *privCatalog) leafSet(p string) map[string]bool {
	if set, ok := c.sets[p]; ok {
		return set
	}
	set := map[string]bool{}
	for _, l := range c.leaves(p) {
		set[l] = true
	}
	c.sets[p] = set
	return set
}

// grantLevels answers privilegeAt for one grantee's rows without scanning
// every row of the snapshot or walking the privilege tree per row.
type grantLevels struct {
	rows   []GrantRow
	scopes []scope
	sets   []map[string]bool
	narrow []int
	tree   privTree
}

func newGrantLevels(c *privCatalog, tree privTree, t Target, all []GrantRow) *grantLevels {
	g := &grantLevels{tree: tree}
	for _, r := range all {
		if r.Grantee() != t {
			continue
		}
		g.rows = append(g.rows, r)
		g.scopes = append(g.scopes, scopeOf(r))
		var set map[string]bool
		if c != nil {
			set = c.leafSet(r.Access)
		}
		g.sets = append(g.sets, set)
		g.narrow = append(g.narrow, tree.narrowness(r.Access))
	}
	return g
}

// at is privilegeAt over the indexed rows.
func (g *grantLevels) at(access string, sc scope) int {
	best := privSource{depth: -1}
	for i, r := range g.rows {
		if !sc.within(g.scopes[i]) {
			continue
		}
		own := r.Access == access
		if !own && !(r.Access == "ALL" || g.sets[i][access] || (g.sets[i] == nil && g.tree.covers(r.Access, access))) {
			continue
		}
		src := privSource{depth: g.scopes[i].depth(), own: own, level: privLevel(r)}
		if !own {
			src.narrow = g.narrow[i]
		}
		if best.depth < 0 || better(src, best) {
			best = src
		}
	}
	if best.depth < 0 {
		return privNone
	}
	return best.level
}

// applies reports whether a privilege can be held on objects at depth: a
// TABLE privilege on *.*, db.* and db.table, a GLOBAL one only on *.*.
func (c *privCatalog) applies(leaf string, depth int) bool {
	level, ok := c.levels[leaf]
	if !ok {
		return true
	}
	switch level {
	case "COLUMN":
		return true
	case "TABLE", "VIEW", "DICTIONARY":
		return depth <= 2
	case "DATABASE":
		return depth <= 1
	}
	return depth == 0
}

// names writes a set of privileges with the fewest names, as SHOW GRANTS
// does: a group stands for its privileges when all of them are in the set.
func (c *privCatalog) names(set map[string]bool) []string {
	var out []string
	used := map[string]bool{}
	var visit func(g string)
	visit = func(g string) {
		lv := c.leaves(g)
		complete := len(lv) > 0
		for _, l := range lv {
			if !set[l] {
				complete = false
				break
			}
		}
		if complete {
			out = append(out, g)
			for _, l := range lv {
				used[l] = true
			}
			return
		}
		for _, k := range c.children[g] {
			visit(k)
		}
	}
	for _, r := range c.roots {
		visit(r)
	}
	for _, l := range sortedNames(set) {
		if !used[l] {
			out = append(out, l)
		}
	}
	sort.Strings(out)
	return out
}

// up returns the object directly above sc.
func (sc scope) up() scope {
	switch {
	case sc.column != "":
		return scope{sc.db, sc.table, ""}
	case sc.table != "":
		return scope{sc.db, "", ""}
	}
	return scope{}
}

// normalizeRows rewrites t's privilege rows from the levels they give: on
// every object a row names, each privilege that differs from the object
// above gets a row, and complete groups are named by the group.
func normalizeRows(c *privCatalog, t Target, rows []GrantRow) []GrantRow {
	idx := newGrantLevels(c, c.tree, t, rows)
	var points []scope
	leafSet := map[string]bool{}
	for _, g := range rows {
		if !containsScope(points, scopeOf(g)) {
			points = append(points, scopeOf(g))
		}
		for _, l := range c.leaves(g.Access) {
			leafSet[l] = true
		}
	}
	sort.Slice(points, func(i, j int) bool { return scopeLess(points[i], points[j]) })
	leaves := sortedNames(leafSet)
	type kind struct{ partial, opt bool }
	kinds := []kind{{false, false}, {false, true}, {true, false}, {true, true}}
	var out []GrantRow
	for _, n := range points {
		sets := map[kind]map[string]bool{}
		for _, f := range leaves {
			if !c.applies(f, n.depth()) {
				continue
			}
			level := idx.at(f, n)
			parent := privNone
			if n.depth() > 0 {
				parent = idx.at(f, n.up())
			}
			if level == parent {
				continue
			}
			var k kind
			switch {
			case level == privNone:
				k = kind{true, false}
			case level == privGranted && parent == privWithGrantOption:
				k = kind{true, true}
			default:
				k = kind{false, level == privWithGrantOption}
			}
			if sets[k] == nil {
				sets[k] = map[string]bool{}
			}
			sets[k][f] = true
		}
		for _, k := range kinds {
			if sets[k] == nil {
				continue
			}
			for _, name := range c.names(sets[k]) {
				r := GrantRow{Access: name, Database: n.db, Table: n.table, Column: n.column, PartialRevoke: k.partial, GrantOption: k.opt}
				if t.Type == "user" {
					r.UserName = t.Name
				} else {
					r.RoleName = t.Name
				}
				out = append(out, r)
			}
		}
	}
	return out
}

// normalizeGrantee rewrites t's rows in s with normalizeRows, keeping the
// other rows where they are.
func normalizeGrantee(s *Snapshot, c *privCatalog, t Target) {
	var mine, rest []GrantRow
	for _, g := range s.Grants {
		if g.Grantee() == t {
			mine = append(mine, g)
		} else {
			rest = append(rest, g)
		}
	}
	s.Grants = append(rest, normalizeRows(c, t, mine)...)
}
