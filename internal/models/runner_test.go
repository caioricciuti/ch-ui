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
	if _, err := r.RunNamedPipeline(conn, "no-such-pipeline", "test"); !errors.Is(err, ErrPipelineNotFound) {
		t.Fatalf("want ErrPipelineNotFound, got %v", err)
	}
	var selErr *SelectorError
	if _, err := r.Run(conn, RunOptions{Selector: "+no_such_model"}); !errors.As(err, &selErr) {
		t.Fatalf("want a selector error, got %v", err)
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

func scheduledPipeline(t *testing.T, db *database.DB, conn, selector string, enabled bool) string {
	t.Helper()
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	id, err := db.CreateModelPipeline(database.ModelPipeline{
		ConnectionID: conn, Name: "nightly", Selector: selector, Cron: "0 * * * *", Enabled: enabled, NextRunAt: &past,
	})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSchedulerRecordsFailedRuns(t *testing.T) {
	r, db, conn := fakeRunner(t, "broken_column")
	if _, err := db.CreateModel(conn, "bad", "", "default", "view", "SELECT broken_column", "", "", ""); err != nil {
		t.Fatal(err)
	}
	id := scheduledPipeline(t, db, conn, "bad", true)
	NewScheduler(db, r).tick()
	p, err := db.GetModelPipelineByID(id)
	if err != nil || p == nil {
		t.Fatalf("pipeline: %v %v", p, err)
	}
	if p.LastStatus == nil || *p.LastStatus != "error" {
		t.Fatalf("a run with a failed model must be recorded as error, got %v", p.LastStatus)
	}
	if p.LastError == nil || !strings.Contains(*p.LastError, "1 failed") {
		t.Fatalf("last_error should say what failed, got %v", p.LastError)
	}
	if p.LastRunID == nil || p.NextRunAt == nil {
		t.Fatalf("run id and next run must be recorded: %+v", p)
	}
	runs, _ := db.GetModelRuns(conn, 10, 0)
	if len(runs) != 1 || runs[0].PipelineID == nil || *runs[0].PipelineID != id || runs[0].Selector != "bad" {
		t.Fatalf("the run must record its pipeline and selector: %+v", runs)
	}
}

func TestSchedulerSkipsPausedSchedules(t *testing.T) {
	r, db, conn := fakeRunner(t, "")
	if _, err := db.CreateModel(conn, "m", "", "default", "view", "SELECT 1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	scheduledPipeline(t, db, conn, "m", false)
	NewScheduler(db, r).tick()
	runs, _ := db.GetModelRuns(conn, 10, 0)
	if len(runs) != 0 {
		t.Fatalf("a paused schedule must not run, got %d runs", len(runs))
	}
}

func TestMigrateLegacySchedules(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	mk := func(name, sql string) string {
		t.Helper()
		id, err := db.CreateModel(conn, name, "", "default", "view", sql, "", "", "")
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	// raw -> clean -> daily, raw -> other: "+clean+" misses other, so the
	// component's names are used. lone has its own component.
	mk("raw", "SELECT 1")
	clean := mk("clean", "SELECT * FROM $ref(raw)")
	mk("daily", "SELECT * FROM $ref(clean)")
	mk("other", "SELECT * FROM $ref(raw)")
	lone := mk("lone", "SELECT 1")
	// Legacy rows are only read now, so they are seeded directly.
	for _, s := range []struct {
		id, anchor, cron string
		enabled          int
	}{{"sched-clean", clean, "0 3 * * *", 1}, {"sched-lone", lone, "0 4 * * *", 0}} {
		if _, err := db.Conn().Exec(
			`INSERT INTO model_schedules (id, connection_id, anchor_model_id, cron, enabled) VALUES (?, ?, ?, ?, ?)`,
			s.id, conn, s.anchor, s.cron, s.enabled,
		); err != nil {
			t.Fatal(err)
		}
	}
	n, err := MigrateLegacySchedules(db)
	if err != nil || n != 2 {
		t.Fatalf("migrated %d, err %v", n, err)
	}
	if n, err := MigrateLegacySchedules(db); err != nil || n != 0 {
		t.Fatalf("second migration must do nothing: %d %v", n, err)
	}
	byName := map[string]database.ModelPipeline{}
	ps, _ := db.GetModelPipelines(conn)
	for _, p := range ps {
		byName[p.Name] = p
	}
	if p := byName["clean"]; len(strings.Split(p.Selector, ",")) != 4 || strings.Contains(p.Selector, "lone") || p.Cron != "0 3 * * *" || !p.Enabled {
		t.Fatalf("clean pipeline: %+v", p)
	}
	if p := byName["lone"]; p.Selector != "+lone+" || p.Enabled {
		t.Fatalf("lone pipeline: %+v", p)
	}
	if all, err := db.GetAllModelSchedules(); err != nil || len(all) != 2 {
		t.Fatalf("the legacy schedules must be kept: %d %v", len(all), err)
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

func TestSchedulerRecordsAnInvalidSelectorOncePerDueTime(t *testing.T) {
	r, db, conn := fakeRunner(t, "")
	id, err := db.CreateModel(conn, "gone", "", "default", "view", "SELECT 1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateModel(conn, "kept", "", "default", "view", "SELECT 1", "", "", ""); err != nil {
		t.Fatal(err)
	}
	pid := scheduledPipeline(t, db, conn, "kept,+gone", true)
	if err := db.DeleteModel(id); err != nil {
		t.Fatal(err)
	}
	s := NewScheduler(db, r)
	s.tick()
	p, err := db.GetModelPipelineByID(pid)
	if err != nil || p == nil {
		t.Fatalf("pipeline: %v %v", p, err)
	}
	if p.LastStatus == nil || *p.LastStatus != "error" || p.LastError == nil || !strings.Contains(*p.LastError, `unknown model "gone"`) {
		t.Fatalf("an invalid selector must be recorded as an error naming the model: %+v", p)
	}
	if p.NextRunAt == nil || p.LastRunAt == nil {
		t.Fatalf("the next run must still be scheduled: %+v", p)
	}
	next, err := time.Parse(time.RFC3339, *p.NextRunAt)
	if err != nil || !next.After(time.Now()) {
		t.Fatalf("next run must move to the future, got %v", *p.NextRunAt)
	}
	// Not due again until the next cron time: a second tick records nothing new.
	lastRun := *p.LastRunAt
	if _, err := db.Conn().Exec(`UPDATE model_pipelines SET last_run_at = 'marker' WHERE id = ?`, pid); err != nil {
		t.Fatal(err)
	}
	s.tick()
	if p, _ = db.GetModelPipelineByID(pid); p.LastRunAt == nil || *p.LastRunAt != "marker" {
		t.Fatalf("a pipeline that is not due must not be recorded again (was %s): %+v", lastRun, p)
	}
	if runs, _ := db.GetModelRuns(conn, 10, 0); len(runs) != 0 {
		t.Fatalf("an invalid selector must not record a run, got %d", len(runs))
	}
}
