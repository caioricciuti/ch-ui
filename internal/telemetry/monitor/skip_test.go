// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package monitor

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// switchExecutor answers every monitor query with a count of 0 while
// online.
type switchExecutor struct {
	online  atomic.Bool
	queries atomic.Int32
}

func (e *switchExecutor) ExecuteQuery(_, sql, _, _ string, _ time.Duration) (*tunnel.QueryResult, error) {
	e.queries.Add(1)
	if strings.HasPrefix(sql, "DESCRIBE") {
		return &tunnel.QueryResult{Data: json.RawMessage(`[{"name":"Timestamp","type":"DateTime64(9)"},{"name":"Body","type":"String"}]`)}, nil
	}
	return &tunnel.QueryResult{Data: json.RawMessage(`[{"c":0}]`)}, nil
}

func (e *switchExecutor) IsTunnelOnline(string) bool { return e.online.Load() }

func skipMonitor(t *testing.T) (*database.DB, string, string) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	mapping := telemetry.DefaultLogsMapping()
	source, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Name: "logs", Kind: telemetry.KindLogs, Database: "default", Table: "otel_logs", Logs: &mapping, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateTelemetryMonitor(&database.TelemetryMonitor{ConnectionID: conn, Name: "m", Kind: "logs", SourceID: source, WindowSeconds: 60, IntervalSeconds: 30, Comparator: "gt", Threshold: 10, Severity: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	return db, conn, id
}

func monitorEvents(t *testing.T, db *database.DB) int {
	t.Helper()
	evs, err := db.ListAlertEvents(100, eventTypeTelemetryMonitor, "")
	if err != nil {
		t.Fatal(err)
	}
	return len(evs)
}

// An offline connection is skipped: no query, no error state, no event.
func TestTickSkipsOfflineConnections(t *testing.T) {
	db, conn, id := skipMonitor(t)
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "telemetry.monitor", Mode: "service_account", Username: "worker", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	exec := &switchExecutor{}
	r := &MonitorRunner{db: db, gateway: exec, secret: "test-secret", pro: func() bool { return true }}
	r.tick()
	m, err := db.GetTelemetryMonitor(id)
	if err != nil || m.LastRunAt != nil || m.LastState == "error" || m.LastError != nil {
		t.Fatalf("an offline connection must leave the monitor as it was: %#v %v", m, err)
	}
	if n := exec.queries.Load(); n != 0 {
		t.Fatalf("offline: %d queries sent", n)
	}
	if n := monitorEvents(t, db); n != 0 {
		t.Fatalf("offline: %d alert events", n)
	}

	exec.online.Store(true)
	r.tick()
	if m, err = db.GetTelemetryMonitor(id); err != nil || m.LastState != "ok" || m.LastRunAt == nil {
		t.Fatalf("back online the monitor runs: %#v %v", m, err)
	}
}

// Without an account the monitor shows why it does not run, raises no
// event and stays due; with one it runs and the reason clears.
func TestTickShowsMissingAccount(t *testing.T) {
	db, conn, id := skipMonitor(t)
	exec := &switchExecutor{}
	exec.online.Store(true)
	r := &MonitorRunner{db: db, gateway: exec, secret: "test-secret", pro: func() bool { return true }}
	for i := 0; i < 2; i++ {
		r.tick()
	}
	m, err := db.GetTelemetryMonitor(id)
	if err != nil || m.LastState != "error" || m.LastError == nil || !strings.HasPrefix(*m.LastError, "Not evaluated: ") || m.LastRunAt != nil {
		t.Fatalf("missing account: %#v %v", m, err)
	}
	if n := exec.queries.Load(); n != 0 {
		t.Fatalf("missing account: %d queries sent", n)
	}
	if n := monitorEvents(t, db); n != 0 {
		t.Fatalf("missing account raised %d alert events", n)
	}

	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "telemetry.monitor", Mode: "service_account", Username: "worker", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	r.tick()
	if m, err = db.GetTelemetryMonitor(id); err != nil || m.LastState != "ok" || m.LastError != nil || m.LastRunAt == nil {
		t.Fatalf("with an account the monitor runs: %#v %v", m, err)
	}
	if n := monitorEvents(t, db); n != 0 {
		t.Fatalf("recovery raised %d alert events", n)
	}
}
