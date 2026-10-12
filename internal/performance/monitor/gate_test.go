// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package monitor

import (
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// TestMonitorRunsDuringGrace proves a ProGate-backed monitor scans with an
// active license and in the grace window, and does nothing without one.
func TestMonitorRunsDuringGrace(t *testing.T) {
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
			agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
				return &tunnel.AgentMessage{Type: "query_result", Data: []byte("[]")}
			})
			gate := config.NewProGate("Performance monitor", func() config.ProAccess { return tc.access })
			m := New(db, agent.Gateway, "test-secret", gate.Allow)
			if err := db.SetPerformanceMonitor(conn, true); err != nil {
				t.Fatal(err)
			}
			m.tick(time.Now(), make(chan struct{}))
			state, err := db.GetPerformanceMonitor(conn)
			if err != nil {
				t.Fatal(err)
			}
			// Without a service account a scan that runs records an error; a
			// scan that is gated off leaves the state untouched.
			if ran := state.LastError != ""; ran != tc.runs {
				t.Fatalf("access %v: ran=%v (last_error=%q), want %v", tc.access, ran, state.LastError, tc.runs)
			}
		})
	}
}
