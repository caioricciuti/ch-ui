package models

import (
	"log/slog"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

const modelTickInterval = 30 * time.Second

// InterruptedRunMessage is the error recorded on runs that were still running
// when the process stopped.
const InterruptedRunMessage = "interrupted by restart"

// Scheduler checks for due model pipelines and runs their selectors.
type Scheduler struct {
	db     *database.DB
	runner *Runner
	stopCh chan struct{}
}

// NewScheduler creates a new model scheduler.
func NewScheduler(db *database.DB, runner *Runner) *Scheduler {
	return &Scheduler{
		db:     db,
		runner: runner,
		stopCh: make(chan struct{}),
	}
}

// Start marks runs a previous process left in "running" as failed, copies
// legacy per-anchor schedules into named pipelines, then begins the scheduler
// goroutine.
func (s *Scheduler) Start() {
	if n, err := MigrateLegacySchedules(s.db); err != nil {
		slog.Error("Failed to copy model schedules into pipelines", "error", err)
	} else if n > 0 {
		slog.Info("Copied model schedules into named pipelines", "pipelines", n)
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

func (s *Scheduler) tick() {
	pipelines, err := s.db.GetEnabledModelPipelines()
	if err != nil {
		slog.Error("Failed to load scheduled model pipelines", "error", err)
		return
	}

	now := time.Now().UTC()
	for _, p := range pipelines {
		if p.NextRunAt == nil {
			continue
		}
		nextRun, err := time.Parse(time.RFC3339, *p.NextRunAt)
		if err != nil || nextRun.After(now) {
			continue
		}

		slog.Info("Model pipeline schedule triggered",
			"connection_id", p.ConnectionID, "pipeline", p.Name, "selector", p.Selector, "cron", p.Cron)

		status := "success"
		var runError string
		// Run returns an error (a *RunFailedError, with the run recorded)
		// when any model failed or was skipped, so a run with failures is
		// never recorded as a success here.
		runID, runErr := s.runner.Run(p.ConnectionID, RunOptions{Selector: p.Selector, PipelineID: p.ID, TriggeredBy: "scheduler"})
		if runErr != nil {
			status = "error"
			runError = runErr.Error()
			slog.Error("Scheduled model pipeline failed", "connection_id", p.ConnectionID, "pipeline", p.Name, "error", runErr)
		}

		var nextRunAt *string
		if next := NextRuns(p.Cron, p.Timezone, time.Now(), 1); len(next) > 0 {
			formatted := next[0].Format(time.RFC3339)
			nextRunAt = &formatted
		}
		if err := s.db.RecordModelPipelineRun(p.ID, status, runError, runID, nextRunAt); err != nil {
			slog.Error("Failed to record model pipeline run", "pipeline_id", p.ID, "error", err)
		}
	}
}
