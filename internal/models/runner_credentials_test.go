package models

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// A run given credentials executes as that user, with no background account
// configured, and a Check error refuses the run before anything is recorded.
func TestRunWithCallerCredentialsAndCheck(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	id, err := db.CreateModel(conn, "orders_v", "", "analytics", "view", "SELECT 1 AS value", "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(db, agent.Gateway, "test-secret")

	refused := errors.New("refused")
	_, err = r.RunModelWith(conn, id, ScopeOnly, RunOptions{
		TriggeredBy: "mcp:k",
		Credentials: &Credentials{User: "mcp_user", Password: "pw"},
		Check: func(m database.Model, resolved string) error {
			if m.TargetDatabase == "analytics" && resolved != "" {
				return refused
			}
			return nil
		},
	})
	if !errors.Is(err, refused) {
		t.Fatalf("check should refuse the run, got %v", err)
	}
	if runs, _ := db.GetModelRuns(conn, 10, 0); len(runs) != 0 {
		t.Fatalf("a refused run must not be recorded, got %d runs", len(runs))
	}
	if n := len(agent.Messages()); n != 0 {
		t.Fatalf("a refused run must not reach ClickHouse, got %d queries", n)
	}

	runID, err := r.RunModelWith(conn, id, ScopeOnly, RunOptions{
		TriggeredBy: "mcp:k",
		Credentials: &Credentials{User: "mcp_user", Password: "pw"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if runID == "" {
		t.Fatal("run id missing")
	}
	msgs := agent.Messages()
	if len(msgs) == 0 {
		t.Fatal("the run sent nothing to ClickHouse")
	}
	for _, m := range msgs {
		if m.User != "mcp_user" {
			t.Fatalf("query ran as %q, want mcp_user: %s", m.User, m.SQL)
		}
	}
}
