package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// sourceStopTimeout bounds how long a pipeline waits for its source to return
// after cancellation before it reports itself done.
const sourceStopTimeout = 20 * time.Second

// proRecheck is how often a running pipeline with a Pro transform checks
// the license again.
var proRecheck = time.Minute

// RunningPipeline is the runtime state of a single active pipeline.
type RunningPipeline struct {
	PipelineID string
	RunID      string
	Cancel     context.CancelFunc
	Metrics    *Metrics
	StartedAt  time.Time
	Done       chan struct{}

	// specs are the transform and route nodes, shared chain first, for the
	// Pro checks. chain is the shared chain (empty for a source -> sink
	// pipeline); route, when set, splits its output between the sinks, each
	// with its own branch chain. sample keeps the last rows that entered the
	// chain for the editor's preview.
	specs  []transform.NodeSpec
	chain  *transform.Chain
	route  *transform.Route
	sinks  []*sinkPath
	sample recordRing

	// writeHealth reports sink write failures of every sink.
	writeHealth *Health

	// watch is what the ingest alert watcher needs to know about the run;
	// watchReady is set once the run's health exists, and orders the
	// fields before it for the watcher.
	watch      runWatch
	watchReady atomic.Bool

	// userStopped is set by an explicit StopPipeline; shutdown by Runner.Stop.
	// They decide the status written when the run ends.
	userStopped atomic.Bool
	shutdown    atomic.Bool
}

// Runner manages the lifecycle of all running pipelines.
type Runner struct {
	db      *database.DB
	gateway *tunnel.Gateway
	cfg     *config.Config

	mu        sync.Mutex
	pipelines map[string]*RunningPipeline
	stopping  bool

	// hub is the ingest spool the push sources drain; nil without one.
	hub *Hub

	// Connector factories; tests replace them.
	newSource func(nodeType string) (SourceConnector, error)
	newSink   func() SinkConnector
	// proAccess reports the license state for Pro transforms; tests
	// replace it.
	proAccess func() config.ProAccess

	// alerts turns the running pipelines' counters into alert events.
	alerts     *ingestWatch
	alertsStop chan struct{}
}

// NewRunner creates a new pipeline runner.
func NewRunner(db *database.DB, gw *tunnel.Gateway, cfg *config.Config) *Runner {
	r := &Runner{
		db:        db,
		gateway:   gw,
		cfg:       cfg,
		pipelines: make(map[string]*RunningPipeline),
		newSource: NewSource,
	}
	r.alerts = newIngestWatch(r)
	r.newSink = func() SinkConnector { return NewClickHouseSink(r.gateway, r.db, r.cfg.AppSecretKey) }
	r.proAccess = func() config.ProAccess {
		if r.cfg == nil {
			return config.ProNone
		}
		return r.cfg.ProAccess()
	}
	return r
}

// proAllowed reports whether Pro transforms may run: an active license or
// the read-only grace window, as for the other background Pro workers.
func (r *Runner) proAllowed() bool { return r.proAccess() != config.ProNone }

// SetHub gives the runner the ingest spool. Call it before Start.
func (r *Runner) SetHub(h *Hub) { r.hub = h }

// Hub returns the ingest hub, or nil.
func (r *Runner) Hub() *Hub {
	if r == nil {
		return nil
	}
	return r.hub
}

// DropSpool deletes a pipeline's spooled data. Called when the pipeline is
// deleted; stopping a pipeline keeps it.
func (r *Runner) DropSpool(pipelineID string) error { return r.Hub().DropStream(pipelineID) }

// Start resumes any pipelines that were in "running" status: after a crash
// and after a graceful shutdown, which leaves their status untouched.
func (r *Runner) Start() {
	r.startAlerts()
	go func() {
		pipelines, err := r.db.GetPipelinesByStatus("running")
		if err != nil {
			slog.Error("Failed to load running pipelines for recovery", "error", err)
			return
		}
		resumed := 0
		for _, p := range pipelines {
			if err := r.StartPipeline(p.ID); err != nil {
				if errors.Is(err, errShuttingDown) {
					return
				}
				slog.Error("Failed to resume pipeline", "error", err, "pipeline", p.ID)
				r.db.UpdatePipelineStatus(p.ID, "error", err.Error())
				continue
			}
			resumed++
		}
		if resumed > 0 {
			slog.Info("Pipeline runner started", "resumed", resumed)
		}
	}()
}

var errShuttingDown = errors.New("pipeline runner is shutting down")

// Stop gracefully stops all running pipelines for a server shutdown. Their
// database status stays "running" so Start resumes them on the next boot.
func (r *Runner) Stop() {
	r.mu.Lock()
	r.stopping = true
	if r.alertsStop != nil {
		close(r.alertsStop)
		r.alertsStop = nil
	}
	running := make([]*RunningPipeline, 0, len(r.pipelines))
	for _, rp := range r.pipelines {
		running = append(running, rp)
	}
	r.mu.Unlock()

	// Signal and wait without holding the lock: each pipeline removes itself
	// from the map (which needs the lock) before it closes Done.
	for _, rp := range running {
		rp.shutdown.Store(true)
		rp.Cancel()
	}

	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()
	for _, rp := range running {
		select {
		case <-rp.Done:
		case <-timer.C:
			slog.Warn("Timeout waiting for pipelines to stop", "pipeline", rp.PipelineID)
			return
		}
	}
}

// StartPipeline starts a single pipeline by ID.
func (r *Runner) StartPipeline(pipelineID string) error {
	ctx, cancel := context.WithCancel(context.Background())
	rp := &RunningPipeline{
		PipelineID: pipelineID,
		Cancel:     cancel,
		Metrics:    &Metrics{},
		StartedAt:  time.Now(),
		Done:       make(chan struct{}),
	}

	// Reserve the slot before doing any work, so two concurrent starts of
	// the same pipeline cannot both launch it.
	r.mu.Lock()
	if r.stopping {
		r.mu.Unlock()
		cancel()
		return errShuttingDown
	}
	if _, exists := r.pipelines[pipelineID]; exists {
		r.mu.Unlock()
		cancel()
		return fmt.Errorf("pipeline %s is already running", pipelineID)
	}
	r.pipelines[pipelineID] = rp
	r.mu.Unlock()

	launched := false
	defer func() {
		if !launched {
			cancel()
			r.release(rp)
			close(rp.Done)
		}
	}()

	p, err := r.prepare(pipelineID)
	if err != nil {
		return err
	}
	if err := transform.RequirePro(p.specs, !transform.NeedsPro(p.specs) || r.proAllowed()); err != nil {
		return err
	}
	rp.specs, rp.chain, rp.route, rp.sinks = p.specs, p.chain, p.route, p.sinks
	rp.watch = newRunWatch(p)

	runID, err := r.db.CreatePipelineRun(pipelineID, "running")
	if err != nil {
		return fmt.Errorf("create pipeline run: %w", err)
	}
	rp.RunID = runID
	for _, sp := range rp.sinks {
		sp.cfg.Fields[sinkRunID] = runID
		sp.cfg.Fields[sinkPipelineID] = pipelineID
	}

	r.db.UpdatePipelineStatus(pipelineID, "running", "")

	launched = true
	go r.runPipeline(ctx, rp, p.source, p.sourceCfg)

	transforms := p.chain.Len()
	for _, sp := range p.sinks {
		transforms += sp.chain.Len()
	}
	msg := "Pipeline started"
	if transforms > 0 {
		msg = fmt.Sprintf("Pipeline started with %d transform nodes", transforms)
	}
	if len(p.sinks) > 1 {
		msg += fmt.Sprintf(", routing to %d sinks", len(p.sinks))
	}
	r.db.CreatePipelineRunLog(runID, "info", msg)
	slog.Info("Pipeline started", "pipeline", pipelineID, "source", p.sourceCfg.NodeType, "transforms", transforms, "sinks", len(p.sinks), "run", runID)
	return nil
}

// preparedPipeline is a validated pipeline ready to run.
type preparedPipeline struct {
	pipeline  *database.Pipeline
	source    SourceConnector
	sourceCfg ConnectorConfig
	specs     []transform.NodeSpec
	chain     *transform.Chain
	route     *transform.Route
	sinks     []*sinkPath
}

// prepare loads, decrypts and validates a pipeline's graph and compiles its
// transform chains and route.
func (r *Runner) prepare(pipelineID string) (*preparedPipeline, error) {
	pipeline, err := r.db.GetPipelineByID(pipelineID)
	if err != nil || pipeline == nil {
		return nil, fmt.Errorf("pipeline not found: %s", pipelineID)
	}

	nodes, edges, err := r.db.GetPipelineGraph(pipelineID)
	if err != nil {
		return nil, fmt.Errorf("load pipeline graph: %w", err)
	}

	g, err := ResolveGraph(nodes, edges)
	if err != nil {
		return nil, err
	}

	sourceCfg, err := r.parseNodeConfig(g.Source)
	if err != nil {
		return nil, fmt.Errorf("parse source config: %w", err)
	}
	sourceCfg.Fields["pipeline_id"] = pipelineID
	checkpoints, err := r.db.PipelineCheckpointMap(pipelineID)
	if err != nil {
		return nil, fmt.Errorf("load checkpoints: %w", err)
	}
	sourceCfg.Checkpoints = checkpoints
	sourceCfg.Hub = r.hub

	source, err := r.newSource(sourceCfg.NodeType)
	if err != nil {
		return nil, fmt.Errorf("create source connector: %w", err)
	}
	if err := source.Validate(sourceCfg); err != nil {
		return nil, graphErr(g.Source, "%v", err)
	}
	if IsSQLiteSource(sourceCfg.NodeType, sourceCfg.Fields) {
		if err := CheckSQLitePath(stringField(sourceCfg.Fields, "connection_string", ""), r.dataDir()); err != nil {
			return nil, graphErr(g.Source, "%v", err)
		}
	}

	opts := TransformOptions(sourceCfg.NodeType, sourceCfg.Fields)
	p := &preparedPipeline{pipeline: pipeline, source: source, sourceCfg: sourceCfg}
	shared, err := transformSpecs(g.Shared)
	if err != nil {
		return nil, err
	}
	if p.chain, err = transform.Compile(shared, opts); err != nil {
		return nil, err
	}
	p.specs = shared
	if g.Route != nil {
		spec, err := transformSpecs([]database.PipelineNode{*g.Route})
		if err != nil {
			return nil, err
		}
		if p.route, err = transform.CompileRoute(spec[0], opts); err != nil {
			return nil, err
		}
		if !g.UnmatchedConnected {
			p.route.DropUnmatched()
		}
		p.specs = append(p.specs, spec...)
	}
	outputs := map[string]int{}
	if p.route != nil {
		for i, h := range p.route.Handles() {
			outputs[h] = i
		}
	}

	for _, b := range g.Branches {
		sinkCfg, err := r.parseNodeConfig(b.Sink)
		if err != nil {
			return nil, fmt.Errorf("parse sink config: %w", err)
		}
		// Inject runtime fields into the config
		sinkCfg.Fields["connection_id"] = pipeline.ConnectionID
		for _, k := range []string{sinkOTelSignal, sinkOTelTTLDays, sinkRunID, sinkPipelineID} {
			delete(sinkCfg.Fields, k)
		}
		if sourceCfg.NodeType == "source_ingest" {
			// A sink without a table layout creates OpenTelemetry tables
			// for logs and traces and ignores columns a newer exporter
			// layout may add.
			switch signal := IngestSignal(sourceCfg.Fields); signal {
			case SignalLogs, SignalTraces:
				sinkCfg.Fields[sinkOTelSignal] = signal
				sinkCfg.Fields[sinkOTelTTLDays] = intField(sourceCfg.Fields, "ttl_days", 0)
			}
		}
		sink := r.newSink()
		if err := sink.Validate(sinkCfg); err != nil {
			return nil, graphErr(b.Sink, "%v", err)
		}
		specs, err := transformSpecs(b.Chain)
		if err != nil {
			return nil, err
		}
		chain, err := transform.Compile(specs, opts)
		if err != nil {
			return nil, err
		}
		p.specs = append(p.specs, specs...)
		p.sinks = append(p.sinks, &sinkPath{
			nodeID: b.Sink.ID,
			name:   sinkName(b.Sink, sinkCfg),
			output: outputs[b.Handle],
			chain:  chain,
			sink:   sink,
			cfg:    sinkCfg,
		})
	}
	return p, nil
}

func (r *Runner) dataDir() string {
	if r.cfg == nil || r.cfg.DatabasePath == "" {
		return ""
	}
	return filepath.Dir(r.cfg.DatabasePath)
}

// release removes rp from the running set if it still owns the slot.
func (r *Runner) release(rp *RunningPipeline) {
	r.mu.Lock()
	if r.pipelines[rp.PipelineID] == rp {
		delete(r.pipelines, rp.PipelineID)
	}
	r.mu.Unlock()
}

// StopPipeline stops a running pipeline at the user's request.
func (r *Runner) StopPipeline(pipelineID string) error {
	r.mu.Lock()
	rp, exists := r.pipelines[pipelineID]
	r.mu.Unlock()

	if !exists {
		// Pipeline might not be running in-memory, just update DB status
		r.db.UpdatePipelineStatus(pipelineID, "stopped", "")
		return nil
	}

	rp.userStopped.Store(true)
	rp.Cancel()

	select {
	case <-rp.Done:
	case <-time.After(15 * time.Second):
		slog.Warn("Timeout waiting for pipeline to stop", "pipeline", pipelineID)
	}

	return nil
}

// GetRunningMetrics returns metrics for a running pipeline.
func (r *Runner) GetRunningMetrics(pipelineID string) *Metrics {
	r.mu.Lock()
	rp, exists := r.pipelines[pipelineID]
	r.mu.Unlock()
	if !exists {
		return nil
	}
	return rp.Metrics
}

// newHealth returns a Health that reports into the pipeline's last_error and
// run log.
func (r *Runner) newHealth(rp *RunningPipeline) *Health {
	return &Health{
		onReport: func(msg string) {
			r.db.SetPipelineLastError(rp.PipelineID, msg)
			r.db.CreatePipelineRunLog(rp.RunID, "error", msg)
		},
		onRecover: func() {
			r.db.SetPipelineLastError(rp.PipelineID, "")
			r.db.CreatePipelineRunLog(rp.RunID, "info", "Recovered, data is flowing again")
		},
	}
}

// runPipeline is the main execution loop for a single pipeline.
func (r *Runner) runPipeline(ctx context.Context, rp *RunningPipeline, source SourceConnector, sourceCfg ConnectorConfig) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("Pipeline finalize panicked", "pipeline", rp.PipelineID, "panic", p)
		}
		// Leave the running set before signalling Done, so a restart right
		// after Done is never refused as "already running".
		r.release(rp)
		close(rp.Done)
	}()

	sourceCfg.Health = r.newHealth(rp)
	rp.writeHealth = r.newHealth(rp)
	for _, sp := range rp.sinks {
		sp.cfg.Health = rp.writeHealth
	}
	rp.watchReady.Store(true)
	runErr := r.execute(ctx, rp, source, sourceCfg)
	r.finalize(rp, runErr)
}

// execute runs the source and writes its batches until the source ends or ctx
// is cancelled. A panic in the pipeline is returned as an error so it fails
// this pipeline only.
func (r *Runner) execute(ctx context.Context, rp *RunningPipeline, source SourceConnector, sourceCfg ConnectorConfig) (runErr error) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("Pipeline panicked", "pipeline", rp.PipelineID, "panic", p, "stack", string(debug.Stack()))
			runErr = fmt.Errorf("internal error: %v", p)
		}
	}()

	// A small buffer: while a write is being retried the source blocks on
	// send, which is the backpressure that stops it reading further.
	batchCh := make(chan Batch, 1)
	srcDone := make(chan error, 1)
	needsPro := transform.NeedsPro(rp.specs)
	proChecked := time.Now()
	var loopErr error

	go func() {
		var srcErr error
		defer func() {
			if p := recover(); p != nil {
				slog.Error("Pipeline source panicked", "pipeline", rp.PipelineID, "panic", p, "stack", string(debug.Stack()))
				srcErr = fmt.Errorf("internal error in source: %v", p)
			}
			srcDone <- srcErr
			close(batchCh)
		}()
		srcErr = source.Start(ctx, sourceCfg, batchCh)
	}()

loop:
	for {
		select {
		case <-ctx.Done():
			break loop
		case batch, ok := <-batchCh:
			if !ok {
				break loop
			}
			if needsPro && time.Since(proChecked) >= proRecheck {
				proChecked = time.Now()
				if err := transform.RequirePro(rp.specs, r.proAllowed()); err != nil {
					// The batch is not acked, so it is read again later.
					loopErr = err
					break loop
				}
			}
			rp.sample.add(batch.Records)
			r.writeWithRetry(ctx, rp, batch, rp.sinkBatches(batch))
		}
	}

	// Stop the source (it may still be running when ctx was cancelled) and
	// wait for it, so connections such as the Kafka client are closed before
	// the pipeline reports Done.
	rp.Cancel()
	select {
	case err := <-srcDone:
		if loopErr != nil {
			return loopErr
		}
		return err
	case <-time.After(sourceStopTimeout):
		slog.Warn("Pipeline source did not stop in time", "pipeline", rp.PipelineID)
		return loopErr
	}
}

// sinkAttempt is one sink's progress on a batch. It survives retries, so a
// sink that took its rows is not written again while another sink is
// retried, and rows already isolated are not split again.
type sinkAttempt struct {
	done      bool
	iso       *isolation
	rows, dlq int
}

// writeWithRetry writes one source batch to every sink (parts holds each
// sink's rows, see sinkBatches), retrying with exponential backoff until
// every sink succeeded or ctx is cancelled. The batch is acked only after
// every sink wrote its rows; an unacked batch is re-read by the source on
// its next run, with the same dedup tokens, so a sink that already had its
// rows drops the replay. A sink with no rows in the batch does no insert.
//
// When ClickHouse refuses a sink's rows because of their data (a parse or
// type error, see IsDataError) and the sink has a dead-letter table, the
// rows are split to isolate the bad ones: good rows are inserted, bad rows
// go to that sink's dead-letter table, and the sink counts as written.
func (r *Runner) writeWithRetry(ctx context.Context, rp *RunningPipeline, batch Batch, parts []Batch) {
	backoff := minBackoff
	states := make([]sinkAttempt, len(rp.sinks))
	for attempt := 1; ; attempt++ {
		var failed error
		var failedSink *sinkPath
		for i, sp := range rp.sinks {
			st := &states[i]
			if st.done {
				continue
			}
			if len(parts[i].Records) == 0 {
				st.done = true
				continue
			}
			if err := r.writeSink(ctx, rp, sp, st, parts[i]); err != nil {
				if ctx.Err() != nil {
					return
				}
				sp.stats.errors.Add(1)
				rp.Metrics.ErrorsCount.Add(1)
				if failed == nil {
					failed, failedSink = err, sp
				}
				continue
			}
			st.done = true
		}
		if failed == nil {
			r.saveCheckpoint(rp, batch)
			if batch.Ack != nil {
				batch.Ack()
			}
			rp.writeHealth.OK()
			r.recordWrite(rp, parts, states, attempt)
			return
		}
		where, msg := "ClickHouse write", fmt.Sprintf("Write batch failed, retrying (source paused): %v", failed)
		if len(rp.sinks) > 1 {
			where = "ClickHouse write to " + failedSink.name
			msg = fmt.Sprintf("Write to %s failed, retrying the batch (source paused): %v", failedSink.name, failed)
		}
		rp.writeHealth.Fail(where, failed)
		if attempt == 1 {
			r.db.CreatePipelineRunLog(rp.RunID, "error", msg)
		}
		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

// writeSink makes one attempt at writing a sink's rows.
func (r *Runner) writeSink(ctx context.Context, rp *RunningPipeline, sp *sinkPath, st *sinkAttempt, batch Batch) error {
	dls, canDLQ := sp.sink.(DeadLetterSink)
	canDLQ = canDLQ && boolField(sp.cfg.Fields, "dead_letter", true)
	if st.iso == nil {
		rows, err := sp.sink.WriteBatch(ctx, sp.cfg, batch)
		if err == nil {
			st.rows = rows
			return nil
		}
		if !canDLQ || !IsDataError(err) || ctx.Err() != nil {
			return err
		}
		st.iso = newIsolation(len(batch.Records), err)
		r.db.CreatePipelineRunLog(rp.RunID, "warn", fmt.Sprintf("ClickHouse refused a batch of %d rows because of its data; isolating the bad rows: %s", len(batch.Records), shortError(err)))
	}
	err := st.iso.run(ctx, sp.sink, dls, sp.cfg, rp.PipelineID, batch)
	st.rows, st.dlq = st.iso.rows, st.iso.dlq
	return err
}

// recordWrite counts a written batch per sink and for the pipeline.
func (r *Runner) recordWrite(rp *RunningPipeline, parts []Batch, states []sinkAttempt, attempt int) {
	now := time.Now()
	wrote := false
	for i, sp := range rp.sinks {
		part, st := parts[i], states[i]
		if len(part.Records) == 0 {
			continue // a checkpoint-only batch (e.g. an S3 file without records)
		}
		wrote = true
		sp.stats.rows.Add(int64(st.rows))
		sp.stats.batches.Add(1)
		sp.stats.lastWrite.Store(now)
		rp.Metrics.RowsIngested.Add(int64(st.rows))
		rp.Metrics.BatchesSent.Add(1)
		for _, rec := range part.Records {
			rp.Metrics.BytesIngested.Add(int64(len(rec.RawJSON)))
		}
		if st.dlq > 0 {
			rp.Metrics.DLQRows.Add(int64(st.dlq))
			r.db.AddPipelineDLQRows(rp.PipelineID, int64(st.dlq))
			dbName, table := sp.sink.(DeadLetterSink).DeadLetterTable(sp.cfg)
			r.db.CreatePipelineRunLog(rp.RunID, "warn", fmt.Sprintf("%d of %d rows were refused and written to %s.%s", st.dlq, len(part.Records), dbName, table))
		}
	}
	if !wrote {
		return
	}
	rp.Metrics.LastBatchAt.Store(now)
	if attempt > 1 {
		r.db.CreatePipelineRunLog(rp.RunID, "info", fmt.Sprintf("Batch written after %d attempts", attempt))
	}
}

// saveCheckpoint persists the batch's source position. It runs after the
// write and before Ack, in batch order. A failed save is logged: the source
// then re-reads from the previous position after a restart (at least once).
func (r *Runner) saveCheckpoint(rp *RunningPipeline, batch Batch) {
	if batch.Checkpoint == nil || batch.Checkpoint.Key == "" {
		return
	}
	if err := r.db.SavePipelineCheckpoint(rp.PipelineID, batch.Checkpoint.Key, batch.Checkpoint.Position); err != nil {
		slog.Warn("Failed to save pipeline checkpoint", "pipeline", rp.PipelineID, "error", err)
		r.db.CreatePipelineRunLog(rp.RunID, "warn", fmt.Sprintf("Could not save the source position: %v", err))
	}
}

// finalize records how a run ended.
func (r *Runner) finalize(rp *RunningPipeline, runErr error) {
	runStatus := "success"
	pipelineStatus := "success"
	errMsg := ""
	note := ""
	switch {
	case rp.userStopped.Load():
		runStatus, pipelineStatus = "stopped", "stopped"
	case rp.shutdown.Load():
		// Keep the pipeline "running" in the database so it resumes on the
		// next server start.
		runStatus, pipelineStatus = "stopped", ""
		note = "; server shutting down, the pipeline resumes on next start"
	case runErr != nil:
		runStatus, pipelineStatus = "error", "error"
		errMsg = runErr.Error()
	}

	r.db.UpdatePipelineRun(
		rp.RunID, runStatus,
		rp.Metrics.RowsIngested.Load(),
		rp.Metrics.BytesIngested.Load(),
		rp.Metrics.ErrorsCount.Load(),
		errMsg, "{}",
	)
	if pipelineStatus != "" {
		r.db.UpdatePipelineStatus(rp.PipelineID, pipelineStatus, errMsg)
	}
	level := "info"
	if runStatus == "error" {
		level = "error"
	}
	msg := fmt.Sprintf("Pipeline %s (rows: %d, errors: %d)%s", runStatus, rp.Metrics.RowsIngested.Load(), rp.Metrics.ErrorsCount.Load(), note)
	if errMsg != "" {
		msg += ": " + errMsg
	}
	r.db.CreatePipelineRunLog(rp.RunID, level, msg)

	slog.Info("Pipeline finished", "pipeline", rp.PipelineID, "status", runStatus, "rows", rp.Metrics.RowsIngested.Load())
}

// Helper functions

func isSourceType(nodeType string) bool {
	switch nodeType {
	case "source_kafka", "source_webhook", "source_database", "source_s3", "source_ingest":
		return true
	}
	return false
}

// parseNodeConfig unmarshals a node's stored config and decrypts its secrets.
func (r *Runner) parseNodeConfig(node *database.PipelineNode) (ConnectorConfig, error) {
	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(node.ConfigEncrypted), &fields); err != nil {
		return ConnectorConfig{}, fmt.Errorf("unmarshal node config: %w", err)
	}
	if fields == nil {
		fields = map[string]interface{}{}
	}
	key := ""
	if r.cfg != nil {
		key = r.cfg.AppSecretKey
	}
	fields, err := DecryptConfigSecrets(fields, key)
	if err != nil {
		return ConnectorConfig{}, err
	}
	return ConnectorConfig{
		NodeType: node.NodeType,
		Fields:   fields,
	}, nil
}
