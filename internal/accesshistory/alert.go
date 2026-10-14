// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package accesshistory

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// DriftSourceRef is the source_ref of every access drift event, for
// silences that pick this alert.
const DriftSourceRef = "access:drift"

// DriftLinkPath is the page a drift event links to.
const DriftLinkPath = "/governance?section=access-history"

// maxAlertEntities bounds the entities listed in a drift event.
const maxAlertEntities = 20

// DriftAlertKey names the drift alert of a connection. One incident runs
// from the first drift until the access state is back where it was before.
func DriftAlertKey(connectionID string) string {
	return "access:" + connectionID
}

// DriftSeverity is the severity of a drift class: an outside change is a
// warning; an apply with unexplained changes is an error, since something
// changed in the middle of a reviewed change.
func DriftSeverity(drift string) string {
	if drift == DriftMixed {
		return alerts.SeverityError
	}
	return alerts.SeverityWarn
}

// openDrift is the open incident of a connection, read from its newest
// event.
type openDrift struct {
	incident, baseline, hash, severity, host string
}

func latestOpenDrift(db *database.DB, connectionID string) (*openDrift, error) {
	ev, err := db.LatestAlertEventFor(DriftSourceRef, DriftAlertKey(connectionID))
	if err != nil || ev == nil || ev.PayloadJSON == nil {
		return nil, err
	}
	var p struct {
		Status           string `json:"status"`
		IncidentID       string `json:"incident_id"`
		BaselineHash     string `json:"baseline_hash"`
		Hash             string `json:"hash"`
		IncidentSeverity string `json:"incident_severity"`
		Host             string `json:"host"`
	}
	if err := json.Unmarshal([]byte(*ev.PayloadJSON), &p); err != nil || p.Status != "firing" {
		return nil, nil
	}
	return &openDrift{incident: p.IncidentID, baseline: p.BaselineHash, hash: p.Hash, severity: p.IncidentSeverity, host: p.Host}, nil
}

// raiseDriftAlert writes the alert event a new history row calls for, after
// the row is committed. A row with outside or mixed drift fires, unless the
// open incident already fired on the same state; a complete row back at the
// state before the incident resolves it, but only when read from the host
// the drift was last seen on: on a multi-node cluster another node can lack
// a drifted entity kept in local storage, and reading it there is not a
// revert. baseline is the hash of the state
// the row was compared with. Failures are logged, never returned: the row
// is already stored.
// sameHost reports whether two reads came from the same node. An empty host
// (a single-node connection, or a row stored before hosts were recorded)
// matches anything.
func sameHost(a, b string) bool {
	return a == "" || b == "" || a == b
}

func raiseDriftAlert(db *database.DB, row *database.RBACSnapshot, baseline string) {
	open, err := latestOpenDrift(db, row.ConnectionID)
	if err != nil {
		slog.Warn("Access drift alert: failed to read the open incident", "connection", row.ConnectionID, "error", err)
		return
	}
	var title, message, fp, sev string
	var payload map[string]interface{}
	switch {
	case open != nil && row.Complete && open.baseline != "" && row.Hash == open.baseline && sameHost(open.host, row.Host):
		title, message, payload = resolvedDriftContent(db, row, open)
		fp = "access:" + row.ConnectionID + ":resolved:" + open.incident
		sev = open.severity
	case row.Drift == DriftOutside || row.Drift == DriftMixed:
		if open != nil && open.hash == row.Hash {
			return
		}
		sev = DriftSeverity(row.Drift)
		incident, incidentSev, escalated := row.FirstSeenAt, sev, false
		if open != nil {
			incident, baseline = open.incident, open.baseline
			escalated = alerts.SeverityRank(sev) > alerts.SeverityRank(open.severity)
			if !escalated {
				incidentSev = open.severity
			}
		}
		title, message, payload = firingDriftContent(db, row)
		payload["incident_id"], payload["baseline_hash"], payload["incident_severity"] = incident, baseline, incidentSev
		if escalated {
			payload["escalated"] = true
		}
		fp = "access:" + row.ConnectionID + ":drift:" + row.Hash
	default:
		return
	}
	if sev == "" {
		sev = alerts.SeverityWarn
	}
	conn := row.ConnectionID
	if _, err := db.CreateAlertEvent(&conn, alerts.EventTypeAccessDrift, sev, title, message, payload, fp, DriftSourceRef); err != nil {
		slog.Warn("Access drift alert: failed to create event", "connection", row.ConnectionID, "error", err)
	}
}

func connectionName(db *database.DB, id string) string {
	if c, err := db.GetConnectionByID(id); err == nil && c != nil && c.Name != "" {
		return c.Name
	}
	return id
}

func basePayload(db *database.DB, row *database.RBACSnapshot, status string) (string, map[string]interface{}) {
	name := connectionName(db, row.ConnectionID)
	return name, map[string]interface{}{
		"connection_id": row.ConnectionID, "connection_name": name, "alert_key": DriftAlertKey(row.ConnectionID),
		"status": status, "snapshot_id": row.ID, "hash": row.Hash, "drift": row.Drift, "source": row.Source,
		"actor": row.Actor, "ch_user": row.CHUser, "host": row.Host, "link_path": DriftLinkPath,
	}
}

func firingDriftContent(db *database.DB, row *database.RBACSnapshot) (string, string, map[string]interface{}) {
	name, payload := basePayload(db, row, "firing")
	var sum Summary
	_ = json.Unmarshal([]byte(row.Summary), &sum)
	added, removed, changed := total(sum.Added), total(sum.Removed), total(sum.Changed)
	payload["added"], payload["removed"], payload["changed"] = added, removed, changed
	payload["counts"] = map[string]map[string]int{"added": sum.Added, "removed": sum.Removed, "changed": sum.Changed}

	entities := sum.Entities
	title := "Access changed outside CH-UI on " + name
	lead := fmt.Sprintf("Access control on connection %s changed outside CH-UI", name)
	if row.Drift == DriftMixed {
		entities = sum.Unexplained
		title = "Unexpected access changes during an apply on " + name
		lead = fmt.Sprintf("An apply on connection %s left changes its operations do not account for", name)
	}
	listed := entities
	if len(listed) > maxAlertEntities {
		listed = listed[:maxAlertEntities]
	}
	payload["entities"], payload["entities_total"] = listed, len(entities)
	if sum.Truncated {
		payload["entities_truncated"] = true
	}

	var b strings.Builder
	fmt.Fprintf(&b, "%s, seen by %s (actor %s, ClickHouse user %s): %d added, %d removed, %d changed.",
		lead, sourceLabel(row.Source), orDash(row.Actor), orDash(row.CHUser), added, removed, changed)
	if len(listed) > 0 {
		b.WriteString(" Entities: " + strings.Join(listed, ", "))
		if more := len(entities) - len(listed); more > 0 {
			fmt.Fprintf(&b, " and %d more", more)
		}
		b.WriteString(".")
	}
	b.WriteString(" Open Governance, Access history to review or revert.")
	return title, b.String(), payload
}

func resolvedDriftContent(db *database.DB, row *database.RBACSnapshot, open *openDrift) (string, string, map[string]interface{}) {
	name, payload := basePayload(db, row, "resolved")
	payload["incident_id"], payload["baseline_hash"], payload["incident_severity"] = open.incident, open.baseline, open.severity
	title := "[RESOLVED] Access drift on " + name
	message := fmt.Sprintf("Access control on connection %s is back to the state before the drift, seen by %s (actor %s, ClickHouse user %s).",
		name, sourceLabel(row.Source), orDash(row.Actor), orDash(row.CHUser))
	return title, message, payload
}

func total(m map[string]int) int {
	n := 0
	for _, v := range m {
		n += v
	}
	return n
}

func sourceLabel(s string) string {
	switch s {
	case SourceApply:
		return "an apply"
	case SourceRevert:
		return "a revert"
	case SourceSync:
		return "governance sync"
	case SourceView:
		return "a page view"
	case SourceManual:
		return "a manual snapshot"
	}
	return s
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
