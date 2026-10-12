package models

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestBackgroundAccountExecution(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	// A unique name per run: a model never replaces a view it did not build,
	// so a leftover from an earlier live run would fail this one.
	name := fmt.Sprintf("chui_bg_model_%d", time.Now().UnixNano())
	_, err := db.CreateModel(conn, name, "", "default", "view", "SELECT 1 AS value", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL"); endpoint != "" {
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if _, err := connector.NewCHClient(endpoint, false).Execute(ctx, "DROP VIEW IF EXISTS default."+name, os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), nil); err != nil {
				t.Error(err)
			}
		})
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
