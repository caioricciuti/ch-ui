// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package scheduler

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// A long schedule must not block other schedules, must not overlap itself,
// and tick must return while it is still running.
func TestSlowScheduleDoesNotBlockOthers(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	// The slow query never gets an answer, so its run ends at its timeout.
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.Contains(msg.SQL+msg.Query, "slow") {
			return nil
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`)}
	})
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "schedule", Mode: "service_account", Username: "svc", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	mk := func(name, sql string, timeoutMs int) string {
		t.Helper()
		q, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: name, Query: sql, ConnectionID: conn})
		if err != nil {
			t.Fatal(err)
		}
		id, err := db.CreateSchedule(name, q, conn, "* * * * *", "UTC", "admin", timeoutMs)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	slow := mk("slow", "SELECT 'slow'", 3000)
	fast := mk("fast", "SELECT 'fast'", 1000)
	makeDue := func(id string) {
		t.Helper()
		past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
		if _, err := db.Conn().Exec("UPDATE schedules SET next_run_at=? WHERE id=?", past, id); err != nil {
			t.Fatal(err)
		}
	}
	runs := func(id string) []database.ScheduleRun {
		t.Helper()
		out, err := db.GetScheduleRuns(id, 100, 0)
		if err != nil {
			t.Fatal(err)
		}
		return out
	}

	r := NewRunner(db, agent.Gateway, "test-secret", func() bool { return true })
	makeDue(slow)
	makeDue(fast)

	began := time.Now()
	r.tick()
	if time.Since(began) > time.Second {
		t.Fatal("tick must not wait for runs to finish")
	}

	deadline := time.Now().Add(2 * time.Second)
	for {
		if rs := runs(fast); len(rs) == 1 && rs[0].Status == "success" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("fast schedule did not finish while the slow one was running")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !r.isRunning(slow) {
		t.Fatal("slow schedule should still be running")
	}

	// Another tick while the slow run is in flight must not start it again.
	makeDue(slow)
	r.tick()
	r.wait()
	if rs := runs(slow); len(rs) != 1 {
		t.Fatalf("slow schedule overlapped itself: %d runs", len(rs))
	}
}

func TestSlowThreshold(t *testing.T) {
	cases := map[int]int{0: 48000, 1000: 1000, 500: 1000, 10000: 8000, 30000: 24000, 120000: 96000}
	for timeout, want := range cases {
		if got := slowThresholdMs(timeout); got != want {
			t.Errorf("slowThresholdMs(%d) = %d, want %d", timeout, got, want)
		}
	}
}
