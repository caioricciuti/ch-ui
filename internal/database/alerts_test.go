// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"strings"
	"testing"
	"time"
)

// TestAlertRuleChannelDispatchFlow proves the direct rule→channel delivery
// path: bind a channel to a rule, queue a dispatch job for an event, and
// verify the due-job view carries everything the dispatcher needs to send.
func TestAlertRuleChannelDispatchFlow(t *testing.T) {
	db := openTestDB(t)

	channelID, err := db.CreateAlertChannel("ops-mail", "smtp", "encrypted-config", true, "admin")
	if err != nil {
		t.Fatalf("CreateAlertChannel: %v", err)
	}
	ruleID, err := db.CreateAlertRule("policy alerts", "policy.violation", "warn", true, 300, 3, "", "", "admin")
	if err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}

	if err := db.ReplaceAlertRuleChannels(ruleID, []AlertRuleChannel{
		{ChannelID: channelID, Recipients: []string{"ops@example.com", "data@example.com"}, IsActive: true},
	}); err != nil {
		t.Fatalf("ReplaceAlertRuleChannels: %v", err)
	}

	bindings, err := db.ListActiveAlertRuleChannels(ruleID)
	if err != nil {
		t.Fatalf("ListActiveAlertRuleChannels: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected 1 active binding, got %d", len(bindings))
	}
	binding := bindings[0]
	if binding.ChannelID != channelID || binding.ChannelType != "smtp" || binding.ChannelName != "ops-mail" {
		t.Fatalf("unexpected binding: %+v", binding)
	}
	if len(binding.Recipients) != 2 {
		t.Fatalf("expected 2 recipients, got %v", binding.Recipients)
	}

	eventID, err := db.CreateAlertEvent(nil, "policy.violation", "warn", "Policy violated", "detail", nil, "fp-1", "ref-1")
	if err != nil {
		t.Fatalf("CreateAlertEvent: %v", err)
	}

	now := time.Now().UTC()
	if _, err := db.CreateAlertDispatchJob(eventID, ruleID, binding.ChannelID, binding.RecipientsJSON, 3, now.Add(-time.Second)); err != nil {
		t.Fatalf("CreateAlertDispatchJob: %v", err)
	}

	jobs, err := db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatalf("ListDueAlertDispatchJobs: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("expected 1 due job, got %d", len(jobs))
	}
	job := jobs[0]
	if job.RuleID != ruleID || job.ChannelID != channelID {
		t.Fatalf("job not bound to rule/channel: %+v", job.AlertDispatchJob)
	}
	if job.ChannelType != "smtp" || job.ChannelConfigEncrypted != "encrypted-config" {
		t.Fatalf("job missing channel details: %+v", job)
	}
	if !strings.Contains(job.RecipientsJSON, "ops@example.com") {
		t.Fatalf("job recipients not snapshotted: %q", job.RecipientsJSON)
	}

	// Cooldown dedupe is keyed on rule+channel+fingerprint.
	seen, err := db.HasRecentAlertDispatch(ruleID, channelID, "fp-1", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("HasRecentAlertDispatch: %v", err)
	}
	if !seen {
		t.Fatal("expected recent dispatch to be detected for dedupe")
	}
	seen, err = db.HasRecentAlertDispatch(ruleID, channelID, "fp-other", now.Add(-time.Minute))
	if err != nil {
		t.Fatalf("HasRecentAlertDispatch (other fingerprint): %v", err)
	}
	if seen {
		t.Fatal("did not expect dedupe hit for a different fingerprint")
	}

	if err := db.MarkAlertDispatchJobSent(job.ID, "msg-1"); err != nil {
		t.Fatalf("MarkAlertDispatchJobSent: %v", err)
	}
	jobs, err = db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatalf("ListDueAlertDispatchJobs after sent: %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no due jobs after send, got %d", len(jobs))
	}
}

// TestMigrateAlertRoutesToRuleChannels seeds the legacy routing schema
// (alert_rule_routes + alert_route_policies + alert_route_digests + a
// route_id-based alert_dispatch_jobs) and verifies the migration rewires
// bindings onto alert_rule_channels, snapshots queued job recipients, and
// drops the legacy tables.
func TestMigrateAlertRoutesToRuleChannels(t *testing.T) {
	db := openTestDB(t)

	channelID, err := db.CreateAlertChannel("legacy-mail", "resend", "enc", true, "admin")
	if err != nil {
		t.Fatalf("CreateAlertChannel: %v", err)
	}
	ruleID, err := db.CreateAlertRule("legacy rule", "schedule.failed", "warn", true, 0, 5, "", "", "admin")
	if err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}
	eventID, err := db.CreateAlertEvent(nil, "schedule.failed", "error", "boom", "boom", nil, "fp-legacy", "")
	if err != nil {
		t.Fatalf("CreateAlertEvent: %v", err)
	}

	// Recreate the legacy schema alongside the new tables.
	mustExec(t, db, `CREATE TABLE alert_rule_routes (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
		recipients_json TEXT NOT NULL,
		is_active INTEGER DEFAULT 1,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	mustExec(t, db, `CREATE TABLE alert_route_policies (
		route_id TEXT PRIMARY KEY REFERENCES alert_rule_routes(id) ON DELETE CASCADE,
		delivery_mode TEXT NOT NULL DEFAULT 'immediate'
	)`)
	mustExec(t, db, `CREATE TABLE alert_route_digests (
		id TEXT PRIMARY KEY,
		route_id TEXT NOT NULL REFERENCES alert_rule_routes(id) ON DELETE CASCADE
	)`)
	mustExec(t, db, `DROP TABLE alert_dispatch_jobs`)
	mustExec(t, db, `CREATE TABLE alert_dispatch_jobs (
		id TEXT PRIMARY KEY,
		event_id TEXT NOT NULL REFERENCES alert_events(id) ON DELETE CASCADE,
		rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
		route_id TEXT NOT NULL REFERENCES alert_rule_routes(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
		status TEXT NOT NULL DEFAULT 'queued',
		attempt_count INTEGER DEFAULT 0,
		max_attempts INTEGER DEFAULT 5,
		next_attempt_at TEXT NOT NULL,
		last_error TEXT,
		provider_message_id TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
		sent_at TEXT
	)`)

	// Two legacy routes to the same channel: recipients must be merged.
	mustExec(t, db,
		`INSERT INTO alert_rule_routes (id, rule_id, channel_id, recipients_json, is_active, created_at, updated_at)
		 VALUES ('route-1', ?, ?, '["a@example.com","b@example.com"]', 1, '2025-01-01T00:00:00Z', '2025-01-01T00:00:00Z')`,
		ruleID, channelID)
	mustExec(t, db,
		`INSERT INTO alert_rule_routes (id, rule_id, channel_id, recipients_json, is_active, created_at, updated_at)
		 VALUES ('route-2', ?, ?, '["b@example.com","c@example.com"]', 0, '2025-01-02T00:00:00Z', '2025-01-02T00:00:00Z')`,
		ruleID, channelID)
	mustExec(t, db, `INSERT INTO alert_route_policies (route_id, delivery_mode) VALUES ('route-1', 'digest')`)
	mustExec(t, db, `INSERT INTO alert_route_digests (id, route_id) VALUES ('digest-1', 'route-1')`)
	mustExec(t, db,
		`INSERT INTO alert_dispatch_jobs (id, event_id, rule_id, route_id, channel_id, status, next_attempt_at)
		 VALUES ('job-1', ?, ?, 'route-1', ?, 'queued', '2025-01-01T00:00:00Z')`,
		eventID, ruleID, channelID)

	if err := db.migrateAlertRoutesToRuleChannels(); err != nil {
		t.Fatalf("migrateAlertRoutesToRuleChannels: %v", err)
	}

	// Bindings migrated and merged per (rule, channel).
	bindings, err := db.ListAlertRuleChannels(ruleID)
	if err != nil {
		t.Fatalf("ListAlertRuleChannels: %v", err)
	}
	if len(bindings) != 1 {
		t.Fatalf("expected 1 merged binding, got %d", len(bindings))
	}
	if got := bindings[0].Recipients; len(got) != 3 {
		t.Fatalf("expected merged recipients [a b c], got %v", got)
	}
	if !bindings[0].IsActive {
		t.Fatal("expected merged binding active when any legacy route was active")
	}

	// Queued job kept, recipients snapshotted from its route, route_id gone.
	if got := countRows(t, db, "alert_dispatch_jobs"); got != 1 {
		t.Fatalf("expected 1 migrated dispatch job, got %d", got)
	}
	var recipientsJSON string
	if err := db.conn.QueryRow(`SELECT recipients_json FROM alert_dispatch_jobs WHERE id = 'job-1'`).Scan(&recipientsJSON); err != nil {
		t.Fatalf("read migrated job recipients: %v", err)
	}
	if !strings.Contains(recipientsJSON, "a@example.com") {
		t.Fatalf("expected job recipients snapshotted from route, got %q", recipientsJSON)
	}
	hasRouteID, err := db.columnExists("alert_dispatch_jobs", "route_id")
	if err != nil {
		t.Fatalf("columnExists: %v", err)
	}
	if hasRouteID {
		t.Fatal("expected route_id column removed from alert_dispatch_jobs")
	}

	// Legacy tables dropped.
	for _, table := range []string{"alert_rule_routes", "alert_route_policies", "alert_route_digests"} {
		exists, err := db.tableExists(table)
		if err != nil {
			t.Fatalf("tableExists(%s): %v", table, err)
		}
		if exists {
			t.Fatalf("expected legacy table %s to be dropped", table)
		}
	}

	// Idempotent: a second run is a no-op.
	if err := db.migrateAlertRoutesToRuleChannels(); err != nil {
		t.Fatalf("second migrateAlertRoutesToRuleChannels: %v", err)
	}
}
