// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package governance

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ── Store interface for policy evaluation ───────────────────────────────────
// Store is expected to be defined elsewhere in the package (e.g., store.go).
// For now we declare a minimal interface so the policy engine compiles
// independently.  Replace with the concrete type once the store is wired up.

// PolicyStore is the interface the policy engine needs from the governance
// data store.
type PolicyStore interface {
	GetAccessMatrixForUser(connectionID, userName string) ([]AccessMatrixEntry, error)
}

// ── Public API ──────────────────────────────────────────────────────────────

// EvaluatePolicies checks a query log entry against all provided policies and
// returns any violations.  It uses the store to look up the user's roles in
// the access matrix.
func EvaluatePolicies(connectionID string, entry QueryLogEntry, policies []Policy, store PolicyStore) []PolicyViolation {
	// Parse the tables_used JSON field.
	tablesUsed := parseTablesUsed(entry.TablesUsed)

	// Retrieve the user's roles / privileges from the access matrix.
	matrixEntries, err := store.GetAccessMatrixForUser(connectionID, entry.User)
	if err != nil {
		// If we can't resolve roles we can't evaluate — return empty.
		return nil
	}

	userRoles := collectUserRoles(matrixEntries)

	now := time.Now().UTC().Format(time.RFC3339)

	var violations []PolicyViolation
	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}
		if normalizePolicyEnforcementMode(policy.EnforcementMode) == "block" {
			continue
		}

		// Check whether the query touches the object protected by this policy.
		if !queryTouchesObject(tablesUsed, entry.QueryText, policy, columnLookup(store, connectionID)) {
			continue
		}

		// Check whether the user holds the required role.
		if hasRole(userRoles, policy.RequiredRole) {
			continue
		}

		// No required role found → create a violation.
		detail := fmt.Sprintf(
			"User %q executed a query touching %s without required role %q",
			entry.User,
			describePolicyObject(policy),
			policy.RequiredRole,
		)

		violations = append(violations, PolicyViolation{
			ID:              uuid.New().String(),
			ConnectionID:    connectionID,
			PolicyID:        policy.ID,
			QueryLogID:      entry.ID,
			User:            entry.User,
			ViolationDetail: detail,
			Severity:        policy.Severity,
			DetectedAt:      now,
			CreatedAt:       now,
			PolicyName:      policy.Name,
		})
	}

	return violations
}

// ── Internal helpers ────────────────────────────────────────────────────────

// parseTablesUsed deserialises the JSON array stored in QueryLogEntry.TablesUsed.
// It returns an empty slice on error or empty input.
func parseTablesUsed(raw string) []string {
	if raw == "" || raw == "[]" {
		return nil
	}
	var tables []string
	if err := json.Unmarshal([]byte(raw), &tables); err != nil {
		return nil
	}
	return tables
}

// collectUserRoles extracts the set of distinct role names from access matrix
// entries.  Both direct grants (role_name is set) and privilege names are
// collected so we can match on either.
func collectUserRoles(entries []AccessMatrixEntry) map[string]bool {
	roles := make(map[string]bool, len(entries))
	for _, e := range entries {
		if e.RoleName != nil && *e.RoleName != "" {
			roles[strings.ToLower(*e.RoleName)] = true
		}
	}
	return roles
}

// hasRole checks whether the user's role set contains the required role
// (case-insensitive comparison).
func hasRole(userRoles map[string]bool, requiredRole string) bool {
	return userRoles[strings.ToLower(requiredRole)]
}

// tableColumnLister is implemented by Store. Stores without it (test fakes)
// make SELECT * count as touching every column policy on the table.
type tableColumnLister interface {
	GetColumns(connectionID, dbName, tableName string) ([]GovColumn, error)
}

// columnLookup returns the known column names of a table from governance
// metadata, or nil when they are not known.
func columnLookup(store interface{}, connectionID string) func(db, table string) []string {
	lister, ok := store.(tableColumnLister)
	if !ok || lister == nil {
		return nil
	}
	return func(db, table string) []string {
		if db == "" || table == "" {
			return nil
		}
		cols, err := lister.GetColumns(connectionID, db, table)
		if err != nil || len(cols) == 0 {
			return nil
		}
		names := make([]string, 0, len(cols))
		for _, c := range cols {
			names = append(names, c.ColumnName)
		}
		return names
	}
}

// queryTouchesObject determines whether a query (identified by its list of
// tables used and the raw SQL text) accesses the object described by a policy.
// knownColumns, when not nil, returns the table's columns from governance
// metadata so SELECT * can be resolved.
func queryTouchesObject(tablesUsed []string, queryText string, policy Policy, knownColumns func(db, table string) []string) bool {
	switch strings.ToLower(policy.ObjectType) {
	case "database":
		return touchesDatabase(tablesUsed, deref(policy.ObjectDatabase))

	case "table":
		return touchesTable(tablesUsed, deref(policy.ObjectDatabase), deref(policy.ObjectTable))

	case "column":
		if !touchesTable(tablesUsed, deref(policy.ObjectDatabase), deref(policy.ObjectTable)) {
			return false
		}
		// For column-level policies, check if the column name appears in the
		// query text.  This is a heuristic — a full parser would be needed
		// for perfect accuracy.
		col := deref(policy.ObjectColumn)
		if col == "" {
			return false
		}
		// A column named only in a "* EXCEPT (...)" list is not read.
		if columnMentioned(exceptListRe.ReplaceAllString(queryText, " "), col) {
			return true
		}
		// SELECT * (or t.*) reads every column of the table, unless the
		// column is listed in its EXCEPT clause. When governance metadata
		// knows the table's columns and this one is not among them, the
		// star cannot reach it.
		if !selectsAllColumnsIncluding(queryText, col) {
			return false
		}
		if knownColumns != nil {
			if cols := knownColumns(deref(policy.ObjectDatabase), deref(policy.ObjectTable)); len(cols) > 0 {
				for _, c := range cols {
					if strings.EqualFold(c, col) {
						return true
					}
				}
				return false
			}
		}
		return true

	default:
		return false
	}
}

// touchesDatabase returns true if any entry in tablesUsed belongs to the
// given database.  tablesUsed entries are expected in "db.table" format.
func touchesDatabase(tablesUsed []string, database string) bool {
	if database == "" {
		return false
	}
	lowerDB := strings.ToLower(database)
	for _, t := range tablesUsed {
		parts := strings.SplitN(t, ".", 2)
		if len(parts) == 2 && strings.ToLower(parts[0]) == lowerDB {
			return true
		}
	}
	return false
}

// touchesTable returns true if the specific db.table combination appears in
// tablesUsed.
func touchesTable(tablesUsed []string, database, table string) bool {
	if table == "" {
		return false
	}
	lowerDB := strings.ToLower(database)
	lowerTbl := strings.ToLower(table)

	for _, t := range tablesUsed {
		parts := strings.SplitN(t, ".", 2)
		switch {
		case len(parts) == 2:
			// A policy without a database protects the table name in any
			// database.
			if (lowerDB == "" || strings.ToLower(parts[0]) == lowerDB) && strings.ToLower(parts[1]) == lowerTbl {
				return true
			}
		case len(parts) == 1:
			// No database qualifier in tablesUsed — match on table name alone
			// only when the policy also has no database.
			if lowerDB == "" && strings.ToLower(parts[0]) == lowerTbl {
				return true
			}
		}
	}
	return false
}

// columnMentioned does a case-insensitive check for the column identifier in
// the query text.  It looks for the column name as a whole word (surrounded
// by non-identifier characters or string boundaries).
func columnMentioned(queryText, column string) bool {
	lower := strings.ToLower(queryText)
	col := strings.ToLower(column)
	idx := 0
	for {
		pos := strings.Index(lower[idx:], col)
		if pos < 0 {
			return false
		}
		pos += idx
		// Check word boundaries.
		startOK := pos == 0 || !isIdentChar(lower[pos-1])
		endPos := pos + len(col)
		endOK := endPos >= len(lower) || !isIdentChar(lower[endPos])
		if startOK && endOK {
			return true
		}
		idx = pos + 1
	}
}

// starSelectRe matches a star select item: "SELECT *", "SELECT DISTINCT *",
// ", *" and "t.*", with an optional EXCEPT list. count(*) is not matched.
var starSelectRe = regexp.MustCompile("(?i)(?:\\bSELECT\\s+(?:DISTINCT\\s+)?|,\\s*|[\\w`]\\.)\\*(\\s+EXCEPT\\s*(?:\\(([^)]*)\\)|([\\w`]+)))?")

// exceptListRe matches the column list of "* EXCEPT (a, b)" / "* EXCEPT a".
var exceptListRe = regexp.MustCompile("(?i)\\*\\s+EXCEPT\\s*(?:\\([^)]*\\)|[\\w`]+)")

// selectsAllColumnsIncluding reports whether the query has a star select
// item whose EXCEPT list (if any) does not exclude column.
func selectsAllColumnsIncluding(queryText, column string) bool {
	for _, m := range starSelectRe.FindAllStringSubmatch(queryText, -1) {
		if m[1] == "" {
			return true
		}
		excluded := m[2] + "," + m[3]
		if !columnMentioned(strings.ReplaceAll(excluded, "`", " "), column) {
			return true
		}
	}
	return false
}

// isIdentChar returns true for characters that can appear in a SQL identifier.
func isIdentChar(c byte) bool {
	return (c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c == '_'
}

// describePolicyObject returns a human-readable description of the object a
// policy protects, for use in violation messages.
func describePolicyObject(p Policy) string {
	switch strings.ToLower(p.ObjectType) {
	case "database":
		return fmt.Sprintf("database %q", deref(p.ObjectDatabase))
	case "table":
		db := deref(p.ObjectDatabase)
		tbl := deref(p.ObjectTable)
		if db != "" {
			return fmt.Sprintf("table %q.%q", db, tbl)
		}
		return fmt.Sprintf("table %q", tbl)
	case "column":
		db := deref(p.ObjectDatabase)
		tbl := deref(p.ObjectTable)
		col := deref(p.ObjectColumn)
		if db != "" {
			return fmt.Sprintf("column %q.%q.%q", db, tbl, col)
		}
		return fmt.Sprintf("column %q.%q", tbl, col)
	default:
		return p.ObjectType
	}
}

// deref safely dereferences a string pointer, returning the empty string for nil.
func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
