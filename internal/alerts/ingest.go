// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"encoding/json"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// Ingest and pipeline event types, raised by the pipeline runner from its
// in-memory counters. Resolved events reuse the type of the incident they
// close.
const (
	EventTypeIngestStalled       = "ingest.stalled"
	EventTypeIngestSpoolBacklog  = "ingest.spool_backlog"
	EventTypeIngestRejecting     = "ingest.rejecting"
	EventTypeIngestAuthFailures  = "ingest.auth_failures"
	EventTypePipelineSinkFailing = "pipeline.sink_failing"
)

// PipelineEventTypes are the event types the pipeline runner raises.
var PipelineEventTypes = []string{
	EventTypeIngestStalled, EventTypeIngestSpoolBacklog, EventTypeIngestRejecting,
	EventTypeIngestAuthFailures, EventTypePipelineSinkFailing,
}

// SpoolSourceRef is the source_ref of the instance-wide spool capacity
// alert; PipelineSourceRef names the source_ref of every other pipeline
// event, so a silence can pick one pipeline.
const SpoolSourceRef = "ingest:spool"

// PipelineSourceRef is the source_ref of the events of one pipeline.
func PipelineSourceRef(pipelineID string) string { return "pipeline:" + pipelineID }

// PipelineLinkPath is the page a pipeline event links to.
func PipelineLinkPath(pipelineID string) string { return "/pipelines/" + pipelineID }

// PipelineAlertKey names one alert of one pipeline: kind is stalled,
// spool_backlog, rejecting, auth_failures or sink_failing.
func PipelineAlertKey(pipelineID, kind string) string {
	return "pipeline:" + pipelineID + ":" + kind
}

// SpoolAlertKey names the instance-wide spool capacity alert.
const SpoolAlertKey = "ingest:spool"

// OpenIncident is an incident whose newest event is a firing one.
type OpenIncident struct {
	Event      database.AlertEvent
	AlertKey   string
	IncidentID string
	// Severity is the incident's severity, which can be above the newest
	// event's after an escalation.
	Severity string
	Payload  map[string]interface{}
}

// OpenIncidents returns the incidents of the given event types that have
// not resolved, read from the newest limit events of each type. An alert
// whose newest event is a resolve is closed.
func OpenIncidents(db *database.DB, types []string, limit int) ([]OpenIncident, error) {
	var out []OpenIncident
	for _, t := range types {
		events, err := db.ListAlertEvents(limit, t, "")
		if err != nil {
			return nil, err
		}
		seen := map[string]bool{}
		for _, ev := range events {
			id := IdentifyEvent(ev)
			if seen[id.AlertKey] {
				continue
			}
			seen[id.AlertKey] = true
			if id.Kind == kindResolved || id.Kind == kindOther {
				continue
			}
			var payload map[string]interface{}
			if ev.PayloadJSON != nil {
				_ = json.Unmarshal([]byte(*ev.PayloadJSON), &payload)
			}
			sev, _ := payload["incident_severity"].(string)
			if sev == "" {
				sev = ev.Severity
			}
			out = append(out, OpenIncident{Event: ev, AlertKey: id.AlertKey, IncidentID: id.IncidentID, Severity: sev, Payload: payload})
		}
	}
	return out, nil
}
