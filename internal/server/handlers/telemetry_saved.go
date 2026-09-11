package handlers

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/telemetry/monitor"
)

// ── Saved searches ─────────────────────────────────────────────────

type savedSearchInput struct {
	Kind        string  `json:"kind"`
	Name        string  `json:"name"`
	Query       string  `json:"query"`
	RangePreset string  `json:"range_preset"`
	SourceID    *string `json:"source_id"`
}

func (in *savedSearchInput) validate() error {
	in.Name = strings.TrimSpace(in.Name)
	in.Kind = strings.TrimSpace(in.Kind)
	in.RangePreset = strings.TrimSpace(in.RangePreset)
	if in.Name == "" {
		return fmt.Errorf("name is required")
	}
	if len(in.Name) > 120 {
		return fmt.Errorf("name is limited to 120 characters")
	}
	if in.Kind != string(telemetry.KindLogs) && in.Kind != string(telemetry.KindTraces) {
		return fmt.Errorf("kind must be logs or traces")
	}
	if len(in.Query) > 4000 {
		return fmt.Errorf("query is limited to 4000 characters")
	}
	if in.RangePreset == "" {
		in.RangePreset = "1h"
	}
	if len(in.RangePreset) > 64 {
		return fmt.Errorf("range_preset is too long")
	}
	return nil
}

// ListSavedSearches returns the saved searches of the session connection.
func (h *TelemetryHandler) ListSavedSearches(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	searches, err := h.DB.ListTelemetrySavedSearches(session.ConnectionID)
	if err != nil {
		slog.Error("Failed to list telemetry saved searches", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list saved searches")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "searches": searches})
}

// CreateSavedSearch stores a named search.
func (h *TelemetryHandler) CreateSavedSearch(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var in savedSearchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.SourceID != nil && *in.SourceID != "" && h.loadSource(w, *in.SourceID, session.ConnectionID) == nil {
		return
	}
	s := &database.TelemetrySavedSearch{
		ConnectionID: session.ConnectionID, Kind: in.Kind, Name: in.Name, Query: in.Query,
		RangePreset: in.RangePreset, SourceID: in.SourceID, CreatedBy: strPtr(session.ClickhouseUser),
	}
	id, err := h.DB.CreateTelemetrySavedSearch(s)
	if err != nil {
		slog.Error("Failed to create telemetry saved search", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to save search")
		return
	}
	h.audit("telemetry.search.saved", session, in.Name)
	saved, _ := h.DB.GetTelemetrySavedSearch(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "search": saved})
}

// UpdateSavedSearch replaces a saved search's fields.
func (h *TelemetryHandler) UpdateSavedSearch(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetTelemetrySavedSearch(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load saved search")
		return
	}
	if existing == nil || existing.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Saved search not found")
		return
	}
	var in savedSearchInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if err := in.validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if in.SourceID != nil && *in.SourceID != "" && h.loadSource(w, *in.SourceID, session.ConnectionID) == nil {
		return
	}
	existing.Kind, existing.Name, existing.Query, existing.RangePreset, existing.SourceID = in.Kind, in.Name, in.Query, in.RangePreset, in.SourceID
	if err := h.DB.UpdateTelemetrySavedSearch(existing); err != nil {
		slog.Error("Failed to update telemetry saved search", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update saved search")
		return
	}
	h.audit("telemetry.search.updated", session, in.Name)
	saved, _ := h.DB.GetTelemetrySavedSearch(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "search": saved})
}

// DeleteSavedSearch removes a saved search.
func (h *TelemetryHandler) DeleteSavedSearch(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetTelemetrySavedSearch(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load saved search")
		return
	}
	if existing == nil || existing.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Saved search not found")
		return
	}
	if err := h.DB.DeleteTelemetrySavedSearch(id); err != nil && err != sql.ErrNoRows {
		slog.Error("Failed to delete telemetry saved search", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete saved search")
		return
	}
	h.audit("telemetry.search.deleted", session, existing.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ── Monitors ───────────────────────────────────────────────────────

type monitorInput struct {
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	SourceID        string  `json:"source_id"`
	Query           string  `json:"query"`
	WindowSeconds   int     `json:"window_seconds"`
	IntervalSeconds int     `json:"interval_seconds"`
	Comparator      string  `json:"comparator"`
	Threshold       float64 `json:"threshold"`
	Severity        string  `json:"severity"`
	Enabled         bool    `json:"enabled"`
}

func (in *monitorInput) apply(m *database.TelemetryMonitor) {
	m.Name = strings.TrimSpace(in.Name)
	m.Kind = strings.TrimSpace(in.Kind)
	m.SourceID = strings.TrimSpace(in.SourceID)
	m.Query = in.Query
	m.WindowSeconds = in.WindowSeconds
	m.IntervalSeconds = in.IntervalSeconds
	m.Comparator = strings.TrimSpace(in.Comparator)
	m.Threshold = in.Threshold
	m.Severity = strings.TrimSpace(in.Severity)
	m.Enabled = in.Enabled
}

// decodeMonitor reads and validates a monitor body against the session's
// sources; on failure it has already written the error.
func (h *TelemetryHandler) decodeMonitor(w http.ResponseWriter, r *http.Request, connID string, m *database.TelemetryMonitor) bool {
	var in monitorInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return false
	}
	in.apply(m)
	if len(m.Query) > 4000 {
		writeError(w, http.StatusBadRequest, "query is limited to 4000 characters")
		return false
	}
	if err := monitor.ValidateMonitor(m); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	src := h.loadSource(w, m.SourceID, connID)
	if src == nil {
		return false
	}
	if string(src.Kind) != m.Kind {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("source is a %s source, monitor kind is %s", src.Kind, m.Kind))
		return false
	}
	return true
}

// ListMonitors returns the monitors of the session connection.
func (h *TelemetryHandler) ListMonitors(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	monitors, err := h.DB.ListTelemetryMonitors(session.ConnectionID)
	if err != nil {
		slog.Error("Failed to list telemetry monitors", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list monitors")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "monitors": monitors})
}

// CreateMonitor stores a monitor.
func (h *TelemetryHandler) CreateMonitor(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	m := database.TelemetryMonitor{ConnectionID: session.ConnectionID, CreatedBy: strPtr(session.ClickhouseUser)}
	if !h.decodeMonitor(w, r, session.ConnectionID, &m) {
		return
	}
	id, err := h.DB.CreateTelemetryMonitor(&m)
	if err != nil {
		slog.Error("Failed to create telemetry monitor", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create monitor")
		return
	}
	h.audit("telemetry.monitor.created", session, m.Name)
	saved, _ := h.DB.GetTelemetryMonitor(id)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "monitor": saved})
}

// UpdateMonitor replaces a monitor's fields.
func (h *TelemetryHandler) UpdateMonitor(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetTelemetryMonitor(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load monitor")
		return
	}
	if existing == nil || existing.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Monitor not found")
		return
	}
	if !h.decodeMonitor(w, r, session.ConnectionID, existing) {
		return
	}
	if err := h.DB.UpdateTelemetryMonitor(existing); err != nil {
		slog.Error("Failed to update telemetry monitor", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to update monitor")
		return
	}
	h.audit("telemetry.monitor.updated", session, existing.Name)
	saved, _ := h.DB.GetTelemetryMonitor(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "monitor": saved})
}

// DeleteMonitor removes a monitor.
func (h *TelemetryHandler) DeleteMonitor(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetTelemetryMonitor(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load monitor")
		return
	}
	if existing == nil || existing.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Monitor not found")
		return
	}
	if err := h.DB.DeleteTelemetryMonitor(id); err != nil && err != sql.ErrNoRows {
		slog.Error("Failed to delete telemetry monitor", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete monitor")
		return
	}
	h.audit("telemetry.monitor.deleted", session, existing.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// RunMonitor evaluates a monitor now with the session's own credentials.
func (h *TelemetryHandler) RunMonitor(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.creds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetTelemetryMonitor(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load monitor")
		return
	}
	if existing == nil || existing.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "Monitor not found")
		return
	}
	runner := monitor.NewMonitorRunner(h.DB, h.Gateway, h.Config.AppSecretKey)
	value, firing, err := runner.Evaluate(existing, monitor.Credentials{User: c.user, Password: c.password})
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	updated, _ := h.DB.GetTelemetryMonitor(id)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "value": value, "firing": firing, "monitor": updated})
}
