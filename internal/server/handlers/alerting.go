// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
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
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/sqlalerts"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// AlertingHandler serves the built-in health checks and the SQL threshold
// alerts of the session's connection. Mounted behind RequirePro.
type AlertingHandler struct {
	DB         *database.DB
	Gateway    *tunnel.Gateway
	Config     *config.Config
	Guardrails *governance.GuardrailService
}

// Routes returns the alerting routes. Reads are open to every signed-in
// user; health thresholds are admin-only; SQL alert writes need a writer.
func (h *AlertingHandler) Routes() chi.Router {
	r := chi.NewRouter()
	writer := middleware.RequireWriter()
	admin := middleware.RequireAdmin(h.DB)

	r.Get("/health/settings", h.GetHealthSettings)
	r.With(admin).Put("/health/settings", h.PutHealthSettings)
	r.Get("/health/state", h.GetHealthState)

	r.Get("/sql", h.ListSQLAlerts)
	r.With(writer).Post("/sql", h.CreateSQLAlert)
	r.With(writer).Post("/sql/test", h.TestSQLAlert)
	r.With(writer).Put("/sql/{id}", h.UpdateSQLAlert)
	r.With(writer).Delete("/sql/{id}", h.DeleteSQLAlert)
	r.With(writer).Post("/sql/{id}/run", h.RunSQLAlert)

	r.Get("/evaluations", h.ListEvaluations)
	return r
}

// ── Health checks ────────────────────────────────────────────────────────────

func (h *AlertingHandler) healthSettingsResponse(w http.ResponseWriter, connID string) {
	settings, err := clusterhealth.LoadHealthSettings(h.DB, connID)
	if err != nil {
		slog.Error("Failed to load health alert settings", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load health check settings")
		return
	}
	collection, err := clusterhealth.NewStore(h.DB).GetSettings(connID)
	if err != nil {
		slog.Warn("Failed to load cluster health settings", "error", err)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "settings": settings, "collection_enabled": collection.Enabled})
}

// GetHealthSettings returns the effective configuration of every check.
func (h *AlertingHandler) GetHealthSettings(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	h.healthSettingsResponse(w, session.ConnectionID)
}

// PutHealthSettings stores the configuration of the given checks.
func (h *AlertingHandler) PutHealthSettings(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body struct {
		Settings []clusterhealth.HealthCheckSetting `json:"settings"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	if len(body.Settings) == 0 {
		writeError(w, http.StatusBadRequest, "settings is required")
		return
	}
	rows := make([]database.HealthAlertSetting, 0, len(body.Settings))
	seen := map[string]bool{}
	for _, in := range body.Settings {
		s, err := clusterhealth.ValidateHealthSetting(in)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if seen[s.CheckKind] {
			writeError(w, http.StatusBadRequest, "each check may appear once")
			return
		}
		seen[s.CheckKind] = true
		rows = append(rows, database.HealthAlertSetting{
			CheckKind: s.CheckKind, Enabled: s.Enabled, WarnThreshold: s.WarnThreshold, CritThreshold: s.CritThreshold,
			FreeBytesFloor: s.FreeBytesFloor, ForEvaluations: s.ForEvaluations, ResolveEvaluations: s.ResolveEvaluations,
		})
	}
	if err := h.DB.SaveHealthAlertSettings(session.ConnectionID, rows); err != nil {
		slog.Error("Failed to save health alert settings", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to save health check settings")
		return
	}
	kinds := make([]string, 0, len(rows))
	for _, s := range rows {
		kinds = append(kinds, s.CheckKind)
		if !s.Enabled {
			if err := clusterhealth.CloseCheckIncidents(h.DB, session.ConnectionID, s.CheckKind, time.Now()); err != nil {
				slog.Warn("Failed to close the incidents of a disabled health check", "check", s.CheckKind, "error", err)
			}
		}
	}
	h.audit(session, "health_alerts.settings_updated", strings.Join(kinds, ","))
	h.healthSettingsResponse(w, session.ConnectionID)
}

// GetHealthState returns the pending and firing check nodes.
func (h *AlertingHandler) GetHealthState(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	states, err := h.DB.ListHealthAlertStates(session.ConnectionID, true)
	if err != nil {
		slog.Error("Failed to list health alert state", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load health check state")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "states": states})
}

// ── SQL alerts ───────────────────────────────────────────────────────────────

type sqlAlertInput struct {
	Name               string  `json:"name"`
	SQL                string  `json:"sql"`
	Mode               string  `json:"mode"`
	Comparator         string  `json:"comparator"`
	Threshold          float64 `json:"threshold"`
	Severity           string  `json:"severity"`
	IntervalSeconds    int     `json:"interval_seconds"`
	TimeoutSeconds     int     `json:"timeout_seconds"`
	ForEvaluations     int     `json:"for_evaluations"`
	ResolveEvaluations int     `json:"resolve_evaluations"`
	Enabled            bool    `json:"enabled"`
}

func (h *AlertingHandler) runner() *sqlalerts.Runner {
	var guard sqlalerts.Guardrails
	if h.Guardrails != nil {
		guard = h.Guardrails
	}
	// No background gate: the runner is only used for one-off runs, and
	// the routes are behind RequirePro.
	return sqlalerts.NewRunner(h.DB, h.Gateway, guard, h.Config.AppSecretKey, nil)
}

func (h *AlertingHandler) sessionCreds(r *http.Request) (sqlalerts.Credentials, *middleware.SessionInfo, error) {
	session := middleware.GetSession(r)
	if session == nil {
		return sqlalerts.Credentials{}, nil, fmt.Errorf("not authenticated")
	}
	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		return sqlalerts.Credentials{}, nil, fmt.Errorf("failed to decrypt credentials")
	}
	return sqlalerts.Credentials{User: session.ClickhouseUser, Password: password}, session, nil
}

// checkGuardrails evaluates the alert SQL as the session user; on a block
// it has written the error.
func (h *AlertingHandler) checkGuardrails(w http.ResponseWriter, session *middleware.SessionInfo, sqlText string) bool {
	if h.Guardrails == nil {
		return true
	}
	decision, err := h.Guardrails.EvaluateQuery(session.ConnectionID, session.ClickhouseUser, sqlText, sqlalerts.GuardrailEndpoint)
	if err != nil {
		slog.Error("Guardrail evaluation failed for SQL alert", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to evaluate governance guardrails")
		return false
	}
	if decision.Allowed {
		return true
	}
	msg := "Query blocked by a governance policy"
	if decision.Block != nil && decision.Block.PolicyName != "" {
		msg = "Query blocked by policy " + decision.Block.PolicyName
	}
	writeError(w, http.StatusForbidden, msg)
	return false
}

// decodeSQLAlert reads and validates a body into a; on failure it has
// written the error.
func (h *AlertingHandler) decodeSQLAlert(w http.ResponseWriter, r *http.Request, session *middleware.SessionInfo, a *database.SQLAlert) bool {
	var in sqlAlertInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return false
	}
	a.Name, a.SQL, a.Mode, a.Comparator = in.Name, in.SQL, strings.TrimSpace(in.Mode), strings.TrimSpace(in.Comparator)
	a.Threshold, a.Severity = in.Threshold, strings.TrimSpace(in.Severity)
	a.IntervalSeconds, a.TimeoutSeconds = in.IntervalSeconds, in.TimeoutSeconds
	a.ForEvaluations, a.ResolveEvaluations, a.Enabled = in.ForEvaluations, in.ResolveEvaluations, in.Enabled
	if err := sqlalerts.ValidateAlert(a); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return false
	}
	return h.checkGuardrails(w, session, a.SQL)
}

// loadOwnedAlert loads an alert of the session connection; with forWrite
// only its creator or an admin passes. On failure it has written the error.
func (h *AlertingHandler) loadOwnedAlert(w http.ResponseWriter, r *http.Request, session *middleware.SessionInfo, forWrite bool) *database.SQLAlert {
	a, err := h.DB.GetSQLAlert(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load SQL alert")
		return nil
	}
	if a == nil || a.ConnectionID != session.ConnectionID {
		writeError(w, http.StatusNotFound, "SQL alert not found")
		return nil
	}
	if forWrite && session.UserRole != "admin" && (a.CreatedBy == nil || *a.CreatedBy != middleware.Actor(session)) {
		writeError(w, http.StatusForbidden, "Only the creator or an admin can change this alert")
		return nil
	}
	return a
}

// ListSQLAlerts returns the SQL alerts of the session connection and the
// mode of the background account they run with.
func (h *AlertingHandler) ListSQLAlerts(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	list, err := h.DB.ListSQLAlerts(session.ConnectionID)
	if err != nil {
		slog.Error("Failed to list SQL alerts", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list SQL alerts")
		return
	}
	for i := range list {
		list[i] = sqlalerts.View(list[i])
	}
	mode := "session"
	if account, err := h.DB.GetBackgroundCredential(session.ConnectionID, sqlalerts.Worker); err == nil {
		mode = account.Mode
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "alerts": list, "worker_mode": mode})
}

// CreateSQLAlert stores a SQL alert.
func (h *AlertingHandler) CreateSQLAlert(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	actor := middleware.Actor(session)
	a := database.SQLAlert{ConnectionID: session.ConnectionID, CreatedBy: &actor}
	if !h.decodeSQLAlert(w, r, session, &a) {
		return
	}
	id, err := h.DB.CreateSQLAlert(&a)
	if err != nil {
		slog.Error("Failed to create SQL alert", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create SQL alert")
		return
	}
	h.audit(session, "sql_alert.created", a.Name)
	saved, _ := h.DB.GetSQLAlert(id)
	if saved == nil {
		writeError(w, http.StatusInternalServerError, "Failed to load SQL alert")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]interface{}{"success": true, "alert": sqlalerts.View(*saved)})
}

// UpdateSQLAlert replaces a SQL alert's definition. Changing the query or
// the condition resets its state.
func (h *AlertingHandler) UpdateSQLAlert(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	existing := h.loadOwnedAlert(w, r, session, true)
	if existing == nil {
		return
	}
	before := *existing
	if !h.decodeSQLAlert(w, r, session, existing) {
		return
	}
	reset := before.SQL != existing.SQL || before.Mode != existing.Mode || before.Comparator != existing.Comparator ||
		before.Threshold != existing.Threshold || before.Severity != existing.Severity
	// The update only applies to the state it saw, so the incident it ends
	// is known; a run that moved the state in between means try again.
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		if err = h.DB.UpdateSQLAlert(existing, reset); !errors.Is(err, database.ErrSQLAlertStateChanged) {
			break
		}
		fresh, ferr := h.DB.GetSQLAlert(existing.ID)
		if ferr != nil || fresh == nil {
			err = sql.ErrNoRows
			break
		}
		existing.State = fresh.State
	}
	if err != nil {
		slog.Error("Failed to update SQL alert", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to update SQL alert")
		return
	}
	if existing.State == alerts.StateFiring && (reset || !existing.Enabled) {
		reason := "the alert was turned off"
		if reset {
			reason = "its query or condition changed"
		}
		sqlalerts.CloseIncident(h.DB, &before, reason, time.Now())
	}
	h.audit(session, "sql_alert.updated", existing.Name)
	saved, _ := h.DB.GetSQLAlert(existing.ID)
	if saved == nil {
		writeError(w, http.StatusNotFound, "SQL alert not found")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "alert": sqlalerts.View(*saved)})
}

// DeleteSQLAlert removes a SQL alert.
func (h *AlertingHandler) DeleteSQLAlert(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	existing := h.loadOwnedAlert(w, r, session, true)
	if existing == nil {
		return
	}
	if err := h.DB.DeleteSQLAlert(existing.ID); err != nil && err != sql.ErrNoRows {
		slog.Error("Failed to delete SQL alert", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete SQL alert")
		return
	}
	if existing.State == alerts.StateFiring {
		sqlalerts.CloseIncident(h.DB, existing, "the alert was deleted", time.Now())
	}
	h.audit(session, "sql_alert.deleted", existing.Name)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// TestSQLAlert runs a query once with the session's own credentials and
// stores nothing.
func (h *AlertingHandler) TestSQLAlert(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	var in sqlAlertInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body")
		return
	}
	mode := strings.TrimSpace(in.Mode)
	if mode == "" {
		mode = "value"
	}
	if mode != "value" && mode != "rows" {
		writeError(w, http.StatusBadRequest, "mode must be value or rows")
		return
	}
	comparator := strings.TrimSpace(in.Comparator)
	if !sqlalerts.ValidComparator(comparator) {
		writeError(w, http.StatusBadRequest, "comparator must be gt, gte, lt, lte, eq or ne")
		return
	}
	timeout := in.TimeoutSeconds
	if timeout == 0 {
		timeout = sqlalerts.DefaultTimeout
	}
	if timeout < sqlalerts.MinTimeout || timeout > sqlalerts.MaxTimeout {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("timeout_seconds must be between %d and %d", sqlalerts.MinTimeout, sqlalerts.MaxTimeout))
		return
	}
	if _, err := sqlalerts.ValidateSQL(in.SQL); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := h.runner().Run(session.ConnectionID, c, in.SQL, mode, comparator, in.Threshold, timeout)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true, "value": res.Value, "rows": res.Rows, "rows_preview": res.Preview,
		"firing": res.Firing, "duration_ms": res.DurationMs,
	})
}

// canRecordRun reports whether a manual run of an alert may move its state:
// only for an enabled alert, and only by its creator or an admin. Anyone
// else gets a dry run.
func canRecordRun(session *middleware.SessionInfo, a *database.SQLAlert) bool {
	if !a.Enabled {
		return false
	}
	return session.UserRole == "admin" || (a.CreatedBy != nil && *a.CreatedBy == middleware.Actor(session))
}

// RunSQLAlert evaluates a stored alert now with the session's credentials.
// For the creator or an admin, on an enabled alert, the result is recorded
// like a scheduled run. Otherwise it is a dry run: the result is returned
// and the alert's state, error count and events stay as they are, so a
// disabled alert never opens an incident nothing would close, and another
// user's credentials never move the state or auto-disable it.
func (h *AlertingHandler) RunSQLAlert(w http.ResponseWriter, r *http.Request) {
	c, session, err := h.sessionCreds(r)
	if err != nil {
		writeError(w, http.StatusUnauthorized, err.Error())
		return
	}
	existing := h.loadOwnedAlert(w, r, session, false)
	if existing == nil {
		return
	}
	if !canRecordRun(session, existing) {
		res, err := h.runner().Run(existing.ConnectionID, c, existing.SQL, existing.Mode, existing.Comparator, existing.Threshold, existing.TimeoutSeconds)
		if err != nil {
			writeError(w, http.StatusBadGateway, err.Error())
			return
		}
		reason := "the alert is turned off"
		if existing.Enabled {
			reason = "only its creator or an admin can record a run"
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success": true, "value": res.Value, "firing": res.Firing, "alert": sqlalerts.View(*existing),
			"recorded": false, "reason": reason,
		})
		return
	}
	out, err := h.runner().Evaluate(existing, c)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	saved, _ := h.DB.GetSQLAlert(existing.ID)
	var view interface{}
	if saved != nil {
		view = sqlalerts.View(*saved)
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "value": out.Value, "firing": out.Firing, "alert": view, "recorded": true})
}

// ListEvaluations returns the evaluation history of a health check or a SQL
// alert on the session connection.
func (h *AlertingHandler) ListEvaluations(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	source := r.URL.Query().Get("source")
	if source != "" && source != "health" && source != "sql" {
		writeError(w, http.StatusBadRequest, "source must be health or sql")
		return
	}
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	evals, err := h.DB.ListAlertEvaluations(session.ConnectionID, source, strings.TrimSpace(r.URL.Query().Get("id")), limit)
	if err != nil {
		slog.Error("Failed to list alert evaluations", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list evaluations")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "evaluations": evals})
}

func (h *AlertingHandler) audit(session *middleware.SessionInfo, action, details string) {
	if err := h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         action,
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(details),
	}); err != nil {
		slog.Warn("Failed to audit alerting change", "action", action, "error", err)
	}
}
