// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package governance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/querylogactor"
	"github.com/google/uuid"
)

const queryLogBatchLimit = 5000
const defaultQueryLogWatermark = "2000-01-01 00:00:00.000000"

// syncQueryLog harvests recent queries from system.query_log, classifies them,
// and evaluates access policies.
//
// Harvesting is gated by the query_harvest_mode governance setting: "off"
// skips entirely, "auto" (default) harvests only when at least one policy
// exists for the connection, "always" harvests unconditionally.
func (s *Syncer) syncQueryLog(ctx context.Context, creds CHCredentials) (*QueryLogSyncResult, error) {
	connID := creds.ConnectionID
	now := time.Now().UTC().Format(time.RFC3339)

	skipHarvest := false
	switch mode := s.db.QueryHarvestMode(); mode {
	case database.QueryHarvestModeOff:
		skipHarvest = true
		slog.Debug("Query log harvest skipped: harvesting disabled", "connection", connID)
	case database.QueryHarvestModeAuto:
		policies, err := s.store.GetPolicies(connID)
		if err != nil {
			slog.Warn("Query log harvest: failed to load policies for auto mode", "connection", connID, "error", err)
		} else if len(policies) == 0 {
			skipHarvest = true
			slog.Debug("Query log harvest skipped: auto mode with no policies", "connection", connID)
		}
	}
	if skipHarvest {
		// Mark the state as freshly synced (keeping the stored watermark) so
		// the background staleness check does not retrigger syncs every tick.
		if err := s.store.UpsertSyncState(connID, string(SyncQueryLog), "idle", nil, nil, 0); err != nil {
			slog.Error("Failed to update sync state for skipped query log harvest", "error", err)
		}
		return &QueryLogSyncResult{}, nil
	}

	// Update sync state to running
	if err := s.store.MarkSyncRunning(connID, string(SyncQueryLog), creds.User); err != nil {
		slog.Error("Failed to update sync state", "error", err)
	}

	result := &QueryLogSyncResult{}
	var syncErr error

	defer func() {
		status := "idle"
		var errMsg *string
		if syncErr != nil {
			status = "error"
			e := syncErr.Error()
			errMsg = &e
		}
		// Only advance the watermark when this run read rows and stored
		// them. On any error the stored watermark stays where it was, so
		// the next run reads the same window again instead of skipping it.
		var watermark *string
		if syncErr == nil && result.NewWatermark != "" {
			wm := result.NewWatermark
			watermark = &wm
		}
		if err := s.store.UpsertSyncState(connID, string(SyncQueryLog), status, watermark, errMsg, result.QueriesIngested); err != nil {
			slog.Error("Failed to update sync state after query log sync", "error", err)
		}
	}()

	// Get current watermark from sync state
	wm := parseQueryLogWatermark("")
	state, err := s.store.GetSyncState(connID, string(SyncQueryLog))
	if err == nil && state != nil && state.Watermark != nil && *state.Watermark != "" {
		wm = parseQueryLogWatermark(*state.Watermark)
	}
	minDuration := QueryHarvestMinDurationMs(s.db)
	accounts := s.personAccounts(connID)

	syncUsers := s.governanceUsers(connID, creds.User)

	rows, err := s.executeQuery(ctx, creds, buildQueryLogHarvestSQL(wm, syncUsers, minDuration, true, accounts))
	if err != nil {
		// Fallback for older CH setups where the "tables" column may be unavailable.
		rows, err = s.executeQuery(ctx, creds, buildQueryLogHarvestSQL(wm, syncUsers, minDuration, false, accounts))
		if err != nil {
			syncErr = fmt.Errorf("query_log query failed: %w", err)
			return result, syncErr
		}
	}

	if len(rows) == 0 {
		return result, nil
	}

	// Build QueryLogEntry batch
	var entries []QueryLogEntry
	var latest queryLogWatermark

	for _, row := range rows {
		queryText := fmt.Sprintf("%v", row["query"])
		normalized := normalizeQuery(queryText)
		hash := hashNormalized(normalized)
		kind := classifyQuery(queryText)
		eventTime := fmt.Sprintf("%v", row["event_time"])

		isError := false
		var errorMsg *string
		if exCode := toInt64(row["exception_code"]); exCode != 0 {
			isError = true
			ex := fmt.Sprintf("%v", row["exception"])
			errorMsg = &ex
		}

		entry := QueryLogEntry{
			ID:             uuid.NewString(),
			ConnectionID:   connID,
			QueryID:        fmt.Sprintf("%v", row["query_id"]),
			User:           fmt.Sprintf("%v", row["ch_user"]),
			Actor:          fmt.Sprintf("%v", row["actor"]),
			QueryText:      queryText,
			NormalizedHash: hash,
			QueryKind:      kind,
			EventTime:      eventTime,
			DurationMs:     toInt64(row["query_duration_ms"]),
			ReadRows:       toInt64(row["read_rows"]),
			ReadBytes:      toInt64(row["read_bytes"]),
			ResultRows:     toInt64(row["result_rows"]),
			WrittenRows:    toInt64(row["written_rows"]),
			WrittenBytes:   toInt64(row["written_bytes"]),
			MemoryUsage:    toInt64(row["memory_usage"]),
			TablesUsed:     extractTablesJSON(row["tables"]),
			IsError:        isError,
			ErrorMessage:   errorMsg,
			CreatedAt:      now,
		}

		entries = append(entries, entry)
		latest = parseQueryLogWatermark(fmt.Sprintf("%v|%s", row["event_time_us"], entry.QueryID))
	}

	// Batch insert into SQLite
	inserted, err := s.store.InsertQueryLogBatch(entries)
	if err != nil {
		syncErr = fmt.Errorf("failed to batch insert query log: %w", err)
		return result, syncErr
	}
	result.QueriesIngested = inserted
	result.NewWatermark = latest.String()

	// Evaluate policies against each entry
	violationCount := 0
	policies, err := s.store.GetPolicies(connID)
	if err != nil {
		slog.Warn("Failed to load policies for violation check", "error", err)
	} else if len(policies) > 0 {
		for _, entry := range entries {
			// A row already stored by an earlier run was evaluated then;
			// evaluating it again would duplicate its violations.
			if !entry.Inserted {
				continue
			}
			violations := EvaluatePolicies(connID, entry, policies, s.store)
			for _, v := range violations {
				violationID, err := s.store.CreateViolation(connID, v.PolicyID, v.QueryLogID, v.User, v.ViolationDetail, v.Severity, "post_exec", "")
				if err != nil {
					slog.Error("Failed to insert policy violation", "error", err)
					continue
				}

				policyName := strings.TrimSpace(v.PolicyName)
				if policyName == "" {
					policyName = v.PolicyID
				}
				alertSeverity := strings.ToLower(strings.TrimSpace(v.Severity))
				if alertSeverity == "" {
					alertSeverity = alerts.SeverityWarn
				}
				fingerprint := fmt.Sprintf("policy:%s:user:%s:hash:%s", v.PolicyID, v.User, entry.NormalizedHash)
				if _, err := s.db.CreateAlertEvent(
					&connID,
					alerts.EventTypePolicyViolation,
					alertSeverity,
					fmt.Sprintf("Policy violation: %s", policyName),
					v.ViolationDetail,
					map[string]interface{}{
						"violation_id":       violationID,
						"policy_id":          v.PolicyID,
						"policy_name":        v.PolicyName,
						"query_id":           entry.QueryID,
						"query_kind":         entry.QueryKind,
						"ch_user":            entry.User,
						"query_hash":         entry.NormalizedHash,
						"event_time":         entry.EventTime,
						"violation_severity": v.Severity,
					},
					fingerprint,
					violationID,
				); err != nil {
					slog.Warn("Failed to create alert event for policy violation", "error", err)
				}
				if _, created, err := s.store.UpsertIncidentFromViolation(
					connID,
					violationID,
					policyName,
					v.User,
					alertSeverity,
					v.ViolationDetail,
				); err != nil {
					slog.Warn("Failed to upsert incident for policy violation", "violation", violationID, "error", err)
				} else if created {
					slog.Info("Governance incident created from violation", "violation", violationID)
				}
				violationCount++
			}
		}
	}
	result.ViolationsFound = violationCount

	slog.Info("Query log sync completed",
		"connection", connID,
		"ingested", result.QueriesIngested,
		"violations", result.ViolationsFound,
		"new_watermark", result.NewWatermark,
	)

	return result, nil
}

// ── Query helper functions ──────────────────────────────────────────────────

// classifyQuery returns a classification string for the query type.
func classifyQuery(query string) string {
	trimmed := strings.TrimSpace(query)
	for strings.HasPrefix(trimmed, "--") {
		if idx := strings.Index(trimmed, "\n"); idx >= 0 {
			trimmed = strings.TrimSpace(trimmed[idx+1:])
		} else {
			break
		}
	}

	upper := strings.ToUpper(trimmed)
	switch {
	case strings.HasPrefix(upper, "SELECT") || strings.HasPrefix(upper, "WITH"):
		return "Select"
	case strings.HasPrefix(upper, "INSERT"):
		return "Insert"
	case strings.HasPrefix(upper, "CREATE"):
		return "Create"
	case strings.HasPrefix(upper, "ALTER"):
		return "Alter"
	case strings.HasPrefix(upper, "DROP"):
		return "Drop"
	default:
		return "Other"
	}
}

var (
	stringLiteralRe = regexp.MustCompile(`'[^']*'`)
	numberLiteralRe = regexp.MustCompile(`\b\d+\.?\d*\b`)
	multiSpaceRe    = regexp.MustCompile(`\s+`)
)

func normalizeQuery(query string) string {
	normalized := stringLiteralRe.ReplaceAllString(query, "'?'")
	normalized = numberLiteralRe.ReplaceAllString(normalized, "?")
	normalized = multiSpaceRe.ReplaceAllString(normalized, " ")
	normalized = strings.TrimSpace(normalized)
	return strings.ToUpper(normalized)
}

func hashNormalized(normalized string) string {
	h := sha256.Sum256([]byte(normalized))
	return fmt.Sprintf("%x", h)[:32]
}

func extractTablesJSON(v interface{}) string {
	if v == nil {
		return "[]"
	}

	switch val := v.(type) {
	case string:
		if strings.HasPrefix(val, "[") {
			return val
		}
		if val == "" {
			return "[]"
		}
		b, _ := json.Marshal([]string{val})
		return string(b)
	case []interface{}:
		strs := make([]string, 0, len(val))
		for _, item := range val {
			strs = append(strs, fmt.Sprintf("%v", item))
		}
		b, _ := json.Marshal(strs)
		return string(b)
	case []string:
		b, _ := json.Marshal(val)
		return string(b)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "[]"
		}
		return string(b)
	}
}

// queryLogWatermark is the harvest position: the event_time_microseconds of
// the last stored row plus its query_id as a tiebreak, so rows sharing a
// timestamp are never skipped when a batch ends between them.
type queryLogWatermark struct {
	TS      string // "2006-01-02 15:04:05.000000", ClickHouse server time zone
	QueryID string
}

// String encodes the watermark as "<ts>|<query_id>" for gov_sync_state.
func (w queryLogWatermark) String() string {
	return w.TS + "|" + w.QueryID
}

// parseQueryLogWatermark reads a stored watermark. Older versions stored a
// plain timestamp (seconds or RFC3339); those parse with an empty query_id.
func parseQueryLogWatermark(v string) queryLogWatermark {
	raw := strings.TrimSpace(v)
	qid := ""
	if idx := strings.Index(raw, "|"); idx >= 0 {
		qid = raw[idx+1:]
		raw = raw[:idx]
	}
	return queryLogWatermark{TS: sanitizeQueryLogTimestamp(raw), QueryID: qid}
}

func sanitizeQueryLogTimestamp(v string) string {
	s := strings.TrimSpace(v)
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format(queryLogTimestampLayout)
		}
	}
	return defaultQueryLogWatermark
}

const queryLogTimestampLayout = "2006-01-02 15:04:05.000000"

// GovernanceLogComment tags every query the governance syncer sends, so the
// query-log harvest can leave out its own reads.
const GovernanceLogComment = "ch-ui:governance"

// chQuote returns s as a ClickHouse single-quoted string literal.
func chQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return "'" + s + "'"
}

// personAccounts lists the connection's per-person SSO users so the harvest
// labels their untagged queries by email. A lookup failure only loses the
// relabeling, never the harvest.
func (s *Syncer) personAccounts(connID string) []querylogactor.Account {
	if s.db == nil {
		return nil
	}
	users, err := s.db.ListSSOClickHouseUsers(connID)
	if err != nil {
		slog.Warn("Query log harvest: failed to load SSO users for attribution", "connection", connID, "error", err)
		return nil
	}
	accounts := make([]querylogactor.Account, 0, len(users))
	for _, u := range users {
		accounts = append(accounts, querylogactor.Account{User: u.CHUser, Person: u.Email})
	}
	return accounts
}

// governanceUsers lists the ClickHouse users governance syncs of the
// connection ran as: the current one plus those recorded in the sync state,
// so the syncer's own queries stay out of the harvest when the account
// changed between syncs (or a manual sync ran as someone else).
func (s *Syncer) governanceUsers(connID, current string) []string {
	users := []string{current}
	recorded, err := s.store.SyncUsers(connID)
	if err != nil {
		slog.Warn("Query log harvest: failed to load governance sync users", "connection", connID, "error", err)
		return users
	}
	for _, u := range recorded {
		if u != current {
			users = append(users, u)
		}
	}
	return users
}

// buildQueryLogHarvestSQL reads system.query_log after the watermark. It keeps
// finished queries and both exception types (so denied attempts are audited),
// and leaves out only the governance syncer's own queries: log_comment alone
// is not enough, since any user can set it, so the user must also be one the
// syncer ran as (syncUsers).
// ch_user stays the ClickHouse account (policies refer to it); actor is the
// person behind the row (querylogactor.Expr).
func buildQueryLogHarvestSQL(wm queryLogWatermark, syncUsers []string, minDurationMs int64, withTables bool, accounts []querylogactor.Account) string {
	tables := "tables"
	if !withTables {
		tables = "CAST([], 'Array(String)') AS tables"
	}
	ts := fmt.Sprintf("parseDateTime64BestEffort(%s, 6)", chQuote(wm.TS))
	quotedUsers := make([]string, len(syncUsers))
	for i, u := range syncUsers {
		quotedUsers[i] = chQuote(u)
	}
	duration := ""
	if minDurationMs > 0 {
		// The floor only applies to successful queries; failures are always kept.
		duration = fmt.Sprintf("\n\t  AND (type != 'QueryFinish' OR query_duration_ms >= %d)", minDurationMs)
	}
	return fmt.Sprintf(`SELECT
		query_id,
		user AS ch_user,
		%s AS actor,
		query,
		event_time,
		toString(event_time_microseconds) AS event_time_us,
		query_duration_ms,
		read_rows,
		read_bytes,
		result_rows,
		written_rows,
		written_bytes,
		memory_usage,
		%s,
		exception_code,
		exception
	FROM system.query_log
	WHERE type IN ('QueryFinish', 'ExceptionBeforeStart', 'ExceptionWhileProcessing')
	  AND is_initial_query = 1
	  AND event_date >= toDate(%s)
	  AND event_time >= toDateTime(%s)
	  AND (event_time_microseconds, query_id) > (%s, %s)
	  AND NOT (log_comment = %s AND user IN (%s))%s
	ORDER BY event_time_microseconds ASC, query_id ASC
	LIMIT %d`, querylogactor.Expr(accounts), tables, ts, ts, ts, chQuote(wm.QueryID), chQuote(GovernanceLogComment), strings.Join(quotedUsers, ", "), duration, queryLogBatchLimit)
}

// SettingQueryHarvestMinDurationKey stores the optional duration floor (ms)
// for harvested successful queries. Unset or 0 keeps every query.
const SettingQueryHarvestMinDurationKey = "governance.query_harvest_min_duration_ms"

// MaxQueryHarvestMinDurationMs bounds the configurable floor (one hour).
const MaxQueryHarvestMinDurationMs = 3_600_000

type settingsStore interface {
	GetSetting(key string) (string, error)
	SetSetting(key, value string) error
}

// QueryHarvestMinDurationMs returns the configured duration floor, 0 by default.
func QueryHarvestMinDurationMs(db settingsStore) int64 {
	if db == nil {
		return 0
	}
	v, err := db.GetSetting(SettingQueryHarvestMinDurationKey)
	if err != nil {
		return 0
	}
	n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
	if err != nil || n < 0 || n > MaxQueryHarvestMinDurationMs {
		return 0
	}
	return n
}

// SetQueryHarvestMinDurationMs validates and stores the duration floor.
func SetQueryHarvestMinDurationMs(db settingsStore, ms int64) error {
	if ms < 0 || ms > MaxQueryHarvestMinDurationMs {
		return fmt.Errorf("min_duration_ms must be between 0 and %d", MaxQueryHarvestMinDurationMs)
	}
	return db.SetSetting(SettingQueryHarvestMinDurationKey, strconv.FormatInt(ms, 10))
}
