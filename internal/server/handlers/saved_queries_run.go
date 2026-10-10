// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// parseStoredParams decodes a saved query's stored parameters JSON into a map.
func parseStoredParams(stored *string) map[string]string {
	out := map[string]string{}
	if stored == nil || strings.TrimSpace(*stored) == "" {
		return out
	}
	_ = json.Unmarshal([]byte(*stored), &out)
	return out
}

// Run executes a saved query with bind parameters and returns the result as JSON.
// Stored parameter defaults are merged with values supplied in the request
// (request values win). Pro-only.
func (h *SavedQueriesHandler) Run(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Query ID is required")
		return
	}

	sq, err := h.DB.GetSavedQueryByID(id)
	if err != nil {
		slog.Error("Failed to get saved query for run", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved query")
		return
	}
	if sq == nil {
		writeError(w, http.StatusNotFound, "Saved query not found")
		return
	}

	var body struct {
		Params        map[string]string `json:"params"`
		Timeout       int               `json:"timeout"`
		MaxResultRows int               `json:"maxResultRows"`
	}
	// Body is optional — a parameterless saved query can be run with no body.
	_ = json.NewDecoder(r.Body).Decode(&body)

	query := strings.TrimSpace(sq.Query)
	if query == "" {
		writeError(w, http.StatusBadRequest, "Saved query is empty")
		return
	}

	// Merge stored defaults with request-supplied params (request wins).
	merged := parseStoredParams(sq.Parameters)
	for k, v := range body.Params {
		merged[k] = v
	}

	timeout := 30 * time.Second
	if body.Timeout > 0 {
		timeout = time.Duration(body.Timeout) * time.Second
	}
	if timeout > maxQueryTimeout {
		timeout = maxQueryTimeout
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	start := time.Now()
	result, err := h.Gateway.ExecuteQueryWithSettings(
		session.ConnectionID,
		query,
		session.ClickhouseUser,
		password,
		buildParamSettings(merged),
		timeout,
	)
	elapsed := time.Since(start).Milliseconds()
	if err != nil {
		slog.Warn("Saved query run failed", "error", err, "id", id, "connection", session.ConnectionID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "saved_query.run",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(sq.Name),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, executeQueryResponse{
		Success:    true,
		Data:       result.Data,
		Meta:       result.Meta,
		Statistics: result.Stats,
		Rows:       countRows(result.Data),
		ElapsedMS:  elapsed,
	})
}
