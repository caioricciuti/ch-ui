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
	r.Get("/", h.List)
	r.Get("/{id}", h.Get)
	r.Post("/", h.Create)
	r.Put("/{id}", h.Update)
	r.Delete("/{id}", h.Delete)
	r.Get("/{id}/runs", h.ListRuns)
	r.Post("/{id}/run", h.ManualRun)
}

// List returns all scheduled jobs.
func (h *SchedulesHandler) List(w http.ResponseWriter, r *http.Request) {
	schedules, err := h.DB.GetSchedules()
	if err != nil {
		slog.Error("Failed to list schedules", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch schedules"})
		return
	}

	if schedules == nil {
		schedules = []database.Schedule{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"schedules": schedules})
}

// Get returns a single scheduled job by ID.
func (h *SchedulesHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule ID is required"})
		return
	}

	schedule, err := h.DB.GetScheduleByID(id)
	if err != nil {
		slog.Error("Failed to get schedule", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch schedule"})
		return
	}
	if schedule == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Schedule not found"})
		return
	}

	writeJSON(w, http.StatusOK, schedule)
}

// Create creates a new scheduled job.
func (h *SchedulesHandler) Create(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
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
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	cronExpr := strings.TrimSpace(body.Cron)
	savedQueryID := strings.TrimSpace(body.SavedQueryID)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name is required"})
		return
	}
	if cronExpr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Cron expression is required"})
		return
	}
	if !scheduler.ValidateCron(cronExpr) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid cron expression"})
		return
	}
	if savedQueryID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Saved query ID is required"})
		return
	}

	// Verify saved query exists
	savedQuery, err := h.DB.GetSavedQueryByID(savedQueryID)
	if err != nil {
		slog.Error("Failed to verify saved query", "error", err, "saved_query_id", savedQueryID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to verify saved query"})
		return
	}
	if savedQuery == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Saved query not found"})
		return
	}

	timezone := strings.TrimSpace(body.Timezone)
	if timezone == "" {
		timezone = "UTC"
	}

	timeoutMs := 60000
	if body.TimeoutMs != nil && *body.TimeoutMs > 0 {
		timeoutMs = *body.TimeoutMs
	}

	connectionID := strings.TrimSpace(body.ConnectionID)
	if connectionID == "" {
		if savedQuery.ConnectionID != nil {
			connectionID = *savedQuery.ConnectionID
		}
		if connectionID == "" {
			connectionID = session.ConnectionID
		}
	}

	id, err := h.DB.CreateSchedule(name, savedQueryID, connectionID, cronExpr, timezone, session.ClickhouseUser, timeoutMs)
	if err != nil {
		slog.Error("Failed to create schedule", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create schedule"})
		return
	}

	// Set next run time
	next := scheduler.ComputeNextRun(cronExpr, time.Now().UTC())
	if next != nil {
		h.DB.UpdateScheduleStatus(id, "", "", next)
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "schedule.created",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule ID is required"})
		return
	}

	existing, err := h.DB.GetScheduleByID(id)
	if err != nil {
		slog.Error("Failed to get schedule for update", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch schedule"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Scheduled job not found"})
		return
	}

	var body struct {
		Name      *string `json:"name"`
		Cron      *string `json:"cron"`
		Timezone  *string `json:"timezone"`
		Enabled   *bool   `json:"enabled"`
		TimeoutMs *int    `json:"timeout_ms"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
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
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Name is required"})
			return
		}
		name = n
		changed = true
	}
	if body.Cron != nil {
		c := strings.TrimSpace(*body.Cron)
		if c == "" || !scheduler.ValidateCron(c) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid cron expression"})
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
		timezone = tz
		changed = true
	}
	if body.Enabled != nil {
		enabled = *body.Enabled
		changed = true
	}
	if body.TimeoutMs != nil {
		if *body.TimeoutMs <= 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "timeout_ms must be greater than 0"})
			return
		}
		timeoutMs = *body.TimeoutMs
		changed = true
	}

	if !changed {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "No valid fields to update"})
		return
	}

	if err := h.DB.UpdateSchedule(id, name, cron, timezone, enabled, timeoutMs); err != nil {
		slog.Error("Failed to update schedule", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update schedule"})
		return
	}

	// Recompute next run
	if enabled {
		next := scheduler.ComputeNextRun(cron, time.Now().UTC())
		h.DB.UpdateScheduleStatus(id, "", "", next)
	} else {
		h.DB.UpdateScheduleStatus(id, "", "", nil)
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "schedule.updated",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// Delete removes a scheduled job.
func (h *SchedulesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule ID is required"})
		return
	}

	existing, err := h.DB.GetScheduleByID(id)
	if err != nil {
		slog.Error("Failed to get schedule for delete", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch schedule"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Scheduled job not found"})
		return
	}

	if err := h.DB.DeleteSchedule(id); err != nil {
		slog.Error("Failed to delete schedule", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete schedule"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "schedule.deleted",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(existing.Name),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ListRuns returns the execution history for a scheduled job.
func (h *SchedulesHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule ID is required"})
		return
	}

	schedule, err := h.DB.GetScheduleByID(id)
	if err != nil {
		slog.Error("Failed to get schedule for runs", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch schedule"})
		return
	}
	if schedule == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Scheduled job not found"})
		return
	}

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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch runs"})
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Schedule ID is required"})
		return
	}

	schedule, err := h.DB.GetScheduleByID(id)
	if err != nil {
		slog.Error("Failed to get schedule for manual run", "error", err, "id", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch schedule"})
		return
	}
	if schedule == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Scheduled job not found"})
		return
	}

	// Get the saved query
	savedQuery, err := h.DB.GetSavedQueryByID(schedule.SavedQueryID)
	if err != nil {
		slog.Error("Failed to fetch saved query for manual run", "error", err, "saved_query_id", schedule.SavedQueryID)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to fetch saved query"})
		return
	}
	if savedQuery == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Saved query not found"})
		return
	}

	// Determine connection
	connectionID := session.ConnectionID
	if schedule.ConnectionID != nil && *schedule.ConnectionID != "" {
		connectionID = *schedule.ConnectionID
	}

	// Decrypt credentials
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password for manual run", "error", err, "schedule", id)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	timeout := time.Duration(schedule.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	// Create a run record
	runID, err := h.DB.CreateScheduleRun(id, "running")
	if err != nil {
		slog.Error("Failed to create schedule run", "error", err, "schedule", id)
	}

	// Execute query
	start := time.Now()
	result, execErr := h.Gateway.ExecuteQuery(connectionID, savedQuery.Query, session.ClickhouseUser, password, timeout)
	elapsed := time.Since(start).Milliseconds()

	status := "success"
	var runErr string
	rowCount := 0
	if execErr != nil {
		status = "error"
		runErr = execErr.Error()
	} else if result != nil {
		rowCount = countRows(result.Data)
	}

	// Update run record
	if runID != "" {
		h.DB.UpdateScheduleRun(runID, status, rowCount, int(elapsed), runErr)
	}

	// Update schedule status
	var nextRun *time.Time
	if schedule.Enabled {
		nextRun = scheduler.ComputeNextRun(schedule.Cron, time.Now().UTC())
	}
	h.DB.UpdateScheduleStatus(id, status, runErr, nextRun)

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "schedule.manual_run",
		Username: strPtr(session.ClickhouseUser),
		Details:  strPtr(fmt.Sprintf("status=%s elapsed=%dms", status, elapsed)),
	})

	if execErr != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"run_id":  runID,
			"status":  "error",
			"error":   execErr.Error(),
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
