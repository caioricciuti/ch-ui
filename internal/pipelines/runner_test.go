package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
)

// fakeSink records batches; fail makes the first n writes fail.
type fakeSink struct {
	mu     sync.Mutex
	fail   int
	calls  int
	tokens []string
	rows   int
}

func (f *fakeSink) Type() string                   { return "sink_clickhouse" }
func (f *fakeSink) Validate(ConnectorConfig) error { return nil }
func (f *fakeSink) WriteBatch(ctx context.Context, _ ConnectorConfig, b Batch) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.fail {
		return 0, errors.New("clickhouse down")
	}
	f.tokens = append(f.tokens, b.DedupToken)
	f.rows += len(b.Records)
	return len(b.Records), nil
}

func (f *fakeSink) snapshot() (calls, rows int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls, f.rows
}

// funcSource runs fn as its Start.
type funcSource struct {
	fn func(ctx context.Context, out chan<- Batch) error
}

func (s *funcSource) Type() string                   { return "source_webhook" }
func (s *funcSource) Validate(ConnectorConfig) error { return nil }
func (s *funcSource) Start(ctx context.Context, _ ConnectorConfig, out chan<- Batch) error {
	return s.fn(ctx, out)
}

func shortBackoff(t *testing.T) {
	t.Helper()
	oldMin, oldMax := minBackoff, maxBackoff
	minBackoff, maxBackoff = 5*time.Millisecond, 20*time.Millisecond
	t.Cleanup(func() { minBackoff, maxBackoff = oldMin, oldMax })
}

func testRunnerDB(t *testing.T) (*database.DB, string, *config.Config) {
	t.Helper()
	dir := t.TempDir()
	db, err := dbtest.OpenIn(t, dir, "ch-ui.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	return db, conn, &config.Config{AppSecretKey: "test-secret", DatabasePath: filepath.Join(dir, "ch-ui.db")}
}

// webhookPipeline creates a pipeline with a webhook source wired to a sink.
func webhookPipeline(t *testing.T, db *database.DB, conn, name string) string {
	t.Helper()
	id, err := db.CreatePipeline(name, "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	src, _ := json.Marshal(map[string]interface{}{"batch_size": 10, "batch_timeout_ms": 100})
	sink, _ := json.Marshal(map[string]interface{}{"database": "default", "table": "t"})
	nodes := []database.PipelineNode{
		{ID: id + "-src", NodeType: "source_webhook", ConfigEncrypted: string(src)},
		{ID: id + "-sink", NodeType: "sink_clickhouse", ConfigEncrypted: string(sink)},
	}
	edges := []database.PipelineEdge{{ID: id + "-e", SourceNodeID: id + "-src", TargetNodeID: id + "-sink"}}
	if err := db.SavePipelineGraph(id, nodes, edges, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

func newTestRunner(t *testing.T, db *database.DB, cfg *config.Config, sink SinkConnector) *Runner {
	r := NewRunner(db, nil, cfg)
	r.newSink = func() SinkConnector { return sink }
	r.SetHub(testHub(t, filepath.Join(filepath.Dir(cfg.DatabasePath), "ingest-spool"), spool.Options{}))
	return r
}

func status(t *testing.T, db *database.DB, id string) string {
	t.Helper()
	p, err := db.GetPipelineByID(id)
	if err != nil || p == nil {
		t.Fatalf("load pipeline %s: %v", id, err)
	}
	return p.Status
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func (r *Runner) isRunning(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, ok := r.pipelines[id]
	return ok
}

// Two running pipelines stop within a bound on shutdown, keep status
// "running", and are resumed by the next runner.
func TestRunnerShutdownKeepsRunningAndResumes(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	a := webhookPipeline(t, db, conn, "a")
	b := webhookPipeline(t, db, conn, "b")

	r1 := newTestRunner(t, db, cfg, &fakeSink{})
	for _, id := range []string{a, b} {
		if err := r1.StartPipeline(id); err != nil {
			t.Fatal(err)
		}
	}

	done := make(chan struct{})
	go func() { r1.Stop(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not return within 5s (deadlock)")
	}
	for _, id := range []string{a, b} {
		if r1.isRunning(id) {
			t.Fatalf("%s still in the running set after Stop", id)
		}
		if got := status(t, db, id); got != "running" {
			t.Fatalf("%s status after shutdown = %q, want running", id, got)
		}
	}
	if err := r1.StartPipeline(a); !errors.Is(err, errShuttingDown) {
		t.Fatalf("start after Stop: %v, want errShuttingDown", err)
	}

	r2 := newTestRunner(t, db, cfg, &fakeSink{})
	r2.Start()
	waitFor(t, "both pipelines resumed", func() bool { return r2.isRunning(a) && r2.isRunning(b) })
	r2.Stop()
}

func TestRunnerUserStopSetsStopped(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	id := webhookPipeline(t, db, conn, "a")
	r := newTestRunner(t, db, cfg, &fakeSink{})
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	if err := r.StopPipeline(id); err != nil {
		t.Fatal(err)
	}
	if got := status(t, db, id); got != "stopped" {
		t.Fatalf("status = %q, want stopped", got)
	}
	// The slot is free right after Done, so a restart works at once.
	if err := r.StartPipeline(id); err != nil {
		t.Fatalf("restart: %v", err)
	}
	r.Stop()
}

func TestRunnerConcurrentStartLaunchesOnce(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	id := webhookPipeline(t, db, conn, "a")
	var starts atomic.Int32
	r := newTestRunner(t, db, cfg, &fakeSink{})
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, _ chan<- Batch) error {
			starts.Add(1)
			<-ctx.Done()
			return nil
		}}, nil
	}
	var wg sync.WaitGroup
	var ok atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if r.StartPipeline(id) == nil {
				ok.Add(1)
			}
		}()
	}
	wg.Wait()
	if ok.Load() != 1 {
		t.Fatalf("%d starts succeeded, want 1", ok.Load())
	}
	waitFor(t, "source start", func() bool { return starts.Load() == 1 })
	r.Stop()
	if starts.Load() != 1 {
		t.Fatalf("source started %d times", starts.Load())
	}
}

// A failed write is retried, the source is held back meanwhile, and the
// batch is acked only once, after the write succeeds.
func TestRunnerRetriesFailedWriteAndAcksAfterSuccess(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := webhookPipeline(t, db, conn, "a")
	sink := &fakeSink{fail: 4}
	r := newTestRunner(t, db, cfg, sink)

	var acks atomic.Int32
	sent := make(chan int, 10)
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, out chan<- Batch) error {
			for i := 0; i < 3; i++ {
				b := Batch{Records: []Record{{Data: map[string]interface{}{"i": i}}}, Ack: func() { acks.Add(1) }, DedupToken: "tok"}
				select {
				case out <- b:
					sent <- i
				case <-ctx.Done():
					return nil
				}
			}
			<-ctx.Done()
			return nil
		}}, nil
	}
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "all rows written", func() bool { _, rows := sink.snapshot(); return rows == 3 })
	r.Stop()
	if acks.Load() != 3 {
		t.Fatalf("acks = %d, want 3", acks.Load())
	}
	calls, _ := sink.snapshot()
	if calls != 7 {
		t.Fatalf("sink calls = %d, want 7 (4 failures + 3 writes)", calls)
	}
	p, _ := db.GetPipelineByID(id)
	if p.LastError != nil {
		t.Fatalf("last_error not cleared after recovery: %q", *p.LastError)
	}
}

// After healthThreshold failures last_error is set while the pipeline stays
// running; a cancelled retry never acks.
func TestRunnerReportsPersistentFailureWithoutAck(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := webhookPipeline(t, db, conn, "a")
	sink := &fakeSink{fail: 1 << 30}
	r := newTestRunner(t, db, cfg, sink)
	var acks atomic.Int32
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, out chan<- Batch) error {
			select {
			case out <- Batch{Records: []Record{{Data: map[string]interface{}{"a": 1}}}, Ack: func() { acks.Add(1) }}:
			case <-ctx.Done():
			}
			<-ctx.Done()
			return nil
		}}, nil
	}
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "last_error set", func() bool {
		p, _ := db.GetPipelineByID(id)
		return p.LastError != nil && p.Status == "running"
	})
	if err := r.StopPipeline(id); err != nil {
		t.Fatal(err)
	}
	if acks.Load() != 0 {
		t.Fatal("failed batch was acked")
	}
}

func TestRunnerSourcePanicFailsOnlyThatPipeline(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	id := webhookPipeline(t, db, conn, "a")
	r := newTestRunner(t, db, cfg, &fakeSink{})
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(context.Context, chan<- Batch) error { panic("boom") }}, nil
	}
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "error status", func() bool { return status(t, db, id) == "error" && !r.isRunning(id) })
	r.Stop()
}

func TestSourcesRejectZeroIntervals(t *testing.T) {
	base := map[string]map[string]interface{}{
		"source_kafka":    {"brokers": "b:9092", "topic": "t"},
		"source_webhook":  {},
		"source_database": {"db_type": "postgres", "connection_string": "x", "query": "SELECT 1"},
		"source_s3":       {"bucket": "b", "access_key": "a", "secret_key": "s"},
	}
	for typ, fields := range base {
		src, _ := NewSource(typ)
		if err := src.Validate(ConnectorConfig{NodeType: typ, Fields: fields}); err != nil {
			t.Fatalf("%s: valid config rejected: %v", typ, err)
		}
		for _, bad := range []map[string]interface{}{
			{"batch_size": float64(0)},
			{"batch_timeout_ms": float64(0)},
			{"batch_timeout_ms": float64(50)},
			{"poll_interval": float64(0)},
		} {
			f := map[string]interface{}{}
			for k, v := range fields {
				f[k] = v
			}
			for k, v := range bad {
				f[k] = v
			}
			if err := src.Validate(ConnectorConfig{NodeType: typ, Fields: f}); err == nil {
				t.Errorf("%s accepted %v", typ, bad)
			}
		}
	}
}
