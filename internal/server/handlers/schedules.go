// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/cronexpr"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// SchedulesHandler handles scheduled job CRUD and execution.
type SchedulesHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

// Routes registers schedule routes on the given router.
func (h *SchedulesHandler) Routes(r chi.Router) {
	// Jobs are shared workspace objects. Only writers may change or execute
	// them, including jobs that delegate a dedicated account's grants.
	writer := middleware.RequireWriter()
	r.Get("/", h.List)
	r.Get("/{id}", h.Get)
	r.With(writer).Post("/", h.Create)
	r.With(writer).Put("/{id}", h.Update)
	r.With(writer).Delete("/{id}", h.Delete)
	r.Get("/{id}/runs", h.ListRuns)
	r.With(writer).Post("/{id}/run", h.ManualRun)
}

// List returns the scheduled jobs that run on the caller's connection.
func (h *SchedulesHandler) List(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	all, err := h.DB.GetSchedules()
	if err != nil {
		slog.Error("Failed to list schedules", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to fetch schedules")
		return
	}
	queries, err := h.DB.GetSavedQueries()
	if err != nil {
		slog.Error("Failed to list saved queries for schedules", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to fetch schedules")
		return
	}
	byID := make(map[string]*database.SavedQuery, len(queries))
	for i := range queries {
		byID[queries[i].ID] = &queries[i]
	}

	schedules := []database.Schedule{}
	for i := range all {
		sq := byID[all[i].SavedQueryID]
		if scheduleConnection(&all[i], sq, session.ConnectionID) != session.ConnectionID {
			continue
		}
		// Name only: a job keeps running a query that became private, and the
		// list still says which one without exposing its SQL.
		if sq != nil {
			all[i].SavedQueryName = sq.Name
		}
		schedules = append(schedules, all[i])
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"schedules": schedules})
}

// scheduleConnection is the connection a job runs on, resolved as the
// scheduler does. A job with no connection at all falls back to fallback,
// as run_schedule_now does over MCP.
func scheduleConnection(schedule *database.Schedule, savedQuery *database.SavedQuery, fallback string) string {
	if conn := scheduler.ConnectionFor(schedule, savedQuery); conn != "" {
		return conn
	}
	return fallback
}

// scheduleForSession loads the job named in the URL when it runs on the
// caller's connection. Jobs run with their connection's credentials, so one
// on another connection is reported as not found. It returns the job and
// its saved query (nil when that is gone). On failure it writes the
// response and returns nil.
func (h *SchedulesHandler) scheduleForSession(w http.ResponseWriter, r *http.Request, session *middleware.SessionInfo) (*database.Schedule, *database.SavedQuery) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Schedule ID is required")
		return nil, nil
	}
	schedule, err := h.DB.GetScheduleByID(id)
	if err != nil {
		slog.Error("Failed to get schedule", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch schedule")
		return nil, nil
	}
	if schedule == nil {
		writeError(w, http.StatusNotFound, "Scheduled job not found")
		return nil, nil
	}
	savedQuery, err := h.DB.GetSavedQueryByID(schedule.SavedQueryID)
	if err != nil {
		slog.Error("Failed to fetch saved query for schedule", "error", err, "saved_query_id", schedule.SavedQueryID)
		writeError(w, http.StatusInternalServerError, "Failed to fetch saved query")
		return nil, nil
	}
	if scheduleConnection(schedule, savedQuery, session.ConnectionID) != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Scheduled job not found")
		return nil, nil
	}
	return schedule, savedQuery
}

// Get returns a single scheduled job by ID.
func (h *SchedulesHandler) Get(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	schedule, _ := h.scheduleForSession(w, r, session)
	if schedule == nil {
		return
	}

	writeJSON(w, http.StatusOK, schedule)
}

// Create creates a new scheduled job.
func (h *SchedulesHandler) Create(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body struct {
		Name         string `json:"name"`
		Cron         string `json:"cron"`
		SavedQueryID string `json:"saved_query_id"`
		ConnectionID string `json:"connection_id"`
		Timezone     string `json:"timezone"`
		TimeoutMs    *int   `json:"timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if !sessionConnectionOnly(w, session, body.ConnectionID, "Scheduled jobs") {
		return
	}

	name := strings.TrimSpace(body.Name)
	cronExpr := strings.TrimSpace(body.Cron)
	savedQueryID := strings.TrimSpace(body.SavedQueryID)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Name is required")
		return
	}
	if cronExpr == "" {
		writeError(w, http.StatusBadRequest, "Cron expression is required")
		return
	}
	if !cronexpr.ValidateCron(cronExpr) {
		writeError(w, http.StatusBadRequest, "Invalid cron expression")
		return
	}
	if savedQueryID == "" {
		writeError(w, http.StatusBadRequest, "Saved query ID is required")
		return
	}

	// Verify the saved query exists and the caller can see it.
	savedQuery, err := h.DB.GetSavedQueryVisibleTo(savedQueryID, byIDViewer(session))
	if err != nil {
		slog.Error("Failed to verify saved query", "error", err, "saved_query_id", savedQueryID)
		writeError(w, http.StatusInternalServerError, "Failed to verify saved query")
		return
	}
	if savedQuery == nil {
		writeError(w, http.StatusBadRequest, "Saved query not found")
		return
	}

	timezone := strings.TrimSpace(body.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		writeError(w, http.StatusBadRequest, "Unknown timezone")
		return
	}

	timeoutMs := 60000
	if body.TimeoutMs != nil && *body.TimeoutMs > 0 {
		timeoutMs = *body.TimeoutMs
	}

	// A job runs on the caller's connection, never on another ClickHouse
	// server the caller has not signed in to.
	connectionID := session.ConnectionID

	id, err := h.DB.CreateSchedule(name, savedQueryID, connectionID, cronExpr, timezone, middleware.Actor(session), timeoutMs)
	if err != nil {
		slog.Error("Failed to create schedule", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create schedule")
		return
	}

	// Set next run time
	next := cronexpr.ComputeNextRunInZone(cronExpr, timezone, time.Now().UTC())
	if next != nil {
		if err := h.DB.SetScheduleNextRun(id, next); err != nil {
			slog.Error("Failed to set schedule next run", "error", err, "id", id)
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "schedule.created",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr(name),
	})

	schedule, err := h.DB.GetScheduleByID(id)
	if err != nil || schedule == nil {
		writeJSON(w, http.StatusCreated, map[string]string{"id": id})
		return
	}

	writeJSON(w, http.StatusCreated, schedule)
}

// Update updates an existing scheduled job.
func (h *SchedulesHandler) Update(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	existing, _ := h.scheduleForSession(w, r, session)
	if existing == nil {
		return
	}
	id := existing.ID

	var body struct {
		Name         *string `json:"name"`
		Cron         *string `json:"cron"`
		Timezone     *string `json:"timezone"`
		Enabled      *bool   `json:"enabled"`
		TimeoutMs    *int    `json:"timeout_ms"`
		ConnectionID string  `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if !sessionConnectionOnly(w, session, body.ConnectionID, "Scheduled jobs") {
		return
	}

	name := existing.Name
	cron := existing.Cron
	timezone := existing.Timezone
	enabled := existing.Enabled
	timeoutMs := existing.TimeoutMs

	changed := false
	if body.Name != nil {
		n := strings.TrimSpace(*body.Name)
		if n == "" {
			writeError(w, http.StatusBadRequest, "Name is required")
			return
		}
		name = n
		changed = true
	}
	if body.Cron != nil {
		c := strings.TrimSpace(*body.Cron)
		if c == "" || !cronexpr.ValidateCron(c) {
			writeError(w, http.StatusBadRequest, "Invalid cron expression")
			return
		}
		cron = c
		changed = true
	}
	if body.Timezone != nil {
		tz := strings.TrimSpace(*body.Timezone)
		if tz == "" {
			tz = "UTC"
		}
		if _, err := time.LoadLocation(tz); err != nil {
			writeError(w, http.StatusBadRequest, "Unknown timezone")
			return
		}
		timezone = tz
		changed = true
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
		changed = true
	}
	if body.TimeoutMs != nil {
		if *body.TimeoutMs <= 0 {
			writeError(w, http.StatusBadRequest, "timeout_ms must be greater than 0")
			return
		}
		timeoutMs = *body.TimeoutMs
		changed = true
	}

	if !changed {
		writeError(w, http.StatusBadRequest, "No valid fields to update")
		return
	}

	if err := h.DB.UpdateSchedule(id, name, cron, timezone, enabled, timeoutMs); err != nil {
		slog.Error("Failed to update schedule", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update schedule")
		return
	}

	// Recompute next run. Only next_run_at moves: an edit is not a run, so
	// the last run time and status stay as they were.
	var next *time.Time
	if enabled {
		next = cronexpr.ComputeNextRunInZone(cron, timezone, time.Now().UTC())
	}
	if err := h.DB.SetScheduleNextRun(id, next); err != nil {
		slog.Error("Failed to set schedule next run", "error", err, "id", id)
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "schedule.updated",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr(name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Delete removes a scheduled job.
func (h *SchedulesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	existing, _ := h.scheduleForSession(w, r, session)
	if existing == nil {
		return
	}
	id := existing.ID

	if err := h.DB.DeleteSchedule(id); err != nil {
		slog.Error("Failed to delete schedule", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete schedule")
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "schedule.deleted",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		Details:        strPtr(existing.Name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ListRuns returns the execution history for a scheduled job.
func (h *SchedulesHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	schedule, _ := h.scheduleForSession(w, r, session)
	if schedule == nil {
		return
	}
	id := schedule.ID

	limit := 50
	if raw := r.URL.Query().Get("limit"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 && parsed <= 500 {
			limit = parsed
		}
	}
	offset := 0
	if raw := r.URL.Query().Get("offset"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed >= 0 && parsed <= 1000000 {
			offset = parsed
		}
	}

	runs, err := h.DB.GetScheduleRuns(id, limit+1, offset)
	if err != nil {
		slog.Error("Failed to list schedule runs", "error", err, "schedule", id)
		writeError(w, http.StatusInternalServerError, "Failed to fetch runs")
		return
	}

	if runs == nil {
		runs = []database.ScheduleRun{}
	}

	hasMore := len(runs) > limit
	if hasMore {
		runs = runs[:limit]
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"runs":        runs,
		"has_more":    hasMore,
		"next_offset": offset + len(runs),
	})
}

// ManualRun triggers a manual execution of a scheduled job.
func (h *SchedulesHandler) ManualRun(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	// The job runs on its own connection with the caller's ClickHouse
	// credentials, so it must be the connection the caller signed in to.
	// scheduleForSession refuses any other.
	schedule, savedQuery := h.scheduleForSession(w, r, session)
	if schedule == nil {
		return
	}
	id := schedule.ID
	if savedQuery == nil {
		writeError(w, http.StatusBadRequest, "Saved query not found")
		return
	}
	connectionID := session.ConnectionID

	// Decrypt credentials
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password for manual run", "error", err, "schedule", id)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	res := scheduler.RunNow(r.Context(), h.DB, h.Gateway, schedule, savedQuery.Query, connectionID, session.ClickhouseUser, password)
	runID, status, elapsed := res.RunID, res.Status, res.ElapsedMs

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "schedule.manual_run",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   &connectionID,
		Details:        strPtr(fmt.Sprintf("status=%s elapsed=%dms", status, elapsed)),
	})

	if res.Status == "error" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"run_id":  runID,
			"status":  "error",
			"error":   res.Error,
			"elapsed": elapsed,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"run_id":  runID,
		"status":  "success",
		"elapsed": elapsed,
	})
}
