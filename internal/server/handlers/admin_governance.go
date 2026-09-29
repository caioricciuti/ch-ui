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

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// governanceSettingsResponse is the shape returned by the governance settings
// endpoints so the frontend can render the toggle state + last-change metadata.
type governanceSettingsResponse struct {
	SyncEnabled     bool   `json:"sync_enabled"`
	UpdatedAt       string `json:"updated_at"`
	UpdatedBy       string `json:"updated_by"`
	BannerDismissed bool   `json:"banner_dismissed"`
	SyncerRunning   bool   `json:"syncer_running"`
}

// GetGovernanceSettings returns the current governance sync toggle state.
func (h *AdminHandler) GetGovernanceSettings(w http.ResponseWriter, r *http.Request) {
	resp := h.buildGovernanceSettingsResponse()
	writeJSON(w, http.StatusOK, resp)
}

// UpdateGovernanceSettings flips the governance sync toggle and, if the syncer
// handle is wired in, starts or stops the background goroutine accordingly.
func (h *AdminHandler) UpdateGovernanceSettings(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	var body struct {
		SyncEnabled     *bool `json:"sync_enabled"`
		BannerDismissed *bool `json:"banner_dismissed"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	actor, chUser := "unknown", ""
	if session != nil {
		actor, chUser = middleware.Actor(session), session.ClickhouseUser
	}

	// Governance sync is Pro. Turning it on needs a license that is active or
	// in grace; turning it off is always allowed.
	if body.SyncEnabled != nil && *body.SyncEnabled && (h.Config == nil || h.Config.ProAccess() == config.ProNone) {
		writeError(w, http.StatusPaymentRequired, "Pro license required to enable governance sync")
		return
	}

	if body.SyncEnabled != nil {
		if err := h.DB.SetGovernanceSyncEnabled(*body.SyncEnabled, actor); err != nil {
			slog.Error("Failed to persist governance sync setting", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to save setting")
			return
		}

		if h.GovSyncer != nil {
			if *body.SyncEnabled {
				h.GovSyncer.StartBackground()
			} else {
				h.GovSyncer.Stop()
			}
		}

		details := fmt.Sprintf(`{"sync_enabled":%t}`, *body.SyncEnabled)
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:         "governance.sync_toggle",
			Username:       strPtr(actor),
			ClickhouseUser: strPtr(chUser),
			Details:        strPtr(details),
			IPAddress:      strPtr(r.RemoteAddr),
		})
	}

	if body.BannerDismissed != nil && *body.BannerDismissed {
		if err := h.DB.SetSetting(database.SettingGovernanceUpgradeBanner, "true"); err != nil {
			slog.Warn("Failed to persist governance banner dismissal", "error", err)
		}
	}

	writeJSON(w, http.StatusOK, h.buildGovernanceSettingsResponse())
}

func (h *AdminHandler) buildGovernanceSettingsResponse() governanceSettingsResponse {
	enabled := h.DB.GovernanceSyncEnabled()
	updatedAt, _ := h.DB.GetSetting(database.SettingGovernanceSyncUpdatedAt)
	updatedBy, _ := h.DB.GetSetting(database.SettingGovernanceSyncUpdatedBy)
	bannerDismissed, _ := h.DB.GetSetting(database.SettingGovernanceUpgradeBanner)

	running := false
	if h.GovSyncer != nil {
		running = h.GovSyncer.IsRunning()
	}

	return governanceSettingsResponse{
		SyncEnabled:     enabled,
		UpdatedAt:       updatedAt,
		UpdatedBy:       updatedBy,
		BannerDismissed: bannerDismissed == "true",
		SyncerRunning:   running,
	}
}
