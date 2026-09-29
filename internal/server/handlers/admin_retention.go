package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// retentionSettingsResponse is the shape returned by the retention endpoints
// so the frontend can render per-table windows plus last-run stats.
type retentionSettingsResponse struct {
	Config   database.RetentionConfig `json:"config"`
	Defaults database.RetentionConfig `json:"defaults"`
	LastRun  *database.RetentionStats `json:"last_run"`
}

// GetRetentionSettings returns the current data retention config and the
// stats of the most recent retention run.
func (h *AdminHandler) GetRetentionSettings(w http.ResponseWriter, r *http.Request) {
	resp, err := h.buildRetentionSettingsResponse()
	if err != nil {
		slog.Error("Failed to load retention config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load retention settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateRetentionSettings persists new per-table retention windows. Fields
// omitted from the body keep their current value; 0 disables pruning for that
// table.
func (h *AdminHandler) UpdateRetentionSettings(w http.ResponseWriter, r *http.Request) {
	cfg, err := h.DB.GetRetentionConfig()
	if err != nil {
		slog.Error("Failed to load retention config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load retention settings")
		return
	}

	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.DB.SetRetentionConfig(cfg); err != nil {
		slog.Error("Failed to save retention config", "error", err)
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	actor, chUser := "unknown", ""
	if session := middleware.GetSession(r); session != nil {
		actor, chUser = middleware.Actor(session), session.ClickhouseUser
	}
	if details, err := json.Marshal(cfg); err == nil {
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:         "retention.config_update",
			Username:       strPtr(actor),
			ClickhouseUser: strPtr(chUser),
			Details:        strPtr(string(details)),
			IPAddress:      strPtr(r.RemoteAddr),
		})
	}

	resp, err := h.buildRetentionSettingsResponse()
	if err != nil {
		slog.Error("Failed to load retention config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load retention settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (h *AdminHandler) buildRetentionSettingsResponse() (retentionSettingsResponse, error) {
	cfg, err := h.DB.GetRetentionConfig()
	if err != nil {
		return retentionSettingsResponse{}, err
	}

	var lastRun *database.RetentionStats
	if stats := h.DB.RetentionStats(); stats.LastRunAt != "" {
		lastRun = &stats
	}

	return retentionSettingsResponse{
		Config:   cfg,
		Defaults: database.DefaultRetentionConfig(),
		LastRun:  lastRun,
	}, nil
}
