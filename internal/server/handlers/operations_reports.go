// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti. See LICENSE.BSL.

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/operations"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

type OperationsReportsHandler struct {
	DB     *database.DB
	Config *config.Config
	Runner *operations.Runner
}

// Reports contain persisted system-wide aggregates collected by an explicitly
// configured account. Restrict them to administrators, including reads.
func (h *OperationsReportsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(middleware.RequireAdmin(h.DB))
	r.Get("/", h.list)
	r.Get("/settings", h.settings)
	r.Put("/settings", h.saveSettings)
	r.Get("/channels", h.channels)
	r.Post("/generate", h.generate)
	r.Post("/{id}/send", h.send)
	return r
}

func (h *OperationsReportsHandler) list(w http.ResponseWriter, r *http.Request) {
	reports, err := h.DB.ListOperationsReports(middleware.GetSession(r).ConnectionID, 52)
	if err != nil {
		writeError(w, 500, "Could not load reports")
		return
	}
	writeJSON(w, 200, map[string]any{"reports": reports})
}

func (h *OperationsReportsHandler) settings(w http.ResponseWriter, r *http.Request) {
	s, err := h.DB.GetOperationsReportSettings(middleware.GetSession(r).ConnectionID)
	if err != nil {
		writeError(w, 500, "Could not load report settings")
		return
	}
	writeJSON(w, 200, s)
}

func (h *OperationsReportsHandler) channels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.DB.ListAlertChannels()
	if err != nil {
		writeError(w, 500, "Could not load email channels")
		return
	}
	out := []map[string]string{}
	for _, c := range channels {
		if c.IsActive {
			out = append(out, map[string]string{"id": c.ID, "name": c.Name, "type": c.ChannelType})
		}
	}
	writeJSON(w, 200, map[string]any{"channels": out})
}

func (h *OperationsReportsHandler) saveSettings(w http.ResponseWriter, r *http.Request) {
	var s database.OperationsReportSettings
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 16384)).Decode(&s); err != nil {
		writeError(w, 400, "Invalid report settings")
		return
	}
	s.ConnectionID = middleware.GetSession(r).ConnectionID
	for i := range s.Recipients {
		s.Recipients[i] = strings.TrimSpace(s.Recipients[i])
	}
	if err := operations.ValidateSettings(s); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if s.ChannelID != "" {
		c, err := h.DB.GetAlertChannelByID(s.ChannelID)
		if err != nil {
			writeError(w, 500, "Could not load email channel")
			return
		}
		if c == nil || !c.IsActive {
			writeError(w, 400, "Choose an active email channel")
			return
		}
	}
	if s.Enabled {
		account, err := h.DB.GetBackgroundCredential(s.ConnectionID, "operations.report")
		if err != nil {
			writeError(w, 500, "Could not load background account")
			return
		}
		if account.Mode != "service_account" {
			writeError(w, 400, "Configure a dedicated Weekly operations reports account in Admin → Connections before enabling the schedule")
			return
		}
	}
	s.NextRunAt = operations.NextWeekly(time.Now(), s.Weekday, s.Hour).Format(time.RFC3339)
	if err := h.DB.SaveOperationsReportSettings(s); err != nil {
		writeError(w, 500, "Could not save report settings")
		return
	}
	h.audit(r, "operations.report.settings_updated")
	h.settings(w, r)
}

func (h *OperationsReportsHandler) generate(w http.ResponseWriter, r *http.Request) {
	sess := middleware.GetSession(r)
	password, err := crypto.Decrypt(sess.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		writeError(w, 500, "Could not read connection credentials")
		return
	}
	actor := sess.AuthSubject
	if actor == "" {
		actor = sess.ClickhouseUser
	}
	report, err := h.Runner.Generate(r.Context(), sess.ConnectionID, sess.ClickhouseUser, password, actor, "", time.Now(), database.OperationsReportSettings{})
	if err != nil {
		writeError(w, 502, "Could not generate report. Check connection availability and SELECT access to system.query_log.")
		return
	}
	h.audit(r, "operations.report.generated")
	writeJSON(w, http.StatusCreated, report)
}

func (h *OperationsReportsHandler) send(w http.ResponseWriter, r *http.Request) {
	conn := middleware.GetSession(r).ConnectionID
	report, err := h.DB.GetOperationsReport(conn, chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, 500, "Could not load report")
		return
	}
	if report == nil {
		writeError(w, 404, "Report not found")
		return
	}
	if report.DeliveryStatus == "queued" || report.DeliveryStatus == "retry" || report.DeliveryStatus == "sending" {
		writeError(w, 409, "This report is already queued for email delivery")
		return
	}
	s, err := h.DB.GetOperationsReportSettings(conn)
	if err != nil {
		writeError(w, 500, "Could not load email settings")
		return
	}
	if s.ChannelID == "" || len(s.Recipients) == 0 {
		writeError(w, 400, "Save an email channel and recipients first")
		return
	}
	if err := operations.ValidateSettings(s); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := h.DB.QueueOperationsReport(conn, report.ID, s.ChannelID, s.Recipients); err != nil {
		writeError(w, 500, "Could not queue report")
		return
	}
	h.audit(r, "operations.report.email_queued")
	writeJSON(w, http.StatusAccepted, map[string]any{"success": true, "message": "Queued for email delivery"})
}

func (h *OperationsReportsHandler) audit(r *http.Request, action string) {
	s := middleware.GetSession(r)
	actor := s.AuthSubject
	if actor == "" {
		actor = s.ClickhouseUser
	}
	_ = h.DB.CreateAuditLog(database.AuditLogParams{Action: action, Username: &actor, ConnectionID: &s.ConnectionID})
}
