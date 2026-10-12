// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package monitor

import (
	"encoding/json"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// TestMonitorEventsAcrossErrors walks one monitor through ok, firing,
// error, firing, error and checks the alert events: one firing event for
// the whole incident, one error event per transition into error.
func TestMonitorEventsAcrossErrors(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var mu sync.Mutex
	count, fail := `0`, false
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		mu.Lock()
		defer mu.Unlock()
		if strings.HasPrefix(msg.SQL, "DESCRIBE") {
			return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"name":"Timestamp","type":"DateTime64(9)"},{"name":"Body","type":"String"}]`)}
		}
		if fail {
			return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 60. Table default.otel_logs does not exist"}
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"c":` + count + `}]`)}
	})
	mapping := telemetry.DefaultLogsMapping()
	source, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Name: "logs", Kind: telemetry.KindLogs, Database: "default", Table: "otel_logs", Logs: &mapping, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateTelemetryMonitor(&database.TelemetryMonitor{ConnectionID: conn, Name: "errors", Kind: "logs", SourceID: source, WindowSeconds: 60, IntervalSeconds: 30, Comparator: "gt", Threshold: 10, Severity: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	r := NewMonitorRunner(db, agent.Gateway, "test-secret", nil)
	step := func(c string, failing bool) {
		t.Helper()
		mu.Lock()
		count, fail = c, failing
		mu.Unlock()
		m, err := db.GetTelemetryMonitor(id)
		if err != nil || m == nil {
			t.Fatal(err)
		}
		_, _, _ = r.Evaluate(m, Credentials{User: "u", Password: "p"})
	}
	events := func() (firing, errs int) {
		t.Helper()
		list, err := db.ListAlertEvents(100, eventTypeTelemetryMonitor, "")
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range list {
			if strings.HasSuffix(e.Title, "evaluation failed") {
				errs++
			} else {
				firing++
			}
		}
		return
	}
	step("1", false)  // ok
	step("50", false) // firing: event
	step("0", true)   // error: event
	step("0", true)   // still error: quiet
	step("50", false) // firing again, same incident: quiet
	if f, e := events(); f != 1 || e != 1 {
		t.Fatalf("after firing, error, firing: %d firing and %d error events, want 1 and 1", f, e)
	}
	step("1", false)  // ok
	step("50", false) // a new incident: event
	if f, _ := events(); f != 2 {
		t.Fatalf("a new incident must emit: %d firing events", f)
	}
	m, _ := db.GetTelemetryMonitor(id)
	if m.LastState != "firing" {
		t.Fatalf("state %q", m.LastState)
	}
}

// slowExecutor answers like ClickHouse after a delay and records the peak
// number of queries in flight.
type slowExecutor struct {
	inFlight, peak int32
}

func (e *slowExecutor) ExecuteQuery(_, sql, _, _ string, _ time.Duration) (*tunnel.QueryResult, error) {
	if strings.HasPrefix(sql, "DESCRIBE") {
		return &tunnel.QueryResult{Data: json.RawMessage(`[{"name":"Timestamp","type":"DateTime64(9)"},{"name":"Body","type":"String"}]`)}, nil
	}
	n := atomic.AddInt32(&e.inFlight, 1)
	for {
		p := atomic.LoadInt32(&e.peak)
		if n <= p || atomic.CompareAndSwapInt32(&e.peak, p, n) {
			break
		}
	}
	time.Sleep(100 * time.Millisecond)
	atomic.AddInt32(&e.inFlight, -1)
	return &tunnel.QueryResult{Data: json.RawMessage(`[{"c":0}]`)}, nil
}

// TestTickEvaluatesConcurrently checks the worker pool: with several slow
// monitors due, a tick runs more than one query at a time, never more
// than monitorWorkers, and still evaluates them all.
func TestTickEvaluatesConcurrently(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "telemetry.monitor", Mode: "service_account", Username: "worker", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	mapping := telemetry.DefaultLogsMapping()
	source, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Name: "logs", Kind: telemetry.KindLogs, Database: "default", Table: "otel_logs", Logs: &mapping, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for i := 0; i < 8; i++ {
		id, err := db.CreateTelemetryMonitor(&database.TelemetryMonitor{ConnectionID: conn, Name: "m", Kind: "logs", SourceID: source, WindowSeconds: 60, IntervalSeconds: 30, Comparator: "gt", Threshold: 10, Severity: "warn", Enabled: true})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	exec := &slowExecutor{}
	gate := config.NewProGate("Telemetry monitors", func() config.ProAccess { return config.ProActive })
	r := &MonitorRunner{db: db, gateway: exec, secret: "test-secret", pro: gate.Allow}
	r.tick()
	if p := atomic.LoadInt32(&exec.peak); p < 2 || p > monitorWorkers {
		t.Fatalf("peak concurrency %d, want 2..%d", p, monitorWorkers)
	}
	for _, id := range ids {
		m, err := db.GetTelemetryMonitor(id)
		if err != nil || m == nil || m.LastState != "ok" {
			t.Fatalf("monitor %s not evaluated: %#v %v", id, m, err)
		}
	}
}
