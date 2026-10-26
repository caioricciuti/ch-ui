package models

import (
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

const modelTickInterval = 30 * time.Second

// SchedulerTrigger is the TriggeredBy of runs the scheduler starts.
const SchedulerTrigger = "scheduler"

// InterruptedRunMessage is the error recorded on runs that were still running
// when the process stopped.
const InterruptedRunMessage = "interrupted by restart"

// Scheduler checks for due model pipelines and runs their selectors.
type Scheduler struct {
	db     *database.DB
	runner *Runner
	stopCh chan struct{}

	// busy holds the connections with scheduled groups in flight.
	mu   sync.Mutex
	busy map[string]bool
	wg   sync.WaitGroup
}

// NewScheduler creates a new model scheduler.
func NewScheduler(db *database.DB, runner *Runner) *Scheduler {
	return &Scheduler{
		db:     db,
		runner: runner,
		stopCh: make(chan struct{}),
		busy:   make(map[string]bool),
	}
}

// Start marks runs a previous process left in "running" as failed, copies
// legacy per-anchor schedules into named pipelines, then begins the scheduler
// goroutine.
func (s *Scheduler) Start() {
	if n, err := MigrateLegacySchedules(s.db); err != nil {
		slog.Error("Failed to copy model schedules into model groups", "error", err)
	} else if n > 0 {
		slog.Info("Copied model schedules into model groups", "pipelines", n)
	}
	if n, err := s.db.MarkInterruptedModelRuns(InterruptedRunMessage); err != nil {
		slog.Error("Failed to close interrupted model runs", "error", err)
	} else if n > 0 {
		slog.Warn("Marked interrupted model runs as failed", "runs", n)
	}
	go func() {
		defer safe.Recover("model-scheduler")
		slog.Info("Model scheduler started", "interval", modelTickInterval)
		ticker := time.NewTicker(modelTickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-s.stopCh:
				slog.Info("Model scheduler stopped")
				return
			case <-ticker.C:
				s.tick()
			}
		}
	}()
}

// Stop signals the scheduler goroutine to stop.
func (s *Scheduler) Stop() {
	close(s.stopCh)
}

// tick starts the due groups and returns without waiting for them. Groups
// of one connection run one after another in a single goroutine, and
// different connections run side by side, so a long run on one ClickHouse
// server never delays the groups of another. A connection whose previous
// groups are still running is skipped: its due groups stay due and start
// on a later tick.
func (s *Scheduler) tick() {
	pipelines, err := s.db.GetEnabledModelPipelines()
	if err != nil {
		slog.Error("Failed to load scheduled model groups", "error", err)
		return
	}

	now := time.Now().UTC()
	due := map[string][]database.ModelPipeline{}
	var order []string
	for _, p := range pipelines {
		if p.NextRunAt == nil {
			continue
		}
		nextRun, err := time.Parse(time.RFC3339, *p.NextRunAt)
		if err != nil || nextRun.After(now) {
			continue
		}
		if _, ok := due[p.ConnectionID]; !ok {
			order = append(order, p.ConnectionID)
		}
		due[p.ConnectionID] = append(due[p.ConnectionID], p)
	}
	for _, connID := range order {
		s.start(connID, due[connID])
	}
}

// start runs the due groups of one connection in the background unless
// that connection already has scheduled groups running.
func (s *Scheduler) start(connID string, due []database.ModelPipeline) {
	s.mu.Lock()
	if s.busy[connID] {
		s.mu.Unlock()
		slog.Debug("Model groups still running on connection, skipping", "connection_id", connID, "due", len(due))
		return
	}
	s.busy[connID] = true
	s.mu.Unlock()
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer func() {
			s.mu.Lock()
			delete(s.busy, connID)
			s.mu.Unlock()
		}()
		defer safe.Recover("model-scheduler-run")
		for _, p := range due {
			s.runPipeline(p)
		}
	}()
}

// wait blocks until every started run has finished (tests).
func (s *Scheduler) wait() {
	s.wg.Wait()
}

func (s *Scheduler) runPipeline(p database.ModelPipeline) {
	slog.Info("Model group schedule triggered",
		"connection_id", p.ConnectionID, "pipeline", p.Name, "selector", p.Selector, "cron", p.Cron)

	status := "success"
	var runError string
	// Run returns an error (a *RunFailedError, with the run recorded)
	// when any model failed or was skipped, so a run with failures is
	// never recorded as a success here.
	runID, runErr := s.runner.Run(p.ConnectionID, RunOptions{Selector: p.Selector, PipelineID: p.ID, TriggeredBy: SchedulerTrigger})
	if runErr != nil {
		status = "error"
		runError = runErr.Error()
		slog.Error("Scheduled model group failed", "connection_id", p.ConnectionID, "pipeline", p.Name, "error", runErr)
	}

	var nextRunAt *string
	if next := NextRuns(p.Cron, p.Timezone, time.Now(), 1); len(next) > 0 {
		formatted := next[0].Format(time.RFC3339)
		nextRunAt = &formatted
	}
	if err := s.db.RecordModelPipelineRun(p.ID, status, runError, runID, nextRunAt); err != nil {
		slog.Error("Failed to record model group run", "pipeline_id", p.ID, "error", err)
	}
}
