// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Executor runs one query as the signed-in session user and returns rows as
// JSON objects. Settings may be nil.
type Executor interface {
	Query(ctx context.Context, sql string, settings map[string]string) ([]map[string]any, error)
}

// Entity is the common shape of a named access entity.
type Entity struct {
	Name    string `json:"name"`
	Storage string `json:"storage"`
}

// UserEntity is a ClickHouse user with its default roles.
type UserEntity struct {
	Name         string   `json:"name"`
	Storage      string   `json:"storage"`
	DefaultAll   bool     `json:"default_roles_all"`
	DefaultRoles []string `json:"default_roles"`
	DefaultExcpt []string `json:"default_roles_except"`
}

// RoleGrant is one row of system.role_grants.
type RoleGrant struct {
	UserName    string `json:"user_name,omitempty"`
	RoleName    string `json:"role_name,omitempty"`
	GrantedRole string `json:"granted_role"`
	IsDefault   bool   `json:"is_default"`
	AdminOption bool   `json:"admin_option"`
}

// Grantee returns the user or role holding the grant.
func (g RoleGrant) Grantee() Target {
	if g.UserName != "" {
		return Target{Type: "user", Name: g.UserName}
	}
	return Target{Type: "role", Name: g.RoleName}
}

// GrantRow is one row of system.grants.
type GrantRow struct {
	UserName      string `json:"user_name,omitempty"`
	RoleName      string `json:"role_name,omitempty"`
	Access        string `json:"access"`
	Database      string `json:"database,omitempty"`
	Table         string `json:"table,omitempty"`
	Column        string `json:"column,omitempty"`
	PartialRevoke bool   `json:"partial_revoke,omitempty"`
	GrantOption   bool   `json:"grant_option,omitempty"`
}

// Grantee returns the user or role holding the grant.
func (g GrantRow) Grantee() Target {
	if g.UserName != "" {
		return Target{Type: "user", Name: g.UserName}
	}
	return Target{Type: "role", Name: g.RoleName}
}

// RowPolicy is a row of system.row_policies.
type RowPolicy struct {
	FullName string `json:"full_name"`
	Storage  string `json:"storage"`
	RowPolicySpec
}

// Quota is a quota with its limits.
type Quota struct {
	Storage string `json:"storage"`
	QuotaSpec
}

// SettingsProfile is a profile with its elements.
type SettingsProfile struct {
	Storage string `json:"storage"`
	ProfileSpec
}

// Capabilities says what the session user may do, read from SHOW GRANTS.
type Capabilities struct {
	User             string   `json:"user"`
	Roles            []string `json:"roles"`
	AccessManagement bool     `json:"access_management"`
	ManageRoles      bool     `json:"manage_roles"`
	ManageRowPolicy  bool     `json:"manage_row_policies"`
	ManageQuotas     bool     `json:"manage_quotas"`
	ManageProfiles   bool     `json:"manage_settings_profiles"`
	RoleAdmin        bool     `json:"role_admin"`
	ShowAccess       bool     `json:"show_access"`
	Cluster          bool     `json:"cluster"`
	GrantOption      bool     `json:"grant_option"`
	Grants           []string `json:"grants"`
}

// Snapshot is the access-control state of one connection, read live.
type Snapshot struct {
	Roles            []Entity          `json:"roles"`
	Users            []UserEntity      `json:"users"`
	RoleGrants       []RoleGrant       `json:"role_grants"`
	Grants           []GrantRow        `json:"grants"`
	RowPolicies      []RowPolicy       `json:"row_policies"`
	Quotas           []Quota           `json:"quotas"`
	SettingsProfiles []SettingsProfile `json:"settings_profiles"`
	// RoleProfiles maps a role to the settings profiles it inherits.
	RoleProfiles map[string][]string `json:"role_profiles"`
	Capabilities Capabilities        `json:"capabilities"`
	// Privileges lists system.privileges for the privilege picker. It is
	// not part of the fingerprint.
	Privileges []Privilege `json:"privileges"`
	Errors     []string    `json:"errors,omitempty"`

	// The fields below feed the effective access explainer. None of them
	// is part of the fingerprint, so plans made before they existed stay
	// valid.

	// UserSettings and RoleSettings hold the settings elements set
	// directly on a user or role, in order: settings and inherited
	// profiles.
	UserSettings map[string][]ProfileElement `json:"user_settings,omitempty"`
	RoleSettings map[string][]ProfileElement `json:"role_settings,omitempty"`
	// PrivilegeParents maps a privilege to its parent_group in
	// system.privileges (SELECT -> ALL).
	PrivilegeParents map[string]string `json:"privilege_parents,omitempty"`
	// PrivilegeLevels maps a privilege to its level in system.privileges
	// (GLOBAL, DATABASE, TABLE, COLUMN, ...); groups have none.
	PrivilegeLevels map[string]string `json:"privilege_levels,omitempty"`
	// catalog caches the privilege catalog for applyOp, which owns the
	// snapshot it changes. Copies made by cloneSnapshot start without it.
	catalog *privCatalog
	// QuotaUsage is system.quotas_usage, nil when the login cannot read it.
	QuotaUsage []QuotaUsage `json:"quota_usage,omitempty"`
	// Host is hostName() of the node that answered.
	Host string `json:"host,omitempty"`
	// MultiNode is true when system.clusters has a cluster of more than
	// one node, so entities not stored in replicated storage are local.
	MultiNode bool `json:"multi_node,omitempty"`
	// UserLogins maps a user to its authentication type and hosts, never a
	// secret. Nil when system.users could not be read for them.
	UserLogins map[string]UserLogin `json:"user_logins,omitempty"`
	// UserStorages are the types of system.user_directories in precedence
	// order, empty when unknown.
	UserStorages []string `json:"user_storages,omitempty"`
}

// ProfileElement is one settings element attached directly to a user or a
// role: either a setting or an inherited profile.
type ProfileElement struct {
	Inherit string `json:"inherit,omitempty"`
	ProfileSetting
}

// QuotaUsage is one row of system.quotas_usage: the consumption of one
// quota key in one interval.
type QuotaUsage struct {
	Quota    string             `json:"quota"`
	Key      string             `json:"key"`
	Duration uint64             `json:"duration"`
	EndTime  string             `json:"end_time,omitempty"`
	Used     map[string]float64 `json:"used"`
}

// Privilege is one row of system.privileges.
type Privilege struct {
	Name  string `json:"name"`
	Level string `json:"level"`
}

// ReadSnapshot reads every access entity visible to the session user. A
// failed section is recorded in Errors and left empty; the rest still loads.
func ReadSnapshot(ctx context.Context, ex Executor) (*Snapshot, error) {
	s := &Snapshot{RoleProfiles: map[string][]string{}, UserSettings: map[string][]ProfileElement{}, RoleSettings: map[string][]ProfileElement{}}
	fail := func(section string, err error) {
		s.Errors = append(s.Errors, section+": "+err.Error())
	}

	if rows, err := ex.Query(ctx, `SELECT name, storage FROM system.roles ORDER BY name`, nil); err != nil {
		fail("roles", err)
	} else {
		for _, r := range rows {
			s.Roles = append(s.Roles, Entity{Name: str(r, "name"), Storage: str(r, "storage")})
		}
	}

	if rows, err := ex.Query(ctx, `SELECT name, storage, default_roles_all, default_roles_list, default_roles_except FROM system.users ORDER BY name`, nil); err != nil {
		fail("users", err)
	} else {
		for _, r := range rows {
			s.Users = append(s.Users, UserEntity{
				Name: str(r, "name"), Storage: str(r, "storage"), DefaultAll: boolean(r, "default_roles_all"),
				DefaultRoles: strs(r, "default_roles_list"), DefaultExcpt: strs(r, "default_roles_except"),
			})
		}
	}

	if rows, err := ex.Query(ctx, `SELECT ifNull(user_name, '') AS user_name, ifNull(role_name, '') AS role_name, granted_role_name, granted_role_is_default, with_admin_option
		FROM system.role_grants ORDER BY user_name, role_name, granted_role_name`, nil); err != nil {
		fail("role_grants", err)
	} else {
		for _, r := range rows {
			s.RoleGrants = append(s.RoleGrants, RoleGrant{
				UserName: str(r, "user_name"), RoleName: str(r, "role_name"), GrantedRole: str(r, "granted_role_name"),
				IsDefault: boolean(r, "granted_role_is_default"), AdminOption: boolean(r, "with_admin_option"),
			})
		}
	}

	if rows, err := ex.Query(ctx, `SELECT ifNull(user_name, '') AS user_name, ifNull(role_name, '') AS role_name, toString(access_type) AS access,
		ifNull(database, '') AS db, ifNull(table, '') AS tbl, ifNull(column, '') AS col, is_partial_revoke, grant_option
		FROM system.grants ORDER BY user_name, role_name, access, db, tbl, col`, nil); err != nil {
		fail("grants", err)
	} else {
		for _, r := range rows {
			s.Grants = append(s.Grants, GrantRow{
				UserName: str(r, "user_name"), RoleName: str(r, "role_name"), Access: str(r, "access"),
				Database: str(r, "db"), Table: str(r, "tbl"), Column: str(r, "col"),
				PartialRevoke: boolean(r, "is_partial_revoke"), GrantOption: boolean(r, "grant_option"),
			})
		}
	}

	if rows, err := ex.Query(ctx, `SELECT name, short_name, database, table, storage, ifNull(select_filter, '') AS select_filter,
		is_restrictive, apply_to_all, apply_to_list, apply_to_except FROM system.row_policies ORDER BY name`, nil); err != nil {
		fail("row_policies", err)
	} else {
		for _, r := range rows {
			s.RowPolicies = append(s.RowPolicies, RowPolicy{
				FullName: str(r, "name"), Storage: str(r, "storage"),
				RowPolicySpec: RowPolicySpec{
					Name: str(r, "short_name"), Database: str(r, "database"), Table: str(r, "table"),
					Using: str(r, "select_filter"), Restrictive: boolean(r, "is_restrictive"),
					To: Assignees{All: boolean(r, "apply_to_all"), Names: strs(r, "apply_to_list"), Except: strs(r, "apply_to_except")},
				},
			})
		}
	}

	if err := readQuotas(ctx, ex, s); err != nil {
		fail("quotas", err)
	}
	if err := readProfiles(ctx, ex, s); err != nil {
		fail("settings_profiles", err)
	}
	readCapabilities(ctx, ex, s)
	s.Privileges = []Privilege{}
	s.PrivilegeParents = map[string]string{}
	s.PrivilegeLevels = map[string]string{}
	if rows, err := ex.Query(ctx, `SELECT toString(privilege) AS name, ifNull(toString(level), '') AS level, ifNull(toString(parent_group), '') AS parent
		FROM system.privileges ORDER BY name`, nil); err == nil {
		for _, r := range rows {
			name := str(r, "name")
			if parent := str(r, "parent"); parent != "" {
				s.PrivilegeParents[name] = parent
			}
			if level := str(r, "level"); level != "" {
				s.PrivilegeLevels[name] = level
			}
			if privilegeRe.MatchString(name) {
				s.Privileges = append(s.Privileges, Privilege{Name: name, Level: str(r, "level")})
			}
		}
	}
	readExplainerFacts(ctx, ex, s)
	readUserLogins(ctx, ex, s)
	return s, nil
}

// readExplainerFacts loads what only the explainer needs. Failures leave the
// field empty and are not recorded as errors, since the editor does not
// depend on them.
func readExplainerFacts(ctx context.Context, ex Executor, s *Snapshot) {
	if rows, err := ex.Query(ctx, `SELECT hostName() AS h`, nil); err == nil && len(rows) > 0 {
		s.Host = str(rows[0], "h")
	}
	if rows, err := ex.Query(ctx, `SELECT count() AS n FROM (SELECT cluster FROM system.clusters GROUP BY cluster HAVING count() > 1)`, nil); err == nil && len(rows) > 0 {
		s.MultiNode = u64(rows[0], "n") > 0
	}
	if rows, err := ex.Query(ctx, `SELECT * FROM system.quotas_usage`, nil); err == nil {
		s.QuotaUsage = []QuotaUsage{}
		for _, r := range rows {
			u := QuotaUsage{Quota: str(r, "quota_name"), Key: str(r, "quota_key"), Duration: u64(r, "duration"), EndTime: str(r, "end_time"), Used: map[string]float64{}}
			for _, res := range QuotaResources {
				if v, ok := r[res]; ok && v != nil {
					f, _ := strconv.ParseFloat(str(r, res), 64)
					u.Used[res] = f
				}
			}
			s.QuotaUsage = append(s.QuotaUsage, u)
		}
	}
}

func readQuotas(ctx context.Context, ex Executor, s *Snapshot) error {
	rows, err := ex.Query(ctx, `SELECT name, storage, arrayStringConcat(arrayMap(k -> toString(k), keys), ',') AS keyed_by,
		apply_to_all, apply_to_list, apply_to_except FROM system.quotas ORDER BY name`, nil)
	if err != nil {
		return err
	}
	byName := map[string]int{}
	for _, r := range rows {
		q := Quota{Storage: str(r, "storage"), QuotaSpec: QuotaSpec{
			Name: str(r, "name"), KeyedBy: str(r, "keyed_by"), Intervals: []QuotaInterval{},
			To: Assignees{All: boolean(r, "apply_to_all"), Names: strs(r, "apply_to_list"), Except: strs(r, "apply_to_except")},
		}}
		byName[q.Name] = len(s.Quotas)
		s.Quotas = append(s.Quotas, q)
	}
	cols := make([]string, 0, len(QuotaResources))
	for _, res := range QuotaResources {
		cols = append(cols, "max_"+res)
	}
	limits, err := ex.Query(ctx, `SELECT quota_name, duration, is_randomized_interval, `+strings.Join(cols, ", ")+`
		FROM system.quota_limits ORDER BY quota_name, duration`, nil)
	if err != nil {
		return err
	}
	for _, r := range limits {
		i, ok := byName[str(r, "quota_name")]
		if !ok {
			continue
		}
		seconds := u64(r, "duration")
		n, unit := splitDuration(seconds)
		iv := QuotaInterval{Duration: n, Unit: unit, Randomized: boolean(r, "is_randomized_interval"), Limits: map[string]uint64{}}
		for _, res := range QuotaResources {
			if v, ok := r["max_"+res]; ok && v != nil {
				if n := u64(r, "max_"+res); n > 0 {
					iv.Limits[res] = n
				}
			}
		}
		s.Quotas[i].Intervals = append(s.Quotas[i].Intervals, iv)
	}
	return nil
}

func readProfiles(ctx context.Context, ex Executor, s *Snapshot) error {
	rows, err := ex.Query(ctx, `SELECT name, storage, apply_to_all, apply_to_list, apply_to_except FROM system.settings_profiles ORDER BY name`, nil)
	if err != nil {
		return err
	}
	byName := map[string]int{}
	for _, r := range rows {
		p := SettingsProfile{Storage: str(r, "storage"), ProfileSpec: ProfileSpec{
			Name: str(r, "name"), Settings: []ProfileSetting{},
			To: Assignees{All: boolean(r, "apply_to_all"), Names: strs(r, "apply_to_list"), Except: strs(r, "apply_to_except")},
		}}
		byName[p.Name] = len(s.SettingsProfiles)
		s.SettingsProfiles = append(s.SettingsProfiles, p)
	}
	elems, err := ex.Query(ctx, `SELECT ifNull(profile_name, '') AS profile_name, ifNull(role_name, '') AS role_name, ifNull(user_name, '') AS user_name,
		index, setting_name, value, min, max, toString(writability) AS writability, ifNull(inherit_profile, '') AS inherit_profile
		FROM system.settings_profile_elements ORDER BY profile_name, role_name, user_name, index`, nil)
	if err != nil {
		return err
	}
	for _, r := range elems {
		profile, role, user := str(r, "profile_name"), str(r, "role_name"), str(r, "user_name")
		inherit := str(r, "inherit_profile")
		if role != "" || user != "" {
			el := ProfileElement{Inherit: inherit, ProfileSetting: ProfileSetting{
				Name: str(r, "setting_name"), Value: optStr(r, "value"), Min: optStr(r, "min"), Max: optStr(r, "max"), Writability: str(r, "writability"),
			}}
			if role != "" {
				if inherit != "" {
					s.RoleProfiles[role] = append(s.RoleProfiles[role], inherit)
				}
				s.RoleSettings[role] = append(s.RoleSettings[role], el)
			} else {
				s.UserSettings[user] = append(s.UserSettings[user], el)
			}
			continue
		}
		i, ok := byName[profile]
		if profile == "" || !ok {
			continue
		}
		if inherit != "" {
			s.SettingsProfiles[i].Inherit = append(s.SettingsProfiles[i].Inherit, inherit)
			continue
		}
		name := str(r, "setting_name")
		if name == "" {
			continue
		}
		s.SettingsProfiles[i].Settings = append(s.SettingsProfiles[i].Settings, ProfileSetting{
			Name: name, Value: optStr(r, "value"), Min: optStr(r, "min"), Max: optStr(r, "max"), Writability: str(r, "writability"),
		})
	}
	return nil
}

func readCapabilities(ctx context.Context, ex Executor, s *Snapshot) {
	if rows, err := ex.Query(ctx, `SELECT currentUser() AS u`, nil); err == nil && len(rows) > 0 {
		s.Capabilities.User = str(rows[0], "u")
	}
	if rows, err := ex.Query(ctx, `SELECT role_name FROM system.enabled_roles ORDER BY role_name`, nil); err == nil {
		for _, r := range rows {
			s.Capabilities.Roles = append(s.Capabilities.Roles, str(r, "role_name"))
		}
	}
	lines := []string{}
	if rows, err := ex.Query(ctx, `SHOW GRANTS`, nil); err == nil {
		lines = append(lines, firstColumn(rows)...)
	}
	for _, role := range s.Capabilities.Roles {
		if rows, err := ex.Query(ctx, `SHOW GRANTS FOR `+quoteIdent(role), nil); err == nil {
			lines = append(lines, firstColumn(rows)...)
		}
	}
	s.Capabilities.Grants = lines
	applyCapabilities(&s.Capabilities, lines)
}

// applyCapabilities derives capability flags from SHOW GRANTS lines. Only
// global grants (ON *.* or ON *) count.
func applyCapabilities(c *Capabilities, lines []string) {
	global := map[string]bool{}
	for _, line := range lines {
		l := strings.TrimSpace(line)
		if !strings.HasPrefix(l, "GRANT ") {
			continue
		}
		on := strings.LastIndex(l, " ON ")
		to := strings.LastIndex(l, " TO ")
		if on < 0 || to < on {
			continue
		}
		object := strings.TrimSpace(l[on+4 : to])
		if object != "*.*" && object != "*" {
			continue
		}
		if strings.HasSuffix(l, "WITH GRANT OPTION") {
			c.GrantOption = true
		}
		for _, p := range strings.Split(l[len("GRANT "):on], ",") {
			global[strings.TrimSpace(p)] = true
		}
	}
	all := global["ALL"] || global["ALL PRIVILEGES"]
	am := all || global["ACCESS MANAGEMENT"]
	has := func(names ...string) bool {
		if am {
			return true
		}
		for _, n := range names {
			if !global[n] {
				return false
			}
		}
		return true
	}
	c.AccessManagement = am
	c.ManageRoles = has("CREATE ROLE", "ALTER ROLE", "DROP ROLE")
	c.ManageRowPolicy = has("CREATE ROW POLICY", "ALTER ROW POLICY", "DROP ROW POLICY")
	c.ManageQuotas = has("CREATE QUOTA", "ALTER QUOTA", "DROP QUOTA")
	c.ManageProfiles = has("CREATE SETTINGS PROFILE", "ALTER SETTINGS PROFILE", "DROP SETTINGS PROFILE")
	c.RoleAdmin = has("ROLE ADMIN")
	c.ShowAccess = am || global["SHOW ACCESS"] || (global["SHOW USERS"] && global["SHOW ROLES"])
	c.Cluster = all || global["CLUSTER"]
	// SHOW GRANTS lists the granted privileges one by one and never prints
	// ACCESS MANAGEMENT itself, so holding every part counts as holding it.
	if !am && c.ManageRoles && c.ManageRowPolicy && c.ManageQuotas && c.ManageProfiles && c.RoleAdmin && c.ShowAccess {
		c.AccessManagement = true
	}
}

// Fingerprint hashes the snapshot so a plan made on one state cannot be
// applied on another.
func (s *Snapshot) Fingerprint() string {
	payload := struct {
		Roles            []Entity
		Users            []UserEntity
		RoleGrants       []RoleGrant
		Grants           []GrantRow
		RowPolicies      []RowPolicy
		Quotas           []Quota
		SettingsProfiles []SettingsProfile
		RoleProfiles     map[string][]string
	}{s.Roles, s.Users, s.RoleGrants, s.Grants, s.RowPolicies, s.Quotas, s.SettingsProfiles, s.RoleProfiles}
	b, _ := json.Marshal(payload)
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// Lookups.

func (s *Snapshot) role(name string) *Entity {
	for i := range s.Roles {
		if s.Roles[i].Name == name {
			return &s.Roles[i]
		}
	}
	return nil
}

func (s *Snapshot) user(name string) *UserEntity {
	for i := range s.Users {
		if s.Users[i].Name == name {
			return &s.Users[i]
		}
	}
	return nil
}

func (s *Snapshot) rowPolicy(name, database, table string) *RowPolicy {
	for i := range s.RowPolicies {
		p := &s.RowPolicies[i]
		if p.Name == name && p.Database == database && p.Table == table {
			return p
		}
	}
	return nil
}

// RowPolicyByFullName finds a policy by its system.row_policies name.
func (s *Snapshot) RowPolicyByFullName(full string) *RowPolicy {
	for i := range s.RowPolicies {
		if s.RowPolicies[i].FullName == full {
			return &s.RowPolicies[i]
		}
	}
	return nil
}

func (s *Snapshot) quota(name string) *Quota {
	for i := range s.Quotas {
		if s.Quotas[i].Name == name {
			return &s.Quotas[i]
		}
	}
	return nil
}

func (s *Snapshot) profile(name string) *SettingsProfile {
	for i := range s.SettingsProfiles {
		if s.SettingsProfiles[i].Name == name {
			return &s.SettingsProfiles[i]
		}
	}
	return nil
}

// storageOf returns the storage of a named entity, or "" when unknown.
func (s *Snapshot) storageOf(kind, name string) string {
	switch kind {
	case KindRole:
		if r := s.role(name); r != nil {
			return r.Storage
		}
	case KindUser:
		if u := s.user(name); u != nil {
			return u.Storage
		}
	case KindQuota:
		if q := s.quota(name); q != nil {
			return q.Storage
		}
	case KindSettingsProfile:
		if p := s.profile(name); p != nil {
			return p.Storage
		}
	}
	return ""
}

// Row decoding. ClickHouse JSON quotes 64-bit integers, so numbers arrive
// either as JSON numbers or as strings.

func str(r map[string]any, k string) string {
	switch v := r[k].(type) {
	case nil:
		return ""
	case string:
		return v
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

func optStr(r map[string]any, k string) *string {
	if r[k] == nil {
		return nil
	}
	v := str(r, k)
	return &v
}

func u64(r map[string]any, k string) uint64 {
	switch v := r[k].(type) {
	case float64:
		if v < 0 {
			return 0
		}
		return uint64(v)
	case string:
		n, _ := strconv.ParseFloat(v, 64)
		if n < 0 {
			return 0
		}
		return uint64(n)
	case bool:
		if v {
			return 1
		}
	}
	return 0
}

func boolean(r map[string]any, k string) bool {
	switch v := r[k].(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	}
	return false
}

func strs(r map[string]any, k string) []string {
	out := []string{}
	if arr, ok := r[k].([]any); ok {
		for _, v := range arr {
			if s, ok := v.(string); ok {
				out = append(out, s)
			} else if v != nil {
				out = append(out, fmt.Sprint(v))
			}
		}
	}
	sort.Strings(out)
	return out
}

func firstColumn(rows []map[string]any) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		keys := make([]string, 0, len(r))
		for k := range r {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			out = append(out, str(r, keys[0]))
		}
	}
	return out
}

var durationUnits = []struct {
	name    string
	seconds uint64
}{
	{"year", 31556952}, {"quarter", 7889238}, {"month", 2629746}, {"week", 604800},
	{"day", 86400}, {"hour", 3600}, {"minute", 60}, {"second", 1},
}

// splitDuration turns seconds into the largest whole unit.
func splitDuration(seconds uint64) (uint64, string) {
	for _, u := range durationUnits {
		if seconds >= u.seconds && seconds%u.seconds == 0 {
			return seconds / u.seconds, u.name
		}
	}
	return seconds, "second"
}

// unitSeconds returns the length of one unit in seconds.
func unitSeconds(unit string) uint64 {
	for _, u := range durationUnits {
		if u.name == unit {
			return u.seconds
		}
	}
	return 0
}
