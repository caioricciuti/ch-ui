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
	case OpSetRoleProfile:
		if op.Profile == "" {
			delete(s.RoleProfiles, op.Name)
		} else {
			s.RoleProfiles[op.Name] = []string{op.Profile}
		}
	case OpGrantPrivilege, OpRevokePrivilege:
		db, tbl := normDB(op.Database), normDB(op.Table)
		for _, a := range op.Access {
			for _, col := range columnsOrBlank(op.Columns) {
				applyGrant(s, *op.Target, GrantRow{Access: a, Database: db, Table: tbl, Column: col}, op.Kind == OpGrantPrivilege, op.GrantOption)
			}
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
			u.DefaultExcpt = []string{}
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

func applyGrant(s *Snapshot, t Target, row GrantRow, grant, grantOptionOnly bool) {
	if t.Type == "user" {
		row.UserName = t.Name
	} else {
		row.RoleName = t.Name
	}
	idx := -1
	for i, g := range s.Grants {
		if g.Grantee() == t && g.Access == row.Access && g.Database == row.Database && g.Table == row.Table && g.Column == row.Column {
			idx = i
		}
	}
	if grant {
		if idx >= 0 && s.Grants[idx].PartialRevoke {
			s.Grants = append(s.Grants[:idx], s.Grants[idx+1:]...)
			idx = -1
		}
		if idx >= 0 {
			s.Grants[idx].GrantOption = s.Grants[idx].GrantOption || grantOptionOnly
			return
		}
		row.GrantOption = grantOptionOnly
		s.Grants = append(s.Grants, row)
		return
	}
	if idx >= 0 {
		if grantOptionOnly {
			s.Grants[idx].GrantOption = false
		} else {
			s.Grants = append(s.Grants[:idx], s.Grants[idx+1:]...)
		}
		return
	}
	if !grantOptionOnly {
		row.PartialRevoke = true
		s.Grants = append(s.Grants, row)
	}
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
		dr := Op{Kind: OpSetDefaultRoles, Target: &Target{Type: "user", Name: name}, DefaultAll: u.DefaultAll, Roles: u.DefaultRoles}
		head = Render(dr, "", nil)
		if u.DefaultAll && len(u.DefaultExcpt) > 0 {
			head += " EXCEPT " + identList(u.DefaultExcpt)
		}
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
