package scheduler

import (
	"encoding/json"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"testing"
)

func TestBackgroundAccountExecution(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	query, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: conn})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSchedule("job", query, conn, "* * * * *", "UTC", "admin", 1000)
	if err != nil {
		t.Fatal(err)
	}
	job, err := db.GetScheduleByID(id)
	if err != nil {
		t.Fatal(err)
	}
	r := NewRunner(db, agent.Gateway, "test-secret")
	agent.CheckWorker(t, db, conn, "schedule", func() { r.runSchedule(*job) })
	runs, err := db.GetScheduleRuns(id, 10, 0)
	if err != nil || len(runs) != 3 {
		t.Fatalf("runs: %d %v", len(runs), err)
	}
	statuses := map[string]int{}
	for _, run := range runs {
		statuses[run.Status]++
	}
	if statuses["success"] != 2 || statuses["error"] != 1 {
		t.Fatalf("statuses: %v", statuses)
	}
}
