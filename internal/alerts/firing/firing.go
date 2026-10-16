// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package firing lists the open alert incidents of health checks, SQL alerts
// and pipelines with the ack that covers each. It is the data behind the
// firing view; access drift, which only admins see, is added by the caller.
package firing

import (
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/sqlalerts"
)

// Incident is one open incident.
type Incident struct {
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
	// Access drift and ingest: the latest event of the incident, when it
	// was seen, and the page to review it on.
	Summary  *string `json:"summary,omitempty"`
	LastSeen *string `json:"last_seen,omitempty"`
	LinkPath *string `json:"link_path,omitempty"`
	// Ingest and pipeline incidents only.
	PipelineID *string `json:"pipeline_id,omitempty"`
	EventType  *string `json:"event_type,omitempty"`
}

// pipelineIncidentScan is how many recent events per ingest or pipeline
// type the firing view reads to find the open incidents.
const pipelineIncidentScan = 500

// Acks indexes the open acknowledgements by alert key.
type Acks map[string]database.AlertAck

// LoadAcks reads the open acknowledgements.
func LoadAcks(db *database.DB) (Acks, error) {
	list, err := db.ListOpenAlertAcks()
	if err != nil {
		return nil, fmt.Errorf("load acknowledgements: %w", err)
	}
	out := Acks{}
	for _, a := range list {
		out[a.AlertKey] = a
	}
	return out, nil
}

// For returns the ack covering an incident of an alert, or nil. An ack
// bound to another incident of the same alert does not cover this one.
func (a Acks) For(key, incident string) *database.AlertAck {
	ack, ok := a[key]
	if !ok {
		return nil
	}
	if ack.IncidentID != nil && *ack.IncidentID != "" && incident != "" && *ack.IncidentID != incident {
		return nil
	}
	return &ack
}

// Open returns the open incidents of health checks, SQL alerts and
// pipelines (ingest stalls, spool backlog, rejections, failing sinks) with
// the ack that covers each. connectionID "" means every connection.
func Open(db *database.DB, acks Acks, connectionID string) ([]Incident, error) {
	out := []Incident{}
	onConn := func(id string) bool { return connectionID == "" || id == connectionID }

	states, err := db.ListFiringHealthStates()
	if err != nil {
		return nil, fmt.Errorf("load health check state: %w", err)
	}
	for _, st := range states {
		if !onConn(st.ConnectionID) {
			continue
		}
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
		out = append(out, Incident{
			Source: "health", AlertKey: key, IncidentID: inc, ConnectionID: st.ConnectionID, CheckKind: &kind, Node: &node,
			Name: check.Label, Severity: sev, Since: inc, EventID: st.LastEventID, Ack: acks.For(key, inc),
		})
	}

	list, err := db.ListSQLAlerts(connectionID)
	if err != nil {
		return nil, fmt.Errorf("load SQL alerts: %w", err)
	}
	for _, a := range list {
		if a.State != alerts.StateFiring {
			continue
		}
		id := a.ID
		key := sqlalerts.AlertKey(a.ID)
		inc, _ := db.LatestAlertIncidentID(a.ID, key)
		item := Incident{
			Source: "sql", AlertKey: key, IncidentID: inc, ConnectionID: a.ConnectionID, SQLAlertID: &id,
			Name: a.Name, Severity: a.Severity, Since: inc, Ack: acks.For(key, inc),
		}
		if ev, err := db.LatestAlertEventFor(a.ID, key); err == nil && ev != nil {
			evID := ev.ID
			item.EventID = &evID
		}
		out = append(out, item)
	}

	open, err := alerts.OpenIncidents(db, alerts.PipelineEventTypes, pipelineIncidentScan)
	if err != nil {
		return nil, fmt.Errorf("load ingest incidents: %w", err)
	}
	for _, inc := range open {
		item := pipelineIncident(inc, acks.For(inc.AlertKey, inc.IncidentID))
		if onConn(item.ConnectionID) {
			out = append(out, item)
		}
	}
	return out, nil
}

// pipelineIncident renders an open ingest or pipeline incident.
func pipelineIncident(inc alerts.OpenIncident, ack *database.AlertAck) Incident {
	str := func(k string) string { s, _ := inc.Payload[k].(string); return s }
	conn := ""
	if inc.Event.ConnectionID != nil {
		conn = *inc.Event.ConnectionID
	}
	evID, seen, summary := inc.Event.ID, inc.Event.CreatedAt, inc.Event.Message
	item := Incident{
		Source: "ingest", AlertKey: inc.AlertKey, IncidentID: inc.IncidentID, ConnectionID: conn,
		Name: inc.Event.Title, Severity: inc.Severity, Since: inc.IncidentID, EventID: &evID, Ack: ack,
		Summary: &summary, LastSeen: &seen,
	}
	if p := str("pipeline_id"); p != "" {
		item.PipelineID = &p
	}
	if link := str("link_path"); strings.HasPrefix(link, "/") {
		item.LinkPath = &link
	}
	if t := inc.Event.EventType; t != "" {
		item.EventType = &t
	}
	return item
}
