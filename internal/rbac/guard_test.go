// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import "testing"

func guardSnapshot() *Snapshot {
	return &Snapshot{
		RoleProfiles: map[string][]string{},
		Roles: []Entity{
			{Name: "admins", Storage: "local_directory"},
			{Name: "readers", Storage: "local_directory"},
			{Name: "xmlrole", Storage: "users_xml"},
			{Name: "shared", Storage: "replicated"},
		},
		Users: []UserEntity{
			{Name: "me", Storage: "local_directory"},
			{Name: "alice", Storage: "local_directory"},
			{Name: "default", Storage: "users_xml"},
			{Name: "svc", Storage: "local_directory"},
		},
		Grants: []GrantRow{
			{RoleName: "admins", Access: "ACCESS MANAGEMENT"},
			{RoleName: "readers", Access: "SELECT", Database: "d"},
		},
		RoleGrants: []RoleGrant{{UserName: "me", GrantedRole: "admins"}},
		SettingsProfiles: []SettingsProfile{
			{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "ro", Settings: []ProfileSetting{{Name: "readonly", Value: sp("1")}}}},
			{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "child", Inherit: []string{"ro"}}},
			{Storage: "local_directory", ProfileSpec: ProfileSpec{Name: "fast", Settings: []ProfileSetting{{Name: "max_threads", Value: sp("8")}}}},
		},
		Capabilities: Capabilities{User: "me", Roles: []string{"admins"}},
	}
}

func codes(ws []Warning) map[string]string {
	out := map[string]string{}
	for _, w := range ws {
		out[w.Code] = w.Severity
	}
	return out
}

func TestGuardMatrix(t *testing.T) {
	ctx := GuardContext{ServiceAccounts: map[string]string{"svc": "background schedule"}}
	cases := []struct {
		name     string
		op       Op
		cluster  string
		code     string
		severity string // "" means the code must not appear
	}{
		{"revoke AM from self", Op{Kind: OpRevokePrivilege, Target: user("me"), Access: []string{"ACCESS MANAGEMENT"}}, "", CodeSelfRevoke, SeverityBlock},
		{"revoke ALL from held role", Op{Kind: OpRevokePrivilege, Target: role("admins"), Access: []string{"ALL"}}, "", CodeSelfRevoke, SeverityBlock},
		{"revoke ROLE ADMIN from self", Op{Kind: OpRevokePrivilege, Target: user("me"), Access: []string{"ROLE ADMIN"}}, "", CodeSelfRevoke, SeverityBlock},
		{"revoke SHOW USERS from self", Op{Kind: OpRevokePrivilege, Target: user("me"), Access: []string{"SHOW USERS"}}, "", CodeSelfRevoke, SeverityBlock},
		{"revoke grant option only from self", Op{Kind: OpRevokePrivilege, Target: user("me"), Access: []string{"ACCESS MANAGEMENT"}, GrantOption: true}, "", CodeSelfRevoke, ""},
		{"revoke SELECT from self", Op{Kind: OpRevokePrivilege, Target: user("me"), Access: []string{"SELECT"}}, "", CodeSelfRevoke, ""},
		{"revoke SHOW USERS from other", Op{Kind: OpRevokePrivilege, Target: user("alice"), Access: []string{"SHOW USERS"}}, "", CodeAdminDemotion, SeverityAck},
		{"revoke held role from self", Op{Kind: OpRevokeRole, Target: user("me"), Roles: []string{"admins"}}, "", CodeSelfRevoke, SeverityBlock},
		{"revoke harmless role from self", Op{Kind: OpRevokeRole, Target: user("me"), Roles: []string{"readers"}}, "", CodeSelfRoleChange, SeverityAck},
		{"drop held role", Op{Kind: OpDropRole, Name: "admins"}, "", CodeSelfDropRole, SeverityBlock},
		{"drop other role", Op{Kind: OpDropRole, Name: "readers"}, "", CodeSelfDropRole, ""},
		{"restrictive policy to all", Op{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", Restrictive: true, To: Assignees{All: true}}}, "", CodeSelfRestrictive, SeverityBlock},
		{"restrictive policy to held role", Op{Kind: OpAlterRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", Restrictive: true, To: Assignees{Names: []string{"admins"}}}}, "", CodeSelfRestrictive, SeverityBlock},
		{"restrictive policy except self", Op{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", Restrictive: true, To: Assignees{All: true, Except: []string{"me"}}}}, "", CodeSelfRestrictive, ""},
		{"permissive policy to self", Op{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", To: Assignees{Names: []string{"me"}}}}, "", CodeSelfRestrictive, ""},
		{"new policy scope", Op{Kind: OpCreateRowPolicy, RowPolicy: &RowPolicySpec{Name: "p", Database: "d", Table: "t", Using: "1", To: Assignees{Names: []string{"alice"}}}}, "", CodeRowPolicyScope, SeverityAck},
		{"readonly profile to self", Op{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "x", Settings: []ProfileSetting{{Name: "readonly", Value: sp("2")}}, To: Assignees{Names: []string{"me"}}}}, "", CodeSelfReadonlyProfile, SeverityBlock},
		{"readonly zero to self", Op{Kind: OpCreateSettingsProfile, SettingsProfile: &ProfileSpec{Name: "x", Settings: []ProfileSetting{{Name: "readonly", Value: sp("0")}}, To: Assignees{Names: []string{"me"}}}}, "", CodeSelfReadonlyProfile, ""},
		{"inherited readonly to self", Op{Kind: OpAlterSettingsProfile, SettingsProfile: &ProfileSpec{Name: "child", Inherit: []string{"ro"}, To: Assignees{All: true}}}, "", CodeSelfReadonlyProfile, SeverityBlock},
		{"readonly profile on held role", Op{Kind: OpSetRoleProfile, Name: "admins", Profile: "ro"}, "", CodeSelfReadonlyProfile, SeverityBlock},
		{"plain profile on held role", Op{Kind: OpSetRoleProfile, Name: "admins", Profile: "fast"}, "", CodeSelfReadonlyProfile, ""},
		{"users_xml role", Op{Kind: OpDropRole, Name: "xmlrole"}, "", CodeReadOnlyStorage, SeverityBlock},
		{"users_xml user grant", Op{Kind: OpGrantPrivilege, Target: user("default"), Access: []string{"SELECT"}}, "", CodeReadOnlyStorage, SeverityBlock},
		{"service account grant", Op{Kind: OpGrantPrivilege, Target: user("svc"), Access: []string{"SELECT"}}, "", CodeServiceAccount, SeverityAck},
		{"service account in quota", Op{Kind: OpCreateQuota, Quota: &QuotaSpec{Name: "q", To: Assignees{Names: []string{"svc"}}}}, "", CodeServiceAccount, SeverityAck},
		{"replicated on cluster", Op{Kind: OpGrantPrivilege, Target: role("shared"), Access: []string{"SELECT"}}, "c", CodeOnClusterReplicated, SeverityAck},
		{"replicated no cluster", Op{Kind: OpGrantPrivilege, Target: role("shared"), Access: []string{"SELECT"}}, "", CodeOnClusterReplicated, ""},
	}
	for _, c := range cases {
		got := codes(Guard([]Op{c.op}, guardSnapshot(), c.cluster, ctx))
		if got[c.code] != c.severity {
			t.Errorf("%s: %s = %q, want %q (all: %v)", c.name, c.code, got[c.code], c.severity, got)
		}
	}
}

func TestPlanBlockedAndAcknowledged(t *testing.T) {
	plan := &Plan{Warnings: []Warning{{Code: "a", Severity: SeverityAck}, {Code: "a", Severity: SeverityAck}, {Code: "b", Severity: SeverityAck}}}
	if plan.Blocked() {
		t.Fatal("ack warnings must not block")
	}
	if missing := plan.Unacknowledged([]string{"a"}); len(missing) != 1 || missing[0] != "b" {
		t.Fatalf("missing = %v", missing)
	}
	plan.Warnings = append(plan.Warnings, Warning{Code: "c", Severity: SeverityBlock})
	if !plan.Blocked() {
		t.Fatal("block warning must block")
	}
}
