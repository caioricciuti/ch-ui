// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
)

func ingestEvent(t *testing.T, db *database.DB, eventType, key, incident, status, sev string) string {
	t.Helper()
	payload := map[string]interface{}{"alert_key": key, "incident_id": incident, "status": status, "incident_severity": sev,
		"pipeline_id": "p1", "link_path": PipelineLinkPath("p1")}
	id, err := db.CreateAlertEvent(nil, eventType, sev, "t", "m", payload, key+":"+status+":"+incident, PipelineSourceRef("p1"))
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// The newest event of each alert decides: a firing one is open, a resolve
// closes it, and other types are not read.
func TestOpenIncidents(t *testing.T) {
	db := openTestDB(t)
	stalled := PipelineAlertKey("p1", "stalled")
	rejecting := PipelineAlertKey("p1", "rejecting")
	ingestEvent(t, db, EventTypeIngestStalled, stalled, "2026-10-16T10:00:00Z", "firing", SeverityWarn)
	ingestEvent(t, db, EventTypeIngestRejecting, rejecting, "2026-10-16T09:00:00Z", "firing", SeverityWarn)
	db.Conn().Exec(`UPDATE alert_events SET created_at = '2026-10-16T09:00:00Z'`)
	ingestEvent(t, db, EventTypeIngestRejecting, rejecting, "2026-10-16T09:00:00Z", "resolved", SeverityWarn)
	ingestEvent(t, db, EventTypeSQLAlert, "sql:x", "2026-10-16T10:00:00Z", "firing", SeverityWarn)

	open, err := OpenIncidents(db, PipelineEventTypes, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(open) != 1 || open[0].AlertKey != stalled || open[0].IncidentID != "2026-10-16T10:00:00Z" || open[0].Severity != SeverityWarn {
		t.Fatalf("want only the stall open, got %+v", open)
	}
}

func TestIngestRuleMatching(t *testing.T) {
	ev := database.AlertEvent{EventType: EventTypeIngestStalled, Severity: SeverityWarn}
	for _, c := range []struct {
		ruleType, min string
		want          bool
	}{
		{"ingest.*", SeverityInfo, true},
		{"ingest.stalled", SeverityWarn, true},
		{"ingest.rejecting", SeverityInfo, false},
		{"pipeline.*", SeverityInfo, false},
		{"*", SeverityInfo, true},
		{"ingest.*", SeverityError, false},
	} {
		if got := ruleMatchesEvent(database.AlertRule{EventType: c.ruleType, SeverityMin: c.min}, ev); got != c.want {
			t.Errorf("rule %s/%s: got %v", c.ruleType, c.min, got)
		}
	}
	sink := database.AlertEvent{EventType: EventTypePipelineSinkFailing, Severity: SeverityError}
	if !ruleMatchesEvent(database.AlertRule{EventType: "pipeline.*", SeverityMin: SeverityWarn}, sink) {
		t.Fatal("pipeline.* should match pipeline.sink_failing")
	}
}

// An ingest event goes through an ingest.* rule to a webhook, with the
// pipeline page as its link.
func TestIngestEventWebhookLink(t *testing.T) {
	srv, got := webhookServer(t)
	db := openTestDB(t)
	d := &Dispatcher{db: db, cfg: &config.Config{AppSecretKey: testSecret, AppURL: "https://chui.example.com"}}
	channelID, err := db.CreateAlertChannel("hook", ChannelTypeWebhook, encryptConfig(t, map[string]interface{}{
		WebhookCfgURL: srv.URL, WebhookCfgAllowHTTP: true, WebhookCfgAllowPrivate: true,
	}), true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("ingest", "ingest.*", SeverityWarn, true, 0, 1, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{{ChannelID: channelID, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	ingestEvent(t, db, EventTypeIngestStalled, PipelineAlertKey("p1", "stalled"), "2026-10-16T10:00:00Z", "firing", SeverityWarn)
	d.materializeEventJobs()
	d.processDueJobs()
	if status, _ := jobStatus(t, db); status != "sent" {
		t.Fatalf("job %s", status)
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	var ev WebhookEvent
	if err := json.Unmarshal(got.body, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.EventType != EventTypeIngestStalled || ev.Link != "https://chui.example.com/pipelines/p1" || !strings.Contains(string(ev.Payload), `"pipeline_id":"p1"`) {
		t.Fatalf("unexpected webhook body: %s", got.body)
	}
}
