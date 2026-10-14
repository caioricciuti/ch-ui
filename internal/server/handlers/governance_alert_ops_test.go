// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestAlertOpsRoles(t *testing.T) {
	_, _, connA, _, router := newGovernanceRoleRouter(t)
	silence := `{"reason":"upgrade","connection_id":"` + connA + `","duration_seconds":3600}`
	cases := []struct {
		role, method, path, body string
	}{
		{"viewer", http.MethodPost, "/alerts/silences", silence},
		{"analyst", http.MethodPost, "/alerts/silences", silence},
		{"viewer", http.MethodPut, "/alerts/silences/x", silence},
		{"analyst", http.MethodPut, "/alerts/silences/x", silence},
		{"viewer", http.MethodPost, "/alerts/silences/x/expire", ""},
		{"analyst", http.MethodPost, "/alerts/silences/x/expire", ""},
		{"viewer", http.MethodPost, "/alerts/acks", `{"event_id":"x"}`},
		{"viewer", http.MethodDelete, "/alerts/acks/x", ""},
	}
	for _, c := range cases {
		if rec := serveAs(router, c.role, connA, c.method, c.path, c.body); rec.Code != http.StatusForbidden {
			t.Errorf("%s %s %s: %d, want 403", c.role, c.method, c.path, rec.Code)
		}
	}
	// Writers reach the ack routes (404: no such event or ack).
	if rec := serveAs(router, "analyst", connA, http.MethodPost, "/alerts/acks", `{"event_id":"x"}`); rec.Code != http.StatusNotFound {
		t.Errorf("analyst POST /acks: %d", rec.Code)
	}
	if rec := serveAs(router, "analyst", connA, http.MethodDelete, "/alerts/acks/x", ""); rec.Code != http.StatusNotFound {
		t.Errorf("analyst DELETE /acks: %d", rec.Code)
	}
	for _, path := range []string{"/alerts/silences", "/alerts/firing"} {
		if rec := serveAs(router, "viewer", connA, http.MethodGet, path, ""); rec.Code != http.StatusOK {
			t.Errorf("viewer GET %s: %d", path, rec.Code)
		}
	}
}

func TestAlertSilenceLifecycle(t *testing.T) {
	_, _, connA, _, router := newGovernanceRoleRouter(t)
	for _, bad := range []string{
		`{"reason":"x","duration_seconds":3600}`,
		`{"reason":"","connection_id":"` + connA + `","duration_seconds":3600}`,
		`{"reason":"x","connection_id":"nope","duration_seconds":3600}`,
		`{"reason":"x","event_type":"bogus","duration_seconds":3600}`,
		`{"reason":"x","event_type":"health.*"}`,
		`{"reason":"x","event_type":"health.*","starts_at":"2020-01-01T00:00:00Z","duration_seconds":60}`,
	} {
		if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/silences", bad); rec.Code != http.StatusBadRequest {
			t.Errorf("accepted (%d): %s", rec.Code, bad)
		}
	}
	start := time.Now().Add(time.Hour).In(time.FixedZone("x", 2*3600)).Format(time.RFC3339)
	rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/silences",
		`{"reason":"upgrade","event_type":"health.*","source_ref":"health:disk","starts_at":"`+start+`","duration_seconds":14400}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)

	var list struct {
		Silences []database.AlertSilence `json:"silences"`
	}
	rec = serveAs(router, "viewer", connA, http.MethodGet, "/alerts/silences", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	if len(list.Silences) != 1 || list.Silences[0].Status != database.SilenceScheduled || !strings.HasSuffix(list.Silences[0].StartsAt, "Z") {
		t.Fatalf("list: %s", rec.Body.String())
	}

	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/silences/"+created.ID,
		`{"reason":"upgrade 2","connection_id":"`+connA+`","duration_seconds":600}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/silences/"+created.ID+"/expire", ""); rec.Code != http.StatusOK {
		t.Fatalf("expire: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/silences/"+created.ID+"/expire", ""); rec.Code != http.StatusConflict {
		t.Fatalf("expire twice: %d", rec.Code)
	}
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/silences/"+created.ID, `{"reason":"x","node":"n","duration_seconds":60}`); rec.Code != http.StatusConflict {
		t.Fatalf("update after expiry: %d", rec.Code)
	}
}

func TestAlertAcksAndFiring(t *testing.T) {
	db, _, connA, _, router := newGovernanceRoleRouter(t)
	incident := "2026-10-13T10:00:00Z"
	key := "health:" + connA + ":disk:ch-1"
	c := connA
	evID, err := db.CreateAlertEvent(&c, "health.disk", "warn", "Disk usage on ch-1: 85%", "m",
		map[string]interface{}{"status": "firing", "alert_key": key, "incident_id": incident, "node": "ch-1"}, key+":warn:"+incident, "health:disk")
	if err != nil {
		t.Fatal(err)
	}
	sev := "warn"
	if err := db.SaveHealthAlertStates([]database.HealthAlertState{{ConnectionID: connA, CheckKind: "disk", Node: "ch-1", State: "firing",
		Severity: &sev, Since: &incident, LastEventID: &evID}}); err != nil {
		t.Fatal(err)
	}

	rec := serveAs(router, "analyst", connA, http.MethodPost, "/alerts/acks", `{"event_id":"`+evID+`","comment":"on it"}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("ack: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID  string            `json:"id"`
		Ack database.AlertAck `json:"ack"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if created.Ack.AlertKey != key || created.Ack.IncidentID == nil || *created.Ack.IncidentID != incident || created.Ack.ExpiresAt != nil {
		t.Fatalf("ack: %+v", created.Ack)
	}

	var firing struct {
		Incidents []firingIncident `json:"incidents"`
	}
	rec = serveAs(router, "viewer", connA, http.MethodGet, "/alerts/firing", "")
	_ = json.Unmarshal(rec.Body.Bytes(), &firing)
	if len(firing.Incidents) != 1 || firing.Incidents[0].Ack == nil || firing.Incidents[0].Ack.ID != created.ID ||
		firing.Incidents[0].EventID == nil || *firing.Incidents[0].EventID != evID || firing.Incidents[0].Name != "Disk usage" {
		t.Fatalf("firing: %s", rec.Body.String())
	}

	if rec := serveAs(router, "analyst", connA, http.MethodDelete, "/alerts/acks/"+created.ID, ""); rec.Code != http.StatusOK {
		t.Fatalf("unack: %d %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "analyst", connA, http.MethodDelete, "/alerts/acks/"+created.ID, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unack twice: %d", rec.Code)
	}

	// An alert without incidents gets a 24 hour expiry by default.
	sched, _ := db.CreateAlertEvent(&c, "schedule.failed", "error", "Scheduled query failed: s", "m", map[string]interface{}{"schedule_id": "s"}, "schedule:s:error", "run")
	rec = serveAs(router, "admin", connA, http.MethodPost, "/alerts/acks", `{"event_id":"`+sched+`"}`)
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if rec.Code != http.StatusCreated || created.Ack.ExpiresAt == nil {
		t.Fatalf("schedule ack: %d %s", rec.Code, rec.Body.String())
	}
	if exp, _ := time.Parse(time.RFC3339, *created.Ack.ExpiresAt); time.Until(exp) < 23*time.Hour || time.Until(exp) > 25*time.Hour {
		t.Fatalf("default expiry %s", *created.Ack.ExpiresAt)
	}
}

func TestAlertRuleConnectionIDs(t *testing.T) {
	db, _, connA, connB, router := newGovernanceRoleRouter(t)
	bad := `{"name":"r","event_type":"*","severity_min":"info","connection_ids":["nope"],"channels":[]}`
	if rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/rules", bad); rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown connection accepted: %d", rec.Code)
	}
	rec := serveAs(router, "admin", connA, http.MethodPost, "/alerts/rules",
		`{"name":"r","event_type":"health.connection_unreachable","severity_min":"info","connection_ids":["`+connA+`","`+connA+`"],"channels":[]}`)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	if r, _ := db.GetAlertRuleByID(created.ID); len(r.ConnectionIDs) != 1 || r.ConnectionIDs[0] != connA {
		t.Fatalf("stored scope: %+v", r.ConnectionIDs)
	}
	// Omitted keeps the scope, a list replaces it, null clears it.
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/rules/"+created.ID, `{"name":"r2"}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	if r, _ := db.GetAlertRuleByID(created.ID); len(r.ConnectionIDs) != 1 {
		t.Fatalf("omitted connection_ids changed the scope: %+v", r.ConnectionIDs)
	}
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/rules/"+created.ID, `{"connection_ids":["`+connB+`"]}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	if r, _ := db.GetAlertRuleByID(created.ID); len(r.ConnectionIDs) != 1 || r.ConnectionIDs[0] != connB {
		t.Fatalf("scope not replaced: %+v", r.ConnectionIDs)
	}
	if rec := serveAs(router, "admin", connA, http.MethodPut, "/alerts/rules/"+created.ID, `{"connection_ids":null}`); rec.Code != http.StatusOK {
		t.Fatalf("update: %d", rec.Code)
	}
	if r, _ := db.GetAlertRuleByID(created.ID); r.ConnectionIDs != nil {
		t.Fatalf("scope not cleared: %+v", r.ConnectionIDs)
	}
	if rec := serveAs(router, "viewer", connA, http.MethodGet, "/alerts/rules", ""); !strings.Contains(rec.Body.String(), `"connection_ids":null`) {
		t.Fatalf("rules list: %s", rec.Body.String())
	}
}

func TestSupportedEventTypeAccessDrift(t *testing.T) {
	if !isSupportedEventType("access.drift") || isSupportedEventType("access.other") {
		t.Fatal("access.drift must be selectable, unknown access types must not")
	}
}
