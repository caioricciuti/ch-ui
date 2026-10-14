// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package rbac plans and renders ClickHouse access-control changes: roles,
// privilege and role grants, row policies, quotas and settings profiles.
// The client sends structured operations only; every SQL statement is built
// here from validated parts, so no raw SQL from a browser ever reaches
// ClickHouse.
package rbac

// Operation kinds accepted by Plan.
const (
	OpCreateRole            = "create_role"
	OpDropRole              = "drop_role"
	OpSetRoleProfile        = "set_role_profile"
	OpGrantPrivilege        = "grant_privilege"
	OpRevokePrivilege       = "revoke_privilege"
	OpGrantRole             = "grant_role"
	OpRevokeRole            = "revoke_role"
	OpSetDefaultRoles       = "set_default_roles"
	OpCreateRowPolicy       = "create_row_policy"
	OpAlterRowPolicy        = "alter_row_policy"
	OpDropRowPolicy         = "drop_row_policy"
	OpCreateQuota           = "create_quota"
	OpAlterQuota            = "alter_quota"
	OpDropQuota             = "drop_quota"
	OpCreateSettingsProfile = "create_settings_profile"
	OpAlterSettingsProfile  = "alter_settings_profile"
	OpDropSettingsProfile   = "drop_settings_profile"
)

// Entity kinds, used for diffs and the entity endpoint.
const (
	KindRole            = "role"
	KindUser            = "user"
	KindRowPolicy       = "row_policy"
	KindQuota           = "quota"
	KindSettingsProfile = "settings_profile"
)

// Warning severities. A block warning refuses the plan; an ack warning needs
// an explicit acknowledgement before apply.
const (
	SeverityBlock = "block"
	SeverityAck   = "ack"
)

// Target names the grantee of a privilege or role grant.
type Target struct {
	Type string `json:"type"` // "role" or "user"
	Name string `json:"name"`
}

// Assignees is the TO clause of a row policy, quota or settings profile.
type Assignees struct {
	All    bool     `json:"all,omitempty" yaml:"all,omitempty"`
	Names  []string `json:"names,omitempty" yaml:"names,omitempty"`
	Except []string `json:"except,omitempty" yaml:"except,omitempty"`
}

// RowPolicySpec describes a row policy. Name is the short name; the policy
// is identified by (Name, Database, Table).
type RowPolicySpec struct {
	Name        string    `json:"name" yaml:"name"`
	Database    string    `json:"database" yaml:"database"`
	Table       string    `json:"table" yaml:"table"`
	Using       string    `json:"using" yaml:"using"`
	Restrictive bool      `json:"restrictive,omitempty" yaml:"restrictive,omitempty"`
	To          Assignees `json:"to" yaml:"to"`
}

// QuotaInterval is one FOR INTERVAL clause. Limits maps a resource name
// (queries, read_rows, ...) to its maximum; a missing key or 0 is no limit.
type QuotaInterval struct {
	Duration   uint64            `json:"duration" yaml:"duration"`
	Unit       string            `json:"unit" yaml:"unit"`
	Randomized bool              `json:"randomized,omitempty" yaml:"randomized,omitempty"`
	Limits     map[string]uint64 `json:"limits" yaml:"limits,omitempty"`
}

// QuotaSpec describes a quota.
type QuotaSpec struct {
	Name      string          `json:"name" yaml:"name"`
	KeyedBy   string          `json:"keyed_by,omitempty" yaml:"keyed_by,omitempty"`
	Intervals []QuotaInterval `json:"intervals" yaml:"intervals,omitempty"`
	To        Assignees       `json:"to" yaml:"to"`
}

// ProfileSetting is one setting constraint inside a settings profile.
type ProfileSetting struct {
	Name string `json:"name" yaml:"name"`
	// Value, Min and Max are optional; nil means not set.
	Value       *string `json:"value,omitempty" yaml:"value,omitempty"`
	Min         *string `json:"min,omitempty" yaml:"min,omitempty"`
	Max         *string `json:"max,omitempty" yaml:"max,omitempty"`
	Writability string  `json:"writability,omitempty" yaml:"writability,omitempty"` // "", CONST, WRITABLE, CHANGEABLE_IN_READONLY
}

// ProfileSpec describes a settings profile.
type ProfileSpec struct {
	Name     string           `json:"name" yaml:"name"`
	Settings []ProfileSetting `json:"settings" yaml:"settings,omitempty"`
	Inherit  []string         `json:"inherit,omitempty" yaml:"inherit,omitempty"`
	To       Assignees        `json:"to" yaml:"to"`
}

// Op is one requested change. Which fields matter depends on Kind.
type Op struct {
	Kind string `json:"kind"`

	// Name is the role, quota or settings profile name for role, quota and
	// profile ops.
	Name string `json:"name,omitempty"`
	// Profile is the settings profile attached to a role (create_role,
	// set_role_profile). Empty in set_role_profile clears it.
	Profile string `json:"profile,omitempty"`

	// Grants and default roles.
	Target      *Target  `json:"target,omitempty"`
	Access      []string `json:"access,omitempty"`
	Database    string   `json:"database,omitempty"`
	Table       string   `json:"table,omitempty"`
	Columns     []string `json:"columns,omitempty"`
	GrantOption bool     `json:"grant_option,omitempty"`
	Roles       []string `json:"roles,omitempty"`
	AdminOption bool     `json:"admin_option,omitempty"`
	// DefaultAll sets DEFAULT ROLE ALL; with no Roles and DefaultAll false
	// the user gets DEFAULT ROLE NONE. DefaultExcept, with DefaultAll, sets
	// DEFAULT ROLE ALL EXCEPT those roles.
	DefaultAll    bool     `json:"default_all,omitempty"`
	DefaultExcept []string `json:"default_except,omitempty"`

	RowPolicy       *RowPolicySpec `json:"row_policy,omitempty"`
	Quota           *QuotaSpec     `json:"quota,omitempty"`
	SettingsProfile *ProfileSpec   `json:"settings_profile,omitempty"`
}

// Statement is one rendered DDL statement of a plan.
type Statement struct {
	Seq        int    `json:"seq"`
	SQL        string `json:"sql"`
	OpIndex    int    `json:"op_index"`
	InverseSQL string `json:"inverse_sql"`
	// Inverse holds the operations that undo this statement, used to build
	// rollback_ops after apply.
	Inverse []Op `json:"-"`
}

// Diff is the before and after of one entity touched by a plan.
type Diff struct {
	Kind     string `json:"kind"`
	Entity   string `json:"entity"`
	Current  string `json:"current"`
	Proposed string `json:"proposed"`
}

// Warning is a guard finding.
type Warning struct {
	Code     string `json:"code"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

// Plan is the result of planning a change set.
type Plan struct {
	Statements []Statement `json:"statements"`
	Diffs      []Diff      `json:"diffs"`
	Warnings   []Warning   `json:"warnings"`
	PlanHash   string      `json:"plan_hash"`
	Cluster    string      `json:"cluster,omitempty"`
}

// Blocked reports whether any warning is a block.
func (p *Plan) Blocked() bool {
	for _, w := range p.Warnings {
		if w.Severity == SeverityBlock {
			return true
		}
	}
	return false
}

// Unacknowledged returns the codes of ack warnings missing from acked.
func (p *Plan) Unacknowledged(acked []string) []string {
	have := make(map[string]bool, len(acked))
	for _, c := range acked {
		have[c] = true
	}
	var missing []string
	seen := map[string]bool{}
	for _, w := range p.Warnings {
		if w.Severity == SeverityAck && !have[w.Code] && !seen[w.Code] {
			seen[w.Code] = true
			missing = append(missing, w.Code)
		}
	}
	return missing
}

// HostResult is the outcome of an ON CLUSTER statement on one host.
type HostResult struct {
	Host   string `json:"host"`
	Status string `json:"status"` // applied, failed, pending
	Error  string `json:"error,omitempty"`
}

// Result is the outcome of one statement during apply.
type Result struct {
	Seq    int    `json:"seq"`
	SQL    string `json:"sql"`
	Status string `json:"status"` // applied, failed, skipped
	Error  string `json:"error,omitempty"`
	// Code is "already_exists" when a CREATE hit an existing entity.
	Code  string       `json:"code,omitempty"`
	Hosts []HostResult `json:"hosts,omitempty"`
}

// Result statuses.
const (
	StatusApplied = "applied"
	StatusFailed  = "failed"
	StatusSkipped = "skipped"
	StatusPending = "pending"
)
