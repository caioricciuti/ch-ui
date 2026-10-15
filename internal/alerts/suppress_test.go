// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

type suppressFixture struct {
	t            *testing.T
	db           *database.DB
	d            *Dispatcher
	rule, chID   string
	connA, connB string
}

func newSuppressFixture(t *testing.T) *suppressFixture {
	t.Helper()
	db := openTestDB(t)
	f := &suppressFixture{t: t, db: db, d: &Dispatcher{db: db}}
	var err error
	if f.connA, err = db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"}); err != nil {
		t.Fatal(err)
	}
	if f.connB, err = db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"}); err != nil {
		t.Fatal(err)
	}
	if f.chID, err = db.CreateAlertChannel("hook", ChannelTypeWebhook, "enc", true, "admin"); err != nil {
		t.Fatal(err)
	}
	if f.rule, err = db.CreateAlertRule("all", "*", SeverityInfo, true, 0, 3, "", "", "admin"); err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(f.rule, []database.AlertRuleChannel{{ChannelID: f.chID, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	return f
}

// health writes a health check event the way the harvester does.
func (f *suppressFixture) health(conn, check, node, incident, status, severity string, extra map[string]interface{}) string {
	f.t.Helper()
	payload := map[string]interface{}{
		"connection_id": conn, "check": check, "node": node, "status": status,
		"alert_key": "health:" + conn + ":" + check + ":" + node, "incident_id": incident,
	}
	for k, v := range extra {
		payload[k] = v
	}
	c := conn
	id, err := f.db.CreateAlertEvent(&c, "health."+check, severity, check+" "+status, "m", payload,
		"health:"+conn+":"+check+":"+node+":"+status+":"+incident+":"+time.Now().Format(time.RFC3339Nano), "health:"+check)
	if err != nil {
		f.t.Fatal(err)
	}
	// Events are ordered by created_at (seconds): keep them apart.
	f.bump(id)
	return id
}

var eventClock = time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC)

// bump gives each event its own created_at, in creation order.
func (f *suppressFixture) bump(id string) {
	eventClock = eventClock.Add(time.Second)
	if _, err := f.db.Conn().Exec(`UPDATE alert_events SET created_at = ? WHERE id = ?`, eventClock.Format(time.RFC3339), id); err != nil {
		f.t.Fatal(err)
	}
}

func (f *suppressFixture) silence(s database.AlertSilence) string {
	f.t.Helper()
	now := time.Now().UTC()
	if s.StartsAt == "" {
		s.StartsAt = now.Add(-time.Minute).Format(time.RFC3339)
	}
	if s.EndsAt == "" {
		s.EndsAt = now.Add(time.Hour).Format(time.RFC3339)
	}
	s.Reason = "maintenance"
	id, err := f.db.CreateAlertSilence(s)
	if err != nil {
		f.t.Fatal(err)
	}
	return id
}

func (f *suppressFixture) status(id string) (string, string) {
	f.t.Helper()
	ev, err := f.db.GetAlertEvent(id)
	if err != nil || ev == nil {
		f.t.Fatalf("event %s: %v", id, err)
	}
	by := ""
	if ev.SuppressedBy != nil {
		by = *ev.SuppressedBy
	}
	return ev.Status, by
}

func (f *suppressFixture) delivered(id string) bool {
	f.t.Helper()
	ok, err := f.db.AlertDispatchJobExists(id, f.rule, f.chID)
	if err != nil {
		f.t.Fatal(err)
	}
	return ok
}

func (f *suppressFixture) expect(id, status, by string, delivered bool) {
	f.t.Helper()
	gotStatus, gotBy := f.status(id)
	if gotStatus != status || gotBy != by || f.delivered(id) != delivered {
		f.t.Fatalf("event %s: status %q by %q delivered %v, want %q %q %v", id, gotStatus, gotBy, f.delivered(id), status, by, delivered)
	}
}

func sp(s string) *string { return &s }

func TestSilenceMatchers(t *testing.T) {
	f := newSuppressFixture(t)
	byConn := f.silence(database.AlertSilence{ConnectionID: sp(f.connA)})
	onA := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	onB := f.health(f.connB, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	f.expect(onA, database.AlertEventSilenced, "silence:"+byConn, false)
	f.expect(onB, "processed", "", true)

	byType := f.silence(database.AlertSilence{EventType: sp("health.*"), ConnectionID: sp(f.connB)})
	healthB := f.health(f.connB, "parts", "ch-1", "i2", "firing", SeverityWarn, nil)
	c := f.connB
	sqlB, _ := f.db.CreateAlertEvent(&c, EventTypeSQLAlert, SeverityWarn, "sql", "m", map[string]interface{}{"status": "firing", "alert_key": "sql:x", "incident_id": "t"}, "sql:x:firing:1", "x")
	f.bump(sqlB)
	f.d.materializeEventJobs()
	f.expect(healthB, database.AlertEventSilenced, "silence:"+byType, false)
	f.expect(sqlB, "processed", "", true)

	for _, id := range []string{byConn, byType} {
		if _, err := f.db.ExpireAlertSilence(id, "admin", time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	bySource := f.silence(database.AlertSilence{SourceRef: sp("y")})
	sqlY, _ := f.db.CreateAlertEvent(&c, EventTypeSQLAlert, SeverityWarn, "sql", "m", map[string]interface{}{"status": "firing", "alert_key": "sql:y", "incident_id": "t"}, "sql:y:firing:1", "y")
	f.bump(sqlY)
	byNode := f.silence(database.AlertSilence{Node: sp("ch-9"), SourceRef: sp("health:mutation_stuck")})
	node9 := f.health(f.connB, "mutation_stuck", "ch-9", "i3", "firing", SeverityWarn, nil)
	node8 := f.health(f.connB, "mutation_stuck", "ch-8", "i3", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	f.expect(sqlY, database.AlertEventSilenced, "silence:"+bySource, false)
	f.expect(node9, database.AlertEventSilenced, "silence:"+byNode, false)
	f.expect(node8, "processed", "", true)

	// No matchers selects nothing; a scheduled silence holds nothing yet.
	if SilenceMatches(database.AlertSilence{}, database.AlertEvent{EventType: "health.disk"}, "") {
		t.Fatal("a silence without matchers must match nothing")
	}
	f.silence(database.AlertSilence{ConnectionID: sp(f.connB), StartsAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), EndsAt: time.Now().UTC().Add(2 * time.Hour).Format(time.RFC3339)})
	later := f.health(f.connB, "replication_lag", "ch-1", "i4", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	f.expect(later, "processed", "", true)
}

// A silence only affects events that are still new.
func TestSilenceLeavesProcessedEvents(t *testing.T) {
	f := newSuppressFixture(t)
	ev := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	f.silence(database.AlertSilence{ConnectionID: sp(f.connA)})
	f.d.materializeEventJobs()
	f.expect(ev, "processed", "", true)
}

func (f *suppressFixture) ack(key, incident, severity string) database.AlertAck {
	f.t.Helper()
	a, err := f.db.CreateAlertAck(database.AlertAck{AlertKey: key, IncidentID: sp(incident), Severity: sp(severity), ConnectionID: sp(f.connA)})
	if err != nil {
		f.t.Fatal(err)
	}
	return *a
}

func (f *suppressFixture) ackState(id string) string {
	f.t.Helper()
	a, err := f.db.GetAlertAck(id)
	if err != nil || a == nil {
		f.t.Fatalf("ack %s: %v", id, err)
	}
	if a.ClearedReason == nil {
		return ""
	}
	return *a.ClearedReason
}

func TestAckSuppressesUntilResolve(t *testing.T) {
	f := newSuppressFixture(t)
	key := "health:" + f.connA + ":disk:ch-1"
	fire := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityCritical, nil)
	f.d.materializeEventJobs()
	f.expect(fire, "processed", "", true)
	a := f.ack(key, "i1", SeverityCritical)

	deesc := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, map[string]interface{}{"deescalated": true})
	f.d.materializeEventJobs()
	f.expect(deesc, database.AlertEventAcknowledged, "ack:"+a.ID, false)

	// Back to critical: not above the acknowledged severity.
	reesc := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityCritical, map[string]interface{}{"escalated": true})
	f.d.materializeEventJobs()
	f.expect(reesc, database.AlertEventAcknowledged, "ack:"+a.ID, false)

	// The resolve still goes out and clears the ack.
	res := f.health(f.connA, "disk", "ch-1", "i1", "resolved", SeverityCritical, nil)
	f.d.materializeEventJobs()
	f.expect(res, "processed", "", true)
	if r := f.ackState(a.ID); r != "resolved" {
		t.Fatalf("ack cleared as %q, want resolved", r)
	}

	// The next incident is not covered.
	next := f.health(f.connA, "disk", "ch-1", "i2", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	f.expect(next, "processed", "", true)
}

func TestAckClearedByEscalation(t *testing.T) {
	f := newSuppressFixture(t)
	key := "health:" + f.connA + ":disk:ch-1"
	f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	a := f.ack(key, "i1", SeverityWarn)
	esc := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityCritical, map[string]interface{}{"escalated": true})
	f.d.materializeEventJobs()
	f.expect(esc, "processed", "", true)
	if r := f.ackState(a.ID); r != "escalated" {
		t.Fatalf("ack cleared as %q, want escalated", r)
	}
}

func TestAckWithoutIncidentExpires(t *testing.T) {
	f := newSuppressFixture(t)
	c := f.connA
	mk := func() string {
		id, err := f.db.CreateAlertEvent(&c, EventTypeScheduleFailed, SeverityError, "failed", "m", map[string]interface{}{"schedule_id": "s"}, "schedule:s:error", "run")
		if err != nil {
			t.Fatal(err)
		}
		f.bump(id)
		return id
	}
	first := mk()
	f.d.materializeEventJobs()
	ev, _ := f.db.GetAlertEvent(first)
	id := IdentifyEvent(*ev)
	if id.AlertKey != "schedule:s:error" || id.IncidentID != "" {
		t.Fatalf("identity %+v", id)
	}
	a, err := f.db.CreateAlertAck(database.AlertAck{AlertKey: id.AlertKey, Severity: sp(SeverityError), ExpiresAt: sp(time.Now().UTC().Add(time.Hour).Format(time.RFC3339))})
	if err != nil {
		t.Fatal(err)
	}
	second := mk()
	f.d.materializeEventJobs()
	f.expect(second, database.AlertEventAcknowledged, "ack:"+a.ID, false)

	if _, err := f.db.Conn().Exec(`UPDATE alert_acks SET expires_at = ? WHERE id = ?`, time.Now().UTC().Add(-time.Second).Format(time.RFC3339), a.ID); err != nil {
		t.Fatal(err)
	}
	third := mk()
	f.d.materializeEventJobs()
	f.expect(third, "processed", "", true)
	if r := f.ackState(a.ID); r != "expired" {
		t.Fatalf("ack cleared as %q, want expired", r)
	}
}

func (f *suppressFixture) lapse(silenceID string) {
	f.t.Helper()
	past := time.Now().UTC().Add(-time.Second).Format(time.RFC3339)
	if _, err := f.db.Conn().Exec(`UPDATE alert_silences SET ends_at = ? WHERE id = ?`, past, silenceID); err != nil {
		f.t.Fatal(err)
	}
}

// When a silence ends, an incident it held back that is still firing is
// delivered once, by its latest held-back event.
func TestSilenceEndRedelivers(t *testing.T) {
	f := newSuppressFixture(t)
	sil := f.silence(database.AlertSilence{ConnectionID: sp(f.connA)})
	fire := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityCritical, nil)
	deesc := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, map[string]interface{}{"deescalated": true})
	// A closed incident is not redelivered.
	gone := f.health(f.connA, "parts", "ch-1", "i9", "firing", SeverityWarn, nil)
	goneRes := f.health(f.connA, "parts", "ch-1", "i9", "resolved", SeverityWarn, nil)
	f.d.materializeEventJobs()
	for _, id := range []string{fire, deesc, gone, goneRes} {
		f.expect(id, database.AlertEventSilenced, "silence:"+sil, false)
	}

	f.lapse(sil)
	f.d.materializeEventJobs()
	f.expect(fire, database.AlertEventSilenced, "silence:"+sil, false)
	f.expect(deesc, "processed", "", true)
	f.expect(gone, database.AlertEventSilenced, "silence:"+sil, false)
	s, _ := f.db.GetAlertSilence(sil)
	if s.ExpiredAt == nil || s.Status != database.SilenceExpired {
		t.Fatalf("lapsed silence not marked expired: %+v", s)
	}

	// Once only: the next tick changes nothing, and the resolve that
	// follows a redelivered incident goes out.
	f.d.materializeEventJobs()
	res := f.health(f.connA, "disk", "ch-1", "i1", "resolved", SeverityWarn, nil)
	f.d.materializeEventJobs()
	f.expect(res, "processed", "", true)
}

// A resolve whose firing was silenced is dropped, even once the silence
// has ended.
func TestResolveOfSilencedIncidentDropped(t *testing.T) {
	f := newSuppressFixture(t)
	sil := f.silence(database.AlertSilence{ConnectionID: sp(f.connA)})
	fire := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	f.d.materializeEventJobs()
	f.expect(fire, database.AlertEventSilenced, "silence:"+sil, false)

	res := f.health(f.connA, "disk", "ch-1", "i1", "resolved", SeverityWarn, nil)
	f.lapse(sil)
	f.d.materializeEventJobs()
	f.expect(fire, database.AlertEventSilenced, "silence:"+sil, false)
	f.expect(res, database.AlertEventSilenced, "silence:"+sil, false)
}

func TestRuleConnectionScope(t *testing.T) {
	a, b := "conn-a", "conn-b"
	scoped := database.AlertRule{EventType: "*", SeverityMin: SeverityInfo, ConnectionIDs: []string{a}}
	open := database.AlertRule{EventType: "*", SeverityMin: SeverityInfo}
	evA := database.AlertEvent{EventType: EventTypeSQLAlert, Severity: SeverityWarn, ConnectionID: &a}
	evB := database.AlertEvent{EventType: EventTypeSQLAlert, Severity: SeverityWarn, ConnectionID: &b}
	evNone := database.AlertEvent{EventType: EventTypePolicyViolation, Severity: SeverityWarn}
	cases := []struct {
		rule database.AlertRule
		ev   database.AlertEvent
		want bool
	}{
		{scoped, evA, true}, {scoped, evB, false}, {scoped, evNone, false},
		{open, evA, true}, {open, evB, true}, {open, evNone, true},
	}
	for i, c := range cases {
		if got := ruleMatchesEvent(c.rule, c.ev); got != c.want {
			t.Errorf("case %d: got %v want %v", i, got, c.want)
		}
	}

	// Stored and read back through the dispatcher.
	f := newSuppressFixture(t)
	if err := f.db.SetAlertRuleConnections(f.rule, []string{f.connA}); err != nil {
		t.Fatal(err)
	}
	onA := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	onB := f.health(f.connB, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	none, _ := f.db.CreateAlertEvent(nil, EventTypePolicyViolation, SeverityWarn, "p", "m", nil, "fp", "")
	f.d.materializeEventJobs()
	f.expect(onA, "processed", "", true)
	f.expect(onB, "processed", "", false)
	f.expect(none, "processed", "", false)
}

// Without a license the dispatcher does nothing: events, silences and acks
// keep their state.
func TestLicenseLapseKeepsState(t *testing.T) {
	f := newSuppressFixture(t)
	f.d.allow = func() bool { return false }
	sil := f.silence(database.AlertSilence{ConnectionID: sp(f.connA)})
	ev := f.health(f.connA, "disk", "ch-1", "i1", "firing", SeverityWarn, nil)
	f.lapse(sil)
	f.d.tick()
	f.expect(ev, "new", "", false)
	if s, _ := f.db.GetAlertSilence(sil); s.ExpiredAt != nil {
		t.Fatalf("silence changed without a license: %+v", s)
	}
}

func TestAlertKeyFromFingerprint(t *testing.T) {
	for fp, want := range map[string]string{
		"health:c1:disk:ch-1:critical:2026-10-13T10:00:00Z": "health:c1:disk:ch-1",
		"sql:abc:firing:123":    "sql:abc",
		"schedule:s:error":      "schedule:s:error",
		"health:c1:summary:123": "health:c1:summary:123",
	} {
		if got := AlertKeyFromFingerprint(fp); got != want {
			t.Errorf("%s: %s, want %s", fp, got, want)
		}
	}
}

// drift writes an access drift event the way access history does.
func (f *suppressFixture) drift(incident, status, severity, hash string, extra map[string]interface{}) string {
	f.t.Helper()
	payload := map[string]interface{}{
		"connection_id": f.connA, "alert_key": "access:" + f.connA, "status": status, "incident_id": incident,
		"incident_severity": severity, "hash": hash, "link_path": "/governance?section=access-history",
	}
	for k, v := range extra {
		payload[k] = v
	}
	c := f.connA
	id, err := f.db.CreateAlertEvent(&c, EventTypeAccessDrift, severity, "drift "+status, "m", payload,
		"access:"+f.connA+":"+status+":"+hash, "access:drift")
	if err != nil {
		f.t.Fatal(err)
	}
	f.bump(id)
	return id
}

// An ack on a drift incident holds back further drift of that incident; an
// escalation to a mixed apply goes out and ends it, and so does the resolve.
func TestDriftAckSuppressesUntilResolve(t *testing.T) {
	f := newSuppressFixture(t)
	key := "access:" + f.connA
	fire := f.drift("i1", "firing", SeverityWarn, "h1", nil)
	f.d.materializeEventJobs()
	f.expect(fire, "processed", "", true)
	a := f.ack(key, "i1", SeverityWarn)

	more := f.drift("i1", "firing", SeverityWarn, "h2", nil)
	f.d.materializeEventJobs()
	f.expect(more, database.AlertEventAcknowledged, "ack:"+a.ID, false)

	res := f.drift("i1", "resolved", SeverityWarn, "h0", nil)
	f.d.materializeEventJobs()
	f.expect(res, "processed", "", true)
	if r := f.ackState(a.ID); r != "resolved" {
		t.Fatalf("ack cleared as %q, want resolved", r)
	}

	f.drift("i2", "firing", SeverityWarn, "h3", nil)
	f.d.materializeEventJobs()
	b := f.ack(key, "i2", SeverityWarn)
	esc := f.drift("i2", "firing", SeverityError, "h4", map[string]interface{}{"escalated": true})
	f.d.materializeEventJobs()
	f.expect(esc, "processed", "", true)
	if r := f.ackState(b.ID); r != "escalated" {
		t.Fatalf("ack cleared as %q, want escalated", r)
	}
}
