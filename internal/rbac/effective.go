// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// The effective access explainer answers "what can user U do on db.table,
// and why" and the reverse "who can do P on db.table". Everything here is a
// pure function of a Snapshot; ExplainLive and WhoCanLive read the snapshot
// and cross-check with SHOW GRANTS ... FINAL.

// Verdict statuses.
const (
	VerdictAllowed     = "allowed"
	VerdictPartial     = "partial"
	VerdictDenied      = "denied"
	VerdictBlocked     = "blocked"
	VerdictConditional = "conditional"
	VerdictUnknown     = "unknown"
)

// Evidence effects.
const (
	EffectAllows  = "allows"
	EffectRemoves = "removes"
	EffectFilters = "filters"
	EffectLimits  = "limits"
	EffectBlocks  = "blocks"
)

// Evidence modes: when a step applies.
const (
	ModeBoth     = "both"
	ModeAllRoles = "all_roles"
)

// ErrUnknownUser is returned when the snapshot lists every user and the
// asked one is not among them.
var ErrUnknownUser = errors.New("user not found")

// ExplainQuery is one question. Database, Table and Column narrow the
// object from *.* down to one column; Privilege defaults to SELECT.
type ExplainQuery struct {
	User      string `json:"user"`
	Database  string `json:"database"`
	Table     string `json:"table"`
	Column    string `json:"column"`
	Privilege string `json:"privilege"`
}

// Validate checks names and the privilege. known may be nil.
func (q *ExplainQuery) Validate(userRequired bool, known map[string]bool) error {
	q.Privilege = strings.ToUpper(strings.TrimSpace(q.Privilege))
	if q.Privilege == "" {
		q.Privilege = "SELECT"
	}
	if userRequired || q.User != "" {
		if err := ValidateName("user", q.User); err != nil {
			return err
		}
	}
	if q.Table != "" && q.Database == "" {
		return fmt.Errorf("a table needs a database")
	}
	if q.Column != "" && q.Table == "" {
		return fmt.Errorf("a column needs a table")
	}
	for what, v := range map[string]string{"database": q.Database, "table": q.Table, "column": q.Column} {
		if v != "" {
			if err := ValidateName(what, v); err != nil {
				return err
			}
		}
	}
	return ValidatePrivilege(q.Privilege, known)
}

// Object renders the queried object as db.table(column).
func (q ExplainQuery) Object() string {
	return objectLabel(q.Database, q.Table, q.Column)
}

// Verdict is the answer for one role mode.
type Verdict struct {
	Status  string `json:"status"`
	Summary string `json:"summary"`
	// Only lists the objects allowed when the status is partial and access
	// covers some of them; Except lists those removed from a broader grant.
	Only        []string `json:"only,omitempty"`
	Except      []string `json:"except,omitempty"`
	GrantOption bool     `json:"grant_option"`
	RowFiltered bool     `json:"row_filtered"`
	// Roles are the roles active in this mode.
	Roles []string `json:"roles"`
	// Via lists the paths whose grants allow access.
	Via []string `json:"via,omitempty"`
}

// Evidence is one step of the explanation.
type Evidence struct {
	Step        int    `json:"step"`
	Mode        string `json:"mode"`
	Source      string `json:"source"`
	Statement   string `json:"statement"`
	Effect      string `json:"effect"`
	Detail      string `json:"detail,omitempty"`
	GrantOption bool   `json:"grant_option,omitempty"`
	Local       bool   `json:"local,omitempty"`
}

// Node says which server answered.
type Node struct {
	Host string `json:"host"`
	// MultiNode is true when a cluster of several nodes exists, so what was
	// read here may differ elsewhere.
	MultiNode bool `json:"multi_node"`
	// LocalEntities are involved entities not stored in replicated storage
	// while a cluster exists.
	LocalEntities []string `json:"local_entities"`
}

// RowPolicyOutcome is how row policies shape what a user reads.
type RowPolicyOutcome struct {
	TableHasPolicies bool     `json:"table_has_policies"`
	Permissive       []string `json:"permissive"`
	Restrictive      []string `json:"restrictive"`
	// Filter is the combined condition when it does not depend on server
	// configuration.
	Filter string `json:"filter,omitempty"`
	// NoPermissive is true when the table has policies but no permissive
	// one applies. The outcome then depends on
	// users_without_row_policies_can_read_rows, which cannot be read.
	NoPermissive     bool   `json:"no_permissive"`
	IfSettingOn      string `json:"if_setting_on,omitempty"`
	IfSettingOff     string `json:"if_setting_off,omitempty"`
	FiltersOrHides   bool   `json:"filters"`
	SettingDependent bool   `json:"setting_dependent"`
}

// RowPolicies holds the outcome per role mode. Applicable is false when the
// question is not about SELECT on one table.
type RowPolicies struct {
	Applicable bool             `json:"applicable"`
	Note       string           `json:"note,omitempty"`
	Default    RowPolicyOutcome `json:"default"`
	AllRoles   RowPolicyOutcome `json:"all_roles"`
}

// ProfileElementResult is one settings element that applies, with where it
// came from.
type ProfileElementResult struct {
	Setting     string `json:"setting"`
	Value       string `json:"value,omitempty"`
	Min         string `json:"min,omitempty"`
	Max         string `json:"max,omitempty"`
	Writability string `json:"writability,omitempty"`
	Source      string `json:"source"`
	Local       bool   `json:"local,omitempty"`
}

// ProfileResult is the settings that apply at login.
type ProfileResult struct {
	// Elements are in merge order; a later element wins.
	Elements []ProfileElementResult `json:"elements"`
	Readonly string                 `json:"readonly"`
	AllowDDL bool                   `json:"allow_ddl"`
	Const    []string               `json:"const"`
	Verdicts []string               `json:"verdicts"`
	// Blocks says why the privilege is refused by settings, if it is.
	Blocks string `json:"blocks,omitempty"`
}

// QuotaIntervalResult is one interval of a matching quota.
type QuotaIntervalResult struct {
	Interval string             `json:"interval"`
	Limits   map[string]uint64  `json:"limits"`
	Used     map[string]float64 `json:"used,omitempty"`
	EndTime  string             `json:"end_time,omitempty"`
}

// QuotaResult is a quota that applies to the user.
type QuotaResult struct {
	Name      string                `json:"name"`
	KeyedBy   string                `json:"keyed_by"`
	KeyNote   string                `json:"key_note"`
	Storage   string                `json:"storage"`
	Local     bool                  `json:"local,omitempty"`
	Intervals []QuotaIntervalResult `json:"intervals"`
	// UsageVisible is false when system.quotas_usage could not be read.
	UsageVisible bool `json:"usage_visible"`
}

// Verification is the cross-check against ClickHouse.
type Verification struct {
	// Status is match, mismatch or unavailable.
	Status     string   `json:"status"`
	Statement  string   `json:"statement,omitempty"`
	ClickHouse *Verdict `json:"clickhouse,omitempty"`
	Detail     string   `json:"detail,omitempty"`
}

// Explanation answers one forward question.
type Explanation struct {
	Query   ExplainQuery `json:"query"`
	Node    Node         `json:"node"`
	Storage string       `json:"storage"`
	Verdict struct {
		Default  Verdict `json:"default"`
		AllRoles Verdict `json:"all_roles"`
	} `json:"verdict"`
	Evidence          []Evidence    `json:"evidence"`
	RowPolicies       RowPolicies   `json:"row_policies"`
	Profile           ProfileResult `json:"profile"`
	Quotas            []QuotaResult `json:"quotas"`
	Verification      Verification  `json:"verification"`
	Warnings          []string      `json:"warnings"`
	Incomplete        bool          `json:"incomplete"`
	IncompleteReasons []string      `json:"incomplete_reasons"`

	// grantAll is the all-roles verdict before settings blocks, kept for
	// verification; implicitOnly marks access that rests on the system
	// table rule alone.
	grantAll     Verdict
	implicitOnly bool
}

// WhoCanUser is one row of the reverse question.
type WhoCanUser struct {
	User        string   `json:"user"`
	Storage     string   `json:"storage"`
	Default     Verdict  `json:"default"`
	AllRoles    Verdict  `json:"all_roles"`
	Via         []string `json:"via"`
	RowFiltered bool     `json:"row_filtered"`
	Columns     []string `json:"columns"`
	Local       bool     `json:"local,omitempty"`
}

// WhoCanResult answers the reverse question.
type WhoCanResult struct {
	Query             ExplainQuery `json:"query"`
	Node              Node         `json:"node"`
	Users             []WhoCanUser `json:"users"`
	Incomplete        bool         `json:"incomplete"`
	IncompleteReasons []string     `json:"incomplete_reasons"`
}

// ── Role closure ─────────────────────────────────────────────────────────

// roleSet is the set of roles active in one mode, each with the path that
// enabled it (user first).
type roleSet struct {
	order []string
	paths map[string][]string
}

func (rs roleSet) has(name string) bool { _, ok := rs.paths[name]; return ok }

func (rs roleSet) names() map[string]bool {
	out := make(map[string]bool, len(rs.order))
	for _, r := range rs.order {
		out[r] = true
	}
	return out
}

// closure follows role grants from the starting roles breadth first, so
// every role keeps its shortest path. Cycles stop at the first repeat.
func closure(s *Snapshot, user string, start []string) roleSet {
	children := map[string][]string{}
	for _, g := range s.RoleGrants {
		if g.RoleName != "" {
			children[g.RoleName] = append(children[g.RoleName], g.GrantedRole)
		}
	}
	rs := roleSet{paths: map[string][]string{}}
	queue := []string{}
	for _, r := range start {
		if !rs.has(r) {
			rs.paths[r] = []string{user, r}
			rs.order = append(rs.order, r)
			queue = append(queue, r)
		}
	}
	for len(queue) > 0 {
		r := queue[0]
		queue = queue[1:]
		for _, c := range children[r] {
			if rs.has(c) {
				continue
			}
			p := append(append([]string{}, rs.paths[r]...), c)
			rs.paths[c] = p
			rs.order = append(rs.order, c)
			queue = append(queue, c)
		}
	}
	return rs
}

// userRoles returns the roles active at login (default) and after SET ROLE
// ALL (all granted).
func userRoles(s *Snapshot, user string) (def, all roleSet) {
	var granted, defaults []string
	u := s.user(user)
	except := map[string]bool{}
	listed := map[string]bool{}
	if u != nil {
		for _, r := range u.DefaultExcpt {
			except[r] = true
		}
		for _, r := range u.DefaultRoles {
			listed[r] = true
		}
	}
	for _, g := range s.RoleGrants {
		if g.UserName != user {
			continue
		}
		granted = append(granted, g.GrantedRole)
		isDefault := g.IsDefault
		if u != nil {
			isDefault = (u.DefaultAll && !except[g.GrantedRole]) || (!u.DefaultAll && listed[g.GrantedRole])
		}
		if isDefault {
			defaults = append(defaults, g.GrantedRole)
		}
	}
	return closure(s, user, defaults), closure(s, user, granted)
}

// ── Privilege coverage ───────────────────────────────────────────────────

// privTree answers whether a granted privilege covers an asked one.
type privTree struct{ parents map[string]string }

func normPriv(p string) string {
	p = strings.TrimSpace(p)
	if strings.EqualFold(p, "ALL PRIVILEGES") {
		return "ALL"
	}
	return p
}

// ancestors returns p and its parent groups, nearest first.
func (t privTree) ancestors(p string) []string {
	out := []string{p}
	seen := map[string]bool{p: true}
	for cur := p; ; {
		parent, ok := t.parents[cur]
		if !ok || parent == "" || seen[parent] {
			break
		}
		seen[parent] = true
		out = append(out, parent)
		cur = parent
	}
	return out
}

func (t privTree) covers(granted, asked string) bool {
	g, a := normPriv(granted), normPriv(asked)
	if g == a || g == "ALL" {
		return true
	}
	if len(t.parents) == 0 {
		// Without system.privileges, fall back to the name prefix
		// (ALTER covers ALTER UPDATE).
		return strings.HasPrefix(a, g+" ")
	}
	for _, anc := range t.ancestors(a) {
		if anc == g {
			return true
		}
	}
	return false
}

// topGroup is the ancestor right under ALL (ALTER for ALTER UPDATE).
func (t privTree) topGroup(p string) string {
	p = normPriv(p)
	anc := t.ancestors(p)
	if len(anc) > 1 {
		for i := len(anc) - 1; i >= 0; i-- {
			if anc[i] != "ALL" && anc[i] != "NONE" {
				return anc[i]
			}
		}
	}
	if f := strings.Fields(p); len(f) > 0 {
		return f[0]
	}
	return p
}

var readGroups = map[string]bool{"SELECT": true, "SHOW": true, "DICTGET": true, "INTROSPECTION": true, "SOURCES": true, "TABLE ENGINE": true, "NAMED COLLECTION": true}
var ddlGroups = map[string]bool{"CREATE": true, "ALTER": true, "DROP": true, "TRUNCATE": true, "UNDROP TABLE": true}

func (t privTree) isWrite(p string) bool { return !readGroups[strings.ToUpper(t.topGroup(p))] }
func (t privTree) isDDL(p string) bool   { return ddlGroups[t.topGroup(p)] }

// ── Per grantee evaluation ───────────────────────────────────────────────

type target struct{ db, table, column string }

func (q ExplainQuery) target() target { return target{q.Database, q.Table, q.Column} }

func (t target) level() int {
	switch {
	case t.db == "":
		return 0
	case t.table == "":
		return 1
	case t.column == "":
		return 2
	}
	return 3
}

func rowLevel(g GrantRow) int {
	return target{g.Database, g.Table, g.Column}.level()
}

// tableMatch compares a grant table with the asked table; a trailing * is a
// wildcard grant (SHOW GRANTS prints them that way).
func tableMatch(grant, asked string) bool {
	if strings.HasSuffix(grant, "*") && len(grant) > 1 {
		return strings.HasPrefix(asked, strings.TrimSuffix(grant, "*"))
	}
	return grant == asked
}

// within reports whether the row's object contains the target (at or above)
// or lies inside it (below), comparing only the levels both have.
func within(g GrantRow, t target) bool {
	l, tl := rowLevel(g), t.level()
	n := l
	if tl < n {
		n = tl
	}
	if n >= 1 && !tableMatch(g.Database, t.db) {
		return false
	}
	if n >= 2 && !tableMatch(g.Table, t.table) {
		return false
	}
	if n >= 3 && g.Column != t.column {
		return false
	}
	return true
}

// grantOnlyRevoke is a REVOKE GRANT OPTION FOR row: access stays.
func grantOnlyRevoke(g GrantRow) bool { return g.PartialRevoke && g.GrantOption }

type granteeEval struct {
	status      string // allowed, partial, denied
	only        []string
	except      []string
	grantOption bool
	decidedBy   *GrantRow
}

// belowName names a row more specific than the target, relative to it.
func belowName(g GrantRow, t target) string {
	switch t.level() {
	case 0:
		return objectLabel(g.Database, g.Table, g.Column)
	case 1:
		if g.Column != "" {
			return g.Table + "(" + g.Column + ")"
		}
		return g.Table
	}
	return g.Column
}

// evalGrantee decides access for one grantee: the most specific covering
// row at or above the target wins, then rows below it make it partial.
func evalGrantee(rows []GrantRow, priv string, t target, tree privTree) granteeEval {
	var at [4][]GrantRow
	var below []GrantRow
	for _, g := range rows {
		if !tree.covers(g.Access, priv) || !within(g, t) {
			continue
		}
		if l := rowLevel(g); l <= t.level() {
			at[l] = append(at[l], g)
		} else {
			below = append(below, g)
		}
	}
	ev := granteeEval{status: VerdictDenied}
	decidedAt := -1
	for l := t.level(); l >= 0 && decidedAt < 0; l-- {
		for i := range at[l] {
			g := at[l][i]
			if grantOnlyRevoke(g) {
				continue
			}
			ev.decidedBy, decidedAt = &at[l][i], l
			if g.PartialRevoke {
				ev.status = VerdictDenied
				break
			}
			ev.status = VerdictAllowed
		}
	}
	if ev.status == VerdictAllowed {
		// A REVOKE GRANT OPTION FOR row as specific as the deciding grant
		// removes the option but keeps access.
		ev.grantOption = ev.decidedBy.GrantOption
		for l := t.level(); l >= decidedAt; l-- {
			for _, g := range at[l] {
				if grantOnlyRevoke(g) {
					ev.grantOption = false
				}
			}
		}
	}
	for _, g := range below {
		if grantOnlyRevoke(g) {
			continue
		}
		name := belowName(g, t)
		if ev.status == VerdictAllowed && g.PartialRevoke {
			ev.except = appendUnique(ev.except, name)
		} else if ev.status != VerdictAllowed && !g.PartialRevoke {
			ev.only = appendUnique(ev.only, name)
		}
	}
	if ev.status == VerdictAllowed && len(ev.except) > 0 {
		ev.status = VerdictPartial
	}
	if ev.status != VerdictAllowed && ev.status != VerdictPartial && len(ev.only) > 0 {
		ev.status = VerdictPartial
		ev.except = nil
	}
	return ev
}

// unionEval combines grantees: a revoke in one grantee never cancels a
// grant from another.
func unionEval(evals []granteeEval) granteeEval {
	out := granteeEval{status: VerdictDenied}
	var excepts [][]string
	var only []string
	for _, e := range evals {
		switch {
		case e.status == VerdictAllowed:
			out.status = VerdictAllowed
			out.grantOption = out.grantOption || e.grantOption
		case e.status == VerdictPartial && len(e.except) > 0:
			excepts = append(excepts, e.except)
			out.grantOption = out.grantOption || e.grantOption
		case e.status == VerdictPartial:
			for _, o := range e.only {
				only = appendUnique(only, o)
			}
		}
	}
	if out.status == VerdictAllowed {
		return out
	}
	if len(excepts) > 0 {
		// Removed only where every broad grant removes it and no narrow
		// grant puts it back.
		common := excepts[0]
		for _, ex := range excepts[1:] {
			common = intersect(common, ex)
		}
		var except []string
		for _, c := range common {
			if !contains(only, c) {
				except = append(except, c)
			}
		}
		if len(except) == 0 {
			out.status = VerdictAllowed
			return out
		}
		out.status, out.except = VerdictPartial, except
		return out
	}
	if len(only) > 0 {
		sort.Strings(only)
		out.status, out.only = VerdictPartial, only
	}
	return out
}

// ── Explainer ────────────────────────────────────────────────────────────

// alwaysReadableSystem are system tables any user can read even when
// select_from_system_db_requires_grant is on.
var alwaysReadableSystem = map[string]bool{
	"aggregate_function_combinators": true, "collations": true, "columns": true, "contributors": true, "current_roles": true,
	"data_type_families": true, "database_engines": true, "databases": true, "enabled_roles": true, "formats": true,
	"functions": true, "licenses": true, "one": true, "privileges": true, "quota_usage": true, "settings": true,
	"table_engines": true, "table_functions": true, "tables": true, "time_zones": true,
}

type explainer struct {
	s    *Snapshot
	q    ExplainQuery
	tree privTree
	t    target
	user string
}

func newExplainer(s *Snapshot, q ExplainQuery) *explainer {
	return &explainer{s: s, q: q, tree: privTree{parents: s.PrivilegeParents}, t: q.target(), user: q.User}
}

// grantee is a user or a role with the path that made it active.
type grantee struct {
	target Target
	path   []string
}

func (x *explainer) grantees(rs roleSet) []grantee {
	out := []grantee{{target: Target{Type: "user", Name: x.user}, path: []string{x.user}}}
	for _, r := range rs.order {
		out = append(out, grantee{target: Target{Type: "role", Name: r}, path: rs.paths[r]})
	}
	return out
}

func (x *explainer) rowsOf(t Target) []GrantRow {
	var out []GrantRow
	for _, g := range x.s.Grants {
		if g.Grantee() == t {
			out = append(out, g)
		}
	}
	return out
}

func pathLabel(path []string) string {
	if len(path) == 0 {
		return ""
	}
	parts := []string{"user " + path[0]}
	for _, r := range path[1:] {
		parts = append(parts, "role "+r)
	}
	return strings.Join(parts, " > ")
}

// incompleteReasons lists why the snapshot may not show everything.
func incompleteReasons(s *Snapshot) []string {
	var out []string
	if !s.Capabilities.ShowAccess {
		out = append(out, "This login lacks SHOW ACCESS, so system.grants and system.role_grants only show its own entries.")
	}
	for _, e := range s.Errors {
		for _, sec := range []string{"roles:", "users:", "role_grants:", "grants:", "row_policies:", "quotas:", "settings_profiles:"} {
			if strings.HasPrefix(e, sec) {
				out = append(out, "Could not read "+strings.TrimSuffix(sec, ":")+": "+strings.TrimPrefix(e, sec))
			}
		}
	}
	return out
}

func (x *explainer) local(kind, name string) bool {
	if !x.s.MultiNode {
		return false
	}
	st := x.s.storageOf(kind, name)
	return st != "" && st != "replicated"
}

// grantVerdict computes the grant-level verdict for one role set.
func (x *explainer) grantVerdict(rs roleSet) (Verdict, []grantee) {
	gs := x.grantees(rs)
	evals := make([]granteeEval, len(gs))
	for i, g := range gs {
		evals[i] = evalGrantee(x.rowsOf(g.target), x.q.Privilege, x.t, x.tree)
	}
	u := unionEval(evals)
	v := Verdict{Status: u.status, Only: u.only, Except: u.except, GrantOption: u.grantOption, Roles: append([]string{}, rs.order...)}
	for i, e := range evals {
		if e.status == VerdictAllowed || e.status == VerdictPartial {
			v.Via = append(v.Via, pathLabel(gs[i].path))
		}
	}
	if v.Status == VerdictDenied && x.implicitStatus() != "" {
		v.Status = x.implicitStatus()
	}
	return v, gs
}

// implicitStatus covers system and information_schema tables readable
// without a grant.
func (x *explainer) implicitStatus() string {
	if x.tree.topGroup(x.q.Privilege) != "SELECT" {
		return ""
	}
	switch x.q.Database {
	case "system":
		if alwaysReadableSystem[x.q.Table] {
			return VerdictAllowed
		}
		return VerdictConditional
	case "information_schema", "INFORMATION_SCHEMA":
		return VerdictConditional
	}
	return ""
}

func (x *explainer) implicitDetail() string {
	if x.q.Database == "system" && alwaysReadableSystem[x.q.Table] {
		return "Every user can read this system table without a grant."
	}
	if x.q.Database == "system" {
		return "Readable without a grant only when select_from_system_db_requires_grant is off."
	}
	return "Readable without a grant only when select_from_information_schema_requires_grant is off."
}

// Explain answers the forward question from a snapshot.
func Explain(s *Snapshot, q ExplainQuery) (*Explanation, error) {
	x := newExplainer(s, q)
	e := &Explanation{Query: q, Evidence: []Evidence{}, Warnings: []string{}, Quotas: []QuotaResult{}, IncompleteReasons: incompleteReasons(s)}
	e.Incomplete = len(e.IncompleteReasons) > 0
	e.Node = Node{Host: s.Host, MultiNode: s.MultiNode, LocalEntities: []string{}}
	u := s.user(q.User)
	if u == nil {
		if !e.Incomplete {
			return nil, ErrUnknownUser
		}
		e.IncompleteReasons = append(e.IncompleteReasons, "User "+q.User+" is not visible to this login.")
	} else {
		e.Storage = u.Storage
		if u.Storage == "users_xml" {
			e.Warnings = append(e.Warnings, "User "+q.User+" is defined in users.xml; change it in the server configuration, not with SQL.")
		}
	}
	if x.local(KindUser, q.User) {
		e.Node.LocalEntities = appendUnique(e.Node.LocalEntities, "user "+q.User)
	}

	def, all := userRoles(s, q.User)
	dv, _ := x.grantVerdict(def)
	av, gs := x.grantVerdict(all)
	e.grantAll = av
	e.implicitOnly = len(av.Via) == 0 && x.implicitStatus() != ""

	// Evidence: grant rows of every active grantee.
	for _, g := range gs {
		mode := ModeBoth
		if g.target.Type == "role" && !def.has(g.target.Name) {
			mode = ModeAllRoles
		}
		local := g.target.Type == "role" && x.local(KindRole, g.target.Name)
		if local {
			e.Node.LocalEntities = appendUnique(e.Node.LocalEntities, "role "+g.target.Name)
		}
		for _, row := range x.rowsOf(g.target) {
			if !x.tree.covers(row.Access, q.Privilege) || !within(row, x.t) {
				continue
			}
			ev := Evidence{Mode: mode, Source: pathLabel(g.path), Statement: grantStatement(row), GrantOption: row.GrantOption && !row.PartialRevoke, Local: local}
			below := rowLevel(row) > x.t.level()
			switch {
			case grantOnlyRevoke(row):
				ev.Effect, ev.Detail = EffectLimits, "Removes only the grant option."
			case row.PartialRevoke:
				ev.Effect = EffectRemoves
				ev.Detail = "Partial revoke: subtracts from " + g.target.Type + " " + g.target.Name + " only."
				if below {
					ev.Detail = "Partial revoke on " + belowName(row, x.t) + ", for this " + g.target.Type + " only."
				}
			default:
				ev.Effect = EffectAllows
				if below {
					ev.Detail = "Covers only " + belowName(row, x.t) + "."
				} else if normPriv(row.Access) != normPriv(q.Privilege) {
					ev.Detail = row.Access + " includes " + q.Privilege + "."
				}
				if row.GrantOption {
					ev.Detail = strings.TrimSpace(ev.Detail + " With grant option.")
				}
			}
			e.Evidence = append(e.Evidence, ev)
		}
	}
	if e.implicitOnly {
		e.Evidence = append(e.Evidence, Evidence{Mode: ModeBoth, Source: "server", Statement: "implicit SELECT ON " + q.Object(), Effect: EffectAllows, Detail: x.implicitDetail()})
	}

	// Row policies.
	e.RowPolicies = x.rowPolicies(def, all, e)
	// Profiles: the login set is reported; SET ROLE ALL may add more.
	e.Profile = x.profile(def, e, ModeBoth)
	allProfile := x.profile(all, e, ModeAllRoles)
	// Quotas.
	e.Quotas = x.quotas(def, e)

	e.Verdict.Default = x.finish(dv, e.RowPolicies.Default, e.Profile, e.Incomplete)
	e.Verdict.AllRoles = x.finish(av, e.RowPolicies.AllRoles, allProfile, e.Incomplete)
	for i := range e.Evidence {
		e.Evidence[i].Step = i + 1
	}
	return e, nil
}

// finish applies row policies, settings and incompleteness to a grant
// verdict and writes its summary.
func (x *explainer) finish(v Verdict, rp RowPolicyOutcome, p ProfileResult, incomplete bool) Verdict {
	if (v.Status == VerdictAllowed || v.Status == VerdictPartial) && p.Blocks != "" {
		v.Status = VerdictBlocked
	}
	if v.Status == VerdictDenied && incomplete {
		v.Status = VerdictUnknown
	}
	v.RowFiltered = (v.Status == VerdictAllowed || v.Status == VerdictPartial) && rp.FiltersOrHides
	v.Summary = summarize(v, x, p, rp)
	return v
}

func summarize(v Verdict, x *explainer, p ProfileResult, rp RowPolicyOutcome) string {
	var s string
	switch v.Status {
	case VerdictAllowed:
		s = "Allowed"
	case VerdictPartial:
		if len(v.Only) > 0 {
			s = "Only " + strings.Join(v.Only, ", ")
		} else {
			s = "Allowed except " + strings.Join(v.Except, ", ")
		}
	case VerdictBlocked:
		s = "Granted, but " + p.Blocks
	case VerdictConditional:
		s = "Depends on server settings: " + strings.ToLower(x.implicitDetail()[:1]) + x.implicitDetail()[1:]
	case VerdictUnknown:
		s = "Unknown: this login cannot see every grant"
	default:
		s = "No access"
	}
	if v.RowFiltered {
		if rp.SettingDependent {
			s += "; rows depend on users_without_row_policies_can_read_rows"
		} else {
			s += "; rows filtered by row policies"
		}
	}
	if v.GrantOption && (v.Status == VerdictAllowed || v.Status == VerdictPartial) {
		s += "; with grant option"
	}
	return s
}

// ── Row policies ─────────────────────────────────────────────────────────

func appliesTo(a Assignees, user string, roles map[string]bool) bool {
	in := func(list []string) bool {
		for _, n := range list {
			if n == user || roles[n] {
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

func (x *explainer) rowPolicies(def, all roleSet, e *Explanation) RowPolicies {
	out := RowPolicies{}
	if x.q.Table == "" {
		out.Note = "Pick a table to see row policies."
		return out
	}
	if x.tree.topGroup(x.q.Privilege) != "SELECT" {
		out.Note = "Row policies filter SELECT only."
		return out
	}
	out.Applicable = true
	var onTable []RowPolicy
	for _, p := range x.s.RowPolicies {
		if p.Database == x.q.Database && (p.Table == x.q.Table || p.Table == "" || p.Table == "*") {
			onTable = append(onTable, p)
		}
	}
	out.Default = x.policyOutcome(onTable, def, e, ModeBoth)
	out.AllRoles = x.policyOutcome(onTable, all, e, ModeAllRoles)
	return out
}

func (x *explainer) policyOutcome(onTable []RowPolicy, rs roleSet, e *Explanation, mode string) RowPolicyOutcome {
	o := RowPolicyOutcome{TableHasPolicies: len(onTable) > 0, Permissive: []string{}, Restrictive: []string{}}
	if len(onTable) == 0 {
		return o
	}
	roles := rs.names()
	var perm, restr []string
	for _, p := range onTable {
		if !appliesTo(p.To, x.user, roles) {
			continue
		}
		local := x.s.MultiNode && p.Storage != "replicated"
		if p.Restrictive {
			o.Restrictive = append(o.Restrictive, p.FullName)
			restr = append(restr, "("+p.Using+")")
		} else {
			o.Permissive = append(o.Permissive, p.FullName)
			perm = append(perm, "("+p.Using+")")
		}
		if e != nil {
			if local {
				e.Node.LocalEntities = appendUnique(e.Node.LocalEntities, "row policy "+p.FullName)
			}
			kind := "PERMISSIVE"
			if p.Restrictive {
				kind = "RESTRICTIVE"
			}
			ev := Evidence{Mode: mode, Source: "row policy " + p.FullName, Statement: "USING " + p.Using + " AS " + kind,
				Effect: EffectFilters, Detail: "Applies to " + assigneesText(p.To) + ".", Local: local}
			if !hasEvidenceStatement(e.Evidence, ev) {
				e.Evidence = append(e.Evidence, ev)
			}
		}
	}
	restrictive := strings.Join(restr, " AND ")
	if len(perm) > 0 {
		o.Filter = "(" + strings.Join(perm, " OR ") + ")"
		if restrictive != "" {
			o.Filter += " AND " + restrictive
		}
		o.FiltersOrHides = true
		return o
	}
	o.NoPermissive = true
	o.SettingDependent = true
	o.FiltersOrHides = true
	if restrictive != "" {
		o.IfSettingOn = "Rows matching " + restrictive
	} else {
		o.IfSettingOn = "All rows"
	}
	o.IfSettingOff = "No rows"
	if e != nil && mode == ModeBoth {
		e.Evidence = append(e.Evidence, Evidence{Mode: mode, Source: "server", Statement: "users_without_row_policies_can_read_rows",
			Effect: EffectFilters, Detail: "The table has row policies but none permissive applies. If the setting is on: " + strings.ToLower(o.IfSettingOn[:1]) + o.IfSettingOn[1:] + "; if off: no rows. CH-UI cannot read this setting."})
	}
	return o
}

func hasEvidenceStatement(list []Evidence, ev Evidence) bool {
	for _, e := range list {
		if e.Source == ev.Source && e.Statement == ev.Statement {
			return true
		}
	}
	return false
}

func assigneesText(a Assignees) string {
	if a.All {
		if len(a.Except) > 0 {
			return "ALL EXCEPT " + strings.Join(a.Except, ", ")
		}
		return "ALL"
	}
	if len(a.Names) == 0 {
		return "nobody"
	}
	return strings.Join(a.Names, ", ")
}

// ── Profiles ─────────────────────────────────────────────────────────────

func (x *explainer) profile(rs roleSet, e *Explanation, mode string) ProfileResult {
	out := ProfileResult{Elements: []ProfileElementResult{}, Const: []string{}, Verdicts: []string{}, AllowDDL: true}
	var expand func(name, source string, seen map[string]bool)
	add := func(ps ProfileSetting, source string, local bool) {
		if ps.Name == "" {
			return
		}
		el := ProfileElementResult{Setting: ps.Name, Writability: ps.Writability, Source: source, Local: local}
		if ps.Value != nil {
			el.Value = *ps.Value
		}
		if ps.Min != nil {
			el.Min = *ps.Min
		}
		if ps.Max != nil {
			el.Max = *ps.Max
		}
		out.Elements = append(out.Elements, el)
	}
	expand = func(name, source string, seen map[string]bool) {
		if seen[name] {
			return
		}
		seen[name] = true
		p := x.s.profile(name)
		if p == nil {
			return
		}
		local := x.local(KindSettingsProfile, name)
		if local && e != nil {
			e.Node.LocalEntities = appendUnique(e.Node.LocalEntities, "settings profile "+name)
		}
		for _, inh := range p.Inherit {
			expand(inh, source+" > profile "+inh, seen)
		}
		for _, ps := range p.Settings {
			add(ps, source, local)
		}
	}
	roles := rs.names()
	if x.s.profile("default") != nil {
		expand("default", "profile default (server default)", map[string]bool{})
	}
	for _, p := range x.s.SettingsProfiles {
		if appliesTo(p.To, x.user, roles) {
			expand(p.Name, "profile "+p.Name+" (TO "+assigneesText(p.To)+")", map[string]bool{})
		}
	}
	elements := func(list []ProfileElement, source string, local bool) {
		for _, el := range list {
			if el.Inherit != "" {
				expand(el.Inherit, source+" > profile "+el.Inherit, map[string]bool{})
				continue
			}
			add(el.ProfileSetting, source, local)
		}
	}
	for _, r := range rs.order {
		elements(x.s.RoleSettings[r], pathLabel(rs.paths[r]), x.local(KindRole, r))
	}
	elements(x.s.UserSettings[x.user], "user "+x.user, x.local(KindUser, x.user))

	// Last value wins; constraints too.
	values := map[string]string{}
	writ := map[string]string{}
	for _, el := range out.Elements {
		if el.Value != "" {
			values[el.Setting] = el.Value
		}
		if el.Writability != "" {
			writ[el.Setting] = el.Writability
		}
	}
	out.Readonly = values["readonly"]
	if out.Readonly == "" {
		out.Readonly = "0"
	}
	if v, ok := values["allow_ddl"]; ok && (v == "0" || strings.EqualFold(v, "false")) {
		out.AllowDDL = false
	}
	for name, w := range writ {
		if w == "CONST" {
			out.Const = append(out.Const, name)
		}
	}
	sort.Strings(out.Const)
	ro, _ := strconv.Atoi(out.Readonly)
	switch {
	case ro == 1:
		out.Verdicts = append(out.Verdicts, "readonly = 1: only reads; writes, DDL and SET are refused.")
	case ro >= 2:
		out.Verdicts = append(out.Verdicts, "readonly = 2: only reads; settings may still change.")
	}
	if !out.AllowDDL {
		out.Verdicts = append(out.Verdicts, "allow_ddl = 0: CREATE, ALTER, DROP and TRUNCATE are refused.")
	}
	if len(out.Const) > 0 {
		out.Verdicts = append(out.Verdicts, "Fixed (CONST) settings: "+strings.Join(out.Const, ", ")+".")
	}
	if ro >= 1 && x.tree.isWrite(x.q.Privilege) {
		out.Blocks = "readonly = " + out.Readonly + " refuses " + x.q.Privilege
	} else if !out.AllowDDL && x.tree.isDDL(x.q.Privilege) {
		out.Blocks = "allow_ddl = 0 refuses " + x.q.Privilege
	}
	if e != nil {
		for _, el := range out.Elements {
			relevant := el.Setting == "readonly" || el.Setting == "allow_ddl" || el.Writability == "CONST"
			if !relevant {
				continue
			}
			effect := EffectLimits
			if out.Blocks != "" && (el.Setting == "readonly" || el.Setting == "allow_ddl") {
				effect = EffectBlocks
			}
			stmt := el.Setting
			if el.Value != "" {
				stmt += " = " + el.Value
			}
			if el.Writability != "" {
				stmt += " " + el.Writability
			}
			ev := Evidence{Mode: mode, Source: el.Source, Statement: "SETTINGS " + stmt, Effect: effect, Local: el.Local}
			if effect == EffectBlocks {
				ev.Detail = out.Blocks + "."
			}
			if !hasEvidenceStatement(e.Evidence, ev) {
				e.Evidence = append(e.Evidence, ev)
			}
		}
	}
	return out
}

// ── Quotas ───────────────────────────────────────────────────────────────

func (x *explainer) quotas(rs roleSet, e *Explanation) []QuotaResult {
	out := []QuotaResult{}
	roles := rs.names()
	for _, q := range x.s.Quotas {
		if !appliesTo(q.To, x.user, roles) {
			continue
		}
		r := QuotaResult{Name: q.Name, KeyedBy: q.KeyedBy, Storage: q.Storage, Local: x.local(KindQuota, q.Name), Intervals: []QuotaIntervalResult{}, UsageVisible: x.s.QuotaUsage != nil}
		key := ""
		switch {
		case q.KeyedBy == "":
			r.KeyNote = "Not keyed: every user it applies to shares one counter."
		case strings.HasPrefix(q.KeyedBy, "user_name") || q.KeyedBy == "client_key,user_name":
			r.KeyNote = "Counted per user."
			key = x.user
		default:
			r.KeyNote = "Counted per " + strings.ReplaceAll(q.KeyedBy, ",", " or ") + ", so usage is not tied to this user."
			key = "\x00"
		}
		for _, iv := range q.Intervals {
			ir := QuotaIntervalResult{Interval: fmt.Sprintf("%d %s", iv.Duration, iv.Unit), Limits: iv.Limits}
			secs := iv.Duration * unitSeconds(iv.Unit)
			for _, u := range x.s.QuotaUsage {
				if u.Quota == q.Name && u.Key == key && u.Duration == secs {
					ir.Used, ir.EndTime = u.Used, u.EndTime
				}
			}
			r.Intervals = append(r.Intervals, ir)
		}
		if r.Local {
			e.Node.LocalEntities = appendUnique(e.Node.LocalEntities, "quota "+q.Name)
		}
		var lim []string
		for _, ir := range r.Intervals {
			keys := make([]string, 0, len(ir.Limits))
			for k := range ir.Limits {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			parts := []string{}
			for _, k := range keys {
				if used, ok := ir.Used[k]; ok {
					parts = append(parts, fmt.Sprintf("%s %s of %d", k, strconv.FormatFloat(used, 'f', -1, 64), ir.Limits[k]))
				} else {
					parts = append(parts, fmt.Sprintf("%s = %d", k, ir.Limits[k]))
				}
			}
			if len(parts) == 0 {
				parts = []string{"tracking only"}
			}
			lim = append(lim, ir.Interval+": "+strings.Join(parts, ", "))
		}
		note := r.KeyNote
		if !r.UsageVisible {
			note += " Usage is not visible to this login."
		}
		e.Evidence = append(e.Evidence, Evidence{Mode: ModeBoth, Source: "quota " + q.Name, Statement: strings.Join(lim, "; "), Effect: EffectLimits, Detail: note, Local: r.Local})
		out = append(out, r)
	}
	if len(out) > 1 {
		e.Warnings = append(e.Warnings, "Several quotas match; ClickHouse tracks the user against one of them.")
	}
	return out
}

// ── Reverse question ─────────────────────────────────────────────────────

// WhoCan lists every visible user that can use the privilege on the object
// at login or after SET ROLE ALL.
func WhoCan(s *Snapshot, q ExplainQuery) *WhoCanResult {
	out := &WhoCanResult{Query: q, Users: []WhoCanUser{}, IncompleteReasons: incompleteReasons(s)}
	out.Incomplete = len(out.IncompleteReasons) > 0
	out.Node = Node{Host: s.Host, MultiNode: s.MultiNode, LocalEntities: []string{}}
	for _, u := range s.Users {
		uq := q
		uq.User = u.Name
		e, err := Explain(s, uq)
		if err != nil {
			continue
		}
		d, a := e.Verdict.Default, e.Verdict.AllRoles
		has := func(v Verdict) bool {
			return v.Status == VerdictAllowed || v.Status == VerdictPartial || v.Status == VerdictBlocked || v.Status == VerdictConditional
		}
		if !has(d) && !has(a) {
			continue
		}
		// Conditional system access applies to everyone; list only users
		// who hold a grant for it.
		if d.Status == VerdictConditional && a.Status == VerdictConditional {
			continue
		}
		row := WhoCanUser{User: u.Name, Storage: u.Storage, Default: d, AllRoles: a, Via: a.Via, RowFiltered: d.RowFiltered || a.RowFiltered, Columns: []string{}, Local: len(e.Node.LocalEntities) > 0}
		if row.Via == nil {
			row.Via = []string{}
		}
		if q.Table != "" && q.Column == "" {
			v := a
			if len(v.Only) > 0 {
				row.Columns = v.Only
			} else if len(v.Except) > 0 {
				for _, c := range v.Except {
					row.Columns = append(row.Columns, "not "+c)
				}
			}
		}
		out.Users = append(out.Users, row)
		for _, l := range e.Node.LocalEntities {
			out.Node.LocalEntities = appendUnique(out.Node.LocalEntities, l)
		}
	}
	sort.Slice(out.Users, func(i, j int) bool { return out.Users[i].User < out.Users[j].User })
	return out
}

// ── Verification against SHOW GRANTS ... FINAL ───────────────────────────

// ParseGrantLines turns SHOW GRANTS output into grant rows. Role grants
// (no ON clause) and objects that are not *.*, db.* or db.table are
// skipped.
func ParseGrantLines(lines []string) []GrantRow {
	var out []GrantRow
	for _, line := range lines {
		l := strings.TrimSpace(line)
		revoke, optionOnly := false, false
		switch {
		case strings.HasPrefix(l, "REVOKE GRANT OPTION FOR "):
			revoke, optionOnly = true, true
			l = l[len("REVOKE GRANT OPTION FOR "):]
		case strings.HasPrefix(l, "REVOKE "):
			revoke = true
			l = l[len("REVOKE "):]
		case strings.HasPrefix(l, "GRANT "):
			l = l[len("GRANT "):]
		default:
			continue
		}
		on := topLevelIndex(l, " ON ")
		if on < 0 {
			continue
		}
		privs, rest := l[:on], l[on+4:]
		db, table, rest, ok := parseObject(rest)
		if !ok {
			continue
		}
		withOption := strings.HasSuffix(rest, " WITH GRANT OPTION")
		for _, p := range splitTopLevel(privs) {
			name, cols := p, []string(nil)
			if i := strings.Index(p, "("); i > 0 && strings.HasSuffix(p, ")") {
				name = strings.TrimSpace(p[:i])
				for _, c := range splitTopLevel(p[i+1 : len(p)-1]) {
					cols = append(cols, unquoteIdent(c))
				}
			}
			base := GrantRow{UserName: "_", Access: normPriv(name), Database: db, Table: table, PartialRevoke: revoke, GrantOption: withOption || optionOnly}
			if len(cols) == 0 {
				out = append(out, base)
				continue
			}
			for _, c := range cols {
				r := base
				r.Column = c
				out = append(out, r)
			}
		}
	}
	return out
}

// topLevelIndex finds sep outside backticks and parentheses.
func topLevelIndex(s, sep string) int {
	depth, quoted := 0, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case quoted:
			if c == '\\' {
				i++
			} else if c == '`' {
				quoted = false
			}
		case c == '`':
			quoted = true
		case c == '(':
			depth++
		case c == ')':
			depth--
		case depth == 0 && strings.HasPrefix(s[i:], sep):
			return i
		}
	}
	return -1
}

func splitTopLevel(s string) []string {
	var out []string
	for {
		i := topLevelIndex(s, ",")
		if i < 0 {
			if t := strings.TrimSpace(s); t != "" {
				out = append(out, t)
			}
			return out
		}
		out = append(out, strings.TrimSpace(s[:i]))
		s = s[i+1:]
	}
}

// readIdent reads one identifier, backticked or bare, from the start of s.
func readIdent(s string) (string, string) {
	if strings.HasPrefix(s, "`") {
		var b strings.Builder
		for i := 1; i < len(s); i++ {
			c := s[i]
			if c == '\\' && i+1 < len(s) {
				b.WriteByte(s[i+1])
				i++
				continue
			}
			if c == '`' {
				if i+1 < len(s) && s[i+1] == '`' {
					b.WriteByte('`')
					i++
					continue
				}
				return b.String(), s[i+1:]
			}
			b.WriteByte(c)
		}
		return b.String(), ""
	}
	i := 0
	for i < len(s) && s[i] != '.' && s[i] != ' ' {
		i++
	}
	return s[:i], s[i:]
}

func unquoteIdent(s string) string {
	id, _ := readIdent(strings.TrimSpace(s))
	return id
}

// parseObject reads *, *.*, db.*, db.table (optionally ending in a
// wildcard) and returns what follows.
func parseObject(s string) (db, table, rest string, ok bool) {
	first, after := readIdent(s)
	if !strings.HasPrefix(after, ".") {
		if first == "*" {
			return "", "", after, true
		}
		return "", "", "", false
	}
	second, after := readIdent(after[1:])
	if first == "*" {
		return "", "", after, true
	}
	if second == "*" {
		return first, "", after, true
	}
	return first, second, after, true
}

// Verify compares the all-roles verdict with SHOW GRANTS ... FINAL output,
// which merges every granted role. implicit says the output included WITH
// IMPLICIT grants. On disagreement ClickHouse wins.
func Verify(e *Explanation, s *Snapshot, statement string, lines []string, implicit bool) {
	e.Verification.Statement = statement
	rows := ParseGrantLines(lines)
	tree := privTree{parents: s.PrivilegeParents}
	ev := evalGrantee(rows, e.Query.Privilege, e.Query.target(), tree)
	ch := Verdict{Status: ev.status, Only: ev.only, Except: ev.except, GrantOption: ev.grantOption}
	computed := e.grantAll
	if e.implicitOnly {
		if !implicit {
			e.Verification.Status = "match"
			e.Verification.Detail = "This server does not support WITH IMPLICIT, so the system table rule stays unresolved."
			return
		}
		// ClickHouse resolves the server setting: its implicit grants show
		// whether the table is readable without a grant.
		ch.Summary = summaryAfterVerify(ch.Status)
		for _, v := range []*Verdict{&e.Verdict.Default, &e.Verdict.AllRoles} {
			if v.Status == VerdictConditional || v.Status == VerdictAllowed || v.Status == VerdictUnknown {
				v.Status, v.Summary = ch.Status, ch.Summary
			}
		}
		e.Verification.Status = "match"
		e.Verification.ClickHouse = &ch
		e.Verification.Detail = "ClickHouse resolved the system table rule from its implicit grants."
		return
	}
	ch.Summary = summaryAfterVerify(ch.Status)
	e.Verification.ClickHouse = &ch
	if sameGrantVerdict(computed, ch) {
		e.Verification.Status = "match"
		return
	}
	e.Verification.Status = "mismatch"
	e.Warnings = append(e.Warnings, fmt.Sprintf("%s says %s, the explanation says %s. ClickHouse wins: trust %s.",
		statement, verdictWords(ch), verdictWords(computed), verdictWords(ch)))
	v := &e.Verdict.AllRoles
	if v.Status != VerdictBlocked {
		v.Status, v.Only, v.Except = ch.Status, ch.Only, ch.Except
		v.Summary = summaryAfterVerify(ch.Status) + " (per ClickHouse)"
	}
}

func summaryAfterVerify(status string) string {
	switch status {
	case VerdictAllowed:
		return "Allowed"
	case VerdictPartial:
		return "Partly allowed"
	}
	return "No access"
}

func verdictWords(v Verdict) string {
	switch v.Status {
	case VerdictPartial:
		if len(v.Only) > 0 {
			return "only " + strings.Join(v.Only, ", ")
		}
		return "allowed except " + strings.Join(v.Except, ", ")
	case VerdictAllowed:
		return "allowed"
	}
	return "no access"
}

func sameGrantVerdict(a, b Verdict) bool {
	if a.Status != b.Status {
		return false
	}
	return sameSet(a.Only, b.Only) && sameSet(a.Except, b.Except)
}

// VerificationUnavailable records that SHOW GRANTS ... FINAL could not run.
func VerificationUnavailable(e *Explanation, statement string, err error) {
	e.Verification = Verification{Status: "unavailable", Statement: statement}
	if err != nil {
		e.Verification.Detail = err.Error()
	}
}

// ── Helpers ──────────────────────────────────────────────────────────────

func grantStatement(g GrantRow) string {
	access := g.Access
	if g.Column != "" {
		access += "(" + quoteIdent(g.Column) + ")"
	}
	who := quoteIdent(g.Grantee().Name)
	obj := grantObject(g.Database, g.Table)
	switch {
	case grantOnlyRevoke(g):
		return "REVOKE GRANT OPTION FOR " + access + " ON " + obj + " FROM " + who
	case g.PartialRevoke:
		return "REVOKE " + access + " ON " + obj + " FROM " + who
	case g.GrantOption:
		return "GRANT " + access + " ON " + obj + " TO " + who + " WITH GRANT OPTION"
	}
	return "GRANT " + access + " ON " + obj + " TO " + who
}

func objectLabel(db, table, column string) string {
	switch {
	case db == "":
		return "*.*"
	case table == "":
		return db + ".*"
	case column == "":
		return db + "." + table
	}
	return db + "." + table + "(" + column + ")"
}

func appendUnique(list []string, v string) []string {
	for _, x := range list {
		if x == v {
			return list
		}
	}
	return append(list, v)
}

func intersect(a, b []string) []string {
	var out []string
	for _, x := range a {
		if contains(b, x) {
			out = append(out, x)
		}
	}
	return out
}

func sameSet(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for _, x := range a {
		if !contains(b, x) {
			return false
		}
	}
	return true
}

// ── Live entry points ────────────────────────────────────────────────────

func knownPrivileges(s *Snapshot) map[string]bool {
	if len(s.Privileges) == 0 {
		return nil
	}
	known := make(map[string]bool, len(s.Privileges))
	for _, p := range s.Privileges {
		known[p.Name] = true
	}
	return known
}

// ExplainLive reads the snapshot, explains the question and cross-checks
// the result with SHOW GRANTS FOR user WITH IMPLICIT FINAL (falling back to
// plain FINAL on servers without IMPLICIT). It runs reads only.
func ExplainLive(ctx context.Context, ex Executor, q ExplainQuery) (*Explanation, error) {
	if err := q.Validate(true, nil); err != nil {
		return nil, &ValidationError{Err: err}
	}
	snap, err := ReadSnapshot(ctx, ex)
	if err != nil {
		return nil, err
	}
	if err := q.Validate(true, knownPrivileges(snap)); err != nil {
		return nil, &ValidationError{Err: err}
	}
	e, err := Explain(snap, q)
	if err != nil {
		return nil, err
	}
	base := "SHOW GRANTS FOR " + quoteIdent(q.User)
	stmt := base + " WITH IMPLICIT FINAL"
	rows, err := ex.Query(ctx, stmt, nil)
	implicit := true
	if err != nil {
		implicit = false
		stmt = base + " FINAL"
		rows, err = ex.Query(ctx, stmt, nil)
	}
	if err != nil {
		VerificationUnavailable(e, stmt, err)
		return e, nil
	}
	Verify(e, snap, stmt, firstColumn(rows), implicit)
	return e, nil
}

// WhoCanLive reads the snapshot and answers the reverse question.
func WhoCanLive(ctx context.Context, ex Executor, q ExplainQuery) (*WhoCanResult, error) {
	q.User = ""
	if err := q.Validate(false, nil); err != nil {
		return nil, &ValidationError{Err: err}
	}
	snap, err := ReadSnapshot(ctx, ex)
	if err != nil {
		return nil, err
	}
	if err := q.Validate(false, knownPrivileges(snap)); err != nil {
		return nil, &ValidationError{Err: err}
	}
	return WhoCan(snap, q), nil
}
