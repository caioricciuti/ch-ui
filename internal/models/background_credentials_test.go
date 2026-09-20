package models

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
	_, err := db.CreateModel(conn, "test_model", "", "default", "view", "SELECT 1 AS value", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(db, agent.Gateway, "test-secret")
	runs := 0
	agent.CheckWorker(t, db, conn, "model", func() {
		_, err := r.RunAll(conn, "test")
		runs++
		if runs < 3 && err != nil {
			t.Fatal(err)
		}
		if runs == 3 && err == nil {
			t.Fatal("disabled model run accepted")
		}
	})
}
