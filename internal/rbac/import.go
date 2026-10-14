// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"fmt"
	"sort"
	"strings"
)

// MaxImportOps is the most changes one plan holds; Compose refuses more.
const MaxImportOps = 200

// ImportKinds are the parts of an access file an import can apply, named
// as in Prune plus users (default roles).
var ImportKinds = []string{"roles", "users", "grants", "role_grants", "row_policies", "quotas", "profiles"}

// ── Export ──────────────────────────────────────────────────────────

// ExportState is the state of s an access file carries: everything but the
// entities stored in users.xml, which no import could change. excluded
// names those, as "role xml_role". Users carry only default roles, so no
// password or authentication setting is ever part of it.
func ExportState(s *Snapshot) (st *State, excluded []string) {
	full := FromSnapshot(s)
	readOnly := func(kind, name string) bool { return s.storageOf(kind, name) == "users_xml" }
	st = &State{Version: full.Version}
	for _, r := range full.Roles {
		if readOnly(KindRole, r.Name) {
			excluded = append(excluded, "role "+r.Name)
			continue
		}
		st.Roles = append(st.Roles, r)
	}
	for _, u := range full.Users {
		if readOnly(KindUser, u.Name) {
			excluded = append(excluded, "user "+u.Name)
			continue
		}
		st.Users = append(st.Users, u)
	}
	for _, g := range full.Grants {
		if !readOnly(g.Type, g.Name) {
			st.Grants = append(st.Grants, g)
		}
	}
	for _, g := range full.RoleGrants {
		if !readOnly(g.Type, g.Name) {
			st.RoleGrants = append(st.RoleGrants, g)
		}
	}
	for _, p := range s.RowPolicies {
		if p.Storage == "users_xml" {
			excluded = append(excluded, "row policy "+policyFullName(p.RowPolicySpec))
		}
	}
	for _, p := range full.RowPolicies {
		if cur := s.rowPolicy(p.Name, p.Database, p.Table); cur == nil || cur.Storage != "users_xml" {
			st.RowPolicies = append(st.RowPolicies, p)
		}
	}
	for _, q := range full.Quotas {
		if readOnly(KindQuota, q.Name) {
			excluded = append(excluded, "quota "+q.Name)
			continue
		}
		st.Quotas = append(st.Quotas, q)
	}
	for _, p := range full.SettingsProfiles {
		if readOnly(KindSettingsProfile, p.Name) {
			excluded = append(excluded, "settings profile "+p.Name)
			continue
		}
		st.SettingsProfiles = append(st.SettingsProfiles, p)
	}
	return st, excluded
}

// ExportHeader is the comment at the top of an exported file, one line per
// element, without comment markers.
func ExportHeader(src FileSource, excluded []string) string {
	lines := []string{
		"CH-UI access export (chui_access " + fmt.Sprint(FileVersion) + ").",
		"Import it in Governance > Access control > Import. Import never creates users,",
		"and anything this file does not list is left alone unless prune is turned on.",
		"Passwords and authentication settings are never exported.",
	}
	var from []string
	if src.Connection != "" {
		from = append(from, fmt.Sprintf("connection %q", src.Connection))
	}
	if src.Host != "" {
		from = append(from, fmt.Sprintf("host %q", src.Host))
	}
	if src.ExportedAt != "" {
		from = append(from, "exported "+src.ExportedAt)
	}
	if len(from) > 0 {
		lines = append(lines, "Source: "+strings.Join(from, ", ")+".")
	}
	if len(src.Incomplete) > 0 {
		lines = append(lines, "", "Incomplete: these parts could not be read and are missing:")
		for _, e := range src.Incomplete {
			lines = append(lines, "  "+e)
		}
	}
	if len(excluded) > 0 {
		lines = append(lines, "", "Left out because they live in users.xml, which is read-only:")
		for _, e := range excluded {
			lines = append(lines, "  "+e)
		}
	}
	return strings.Join(lines, "\n")
}

// ExportSQL renders st as the statements that would create it on a server
// that has only s's users and users.xml entities. It is for reading; it is
// not imported.
func ExportSQL(s *Snapshot, st *State, header string) string {
	base := &Snapshot{
		RoleProfiles: map[string][]string{}, RoleSettings: map[string][]ProfileElement{}, UserSettings: map[string][]ProfileElement{},
		PrivilegeParents: s.PrivilegeParents, PrivilegeLevels: s.PrivilegeLevels,
	}
	for _, u := range s.Users {
		base.Users = append(base.Users, UserEntity{Name: u.Name, Storage: u.Storage})
	}
	for _, r := range s.Roles {
		if r.Storage == "users_xml" {
			base.Roles = append(base.Roles, r)
		}
	}
	for _, p := range s.SettingsProfiles {
		if p.Storage == "users_xml" {
			base.SettingsProfiles = append(base.SettingsProfiles, p)
		}
	}
	ops, skipped := Reconcile(base, st, ReconcileOpts{})
	var b strings.Builder
	for _, line := range strings.Split(header, "\n") {
		b.WriteString(strings.TrimRight("-- "+line, " ") + "\n")
	}
	b.WriteString("--\n-- For reading only; import the YAML or JSON export instead.\n\n")
	for _, sk := range skipped {
		fmt.Fprintf(&b, "-- skipped %s %s: %s\n", sk.Kind, sk.Name, sk.Reason)
	}
	for _, op := range ops {
		b.WriteString(Render(op, "", nil) + ";\n")
	}
	return b.String()
}

// ── Import ──────────────────────────────────────────────────────────

// Rename maps names in an access file to names on the target, applied
// before planning.
type Rename struct {
	// Principals renames users and roles wherever they appear: entries,
	// grantees, granted roles, default roles and TO clauses.
	Principals map[string]string `json:"principals,omitempty"`
	// Databases renames the database of grants and row policies.
	Databases map[string]string `json:"databases,omitempty"`
}

// ImportOpts tunes PlanImport.
type ImportOpts struct {
	Prune  Prune
	Rename Rename
	// Kinds limits the import to these ImportKinds; empty is all.
	Kinds []string
	// Databases are the databases on the target. Nil skips the check.
	Databases map[string]bool
}

// ImportPlan is the change set an import makes.
type ImportPlan struct {
	Ops     []Op           `json:"ops"`
	Skipped []Skip         `json:"skipped"`
	Summary map[string]int `json:"summary"`
	OverCap bool           `json:"over_cap"`
	Message string         `json:"message,omitempty"`
}

// PlanImport returns the ops that make cur match the file's state, after
// renames, the database check and the kinds filter. It changes nothing.
func PlanImport(cur *Snapshot, st *State, opts ImportOpts) (*ImportPlan, error) {
	include := map[string]bool{}
	for _, k := range opts.Kinds {
		if !contains(ImportKinds, k) {
			return nil, fmt.Errorf("unknown kind %q (expected one of %s)", k, strings.Join(ImportKinds, ", "))
		}
		include[k] = true
	}
	if len(include) == 0 {
		for _, k := range ImportKinds {
			include[k] = true
		}
	}
	st, err := RenameState(st, opts.Rename)
	if err != nil {
		return nil, err
	}
	var skips []Skip
	st, skips = dropMissingDatabases(st, opts.Databases, skips)
	st, skips = limitKinds(cur, st, include, skips)
	prune := opts.Prune
	prune.Roles = prune.Roles && include["roles"]
	prune.Grants = prune.Grants && include["grants"]
	prune.RoleGrants = prune.RoleGrants && include["role_grants"]
	prune.RowPolicies = prune.RowPolicies && include["row_policies"]
	prune.Quotas = prune.Quotas && include["quotas"]
	prune.Profiles = prune.Profiles && include["profiles"]

	ops, skipped := Reconcile(cur, st, ReconcileOpts{Prune: prune})
	plan := &ImportPlan{Ops: ops, Skipped: append(skips, skipped...), Summary: map[string]int{}}
	if plan.Skipped == nil {
		plan.Skipped = []Skip{}
	}
	for _, k := range ImportKinds {
		plan.Summary[k] = 0
	}
	for _, op := range ops {
		plan.Summary[OpSection(op.Kind)]++
	}
	if len(ops) > MaxImportOps {
		plan.OverCap = true
		var parts []string
		for _, k := range ImportKinds {
			if n := plan.Summary[k]; n > 0 {
				parts = append(parts, fmt.Sprintf("%s %d", k, n))
			}
		}
		plan.Message = fmt.Sprintf("The import needs %d changes; one plan holds at most %d. Import fewer kinds at a time (%s).", len(ops), MaxImportOps, strings.Join(parts, ", "))
	}
	return plan, nil
}

// OpSection is the ImportKinds entry an op kind belongs to.
func OpSection(kind string) string {
	switch kind {
	case OpCreateRole, OpDropRole, OpSetRoleProfile:
		return "roles"
	case OpGrantPrivilege, OpRevokePrivilege:
		return "grants"
	case OpGrantRole, OpRevokeRole:
		return "role_grants"
	case OpSetDefaultRoles:
		return "users"
	case OpCreateRowPolicy, OpAlterRowPolicy, OpDropRowPolicy:
		return "row_policies"
	case OpCreateQuota, OpAlterQuota, OpDropQuota:
		return "quotas"
	}
	return "profiles"
}

// RenameState returns a copy of st with the renames applied. Renaming two
// names to one, or onto a name the file already uses, is an error.
func RenameState(st *State, rn Rename) (*State, error) {
	if err := checkRenames("principal", rn.Principals); err != nil {
		return nil, err
	}
	if err := checkRenames("database", rn.Databases); err != nil {
		return nil, err
	}
	out := st.Canonical()
	if len(rn.Principals) > 0 {
		used := map[string]bool{}
		for _, r := range out.Roles {
			used[r.Name] = true
		}
		for _, u := range out.Users {
			used[u.Name] = true
		}
		if err := checkCollisions("principal", rn.Principals, used); err != nil {
			return nil, err
		}
	}
	if len(rn.Databases) > 0 {
		used := map[string]bool{}
		for _, g := range out.Grants {
			for _, p := range g.Privileges {
				if p.Database != "" {
					used[p.Database] = true
				}
			}
		}
		for _, p := range out.RowPolicies {
			used[p.Database] = true
		}
		if err := checkCollisions("database", rn.Databases, used); err != nil {
			return nil, err
		}
	}
	pr := func(n string) string {
		if to, ok := rn.Principals[n]; ok {
			return to
		}
		return n
	}
	prs := func(list []string) []string {
		for i, n := range list {
			list[i] = pr(n)
		}
		return list
	}
	db := func(n string) string {
		if to, ok := rn.Databases[n]; ok && n != "" {
			return to
		}
		return n
	}
	to := func(a *Assignees) {
		prs(a.Names)
		prs(a.Except)
	}
	for i := range out.Roles {
		out.Roles[i].Name = pr(out.Roles[i].Name)
	}
	for i := range out.Users {
		u := &out.Users[i]
		u.Name = pr(u.Name)
		prs(u.DefaultRoles)
		prs(u.DefaultExcept)
	}
	for i := range out.Grants {
		g := &out.Grants[i]
		g.Name = pr(g.Name)
		for j := range g.Privileges {
			g.Privileges[j].Database = db(g.Privileges[j].Database)
		}
	}
	for i := range out.RoleGrants {
		g := &out.RoleGrants[i]
		g.Name, g.Role = pr(g.Name), pr(g.Role)
	}
	for i := range out.RowPolicies {
		p := &out.RowPolicies[i]
		p.Database = db(p.Database)
		to(&p.To)
	}
	for i := range out.Quotas {
		to(&out.Quotas[i].To)
	}
	for i := range out.SettingsProfiles {
		to(&out.SettingsProfiles[i].To)
	}
	return out.Canonical(), nil
}

func checkRenames(what string, m map[string]string) error {
	for _, from := range sortedMapKeys(m) {
		if err := ValidateName(what, from); err != nil {
			return fmt.Errorf("rename %q: %w", from, err)
		}
		if err := ValidateName(what, m[from]); err != nil {
			return fmt.Errorf("rename %q: %w", from, err)
		}
	}
	return nil
}

// checkCollisions refuses renames that would merge two names of the file.
func checkCollisions(what string, m map[string]string, used map[string]bool) error {
	final := map[string]string{}
	for _, n := range sortedNames(used) {
		to := n
		if r, ok := m[n]; ok {
			to = r
		}
		if prev, ok := final[to]; ok {
			return fmt.Errorf("renaming %s %q and %q both give %q", what, prev, n, to)
		}
		final[to] = n
	}
	return nil
}

func sortedMapKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// dropMissingDatabases removes grants and row policies on databases the
// target does not have. Partial revokes stay: they only narrow access, and
// dropping one could widen it once the database exists.
func dropMissingDatabases(st *State, dbs map[string]bool, skips []Skip) (*State, []Skip) {
	if dbs == nil {
		return st, skips
	}
	out := *st
	out.Grants = nil
	for _, g := range st.Grants {
		kept := GranteeGrants{Type: g.Type, Name: g.Name, Privileges: []StateGrant{}}
		missing := map[string]bool{}
		for _, p := range g.Privileges {
			if p.Database != "" && !p.Revoke && !dbs[p.Database] {
				missing[p.Database] = true
				continue
			}
			kept.Privileges = append(kept.Privileges, p)
		}
		for _, d := range sortedNames(missing) {
			skips = append(skips, Skip{Kind: g.Type, Name: g.Name, Reason: fmt.Sprintf("grants on database %q skipped: it does not exist on the target", d)})
		}
		if len(kept.Privileges) > 0 {
			out.Grants = append(out.Grants, kept)
		}
	}
	out.RowPolicies = nil
	for _, p := range st.RowPolicies {
		if !dbs[p.Database] {
			skips = append(skips, Skip{Kind: KindRowPolicy, Name: policyFullName(p), Reason: fmt.Sprintf("database %q does not exist on the target", p.Database)})
			continue
		}
		out.RowPolicies = append(out.RowPolicies, p)
	}
	return &out, skips
}

// limitKinds keeps only the kinds asked for. Roles and users left out stay
// in the state as they are on the target, so their grants still apply.
func limitKinds(cur *Snapshot, st *State, include map[string]bool, skips []Skip) (*State, []Skip) {
	out := *st
	if !include["roles"] {
		out.Roles = nil
		gone := map[string]bool{}
		for _, r := range st.Roles {
			if cur.role(r.Name) == nil {
				gone[r.Name] = true
				skips = append(skips, Skip{Kind: KindRole, Name: r.Name, Reason: "not on the target, and roles are not part of this import"})
				continue
			}
			out.Roles = append(out.Roles, StateRole{Name: r.Name, Profiles: cloneStrings(cur.RoleProfiles[r.Name])})
		}
		out.Grants = nil
		for _, g := range st.Grants {
			if !(g.Type == "role" && gone[g.Name]) {
				out.Grants = append(out.Grants, g)
			}
		}
		out.RoleGrants = nil
		for _, g := range st.RoleGrants {
			if !(g.Type == "role" && gone[g.Name]) {
				out.RoleGrants = append(out.RoleGrants, g)
			}
		}
	}
	if !include["users"] {
		out.Users = nil
		for _, u := range st.Users {
			if c := cur.user(u.Name); c != nil {
				u = canonUser(StateUser{Name: c.Name, DefaultAll: c.DefaultAll, DefaultRoles: c.DefaultRoles, DefaultExcept: c.DefaultExcpt})
			}
			out.Users = append(out.Users, u)
		}
	}
	if !include["grants"] {
		out.Grants = nil
	}
	if !include["role_grants"] {
		out.RoleGrants = nil
	}
	if !include["row_policies"] {
		out.RowPolicies = nil
	}
	if !include["quotas"] {
		out.Quotas = nil
	}
	if !include["profiles"] {
		out.SettingsProfiles = nil
	}
	return &out, skips
}

// ValidateImportOps checks ops against the live catalog, naming the file
// entry an invalid op came from.
func ValidateImportOps(ops []Op, cat Catalog) error {
	for _, op := range ops {
		if err := validateOp(op, cat); err != nil {
			return fmt.Errorf("%s: %w", opEntry(op), err)
		}
	}
	return nil
}

// opEntry names the file entry an op comes from.
func opEntry(op Op) string {
	switch op.Kind {
	case OpGrantPrivilege, OpRevokePrivilege:
		return fmt.Sprintf("grants of %s %s on %s", op.Target.Type, op.Target.Name, formatOn(normDB(op.Database), normDB(op.Table)))
	case OpGrantRole, OpRevokeRole:
		return fmt.Sprintf("role_grants of %s %s", op.Target.Type, op.Target.Name)
	case OpSetDefaultRoles:
		return "users." + op.Target.Name
	case OpCreateRowPolicy, OpAlterRowPolicy, OpDropRowPolicy:
		return "row policy " + policyFullName(*op.RowPolicy)
	case OpCreateQuota, OpAlterQuota:
		return "quotas." + op.Quota.Name
	case OpCreateSettingsProfile, OpAlterSettingsProfile:
		return "settings_profiles." + op.SettingsProfile.Name
	case OpDropQuota:
		return "quota " + op.Name
	case OpDropSettingsProfile:
		return "settings profile " + op.Name
	}
	return "roles." + op.Name
}
