// SPDX-License-Identifier: BUSL-1.1
package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/incidenttimeline"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

type IncidentTimelineGateway interface {
	IsTunnelOnline(string) bool
	ExecuteQueryWithSettingsCtx(context.Context, string, string, string, string, map[string]string, time.Duration) (*tunnel.QueryResult, error)
}

type IncidentTimelineHandler struct {
	DB      *database.DB
	Gateway IncidentTimelineGateway
	Config  *config.Config
}

func (h *IncidentTimelineHandler) Routes(r chi.Router) {
	r.Get("/", h.Get)
	r.Post("/annotations", h.CreateAnnotation)
	r.Delete("/annotations/{id}", h.DeleteAnnotation)
}

func timelineActor(session *middleware.SessionInfo) string {
	if session.AuthSubject != "" {
		return session.AuthSubject
	}
	return session.ClickhouseUser
}

func timelineSession(w http.ResponseWriter, r *http.Request, admin bool) *middleware.SessionInfo {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return nil
	}
	if session.ConnectionID == "" {
		writeError(w, http.StatusBadRequest, "Select a connection")
		return nil
	}
	if (admin && session.UserRole != "admin") || (!admin && session.UserRole != "admin" && session.UserRole != "analyst") {
		writeError(w, http.StatusForbidden, "Insufficient role for incident timeline")
		return nil
	}
	return session
}

func (h *IncidentTimelineHandler) Get(w http.ResponseWriter, r *http.Request) {
	// This combines retained background-account observations and shared incident
	// details. A connection selector is not an authorization boundary; restrict
	// the combined view to administrators until finer-grained access exists.
	session := timelineSession(w, r, true)
	if session == nil {
		return
	}
	window, err := incidenttimeline.ParseWindow(r.URL.Query().Get("from"), r.URL.Query().Get("to"))
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	store := incidenttimeline.Store{DB: h.DB}
	incidentID := strings.TrimSpace(r.URL.Query().Get("incident_id"))
	if incidentID != "" {
		exists, err := store.IncidentExists(r.Context(), session.ConnectionID, incidentID)
		if err != nil {
			writeError(w, 500, "Could not load incident")
			return
		}
		if !exists {
			writeError(w, 404, "Incident not found on this connection")
			return
		}
	}
	events := []incidenttimeline.Event{}
	coverage := []incidenttimeline.Coverage{}
	appendSource := func(source, message string, rows []incidenttimeline.Event, err error) {
		c := incidenttimeline.Coverage{Source: source, Available: err == nil, Message: message}
		if err != nil {
			slog.Warn("Incident timeline source unavailable", "source", source, "connection", session.ConnectionID, "error", err)
			c.Message = "Source unavailable. Check connection, permissions, table availability and retention."
		} else {
			if len(rows) > incidenttimeline.Limit {
				rows = rows[:incidenttimeline.Limit]
				c.Truncated = true
			}
			c.Events = len(rows)
			events = append(events, rows...)
		}
		coverage = append(coverage, c)
	}
	for _, source := range []string{"incidents", "comments", "deployments", "health"} {
		rows, err := store.Local(r.Context(), session.ConnectionID, source, window)
		message := "Retained records in the selected window; no matching records does not prove nothing happened."
		if source == "health" {
			message = "Retained samples crossing fixed pressure thresholds. Gaps can reflect disabled monitoring or expired retention; normal samples are omitted."
		}
		appendSource(source, message, rows, err)
	}
	var accessErr error
	password := ""
	if h.Gateway == nil || !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		accessErr = fmt.Errorf("connection offline")
	} else if h.Config == nil {
		accessErr = fmt.Errorf("configuration unavailable")
	} else {
		password, accessErr = crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	}
	for _, source := range []string{"queries", "parts"} {
		var rows []map[string]interface{}
		err := accessErr
		if err == nil {
			query := window.QuerySQL()
			if source == "parts" {
				query = window.PartsSQL()
			}
			var result *tunnel.QueryResult
			result, err = h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), session.ConnectionID, query, session.ClickhouseUser, password, map[string]string{
				"readonly": "1", "log_comment": "ch-ui:incident-timeline", "max_result_rows": "501", "result_overflow_mode": "throw",
				"max_execution_time": "10", "max_memory_usage": "536870912", "max_threads": "2",
			}, 15*time.Second)
			if err == nil {
				if result == nil {
					err = fmt.Errorf("missing query response")
				} else if err = json.Unmarshal(result.Data, &rows); err == nil && rows == nil {
					err = fmt.Errorf("query response is not an array")
				}
			}
		}
		observations := []incidenttimeline.Event{}
		for i, row := range rows {
			at := str(row["t"])
			if source == "queries" {
				// The first aggregated bucket can begin before a partial-minute
				// selection. Counts cover only the requested window.
				if bucketStart, err := time.Parse(time.RFC3339Nano, at); err == nil && bucketStart.Before(window.From) {
					at = window.From.Format(time.RFC3339Nano)
				}
			}
			event := incidenttimeline.Event{ID: fmt.Sprintf("%s:%s:%d", source, at, i), At: at, Source: source, Severity: "info", Values: row}
			if source == "queries" {
				event.Kind = "query_bucket"
				event.Title = fmt.Sprintf("%d queries, %d failures", toInt64(row["queries"]), toInt64(row["failures"]))
				event.Details = fmt.Sprintf("%d-second bucket; p95 %.1f ms, maximum %.1f ms. Initial query completion times on the connected node.", window.BucketSeconds(), toFloat(row["p95_ms"]), toFloat(row["max_ms"]))
				if toInt64(row["failures"]) > 0 {
					event.Severity = "warn"
				}
			} else {
				event.Kind = "part_event"
				event.Title = fmt.Sprintf("%s · %s.%s", str(row["event_type"]), str(row["database"]), str(row["table"]))
				event.Details = fmt.Sprintf("Completed operation: %.1f ms; error code %d.", toFloat(row["duration_ms"]), toInt64(row["error"]))
				if toInt64(row["error"]) != 0 {
					event.Severity = "warn"
				}
			}
			observations = append(observations, event)
		}
		message := "Connected node only, using your ClickHouse credentials. Coverage depends on system-log configuration and retention; remote-node activity may be absent."
		appendSource(source, message, observations, err)
	}
	incidenttimeline.Sort(events)
	writeJSON(w, 200, map[string]interface{}{"events": events, "coverage": coverage, "from": window.From.Format(time.RFC3339Nano), "to": window.To.Format(time.RFC3339Nano), "incident_id": incidentID, "bucket_seconds": window.BucketSeconds()})
}

func (h *IncidentTimelineHandler) CreateAnnotation(w http.ResponseWriter, r *http.Request) {
	session := timelineSession(w, r, false)
	if session == nil {
		return
	}
	var body struct {
		OccurredAt string `json:"occurred_at"`
		Title      string `json:"title"`
		Details    string `json:"details"`
	}
	if err := decodeBody(r, &body); err != nil {
		writeError(w, 400, "Invalid request body")
		return
	}
	body.Title = strings.TrimSpace(body.Title)
	body.Details = strings.TrimSpace(body.Details)
	at, err := time.Parse(time.RFC3339Nano, body.OccurredAt)
	if err != nil || len(body.Title) == 0 || len(body.Title) > 200 || len(body.Details) > 4000 {
		writeError(w, 400, "Provide a timestamp, title (1–200 characters) and details (up to 4000 characters)")
		return
	}
	annotation, err := h.DB.CreateIncidentAnnotation(r.Context(), database.IncidentAnnotation{ConnectionID: session.ConnectionID, OccurredAt: at.UTC().Format(time.RFC3339Nano), Title: body.Title, Details: body.Details, CreatedBy: timelineActor(session)})
	if err != nil {
		writeError(w, 500, "Could not save deployment annotation")
		return
	}
	actor := timelineActor(session)
	h.DB.CreateAuditLog(database.AuditLogParams{Action: "incident.annotation.created", Username: &actor, ConnectionID: &session.ConnectionID, Details: &annotation.ID})
	writeJSON(w, 201, annotation)
}

func (h *IncidentTimelineHandler) DeleteAnnotation(w http.ResponseWriter, r *http.Request) {
	session := timelineSession(w, r, false)
	if session == nil {
		return
	}
	err := h.DB.DeleteIncidentAnnotation(r.Context(), session.ConnectionID, chi.URLParam(r, "id"), timelineActor(session), session.UserRole == "admin")
	if err == sql.ErrNoRows {
		writeError(w, 404, "Annotation not found")
		return
	}
	if err != nil {
		writeError(w, 500, "Could not delete annotation")
		return
	}
	actor, id := timelineActor(session), chi.URLParam(r, "id")
	h.DB.CreateAuditLog(database.AuditLogParams{Action: "incident.annotation.deleted", Username: &actor, ConnectionID: &session.ConnectionID, Details: &id})
	writeJSON(w, 200, map[string]bool{"success": true})
}
