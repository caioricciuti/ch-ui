// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package sqlalerts

import (
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

func sqlEvents(t *testing.T, db *database.DB) []database.AlertEvent {
	t.Helper()
	evs, err := db.ListAlertEvents(100, alerts.EventTypeSQLAlert, "")
	if err != nil {
		t.Fatal(err)
	}
	return evs
}

func mustGet(t *testing.T, db *database.DB, id string) *database.SQLAlert {
	t.Helper()
	a, err := db.GetSQLAlert(id)
	if err != nil || a == nil {
		t.Fatalf("get alert: %+v %v", a, err)
	}
	return a
}

// A run that loaded the alert before an edit with reset does not write its
// outcome over the reset, and raises nothing.
func TestStaleRunAfterResetIsDropped(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	v.Store(150)
	agent := valueAgent(t, db, &v, nil)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "lag", SQL: "SELECT 1 AS v", Comparator: "gte", Threshold: 100,
		ForEvaluations: 2, ResolveEvaluations: 1, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	if _, err := r.Evaluate(mustGet(t, db, id), Credentials{User: "u"}); err != nil {
		t.Fatal(err)
	}
	stale := mustGet(t, db, id)
	if stale.State != alerts.StatePending {
		t.Fatalf("want pending, got %s", stale.State)
	}

	edited := *stale
	edited.Threshold = 120
	if err := db.UpdateSQLAlert(&edited, true); err != nil {
		t.Fatal(err)
	}
	out, err := r.Evaluate(stale, Credentials{User: "u"})
	if err != nil {
		t.Fatal(err)
	}
	a := mustGet(t, db, id)
	if a.State != alerts.StateOK || a.BreachCount != 0 || out.State != alerts.StatePending {
		t.Fatalf("stale run wrote over the reset: stored %+v, outcome %+v", a, out)
	}
	if n := len(sqlEvents(t, db)); n != 0 {
		t.Fatalf("stale run raised %d events", n)
	}
}

// An alert that disables itself while firing resolves its incident.
func TestAutoDisableResolvesFiringIncident(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	var fail atomic.Bool
	v.Store(150)
	agent := valueAgent(t, db, &v, &fail)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "lag", SQL: "SELECT 1 AS v", Comparator: "gte", Threshold: 100,
		Severity: "critical", ForEvaluations: 1, ResolveEvaluations: 1, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	if out, err := r.Evaluate(mustGet(t, db, id), Credentials{User: "u"}); err != nil || out.State != alerts.StateFiring {
		t.Fatalf("want firing: %+v %v", out, err)
	}
	fail.Store(true)
	for i := 0; i < MaxConsecutiveErr; i++ {
		if _, err := r.Evaluate(mustGet(t, db, id), Credentials{User: "u"}); err == nil {
			t.Fatal("want an error")
		}
	}
	a := mustGet(t, db, id)
	if a.Enabled || a.State != alerts.StateOK || a.ErrorCount != MaxConsecutiveErr {
		t.Fatalf("after auto-disable: %+v", a)
	}
	var firing, disabled, resolved int
	for _, ev := range sqlEvents(t, db) {
		switch {
		case strings.HasPrefix(ev.Title, "[RESOLVED] "):
			resolved++
			if ev.Severity != "critical" || !strings.Contains(ev.Message, "disabled after") || !strings.HasPrefix(*ev.Fingerprint, "sql:"+id+":resolved:") {
				t.Fatalf("resolved event %+v", ev)
			}
		case strings.Contains(ev.Title, "disabled"):
			disabled++
		default:
			firing++
		}
	}
	if firing != 1 || disabled != 1 || resolved != 1 {
		t.Fatalf("want 1 firing, 1 disabled, 1 resolved; got %d %d %d", firing, disabled, resolved)
	}
}

// Disabling an alert returns its state to ok and keeps its errors; an
// update against a state that moved is refused.
func TestUpdateSQLAlertStateRules(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "a", SQL: "SELECT 1 AS v", Comparator: "gt", Threshold: 1, Enabled: true})
	if _, err := db.Conn().Exec(`UPDATE sql_alerts SET state = 'firing', breach_count = 0, error_count = 2, last_error = 'boom' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	a := mustGet(t, db, id)

	moved := *a
	moved.State = alerts.StateOK
	if err := db.UpdateSQLAlert(&moved, false); !errors.Is(err, database.ErrSQLAlertStateChanged) {
		t.Fatalf("update against a stale state: %v", err)
	}

	a.Enabled = false
	if err := db.UpdateSQLAlert(a, false); err != nil {
		t.Fatal(err)
	}
	got := mustGet(t, db, id)
	if got.State != alerts.StateOK || got.ErrorCount != 2 || got.LastError == nil || *got.LastError != "boom" {
		t.Fatalf("after disable: %+v", got)
	}

	missing := *got
	missing.ID = "nope"
	if err := db.UpdateSQLAlert(&missing, false); err == nil || errors.Is(err, database.ErrSQLAlertStateChanged) {
		t.Fatalf("update of a missing alert: %v", err)
	}
}

// The history timestamps of deleted alerts are dropped on the next tick.
func TestHistoryPrunedForDeletedAlerts(t *testing.T) {
	lastHistory.Store("kept", time.Now())
	lastHistory.Store("gone", time.Now())
	t.Cleanup(func() { lastHistory.Delete("kept") })
	pruneHistory([]database.SQLAlert{{ID: "kept"}})
	if _, ok := lastHistory.Load("gone"); ok {
		t.Fatal("history of a deleted alert kept")
	}
	if _, ok := lastHistory.Load("kept"); !ok {
		t.Fatal("history of a live alert dropped")
	}
}

// A transition whose alert event cannot be written does not advance the
// alert: firing, resolve and the auto-disable of a firing alert all wait
// for the next tick.
func TestSQLStateWaitsForItsEvent(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	var fail atomic.Bool
	agent := valueAgent(t, db, &v, &fail)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "lag", SQL: "SELECT 1 AS v", Comparator: "gte", Threshold: 100,
		ForEvaluations: 1, ResolveEvaluations: 1, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	block := func(on bool) {
		t.Helper()
		q := `DROP TRIGGER fail_events`
		if on {
			q = `CREATE TRIGGER fail_events BEFORE INSERT ON alert_events BEGIN SELECT RAISE(ABORT, 'disk full'); END`
		}
		if _, err := db.Conn().Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	eval := func(value int64) {
		t.Helper()
		v.Store(value)
		_, _ = r.Evaluate(mustGet(t, db, id), Credentials{User: "u"})
	}

	block(true)
	eval(150)
	a := mustGet(t, db, id)
	if a.State != alerts.StateOK || a.LastRunAt != nil || !Due(a, time.Now()) {
		t.Fatalf("firing without an event advanced the alert: %+v", a)
	}
	block(false)
	eval(150)
	if a := mustGet(t, db, id); a.State != alerts.StateFiring || len(sqlEvents(t, db)) != 1 {
		t.Fatalf("retry must fire: %+v, %d events", a, len(sqlEvents(t, db)))
	}

	lastRun := *mustGet(t, db, id).LastRunAt
	block(true)
	eval(5)
	if a := mustGet(t, db, id); a.State != alerts.StateFiring || a.OkCount != 0 || a.LastRunAt == nil || *a.LastRunAt != lastRun {
		t.Fatalf("resolve without an event advanced the alert: %+v", a)
	}

	fail.Store(true)
	if _, err := db.Conn().Exec(`UPDATE sql_alerts SET error_count = ? WHERE id = ?`, MaxConsecutiveErr-1, id); err != nil {
		t.Fatal(err)
	}
	eval(0)
	if a := mustGet(t, db, id); !a.Enabled || a.State != alerts.StateFiring || a.ErrorCount != MaxConsecutiveErr-1 {
		t.Fatalf("auto-disable without the resolve advanced the alert: %+v", a)
	}
	block(false)
	eval(0)
	a = mustGet(t, db, id)
	if a.Enabled || a.State != alerts.StateOK {
		t.Fatalf("retry must disable: %+v", a)
	}
	resolved := 0
	for _, ev := range sqlEvents(t, db) {
		if strings.HasPrefix(ev.Title, "[RESOLVED] ") {
			resolved++
		}
	}
	if resolved != 1 {
		t.Fatalf("want exactly one resolve, got %d", resolved)
	}
}

// Firing and resolve events carry the alert key and the same incident id,
// so an acknowledgement covers exactly one incident.
func TestSQLAlertEventsCarryIncident(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var v atomic.Int64
	v.Store(150)
	agent := valueAgent(t, db, &v, nil)
	id := createAlert(t, db, database.SQLAlert{ConnectionID: conn, Name: "lag", SQL: "SELECT 1 AS v", Comparator: "gte", Threshold: 100,
		ForEvaluations: 1, ResolveEvaluations: 1, Enabled: true})
	r := NewRunner(db, agent.Gateway, nil, "test-secret", nil)
	if _, err := r.Evaluate(mustGet(t, db, id), Credentials{User: "u"}); err != nil {
		t.Fatal(err)
	}
	time.Sleep(1100 * time.Millisecond)
	v.Store(0)
	if _, err := r.Evaluate(mustGet(t, db, id), Credentials{User: "u"}); err != nil {
		t.Fatal(err)
	}
	evs := sqlEvents(t, db)
	if len(evs) != 2 {
		t.Fatalf("want firing and resolve, got %d", len(evs))
	}
	var ids []alerts.EventIdentity
	for _, ev := range evs {
		ids = append(ids, alerts.IdentifyEvent(ev))
	}
	if ids[0].AlertKey != AlertKey(id) || ids[0].AlertKey != ids[1].AlertKey || ids[0].IncidentID == "" || ids[0].IncidentID != ids[1].IncidentID {
		t.Fatalf("identities %+v", ids)
	}
	if !strings.Contains(*evs[0].PayloadJSON, `"status":"resolved"`) && !strings.Contains(*evs[1].PayloadJSON, `"status":"resolved"`) {
		t.Fatal("no resolve")
	}
}
