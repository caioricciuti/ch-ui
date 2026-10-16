// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/alerts"
)

// An open ingest incident is listed for viewers with its pipeline link,
// writers acknowledge it, and rules take the ingest and pipeline types.
func TestIngestIncidentsFiringAndRules(t *testing.T) {
	db, _, connA, _, router := newGovernanceRoleRouter(t)
	incident := "2026-10-16T10:00:00Z"
	key := alerts.PipelineAlertKey("p1", "stalled")
	c := connA
	evID, err := db.CreateAlertEvent(&c, alerts.EventTypeIngestStalled, alerts.SeverityWarn, "Ingest stalled: otel", "no data for 20 minutes",
		map[string]interface{}{"status": "firing", "alert_key": key, "incident_id": incident, "incident_severity": "warn",
			"pipeline_id": "p1", "link_path": alerts.PipelineLinkPath("p1")}, key+":firing:"+incident, alerts.PipelineSourceRef("p1"))
	if err != nil {
		t.Fatal(err)
	}

	var firing struct {
		Incidents []firingIncident `json:"incidents"`
	}
	rec := serveAs(router, "viewer", connA, http.MethodGet, "/alerts/firing", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &firing)
	if len(firing.Incidents) != 1 {
		t.Fatalf("firing: %s", rec.Body.String())
	}
	inc := firing.Incidents[0]
	if inc.Source != "ingest" || inc.AlertKey != key || inc.IncidentID != incident || inc.LinkPath == nil || *inc.LinkPath != "/pipelines/p1" ||
		inc.PipelineID == nil || *inc.PipelineID != "p1" || inc.EventID == nil || *inc.EventID != evID || inc.Name != "Ingest stalled: otel" {
		t.Fatalf("ingest incident: %s", rec.Body.String())
	}
	if rec := serveAs(router, "analyst", connA, http.MethodPost, "/alerts/acks", `{"event_id":"`+evID+`"}`); rec.Code != http.StatusCreated {
		t.Fatalf("ack: %d %s", rec.Code, rec.Body.String())
	}

	for _, typ := range []string{"ingest.*", "ingest.stalled", "ingest.spool_backlog", "ingest.rejecting", "ingest.auth_failures", "pipeline.sink_failing", "pipeline.*"} {
		body := `{"name":"r","event_type":"` + typ + `","severity_min":"info","channels":[]}`
		if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/rules", body); rec.Code != http.StatusCreated {
			t.Errorf("rule %s: %d %s", typ, rec.Code, rec.Body.String())
		}
	}
}
