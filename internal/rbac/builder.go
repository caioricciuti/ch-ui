// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

func quoteIdent(s string) string { return safe.Identifier(s) }

func quoteLit(s string) string { return safe.QuotedLiteral(s) }

// Statement phases. Creates come first so later statements can reference
// the new entities; revokes and drops come last so nothing is removed while
// it is still referenced by an earlier step.
const (
	phaseCreateProfile = iota
	phaseCreateQuota
	phaseCreateRole
	phaseAlter
	phaseGrantPrivilege
	phaseGrantRole
	phaseRowPolicy
	phaseDefaultRole
	phaseRevoke
	phaseDropPolicy
	phaseDropQuota
	phaseDropRole
	phaseDropProfile
)

func phaseOf(kind string) int {
	switch kind {
	case OpCreateSettingsProfile:
		return phaseCreateProfile
	case OpCreateQuota:
		return phaseCreateQuota
	case OpCreateRole:
		return phaseCreateRole
	case OpSetRoleProfile, OpAlterQuota, OpAlterSettingsProfile:
		return phaseAlter
	case OpGrantPrivilege:
		return phaseGrantPrivilege
	case OpGrantRole:
		return phaseGrantRole
	case OpCreateRowPolicy, OpAlterRowPolicy:
		return phaseRowPolicy
	case OpSetDefaultRoles:
		return phaseDefaultRole
	case OpRevokePrivilege, OpRevokeRole:
		return phaseRevoke
	case OpDropRowPolicy:
		return phaseDropPolicy
	case OpDropQuota:
		return phaseDropQuota
	case OpDropRole:
		return phaseDropRole
	case OpDropSettingsProfile:
		return phaseDropProfile
	}
	return phaseRevoke
}

// grantObject renders the ON clause target: *.*, db.* or db.table.
func grantObject(database, table string) string {
	if database == "" || database == "*" {
		return "*.*"
	}
	if table == "" || table == "*" {
		return quoteIdent(database) + ".*"
	}
	return quoteIdent(database) + "." + quoteIdent(table)
}

func privilegeList(access, columns []string) string {
	cols := ""
	if len(columns) > 0 {
		q := make([]string, len(columns))
		for i, c := range columns {
			q[i] = quoteIdent(c)
		}
		cols = "(" + strings.Join(q, ", ") + ")"
	}
	parts := make([]string, len(access))
	for i, a := range access {
		parts[i] = a + cols
	}
	return strings.Join(parts, ", ")
}

func identList(names []string) string {
	q := make([]string, len(names))
	for i, n := range names {
		q[i] = quoteIdent(n)
	}
	return strings.Join(q, ", ")
}

// toClause renders the TO clause of a policy, quota or profile.
func toClause(a Assignees) string {
	switch {
	case a.All && len(a.Except) > 0:
		return " TO ALL EXCEPT " + identList(a.Except)
	case a.All:
		return " TO ALL"
	case len(a.Names) > 0:
		return " TO " + identList(a.Names)
	default:
		return " TO NONE"
	}
}

func settingValue(v string) string {
	if numericRe.MatchString(v) {
		return v
	}
	return quoteLit(v)
}

func profileElements(p ProfileSpec) string {
	var parts []string
	for _, parent := range p.Inherit {
		parts = append(parts, "INHERIT "+quoteLit(parent))
	}
	for _, s := range p.Settings {
		e := s.Name
		if s.Value != nil {
			e += " = " + settingValue(*s.Value)
		}
		if s.Min != nil {
			e += " MIN " + settingValue(*s.Min)
		}
		if s.Max != nil {
			e += " MAX " + settingValue(*s.Max)
		}
		if s.Writability != "" {
			e += " " + s.Writability
		}
		parts = append(parts, e)
	}
	if len(parts) == 0 {
		return ""
	}
	return " SETTINGS " + strings.Join(parts, ", ")
}

func quotaKeyClause(keyedBy string) string {
	if keyedBy == "" {
		return " NOT KEYED"
	}
	return " KEYED BY " + keyedBy
}

func intervalClause(iv QuotaInterval, clear map[string]bool) string {
	head := "FOR "
	if iv.Randomized {
		head += "RANDOMIZED "
	}
	head += "INTERVAL " + strconv.FormatUint(iv.Duration, 10) + " " + iv.Unit
	var limits []string
	for _, res := range QuotaResources {
		if v, ok := iv.Limits[res]; ok && v > 0 {
			limits = append(limits, "MAX "+res+" = "+strconv.FormatUint(v, 10))
		} else if clear[res] {
			limits = append(limits, "MAX "+res+" = 0")
		}
	}
	if len(limits) == 0 {
		return head + " TRACKING ONLY"
	}
	return head + " " + strings.Join(limits, ", ")
}

func intervalSeconds(iv QuotaInterval) uint64 { return iv.Duration * unitSeconds(iv.Unit) }

// quotaIntervals renders the FOR INTERVAL clauses. For an ALTER, intervals
// that exist now but not in the new spec are removed with NO LIMITS, and
// limits that exist now but not in the new spec are reset to 0 (unlimited),
// because ALTER QUOTA only changes what it names.
func quotaIntervals(q QuotaSpec, current *QuotaSpec) string {
	var parts []string
	old := map[uint64]QuotaInterval{}
	if current != nil {
		for _, iv := range current.Intervals {
			old[intervalSeconds(iv)] = iv
		}
	}
	keep := map[uint64]bool{}
	for _, iv := range q.Intervals {
		secs := intervalSeconds(iv)
		keep[secs] = true
		clear := map[string]bool{}
		if prev, ok := old[secs]; ok {
			for res, v := range prev.Limits {
				if v > 0 {
					clear[res] = true
				}
			}
		}
		parts = append(parts, intervalClause(iv, clear))
	}
	if current != nil {
		for _, iv := range current.Intervals {
			if !keep[intervalSeconds(iv)] {
				n, unit := splitDuration(intervalSeconds(iv))
				parts = append(parts, "FOR INTERVAL "+strconv.FormatUint(n, 10)+" "+unit+" NO LIMITS")
			}
		}
	}
	if len(parts) == 0 {
		return ""
	}
	return " " + strings.Join(parts, ", ")
}

// Render builds the SQL of one op. current is the entity as it is now, used
// by ALTER QUOTA to clear removed limits; it may be nil.
func Render(op Op, cluster string, snap *Snapshot) string {
	on := clusterhealth.OnCluster(cluster)
	switch op.Kind {
	case OpCreateRole:
		s := "CREATE ROLE " + quoteIdent(op.Name) + on
		if op.Profile != "" {
			s += " SETTINGS PROFILE " + quoteLit(op.Profile)
		}
		return s
	case OpDropRole:
		return "DROP ROLE IF EXISTS " + quoteIdent(op.Name) + on
	case OpSetRoleProfile:
		if op.Profile == "" {
			return "ALTER ROLE " + quoteIdent(op.Name) + on + " SETTINGS NONE"
		}
		return "ALTER ROLE " + quoteIdent(op.Name) + on + " SETTINGS PROFILE " + quoteLit(op.Profile)
	case OpGrantPrivilege:
		s := "GRANT" + on + " " + privilegeList(op.Access, op.Columns) + " ON " + grantObject(op.Database, op.Table) + " TO " + quoteIdent(op.Target.Name)
		if op.GrantOption {
			s += " WITH GRANT OPTION"
		}
		return s
	case OpRevokePrivilege:
		s := "REVOKE" + on + " "
		if op.GrantOption {
			s += "GRANT OPTION FOR "
		}
		return s + privilegeList(op.Access, op.Columns) + " ON " + grantObject(op.Database, op.Table) + " FROM " + quoteIdent(op.Target.Name)
	case OpGrantRole:
		s := "GRANT" + on + " " + identList(op.Roles) + " TO " + quoteIdent(op.Target.Name)
		if op.AdminOption {
			s += " WITH ADMIN OPTION"
		}
		return s
	case OpRevokeRole:
		s := "REVOKE" + on + " "
		if op.AdminOption {
			s += "ADMIN OPTION FOR "
		}
		return s + identList(op.Roles) + " FROM " + quoteIdent(op.Target.Name)
	case OpSetDefaultRoles:
		s := "ALTER USER " + quoteIdent(op.Target.Name) + on + " DEFAULT ROLE "
		switch {
		case op.DefaultAll:
			return s + "ALL"
		case len(op.Roles) == 0:
			return s + "NONE"
		default:
			return s + identList(op.Roles)
		}
	case OpCreateRowPolicy, OpAlterRowPolicy:
		p := op.RowPolicy
		verb := "CREATE"
		if op.Kind == OpAlterRowPolicy {
			verb = "ALTER"
		}
		mode := "PERMISSIVE"
		if p.Restrictive {
			mode = "RESTRICTIVE"
		}
		return verb + " ROW POLICY " + quoteIdent(p.Name) + on + " ON " + quoteIdent(p.Database) + "." + quoteIdent(p.Table) +
			" FOR SELECT USING (" + p.Using + ") AS " + mode + toClause(p.To)
	case OpDropRowPolicy:
		p := op.RowPolicy
		return "DROP ROW POLICY IF EXISTS " + quoteIdent(p.Name) + " ON " + quoteIdent(p.Database) + "." + quoteIdent(p.Table) + on
	case OpCreateQuota:
		q := op.Quota
		return "CREATE QUOTA " + quoteIdent(q.Name) + on + quotaKeyClause(q.KeyedBy) + quotaIntervals(*q, nil) + toClause(q.To)
	case OpAlterQuota:
		q := op.Quota
		var cur *QuotaSpec
		if snap != nil {
			if c := snap.quota(q.Name); c != nil {
				cur = &c.QuotaSpec
			}
		}
		return "ALTER QUOTA " + quoteIdent(q.Name) + on + quotaKeyClause(q.KeyedBy) + quotaIntervals(*q, cur) + toClause(q.To)
	case OpDropQuota:
		return "DROP QUOTA IF EXISTS " + quoteIdent(op.Name) + on
	case OpCreateSettingsProfile:
		p := op.SettingsProfile
		return "CREATE SETTINGS PROFILE " + quoteIdent(p.Name) + on + profileElements(*p) + toClause(p.To)
	case OpAlterSettingsProfile:
		p := op.SettingsProfile
		els := profileElements(*p)
		if els == "" {
			els = " SETTINGS NONE"
		}
		return "ALTER SETTINGS PROFILE " + quoteIdent(p.Name) + on + els + toClause(p.To)
	case OpDropSettingsProfile:
		return "DROP SETTINGS PROFILE IF EXISTS " + quoteIdent(op.Name) + on
	}
	return ""
}

// Inverse returns the ops that undo op, given the state before it ran.
func Inverse(op Op, snap *Snapshot) []Op {
	switch op.Kind {
	case OpCreateRole:
		return []Op{{Kind: OpDropRole, Name: op.Name}}
	case OpDropRole:
		inv := []Op{{Kind: OpCreateRole, Name: op.Name, Profile: firstOf(snap.RoleProfiles[op.Name])}}
		inv = append(inv, grantsAsOps(snap, Target{Type: "role", Name: op.Name})...)
		for _, g := range snap.RoleGrants {
			if g.GrantedRole == op.Name {
				t := g.Grantee()
				inv = append(inv, Op{Kind: OpGrantRole, Target: &t, Roles: []string{op.Name}, AdminOption: g.AdminOption})
			}
		}
		return inv
	case OpSetRoleProfile:
		return []Op{{Kind: OpSetRoleProfile, Name: op.Name, Profile: firstOf(snap.RoleProfiles[op.Name])}}
	case OpGrantPrivilege:
		inv := op
		inv.Kind = OpRevokePrivilege
		inv.GrantOption = false
		return []Op{inv}
	case OpRevokePrivilege:
		inv := op
		inv.Kind = OpGrantPrivilege
		if op.GrantOption {
			inv.GrantOption = true
		}
		return []Op{inv}
	case OpGrantRole:
		inv := op
		inv.Kind = OpRevokeRole
		inv.AdminOption = false
		return []Op{inv}
	case OpRevokeRole:
		inv := op
		inv.Kind = OpGrantRole
		if op.AdminOption {
			inv.AdminOption = true
		} else {
			for _, g := range snap.RoleGrants {
				t := g.Grantee()
				if t == *op.Target && contains(op.Roles, g.GrantedRole) && g.AdminOption {
					inv.AdminOption = true
				}
			}
		}
		return []Op{inv}
	case OpSetDefaultRoles:
		u := snap.user(op.Target.Name)
		inv := Op{Kind: OpSetDefaultRoles, Target: op.Target}
		if u != nil {
			inv.DefaultAll = u.DefaultAll && len(u.DefaultExcpt) == 0
			if !u.DefaultAll {
				inv.Roles = append([]string{}, u.DefaultRoles...)
			}
		}
		return []Op{inv}
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

// grantsAsOps turns the current grants of t into grant_privilege ops, one
// per object, so a dropped role can be recreated with its privileges.
func grantsAsOps(snap *Snapshot, t Target) []Op {
	type key struct {
		db, table, column string
		grantOption       bool
	}
	groups := map[key][]string{}
	var order []key
	for _, g := range snap.Grants {
		if g.Grantee() != t || g.PartialRevoke {
			continue
		}
		k := key{g.Database, g.Table, g.Column, g.GrantOption}
		if _, ok := groups[k]; !ok {
			order = append(order, k)
		}
		groups[k] = append(groups[k], g.Access)
	}
	var ops []Op
	for _, k := range order {
		tt := t
		op := Op{Kind: OpGrantPrivilege, Target: &tt, Access: groups[k], Database: k.db, Table: k.table, GrantOption: k.grantOption}
		if k.column != "" {
			op.Columns = []string{k.column}
		}
		ops = append(ops, op)
	}
	return ops
}

// Compose validates ops and builds the plan: ordered statements with their
// inverses, entity diffs, guard warnings and the plan hash. It reads nothing
// from ClickHouse; the snapshot and catalog are passed in.
func Compose(ops []Op, snap *Snapshot, cat Catalog, cluster string, ctx GuardContext) (*Plan, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("no changes to plan")
	}
	if len(ops) > 200 {
		return nil, fmt.Errorf("too many changes in one plan (max 200)")
	}
	if cluster != "" && !clusterhealth.IsValidClusterName(cluster) {
		return nil, fmt.Errorf("invalid cluster name")
	}
	for i, op := range ops {
		if err := validateOp(op, cat); err != nil {
			return nil, fmt.Errorf("change %d: %w", i+1, err)
		}
	}
	plan := &Plan{Cluster: cluster, Statements: []Statement{}, Diffs: []Diff{}, Warnings: []Warning{}}
	for i, op := range ops {
		inv := Inverse(op, snap)
		invSQL := make([]string, 0, len(inv))
		for _, o := range inv {
			invSQL = append(invSQL, Render(o, cluster, snap))
		}
		plan.Statements = append(plan.Statements, Statement{
			SQL: Render(op, cluster, snap), OpIndex: i, Inverse: inv, InverseSQL: strings.Join(invSQL, ";\n"), phase: phaseOf(op.Kind),
		})
	}
	sort.SliceStable(plan.Statements, func(a, b int) bool { return plan.Statements[a].phase < plan.Statements[b].phase })
	for i := range plan.Statements {
		plan.Statements[i].Seq = i + 1
	}
	plan.Diffs = Diffs(ops, snap)
	plan.Warnings = Guard(ops, snap, cluster, ctx)
	plan.PlanHash = planHash(plan.Statements, snap)
	return plan, nil
}

func planHash(stmts []Statement, snap *Snapshot) string {
	h := sha256.New()
	for _, s := range stmts {
		h.Write([]byte(s.SQL))
		h.Write([]byte{0})
	}
	h.Write([]byte(snap.Fingerprint()))
	return hex.EncodeToString(h.Sum(nil))
}

// RollbackOps returns the inverse ops of the applied statements, newest
// first, so the rollback undoes the change in reverse order.
func RollbackOps(plan *Plan, results []Result) []Op {
	applied := map[int]bool{}
	for _, r := range results {
		if r.Status == StatusApplied {
			applied[r.Seq] = true
		}
	}
	var out []Op
	for i := len(plan.Statements) - 1; i >= 0; i-- {
		s := plan.Statements[i]
		if applied[s.Seq] {
			out = append(out, s.Inverse...)
		}
	}
	if out == nil {
		out = []Op{}
	}
	return out
}
