package governance

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/google/uuid"
)

const queryLogBatchLimit = 5000
const defaultQueryLogWatermark = "2000-01-01 00:00:00"

// syncQueryLog harvests recent queries from system.query_log, classifies them,
// extracts lineage, and evaluates access policies.
func (s *Syncer) syncQueryLog(ctx context.Context, creds CHCredentials) (*QueryLogSyncResult, error) {
	connID := creds.ConnectionID
	now := time.Now().UTC().Format(time.RFC3339)

	// Update sync state to running
	if err := s.store.UpsertSyncState(connID, string(SyncQueryLog), "running", nil, nil, 0); err != nil {
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
		watermark := result.NewWatermark
		if watermark == "" {
			watermark = now
		}
		if err := s.store.UpsertSyncState(connID, string(SyncQueryLog), status, &watermark, errMsg, result.QueriesIngested); err != nil {
			slog.Error("Failed to update sync state after query log sync", "error", err)
		}
	}()

	// Get current watermark from sync state
	watermark := defaultQueryLogWatermark
	state, err := s.store.GetSyncState(connID, string(SyncQueryLog))
	if err == nil && state != nil && state.Watermark != nil && *state.Watermark != "" {
		watermark = sanitizeQueryLogWatermark(*state.Watermark)
	}

	// Query system.query_log for finished queries since watermark
	sql := fmt.Sprintf(`SELECT
		query_id,
		user AS ch_user,
		query,
		event_time,
		query_duration_ms,
		read_rows,
		read_bytes,
		result_rows,
		written_rows,
		written_bytes,
		memory_usage,
		tables,
		exception_code,
		exception
	FROM system.query_log
	WHERE type = 'QueryFinish'
	  AND is_initial_query = 1
	  AND event_time > parseDateTimeBestEffort('%s')
	ORDER BY event_time ASC
	LIMIT %d`, watermark, queryLogBatchLimit)

	rows, err := s.executeQuery(creds, sql)
	if err != nil {
		// Fallback for older CH setups where the "tables" column may be unavailable.
		fallbackSQL := fmt.Sprintf(`SELECT
		query_id,
		user AS ch_user,
		query,
		event_time,
		query_duration_ms,
		read_rows,
		read_bytes,
		result_rows,
		written_rows,
		written_bytes,
		memory_usage,
		CAST([], 'Array(String)') AS tables,
		exception_code,
		exception
	FROM system.query_log
	WHERE type = 'QueryFinish'
	  AND is_initial_query = 1
	  AND event_time > parseDateTimeBestEffort('%s')
	ORDER BY event_time ASC
	LIMIT %d`, watermark, queryLogBatchLimit)

		rows, err = s.executeQuery(creds, fallbackSQL)
		if err != nil {
			syncErr = fmt.Errorf("query_log query failed: %w", err)
			return result, syncErr
		}
	}

	if len(rows) == 0 {
		result.NewWatermark = watermark
		return result, nil
	}

	// Build QueryLogEntry batch
	var entries []QueryLogEntry
	var latestEventTime string

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
		latestEventTime = eventTime
	}

	// Batch insert into SQLite
	inserted, err := s.store.InsertQueryLogBatch(entries)
	if err != nil {
		syncErr = fmt.Errorf("failed to batch insert query log: %w", err)
		return result, syncErr
	}
	result.QueriesIngested = inserted

	// Update watermark to the latest event time
	if latestEventTime != "" {
		result.NewWatermark = latestEventTime
	} else {
		result.NewWatermark = watermark
	}

	// Extract lineage from each entry
	lineageCount := 0
	for _, entry := range entries {
		edges := ExtractLineage(connID, entry)
		for _, edge := range edges {
			if err := s.store.InsertLineageEdge(edge); err != nil {
				slog.Error("Failed to insert lineage edge", "error", err)
				continue
			}
			lineageCount++
		}
	}
	result.LineageEdgesFound = lineageCount

	// Evaluate policies against each entry
	violationCount := 0
	policies, err := s.store.GetPolicies(connID)
	if err != nil {
		slog.Warn("Failed to load policies for violation check", "error", err)
	} else if len(policies) > 0 {
		for _, entry := range entries {
			violations := EvaluatePolicies(connID, entry, policies, s.store)
			for _, v := range violations {
				violationID, err := s.store.CreateViolation(connID, v.PolicyID, v.QueryLogID, v.User, v.ViolationDetail, v.Severity)
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
		"lineage_edges", result.LineageEdgesFound,
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

func sanitizeQueryLogWatermark(v string) string {
	s := strings.TrimSpace(v)
	if s == "" {
		return defaultQueryLogWatermark
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05.999999",
		"2006-01-02 15:04:05",
	}

	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC().Format("2006-01-02 15:04:05")
		}
	}

	// Last-resort hardening against accidental malformed/corrupt values.
	s = strings.ReplaceAll(s, "'", "")
	if s == "" {
		return defaultQueryLogWatermark
	}
	return s
}
