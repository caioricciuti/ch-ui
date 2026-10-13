// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"log/slog"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
)

const (
	// reachabilityGrace is how long after start an offline tunnel is not
	// counted as a breach of the connection check.
	reachabilityGrace = 2 * time.Minute
	// probeTimeout bounds the SELECT 1 of the connection check.
	probeTimeout = 10 * time.Second
)

// reachability is one evaluation of the connection check: a breach, an OK,
// or unknown (the probe could not run). detail says why.
type reachability struct {
	breach  bool
	unknown bool
	detail  string
}

// connectionCheckSetting returns the effective setting of the connection
// check on a connection.
func connectionCheckSetting(db *database.DB, connID string) (HealthCheckSetting, error) {
	settings, err := LoadHealthSettings(db, connID)
	if err != nil {
		return HealthCheckSetting{}, err
	}
	for _, s := range settings {
		if s.CheckKind == CheckConnectionUnreachable {
			return s, nil
		}
	}
	c, _ := LookupHealthCheck(CheckConnectionUnreachable)
	return DefaultHealthSetting(c), nil
}

// connectionCheckEnabled reports whether the connection check runs on a
// connection.
func (h *Harvester) connectionCheckEnabled(connID string) bool {
	set, err := connectionCheckSetting(h.db, connID)
	if err != nil {
		slog.Warn("Health alerts: failed to load the connection check", "connection", connID, "error", err)
		return false
	}
	return set.Enabled
}

// selectOne probes a connection with SELECT 1.
func (h *Harvester) selectOne(creds CHCredentials) reachability {
	_, err := h.gateway.ExecuteQueryWithSettings(creds.ConnectionID, "SELECT 1", creds.User, creds.Password, QuerySettings(), probeTimeout)
	if err != nil {
		return reachability{breach: true, detail: "SELECT 1 failed: " + err.Error()}
	}
	return reachability{}
}

// probeOnly runs the connection check of a connection whose Cluster Health
// sampling is off: SELECT 1 and nothing else.
func (h *Harvester) probeOnly(connID string, now time.Time) {
	h.markPolled(connID, now)
	creds, err := h.findCredentials(connID)
	if err != nil {
		h.evaluateReachability(connID, reachability{unknown: true, detail: "no credentials: " + err.Error()}, now)
		return
	}
	h.evaluateReachability(connID, h.selectOne(creds), now)
}

// evaluateReachability advances the connection check of a connection by one
// evaluation and raises its event. Its state lives on node ConnectionNode.
// Unknown freezes the state and records the reason as an error row.
func (h *Harvester) evaluateReachability(connID string, r reachability, now time.Time) {
	set, err := connectionCheckSetting(h.db, connID)
	if err != nil {
		slog.Warn("Health alerts: failed to load the connection check", "connection", connID, "error", err)
		return
	}
	if !set.Enabled {
		return
	}
	check, _ := LookupHealthCheck(CheckConnectionUnreachable)
	nowStr := now.UTC().Format(time.RFC3339)
	var prev database.HealthAlertState
	stored, err := h.db.ListHealthAlertStates(connID, false)
	if err != nil {
		slog.Warn("Health alerts: failed to load state", "connection", connID, "error", err)
		return
	}
	for _, st := range stored {
		if st.CheckKind == CheckConnectionUnreachable && st.Node == ConnectionNode {
			prev = st
		}
	}
	prevState := prev.State
	if prevState == "" {
		prevState = alerts.StateOK
	}

	if r.unknown {
		if err := h.db.InsertAlertEvaluations([]database.AlertEvaluation{{
			Source: "health", SourceID: CheckConnectionUnreachable, ConnectionID: connID, Node: ConnectionNode,
			Value: prev.LastValue, State: prevState, Error: r.detail, EvaluatedAt: nowStr,
		}}); err != nil {
			slog.Warn("Health alerts: failed to record evaluation", "connection", connID, "error", err)
		}
		return
	}

	value := 0.0
	breach := ""
	if r.breach {
		value, breach = 1, alerts.SeverityCritical
	}
	hPrev := alerts.Hysteresis{State: prevState, BreachCount: prev.BreachCount, OkCount: prev.OkCount}
	if prev.Severity != nil {
		hPrev.Severity = *prev.Severity
	}
	next, transition := alerts.Step(hPrev, breach, false, set.ForEvaluations, set.ResolveEvaluations)
	st := database.HealthAlertState{
		ConnectionID: connID, CheckKind: CheckConnectionUnreachable, Node: ConnectionNode, State: next.State,
		BreachCount: next.BreachCount, OkCount: next.OkCount, LastValue: f64(value),
		Since: prev.Since, LastEventID: prev.LastEventID,
	}
	if next.Severity != "" {
		sev := next.Severity
		st.Severity = &sev
	}
	if next.State != prevState || st.Since == nil {
		st.Since = &nowStr
	}

	if transition != alerts.TransitionNone {
		ev := healthEvent{check: check, setting: set, node: ConnectionNode, value: value, transition: transition,
			severity: next.Severity, incident: *st.Since, at: nowStr, detail: r.detail, stateIdx: 0, ghostIdx: -1}
		if transition == alerts.TransitionResolved {
			ev.incident = incidentOf(prev, nowStr)
			ev.announced = prev.LastEventID != nil
			ev.detail = ""
		}
		// A check turned off while this ran raises nothing.
		if current, err := h.db.ListHealthAlertSettings(connID); err == nil {
			if row, ok := current[CheckConnectionUnreachable]; ok && !row.Enabled {
				return
			}
		}
		events := []healthEvent{ev}
		raiseHealthEvents(h.db, connID, "", events, nil, now)
		if !events[0].created {
			// Not written: the state does not advance, so the next poll
			// raises it again.
			return
		}
		switch transition {
		case alerts.TransitionFiring:
			st.LastEventID = nil
			if events[0].eventID != "" {
				id := events[0].eventID
				st.LastEventID = &id
			}
		case alerts.TransitionEscalated, alerts.TransitionDeescalated:
			if events[0].eventID != "" {
				id := events[0].eventID
				st.LastEventID = &id
			}
		case alerts.TransitionResolved:
			st.Severity = nil
		}
	}

	if err := h.db.SaveHealthAlertStates([]database.HealthAlertState{st}); err != nil {
		slog.Warn("Health alerts: failed to save state", "connection", connID, "error", err)
	}
	changed := next.State != prevState || transition != alerts.TransitionNone
	if h.shouldRecord(connID, CheckConnectionUnreachable, ConnectionNode, changed || r.breach, now) {
		if err := h.db.InsertAlertEvaluations([]database.AlertEvaluation{{
			Source: "health", SourceID: CheckConnectionUnreachable, ConnectionID: connID, Node: ConnectionNode,
			Value: st.LastValue, State: st.State, Error: r.detail, EvaluatedAt: nowStr,
		}}); err != nil {
			slog.Warn("Health alerts: failed to record evaluation", "connection", connID, "error", err)
		}
	}
}
