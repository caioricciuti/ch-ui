package clusterhealth

import (
	"encoding/json"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"testing"
	"time"
)

func TestBackgroundAccountExecution(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	h := NewHarvester(NewStore(db), db, agent.Gateway, "test-secret")
	agent.CheckWorker(t, db, conn, "cluster_health", func() { h.pollConnection(conn, DefaultSettings(conn), time.Now()) })
}
