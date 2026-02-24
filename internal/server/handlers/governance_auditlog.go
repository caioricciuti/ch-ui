package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// ---------- GET /audit-logs ----------

func (h *GovernanceHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	timeRange := strings.TrimSpace(r.URL.Query().Get("timeRange"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	logs, err := h.DB.GetAuditLogsFiltered(limit, timeRange, action, username, search)
	if err != nil {
		slog.Error("Failed to get audit logs", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve audit logs"})
		return
	}

	if logs == nil {
		logs = []database.AuditLog{}
	}

	type auditLogResponse struct {
		database.AuditLog
		ParsedDetails interface{} `json:"parsed_details,omitempty"`
	}

	results := make([]auditLogResponse, 0, len(logs))
	for _, log := range logs {
		entry := auditLogResponse{AuditLog: log}
		if log.Details != nil && *log.Details != "" {
			var parsed interface{}
			if err := json.Unmarshal([]byte(*log.Details), &parsed); err == nil {
				entry.ParsedDetails = parsed
			}
		}
		results = append(results, entry)
	}

	writeJSON(w, http.StatusOK, results)
}
