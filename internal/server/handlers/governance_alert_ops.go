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
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/accesshistory"
	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/sqlalerts"
	"github.com/go-chi/chi/v5"
)

const (
	maxSilenceDuration = 365 * 24 * time.Hour
	maxAckDuration     = 30 * 24 * time.Hour
)

// ── Rule scope ───────────────────────────────────────────────────────────────

// validateConnectionIDs trims and de-duplicates a rule's connection list and
// rejects ids that name no connection.
func (h *GovernanceHandler) validateConnectionIDs(ids []string) ([]string, error) {
	out := make([]string, 0, len(ids))
	seen := map[string]bool{}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	missing, err := h.DB.MissingConnectionIDs(out)
	if err != nil {
		return nil, fmt.Errorf("failed to check connections")
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("unknown connection: %s", strings.Join(missing, ", "))
	}
	return out, nil
}

// parseRuleConnectionIDs reads connection_ids from a rule body: present
// reports whether the field was sent; null and [] both mean every
// connection.
func parseRuleConnectionIDs(raw json.RawMessage) (ids []string, present bool, err error) {
	if len(raw) == 0 {
		return nil, false, nil
	}
	if err := json.Unmarshal(raw, &ids); err != nil {
		return nil, true, fmt.Errorf("connection_ids must be a list of connection ids")
	}
	return ids, true, nil
}

// ── Silences ─────────────────────────────────────────────────────────────────

type silenceBody struct {
	Reason          string  `json:"reason"`
	ConnectionID    *string `json:"connection_id"`
	EventType       *string `json:"event_type"`
	SourceRef       *string `json:"source_ref"`
	Node            *string `json:"node"`
	StartsAt        string  `json:"starts_at"`
	DurationSeconds *int64  `json:"duration_seconds"`
	EndsAt          string  `json:"ends_at"`
}

func optional(v *string) *string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return nil
	}
	s := strings.TrimSpace(*v)
	return &s
}

// silenceFromBody validates a silence body into a silence with UTC times.
func (h *GovernanceHandler) silenceFromBody(b silenceBody, now time.Time) (database.AlertSilence, error) {
	s := database.AlertSilence{
		Reason: strings.TrimSpace(b.Reason), ConnectionID: optional(b.ConnectionID), EventType: optional(b.EventType),
		SourceRef: optional(b.SourceRef), Node: optional(b.Node),
	}
	if s.Reason == "" {
		return s, fmt.Errorf("reason is required")
	}
	if len(s.Reason) > 500 {
		return s, fmt.Errorf("reason must be at most 500 characters")
	}
	if !alerts.SilenceHasMatcher(s) {
		return s, fmt.Errorf("set at least one of connection, alert type, specific alert or node")
	}
	if s.EventType != nil {
		et := strings.ToLower(*s.EventType)
		if !isSupportedEventType(et) {
			return s, fmt.Errorf("unsupported alert type %q", *s.EventType)
		}
		s.EventType = &et
	}
	if s.ConnectionID != nil {
		missing, err := h.DB.MissingConnectionIDs([]string{*s.ConnectionID})
		if err != nil {
			return s, fmt.Errorf("failed to check the connection")
		}
		if len(missing) > 0 {
			return s, fmt.Errorf("unknown connection: %s", *s.ConnectionID)
		}
	}
	start := now
	if strings.TrimSpace(b.StartsAt) != "" {
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(b.StartsAt))
		if err != nil {
			return s, fmt.Errorf("starts_at must be an RFC 3339 time")
		}
		start = t
	}
	var end time.Time
	switch {
	case b.DurationSeconds != nil:
		if *b.DurationSeconds <= 0 {
			return s, fmt.Errorf("duration_seconds must be positive")
		}
		end = start.Add(time.Duration(*b.DurationSeconds) * time.Second)
	case strings.TrimSpace(b.EndsAt) != "":
		t, err := time.Parse(time.RFC3339, strings.TrimSpace(b.EndsAt))
		if err != nil {
			return s, fmt.Errorf("ends_at must be an RFC 3339 time")
		}
		end = t
	default:
		return s, fmt.Errorf("set a duration or an end time")
	}
	if !end.After(start) {
		return s, fmt.Errorf("the silence must end after it starts")
	}
	if !end.After(now) {
		return s, fmt.Errorf("the silence must end in the future")
	}
	if end.Sub(start) > maxSilenceDuration {
		return s, fmt.Errorf("a silence can last at most 365 days")
	}
	s.StartsAt = start.UTC().Format(time.RFC3339)
	s.EndsAt = end.UTC().Format(time.RFC3339)
	return s, nil
}

func silenceAuditDetails(s database.AlertSilence) *string {
	data, _ := json.Marshal(map[string]interface{}{
		"silence": s.ID, "reason": s.Reason, "connection_id": s.ConnectionID, "event_type": s.EventType,
		"source_ref": s.SourceRef, "node": s.Node, "starts_at": s.StartsAt, "ends_at": s.EndsAt,
	})
	d := string(data)
	return &d
}

func (h *GovernanceHandler) auditAlertOp(r *http.Request, action string, connectionID, details *string) {
	session := middleware.GetSession(r)
	params := database.AuditLogParams{Action: action, ConnectionID: connectionID, Details: details, IPAddress: strPtr(getClientIP(r))}
	if session != nil {
		params.Username = strPtr(middleware.Actor(session))
		params.ClickhouseUser = &session.ClickhouseUser
	}
	if err := h.DB.CreateAuditLog(params); err != nil {
		slog.Warn("Alert audit failed", "action", action, "error", err)
	}
}

func (h *GovernanceHandler) ListAlertSilences(w http.ResponseWriter, r *http.Request) {
	silences, err := h.DB.ListAlertSilences(500)
	if err != nil {
		slog.Error("Failed to list alert silences", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list silences")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"silences": silences})
}

func (h *GovernanceHandler) CreateAlertSilence(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body silenceBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s, err := h.silenceFromBody(body, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.CreatedBy = strPtr(middleware.Actor(session))
	id, err := h.DB.CreateAlertSilence(s)
	if err != nil {
		slog.Error("Failed to create alert silence", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create silence")
		return
	}
	s.ID = id
	h.auditAlertOp(r, "alerts.silence.created", s.ConnectionID, silenceAuditDetails(s))
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *GovernanceHandler) UpdateAlertSilence(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetAlertSilence(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load silence")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Silence not found")
		return
	}
	if existing.Status == database.SilenceExpired {
		writeError(w, http.StatusConflict, "This silence has ended and can no longer be changed")
		return
	}
	var body silenceBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	s, err := h.silenceFromBody(body, time.Now().UTC())
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.ID = id
	ok, err := h.DB.UpdateAlertSilence(s)
	if err != nil {
		slog.Error("Failed to update alert silence", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to update silence")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "This silence has ended and can no longer be changed")
		return
	}
	h.auditAlertOp(r, "alerts.silence.updated", s.ConnectionID, silenceAuditDetails(s))
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ExpireAlertSilence ends a silence now and releases the incidents it held
// back that are still firing.
func (h *GovernanceHandler) ExpireAlertSilence(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetAlertSilence(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load silence")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Silence not found")
		return
	}
	ok, err := h.DB.ExpireAlertSilence(id, middleware.Actor(session), time.Now().UTC())
	if err != nil {
		slog.Error("Failed to expire alert silence", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to expire silence")
		return
	}
	if !ok {
		writeError(w, http.StatusConflict, "This silence has already ended")
		return
	}
	n := alerts.ReleaseSilence(h.DB, id)
	details := fmt.Sprintf(`{"silence":%q,"reason":%q,"early":true,"redelivered":%d}`, id, existing.Reason, n)
	h.auditAlertOp(r, "alerts.silence.expired", existing.ConnectionID, &details)
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "redelivered": n})
}

// ── Open incidents and acknowledgements ──────────────────────────────────────

type firingIncident struct {
	Source       string             `json:"source"`
	AlertKey     string             `json:"alert_key"`
	IncidentID   string             `json:"incident_id"`
	ConnectionID string             `json:"connection_id"`
	CheckKind    *string            `json:"check_kind"`
	Node         *string            `json:"node"`
	SQLAlertID   *string            `json:"sql_alert_id"`
	Name         string             `json:"name"`
	Severity     string             `json:"severity"`
	Since        string             `json:"since"`
	EventID      *string            `json:"event_id"`
	Ack          *database.AlertAck `json:"ack"`
	// Access drift only: the latest change of the incident, when it was
	// seen, and the page to review it on.
	Summary  *string `json:"summary,omitempty"`
	LastSeen *string `json:"last_seen,omitempty"`
	LinkPath *string `json:"link_path,omitempty"`
}

// isAdminSession reports whether the request comes from a CH-UI admin.
func isAdminSession(r *http.Request) bool {
	s := middleware.GetSession(r)
	return s != nil && s.UserRole == "admin"
}

// isDriftAlertKey reports whether an alert key names an access drift alert.
func isDriftAlertKey(key string) bool {
	return strings.HasPrefix(key, accesshistory.DriftAlertKey(""))
}

// openDriftIncident returns the open access drift incident of a connection
// from its newest drift event, or nil when there is none.
func (h *GovernanceHandler) openDriftIncident(connectionID string) (*firingIncident, error) {
	key := accesshistory.DriftAlertKey(connectionID)
	ev, err := h.DB.LatestAlertEventFor(accesshistory.DriftSourceRef, key)
	if err != nil || ev == nil || ev.PayloadJSON == nil {
		return nil, err
	}
	var p struct {
		Status           string `json:"status"`
		IncidentID       string `json:"incident_id"`
		IncidentSeverity string `json:"incident_severity"`
		ConnectionName   string `json:"connection_name"`
		Drift            string `json:"drift"`
		Added            int    `json:"added"`
		Removed          int    `json:"removed"`
		Changed          int    `json:"changed"`
	}
	if json.Unmarshal([]byte(*ev.PayloadJSON), &p) != nil || p.Status != "firing" {
		return nil, nil
	}
	name := p.ConnectionName
	if name == "" {
		name = connectionID
	}
	sev := p.IncidentSeverity
	if sev == "" {
		sev = ev.Severity
	}
	what := "Outside change"
	if p.Drift == accesshistory.DriftMixed {
		what = "Unexplained changes during an apply"
	}
	summary := fmt.Sprintf("%s: %d added, %d removed, %d changed", what, p.Added, p.Removed, p.Changed)
	evID, seen, link := ev.ID, ev.CreatedAt, accesshistory.DriftLinkPath
	return &firingIncident{
		Source: "drift", AlertKey: key, IncidentID: p.IncidentID, ConnectionID: connectionID,
		Name: "Access drift on " + name, Severity: sev, Since: p.IncidentID, EventID: &evID,
		Summary: &summary, LastSeen: &seen, LinkPath: &link,
	}, nil
}

// ListFiringIncidents returns the open incidents of health checks and SQL
// alerts across connections, with the ack that covers each. Admins also get
// the open access drift incidents, one per connection.
func (h *GovernanceHandler) ListFiringIncidents(w http.ResponseWriter, r *http.Request) {
	acks, err := h.DB.ListOpenAlertAcks()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load acknowledgements")
		return
	}
	ackByKey := map[string]database.AlertAck{}
	for _, a := range acks {
		ackByKey[a.AlertKey] = a
	}
	ackFor := func(key, incident string) *database.AlertAck {
		a, ok := ackByKey[key]
		if !ok {
			return nil
		}
		if a.IncidentID != nil && *a.IncidentID != "" && incident != "" && *a.IncidentID != incident {
			return nil
		}
		return &a
	}
	out := []firingIncident{}

	states, err := h.DB.ListFiringHealthStates()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load health check state")
		return
	}
	for _, st := range states {
		check, ok := clusterhealth.LookupHealthCheck(st.CheckKind)
		if !ok {
			continue
		}
		kind, node := st.CheckKind, st.Node
		key := clusterhealth.HealthAlertKey(st.ConnectionID, st.CheckKind, st.Node)
		inc := ""
		if st.Since != nil {
			inc = *st.Since
		}
		sev := alerts.SeverityCritical
		if st.Severity != nil && *st.Severity != "" {
			sev = *st.Severity
		}
		out = append(out, firingIncident{
			Source: "health", AlertKey: key, IncidentID: inc, ConnectionID: st.ConnectionID, CheckKind: &kind, Node: &node,
			Name: check.Label, Severity: sev, Since: inc, EventID: st.LastEventID, Ack: ackFor(key, inc),
		})
	}

	list, err := h.DB.ListSQLAlerts("")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load SQL alerts")
		return
	}
	for _, a := range list {
		if a.State != alerts.StateFiring {
			continue
		}
		id := a.ID
		key := sqlalerts.AlertKey(a.ID)
		inc, _ := h.DB.LatestAlertIncidentID(a.ID, key)
		item := firingIncident{
			Source: "sql", AlertKey: key, IncidentID: inc, ConnectionID: a.ConnectionID, SQLAlertID: &id,
			Name: a.Name, Severity: a.Severity, Since: inc, Ack: ackFor(key, inc),
		}
		if ev, err := h.DB.LatestAlertEventFor(a.ID, key); err == nil && ev != nil {
			evID := ev.ID
			item.EventID = &evID
		}
		out = append(out, item)
	}

	// Drift names other users and what they were granted; like the drift
	// events, only admins see it.
	if isAdminSession(r) {
		conns, err := h.DB.GetConnections()
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to load connections")
			return
		}
		for _, c := range conns {
			inc, err := h.openDriftIncident(c.ID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Failed to load access drift incidents")
				return
			}
			if inc != nil {
				inc.Ack = ackFor(inc.AlertKey, inc.IncidentID)
				out = append(out, *inc)
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"incidents": out})
}

// CreateAlertAck acknowledges the incident an event belongs to. Later
// firing and de-escalation events of that incident are held back until it
// resolves, escalates above the acknowledged severity, or the ack expires.
// Access drift is acknowledged by admins only.
func (h *GovernanceHandler) CreateAlertAck(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	var body struct {
		EventID   string `json:"event_id"`
		Comment   string `json:"comment"`
		ExpiresIn *int64 `json:"expires_in"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if strings.TrimSpace(body.EventID) == "" {
		writeError(w, http.StatusBadRequest, "event_id is required")
		return
	}
	comment := strings.TrimSpace(body.Comment)
	if len(comment) > 1000 {
		writeError(w, http.StatusBadRequest, "comment must be at most 1000 characters")
		return
	}
	ev, err := h.DB.GetAlertEvent(strings.TrimSpace(body.EventID))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load the alert event")
		return
	}
	if ev == nil {
		writeError(w, http.StatusNotFound, "Alert event not found")
		return
	}
	if ev.EventType == alerts.EventTypeAccessDrift && session.UserRole != "admin" {
		writeError(w, http.StatusForbidden, "Only admins can acknowledge access drift")
		return
	}
	id := alerts.IdentifyEvent(*ev)
	if strings.HasPrefix(strings.ToUpper(ev.Title), "[RESOLVED]") || id.Kind == "resolved" {
		writeError(w, http.StatusBadRequest, "This incident is already resolved")
		return
	}
	now := time.Now().UTC()
	var ttl time.Duration
	switch {
	case body.ExpiresIn != nil && *body.ExpiresIn > 0:
		ttl = time.Duration(*body.ExpiresIn) * time.Second
		if ttl > maxAckDuration {
			writeError(w, http.StatusBadRequest, "an acknowledgement can last at most 30 days")
			return
		}
	case id.IncidentID == "":
		// An alert that never resolves would stay muted forever.
		ttl = alerts.DefaultAckTTL
	}
	ack := database.AlertAck{
		AlertKey: id.AlertKey, EventID: &ev.ID, ConnectionID: ev.ConnectionID,
		Severity: strPtr(ev.Severity), AckedBy: strPtr(middleware.Actor(session)),
	}
	if id.IncidentID != "" {
		inc := id.IncidentID
		ack.IncidentID = &inc
	}
	if comment != "" {
		ack.Comment = &comment
	}
	if ttl > 0 {
		ack.ExpiresAt = strPtr(now.Add(ttl).Format(time.RFC3339))
	}
	created, err := h.DB.CreateAlertAck(ack)
	if err != nil {
		slog.Error("Failed to create alert ack", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to acknowledge")
		return
	}
	data, _ := json.Marshal(map[string]interface{}{"ack": created.ID, "alert_key": created.AlertKey, "incident_id": created.IncidentID,
		"event_id": ev.ID, "comment": comment, "expires_at": created.ExpiresAt})
	details := string(data)
	h.auditAlertOp(r, "alerts.incident.acknowledged", ev.ConnectionID, &details)
	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": created.ID, "ack": created})
}

func (h *GovernanceHandler) DeleteAlertAck(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	a, err := h.DB.GetAlertAck(chi.URLParam(r, "id"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load the acknowledgement")
		return
	}
	if a == nil || a.ClearedAt != nil {
		writeError(w, http.StatusNotFound, "No open acknowledgement with this id")
		return
	}
	if isDriftAlertKey(a.AlertKey) && session.UserRole != "admin" {
		writeError(w, http.StatusForbidden, "Only admins can remove an access drift acknowledgement")
		return
	}
	actor := middleware.Actor(session)
	if !alerts.ClearAck(h.DB, *a, "unacknowledged", time.Now().UTC(), &actor) {
		writeError(w, http.StatusNotFound, "No open acknowledgement with this id")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}
