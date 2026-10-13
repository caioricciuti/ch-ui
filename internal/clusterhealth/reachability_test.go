// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// pollAgain lets the next tick poll every connection again.
func pollAgain(h *Harvester) {
	h.mu.Lock()
	h.lastPoll = map[string]time.Time{}
	h.mu.Unlock()
}

func offlineHarvester(t *testing.T, db *database.DB) *Harvester {
	t.Helper()
	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	h := NewHarvester(NewStore(db), db, gw, "test-secret", func() bool { return true })
	h.startedAt = time.Now().Add(-10 * time.Minute)
	return h
}

func connectionEvents(t *testing.T, db *database.DB) []database.AlertEvent {
	t.Helper()
	return eventsOf(t, db, alerts.EventTypeHealthConnectionUnreachable)
}

// An offline tunnel breaches on every due poll: the check fires after its
// default 3 evaluations, and two OK probes resolve it.
func TestConnectionUnreachableFiresAndResolves(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	off := offlineHarvester(t, db)
	for i := 0; i < 2; i++ {
		off.tick()
		pollAgain(off)
	}
	if st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode); st == nil || st.State != alerts.StatePending || st.BreachCount != 2 {
		t.Fatalf("want pending after 2 offline polls, got %+v", st)
	}
	if n := len(connectionEvents(t, db)); n != 0 {
		t.Fatalf("fired before 3 breaches: %d events", n)
	}
	// Not due yet: the poll interval holds.
	off.tick()
	off.tick()
	if st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode); st.BreachCount != 0 || st.State != alerts.StateFiring {
		t.Fatalf("third poll must fire once: %+v", st)
	}
	evs := connectionEvents(t, db)
	if len(evs) != 1 || evs[0].Severity != alerts.SeverityCritical || evs[0].Title != "Connection unreachable" {
		t.Fatalf("firing event: %+v", evs)
	}
	var payload map[string]interface{}
	_ = json.Unmarshal([]byte(*evs[0].PayloadJSON), &payload)
	if payload["alert_key"] != HealthAlertKey(conn, CheckConnectionUnreachable, ConnectionNode) || payload["incident_id"] == "" ||
		payload["node"] != ConnectionNode || !strings.Contains(evs[0].Message, "tunnel offline") {
		t.Fatalf("payload %v message %q", payload, evs[0].Message)
	}
	if !strings.Contains(*evs[0].Fingerprint, ":critical:") {
		t.Fatalf("fingerprint %s", *evs[0].Fingerprint)
	}

	// Back online with sampling off: SELECT 1 answers, two OKs resolve.
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"1":1}]`)}
	})
	setHarvesterAccount(t, db, conn)
	store := NewStore(db)
	off2 := DefaultSettings(conn)
	off2.Enabled = false
	if _, err := store.UpsertSettings(off2); err != nil {
		t.Fatal(err)
	}
	on := NewHarvester(store, db, agent.Gateway, "test-secret", func() bool { return true })
	on.tick()
	pollAgain(on)
	on.tick()
	evs = connectionEvents(t, db)
	if len(evs) != 2 || evs[0].Title != "[RESOLVED] Connection unreachable" {
		t.Fatalf("want a resolve, got %+v", evs)
	}
	if st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode); st.State != alerts.StateOK || st.Severity != nil {
		t.Fatalf("state after resolve: %+v", st)
	}
	sqls := agent.Messages()
	if len(sqls) != 2 || strings.TrimSpace(sqls[0].SQL) != "SELECT 1" {
		t.Fatalf("sampling off must only probe: %+v", sqls)
	}
}

// Without credentials the probe cannot run: the state is frozen and the
// reason is recorded as an error evaluation.
func TestConnectionUnreachableNoCredentialsFreezes(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	h := NewHarvester(NewStore(db), db, agent.Gateway, "test-secret", func() bool { return true })
	now := time.Now()
	for i := 0; i < 3; i++ {
		h.evaluateReachability(conn, reachability{breach: true, detail: "tunnel offline"}, now)
	}
	before := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode)
	if before == nil || before.State != alerts.StateFiring {
		t.Fatalf("setup: %+v", before)
	}
	for i := 0; i < 3; i++ {
		h.tick()
		pollAgain(h)
	}
	after := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode)
	if after.State != alerts.StateFiring || after.OkCount != 0 || after.BreachCount != before.BreachCount {
		t.Fatalf("no credentials must freeze the state: %+v", after)
	}
	if n := len(connectionEvents(t, db)); n != 1 {
		t.Fatalf("frozen state raised events: %d", n)
	}
	evals, err := db.ListAlertEvaluations(conn, "health", CheckConnectionUnreachable, 50)
	if err != nil {
		t.Fatal(err)
	}
	errors := 0
	for _, e := range evals {
		if strings.HasPrefix(e.Error, "no credentials") {
			errors++
		}
	}
	if errors != 3 {
		t.Fatalf("want 3 error rows, got %d of %+v", errors, evals)
	}
	if len(agent.Messages()) != 0 {
		t.Fatalf("no query may run without credentials: %+v", agent.Messages())
	}
}

// Zero samples: SELECT 1 decides. A failing probe is a breach.
func TestConnectionUnreachableZeroSamplesProbes(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 210. Connection refused"}
	})
	setHarvesterAccount(t, db, conn)
	h := NewHarvester(NewStore(db), db, agent.Gateway, "test-secret", func() bool { return true })
	h.pollConnection(conn, DefaultSettings(conn), time.Now())
	st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode)
	if st == nil || st.State != alerts.StatePending || st.BreachCount != 1 {
		t.Fatalf("a failed SELECT 1 must breach: %+v", st)
	}
	evals, _ := db.ListAlertEvaluations(conn, "health", CheckConnectionUnreachable, 5)
	if len(evals) == 0 || !strings.Contains(evals[0].Error, "SELECT 1 failed") {
		t.Fatalf("breach reason not recorded: %+v", evals)
	}
}

// Right after a start an offline tunnel is not counted: agents reconnect.
func TestConnectionUnreachableStartupGrace(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	h := offlineHarvester(t, db)
	h.startedAt = time.Now()
	for i := 0; i < 5; i++ {
		h.tick()
		pollAgain(h)
	}
	if st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode); st != nil {
		t.Fatalf("offline polls counted inside the grace window: %+v", st)
	}
	h.startedAt = time.Now().Add(-reachabilityGrace)
	h.tick()
	if st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode); st == nil || st.BreachCount != 1 {
		t.Fatalf("after the grace window an offline poll counts: %+v", st)
	}
}

// Turning the check off stops the probe; its incident is closed like any
// other check's.
func TestConnectionUnreachableDisabled(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	h := offlineHarvester(t, db)
	if err := db.SaveHealthAlertSettings(conn, []database.HealthAlertSetting{
		{CheckKind: CheckConnectionUnreachable, Enabled: false, CritThreshold: f64(0), ForEvaluations: 1, ResolveEvaluations: 1},
	}); err != nil {
		t.Fatal(err)
	}
	h.tick()
	if st := stateOf(t, db, conn, CheckConnectionUnreachable, ConnectionNode); st != nil {
		t.Fatalf("a disabled check must not run: %+v", st)
	}
	set, _ := connectionCheckSetting(db, conn)
	if set.CritThreshold == nil || *set.CritThreshold != 0 {
		t.Fatalf("crit must be pinned to 0: %+v", set)
	}
}
