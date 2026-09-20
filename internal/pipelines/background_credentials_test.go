package pipelines

import (
	"context"
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
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": "default", "table": "target", "create_table": true}}
	batch := Batch{Records: []Record{{Data: map[string]interface{}{"value": float64(1)}}}}
	runs := 0
	agent.CheckWorker(t, db, conn, "pipeline", func() {
		count, err := sink.WriteBatch(context.Background(), cfg, batch)
		runs++
		if runs < 3 && (err != nil || count != 1) {
			t.Fatalf("write: %d %v", count, err)
		}
		if runs == 3 && err == nil {
			t.Fatal("disabled sink accepted batch")
		}
	})
}
