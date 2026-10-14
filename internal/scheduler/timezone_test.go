// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package scheduler

import (
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// TestNextRunUsesScheduleTimezone checks that a run and a skipped slot both
// set the next run from the cron read in the job's timezone, not UTC.
func TestNextRunUsesScheduleTimezone(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	query, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: conn})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSchedule("job", query, conn, "0 9 * * *", "Asia/Tokyo", "admin", 1000)
	if err != nil {
		t.Fatal(err)
	}
	tokyo, err := time.LoadLocation("Asia/Tokyo")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(db, tunnel.NewGateway(db), "test-secret", nil)
	defer r.gateway.Stop()

	check := func(step string) {
		t.Helper()
		s, err := db.GetScheduleByID(id)
		if err != nil || s == nil || s.NextRunAt == nil {
			t.Fatalf("%s: %v %v", step, s, err)
		}
		next, err := time.Parse(time.RFC3339, *s.NextRunAt)
		if err != nil {
			t.Fatal(err)
		}
		if local := next.In(tokyo); local.Hour() != 9 || local.Minute() != 0 {
			t.Errorf("%s: next run %s is %s in Tokyo, want 09:00", step, *s.NextRunAt, local.Format("15:04"))
		}
	}

	s, err := db.GetScheduleByID(id)
	if err != nil || s == nil {
		t.Fatal(err)
	}
	// No agent is connected, so the run records an error; it still sets the
	// next run.
	r.runSchedule(*s)
	check("after run")

	r.skipMissedRun(*s, time.Now().UTC())
	check("after skip")
}
