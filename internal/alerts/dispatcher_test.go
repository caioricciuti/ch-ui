// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

func openTestDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := dbtest.Open(t, "test.db")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// TestMaterializeEventJobs_RuleToChannelDispatch proves that alert events of
// all three supported types are matched against enabled rules and turned into
// dispatch jobs on the rules' directly-bound channels.
func TestMaterializeEventJobs_RuleToChannelDispatch(t *testing.T) {
	db := openTestDB(t)
	d := &Dispatcher{db: db}

	channelID, err := db.CreateAlertChannel("ops", ChannelTypeSMTP, "enc", true, "admin")
	if err != nil {
		t.Fatalf("CreateAlertChannel: %v", err)
	}

	eventTypes := []string{EventTypePolicyViolation, EventTypeScheduleFailed, EventTypeScheduleSlow}
	for _, eventType := range eventTypes {
		ruleID, err := db.CreateAlertRule("rule "+eventType, eventType, SeverityWarn, true, 300, 3, "", "", "admin")
		if err != nil {
			t.Fatalf("CreateAlertRule(%s): %v", eventType, err)
		}
		if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{
			{ChannelID: channelID, Recipients: []string{"ops@example.com"}, IsActive: true},
		}); err != nil {
			t.Fatalf("ReplaceAlertRuleChannels(%s): %v", eventType, err)
		}
		if _, err := db.CreateAlertEvent(nil, eventType, SeverityError, "title "+eventType, "msg", nil, "fp-"+eventType, ""); err != nil {
			t.Fatalf("CreateAlertEvent(%s): %v", eventType, err)
		}
	}

	d.materializeEventJobs()

	jobs, err := db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatalf("ListDueAlertDispatchJobs: %v", err)
	}
	if len(jobs) != len(eventTypes) {
		t.Fatalf("expected %d dispatch jobs (one per event type), got %d", len(eventTypes), len(jobs))
	}
	seen := map[string]bool{}
	for _, job := range jobs {
		seen[job.EventType] = true
		if job.ChannelID != channelID {
			t.Fatalf("job for %s not bound to channel: %+v", job.EventType, job.AlertDispatchJob)
		}
		if parsed := parseRecipients(job.RecipientsJSON); len(parsed) != 1 || parsed[0] != "ops@example.com" {
			t.Fatalf("job for %s has wrong recipients: %q", job.EventType, job.RecipientsJSON)
		}
	}
	for _, eventType := range eventTypes {
		if !seen[eventType] {
			t.Fatalf("no dispatch job materialized for %s", eventType)
		}
	}

	// Cooldown dedupe: a second event with the same fingerprint within the
	// rule cooldown must not create another job.
	if _, err := db.CreateAlertEvent(nil, EventTypePolicyViolation, SeverityError, "dup", "dup", nil, "fp-"+EventTypePolicyViolation, ""); err != nil {
		t.Fatalf("CreateAlertEvent (dup): %v", err)
	}
	d.materializeEventJobs()
	jobs, err = db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatalf("ListDueAlertDispatchJobs after dup: %v", err)
	}
	if len(jobs) != len(eventTypes) {
		t.Fatalf("expected cooldown dedupe to keep %d jobs, got %d", len(eventTypes), len(jobs))
	}

	// Severity below the rule minimum must not dispatch.
	if _, err := db.CreateAlertEvent(nil, EventTypeScheduleSlow, SeverityInfo, "low", "low", nil, "fp-low", ""); err != nil {
		t.Fatalf("CreateAlertEvent (low severity): %v", err)
	}
	d.materializeEventJobs()
	jobs, err = db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatalf("ListDueAlertDispatchJobs after low severity: %v", err)
	}
	if len(jobs) != len(eventTypes) {
		t.Fatalf("expected low-severity event to be skipped, got %d jobs", len(jobs))
	}

	// All events must be marked processed.
	pending, err := db.ListNewAlertEvents(10)
	if err != nil {
		t.Fatalf("ListNewAlertEvents: %v", err)
	}
	if len(pending) != 0 {
		t.Fatalf("expected all events processed, %d still new", len(pending))
	}
}
