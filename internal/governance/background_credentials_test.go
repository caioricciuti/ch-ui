// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package governance

import (
	"encoding/json"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"testing"
)

func TestBackgroundAccountExecution(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	s := NewSyncer(NewStore(db), db, agent.Gateway, "test-secret", func() bool { return true })
	agent.CheckWorker(t, db, conn, "governance", func() {
		if _, err := db.Conn().Exec("DELETE FROM gov_sync_state"); err != nil {
			t.Fatal(err)
		}
		s.backgroundTick()
	})
}
