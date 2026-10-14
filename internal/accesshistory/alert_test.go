// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package accesshistory

import (
	"encoding/json"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/rbac"
)

func driftEvents(t *testing.T, db *database.DB) []database.AlertEvent {
	t.Helper()
	evs, err := db.ListAlertEvents(100, alerts.EventTypeAccessDrift, "")
	if err != nil {
		t.Fatal(err)
	}
	// Newest first from the API; tests read them oldest first.
	for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
		evs[i], evs[j] = evs[j], evs[i]
	}
	return evs
}

func eventPayload(t *testing.T, ev database.AlertEvent) map[string]any {
	t.Helper()
	var p map[string]any
	if ev.PayloadJSON == nil || json.Unmarshal([]byte(*ev.PayloadJSON), &p) != nil {
		t.Fatalf("payload of %s: %v", ev.ID, ev.PayloadJSON)
	}
	return p
}

func TestDriftAlerts(t *testing.T) {
	db, err := dbtest.Open(t, "drift-alerts.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: "prod-token"})
	if err != nil {
		t.Fatal(err)
	}
	a := rbac.Entity{Name: "a", Storage: "replicated"}
	b := rbac.Entity{Name: "b", Storage: "replicated"}
	as := func(user string, s *rbac.Snapshot) Capture {
		return Capture{ConnectionID: conn, Source: SourceSync, Actor: "governance sync", CHUser: user, Snapshot: s}
	}
	expect := func(n int) []database.AlertEvent {
		t.Helper()
		evs := driftEvents(t, db)
		if len(evs) != n {
			t.Fatalf("want %d drift events, got %d: %+v", n, len(evs), evs)
		}
		return evs
	}

	// First rows and a dedup bump: no event.
	base, _ := record(t, db, as("gov", snap("", a)))
	record(t, db, as("other", snap("", a)))
	if _, created, err := Record(db, as("gov", snap("", a))); err != nil || created {
		t.Fatalf("bump: created=%v err=%v", created, err)
	}
	expect(0)

	// An outside change fires once.
	drifted, sum := record(t, db, as("gov", snap("", a, b)))
	if drifted.Drift != DriftOutside {
		t.Fatalf("drift: %+v", drifted)
	}
	ev := expect(1)[0]
	p := eventPayload(t, ev)
	if ev.Severity != alerts.SeverityWarn || ev.Fingerprint == nil || *ev.Fingerprint != "access:"+conn+":drift:"+drifted.Hash ||
		ev.SourceRef == nil || *ev.SourceRef != DriftSourceRef || ev.ConnectionID == nil || *ev.ConnectionID != conn {
		t.Fatalf("firing event: %+v", ev)
	}
	if p["status"] != "firing" || p["alert_key"] != DriftAlertKey(conn) || p["incident_id"] != drifted.FirstSeenAt ||
		p["baseline_hash"] != base.Hash || p["connection_name"] != "prod" || p["source"] != SourceSync ||
		p["ch_user"] != "gov" || p["actor"] != "governance sync" || p["added"] != float64(1) || p["link_path"] != DriftLinkPath {
		t.Fatalf("firing payload: %+v", p)
	}
	if ents, _ := p["entities"].([]any); len(ents) != len(sum.Entities) || len(ents) == 0 || ents[0] != sum.Entities[0] {
		t.Fatalf("entities: %+v vs %+v", p["entities"], sum.Entities)
	}
	if id := alerts.IdentifyEvent(ev); id.Kind != "firing" || id.IncidentID != drifted.FirstSeenAt {
		t.Fatalf("identity: %+v", id)
	}

	// Another ClickHouse user drifting to the same state does not re-fire.
	if row, _ := record(t, db, as("other", snap("", a, b))); row.Drift != DriftOutside {
		t.Fatalf("other user drift: %+v", row)
	}
	expect(1)

	// An apply with an unexplained change escalates the same incident.
	pre := snap("", a, b)
	ops := []rbac.Op{{Kind: rbac.OpCreateRole, Name: "via_ui"}}
	post := rbac.Simulate(pre, ops)
	post.Roles = append(post.Roles, rbac.Entity{Name: "sneaky", Storage: "replicated"})
	mixed, _ := record(t, db, Capture{ConnectionID: conn, Source: SourceApply, Actor: "alice", CHUser: "admin", Snapshot: post, Pre: pre, Applied: ops})
	if mixed.Drift != DriftMixed {
		t.Fatalf("mixed: %+v", mixed)
	}
	ev = expect(2)[1]
	p = eventPayload(t, ev)
	if ev.Severity != alerts.SeverityError || p["incident_id"] != drifted.FirstSeenAt || p["escalated"] != true ||
		p["baseline_hash"] != base.Hash || p["source"] != SourceApply || p["actor"] != "alice" {
		t.Fatalf("mixed event: %+v %+v", ev, p)
	}
	if ents, _ := p["entities"].([]any); len(ents) != 1 {
		t.Fatalf("mixed entities should be the unexplained ones: %+v", p["entities"])
	}

	// Back at the state before the drift: the incident resolves.
	record(t, db, as("gov", snap("", a)))
	ev = expect(3)[2]
	p = eventPayload(t, ev)
	if ev.Severity != alerts.SeverityError || p["status"] != "resolved" || p["incident_id"] != drifted.FirstSeenAt ||
		*ev.Fingerprint != "access:"+conn+":resolved:"+drifted.FirstSeenAt {
		t.Fatalf("resolved event: %+v %+v", ev, p)
	}

	// A change made through CH-UI is no drift and raises nothing.
	pre = snap("", a)
	ops = []rbac.Op{{Kind: rbac.OpCreateRole, Name: "clean"}}
	if row, _ := record(t, db, Capture{ConnectionID: conn, Source: SourceApply, CHUser: "admin", Snapshot: rbac.Simulate(pre, ops), Pre: pre, Applied: ops}); row.Drift != DriftNone {
		t.Fatalf("clean apply: %+v", row)
	}
	expect(3)
}

func TestDriftAlertFailureKeepsRecording(t *testing.T) {
	db, err := dbtest.Open(t, "drift-alert-fail.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "f", TunnelToken: "f-token"})
	if err != nil {
		t.Fatal(err)
	}
	gov := func(s *rbac.Snapshot) Capture {
		return Capture{ConnectionID: conn, Source: SourceManual, CHUser: "gov", Snapshot: s}
	}
	record(t, db, gov(snap("", rbac.Entity{Name: "a"})))
	if _, err := db.Conn().Exec(`DROP TABLE alert_events`); err != nil {
		t.Fatal(err)
	}
	row, created, err := Record(db, gov(snap("", rbac.Entity{Name: "a"}, rbac.Entity{Name: "b"})))
	if err != nil || !created || row.Drift != DriftOutside {
		t.Fatalf("recording must not fail with alerts broken: row=%+v created=%v err=%v", row, created, err)
	}
}
