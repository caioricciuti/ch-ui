// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/csv"
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
		writeError(w, http.StatusInternalServerError, "Failed to retrieve audit logs")
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

// ---------- GET /audit-logs/export ----------
//
// Admin-only bulk export of the audit trail for offline retention or one-off
// SIEM ingestion. Supports format=csv (default) and format=json. Honors the
// same filters as GetAuditLogs but allows a much larger row cap.
func (h *GovernanceHandler) GetAuditLogsExport(w http.ResponseWriter, r *http.Request) {
	limit := 100000
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000000 {
			limit = parsed
		}
	}

	timeRange := strings.TrimSpace(r.URL.Query().Get("timeRange"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	logs, err := h.DB.GetAuditLogsFiltered(limit, timeRange, action, username, search)
	if err != nil {
		slog.Error("Failed to export audit logs", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to export audit logs")
		return
	}
	if logs == nil {
		logs = []database.AuditLog{}
	}

	if strings.EqualFold(r.URL.Query().Get("format"), "json") {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", `attachment; filename="ch-ui-audit-logs.json"`)
		_ = json.NewEncoder(w).Encode(logs)
		return
	}

	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="ch-ui-audit-logs.csv"`)
	cw := csv.NewWriter(w)
	defer cw.Flush()
	_ = cw.Write([]string{"created_at", "action", "username", "ch_user", "connection_id", "ip_address", "details"})
	for _, l := range logs {
		_ = cw.Write([]string{
			l.CreatedAt,
			l.Action,
			derefStr(l.Username),
			derefStr(l.ChUser),
			derefStr(l.ConnectionID),
			derefStr(l.IPAddress),
			derefStr(l.Details),
		})
	}
}

func derefStr(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}
