// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
)

// TestDispatcherRunsDuringGrace proves the dispatcher keeps delivering in the
// license grace window (monitors keep firing there) and pauses without one.
func TestDispatcherRunsDuringGrace(t *testing.T) {
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
			db := openTestDB(t)
			cfg := &config.Config{AppSecretKey: "test-secret"}
			cfg.SetProAccessForTest(func() config.ProAccess { return tc.access })
			d := NewDispatcher(db, cfg)

			if _, err := db.CreateAlertEvent(nil, EventTypeTelemetryMonitor, SeverityError, "t", "m", nil, "fp", ""); err != nil {
				t.Fatalf("CreateAlertEvent: %v", err)
			}
			d.tick()

			pending, err := db.ListNewAlertEvents(10)
			if err != nil {
				t.Fatalf("ListNewAlertEvents: %v", err)
			}
			if processed := len(pending) == 0; processed != tc.runs {
				t.Fatalf("access %v: processed=%v, want %v", tc.access, processed, tc.runs)
			}
		})
	}
}
