// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// DefaultAckTTL is how long an acknowledgement of an alert without
// incidents (one that never resolves, like schedule.failed) lasts when no
// expiry is given.
const DefaultAckTTL = 24 * time.Hour

// Event kinds, from the payload of an event.
const (
	kindOther       = ""
	kindFiring      = "firing"
	kindEscalated   = "escalated"
	kindDeescalated = "deescalated"
	kindResolved    = "resolved"
)

// EventIdentity names the alert and incident an event belongs to.
type EventIdentity struct {
	// AlertKey names the alert: one health check on one node, one SQL alert,
	// or the fingerprint of any other event.
	AlertKey string
	// IncidentID names the incident; "" for alerts without incidents.
	IncidentID string
	// Kind is firing, escalated, deescalated, resolved or "" for events
	// that carry no status.
	Kind string
	Node string
}

// IdentifyEvent reads the alert key, incident and kind of an event from its
// payload. Events written before payloads carried alert_key fall back to
// the fingerprint without its transition suffix.
func IdentifyEvent(ev database.AlertEvent) EventIdentity {
	var payload map[string]interface{}
	if ev.PayloadJSON != nil {
		_ = json.Unmarshal([]byte(*ev.PayloadJSON), &payload)
	}
	str := func(k string) string {
		s, _ := payload[k].(string)
		return s
	}
	id := EventIdentity{AlertKey: str("alert_key"), IncidentID: str("incident_id"), Node: str("node")}
	if id.AlertKey == "" && ev.Fingerprint != nil {
		id.AlertKey = AlertKeyFromFingerprint(*ev.Fingerprint)
	}
	if id.AlertKey == "" {
		id.AlertKey = "event:" + ev.ID
	}
	switch str("status") {
	case "resolved":
		id.Kind = kindResolved
	case "firing":
		id.Kind = kindFiring
		if b, _ := payload["escalated"].(bool); b {
			id.Kind = kindEscalated
		} else if b, _ := payload["deescalated"].(bool); b {
			id.Kind = kindDeescalated
		}
	}
	return id
}

// AlertKeyFromFingerprint strips the transition suffix of a health or SQL
// alert fingerprint; other fingerprints are already per alert.
func AlertKeyFromFingerprint(fp string) string {
	fp = strings.TrimSpace(fp)
	parts := strings.Split(fp, ":")
	switch {
	case parts[0] == "health" && len(parts) >= 5:
		return strings.Join(parts[:4], ":")
	case parts[0] == "sql" && len(parts) >= 3:
		return strings.Join(parts[:2], ":")
	default:
		return fp
	}
}

// SilenceMatches reports whether a silence selects an event. Every matcher
// that is set must match; a silence without matchers selects nothing.
func SilenceMatches(s database.AlertSilence, ev database.AlertEvent, node string) bool {
	set := false
	if v := trimmed(s.ConnectionID); v != "" {
		set = true
		if ev.ConnectionID == nil || strings.TrimSpace(*ev.ConnectionID) != v {
			return false
		}
	}
	if v := trimmed(s.EventType); v != "" {
		set = true
		if !EventTypeMatches(v, ev.EventType) {
			return false
		}
	}
	if v := trimmed(s.SourceRef); v != "" {
		set = true
		if ev.SourceRef == nil || strings.TrimSpace(*ev.SourceRef) != v {
			return false
		}
	}
	if v := trimmed(s.Node); v != "" {
		set = true
		if node != v {
			return false
		}
	}
	return set
}

// SilenceHasMatcher reports whether a silence sets at least one matcher.
func SilenceHasMatcher(s database.AlertSilence) bool {
	return trimmed(s.ConnectionID) != "" || trimmed(s.EventType) != "" || trimmed(s.SourceRef) != "" || trimmed(s.Node) != ""
}

func trimmed(s *string) string {
	if s == nil {
		return ""
	}
	return strings.TrimSpace(*s)
}

// suppressor decides, per new event, whether a silence or an ack holds it
// back. It is loaded once per dispatcher tick.
type suppressor struct {
	db       *database.DB
	now      time.Time
	silences []database.AlertSilence
	acks     map[string]database.AlertAck
}

func loadSuppressor(db *database.DB, now time.Time) (*suppressor, error) {
	silences, err := db.ListActiveAlertSilences(now)
	if err != nil {
		return nil, err
	}
	acks, err := db.ListOpenAlertAcks()
	if err != nil {
		return nil, err
	}
	s := &suppressor{db: db, now: now, silences: silences, acks: map[string]database.AlertAck{}}
	for _, a := range acks {
		s.acks[a.AlertKey] = a
	}
	return s, nil
}

// decide returns the status and suppressed_by of an event that is held
// back, or "" to deliver it. It clears the acks the event ends.
func (s *suppressor) decide(ev database.AlertEvent) (string, string) {
	id := IdentifyEvent(ev)

	var cover *database.AlertAck
	if a, ok := s.acks[id.AlertKey]; ok {
		ackIncident := trimmed(a.IncidentID)
		reason := ""
		switch {
		case ackIncident != "" && id.IncidentID != "" && ackIncident != id.IncidentID:
			reason = "new incident"
		case id.Kind == kindResolved:
			reason = "resolved"
		case id.Kind == kindEscalated && SeverityRank(ev.Severity) > SeverityRank(trimmed(a.Severity)):
			reason = "escalated"
		}
		if reason != "" {
			ClearAck(s.db, a, reason, s.now, nil)
			delete(s.acks, id.AlertKey)
		} else {
			cover = &a
		}
	}

	for _, sil := range s.silences {
		if SilenceMatches(sil, ev, id.Node) {
			return database.AlertEventSilenced, "silence:" + sil.ID
		}
	}
	if id.Kind == kindResolved && id.IncidentID != "" {
		if by := s.silencedIncident(ev.ID, id); by != "" {
			return database.AlertEventSilenced, by
		}
	}
	if cover != nil {
		return database.AlertEventAcknowledged, "ack:" + cover.ID
	}
	return "", ""
}

// silencedIncident returns the silence that held back the incident of a
// resolve, when none of the incident's other events went out.
func (s *suppressor) silencedIncident(eventID string, id EventIdentity) string {
	evs, err := s.db.IncidentAlertEvents(id.AlertKey, id.IncidentID)
	if err != nil {
		slog.Warn("Alert dispatcher failed to load incident events", "alert", id.AlertKey, "error", err)
		return ""
	}
	by := ""
	for _, e := range evs {
		if e.ID == eventID || IdentifyEvent(e).Kind == kindResolved {
			continue
		}
		switch {
		case e.Status == database.AlertEventSilenced && e.SuppressedBy != nil:
			by = *e.SuppressedBy
		case e.Status == "processed":
			return ""
		}
	}
	return by
}

// ClearAck closes an open ack and audits it. actor is nil for the
// dispatcher (resolved, escalated, expired, new incident).
func ClearAck(db *database.DB, a database.AlertAck, reason string, now time.Time, actor *string) bool {
	ok, err := db.ClearAlertAck(a.ID, reason, now)
	if err != nil {
		slog.Warn("Alert dispatcher failed to clear ack", "ack", a.ID, "error", err)
		return false
	}
	if !ok {
		return false
	}
	details, _ := json.Marshal(map[string]string{"ack": a.ID, "alert_key": a.AlertKey, "incident_id": trimmed(a.IncidentID), "reason": reason})
	d := string(details)
	if err := db.CreateAuditLog(database.AuditLogParams{Action: "alerts.incident.unacknowledged", Username: actor, ConnectionID: a.ConnectionID, Details: &d}); err != nil {
		slog.Warn("Alert ack audit failed", "ack", a.ID, "error", err)
	}
	return true
}

// expireAcks clears the acks whose expiry passed.
func expireAcks(db *database.DB, now time.Time) {
	acks, err := db.ListExpiredAlertAcks(now)
	if err != nil {
		slog.Warn("Alert dispatcher failed to list expired acks", "error", err)
		return
	}
	for _, a := range acks {
		ClearAck(db, a, "expired", now, nil)
	}
}

// endLapsedSilences marks silences whose window ran out as expired and
// releases what they held back.
func endLapsedSilences(db *database.DB, now time.Time) {
	lapsed, err := db.ListLapsedAlertSilences(now)
	if err != nil {
		slog.Warn("Alert dispatcher failed to list lapsed silences", "error", err)
		return
	}
	for _, s := range lapsed {
		end, err := time.Parse(time.RFC3339, s.EndsAt)
		if err != nil {
			end = now
		}
		ok, err := db.ExpireAlertSilence(s.ID, "", end)
		if err != nil || !ok {
			continue
		}
		n := ReleaseSilence(db, s.ID)
		details := fmt.Sprintf(`{"silence":%q,"reason":%q,"redelivered":%d}`, s.ID, s.Reason, n)
		if err := db.CreateAuditLog(database.AuditLogParams{Action: "alerts.silence.expired", ConnectionID: s.ConnectionID, Details: &details}); err != nil {
			slog.Warn("Alert silence audit failed", "silence", s.ID, "error", err)
		}
	}
}

// ReleaseSilence runs when a silence ends: every incident it held back that
// is still firing gets its latest held-back event set back to 'new', so it
// is delivered once (unless another silence or an ack covers it). It
// returns how many events were requeued.
func ReleaseSilence(db *database.DB, silenceID string) int {
	evs, err := db.AlertEventsSuppressedBy("silence:" + silenceID)
	if err != nil {
		slog.Warn("Failed to list events held by a silence", "silence", silenceID, "error", err)
		return 0
	}
	type group struct{ key, incident string }
	latest := map[group]string{}
	var order []group
	for _, e := range evs {
		id := IdentifyEvent(e)
		if id.IncidentID == "" || id.Kind == kindResolved || id.Kind == kindOther {
			continue
		}
		g := group{id.AlertKey, id.IncidentID}
		if _, ok := latest[g]; !ok {
			order = append(order, g)
		}
		latest[g] = e.ID
	}
	n := 0
	for _, g := range order {
		incident, err := db.IncidentAlertEvents(g.key, g.incident)
		if err != nil {
			slog.Warn("Failed to load incident events", "alert", g.key, "error", err)
			continue
		}
		resolved := false
		for _, e := range incident {
			if IdentifyEvent(e).Kind == kindResolved {
				resolved = true
				break
			}
		}
		if resolved {
			continue
		}
		if ok, err := db.RequeueAlertEvent(latest[g]); err != nil {
			slog.Warn("Failed to requeue a silenced event", "event", latest[g], "error", err)
		} else if ok {
			n++
		}
	}
	return n
}
