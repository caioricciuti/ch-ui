// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/queryproc"
	"github.com/caioricciuti/ch-ui/internal/reports"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ReportsHandler serves scheduled report delivery. Mounted behind
// RequirePro. Reads are open to every signed-in user; writes, previews and
// sends need a writer, and only the creator or an admin may change or delete
// a report.
type ReportsHandler struct {
	DB     *database.DB
	Config *config.Config
	Runner *reports.Runner
}

// Routes returns the report routes.
func (h *ReportsHandler) Routes() chi.Router {
	r := chi.NewRouter()
	writer := middleware.RequireWriter()
	r.Get("/", h.List)
	r.With(writer).Post("/", h.Create)
	r.Get("/settings", h.GetSettings)
	r.With(middleware.RequireAdmin(h.DB)).Put("/settings", h.PutSettings)
	r.Get("/deliveries/{id}/download", h.Download)
	r.With(writer).Put("/{id}", h.Update)
	r.With(writer).Delete("/{id}", h.Delete)
	r.With(writer).Post("/{id}/preview", h.Preview)
	r.With(writer).Post("/{id}/send", h.Send)
	r.Get("/{id}/deliveries", h.Deliveries)
	return r
}

type reportChannel struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ChannelType string `json:"channel_type"`
	Format      string `json:"format,omitempty"`
	IsActive    bool   `json:"is_active"`
}

// List returns every report, the channels a report can target and the
// report account mode of each connection.
func (h *ReportsHandler) List(w http.ResponseWriter, r *http.Request) {
	list, err := h.DB.ListReportSubscriptions(false)
	if err != nil {
		slog.Error("Failed to list reports", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list reports")
		return
	}
	channels, err := h.DB.ListAlertChannels()
	if err != nil {
		slog.Error("Failed to list alert channels for reports", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list channels")
		return
	}
	outChannels := make([]reportChannel, 0, len(channels))
	for _, c := range channels {
		rc := reportChannel{ID: c.ID, Name: c.Name, ChannelType: c.ChannelType, IsActive: c.IsActive}
		if strings.EqualFold(c.ChannelType, alerts.ChannelTypeWebhook) {
			rc.Format = alerts.WebhookFormatGeneric
			if raw, err := crypto.Decrypt(c.ConfigEncrypted, h.Config.AppSecretKey); err == nil {
				var cfg map[string]interface{}
				if json.Unmarshal([]byte(raw), &cfg) == nil {
					if f, ok := cfg[alerts.WebhookCfgFormat].(string); ok && f != "" {
						rc.Format = f
					}
				}
			}
		}
		outChannels = append(outChannels, rc)
	}
	modes := map[string]string{}
	if conns, err := h.DB.GetConnections(); err == nil {
		for _, c := range conns {
			mode := "session"
			if account, err := h.DB.GetBackgroundCredential(c.ID, reports.Worker); err == nil {
				mode = account.Mode
			}
			modes[c.ID] = mode
		}
	}
	domains, _ := h.DB.GetSetting(reports.SettingRecipientDomains)
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "reports": list, "channels": outChannels, "worker_modes": modes,
		"recipient_domains": reports.ParseDomains(domains),
	})
}

type reportInput struct {
	Name            string                  `json:"name"`
	SourceType      string                  `json:"source_type"`
	SavedQueryID    *string                 `json:"saved_query_id"`
	DashboardID     *string                 `json:"dashboard_id"`
	ConnectionID    string                  `json:"connection_id"`
	Cron            string                  `json:"cron"`
	Timezone        string                  `json:"timezone"`
	Enabled         *bool                   `json:"enabled"`
	Format          string                  `json:"format"`
	RowLimit        int                     `json:"row_limit"`
	InlineRows      int                     `json:"inline_rows"`
	MaxBytes        int                     `json:"max_bytes"`
	TimeoutSeconds  int                     `json:"timeout_seconds"`
	TimeRange       string                  `json:"time_range"`
	Variables       map[string][]string     `json:"variables"`
	SkipIfEmpty     bool                    `json:"skip_if_empty"`
	SubjectTemplate string                  `json:"subject_template"`
	Targets         []database.ReportTarget `json:"targets"`
}

// decode reads and validates a body into s; on failure it has written the
// error.
func (h *ReportsHandler) decode(w http.ResponseWriter, r *http.Request, session *middleware.SessionInfo, s *database.ReportSubscription) bool {
	var in reportInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return false
	}
	s.Name, s.SourceType, s.ConnectionID = in.Name, strings.TrimSpace(in.SourceType), strings.TrimSpace(in.ConnectionID)
	s.SavedQueryID, s.DashboardID = trimmedPtr(in.SavedQueryID), trimmedPtr(in.DashboardID)
	s.Cron, s.Timezone, s.Format = in.Cron, in.Timezone, strings.TrimSpace(in.Format)
	s.RowLimit, s.InlineRows, s.MaxBytes, s.TimeoutSeconds = in.RowLimit, in.InlineRows, in.MaxBytes, in.TimeoutSeconds
	s.TimeRange, s.Variables, s.SkipIfEmpty, s.SubjectTemplate = in.TimeRange, in.Variables, in.SkipIfEmpty, in.SubjectTemplate
	s.Targets = in.Targets
	if in.Enabled != nil {
		s.Enabled = *in.Enabled
	}
	if err := reports.Normalize(s); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if err := validateReportVariables(s.Variables); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	if conn, err := h.DB.GetConnectionByID(s.ConnectionID); err != nil || conn == nil {
		writeError(w, http.StatusBadRequest, "Connection not found")
		return false
	}
	switch s.SourceType {
	case reports.SourceSavedQuery:
		q, err := h.DB.GetSavedQueryByID(*s.SavedQueryID)
		if err != nil || q == nil {
			writeError(w, http.StatusBadRequest, "Saved query not found")
			return false
		}
		if _, err := reports.ValidateSQL(q.Query); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return false
		}
		if _, err := reports.SavedQueryParams(q); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return false
		}
	case reports.SourceDashboard:
		d, err := h.DB.GetDashboardByID(*s.DashboardID)
		if err != nil || d == nil {
			writeError(w, http.StatusBadRequest, "Dashboard not found")
			return false
		}
	}
	domainsRaw, _ := h.DB.GetSetting(reports.SettingRecipientDomains)
	domains := reports.ParseDomains(domainsRaw)
	for i := range s.Targets {
		t := &s.Targets[i]
		channel, err := h.DB.GetAlertChannelByID(t.ChannelID)
		if err != nil || channel == nil {
			writeError(w, http.StatusBadRequest, "Channel not found")
			return false
		}
		t.ChannelName, t.ChannelType = channel.Name, channel.ChannelType
		if strings.EqualFold(channel.ChannelType, alerts.ChannelTypeWebhook) {
			t.Recipients = []string{}
			continue
		}
		if len(t.Recipients) == 0 {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("Add at least one recipient for %s", channel.Name))
			return false
		}
		// The domain list binds analysts; admins may send anywhere.
		if session.UserRole != "admin" {
			if err := reports.CheckDomains(t.Recipients, domains); err != nil {
				writeError(w, http.StatusBadRequest, err.Error())
				return false
			}
		}
	}
	s.NextRunAt = nil
	if s.Enabled {
		if next := reports.NextRun(s, time.Now().UTC()); next != nil {
			v := next.UTC().Format(time.RFC3339)
			s.NextRunAt = &v
		}
	}
	return true
}

func trimmedPtr(p *string) *string {
	if p == nil {
		return nil
	}
	v := strings.TrimSpace(*p)
	if v == "" {
		return nil
	}
	return &v
}

func validateReportVariables(vars map[string][]string) error {
	if len(vars) > 30 {
		return fmt.Errorf("at most 30 variables")
	}
	for name, values := range vars {
		if !queryproc.ValidVarName(name) {
			return fmt.Errorf("variable name %q is not valid", name)
		}
		if len(values) > 100 {
			return fmt.Errorf("variable %s has more than 100 values", name)
		}
		for _, v := range values {
			if len(v) > 1000 {
				return fmt.Errorf("a value of variable %s is longer than 1000 characters", name)
			}
		}
	}
	return nil
}

// load returns the report in the URL; with forWrite only its creator or an
// admin passes. On failure it has written the error.
func (h *ReportsHandler) load(w http.ResponseWriter, r *http.Request, session *middleware.SessionInfo, forWrite bool) *database.ReportSubscription {
	s, err := h.DB.GetReportSubscription(chi.URLParam(r, "id"))
	if err != nil {
		slog.Error("Failed to load report", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load report")
		return nil
	}
	if s == nil {
		writeError(w, http.StatusNotFound, "Report not found")
		return nil
	}
	if forWrite && session.UserRole != "admin" && (s.CreatedBy == nil || *s.CreatedBy != middleware.Actor(session)) {
		writeError(w, http.StatusForbidden, "Only the creator or an admin can change this report")
		return nil
	}
	return s
}

// Create stores a report.
func (h *ReportsHandler) Create(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	actor := middleware.Actor(session)
	s := database.ReportSubscription{CreatedBy: &actor, Enabled: true}
	if !h.decode(w, r, session, &s) {
		return
	}
	id, err := h.DB.CreateReportSubscription(s)
	if err != nil {
		slog.Error("Failed to create report", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create report")
		return
	}
	saved, err := h.DB.GetReportSubscription(id)
	if err != nil || saved == nil {
		writeError(w, http.StatusInternalServerError, "Failed to load report")
		return
	}
	h.audit(session, "report.created", saved, nil)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "report": saved})
}

// Update replaces a report's definition.
func (h *ReportsHandler) Update(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	existing := h.load(w, r, session, true)
	if existing == nil {
		return
	}
	s := *existing
	if !h.decode(w, r, session, &s) {
		return
	}
	if err := h.DB.UpdateReportSubscription(s); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeError(w, http.StatusNotFound, "Report not found")
			return
		}
		slog.Error("Failed to update report", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to update report")
		return
	}
	saved, err := h.DB.GetReportSubscription(s.ID)
	if err != nil || saved == nil {
		writeError(w, http.StatusNotFound, "Report not found")
		return
	}
	h.audit(session, "report.updated", saved, nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "report": saved})
}

// Delete removes a report and its history.
func (h *ReportsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	existing := h.load(w, r, session, true)
	if existing == nil {
		return
	}
	if err := h.DB.DeleteReportSubscription(existing.ID); err != nil && !errors.Is(err, sql.ErrNoRows) {
		slog.Error("Failed to delete report", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete report")
		return
	}
	h.audit(session, "report.deleted", existing, nil)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *ReportsHandler) runner() (*reports.Runner, bool) {
	return h.Runner, h.Runner != nil
}

// Preview renders a report with the report account and sends nothing.
func (h *ReportsHandler) Preview(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	s := h.load(w, r, session, false)
	if s == nil {
		return
	}
	runner, ok := h.runner()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Reports are not available")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Duration(s.TimeoutSeconds+30)*time.Second)
	defer cancel()
	snap, err := runner.Preview(ctx, s)
	if err != nil {
		status := http.StatusBadGateway
		if errors.Is(err, reports.ErrBusy) {
			status = http.StatusTooManyRequests
		}
		writeError(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "subject": snap.Subject, "text": snap.Text, "html": snap.HTML,
		"rows": snap.RowCount, "truncated": snap.Truncated, "empty": snap.Empty,
		"csv_name": snap.CSVName, "csv_bytes": len(snap.CSV),
	})
}

// Send runs a report now and sends it to every target.
func (h *ReportsHandler) Send(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	s := h.load(w, r, session, false)
	if s == nil {
		return
	}
	runner, ok := h.runner()
	if !ok {
		writeError(w, http.StatusServiceUnavailable, "Reports are not available")
		return
	}
	ctx, cancel := context.WithTimeout(context.WithoutCancel(r.Context()), time.Duration(s.TimeoutSeconds)*time.Second+3*time.Minute)
	defer cancel()
	d, err := runner.SendNow(ctx, s)
	if errors.Is(err, reports.ErrBusy) {
		writeError(w, http.StatusTooManyRequests, err.Error())
		return
	}
	extra := map[string]interface{}{"trigger": "manual"}
	if d != nil {
		extra["delivery"] = d.ID
		extra["status"] = d.Status
	}
	h.audit(session, "report.sent_manual", s, extra)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]interface{}{"success": false, "error": err.Error(), "delivery": d})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "delivery": d})
}

// Deliveries returns the latest runs of a report.
func (h *ReportsHandler) Deliveries(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	s := h.load(w, r, session, false)
	if s == nil {
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	list, err := h.DB.ListReportDeliveries(s.ID, limit)
	if err != nil {
		slog.Error("Failed to list report deliveries", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list deliveries")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "deliveries": list})
}

// Download returns the CSV a delivery sent, while its snapshot is kept.
func (h *ReportsHandler) Download(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	d, err := h.DB.GetReportDelivery(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load delivery")
		return
	}
	if d == nil {
		writeError(w, http.StatusNotFound, "Delivery not found")
		return
	}
	var snap reports.Snapshot
	if len(d.Snapshot) == 0 || json.Unmarshal(d.Snapshot, &snap) != nil {
		writeError(w, http.StatusNotFound, "This delivery's content is no longer stored")
		return
	}
	if len(snap.CSV) == 0 {
		writeError(w, http.StatusNotFound, "This delivery has no CSV")
		return
	}
	name := snap.CSVName
	if name == "" {
		name = "report.csv"
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="`+strings.ReplaceAll(name, `"`, "")+`"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(snap.CSV)
}

// GetSettings returns the allowed recipient domains.
func (h *ReportsHandler) GetSettings(w http.ResponseWriter, r *http.Request) {
	raw, _ := h.DB.GetSetting(reports.SettingRecipientDomains)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "recipient_domains": reports.ParseDomains(raw)})
}

// PutSettings stores the allowed recipient domains (admin only).
func (h *ReportsHandler) PutSettings(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body struct {
		RecipientDomains []string `json:"recipient_domains"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	domains := reports.ParseDomains(strings.Join(body.RecipientDomains, ","))
	if len(domains) > 50 {
		writeError(w, http.StatusBadRequest, "At most 50 domains")
		return
	}
	for _, d := range domains {
		if len(d) > 253 || !strings.Contains(d, ".") || strings.ContainsAny(d, "@/ ") {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("%q is not a domain", d))
			return
		}
	}
	if err := h.DB.SetSetting(reports.SettingRecipientDomains, strings.Join(domains, ",")); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save settings")
		return
	}
	details, _ := json.Marshal(map[string]interface{}{"recipient_domains": domains})
	if err := h.DB.CreateAuditLog(database.AuditLogParams{
		Action: "report.settings_updated", Username: strPtr(middleware.Actor(session)), ClickhouseUser: &session.ClickhouseUser,
		Details: strPtr(string(details)),
	}); err != nil {
		slog.Warn("Failed to audit report settings", "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "recipient_domains": domains})
}

func (h *ReportsHandler) audit(session *middleware.SessionInfo, action string, s *database.ReportSubscription, extra map[string]interface{}) {
	recipients := 0
	for _, t := range s.Targets {
		recipients += len(t.Recipients)
	}
	d := map[string]interface{}{"report": s.ID, "name": s.Name, "source": s.SourceType, "channels": len(s.Targets), "recipients": recipients}
	for k, v := range extra {
		d[k] = v
	}
	details, _ := json.Marshal(d)
	conn := s.ConnectionID
	if err := h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   &conn,
		Details:        strPtr(string(details)),
	}); err != nil {
		slog.Warn("Failed to audit report change", "action", action, "error", err)
	}
}
