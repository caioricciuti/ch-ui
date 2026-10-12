package models

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// fakeRunner wires a Runner to a protocol-level agent that answers catalog
// reads with empty results and fails any statement containing failOn.
func fakeRunner(t *testing.T, failOn string) (*Runner, *database.DB, string) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if failOn != "" && strings.Contains(msg.SQL, failOn) {
			return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 47. Unknown identifier"}
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	enc, err := crypto.Encrypt("pw", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "model", Mode: "service_account", Username: "svc", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	return NewRunner(db, agent.Gateway, "test-secret"), db, conn
}

func TestRunSingleUnknownModelRecordsNothing(t *testing.T) {
	r, db, conn := fakeRunner(t, "")
	if _, err := db.CreateModel(conn, "known", "", "default", "view", "SELECT 1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := r.RunSingle(conn, "no-such-model", "test"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("want ErrModelNotFound, got %v", err)
	}
	if _, err := r.RunPipeline(conn, "no-such-model", "test"); !errors.Is(err, ErrModelNotFound) {
		t.Fatalf("want ErrModelNotFound for a pipeline anchor, got %v", err)
	}
	runs, err := db.GetModelRuns(conn, 10, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 0 {
		t.Fatalf("no run may be recorded, got %d", len(runs))
	}
}

func TestFailedRunReturnsRunFailedError(t *testing.T) {
	r, db, conn := fakeRunner(t, "broken_column")
	if _, err := db.CreateModel(conn, "good", "", "default", "view", "SELECT 1 AS id", "", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateModel(conn, "bad", "", "default", "view", "SELECT broken_column FROM $ref(good)", "", "", ""); err != nil {
		t.Fatal(err)
	}
	runID, err := r.RunAll(conn, "test")
	var failed *RunFailedError
	if !errors.As(err, &failed) {
		t.Fatalf("want RunFailedError, got %v", err)
	}
	if failed.RunID != runID || runID == "" || failed.Status != "partial" || failed.Failed != 1 {
		t.Fatalf("unexpected outcome %+v (run %s)", failed, runID)
	}
	run, _ := db.GetModelRunByID(runID)
	if run == nil || run.Status != "partial" {
		t.Fatalf("run must be recorded as partial, got %+v", run)
	}
}

func TestSchedulerRecordsFailedRuns(t *testing.T) {
	r, db, conn := fakeRunner(t, "broken_column")
	id, err := db.CreateModel(conn, "bad", "", "default", "view", "SELECT broken_column", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.UpsertModelSchedule(conn, id, "0 * * * *", past, ""); err != nil {
		t.Fatal(err)
	}
	NewScheduler(db, r).tick()
	sched, err := db.GetModelScheduleByAnchor(conn, id)
	if err != nil || sched == nil {
		t.Fatalf("schedule: %v %v", sched, err)
	}
	if sched.LastStatus == nil || *sched.LastStatus != "error" {
		t.Fatalf("a run with a failed model must be recorded as error, got %v", sched.LastStatus)
	}
	if sched.LastError == nil || !strings.Contains(*sched.LastError, "1 failed") {
		t.Fatalf("last_error should say what failed, got %v", sched.LastError)
	}
}

func TestSchedulerSkipsPausedSchedules(t *testing.T) {
	r, db, conn := fakeRunner(t, "")
	id, err := db.CreateModel(conn, "m", "", "default", "view", "SELECT 1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.UpsertModelScheduleEnabled(conn, id, "0 * * * *", past, "", false); err != nil {
		t.Fatal(err)
	}
	NewScheduler(db, r).tick()
	runs, _ := db.GetModelRuns(conn, 10, 0)
	if len(runs) != 0 {
		t.Fatalf("a paused schedule must not run, got %d runs", len(runs))
	}
}

func TestStartClosesInterruptedRuns(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	a, err := db.CreateModel(conn, "a", "", "default", "view", "SELECT 1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	b, err := db.CreateModel(conn, "b", "", "default", "view", "SELECT 1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	runID, err := db.CreateModelRun(conn, 2, "test")
	if err != nil {
		t.Fatal(err)
	}
	db.CreateModelRunResult(runID, a, "a")
	db.CreateModelRunResult(runID, b, "b")
	db.UpdateModelRunResult(runID, a, "success", "SELECT 1", 1, "")
	db.UpdateModelRunResult(runID, b, "running", "", 0, "")

	s := NewScheduler(db, nil)
	s.Start()
	s.Stop()

	run, _ := db.GetModelRunByID(runID)
	if run.Status != "error" || run.FinishedAt == nil || run.Succeeded != 1 || run.Failed != 1 {
		t.Fatalf("interrupted run not closed: %+v", run)
	}
	results, _ := db.GetModelRunResults(runID)
	for _, res := range results {
		if res.ModelID == b && (res.Status != "error" || res.Error == nil || *res.Error != InterruptedRunMessage) {
			t.Fatalf("interrupted result not closed: %+v", res)
		}
		if res.ModelID == a && res.Status != "success" {
			t.Fatalf("finished result must keep its status: %+v", res)
		}
	}
}
