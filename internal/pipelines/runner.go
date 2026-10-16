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
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// sourceStopTimeout bounds how long a pipeline waits for its source to return
// after cancellation before it reports itself done.
const sourceStopTimeout = 20 * time.Second

// RunningPipeline is the runtime state of a single active pipeline.
type RunningPipeline struct {
	PipelineID string
	RunID      string
	Cancel     context.CancelFunc
	Metrics    *Metrics
	StartedAt  time.Time
	Done       chan struct{}

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
	r.newSink = func() SinkConnector { return NewClickHouseSink(r.gateway, r.db, r.cfg.AppSecretKey) }
	return r
}

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

	source, sink, sourceCfg, sinkCfg, err := r.prepare(pipelineID)
	if err != nil {
		return err
	}

	runID, err := r.db.CreatePipelineRun(pipelineID, "running")
	if err != nil {
		return fmt.Errorf("create pipeline run: %w", err)
	}
	rp.RunID = runID

	r.db.UpdatePipelineStatus(pipelineID, "running", "")

	launched = true
	go r.runPipeline(ctx, rp, source, sink, sourceCfg, sinkCfg)

	r.db.CreatePipelineRunLog(runID, "info", "Pipeline started")
	slog.Info("Pipeline started", "pipeline", pipelineID, "source", sourceCfg.NodeType, "run", runID)
	return nil
}

// prepare loads, decrypts and validates a pipeline's graph.
func (r *Runner) prepare(pipelineID string) (SourceConnector, SinkConnector, ConnectorConfig, ConnectorConfig, error) {
	var none ConnectorConfig
	pipeline, err := r.db.GetPipelineByID(pipelineID)
	if err != nil || pipeline == nil {
		return nil, nil, none, none, fmt.Errorf("pipeline not found: %s", pipelineID)
	}

	nodes, edges, err := r.db.GetPipelineGraph(pipelineID)
	if err != nil {
		return nil, nil, none, none, fmt.Errorf("load pipeline graph: %w", err)
	}

	sourceNode, sinkNode, err := FindSourceAndSink(nodes)
	if err != nil {
		return nil, nil, none, none, err
	}

	connected := false
	for _, e := range edges {
		if e.SourceNodeID == sourceNode.ID && e.TargetNodeID == sinkNode.ID {
			connected = true
			break
		}
	}
	if !connected {
		return nil, nil, none, none, fmt.Errorf("source node is not connected to sink node")
	}

	sourceCfg, err := r.parseNodeConfig(sourceNode)
	if err != nil {
		return nil, nil, none, none, fmt.Errorf("parse source config: %w", err)
	}
	sinkCfg, err := r.parseNodeConfig(sinkNode)
	if err != nil {
		return nil, nil, none, none, fmt.Errorf("parse sink config: %w", err)
	}

	// Inject runtime fields into configs
	sinkCfg.Fields["connection_id"] = pipeline.ConnectionID
	sourceCfg.Fields["pipeline_id"] = pipelineID
	checkpoints, err := r.db.PipelineCheckpointMap(pipelineID)
	if err != nil {
		return nil, nil, none, none, fmt.Errorf("load checkpoints: %w", err)
	}
	sourceCfg.Checkpoints = checkpoints
	sourceCfg.Hub = r.hub
	delete(sinkCfg.Fields, sinkOTelSignal)
	delete(sinkCfg.Fields, sinkOTelTTLDays)
	if sourceCfg.NodeType == "source_ingest" {
		// The sink creates OpenTelemetry tables for logs and traces and
		// ignores columns a newer exporter layout may add.
		switch signal := IngestSignal(sourceCfg.Fields); signal {
		case SignalLogs, SignalTraces:
			sinkCfg.Fields[sinkOTelSignal] = signal
			sinkCfg.Fields[sinkOTelTTLDays] = intField(sourceCfg.Fields, "ttl_days", 0)
		}
	}

	source, err := r.newSource(sourceCfg.NodeType)
	if err != nil {
		return nil, nil, none, none, fmt.Errorf("create source connector: %w", err)
	}
	sink := r.newSink()

	if err := source.Validate(sourceCfg); err != nil {
		return nil, nil, none, none, fmt.Errorf("validate source config: %w", err)
	}
	if err := sink.Validate(sinkCfg); err != nil {
		return nil, nil, none, none, fmt.Errorf("validate sink config: %w", err)
	}
	if IsSQLiteSource(sourceCfg.NodeType, sourceCfg.Fields) {
		if err := CheckSQLitePath(stringField(sourceCfg.Fields, "connection_string", ""), r.dataDir()); err != nil {
			return nil, nil, none, none, err
		}
	}
	return source, sink, sourceCfg, sinkCfg, nil
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
func (r *Runner) runPipeline(ctx context.Context, rp *RunningPipeline, source SourceConnector, sink SinkConnector, sourceCfg, sinkCfg ConnectorConfig) {
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
	sinkCfg.Health = r.newHealth(rp)
	runErr := r.execute(ctx, rp, source, sink, sourceCfg, sinkCfg)
	r.finalize(rp, runErr)
}

// execute runs the source and writes its batches until the source ends or ctx
// is cancelled. A panic in the pipeline is returned as an error so it fails
// this pipeline only.
func (r *Runner) execute(ctx context.Context, rp *RunningPipeline, source SourceConnector, sink SinkConnector, sourceCfg, sinkCfg ConnectorConfig) (runErr error) {
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
			r.writeWithRetry(ctx, rp, sink, sinkCfg, batch)
		}
	}

	// Stop the source (it may still be running when ctx was cancelled) and
	// wait for it, so connections such as the Kafka client are closed before
	// the pipeline reports Done.
	rp.Cancel()
	select {
	case err := <-srcDone:
		return err
	case <-time.After(sourceStopTimeout):
		slog.Warn("Pipeline source did not stop in time", "pipeline", rp.PipelineID)
		return nil
	}
}

// writeWithRetry writes one batch, retrying with exponential backoff until it
// succeeds or ctx is cancelled. The batch is acked only after a successful
// write; an unacked batch is re-read by the source on its next run.
//
// When ClickHouse refuses the batch because of its data (a parse or type
// error, see IsDataError) and the sink has a dead-letter table, the batch is
// split to isolate the bad rows: good rows are inserted, bad rows go to the
// dead-letter table, and the batch counts as written.
func (r *Runner) writeWithRetry(ctx context.Context, rp *RunningPipeline, sink SinkConnector, cfg ConnectorConfig, batch Batch) {
	backoff := minBackoff
	dls, canDLQ := sink.(DeadLetterSink)
	canDLQ = canDLQ && boolField(cfg.Fields, "dead_letter", true)
	var iso *isolation
	for attempt := 1; ; attempt++ {
		rows, dlq, err := 0, 0, error(nil)
		if iso == nil {
			rows, err = sink.WriteBatch(ctx, cfg, batch)
			if err != nil && canDLQ && len(batch.Records) > 0 && IsDataError(err) && ctx.Err() == nil {
				iso = newIsolation(len(batch.Records), err)
				r.db.CreatePipelineRunLog(rp.RunID, "warn", fmt.Sprintf("ClickHouse refused a batch of %d rows because of its data; isolating the bad rows: %s", len(batch.Records), shortError(err)))
			}
		}
		if iso != nil {
			err = iso.run(ctx, sink, dls, cfg, rp.PipelineID, batch)
			rows, dlq = iso.rows, iso.dlq
		}
		if err == nil {
			r.saveCheckpoint(rp, batch)
			if batch.Ack != nil {
				batch.Ack()
			}
			cfg.Health.OK()
			if len(batch.Records) == 0 {
				return // a checkpoint-only batch (e.g. an S3 file without records)
			}
			rp.Metrics.RowsIngested.Add(int64(rows))
			rp.Metrics.BatchesSent.Add(1)
			rp.Metrics.LastBatchAt.Store(time.Now())
			for _, rec := range batch.Records {
				rp.Metrics.BytesIngested.Add(int64(len(rec.RawJSON)))
			}
			if dlq > 0 {
				rp.Metrics.DLQRows.Add(int64(dlq))
				r.db.AddPipelineDLQRows(rp.PipelineID, int64(dlq))
				dbName, table := dls.DeadLetterTable(cfg)
				r.db.CreatePipelineRunLog(rp.RunID, "warn", fmt.Sprintf("%d of %d rows were refused and written to %s.%s", dlq, len(batch.Records), dbName, table))
			}
			if attempt > 1 {
				r.db.CreatePipelineRunLog(rp.RunID, "info", fmt.Sprintf("Batch written after %d attempts", attempt))
			}
			return
		}
		if ctx.Err() != nil {
			return
		}
		rp.Metrics.ErrorsCount.Add(1)
		cfg.Health.Fail("ClickHouse write", err)
		if attempt == 1 {
			r.db.CreatePipelineRunLog(rp.RunID, "error", fmt.Sprintf("Write batch failed, retrying (source paused): %v", err))
		}
		if !sleepCtx(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
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

// FindSourceAndSink returns the single source and sink node of a graph.
func FindSourceAndSink(nodes []database.PipelineNode) (*database.PipelineNode, *database.PipelineNode, error) {
	var sourceNode, sinkNode *database.PipelineNode
	for i := range nodes {
		switch {
		case isSourceType(nodes[i].NodeType):
			if sourceNode != nil {
				return nil, nil, fmt.Errorf("pipeline has multiple source nodes")
			}
			sourceNode = &nodes[i]
		case nodes[i].NodeType == "sink_clickhouse":
			if sinkNode != nil {
				return nil, nil, fmt.Errorf("pipeline has multiple sink nodes")
			}
			sinkNode = &nodes[i]
		}
	}
	if sourceNode == nil {
		return nil, nil, fmt.Errorf("pipeline has no source node")
	}
	if sinkNode == nil {
		return nil, nil, fmt.Errorf("pipeline has no sink node")
	}
	return sourceNode, sinkNode, nil
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
