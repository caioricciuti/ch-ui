package models

import (
	"log/slog"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/scheduler"
)

const modelTickInterval = 30 * time.Second

// Scheduler checks for due model schedules and triggers RunAll.
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

// Start begins the scheduler goroutine.
func (s *Scheduler) Start() {
	go func() {
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
	schedules, err := s.db.GetEnabledModelSchedules()
	if err != nil {
		slog.Error("Failed to load enabled model schedules", "error", err)
		return
	}

	now := time.Now().UTC()
	for _, sched := range schedules {
		if sched.NextRunAt == nil || sched.AnchorModelID == nil {
			continue
		}
		nextRun, err := time.Parse(time.RFC3339, *sched.NextRunAt)
		if err != nil {
			continue
		}
		if nextRun.After(now) {
			continue
		}

		slog.Info("Model schedule triggered",
			"connection_id", sched.ConnectionID,
			"anchor_model_id", *sched.AnchorModelID,
			"cron", sched.Cron)

		status := "success"
		var runError string

		_, runErr := s.runner.RunPipeline(sched.ConnectionID, *sched.AnchorModelID, "scheduler")
		if runErr != nil {
			status = "error"
			runError = runErr.Error()
			slog.Error("Scheduled model run failed",
				"connection_id", sched.ConnectionID,
				"anchor_model_id", *sched.AnchorModelID,
				"error", runErr)
		}

		// Compute next run and update status by schedule ID
		var nextRunAt *string
		if next := scheduler.ComputeNextRun(sched.Cron, time.Now().UTC()); next != nil {
			formatted := next.Format(time.RFC3339)
			nextRunAt = &formatted
		}

		if err := s.db.UpdateModelScheduleStatusByID(sched.ID, status, runError, nextRunAt); err != nil {
			slog.Error("Failed to update model schedule status", "schedule_id", sched.ID, "error", err)
		}
	}
}
