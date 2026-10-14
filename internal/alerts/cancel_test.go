// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// jobOf returns the status and last error of the job of an event.
func (f *suppressFixture) jobOf(eventID string) (string, string) {
	f.t.Helper()
	var status string
	var lastError *string
	if err := f.db.Conn().QueryRow(`SELECT status, last_error FROM alert_dispatch_jobs WHERE event_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, eventID).Scan(&status, &lastError); err != nil {
		f.t.Fatal(err)
	}
	if lastError == nil {
		return status, ""
	}
	return status, *lastError
}

// A job queued before its rule, channel or binding was turned off is
// cancelled with the reason instead of being sent.
func TestQueuedJobCancelledWhenRuleOrChannelOff(t *testing.T) {
	cases := []struct {
		name string
		off  func(f *suppressFixture)
		want string
	}{
		{"rule", func(f *suppressFixture) {
			if err := f.db.UpdateAlertRule(f.rule, "all", "*", SeverityInfo, false, 0, 3, "", ""); err != nil {
				t.Fatal(err)
			}
		}, "rule was turned off"},
		{"channel", func(f *suppressFixture) {
			if err := f.db.UpdateAlertChannel(f.chID, "hook", ChannelTypeWebhook, nil, false); err != nil {
				t.Fatal(err)
			}
		}, "channel was turned off"},
		{"binding", func(f *suppressFixture) {
			if err := f.db.ReplaceAlertRuleChannels(f.rule, []database.AlertRuleChannel{{ChannelID: f.chID, IsActive: false}}); err != nil {
				t.Fatal(err)
			}
		}, "removed from the rule or turned off"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newSuppressFixture(t)
			ev := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
			f.d.materializeEventJobs()
			if status, _ := f.jobOf(ev); status != "queued" {
				t.Fatalf("job status %s, want queued", status)
			}
			c.off(f)
			f.d.processDueJobs()
			status, reason := f.jobOf(ev)
			if status != database.AlertDispatchCancelled || !strings.Contains(reason, c.want) {
				t.Fatalf("job %s %q, want cancelled with %q", status, reason, c.want)
			}
			// Not a silence: the event stays processed.
			f.expect(ev, "processed", "", false)
		})
	}
}

// A silence created after a job was queued holds it back: the job is
// cancelled, the event reads as silenced, and when the silence ends a still
// firing incident is delivered once.
func TestQueuedJobHeldBySilenceAndReleased(t *testing.T) {
	f := newSuppressFixture(t)
	f.d.cfg = &config.Config{AppSecretKey: testSecret}
	fire := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	other := f.health(f.connB, "disk", "ch-1", "i2", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	sil := f.silence(database.AlertSilence{ConnectionID: sp(f.connA)})

	f.d.processDueJobs()
	status, reason := f.jobOf(fire)
	if status != database.AlertDispatchCancelled || !strings.Contains(reason, sil) {
		t.Fatalf("silenced job %s %q", status, reason)
	}
	f.expect(fire, database.AlertEventSilenced, "silence:"+sil, false)
	// The other connection is not covered: its job was attempted (and
	// failed on the test channel config), not cancelled.
	if status, _ := f.jobOf(other); status == database.AlertDispatchCancelled {
		t.Fatalf("uncovered job cancelled")
	}

	f.lapse(sil)
	f.d.materializeEventJobs()
	f.expect(fire, "processed", "", true)
	if status, _ := f.jobOf(fire); status != "queued" {
		t.Fatalf("released event must queue a new job, got %s", status)
	}
}
