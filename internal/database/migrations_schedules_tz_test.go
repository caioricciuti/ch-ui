// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"testing"
	"time"
)

// TestRecomputeZonedScheduleRuns moves the stored next run of enabled
// non-UTC jobs to the cron read in their timezone, once, and leaves UTC and
// disabled jobs alone.
func TestRecomputeZonedScheduleRuns(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	queryID, err := db.CreateSavedQuery(CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: connID})
	if err != nil {
		t.Fatal(err)
	}
	create := func(name, tz string, enabled bool) string {
		t.Helper()
		id, err := db.CreateSchedule(name, queryID, connID, "0 9 * * *", tz, "admin", 1000)
		if err != nil {
			t.Fatal(err)
		}
		if !enabled {
			if err := db.UpdateSchedule(id, name, "0 9 * * *", tz, false, 1000); err != nil {
				t.Fatal(err)
			}
		}
		// What earlier versions stored: 09:00 UTC.
		old := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
		if err := db.SetScheduleNextRun(id, &old); err != nil {
			t.Fatal(err)
		}
		return id
	}
	madrid := create("madrid", "Europe/Madrid", true)
	utc := create("utc", "UTC", true)
	off := create("off", "Europe/Madrid", false)
	nextRun := func(id string) string {
		t.Helper()
		s, err := db.GetScheduleByID(id)
		if err != nil || s == nil || s.NextRunAt == nil {
			t.Fatalf("schedule %s: %v %v", id, s, err)
		}
		return *s.NextRunAt
	}

	// The template database already ran the migration; clear its marker.
	if _, err := db.conn.Exec("DELETE FROM settings WHERE key = ?", settingSchedulesZonedNextRun); err != nil {
		t.Fatal(err)
	}
	// Saturday before Madrid leaves summer time on Sunday 2026-10-25: 09:00
	// Madrid that Sunday is 08:00 UTC.
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	if err := db.recomputeZonedScheduleRuns(now); err != nil {
		t.Fatal(err)
	}
	if got, want := nextRun(madrid), "2026-10-25T08:00:00Z"; got != want {
		t.Errorf("madrid: got %s, want %s", got, want)
	}
	for _, id := range []string{utc, off} {
		if got, want := nextRun(id), "2026-10-25T09:00:00Z"; got != want {
			t.Errorf("%s: got %s, want %s (unchanged)", id, got, want)
		}
	}

	// Once only: a second call leaves next_run_at alone.
	moved := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if err := db.SetScheduleNextRun(madrid, &moved); err != nil {
		t.Fatal(err)
	}
	if err := db.recomputeZonedScheduleRuns(now); err != nil {
		t.Fatal(err)
	}
	if got, want := nextRun(madrid), "2026-11-01T00:00:00Z"; got != want {
		t.Errorf("second call: got %s, want %s", got, want)
	}
}
