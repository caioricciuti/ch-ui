// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package operations

import (
	"context"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// TestReportRunnerRunsDuringGrace proves NewRunner's gate keeps weekly
// reports running with an active license and in the grace window, and
// pauses them without one.
func TestReportRunnerRunsDuringGrace(t *testing.T) {
	cases := []struct {
		name   string
		access config.ProAccess
		runs   bool
	}{
		{"active", config.ProActive, true},
		{"grace", config.ProGrace, true},
		{"none", config.ProNone, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			db, conn := testutil.WorkerDB(t)
			cfg := &config.Config{AppSecretKey: "test-secret"}
			cfg.SetProAccessForTest(func() config.ProAccess { return tc.access })
			runner := NewRunner(db, nil, cfg)
			now := time.Now().UTC()
			settings := database.OperationsReportSettings{ConnectionID: conn, Enabled: true, Weekday: 1, Hour: 9, NextRunAt: now.Add(-time.Minute).Format(time.RFC3339)}
			if err := db.SaveOperationsReportSettings(settings); err != nil {
				t.Fatal(err)
			}
			runner.Tick(context.Background(), now)
			got, err := db.GetOperationsReportSettings(conn)
			if err != nil {
				t.Fatal(err)
			}
			// No background account is configured, so a tick that runs records
			// a failed attempt; a gated tick leaves the schedule untouched.
			if ran := got.LastAttemptAt != ""; ran != tc.runs {
				t.Fatalf("access %v: ran=%v, want %v", tc.access, ran, tc.runs)
			}
		})
	}
}
