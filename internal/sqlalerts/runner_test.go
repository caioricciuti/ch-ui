// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package sqlalerts

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func setAccount(t *testing.T, db *database.DB, conn, mode string) {
	t.Helper()
	c := database.BackgroundCredential{Worker: Worker, Mode: mode}
	if mode == "service_account" {
		enc, err := crypto.Encrypt("pw", "test-secret")
		if err != nil {
			t.Fatal(err)
		}
		c.Username, c.EncryptedPassword = "worker", enc
	}
	if err := db.SetBackgroundCredential(conn, c); err != nil {
		t.Fatal(err)
	}
}

func createAlert(t *testing.T, db *database.DB, a database.SQLAlert) string {
	t.Helper()
	if err := ValidateAlert(&a); err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSQLAlert(&a)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func makeDue(t *testing.T, db *database.DB, id string) {
	t.Helper()
	if _, err := db.Conn().Exec("UPDATE sql_alerts SET last_run_at = NULL WHERE id = ?", id); err != nil {
		t.Fatal(err)
	}
}

func valueAgent(t *testing.T, db *database.DB, value *atomic.Int64, fail *atomic.Bool) *testutil.Agent {
	return testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if fail != nil && fail.Load() {
			return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 60. Unknown table"}
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(fmt.Sprintf(`[{"v":"%d"}]`, value.Load()))}
	})
}

// TestTickFollowsLicense checks that enabled alerts are not evaluated while
// there is no Pro license, are evaluated during grace and while active, and
// stay enabled throughout.
func TestTickFollowsLicense(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	agent := valueAgent(t, db, &v, nil)
	setAccount(t, db, conn, "service_account")
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "a", SQL: "SELECT 1 AS v", Comparator: "gt", Threshold: 10, Enabled: true})

	access := config.ProNone
	gate := config.NewProGate("SQL alerts", func() config.ProAccess { return access })
	r := NewRunner(db, agent.Gateway, nil, "test-secret", gate.Allow)

	r.tick()
	if n := len(agent.Messages()); n != 0 {
		t.Fatalf("ProNone: %d queries sent, want 0", n)
	}
	sent := 0
	for _, state := range []config.ProAccess{config.ProGrace, config.ProActive} {
		access = state
		makeDue(t, db, id)
		r.tick()
		n := len(agent.Messages())
		if n <= sent {
			t.Fatalf("access %v: no queries sent", state)
		}
		sent = n
		a, err := db.GetSQLAlert(id)
		if err != nil || a == nil || a.LastRunAt == nil || !a.Enabled || a.State != alerts.StateOK {
			t.Fatalf("access %v: alert not evaluated: %+v %v", state, a, err)
		}
	}
	access = config.ProNone
	makeDue(t, db, id)
	r.tick()
	if n := len(agent.Messages()); n != sent {
		t.Fatalf("back to ProNone: %d queries sent, want %d", n, sent)
	}
}

// TestSessionModeDoesNotRun checks that SQL alerts never borrow a session:
// without a dedicated account the tick sends nothing.
func TestSessionModeDoesNotRun(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	agent := valueAgent(t, db, &v, nil)
	createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "a", SQL: "SELECT 1 AS v", Comparator: "gt", Threshold: 10, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", func() bool { return true })
	r.tick()
	if n := len(agent.Messages()); n != 0 {
		t.Fatalf("session mode sent %d queries", n)
	}
}

func TestBackgroundAccountExecution(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"v":"1"}]`)}
	})
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "a", SQL: "SELECT 1 AS v", Comparator: "gt", Threshold: 10, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", func() bool { return true })
	agent.CheckWorker(t, db, conn, Worker, func() {
		makeDue(t, db, id)
		r.tick()
	})
	a, err := db.GetSQLAlert(id)
	if err != nil || a.State != alerts.StateOK || a.LastValue == nil || *a.LastValue != 1 || a.ErrorCount != 0 {
		t.Fatalf("alert result: %+v %v", a, err)
	}
	wantTag := tunnel.QueryTag{Surface: "sql_alert"}.WithObject("alert", id).LogComment()
	for _, msg := range agent.Messages() {
		if msg.Type == "query" && (msg.Settings["readonly"] != "1" || msg.Settings["log_comment"] != wantTag || !strings.HasSuffix(msg.SQL, ") LIMIT 101")) {
			t.Fatalf("query not bounded: %q %v", msg.SQL, msg.Settings)
		}
	}
}

func TestEvaluateFiresAndResolves(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	agent := valueAgent(t, db, &v, nil)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "lag", SQL: "SELECT 1 AS v", Comparator: "gte", Threshold: 100,
		Severity: "critical", ForEvaluations: 2, ResolveEvaluations: 2, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	eval := func(value int64) Outcome {
		t.Helper()
		v.Store(value)
		a, err := db.GetSQLAlert(id)
		if err != nil || a == nil {
			t.Fatal(err)
		}
		out, err := r.Evaluate(a, Credentials{User: "u"})
		if err != nil {
			t.Fatal(err)
		}
		return out
	}
	events := func() []database.AlertEvent {
		evs, err := db.ListAlertEvents(100, alerts.EventTypeSQLAlert, "")
		if err != nil {
			t.Fatal(err)
		}
		return evs
	}
	if out := eval(150); out.State != alerts.StatePending || len(events()) != 0 {
		t.Fatalf("first breach: %+v, %d events", out, len(events()))
	}
	if out := eval(150); out.State != alerts.StateFiring || len(events()) != 1 {
		t.Fatalf("second breach: %+v, %d events", out, len(events()))
	}
	eval(200)
	if n := len(events()); n != 1 {
		t.Fatalf("held condition raised %d events", n)
	}
	eval(5)
	if out := eval(5); out.State != alerts.StateOK {
		t.Fatalf("resolve: %+v", out)
	}
	evs := events()
	if len(evs) != 2 {
		t.Fatalf("want firing and resolved events, got %d", len(evs))
	}
	var firing, resolved *database.AlertEvent
	for i := range evs {
		if strings.HasPrefix(evs[i].Title, "[RESOLVED]") {
			resolved = &evs[i]
		} else {
			firing = &evs[i]
		}
	}
	if firing == nil || !strings.HasPrefix(*firing.Fingerprint, "sql:"+id+":firing:") || firing.Severity != "critical" || !strings.Contains(firing.Title, "value 150 >= 100") {
		t.Fatalf("firing event %+v", firing)
	}
	if resolved == nil || !strings.HasPrefix(*resolved.Fingerprint, "sql:"+id+":resolved:") || !strings.Contains(*resolved.PayloadJSON, `"status":"resolved"`) {
		t.Fatalf("resolved event %+v", resolved)
	}
	hist, err := db.ListAlertEvaluations(conn, "sql", id, 50)
	if err != nil || len(hist) < 3 {
		t.Fatalf("history %+v %v", hist, err)
	}
}

func TestAutoDisableAfterErrors(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	var fail atomic.Bool
	fail.Store(true)
	agent := valueAgent(t, db, &v, &fail)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "broken", SQL: "SELECT v FROM gone", Comparator: "gt", Threshold: 1, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	for i := 1; i <= MaxConsecutiveErr; i++ {
		a, _ := db.GetSQLAlert(id)
		if !a.Enabled {
			t.Fatalf("disabled after %d errors", i-1)
		}
		if _, err := r.Evaluate(a, Credentials{User: "u"}); err == nil {
			t.Fatal("want an error")
		}
	}
	a, _ := db.GetSQLAlert(id)
	if a.Enabled || a.ErrorCount != MaxConsecutiveErr || a.LastError == nil || !strings.Contains(*a.LastError, "Unknown table") {
		t.Fatalf("after %d errors: %+v", MaxConsecutiveErr, a)
	}
	evs, _ := db.ListAlertEvents(100, alerts.EventTypeSQLAlert, "")
	if len(evs) != 1 || !strings.Contains(evs[0].Title, "disabled") {
		t.Fatalf("want one disabled event, got %+v", evs)
	}
}

// liveExec runs SQL as the test's ClickHouse admin.
func liveExec(t *testing.T, endpoint, sql string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := connector.NewCHClient(endpoint, false).Execute(ctx, sql, os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), nil); err != nil {
		t.Fatal(err)
	}
}

// TestLiveRun runs real alert queries: a count over system.parts, a write
// refused under readonly=1, and a timeout.
func TestLiveRun(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	r := NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}
	c := Credentials{User: user, Password: os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")}

	res, err := r.Run(context.Background(), conn, c, "SELECT count() AS parts FROM system.parts", "value", "gte", 0, 10)
	if err != nil || res.Value == nil || !res.Firing || res.Rows != 1 {
		t.Fatalf("count over system.parts: %+v %v", res, err)
	}
	res, err = r.Run(context.Background(), conn, c, "SELECT number FROM system.numbers LIMIT 500", "rows", "gt", 100, 10)
	if err != nil || res.Rows != RowLimit || !res.Firing || len(res.Preview) != PreviewRows {
		t.Fatalf("rows capped at %d: %+v %v", RowLimit, res, err)
	}

	// A write hidden in a SELECT is refused by readonly=1.
	table := fmt.Sprintf("chui_sql_alert_live_%d", time.Now().UnixNano())
	liveExec(t, endpoint, "CREATE TABLE default."+table+" (n UInt8) ENGINE = MergeTree ORDER BY n")
	t.Cleanup(func() { liveExec(t, endpoint, "DROP TABLE IF EXISTS default."+table) })
	settings := QuerySettings(10)
	_, err = agent.Gateway.ExecuteQueryWithSettings(conn, "ALTER TABLE default."+table+" DELETE WHERE 1", c.User, c.Password, settings, 15*time.Second)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "readonly") {
		t.Fatalf("ALTER under readonly=1: %v", err)
	}
	if _, err := r.Run(context.Background(), conn, c, "ALTER TABLE default."+table+" DELETE WHERE 1", "value", "gt", 0, 10); err == nil {
		t.Fatal("ALTER passed validation")
	}

	start := time.Now()
	_, err = r.Run(context.Background(), conn, c, "SELECT sleep(3) AS s", "value", "gt", 0, 1)
	if err == nil {
		t.Fatal("sleep(3) with a 1 s timeout succeeded")
	}
	if time.Since(start) > 10*time.Second {
		t.Fatalf("timeout not enforced: %v", time.Since(start))
	}
}
