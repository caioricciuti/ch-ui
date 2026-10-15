// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

func quoteIdent(s string) string { return safe.Identifier(s) }

func quoteLit(s string) string { return safe.QuotedLiteral(s) }

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
// by ALTER QUOTA to clear removed limits; it may be nil. A create_user comes
// out with its hash and salt masked; only Compose builds the real one.
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
		case op.DefaultAll && len(op.DefaultExcept) > 0:
			return s + "ALL EXCEPT " + identList(op.DefaultExcept)
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
	case OpCreateUser:
		return renderCreateUser(op, on, false)
	case OpDropUser:
		return "DROP USER IF EXISTS " + quoteIdent(op.Name) + on
	}
	return ""
}

// Compose validates ops and builds the plan: ordered statements with their
// inverses, entity diffs, guard warnings and the plan hash. It reads nothing
// from ClickHouse; the snapshot and catalog are passed in.
func Compose(ops []Op, snap *Snapshot, cat Catalog, cluster string, ctx GuardContext) (*Plan, error) {
	if len(ops) == 0 {
		return nil, fmt.Errorf("no changes to plan")
	}
	if len(ops) > MaxImportOps {
		return nil, fmt.Errorf("too many changes in one plan (%d, max %d); import or apply them in batches", len(ops), MaxImportOps)
	}
	if cluster != "" && !clusterhealth.IsValidClusterName(cluster) {
		return nil, fmt.Errorf("invalid cluster name")
	}
	for i, op := range ops {
		if err := validateOp(op, cat); err != nil {
			return nil, fmt.Errorf("change %d: %w", i+1, err)
		}
	}
	if err := checkOrder(ops, snap); err != nil {
		return nil, err
	}
	plan := &Plan{Cluster: cluster, Statements: []Statement{}, Diffs: []Diff{}, Warnings: []Warning{}}
	// Statements run in the order they were staged, the same order Diffs
	// simulates, so the preview is what apply does. Each inverse is read
	// from the state right before its statement.
	state := cloneSnapshot(snap)
	for i, op := range ops {
		inv := Inverse(op, state)
		sql := Render(op, cluster, state)
		applyOp(state, op)
		invSQL := make([]string, 0, len(inv))
		for _, o := range inv {
			invSQL = append(invSQL, Render(o, cluster, state))
		}
		st := Statement{Seq: i + 1, SQL: sql, OpIndex: i, Inverse: inv, InverseSQL: strings.Join(invSQL, ";\n")}
		if op.Kind == OpCreateUser {
			st.execSQL = renderCreateUser(op, clusterhealth.OnCluster(cluster), true)
			st.secrets = opSecrets(op)
		}
		plan.Statements = append(plan.Statements, st)
	}
	plan.Diffs = Diffs(ops, snap)
	plan.Warnings = Guard(ops, snap, cluster, ctx)
	plan.PlanHash = planHash(plan.Statements, snap)
	return plan, nil
}

// checkOrder refuses a plan that uses a role, user or settings profile
// before the change that creates it, since statements run in staged order,
// and a plan that creates the same user twice.
func checkOrder(ops []Op, snap *Snapshot) error {
	createdRole, createdProfile, createdUser := map[string]int{}, map[string]int{}, map[string]int{}
	for i, op := range ops {
		if _, ok := createdRole[op.Name]; !ok && op.Kind == OpCreateRole {
			createdRole[op.Name] = i
		}
		if op.Kind == OpCreateUser {
			if j, ok := createdUser[op.Name]; ok {
				return fmt.Errorf("changes %d and %d both create user %q", j+1, i+1, op.Name)
			}
			createdUser[op.Name] = i
		}
		if op.Kind == OpCreateSettingsProfile {
			if _, ok := createdProfile[op.SettingsProfile.Name]; !ok {
				createdProfile[op.SettingsProfile.Name] = i
			}
		}
	}
	for i, op := range ops {
		var roles, profiles, users []string
		switch op.Kind {
		case OpCreateRole, OpCreateUser:
			profiles = []string{op.Profile}
		case OpSetRoleProfile:
			roles, profiles = []string{op.Name}, []string{op.Profile}
		case OpGrantPrivilege, OpRevokePrivilege, OpGrantRole, OpRevokeRole, OpSetDefaultRoles:
			if op.Target.Type == "role" {
				roles = append(roles, op.Target.Name)
			} else {
				users = append(users, op.Target.Name)
			}
			roles = append(append(roles, op.Roles...), op.DefaultExcept...)
		case OpCreateRowPolicy, OpAlterRowPolicy:
			roles = append(append(roles, op.RowPolicy.To.Names...), op.RowPolicy.To.Except...)
		case OpCreateQuota, OpAlterQuota:
			roles = append(append(roles, op.Quota.To.Names...), op.Quota.To.Except...)
		case OpCreateSettingsProfile, OpAlterSettingsProfile:
			roles = append(append(roles, op.SettingsProfile.To.Names...), op.SettingsProfile.To.Except...)
			profiles = op.SettingsProfile.Inherit
		}
		switch op.Kind {
		case OpCreateRowPolicy, OpAlterRowPolicy, OpCreateQuota, OpAlterQuota, OpCreateSettingsProfile, OpAlterSettingsProfile:
			// TO clauses name roles and users alike.
			users = append(users, roles...)
		}
		for _, u := range users {
			if j, ok := createdUser[u]; ok && j > i && snap.user(u) == nil {
				return fmt.Errorf("change %d uses user %q before change %d creates it; stage the create first", i+1, u, j+1)
			}
		}
		for _, r := range roles {
			if j, ok := createdRole[r]; ok && j > i && snap.role(r) == nil {
				return fmt.Errorf("change %d uses role %q before change %d creates it; stage the create first", i+1, r, j+1)
			}
		}
		for _, p := range profiles {
			if j, ok := createdProfile[p]; ok && j > i && snap.profile(p) == nil {
				return fmt.Errorf("change %d uses settings profile %q before change %d creates it; stage the create first", i+1, p, j+1)
			}
		}
	}
	return nil
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
