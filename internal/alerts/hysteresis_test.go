// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// step is one evaluation fed to Step: the breach severity ("" for ok), or
// unknown.
type step struct {
	breach  string
	unknown bool
}

func TestStepStateMachine(t *testing.T) {
	ok := step{}
	warn := step{breach: SeverityWarn}
	crit := step{breach: SeverityCritical}
	unknown := step{unknown: true}
	cases := []struct {
		name        string
		forN, resN  int
		steps       []step
		transitions []string
		final       Hysteresis
	}{
		{"fires after two breaches", 2, 2, []step{warn, warn},
			[]string{"", TransitionFiring}, Hysteresis{State: StateFiring, Severity: SeverityWarn, BreachCount: 2}},
		{"single breach stays pending", 2, 2, []step{warn},
			[]string{""}, Hysteresis{State: StatePending, Severity: SeverityWarn, BreachCount: 1}},
		{"pending clears on ok", 2, 2, []step{warn, ok},
			[]string{"", ""}, Hysteresis{State: StateOK}},
		{"interrupted breaches restart the count", 2, 2, []step{warn, ok, warn},
			[]string{"", "", ""}, Hysteresis{State: StatePending, Severity: SeverityWarn, BreachCount: 1}},
		{"resolves after two oks", 2, 2, []step{warn, warn, ok, ok},
			[]string{"", TransitionFiring, "", TransitionResolved}, Hysteresis{State: StateOK, Severity: SeverityWarn}},
		{"breach between oks keeps firing", 2, 2, []step{warn, warn, ok, warn, ok},
			[]string{"", TransitionFiring, "", "", ""}, Hysteresis{State: StateFiring, Severity: SeverityWarn, OkCount: 1}},
		{"unknown neither breaches nor recovers", 2, 2, []step{warn, unknown, warn, unknown, ok, unknown, ok},
			[]string{"", "", TransitionFiring, "", "", "", TransitionResolved}, Hysteresis{State: StateOK, Severity: SeverityWarn}},
		{"warn to critical escalates once", 1, 1, []step{warn, crit, crit, warn},
			[]string{TransitionFiring, TransitionEscalated, "", ""}, Hysteresis{State: StateFiring, Severity: SeverityCritical, BreachCount: 4}},
		{"one evaluation fires and resolves", 1, 1, []step{crit, ok},
			[]string{TransitionFiring, TransitionResolved}, Hysteresis{State: StateOK, Severity: SeverityCritical}},
		{"zero counts mean one", 0, 0, []step{crit},
			[]string{TransitionFiring}, Hysteresis{State: StateFiring, Severity: SeverityCritical, BreachCount: 1}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var h Hysteresis
			for i, s := range tc.steps {
				var tr string
				h, tr = Step(h, s.breach, s.unknown, tc.forN, tc.resN)
				if tr != tc.transitions[i] {
					t.Fatalf("step %d: transition %q, want %q (state %+v)", i, tr, tc.transitions[i], h)
				}
			}
			if h != tc.final {
				t.Fatalf("final state %+v, want %+v", h, tc.final)
			}
		})
	}
}

func TestRuleMatchesEventWildcards(t *testing.T) {
	cases := []struct {
		rule, event string
		want        bool
	}{
		{"*", EventTypeHealthDisk, true},
		{"any", EventTypeSQLAlert, true},
		{"health.*", EventTypeHealthDisk, true},
		{"HEALTH.*", EventTypeHealthNodeUnreachable, true},
		{"health.*", EventTypeSQLAlert, false},
		{"health.*", "healthy.disk", false},
		{"sql.*", EventTypeSQLAlert, true},
		{EventTypeHealthDisk, EventTypeHealthDisk, true},
		{EventTypeHealthDisk, EventTypeHealthParts, false},
		{"telemetry.monitor", EventTypeTelemetryMonitor, true},
	}
	for _, tc := range cases {
		rule := database.AlertRule{EventType: tc.rule, SeverityMin: SeverityInfo}
		event := database.AlertEvent{EventType: tc.event, Severity: SeverityWarn}
		if got := ruleMatchesEvent(rule, event); got != tc.want {
			t.Errorf("rule %q event %q: got %v, want %v", tc.rule, tc.event, got, tc.want)
		}
	}
	// The severity floor still applies under a wildcard.
	rule := database.AlertRule{EventType: "health.*", SeverityMin: SeverityCritical}
	if ruleMatchesEvent(rule, database.AlertEvent{EventType: EventTypeHealthDisk, Severity: SeverityWarn}) {
		t.Error("warn event passed a critical floor")
	}
}

// TestHealthWildcardRuleDispatches checks that a health.* rule turns health
// events into dispatch jobs and leaves SQL alert events alone.
func TestHealthWildcardRuleDispatches(t *testing.T) {
	db := openTestDB(t)
	d := &Dispatcher{db: db}
	channelID, err := db.CreateAlertChannel("ops", ChannelTypeSMTP, "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("health", "health.*", SeverityWarn, true, 300, 3, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{{ChannelID: channelID, Recipients: []string{"ops@example.com"}, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	for _, ev := range []string{EventTypeHealthDisk, EventTypeHealthNodeUnreachable, EventTypeSQLAlert} {
		if _, err := db.CreateAlertEvent(nil, ev, SeverityCritical, ev, "m", nil, "fp-"+ev, ""); err != nil {
			t.Fatal(err)
		}
	}
	d.materializeEventJobs()
	jobs, err := db.ListDueAlertDispatchJobs(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs for the health events, got %d", len(jobs))
	}
	for _, j := range jobs {
		if j.EventType == EventTypeSQLAlert {
			t.Fatalf("sql.alert matched health.*: %+v", j)
		}
	}
}
