// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// sickCluster answers the harvester for a three-node cluster "prod". While
// healthy is false, ch-3 does not answer, ch-1 has two read-only replicas
// and ch-2 has a mutation pending for 8000 s.
func sickCluster(healthy *atomic.Bool) func(tunnel.GatewayMessage) *tunnel.AgentMessage {
	return func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		ok := healthy.Load()
		var data string
		switch q := msg.SQL; {
		case strings.Contains(q, "HAVING countIf(is_local)"):
			data = `[{"cluster":"prod","nodes":3}]`
		case strings.Contains(q, "WHERE cluster = 'prod'"):
			data = `[{"host_name":"ch-1","is_local":1,"self":"ch-1"},{"host_name":"ch-2","is_local":0,"self":"ch-1"},{"host_name":"ch-3","is_local":0,"self":"ch-1"}]`
		case strings.Contains(q, "system.one"):
			data = `[{"node":"ch-1"},{"node":"ch-2"}]`
			if ok {
				data = `[{"node":"ch-1"},{"node":"ch-2"},{"node":"ch-3"}]`
			}
		case strings.Contains(q, "replicas_readonly"):
			data = `[{"node":"ch-1","replication_max_delay":0,"replication_queue_total":"0","replicas_readonly":"2"}]`
			if ok {
				data = `[{"node":"ch-1","replication_max_delay":0,"replication_queue_total":"0","replicas_readonly":"0"}]`
			}
		case strings.Contains(q, "is_session_expired"):
			data = `[{"node":"ch-1","database":"db","table":"events","is_readonly":1,"absolute_delay":0}]`
		case strings.Contains(q, "mutations_pending"):
			data = `[{"node":"ch-2","mutations_pending":"1","mutations_oldest_age_seconds":"8000"}]`
			if ok {
				data = `[{"node":"ch-2","mutations_pending":"0","mutations_oldest_age_seconds":"0"}]`
			}
		case strings.Contains(q, "latest_fail_reason"):
			data = `[{"node":"ch-2","database":"db","table":"events","mutation_id":"0000000001","command":"DELETE WHERE 1"}]`
		default:
			data = `[]`
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	}
}

func setHarvesterAccount(t *testing.T, db *database.DB, conn string) {
	t.Helper()
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "cluster_health", Mode: "service_account", Username: "worker", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
}

func healthEvents(t *testing.T, db *database.DB) []database.AlertEvent {
	t.Helper()
	events, err := db.ListAlertEvents(100, "", "")
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestHealthAlertsFireAndResolve(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var healthy atomic.Bool
	agent := testutil.NewAgent(t, db, "worker-token", sickCluster(&healthy))
	setHarvesterAccount(t, db, conn)
	h := NewHarvester(NewStore(db), db, agent.Gateway, "test-secret", func() bool { return true })
	poll := func() { h.pollConnection(conn, DefaultSettings(conn), time.Now()) }

	poll()
	if n := len(healthEvents(t, db)); n != 0 {
		t.Fatalf("first breach must only be pending, got %d events", n)
	}
	states, err := db.ListHealthAlertStates(conn, true)
	if err != nil || len(states) != 3 {
		t.Fatalf("want 3 pending states, got %+v %v", states, err)
	}
	for _, s := range states {
		if s.State != alerts.StatePending {
			t.Fatalf("state %+v, want pending", s)
		}
	}

	poll()
	events := healthEvents(t, db)
	want := map[string]string{
		alerts.EventTypeHealthNodeUnreachable: HealthFingerprint(conn, CheckNodeUnreachable, "ch-3", "critical"),
		alerts.EventTypeHealthReplicaReadonly: HealthFingerprint(conn, CheckReplicaReadonly, "ch-1", "critical"),
		alerts.EventTypeHealthMutationStuck:   HealthFingerprint(conn, CheckMutationStuck, "ch-2", "critical"),
	}
	if len(events) != len(want) {
		t.Fatalf("want %d firing events, got %d: %+v", len(want), len(events), events)
	}
	for _, ev := range events {
		fp, ok := want[ev.EventType]
		if !ok || ev.Fingerprint == nil || *ev.Fingerprint != fp || ev.Severity != alerts.SeverityCritical {
			t.Fatalf("unexpected event %s fp=%v sev=%s", ev.EventType, ev.Fingerprint, ev.Severity)
		}
		var payload map[string]interface{}
		if ev.PayloadJSON == nil || json.Unmarshal([]byte(*ev.PayloadJSON), &payload) != nil || payload["status"] != "firing" {
			t.Fatalf("payload of %s: %v", ev.EventType, ev.PayloadJSON)
		}
		if ev.EventType != alerts.EventTypeHealthNodeUnreachable {
			top, _ := payload["top_offenders"].([]interface{})
			if len(top) == 0 {
				t.Fatalf("%s: no top offenders in %s", ev.EventType, *ev.PayloadJSON)
			}
		}
	}
	states, _ = db.ListHealthAlertStates(conn, true)
	for _, s := range states {
		if s.State != alerts.StateFiring || s.LastEventID == nil {
			t.Fatalf("state %+v, want firing with an event id", s)
		}
	}

	// Still firing: no repeat events.
	poll()
	if n := len(healthEvents(t, db)); n != 3 {
		t.Fatalf("a held condition raised more events: %d", n)
	}

	healthy.Store(true)
	poll()
	if n := len(healthEvents(t, db)); n != 3 {
		t.Fatalf("one OK must not resolve yet, got %d events", n)
	}
	poll()
	events = healthEvents(t, db)
	resolved := 0
	for _, ev := range events {
		if !strings.HasPrefix(ev.Title, "[RESOLVED] ") {
			continue
		}
		resolved++
		if ev.Severity != alerts.SeverityCritical || ev.Fingerprint == nil || !strings.HasSuffix(*ev.Fingerprint, ":resolved") {
			t.Fatalf("resolved event %+v", ev)
		}
		if ev.PayloadJSON == nil || !strings.Contains(*ev.PayloadJSON, `"status":"resolved"`) {
			t.Fatalf("resolved payload %v", ev.PayloadJSON)
		}
	}
	if resolved != 3 {
		t.Fatalf("want 3 resolved events, got %d of %d", resolved, len(events))
	}
	if states, _ := db.ListHealthAlertStates(conn, true); len(states) != 0 {
		t.Fatalf("all states must be ok, got %+v", states)
	}
	evals, err := db.ListAlertEvaluations(conn, "health", CheckReplicaReadonly, 50)
	if err != nil || len(evals) < 3 {
		t.Fatalf("evaluation history: %+v %v", evals, err)
	}
}

// evalHarvester evaluates hand-made samples without a gateway.
func evalHarvester(t *testing.T) (*Harvester, *database.DB, string) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	return NewHarvester(NewStore(db), db, nil, "test-secret", nil), db, conn
}

func noExec(string) ([]map[string]interface{}, error) { return nil, fmt.Errorf("no detail") }

func TestHealthAlertsUnknownFreezesState(t *testing.T) {
	h, db, conn := evalHarvester(t)
	now := time.Now()
	bad := []Sample{{Node: "ch-1", ReplicasReadonly: 3}}
	h.evaluateHealthAlerts(conn, "", bad, noExec, now)
	h.evaluateHealthAlerts(conn, "", bad, noExec, now)
	unknown := []Sample{{Node: "ch-1", Unknown: []string{MetricReplication}}}
	for i := 0; i < 3; i++ {
		h.evaluateHealthAlerts(conn, "", unknown, noExec, now)
	}
	// A node missing from the poll (local-only fallback) is frozen too.
	for i := 0; i < 3; i++ {
		h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-2"}}, noExec, now)
	}
	states, err := db.ListHealthAlertStates(conn, true)
	if err != nil || len(states) != 1 || states[0].State != alerts.StateFiring || states[0].OkCount != 0 {
		t.Fatalf("unknown must keep ch-1 firing: %+v %v", states, err)
	}
	if n := len(healthEvents(t, db)); n != 1 {
		t.Fatalf("want only the firing event, got %d", n)
	}
}

func TestHealthAlertsEscalateAndCap(t *testing.T) {
	h, db, conn := evalHarvester(t)
	one := 1
	settings := []database.HealthAlertSetting{
		{CheckKind: CheckReplicationLag, Enabled: true, WarnThreshold: f64(60), CritThreshold: f64(300), ForEvaluations: one, ResolveEvaluations: one},
		{CheckKind: CheckReplicaReadonly, Enabled: true, CritThreshold: f64(0), ForEvaluations: one, ResolveEvaluations: one},
	}
	if err := db.SaveHealthAlertSettings(conn, settings); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1", ReplicationMaxDelay: 120}}, noExec, now)
	h.evaluateHealthAlerts(conn, "", []Sample{{Node: "ch-1", ReplicationMaxDelay: 900}}, noExec, now)
	events := healthEvents(t, db)
	if len(events) != 2 {
		t.Fatalf("want warn firing and critical escalation, got %d", len(events))
	}
	fps := map[string]bool{}
	for _, ev := range events {
		fps[*ev.Fingerprint] = true
	}
	if !fps[HealthFingerprint(conn, CheckReplicationLag, "ch-1", "warn")] || !fps[HealthFingerprint(conn, CheckReplicationLag, "ch-1", "critical")] {
		t.Fatalf("fingerprints %v", fps)
	}

	// 25 nodes with read-only replicas fire at once: 20 events, 1 summary.
	var many []Sample
	for i := 0; i < 25; i++ {
		many = append(many, Sample{Node: fmt.Sprintf("n-%02d", i), ReplicasReadonly: 1})
	}
	h.evaluateHealthAlerts(conn, "", many, noExec, now)
	events, _ = db.ListAlertEvents(100, alerts.EventTypeHealthReplicaReadonly, "")
	if len(events) != maxHealthEventsPerPoll {
		t.Fatalf("want %d capped events, got %d", maxHealthEventsPerPoll, len(events))
	}
	summary, _ := db.ListAlertEvents(100, alerts.EventTypeHealthSummary, "")
	if len(summary) != 1 || !strings.Contains(summary[0].Title, "5 more") {
		t.Fatalf("want one summary of 5, got %+v", summary)
	}
}

func TestValidateHealthSetting(t *testing.T) {
	good := HealthCheckSetting{CheckKind: CheckDisk, Enabled: true, WarnThreshold: f64(70), CritThreshold: f64(95), ForEvaluations: 2, ResolveEvaluations: 3}
	if _, err := ValidateHealthSetting(good); err != nil {
		t.Fatal(err)
	}
	bad := []HealthCheckSetting{
		{CheckKind: "cpu", Enabled: true, CritThreshold: f64(1), ForEvaluations: 1, ResolveEvaluations: 1},
		{CheckKind: CheckDisk, Enabled: true, WarnThreshold: f64(95), CritThreshold: f64(90), ForEvaluations: 1, ResolveEvaluations: 1},
		{CheckKind: CheckDisk, Enabled: true, CritThreshold: f64(120), ForEvaluations: 1, ResolveEvaluations: 1},
		{CheckKind: CheckParts, Enabled: true, CritThreshold: f64(80), ForEvaluations: 0, ResolveEvaluations: 1},
		{CheckKind: CheckReplicationLag, Enabled: true, WarnThreshold: f64(-1), ForEvaluations: 1, ResolveEvaluations: 1},
		{CheckKind: CheckMutationStuck, Enabled: true, ForEvaluations: 1, ResolveEvaluations: 1},
	}
	for i, s := range bad {
		if _, err := ValidateHealthSetting(s); err == nil {
			t.Errorf("case %d accepted: %+v", i, s)
		}
	}
	// Fixed fields are normalized, not rejected.
	s, err := ValidateHealthSetting(HealthCheckSetting{CheckKind: CheckNodeUnreachable, Enabled: true, WarnThreshold: f64(5), CritThreshold: f64(9), ForEvaluations: 1, ResolveEvaluations: 1})
	if err != nil || s.WarnThreshold != nil || s.CritThreshold == nil || *s.CritThreshold != 0 {
		t.Fatalf("node_unreachable normalize: %+v %v", s, err)
	}
}

func TestDiskFreeBytesFloor(t *testing.T) {
	set := HealthCheckSetting{CheckKind: CheckDisk, WarnThreshold: f64(80), CritThreshold: f64(90), FreeBytesFloor: f64(10 << 30)}
	free := float64(5 << 30)
	used := 40.0
	s := Sample{DiskUsedPct: &used, DiskFreeMinBytes: &free}
	if got := breachSeverity(set, used, s); got != alerts.SeverityCritical {
		t.Fatalf("free floor breach = %q", got)
	}
	free = float64(50 << 30)
	if got := breachSeverity(set, used, s); got != "" {
		t.Fatalf("healthy disk = %q", got)
	}
}
