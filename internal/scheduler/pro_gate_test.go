// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package scheduler

import (
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// TestTickFollowsLicense checks that due schedules are skipped while there is
// no Pro license, run during grace and while active, and are never disabled.
func TestTickFollowsLicense(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	query, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: conn})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSchedule("job", query, conn, "* * * * *", "UTC", "admin", 1000)
	if err != nil {
		t.Fatal(err)
	}
	makeDue := func() {
		t.Helper()
		past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
		if _, err := db.Conn().Exec("UPDATE schedules SET next_run_at=? WHERE id=?", past, id); err != nil {
			t.Fatal(err)
		}
	}
	runCount := func() int {
		t.Helper()
		runs, err := db.GetScheduleRuns(id, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		return len(runs)
	}

	access := config.ProNone
	gate := config.NewProGate("Scheduled query jobs", func() config.ProAccess { return access })
	// No agent is connected, so each run records a "tunnel not connected"
	// error. That still counts as a run: the gate is what is under test.
	r := NewRunner(db, tunnel.NewGateway(db), "test-secret", gate.Allow)
	defer r.gateway.Stop()

	makeDue()
	r.tick()
	if n := runCount(); n != 0 {
		t.Fatalf("ProNone: got %d runs, want 0", n)
	}
	job, err := db.GetScheduleByID(id)
	if err != nil || job == nil || !job.Enabled {
		t.Fatalf("ProNone must leave the schedule enabled: %#v %v", job, err)
	}

	want := 0
	for _, state := range []config.ProAccess{config.ProGrace, config.ProActive} {
		access = state
		makeDue()
		r.tick()
		want++
		if n := runCount(); n != want {
			t.Fatalf("access %v: got %d runs, want %d", state, n, want)
		}
	}

	access = config.ProNone
	makeDue()
	r.tick()
	if n := runCount(); n != want {
		t.Fatalf("back to ProNone: got %d runs, want %d", n, want)
	}
}

func TestTickWithoutGateDoesNothing(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	query, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: conn})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSchedule("job", query, conn, "* * * * *", "UTC", "admin", 1000)
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.Conn().Exec("UPDATE schedules SET next_run_at=? WHERE id=?", past, id); err != nil {
		t.Fatal(err)
	}
	r := NewRunner(db, tunnel.NewGateway(db), "test-secret", nil)
	defer r.gateway.Stop()
	r.tick()
	runs, err := db.GetScheduleRuns(id, 100, 0)
	if err != nil || len(runs) != 0 {
		t.Fatalf("nil gate must fail closed: %d runs, %v", len(runs), err)
	}
}
