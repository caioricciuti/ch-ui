package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// RunningPipeline is the runtime state of a single active pipeline.
type RunningPipeline struct {
	PipelineID string
	RunID      string
	Cancel     context.CancelFunc
	Metrics    *Metrics
	StartedAt  time.Time
	Done       chan struct{}
}

// Runner manages the lifecycle of all running pipelines.
type Runner struct {
	db      *database.DB
	gateway *tunnel.Gateway
	cfg     *config.Config

	mu        sync.RWMutex
	pipelines map[string]*RunningPipeline
	stopCh    chan struct{}
}

// NewRunner creates a new pipeline runner.
func NewRunner(db *database.DB, gw *tunnel.Gateway, cfg *config.Config) *Runner {
	return &Runner{
		db:        db,
		gateway:   gw,
		cfg:       cfg,
		pipelines: make(map[string]*RunningPipeline),
		stopCh:    make(chan struct{}),
	}
}

// Start resumes any pipelines that were in "running" status (crash recovery).
func (r *Runner) Start() {
	go func() {
		pipelines, err := r.db.GetPipelinesByStatus("running")
		if err != nil {
			slog.Error("Failed to load running pipelines for recovery", "error", err)
			return
		}
		for _, p := range pipelines {
			if err := r.StartPipeline(p.ID); err != nil {
				slog.Error("Failed to resume pipeline", "error", err, "pipeline", p.ID)
				r.db.UpdatePipelineStatus(p.ID, "error", err.Error())
			}
		}
		if len(pipelines) > 0 {
			slog.Info("Pipeline runner started", "resumed", len(pipelines))
		}
	}()
}

// Stop gracefully stops all running pipelines.
func (r *Runner) Stop() {
	close(r.stopCh)
	r.mu.RLock()
	for _, rp := range r.pipelines {
		rp.Cancel()
	}
	r.mu.RUnlock()

	// Wait for all to finish with timeout
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	r.mu.RLock()
	for _, rp := range r.pipelines {
		select {
		case <-rp.Done:
		case <-timer.C:
			slog.Warn("Timeout waiting for pipeline to stop", "pipeline", rp.PipelineID)
		}
	}
	r.mu.RUnlock()
}

// StartPipeline starts a single pipeline by ID.
func (r *Runner) StartPipeline(pipelineID string) error {
	r.mu.Lock()
	if _, exists := r.pipelines[pipelineID]; exists {
		r.mu.Unlock()
		return fmt.Errorf("pipeline %s is already running", pipelineID)
	}
	r.mu.Unlock()

	// Load pipeline + graph from DB
	pipeline, err := r.db.GetPipelineByID(pipelineID)
	if err != nil || pipeline == nil {
		return fmt.Errorf("pipeline not found: %s", pipelineID)
	}

	nodes, edges, err := r.db.GetPipelineGraph(pipelineID)
	if err != nil {
		return fmt.Errorf("load pipeline graph: %w", err)
	}

	// Find source and sink nodes
	var sourceNode *database.PipelineNode
	var sinkNode *database.PipelineNode
	for i := range nodes {
		switch {
		case isSourceType(nodes[i].NodeType):
			if sourceNode != nil {
				return fmt.Errorf("pipeline has multiple source nodes")
			}
			sourceNode = &nodes[i]
		case nodes[i].NodeType == "sink_clickhouse":
			if sinkNode != nil {
				return fmt.Errorf("pipeline has multiple sink nodes")
			}
			sinkNode = &nodes[i]
		}
	}

	if sourceNode == nil {
		return fmt.Errorf("pipeline has no source node")
	}
	if sinkNode == nil {
		return fmt.Errorf("pipeline has no sink node")
	}

	// Validate that source connects to sink
	connected := false
	for _, e := range edges {
		if e.SourceNodeID == sourceNode.ID && e.TargetNodeID == sinkNode.ID {
			connected = true
			break
		}
	}
	if !connected {
		return fmt.Errorf("source node is not connected to sink node")
	}

	// Parse node configs
	sourceCfg, err := parseNodeConfig(sourceNode)
	if err != nil {
		return fmt.Errorf("parse source config: %w", err)
	}
	sinkCfg, err := parseNodeConfig(sinkNode)
	if err != nil {
		return fmt.Errorf("parse sink config: %w", err)
	}

	// Inject runtime fields into configs
	sinkCfg.Fields["connection_id"] = pipeline.ConnectionID
	sourceCfg.Fields["pipeline_id"] = pipelineID

	// Instantiate connectors
	source, err := NewSource(sourceCfg.NodeType)
	if err != nil {
		return fmt.Errorf("create source connector: %w", err)
	}
	sink := NewClickHouseSink(r.gateway, r.db, r.cfg.AppSecretKey)

	// Validate configs
	if err := source.Validate(sourceCfg); err != nil {
		return fmt.Errorf("validate source config: %w", err)
	}
	if err := sink.Validate(sinkCfg); err != nil {
		return fmt.Errorf("validate sink config: %w", err)
	}

	// Create run record
	runID, err := r.db.CreatePipelineRun(pipelineID, "running")
	if err != nil {
		return fmt.Errorf("create pipeline run: %w", err)
	}

	// Update pipeline status
	r.db.UpdatePipelineStatus(pipelineID, "running", "")

	// Launch goroutine
	ctx, cancel := context.WithCancel(context.Background())
	metrics := &Metrics{}
	done := make(chan struct{})

	rp := &RunningPipeline{
		PipelineID: pipelineID,
		RunID:      runID,
		Cancel:     cancel,
		Metrics:    metrics,
		StartedAt:  time.Now(),
		Done:       done,
	}

	r.mu.Lock()
	r.pipelines[pipelineID] = rp
	r.mu.Unlock()

	go r.runPipeline(ctx, rp, source, sink, sourceCfg, sinkCfg, pipeline.ConnectionID)

	r.db.CreatePipelineRunLog(runID, "info", "Pipeline started")
	slog.Info("Pipeline started", "pipeline", pipelineID, "source", sourceCfg.NodeType, "run", runID)
	return nil
}

// StopPipeline stops a running pipeline.
func (r *Runner) StopPipeline(pipelineID string) error {
	r.mu.RLock()
	rp, exists := r.pipelines[pipelineID]
	r.mu.RUnlock()

	if !exists {
		// Pipeline might not be running in-memory, just update DB status
		r.db.UpdatePipelineStatus(pipelineID, "stopped", "")
		return nil
	}

	rp.Cancel()

	// Wait for goroutine to finish with timeout
	select {
	case <-rp.Done:
	case <-time.After(15 * time.Second):
		slog.Warn("Timeout waiting for pipeline to stop", "pipeline", pipelineID)
	}

	return nil
}

// GetRunningMetrics returns metrics for a running pipeline.
func (r *Runner) GetRunningMetrics(pipelineID string) *Metrics {
	r.mu.RLock()
	rp, exists := r.pipelines[pipelineID]
	r.mu.RUnlock()
	if !exists {
		return nil
	}
	return rp.Metrics
}

// runPipeline is the main execution loop for a single pipeline.
func (r *Runner) runPipeline(ctx context.Context, rp *RunningPipeline, source SourceConnector, sink SinkConnector, sourceCfg, sinkCfg ConnectorConfig, connectionID string) {
	defer close(rp.Done)
	defer func() {
		r.mu.Lock()
		delete(r.pipelines, rp.PipelineID)
		r.mu.Unlock()
	}()

	batchCh := make(chan Batch, 10)
	var sourceErr error

	// Start source in a goroutine
	go func() {
		sourceErr = source.Start(ctx, sourceCfg, batchCh)
		close(batchCh)
	}()

	// Consume batches and write to sink
	for batch := range batchCh {
		select {
		case <-ctx.Done():
			goto done
		default:
		}

		rows, err := sink.WriteBatch(ctx, sinkCfg, batch)
		if err != nil {
			rp.Metrics.ErrorsCount.Add(1)
			r.db.CreatePipelineRunLog(rp.RunID, "error", fmt.Sprintf("Write batch failed: %v", err))
			slog.Error("Pipeline batch write failed", "pipeline", rp.PipelineID, "error", err)
			continue
		}

		rp.Metrics.RowsIngested.Add(int64(rows))
		rp.Metrics.BatchesSent.Add(1)
		rp.Metrics.LastBatchAt.Store(time.Now())

		// Estimate bytes from raw JSON
		for _, rec := range batch.Records {
			rp.Metrics.BytesIngested.Add(int64(len(rec.RawJSON)))
		}
	}

done:
	// Finalize
	status := "success"
	errMsg := ""
	if sourceErr != nil && ctx.Err() == nil {
		status = "error"
		errMsg = sourceErr.Error()
	} else if ctx.Err() != nil {
		status = "stopped"
	}

	r.db.UpdatePipelineRun(
		rp.RunID, status,
		rp.Metrics.RowsIngested.Load(),
		rp.Metrics.BytesIngested.Load(),
		rp.Metrics.ErrorsCount.Load(),
		errMsg, "{}",
	)
	r.db.UpdatePipelineStatus(rp.PipelineID, status, errMsg)
	r.db.CreatePipelineRunLog(rp.RunID, "info", fmt.Sprintf("Pipeline %s (rows: %d, errors: %d)", status, rp.Metrics.RowsIngested.Load(), rp.Metrics.ErrorsCount.Load()))

	slog.Info("Pipeline finished", "pipeline", rp.PipelineID, "status", status, "rows", rp.Metrics.RowsIngested.Load())
}

// Helper functions

func isSourceType(nodeType string) bool {
	switch nodeType {
	case "source_kafka", "source_webhook", "source_database", "source_s3":
		return true
	}
	return false
}

func parseNodeConfig(node *database.PipelineNode) (ConnectorConfig, error) {
	var fields map[string]interface{}
	if err := json.Unmarshal([]byte(node.ConfigEncrypted), &fields); err != nil {
		return ConnectorConfig{}, fmt.Errorf("unmarshal node config: %w", err)
	}
	return ConnectorConfig{
		NodeType: node.NodeType,
		Fields:   fields,
	}, nil
}
