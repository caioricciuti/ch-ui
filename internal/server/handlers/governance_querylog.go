package handlers

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ---------- GET /clickhouse-query-log ----------

var timeRangeDurations = map[string]string{
	"5m": "5 MINUTE", "15m": "15 MINUTE", "30m": "30 MINUTE",
	"1h": "1 HOUR", "6h": "6 HOUR", "12h": "12 HOUR",
	"24h": "24 HOUR", "3d": "3 DAY", "7d": "7 DAY",
}

func (h *GovernanceHandler) GetClickHouseQueryLog(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	timeRange := r.URL.Query().Get("timeRange")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	search := r.URL.Query().Get("search")
	queryKind := r.URL.Query().Get("queryKind")
	status := r.URL.Query().Get("status")

	limit := 100
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	offset := 0
	if offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	var prewhereConditions []string
	var whereConditions []string

	if timeRange != "" {
		if duration, ok := timeRangeDurations[timeRange]; ok {
			prewhereConditions = append(prewhereConditions,
				fmt.Sprintf("event_time >= now() - INTERVAL %s", duration))
		}
	}

	if search != "" {
		escaped := escapeString(search)
		whereConditions = append(whereConditions,
			fmt.Sprintf("(query ILIKE '%%%s%%' OR user ILIKE '%%%s%%')", escaped, escaped))
	}

	if queryKind != "" {
		normalized := strings.ToLower(strings.TrimSpace(queryKind))
		if normalized != "" && normalized != "all" {
			escaped := escapeString(normalized)
			whereConditions = append(whereConditions,
				fmt.Sprintf("lowerUTF8(query_kind) = '%s'", escaped))
		}
	}

	if status != "" {
		switch status {
		case "success":
			whereConditions = append(whereConditions, "exception_code = 0")
		case "error":
			whereConditions = append(whereConditions, "exception_code != 0")
		}
	}

	sql := `SELECT
		type, event_time, query_start_time, query_duration_ms,
		read_rows, read_bytes, written_rows, written_bytes,
		result_rows, result_bytes, memory_usage,
		query, query_kind, user, exception_code, exception,
		is_initial_query, databases, tables
	FROM system.query_log`

	if len(prewhereConditions) > 0 {
		sql += "\nPREWHERE " + strings.Join(prewhereConditions, " AND ")
	}
	if len(whereConditions) > 0 {
		sql += "\nWHERE " + strings.Join(whereConditions, " AND ")
	}
	sql += "\nORDER BY event_time DESC"
	sql += fmt.Sprintf("\nLIMIT %d OFFSET %d", limit, offset)
	sql += "\nFORMAT JSON"

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID, sql,
		session.ClickhouseUser, password,
		60*time.Second,
	)
	if err != nil {
		slog.Warn("Failed to query system.query_log", "error", err, "connection", session.ConnectionID)

		if shouldFallbackToQueryThreadLog(err) {
			fallbackSQL := strings.Replace(sql, "system.query_log", "system.query_thread_log", 1)
			result, err = h.Gateway.ExecuteQuery(
				session.ConnectionID, fallbackSQL,
				session.ClickhouseUser, password,
				60*time.Second,
			)
			if err != nil {
				slog.Warn("Fallback to query_thread_log also failed", "error", err)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		} else {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": result.Data,
		"meta": result.Meta,
	})
}

func shouldFallbackToQueryThreadLog(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "system.query_log") && strings.Contains(msg, "unknown_table") {
		return true
	}
	if strings.Contains(msg, "unknown table expression identifier") && strings.Contains(msg, "system.query_log") {
		return true
	}
	return false
}
