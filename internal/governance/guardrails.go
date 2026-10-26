// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package governance

import (
	"fmt"
	"log/slog"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
)

const defaultGuardrailStaleAfter = 10 * time.Minute

var showTablesFromRe = regexp.MustCompile(`(?i)\bSHOW\s+TABLES\s+(?:FROM|IN)\s+` + tableRefPattern)

type guardrailStore interface {
	GetEnabledPolicies(connectionID string) ([]Policy, error)
	GetAccessMatrixForUser(connectionID, userName string) ([]AccessMatrixEntry, error)
	GetSyncState(connectionID string, syncType string) (*SyncState, error)
	CreateViolation(connectionID, policyID, queryLogID, user, detail, severity, detectionPhase, requestEndpoint string) (string, error)
	UpsertIncidentFromViolation(connectionID, sourceRef, policyName, user, severity, detail string) (string, bool, error)
}

type alertEventWriter interface {
	CreateAlertEvent(connectionID *string, eventType, severity, title, message string, payload interface{}, fingerprint, sourceRef string) (string, error)
}

type GuardrailService struct {
	store      guardrailStore
	alerts     alertEventWriter
	staleAfter time.Duration
	now        func() time.Time
}

type GuardrailDecision struct {
	Allowed bool
	Block   *GuardrailBlock
}

type GuardrailBlock struct {
	PolicyID        string
	PolicyName      string
	Severity        string
	EnforcementMode string
	ViolationID     string
	Detail          string
}

func NewGuardrailService(store *Store, db *database.DB) *GuardrailService {
	return &GuardrailService{
		store:      store,
		alerts:     db,
		staleAfter: defaultGuardrailStaleAfter,
		now:        time.Now,
	}
}

// EvaluateQuery checks a query before it runs. Unqualified table names are
// resolved against ClickHouse's "default" database; use
// EvaluateQueryInDatabase when the session's current database is known.
//
// Guardrails are advisory: they match the query text, and they allow the
// query when the access data is stale or missing (fail open).
func (s *GuardrailService) EvaluateQuery(connectionID, user, queryText, requestEndpoint string) (GuardrailDecision, error) {
	return s.EvaluateQueryInDatabase(connectionID, user, "", queryText, requestEndpoint)
}

// EvaluateQueryInDatabase is EvaluateQuery with the session's current
// database, used to resolve unqualified table names ("" means "default").
func (s *GuardrailService) EvaluateQueryInDatabase(connectionID, user, currentDatabase, queryText, requestEndpoint string) (GuardrailDecision, error) {
	tablesUsed := qualifyTables(extractPolicyTablesFromQuery(queryText), currentDatabase)
	return s.evaluate(connectionID, user, queryText, tablesUsed, requestEndpoint)
}

// qualifyTables prefixes unqualified table names with the current database,
// so a policy on db.table also matches "SELECT ... FROM table" run in db.
func qualifyTables(tables []string, currentDatabase string) []string {
	db := strings.TrimSpace(currentDatabase)
	if db == "" {
		db = "default"
	}
	out := make([]string, 0, len(tables))
	for _, t := range tables {
		if !strings.Contains(t, ".") {
			t = db + "." + t
		}
		out = append(out, t)
	}
	return out
}

func (s *GuardrailService) EvaluateTable(connectionID, user, databaseName, tableName, requestEndpoint string) (GuardrailDecision, error) {
	db := strings.TrimSpace(databaseName)
	tbl := strings.TrimSpace(tableName)
	if db == "" || tbl == "" {
		return GuardrailDecision{Allowed: true}, nil
	}
	queryText := fmt.Sprintf("SELECT * FROM `%s`.`%s`", db, tbl)
	tablesUsed := []string{db + "." + tbl}
	return s.evaluate(connectionID, user, queryText, tablesUsed, requestEndpoint)
}

func (s *GuardrailService) evaluate(connectionID, user, queryText string, tablesUsed []string, requestEndpoint string) (GuardrailDecision, error) {
	policies, err := s.store.GetEnabledPolicies(connectionID)
	if err != nil {
		return GuardrailDecision{}, fmt.Errorf("load enabled policies: %w", err)
	}
	if len(policies) == 0 {
		return GuardrailDecision{Allowed: true}, nil
	}

	uncertain, uncertainReason := s.isAccessStateUncertain(connectionID)
	if uncertain {
		s.emitUncertainGuardrailEvent(connectionID, user, queryText, requestEndpoint, uncertainReason)
		return GuardrailDecision{Allowed: true}, nil
	}

	matrixEntries, err := s.store.GetAccessMatrixForUser(connectionID, user)
	if err != nil {
		s.emitUncertainGuardrailEvent(connectionID, user, queryText, requestEndpoint, "access matrix lookup failed")
		return GuardrailDecision{Allowed: true}, nil
	}

	userRoles := collectUserRoles(matrixEntries)
	blockingPolicies := make([]Policy, 0)

	for _, policy := range policies {
		if !policy.Enabled {
			continue
		}
		if !queryTouchesObject(tablesUsed, queryText, policy, columnLookup(s.store, connectionID)) {
			continue
		}
		if hasRole(userRoles, policy.RequiredRole) {
			continue
		}
		if normalizePolicyEnforcementMode(policy.EnforcementMode) == "block" {
			blockingPolicies = append(blockingPolicies, policy)
		}
	}

	if len(blockingPolicies) == 0 {
		return GuardrailDecision{Allowed: true}, nil
	}

	selected := pickBlockingPolicy(blockingPolicies)
	detail := fmt.Sprintf(
		"Query blocked before execution: user %q touched %s without required role %q",
		user,
		describePolicyObject(selected),
		selected.RequiredRole,
	)
	severity := normalizeGuardrailSeverity(selected.Severity)

	violationID := ""
	createdViolationID, err := s.store.CreateViolation(
		connectionID,
		selected.ID,
		"",
		user,
		detail,
		severity,
		"pre_exec_block",
		requestEndpoint,
	)
	if err != nil {
		slog.Warn("Failed to persist pre-exec guardrail violation", "connection", connectionID, "policy_id", selected.ID, "error", err)
	} else {
		violationID = createdViolationID
		if _, _, err := s.store.UpsertIncidentFromViolation(connectionID, violationID, selected.Name, user, severity, detail); err != nil {
			slog.Warn("Failed to upsert incident from pre-exec guardrail violation", "violation_id", violationID, "error", err)
		}
	}

	if s.alerts != nil {
		fingerprint := fmt.Sprintf("policy:%s:user:%s:hash:%s", selected.ID, user, hashNormalized(normalizeQuery(queryText)))
		payload := map[string]interface{}{
			"guardrail_status":   "blocked",
			"policy_id":          selected.ID,
			"policy_name":        selected.Name,
			"query_hash":         hashNormalized(normalizeQuery(queryText)),
			"request_endpoint":   requestEndpoint,
			"violation_id":       violationID,
			"violation_severity": severity,
			"detection_phase":    "pre_exec_block",
			"enforcement_mode":   "block",
			"blocked_user":       user,
		}
		sourceRef := violationID
		if _, err := s.alerts.CreateAlertEvent(
			&connectionID,
			alerts.EventTypePolicyViolation,
			severity,
			fmt.Sprintf("Policy blocked query: %s", strings.TrimSpace(selected.Name)),
			detail,
			payload,
			fingerprint,
			sourceRef,
		); err != nil {
			slog.Warn("Failed to create blocked guardrail alert event", "connection", connectionID, "policy_id", selected.ID, "error", err)
		}
	}

	return GuardrailDecision{
		Allowed: false,
		Block: &GuardrailBlock{
			PolicyID:        selected.ID,
			PolicyName:      selected.Name,
			Severity:        severity,
			EnforcementMode: "block",
			ViolationID:     violationID,
			Detail:          detail,
		},
	}, nil
}

func (s *GuardrailService) isAccessStateUncertain(connectionID string) (bool, string) {
	state, err := s.store.GetSyncState(connectionID, string(SyncAccess))
	if err != nil {
		return true, "failed to read governance access sync state"
	}
	if state == nil {
		return true, "governance access sync state missing"
	}
	if strings.EqualFold(strings.TrimSpace(state.Status), "error") {
		return true, "governance access sync state is error"
	}
	if state.LastSyncedAt == nil || strings.TrimSpace(*state.LastSyncedAt) == "" {
		return true, "governance access sync has no successful sync timestamp"
	}

	lastSyncedAt, err := time.Parse(time.RFC3339, strings.TrimSpace(*state.LastSyncedAt))
	if err != nil {
		return true, "governance access sync timestamp is invalid"
	}
	if s.now().UTC().Sub(lastSyncedAt.UTC()) > s.staleAfter {
		return true, "governance access sync state is stale"
	}
	return false, ""
}

func (s *GuardrailService) emitUncertainGuardrailEvent(connectionID, user, queryText, requestEndpoint, reason string) {
	if s.alerts == nil {
		return
	}
	queryHash := hashNormalized(normalizeQuery(queryText))
	fingerprint := fmt.Sprintf("guardrail:uncertain:conn:%s:user:%s:hash:%s:endpoint:%s", connectionID, user, queryHash, requestEndpoint)
	payload := map[string]interface{}{
		"guardrail_status": "uncertain",
		"reason":           reason,
		"request_endpoint": requestEndpoint,
		"query_hash":       queryHash,
		"ch_user":          user,
	}
	if _, err := s.alerts.CreateAlertEvent(
		&connectionID,
		alerts.EventTypePolicyViolation,
		alerts.SeverityWarn,
		"Guardrail evaluation uncertain",
		"Guardrail pre-execution evaluation could not be trusted; query was allowed",
		payload,
		fingerprint,
		"",
	); err != nil {
		slog.Warn("Failed to create uncertain guardrail alert event", "connection", connectionID, "error", err)
	}
}

func pickBlockingPolicy(policies []Policy) Policy {
	ordered := make([]Policy, len(policies))
	copy(ordered, policies)
	sort.SliceStable(ordered, func(i, j int) bool {
		left := ordered[i]
		right := ordered[j]
		lp := guardrailSeverityPriority(left.Severity)
		rp := guardrailSeverityPriority(right.Severity)
		if lp != rp {
			return lp > rp
		}
		ln := strings.ToLower(strings.TrimSpace(left.Name))
		rn := strings.ToLower(strings.TrimSpace(right.Name))
		if ln != rn {
			return ln < rn
		}
		return strings.ToLower(strings.TrimSpace(left.ID)) < strings.ToLower(strings.TrimSpace(right.ID))
	})
	return ordered[0]
}

func guardrailSeverityPriority(v string) int {
	switch normalizeGuardrailSeverity(v) {
	case "critical":
		return 4
	case "error":
		return 3
	case "warn":
		return 2
	case "info":
		return 1
	default:
		return 0
	}
}

func normalizeGuardrailSeverity(v string) string {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "critical":
		return "critical"
	case "error":
		return "error"
	case "info":
		return "info"
	default:
		return "warn"
	}
}

func extractPolicyTablesFromQuery(queryText string) []string {
	query := normaliseWhitespace(queryText)
	seen := make(map[string]bool, 16)
	out := make([]string, 0, 8)
	isShowTablesQuery := showTablesFromRe.MatchString(query)
	addTable := func(dbName, tableName string) {
		dbName = strings.TrimSpace(dbName)
		tableName = strings.TrimSpace(tableName)
		if tableName == "" {
			return
		}
		key := tableName
		val := tableName
		if dbName != "" {
			key = strings.ToLower(dbName + "." + tableName)
			val = dbName + "." + tableName
		} else {
			key = strings.ToLower(tableName)
		}
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, val)
	}
	addDatabase := func(dbName string) {
		dbName = strings.TrimSpace(dbName)
		if dbName == "" {
			return
		}
		key := strings.ToLower(dbName + ".__all_tables__")
		if seen[key] {
			return
		}
		seen[key] = true
		out = append(out, dbName+".__all_tables__")
	}

	if !isShowTablesQuery {
		for _, src := range extractSourceTables(query) {
			addTable(src.Database, src.Table)
		}
	}
	if target := extractTarget(query); target != nil {
		addTable(target.Database, target.Table)
	}
	for _, match := range showTablesFromRe.FindAllStringSubmatch(query, -1) {
		if len(match) < 2 {
			continue
		}
		raw := stripBackticks(strings.TrimSpace(match[1]))
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ".", 2)
		if len(parts) == 2 {
			addDatabase(stripBackticks(parts[0]))
			continue
		}
		addDatabase(stripBackticks(raw))
	}
	return out
}

// ── SQL parsing helpers ─────────────────────────────────────────────────────

// tableRefPattern matches a possibly qualified table name: [db.]table
// Handles both backtick-quoted and unquoted identifiers.
const tableRefPattern = `((?:` + "`" + `[^` + "`" + `]+` + "`" + `|[a-zA-Z_][a-zA-Z0-9_]*)(?:\.(?:` + "`" + `[^` + "`" + `]+` + "`" + `|[a-zA-Z_][a-zA-Z0-9_]*))?)`

var (
	fromRe   = regexp.MustCompile(`(?i)\bFROM\s+` + tableRefPattern)
	joinRe   = regexp.MustCompile(`(?i)\bJOIN\s+` + tableRefPattern)
	insertRe = regexp.MustCompile(`(?i)\bINSERT\s+INTO\s+` + tableRefPattern)
	createRe = regexp.MustCompile(`(?i)\bCREATE\s+(?:TABLE|MATERIALIZED\s+VIEW)\s+(?:IF\s+NOT\s+EXISTS\s+)?` + tableRefPattern)
)

// tableRefParsed holds a parsed database.table reference.
type tableRefParsed struct {
	Database string
	Table    string
}

// extractTarget returns the target table for INSERT INTO or CREATE TABLE/MV
// statements.  Returns nil when none is found.
func extractTarget(query string) *tableRefParsed {
	// Try INSERT INTO first.
	if m := insertRe.FindStringSubmatch(query); len(m) > 1 {
		db, tbl := parseTableRef(m[1:])
		return &tableRefParsed{Database: db, Table: tbl}
	}
	// Try CREATE TABLE / MATERIALIZED VIEW.
	if m := createRe.FindStringSubmatch(query); len(m) > 1 {
		db, tbl := parseTableRef(m[1:])
		return &tableRefParsed{Database: db, Table: tbl}
	}
	return nil
}

// extractSourceTables finds all FROM and JOIN table references in the query.
func extractSourceTables(query string) []tableRefParsed {
	seen := map[string]bool{}
	var results []tableRefParsed

	addMatches := func(re *regexp.Regexp) {
		for _, m := range re.FindAllStringSubmatch(query, -1) {
			if len(m) < 2 {
				continue
			}
			db, tbl := parseTableRef(m[1:])
			if isSystemTable(db, tbl) {
				continue
			}
			key := db + "." + tbl
			if seen[key] {
				continue
			}
			seen[key] = true
			results = append(results, tableRefParsed{Database: db, Table: tbl})
		}
	}

	addMatches(fromRe)
	addMatches(joinRe)

	return results
}

// parseTableRef takes the captured groups from a table-reference regex match
// and splits them into (database, table).  If no database qualifier is
// present, database is returned as an empty string.
func parseTableRef(groups []string) (database, table string) {
	if len(groups) == 0 {
		return "", ""
	}
	raw := groups[0]
	raw = stripBackticks(raw)

	parts := strings.SplitN(raw, ".", 2)
	if len(parts) == 2 {
		return stripBackticks(parts[0]), stripBackticks(parts[1])
	}
	return "", stripBackticks(parts[0])
}

// stripBackticks removes surrounding backticks from an identifier.
func stripBackticks(s string) string {
	if len(s) >= 2 && s[0] == '`' && s[len(s)-1] == '`' {
		return s[1 : len(s)-1]
	}
	return s
}

// isSystemTable returns true for ClickHouse system and information_schema
// databases that should be excluded from table extraction.
func isSystemTable(db, table string) bool {
	lower := strings.ToLower(db)
	switch lower {
	case "system", "information_schema", "information_schema_upper", "INFORMATION_SCHEMA":
		return true
	}
	// Also filter tables that look like system tables when no db is specified.
	if db == "" {
		lowerT := strings.ToLower(table)
		if strings.HasPrefix(lowerT, "system.") || strings.HasPrefix(lowerT, "information_schema.") {
			return true
		}
	}
	return false
}

// normaliseWhitespace collapses runs of whitespace into single spaces and
// trims the result.  This simplifies regex matching.
func normaliseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
