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

// Warning codes.
const (
	CodeSelfRevoke          = "self_revoke"
	CodeSelfRoleRevoke      = "self_role_revoke"
	CodeSelfDropRole        = "self_drop_role"
	CodeSelfRestrictive     = "self_restrictive_policy"
	CodeSelfReadonlyProfile = "self_readonly_profile"
	CodeReadOnlyStorage     = "readonly_storage"
	CodeAdminDemotion       = "admin_demotion"
	CodeServiceAccount      = "service_account"
	CodeRowPolicyScope      = "row_policy_scope"
	CodeOnClusterReplicated = "on_cluster_replicated"
	CodeSelfRoleChange      = "self_role_change"
)

// GuardContext carries facts the snapshot does not hold.
type GuardContext struct {
	// ServiceAccounts maps a ClickHouse user name to why CH-UI uses it
	// unattended (background worker, MCP key, SSO service account).
	ServiceAccounts map[string]string
}

// criticalPrivileges keep the session able to manage access and keep CH-UI
// detecting it as an admin.
var criticalPrivileges = map[string]bool{
	"ALL": true, "ACCESS MANAGEMENT": true, "ROLE ADMIN": true, "SHOW USERS": true, "SHOW ACCESS": true,
}

// adminDetectPrivileges are the ones CH-UI's admin auto-detection relies on
// (reading system.users).
var adminDetectPrivileges = map[string]bool{
	"ALL": true, "ACCESS MANAGEMENT": true, "SHOW USERS": true, "SHOW ACCESS": true,
}

type guard struct {
	snap     *Snapshot
	ctx      GuardContext
	cluster  string
	ops      []Op
	out      []Warning
	seen     map[string]bool
	selfUser string
	selfRole map[string]bool
}

func (g *guard) add(code, severity, format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	key := code + "\x00" + msg
	if g.seen[key] {
		return
	}
	g.seen[key] = true
	g.out = append(g.out, Warning{Code: code, Severity: severity, Message: msg})
}

func (g *guard) isSelf(t Target) bool {
	if t.Type == "user" {
		return g.selfUser != "" && t.Name == g.selfUser
	}
	return g.selfRole[t.Name]
}

// coversSelf reports whether a TO clause applies to the session user.
func (g *guard) coversSelf(a Assignees) bool {
	in := func(list []string) bool {
		for _, n := range list {
			if n == g.selfUser || g.selfRole[n] {
				return true
			}
		}
		return false
	}
	if a.All {
		return !in(a.Except)
	}
	return in(a.Names)
}

// roleCarries reports whether role (or a role granted to it) holds one of
// privs globally.
func (g *guard) roleCarries(role string, privs map[string]bool) bool {
	visited := map[string]bool{}
	var walk func(string) bool
	walk = func(r string) bool {
		if visited[r] {
			return false
		}
		visited[r] = true
		for _, gr := range g.snap.Grants {
			if gr.RoleName == r && !gr.PartialRevoke && privs[gr.Access] && gr.Database == "" {
				return true
			}
		}
		for _, rg := range g.snap.RoleGrants {
			if rg.RoleName == r && walk(rg.GrantedRole) {
				return true
			}
		}
		return false
	}
	return walk(role)
}

// profileReadonly reports whether a profile (as proposed in this plan, or
// as it is now) sets readonly to a non-zero value, directly or through one
// level of INHERIT.
func (g *guard) profileReadonly(name string, depth int) bool {
	if depth > 4 || name == "" {
		return false
	}
	var spec *ProfileSpec
	for i := range g.ops {
		op := g.ops[i]
		if (op.Kind == OpCreateSettingsProfile || op.Kind == OpAlterSettingsProfile) && op.SettingsProfile != nil && op.SettingsProfile.Name == name {
			spec = op.SettingsProfile
		}
	}
	if spec == nil {
		if p := g.snap.profile(name); p != nil {
			spec = &p.ProfileSpec
		}
	}
	if spec == nil {
		return false
	}
	if specReadonly(*spec) {
		return true
	}
	for _, parent := range spec.Inherit {
		if g.profileReadonly(parent, depth+1) {
			return true
		}
	}
	return false
}

func specReadonly(p ProfileSpec) bool {
	for _, s := range p.Settings {
		if s.Name == "readonly" && s.Value != nil {
			v := strings.Trim(strings.TrimSpace(*s.Value), "'")
			if v != "" && v != "0" {
				return true
			}
		}
	}
	return false
}

func (g *guard) readOnlyStorage(kind, name string) {
	if g.snap.storageOf(kind, name) == "users_xml" {
		g.add(CodeReadOnlyStorage, SeverityBlock, "%s %q is defined in users.xml, which is read-only. Change it in the server configuration instead.", kindLabel(kind), name)
	}
}

func kindLabel(kind string) string {
	switch kind {
	case KindRole:
		return "Role"
	case KindUser:
		return "User"
	case KindRowPolicy:
		return "Row policy"
	case KindQuota:
		return "Quota"
	case KindSettingsProfile:
		return "Settings profile"
	}
	return kind
}

func sortedNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Guard checks a change set for self-impact and other risks. Block
// warnings refuse the plan; ack warnings need an explicit acknowledgement.
func Guard(ops []Op, snap *Snapshot, cluster string, ctx GuardContext) []Warning {
	g := &guard{snap: snap, ctx: ctx, cluster: cluster, ops: ops, seen: map[string]bool{}, selfUser: snap.Capabilities.User, selfRole: map[string]bool{}}
	for _, r := range snap.Capabilities.Roles {
		g.selfRole[r] = true
	}
	serviceTouched := map[string]bool{}
	checkService := func(names ...string) {
		for _, n := range names {
			if _, ok := ctx.ServiceAccounts[n]; ok {
				serviceTouched[n] = true
			}
		}
	}
	replicated := false
	noteStorage := func(kind, name string) {
		if g.snap.storageOf(kind, name) == "replicated" {
			replicated = true
		}
	}

	for _, op := range ops {
		switch op.Kind {
		case OpDropRole:
			g.readOnlyStorage(KindRole, op.Name)
			noteStorage(KindRole, op.Name)
			if g.selfRole[op.Name] {
				g.add(CodeSelfDropRole, SeverityBlock, "You hold role %q. Dropping it would change your own access in the middle of the change.", op.Name)
			} else if g.roleCarries(op.Name, adminDetectPrivileges) {
				g.add(CodeAdminDemotion, SeverityAck, "Role %q lets its members read system.users. Members who sign in to CH-UI with it may no longer be detected as admins.", op.Name)
			}
		case OpSetRoleProfile:
			g.readOnlyStorage(KindRole, op.Name)
			noteStorage(KindRole, op.Name)
			if g.selfRole[op.Name] && g.profileReadonly(op.Profile, 0) {
				g.add(CodeSelfReadonlyProfile, SeverityBlock, "Profile %q is read-only and would apply to your own session through role %q.", op.Profile, op.Name)
			}
		case OpCreateRole:
			noteStorage(KindRole, op.Name)
		case OpGrantPrivilege, OpRevokePrivilege, OpGrantRole, OpRevokeRole, OpSetDefaultRoles:
			t := *op.Target
			g.readOnlyStorage(t.Type, t.Name)
			noteStorage(t.Type, t.Name)
			if t.Type == "user" {
				checkService(t.Name)
			}
			if op.Kind == OpRevokePrivilege && !op.GrantOption {
				crit, demote := map[string]bool{}, map[string]bool{}
				for _, a := range op.Access {
					if criticalPrivileges[a] {
						crit[a] = true
					}
					if adminDetectPrivileges[a] {
						demote[a] = true
					}
				}
				if len(crit) > 0 && g.isSelf(t) {
					g.add(CodeSelfRevoke, SeverityBlock, "This revokes %s from %s %q, which your session relies on to manage access.", strings.Join(sortedNames(crit), ", "), t.Type, t.Name)
				} else if len(demote) > 0 {
					g.add(CodeAdminDemotion, SeverityAck, "Revoking %s from %s %q stops it reading system.users. People who sign in to CH-UI with it may no longer be detected as admins.", strings.Join(sortedNames(demote), ", "), t.Type, t.Name)
				}
			}
			if op.Kind == OpRevokeRole && !op.AdminOption && g.isSelf(t) {
				for _, r := range op.Roles {
					if g.roleCarries(r, criticalPrivileges) {
						g.add(CodeSelfRevoke, SeverityBlock, "Role %q carries access management privileges your session relies on. Revoking it from %s %q is refused.", r, t.Type, t.Name)
					} else {
						g.add(CodeSelfRoleChange, SeverityAck, "This revokes role %q from %s %q, which changes your own access.", r, t.Type, t.Name)
					}
				}
			}
			if op.Kind == OpSetDefaultRoles && g.isSelf(t) {
				g.add(CodeSelfRoleChange, SeverityAck, "This changes the default roles of your own user %q; it applies from your next sign-in.", t.Name)
			}
		case OpCreateRowPolicy, OpAlterRowPolicy, OpDropRowPolicy:
			p := op.RowPolicy
			if op.Kind != OpCreateRowPolicy {
				if cur := snap.rowPolicy(p.Name, p.Database, p.Table); cur != nil && cur.Storage == "users_xml" {
					g.add(CodeReadOnlyStorage, SeverityBlock, "Row policy %q is defined in users.xml, which is read-only.", policyFullName(*p))
				} else if cur != nil && cur.Storage == "replicated" {
					replicated = true
				}
			}
			if op.Kind == OpDropRowPolicy {
				continue
			}
			checkService(p.To.Names...)
			if p.Restrictive && g.coversSelf(p.To) {
				g.add(CodeSelfRestrictive, SeverityBlock, "Restrictive policy %q would apply to your own session. Exclude your user and roles from it.", policyFullName(*p))
			}
			if op.Kind == OpCreateRowPolicy {
				g.add(CodeRowPolicyScope, SeverityAck, "Row policy %q on %s.%s can hide rows from users it does not mention, depending on the server setting users_without_row_policies_can_read_rows.", p.Name, p.Database, p.Table)
			}
		case OpCreateQuota, OpAlterQuota, OpDropQuota:
			name := op.Name
			if op.Quota != nil {
				name = op.Quota.Name
				checkService(op.Quota.To.Names...)
			}
			if op.Kind != OpCreateQuota {
				g.readOnlyStorage(KindQuota, name)
			}
			noteStorage(KindQuota, name)
		case OpCreateSettingsProfile, OpAlterSettingsProfile, OpDropSettingsProfile:
			name := op.Name
			if op.SettingsProfile != nil {
				name = op.SettingsProfile.Name
				checkService(op.SettingsProfile.To.Names...)
				if g.coversSelf(op.SettingsProfile.To) && g.profileReadonly(name, 0) {
					g.add(CodeSelfReadonlyProfile, SeverityBlock, "Settings profile %q is read-only and would apply to your own session.", name)
				}
			}
			if op.Kind != OpCreateSettingsProfile {
				g.readOnlyStorage(KindSettingsProfile, name)
			}
			noteStorage(KindSettingsProfile, name)
		}
	}
	for _, n := range sortedNames(serviceTouched) {
		g.add(CodeServiceAccount, SeverityAck, "User %q is used by CH-UI unattended (%s). Changing its access can break that work.", n, ctx.ServiceAccounts[n])
	}
	if cluster != "" && replicated {
		g.add(CodeOnClusterReplicated, SeverityAck, "Some entities use replicated access storage, which already copies changes to every node. ON CLUSTER on top of it can fail with \"already exists\" on other hosts.")
	}
	if g.out == nil {
		return []Warning{}
	}
	return g.out
}
