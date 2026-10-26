// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// fastReadonly makes the read-only replica check fire and resolve on one
// evaluation.
func fastReadonly(t *testing.T, db *database.DB, conn string) {
	t.Helper()
	if err := db.SaveHealthAlertSettings(conn, []database.HealthAlertSetting{
		{CheckKind: CheckReplicaReadonly, Enabled: true, CritThreshold: f64(0), ForEvaluations: 1, ResolveEvaluations: 1},
	}); err != nil {
		t.Fatal(err)
	}
}

func eventsOf(t *testing.T, db *database.DB, eventType string) []database.AlertEvent {
	t.Helper()
	evs, err := db.ListAlertEvents(200, eventType, "")
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func stateOf(t *testing.T, db *database.DB, conn, check, node string) *database.HealthAlertState {
	t.Helper()
	states, err := db.ListHealthAlertStates(conn, false)
	if err != nil {
		t.Fatal(err)
	}
	for i := range states {
		if states[i].CheckKind == check && states[i].Node == node {
			return &states[i]
		}
	}
	return nil
}

// A second incident on the same node gets its own fingerprints, so a rule
// cooldown cannot swallow it after the first one resolved.
func TestHealthIncidentsHaveDistinctFingerprints(t *testing.T) {
	h, db, conn := evalHarvester(t)
	fastReadonly(t, db, conn)
	bad := []Sample{{Node: "ch-1", ReplicasReadonly: 1}}
	good := []Sample{{Node: "ch-1"}}
	t0 := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	h.evaluateHealthAlerts(conn, "", bad, false, noExec, t0)
	h.evaluateHealthAlerts(conn, "", good, false, noExec, t0.Add(time.Minute))
	h.evaluateHealthAlerts(conn, "", bad, false, noExec, t0.Add(2*time.Minute))
	h.evaluateHealthAlerts(conn, "", good, false, noExec, t0.Add(3*time.Minute))
	evs := eventsOf(t, db, alerts.EventTypeHealthReplicaReadonly)
	if len(evs) != 4 {
		t.Fatalf("want 2 firings and 2 resolves, got %d", len(evs))
	}
	seen := map[string]bool{}
	for _, ev := range evs {
		if seen[*ev.Fingerprint] {
			t.Fatalf("fingerprint repeated across incidents: %s", *ev.Fingerprint)
		}
		seen[*ev.Fingerprint] = true
	}
	first := t0.Format(time.RFC3339)
	second := t0.Add(2 * time.Minute).Format(time.RFC3339)
	for _, fp := range []string{
		HealthFingerprint(conn, CheckReplicaReadonly, "ch-1", "critical:"+first),
		HealthFingerprint(conn, CheckReplicaReadonly, "ch-1", "resolved:"+first),
		HealthFingerprint(conn, CheckReplicaReadonly, "ch-1", "critical:"+second),
		HealthFingerprint(conn, CheckReplicaReadonly, "ch-1", "resolved:"+second),
	} {
		if !seen[fp] {
			t.Errorf("missing fingerprint %s in %v", fp, seen)
		}
	}
}

// An event that cannot be written leaves the state where it was, so the
// next poll raises it again.
func TestHealthStateWaitsForItsEvent(t *testing.T) {
	h, db, conn := evalHarvester(t)
	fastReadonly(t, db, conn)
	if _, err := db.Conn().Exec(`CREATE TRIGGER fail_events BEFORE INSERT ON alert_events BEGIN SELECT RAISE(ABORT, 'disk full'); END`); err != nil {
		t.Fatal(err)
	}
	bad := []Sample{{Node: "ch-1", ReplicasReadonly: 1}}
	now := time.Now()
	h.evaluateHealthAlerts(conn, "", bad, false, noExec, now)
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-1"); st != nil && st.State == alerts.StateFiring {
		t.Fatalf("state advanced to firing without an event: %+v", st)
	}
	if _, err := db.Conn().Exec(`DROP TRIGGER fail_events`); err != nil {
		t.Fatal(err)
	}
	h.evaluateHealthAlerts(conn, "", bad, false, noExec, now)
	st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-1")
	if st == nil || st.State != alerts.StateFiring || st.LastEventID == nil {
		t.Fatalf("want firing with an event id after the retry, got %+v", st)
	}
	if n := len(eventsOf(t, db, alerts.EventTypeHealthReplicaReadonly)); n != 1 {
		t.Fatalf("want 1 firing event, got %d", n)
	}
}

// Incidents whose firing went into the summary are resolved in a summary
// too, not one by one; the summary fingerprint uses the poll time.
func TestSummarizedIncidentsResolveInSummary(t *testing.T) {
	h, db, conn := evalHarvester(t)
	fastReadonly(t, db, conn)
	var bad, good []Sample
	for i := 0; i < 25; i++ {
		node := fmt.Sprintf("n-%02d", i)
		bad = append(bad, Sample{Node: node, ReplicasReadonly: 1})
		good = append(good, Sample{Node: node})
	}
	t0 := time.Date(2026, 10, 13, 10, 0, 0, 0, time.UTC)
	h.evaluateHealthAlerts(conn, "", bad, false, noExec, t0)
	h.evaluateHealthAlerts(conn, "", good, false, noExec, t0.Add(time.Minute))

	resolved := 0
	for _, ev := range eventsOf(t, db, alerts.EventTypeHealthReplicaReadonly) {
		if strings.HasPrefix(ev.Title, "[RESOLVED] ") {
			resolved++
		}
	}
	if resolved != maxHealthEventsPerPoll {
		t.Fatalf("want %d individual resolves, got %d", maxHealthEventsPerPoll, resolved)
	}
	summaries := eventsOf(t, db, alerts.EventTypeHealthSummary)
	if len(summaries) != 2 {
		t.Fatalf("want a firing and a resolving summary, got %d", len(summaries))
	}
	fps := map[string]bool{}
	resolvingSummary := false
	for _, s := range summaries {
		fps[*s.Fingerprint] = true
		if strings.Contains(s.Title, "5 more") && s.PayloadJSON != nil && strings.Contains(*s.PayloadJSON, `"resolved":5`) {
			resolvingSummary = true
		}
	}
	if !resolvingSummary {
		t.Fatalf("no summary of the 5 summarized resolves: %+v", summaries)
	}
	for _, at := range []time.Time{t0, t0.Add(time.Minute)} {
		if fp := "health:" + conn + ":summary:" + strconv.FormatInt(at.Unix(), 10); !fps[fp] {
			t.Errorf("missing summary fingerprint %s in %v", fp, fps)
		}
	}
	if states, _ := db.ListHealthAlertStates(conn, true); len(states) != 0 {
		t.Fatalf("all incidents must be closed, got %d", len(states))
	}
}

// A node that left the cluster has its incident resolved and its state
// dropped; without the node list, that waits for ghostExpiry.
func TestDepartedNodeIsResolved(t *testing.T) {
	h, db, conn := evalHarvester(t)
	fastReadonly(t, db, conn)
	now := time.Now()
	both := []Sample{{Node: "ch-1"}, {Node: "ch-2", ReplicasReadonly: 1}}
	h.evaluateHealthAlerts(conn, "prod", both, true, noExec, now)
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-2"); st == nil || st.State != alerts.StateFiring {
		t.Fatalf("ch-2 must fire: %+v", st)
	}

	// Node list unknown and the node was seen recently: kept.
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1"}}, false, noExec, now)
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-2"); st == nil || st.State != alerts.StateFiring {
		t.Fatalf("ch-2 must stay firing while its absence is unexplained: %+v", st)
	}

	// The node list is known and ch-2 is gone.
	h.evaluateHealthAlerts(conn, "prod", []Sample{{Node: "ch-1"}}, true, noExec, now)
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-2"); st != nil {
		t.Fatalf("state of the departed node kept: %+v", st)
	}
	var resolve *database.AlertEvent
	for _, ev := range eventsOf(t, db, alerts.EventTypeHealthReplicaReadonly) {
		if strings.HasPrefix(ev.Title, "[RESOLVED] ") {
			ev := ev
			resolve = &ev
		}
	}
	if resolve == nil || !strings.Contains(resolve.Message, "no longer part of the cluster") || resolve.Severity != alerts.SeverityCritical {
		t.Fatalf("resolve of the departed node: %+v", resolve)
	}

	// Without a node list, an absence past ghostExpiry closes the incident.
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-3", ReplicasReadonly: 1}}, false, noExec, now)
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1"}}, false, noExec, now.Add(ghostExpiry-time.Hour))
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-3"); st == nil {
		t.Fatal("ch-3 dropped before the expiry")
	}
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1"}}, false, noExec, now.Add(ghostExpiry+time.Hour))
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-3"); st != nil {
		t.Fatalf("ch-3 kept after the expiry: %+v", st)
	}
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-1"); st == nil {
		t.Fatal("ch-1 is still polled and must keep its state")
	}
}

// A critical incident that settles at warn level raises a de-escalation
// after the resolve evaluations.
func TestHealthDeescalates(t *testing.T) {
	h, db, conn := evalHarvester(t)
	if err := db.SaveHealthAlertSettings(conn, []database.HealthAlertSetting{
		{CheckKind: CheckReplicationLag, Enabled: true, WarnThreshold: f64(60), CritThreshold: f64(300), ForEvaluations: 1, ResolveEvaluations: 2},
	}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1", ReplicationMaxDelay: 900}}, false, noExec, now)
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1", ReplicationMaxDelay: 120}}, false, noExec, now)
	if n := len(eventsOf(t, db, alerts.EventTypeHealthReplicationLag)); n != 1 {
		t.Fatalf("one warn poll must not de-escalate yet, got %d events", n)
	}
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1", ReplicationMaxDelay: 120}}, false, noExec, now)
	evs := eventsOf(t, db, alerts.EventTypeHealthReplicationLag)
	if len(evs) != 2 {
		t.Fatalf("want firing and de-escalation, got %d", len(evs))
	}
	var down *database.AlertEvent
	for i := range evs {
		if evs[i].PayloadJSON != nil && strings.Contains(*evs[i].PayloadJSON, `"deescalated":true`) {
			down = &evs[i]
		}
	}
	if down == nil || down.Severity != alerts.SeverityWarn || !strings.Contains(*down.Fingerprint, ":deescalated:warn:") {
		t.Fatalf("de-escalation event %+v", down)
	}
	if st := stateOf(t, db, conn, CheckReplicationLag, "ch-1"); st == nil || st.Severity == nil || *st.Severity != alerts.SeverityWarn {
		t.Fatalf("state severity after de-escalation: %+v", st)
	}
}

// Turning a check off resolves its firing nodes and clears its state, and a
// poll that saves afterwards cannot bring the state back.
func TestDisabledCheckClosesAndStaysClosed(t *testing.T) {
	h, db, conn := evalHarvester(t)
	fastReadonly(t, db, conn)
	now := time.Now()
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1", ReplicasReadonly: 1}}, false, noExec, now)
	stale := stateOf(t, db, conn, CheckReplicaReadonly, "ch-1")
	if stale == nil || stale.State != alerts.StateFiring {
		t.Fatalf("want firing, got %+v", stale)
	}

	if err := db.SaveHealthAlertSettings(conn, []database.HealthAlertSetting{
		{CheckKind: CheckReplicaReadonly, Enabled: false, CritThreshold: f64(0), ForEvaluations: 1, ResolveEvaluations: 1},
	}); err != nil {
		t.Fatal(err)
	}
	if err := CloseCheckIncidents(db, conn, CheckReplicaReadonly, now); err != nil {
		t.Fatal(err)
	}
	var resolve *database.AlertEvent
	for _, ev := range eventsOf(t, db, alerts.EventTypeHealthReplicaReadonly) {
		if strings.HasPrefix(ev.Title, "[RESOLVED] ") {
			ev := ev
			resolve = &ev
		}
	}
	if resolve == nil || !strings.Contains(resolve.Message, "check was turned off") {
		t.Fatalf("disable must resolve the incident: %+v", resolve)
	}
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-1"); st != nil {
		t.Fatalf("state kept after disable: %+v", st)
	}

	// The save of a poll that loaded the state before the disable.
	if err := db.SaveHealthAlertStates([]database.HealthAlertState{*stale}); err != nil {
		t.Fatal(err)
	}
	if st := stateOf(t, db, conn, CheckReplicaReadonly, "ch-1"); st != nil {
		t.Fatalf("a late save resurrected the state: %+v", st)
	}
}

// Sampling turned off: the next tick resolves the incidents of the checks
// that read samples, once, and leaves the connection check, which still
// runs on probes, as it is.
func TestSamplingOffClosesSampleIncidents(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	h := NewHarvester(NewStore(db), db, tunnel.NewGateway(db), "test-secret", func() bool { return true })
	t.Cleanup(h.gateway.Stop)
	off := DefaultSettings(conn)
	off.Enabled = false
	if _, err := h.store.UpsertSettings(off); err != nil {
		t.Fatal(err)
	}
	crit := alerts.SeverityCritical
	since := "2026-10-13T10:00:00Z"
	eventID := "fired-event"
	if err := db.SaveHealthAlertStates([]database.HealthAlertState{
		{ConnectionID: conn, CheckKind: CheckDisk, Node: "ch-1", State: alerts.StateFiring, Severity: &crit, Since: &since, LastEventID: &eventID},
		{ConnectionID: conn, CheckKind: CheckDisk, Node: "ch-2", State: alerts.StatePending, BreachCount: 1},
		{ConnectionID: conn, CheckKind: CheckConnectionUnreachable, Node: ConnectionNode, State: alerts.StateFiring, Severity: &crit, Since: &since, LastEventID: &eventID},
	}); err != nil {
		t.Fatal(err)
	}

	h.tick()
	h.wait()
	pollAgain(h)
	h.tick()
	h.wait()

	var resolves []database.AlertEvent
	for _, ev := range eventsOf(t, db, alerts.EventTypeHealthDisk) {
		if strings.HasPrefix(ev.Title, "[RESOLVED] ") {
			resolves = append(resolves, ev)
		}
	}
	if len(resolves) != 1 || !strings.Contains(resolves[0].Message, "sampling was turned off") {
		t.Fatalf("want one resolve of the disk incident, got %+v", resolves)
	}
	if !strings.Contains(*resolves[0].PayloadJSON, `"alert_key":"`+HealthAlertKey(conn, CheckDisk, "ch-1")+`"`) ||
		!strings.Contains(*resolves[0].PayloadJSON, `"incident_id":"`+since+`"`) {
		t.Fatalf("resolve must name the incident so its ack clears: %s", *resolves[0].PayloadJSON)
	}
	if st := stateOf(t, db, conn, CheckDisk, "ch-1"); st != nil {
		t.Fatalf("firing disk state kept: %+v", st)
	}
	if st := stateOf(t, db, conn, CheckDisk, "ch-2"); st != nil {
		t.Fatalf("pending disk state kept: %+v", st)
	}
	if st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode); st == nil || st.State != alerts.StateFiring {
		t.Fatalf("connection check must keep its state: %+v", st)
	}
	if n := len(connectionEvents(t, db)); n != 0 {
		t.Fatalf("connection check closed: %d events", n)
	}
}
