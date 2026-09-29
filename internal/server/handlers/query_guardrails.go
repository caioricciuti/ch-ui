// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"log/slog"
	"net/http"

	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func (h *QueryHandler) guardrailsEnabled() bool {
	if h.Guardrails == nil {
		return false
	}
	if h.Config == nil {
		return true
	}
	return h.Config.IsPro()
}

func (h *QueryHandler) enforceGuardrailsForQuery(w http.ResponseWriter, r *http.Request, queryText, requestEndpoint string) bool {
	if !h.guardrailsEnabled() {
		return true
	}
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return false
	}

	decision, err := h.Guardrails.EvaluateQuery(session.ConnectionID, session.ClickhouseUser, queryText, requestEndpoint)
	if err != nil {
		slog.Error("Guardrail pre-exec evaluation failed", "connection", session.ConnectionID, "endpoint", requestEndpoint, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to evaluate governance guardrails")
		return false
	}
	if decision.Allowed {
		return true
	}
	h.writePolicyBlocked(w, decision.Block)
	return false
}

func (h *QueryHandler) enforceGuardrailsForTable(w http.ResponseWriter, r *http.Request, databaseName, tableName, requestEndpoint string) bool {
	if !h.guardrailsEnabled() {
		return true
	}
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return false
	}

	decision, err := h.Guardrails.EvaluateTable(session.ConnectionID, session.ClickhouseUser, databaseName, tableName, requestEndpoint)
	if err != nil {
		slog.Error("Guardrail table pre-exec evaluation failed", "connection", session.ConnectionID, "database", databaseName, "table", tableName, "endpoint", requestEndpoint, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to evaluate governance guardrails")
		return false
	}
	if decision.Allowed {
		return true
	}
	h.writePolicyBlocked(w, decision.Block)
	return false
}

func (h *QueryHandler) writePolicyBlocked(w http.ResponseWriter, block *governance.GuardrailBlock) {
	if block == nil {
		writeJSON(w, http.StatusForbidden, map[string]interface{}{
			"success": false,
			"error":   "Query blocked by governance policy",
			"code":    "policy_blocked",
		})
		return
	}

	writeJSON(w, http.StatusForbidden, map[string]interface{}{
		"success":          false,
		"error":            block.Detail,
		"code":             "policy_blocked",
		"policy_id":        block.PolicyID,
		"policy_name":      block.PolicyName,
		"severity":         block.Severity,
		"enforcement_mode": block.EnforcementMode,
		"violation_id":     block.ViolationID,
	})
}
