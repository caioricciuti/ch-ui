// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// A server that is slow to answer must not hold up the polls of the other
// connections, and is not polled again while its last poll is running.
func TestSlowConnectionDoesNotStallOthers(t *testing.T) {
	db, slowConn := testutil.WorkerDB(t)
	fastConn, err := db.CreateConnection(database.CreateConnectionParams{Name: "fast", TunnelToken: "fast-token"})
	if err != nil {
		t.Fatal(err)
	}
	empty := func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	}
	var first atomic.Bool
	slow := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if first.CompareAndSwap(false, true) {
			time.Sleep(1500 * time.Millisecond)
		}
		return empty(msg)
	})
	fast := slow.AddAgent(t, "fast-token", empty)
	setHarvesterAccount(t, db, slowConn)
	setHarvesterAccount(t, db, fastConn)
	h := NewHarvester(NewStore(db), db, slow.Gateway, "test-secret", func() bool { return true })

	began := time.Now()
	h.tick()
	if time.Since(began) > time.Second {
		t.Fatal("tick must not wait for polls to finish")
	}
	deadline := time.Now().Add(time.Second)
	for len(fast.Messages()) == 0 || h.isPolling(fastConn) {
		if time.Now().After(deadline) {
			t.Fatal("the fast connection was not polled while the slow one was busy")
		}
		time.Sleep(20 * time.Millisecond)
	}
	perPoll := len(fast.Messages())
	if !h.isPolling(slowConn) {
		t.Fatal("the slow connection's poll should still be running")
	}

	// Both due again: only the fast connection is polled.
	pollAgain(h)
	h.tick()
	h.wait()
	if n := len(fast.Messages()); n != 2*perPoll {
		t.Fatalf("fast connection: %d queries, want %d", n, 2*perPoll)
	}
	if n := len(slow.Messages()); n != perPoll {
		t.Fatalf("slow connection polled twice at once: %d queries, want %d", n, perPoll)
	}
}

// A server that answers SELECT 1 but none of the metric queries (a user
// without system table access) is reported in the connection check's
// history once, not taken for a healthy cluster with nothing to say.
func TestNoReadableMetricsIsRecorded(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.TrimSpace(msg.SQL) == "SELECT 1" {
			return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"1":1}]`)}
		}
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 497. DB::Exception: worker: Not enough privileges. (ACCESS_DENIED)"}
	})
	setHarvesterAccount(t, db, conn)
	h := NewHarvester(NewStore(db), db, agent.Gateway, "test-secret", func() bool { return true })
	for i := 0; i < 2; i++ {
		h.tick()
		h.wait()
		pollAgain(h)
	}
	evals, err := db.ListAlertEvaluations(conn, "health", CheckConnectionUnreachable, 10)
	if err != nil {
		t.Fatal(err)
	}
	var noted []database.AlertEvaluation
	for _, e := range evals {
		if strings.Contains(e.Error, "system tables") {
			noted = append(noted, e)
		}
	}
	if len(noted) != 1 || noted[0].State != "ok" || !strings.Contains(noted[0].Error, "ACCESS_DENIED") {
		t.Fatalf("want one ok evaluation explaining the failure, got %+v", evals)
	}
}
