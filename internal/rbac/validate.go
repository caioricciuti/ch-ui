// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package rbac

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"
)

// QuotaResources are the limit names a quota interval accepts, in the order
// ClickHouse prints them.
var QuotaResources = []string{
	"queries", "query_selects", "query_inserts", "errors", "result_rows", "result_bytes",
	"read_rows", "read_bytes", "execution_time", "written_bytes", "failed_sequential_authentications",
}

// QuotaKeys are the KEYED BY values accepted; "" means NOT KEYED.
var QuotaKeys = []string{"", "user_name", "ip_address", "forwarded_ip_address", "client_key", "client_key,user_name", "client_key,ip_address"}

// QuotaUnits are the interval units accepted.
var QuotaUnits = []string{"second", "minute", "hour", "day", "week", "month", "quarter", "year"}

// Writabilities are the setting constraint modes accepted; "" means none.
var Writabilities = []string{"", "CONST", "WRITABLE", "CHANGEABLE_IN_READONLY"}

var (
	privilegeRe = regexp.MustCompile(`^[A-Z][A-Z ]*$`)
	settingRe   = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
	numericRe   = regexp.MustCompile(`^-?[0-9]+(\.[0-9]+)?$`)
)

// Catalog holds the live lists a plan is validated against.
type Catalog struct {
	// Privileges is the set of system.privileges names.
	Privileges map[string]bool
	// Settings is the set of known setting names (only those asked for).
	Settings map[string]bool
	// PolicyErrors maps "db.table|expr" to the EXPLAIN SYNTAX error, when
	// the expression did not parse against the table.
	PolicyErrors map[string]string
}

// PolicyKey is the Catalog.PolicyErrors key for a row policy expression.
func PolicyKey(database, table, expr string) string {
	return database + "\x00" + table + "\x00" + expr
}

const maxNameLen = 256

// ValidateName checks an entity, database, table or column name. Any
// printable text is allowed, since identifiers are always backtick quoted;
// control characters and empty names are not.
func ValidateName(what, name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("%s name is required", what)
	}
	if len(name) > maxNameLen {
		return fmt.Errorf("%s name is longer than %d bytes", what, maxNameLen)
	}
	if !utf8.ValidString(name) {
		return fmt.Errorf("%s name is not valid UTF-8", what)
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return fmt.Errorf("%s name contains a control character", what)
		}
	}
	return nil
}

// ValidatePrivilege checks a privilege name against the pattern and, when
// known, the live system.privileges list.
func ValidatePrivilege(p string, known map[string]bool) error {
	if !privilegeRe.MatchString(p) || strings.Contains(p, "  ") || strings.HasSuffix(p, " ") {
		return fmt.Errorf("privilege %q is not a valid privilege name", p)
	}
	if known != nil && !known[p] {
		return fmt.Errorf("privilege %q is not known to this ClickHouse server", p)
	}
	return nil
}

// ValidateSettingName checks a setting name against the pattern and the live
// system.settings list.
func ValidateSettingName(name string, known map[string]bool) error {
	if !settingRe.MatchString(name) {
		return fmt.Errorf("setting %q is not a valid setting name", name)
	}
	if known != nil && !known[name] {
		return fmt.Errorf("setting %q is not known to this ClickHouse server", name)
	}
	return nil
}

// ValidateRowPolicyExpr checks a USING expression with a small lexer. It
// rejects statement separators, comments, unbalanced quotes and unbalanced
// parentheses so the expression cannot break out of USING ( ... ). The
// server then parses it for real with EXPLAIN SYNTAX against the table.
func ValidateRowPolicyExpr(expr string) error {
	if strings.TrimSpace(expr) == "" {
		return fmt.Errorf("row policy condition is required")
	}
	if len(expr) > 4096 {
		return fmt.Errorf("row policy condition is longer than 4096 bytes")
	}
	if !utf8.ValidString(expr) {
		return fmt.Errorf("row policy condition is not valid UTF-8")
	}
	depth := 0
	for i := 0; i < len(expr); i++ {
		c := expr[i]
		switch {
		case c == 0:
			return fmt.Errorf("row policy condition contains a NUL byte")
		case c == ';':
			return fmt.Errorf("row policy condition may not contain ';'")
		case c == '#':
			return fmt.Errorf("row policy condition may not contain '#'")
		case c == '-' && i+1 < len(expr) && expr[i+1] == '-':
			return fmt.Errorf("row policy condition may not contain comments")
		case c == '/' && i+1 < len(expr) && expr[i+1] == '*':
			return fmt.Errorf("row policy condition may not contain comments")
		case c == '*' && i+1 < len(expr) && expr[i+1] == '/':
			return fmt.Errorf("row policy condition may not contain comments")
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth < 0 {
				return fmt.Errorf("row policy condition has an unbalanced ')'")
			}
		case c == '\'' || c == '"' || c == '`':
			end, err := skipQuoted(expr, i)
			if err != nil {
				return err
			}
			i = end
		}
	}
	if depth != 0 {
		return fmt.Errorf("row policy condition has an unbalanced '('")
	}
	return nil
}

// skipQuoted returns the index of the closing quote of the literal that
// starts at i. Backslash escapes and doubled quotes are honored.
func skipQuoted(s string, i int) (int, error) {
	q := s[i]
	for j := i + 1; j < len(s); j++ {
		switch s[j] {
		case '\\':
			j++
		case q:
			if j+1 < len(s) && s[j+1] == q {
				j++
				continue
			}
			return j, nil
		}
	}
	return 0, fmt.Errorf("row policy condition has an unbalanced quote")
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

func validateAssignees(a Assignees) error {
	for _, n := range append(append([]string{}, a.Names...), a.Except...) {
		if err := ValidateName("assignee", n); err != nil {
			return err
		}
	}
	if !a.All && len(a.Except) > 0 {
		return fmt.Errorf("EXCEPT needs TO ALL")
	}
	return nil
}

func validateTarget(t *Target) error {
	if t == nil {
		return fmt.Errorf("target is required")
	}
	if t.Type != "role" && t.Type != "user" {
		return fmt.Errorf("target type must be role or user")
	}
	return ValidateName(t.Type, t.Name)
}

// validateOp checks one op in isolation, plus the live catalog.
func validateOp(op Op, cat Catalog) error {
	switch op.Kind {
	case OpCreateRole:
		if err := ValidateName("role", op.Name); err != nil {
			return err
		}
		if op.Profile != "" {
			return ValidateName("settings profile", op.Profile)
		}
	case OpDropRole:
		return ValidateName("role", op.Name)
	case OpSetRoleProfile:
		if err := ValidateName("role", op.Name); err != nil {
			return err
		}
		if op.Profile != "" {
			return ValidateName("settings profile", op.Profile)
		}
	case OpGrantPrivilege, OpRevokePrivilege:
		if err := validateTarget(op.Target); err != nil {
			return err
		}
		if len(op.Access) == 0 {
			return fmt.Errorf("at least one privilege is required")
		}
		for _, p := range op.Access {
			if err := ValidatePrivilege(p, cat.Privileges); err != nil {
				return err
			}
		}
		if op.Database != "" && op.Database != "*" {
			if err := ValidateName("database", op.Database); err != nil {
				return err
			}
		}
		if op.Table != "" && op.Table != "*" {
			if op.Database == "" || op.Database == "*" {
				return fmt.Errorf("a table needs a database")
			}
			if err := ValidateName("table", op.Table); err != nil {
				return err
			}
		}
		if len(op.Columns) > 0 {
			if op.Table == "" || op.Table == "*" {
				return fmt.Errorf("columns need a table")
			}
			for _, c := range op.Columns {
				if err := ValidateName("column", c); err != nil {
					return err
				}
			}
		}
	case OpGrantRole, OpRevokeRole:
		if err := validateTarget(op.Target); err != nil {
			return err
		}
		if len(op.Roles) == 0 {
			return fmt.Errorf("at least one role is required")
		}
		for _, r := range op.Roles {
			if err := ValidateName("role", r); err != nil {
				return err
			}
			if op.Target.Type == "role" && r == op.Target.Name {
				return fmt.Errorf("a role cannot be granted to itself")
			}
		}
	case OpSetDefaultRoles:
		if err := validateTarget(op.Target); err != nil {
			return err
		}
		if op.Target.Type != "user" {
			return fmt.Errorf("default roles apply to users only")
		}
		for _, r := range op.Roles {
			if err := ValidateName("role", r); err != nil {
				return err
			}
		}
		if op.DefaultAll && len(op.Roles) > 0 {
			return fmt.Errorf("choose ALL or a list of default roles, not both")
		}
	case OpCreateRowPolicy, OpAlterRowPolicy, OpDropRowPolicy:
		p := op.RowPolicy
		if p == nil {
			return fmt.Errorf("row_policy is required")
		}
		if err := ValidateName("row policy", p.Name); err != nil {
			return err
		}
		if err := ValidateName("database", p.Database); err != nil {
			return err
		}
		if err := ValidateName("table", p.Table); err != nil {
			return err
		}
		if op.Kind == OpDropRowPolicy {
			return nil
		}
		if err := ValidateRowPolicyExpr(p.Using); err != nil {
			return err
		}
		if msg, bad := cat.PolicyErrors[PolicyKey(p.Database, p.Table, p.Using)]; bad {
			return fmt.Errorf("row policy condition does not parse against %s.%s: %s", p.Database, p.Table, msg)
		}
		return validateAssignees(p.To)
	case OpCreateQuota, OpAlterQuota:
		q := op.Quota
		if q == nil {
			return fmt.Errorf("quota is required")
		}
		if err := ValidateName("quota", q.Name); err != nil {
			return err
		}
		if !contains(QuotaKeys, q.KeyedBy) {
			return fmt.Errorf("quota key %q is not supported", q.KeyedBy)
		}
		seen := map[uint64]bool{}
		for _, iv := range q.Intervals {
			if !contains(QuotaUnits, iv.Unit) {
				return fmt.Errorf("interval unit %q is not supported", iv.Unit)
			}
			if iv.Duration == 0 || iv.Duration > 100000 {
				return fmt.Errorf("interval length must be between 1 and 100000")
			}
			secs := iv.Duration * unitSeconds(iv.Unit)
			if seen[secs] {
				return fmt.Errorf("two intervals have the same length")
			}
			seen[secs] = true
			for k := range iv.Limits {
				if !contains(QuotaResources, k) {
					return fmt.Errorf("quota limit %q is not supported", k)
				}
			}
		}
		return validateAssignees(q.To)
	case OpDropQuota:
		return ValidateName("quota", op.Name)
	case OpCreateSettingsProfile, OpAlterSettingsProfile:
		p := op.SettingsProfile
		if p == nil {
			return fmt.Errorf("settings_profile is required")
		}
		if err := ValidateName("settings profile", p.Name); err != nil {
			return err
		}
		seen := map[string]bool{}
		for _, s := range p.Settings {
			if err := ValidateSettingName(s.Name, cat.Settings); err != nil {
				return err
			}
			if seen[s.Name] {
				return fmt.Errorf("setting %q appears twice", s.Name)
			}
			seen[s.Name] = true
			if !contains(Writabilities, s.Writability) {
				return fmt.Errorf("setting mode %q is not supported", s.Writability)
			}
			if s.Value == nil && s.Min == nil && s.Max == nil && s.Writability == "" {
				return fmt.Errorf("setting %q needs a value, a bound or a mode", s.Name)
			}
			for _, v := range []*string{s.Value, s.Min, s.Max} {
				if v != nil && (len(*v) > 1024 || strings.ContainsRune(*v, 0)) {
					return fmt.Errorf("setting %q has an invalid value", s.Name)
				}
			}
		}
		for _, parent := range p.Inherit {
			if err := ValidateName("inherited profile", parent); err != nil {
				return err
			}
			if parent == p.Name {
				return fmt.Errorf("a profile cannot inherit itself")
			}
		}
		return validateAssignees(p.To)
	case OpDropSettingsProfile:
		return ValidateName("settings profile", op.Name)
	default:
		return fmt.Errorf("unknown operation %q", op.Kind)
	}
	return nil
}

// SettingNames returns every setting name the ops reference, for the
// system.settings lookup.
func SettingNames(ops []Op) []string {
	var out []string
	seen := map[string]bool{}
	for _, op := range ops {
		if op.SettingsProfile == nil {
			continue
		}
		for _, s := range op.SettingsProfile.Settings {
			if !seen[s.Name] && settingRe.MatchString(s.Name) {
				seen[s.Name] = true
				out = append(out, s.Name)
			}
		}
	}
	return out
}

// PolicyChecks returns the (database, table, expr) triples to check with
// EXPLAIN SYNTAX.
func PolicyChecks(ops []Op) [][3]string {
	var out [][3]string
	for _, op := range ops {
		if (op.Kind == OpCreateRowPolicy || op.Kind == OpAlterRowPolicy) && op.RowPolicy != nil {
			p := op.RowPolicy
			if ValidateRowPolicyExpr(p.Using) == nil && ValidateName("database", p.Database) == nil && ValidateName("table", p.Table) == nil {
				out = append(out, [3]string{p.Database, p.Table, p.Using})
			}
		}
	}
	return out
}

// PolicyCheckSQL is the statement that parses a USING expression against
// its table without reading data.
func PolicyCheckSQL(database, table, expr string) string {
	return "EXPLAIN SYNTAX SELECT count() FROM " + quoteIdent(database) + "." + quoteIdent(table) + " WHERE (" + expr + ")"
}
