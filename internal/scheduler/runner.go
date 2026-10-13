// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package scheduler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/cronexpr"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const (
	tickInterval  = 30 * time.Second
	maxConcurrent = 3
)

// Runner executes due scheduled jobs on a 30-second tick interval.
type Runner struct {
	db      *database.DB
	gateway *tunnel.Gateway
	secret  string
	pro     func() bool
	stopCh  chan struct{}
	// paused is true while the last tick found Pro work not allowed. The
	// first tick after that skips slots that fell due during the pause.
	paused bool

	// running holds the IDs of schedules with a run in flight, so a slow
	// schedule never overlaps itself while the ticker keeps starting others.
	mu      sync.Mutex
	running map[string]bool
	slots   chan struct{}
	wg      sync.WaitGroup
}

// NewRunner creates a new schedule runner. pro reports whether Pro work may
// run now (see config.ProGate); while it returns false, or is nil, ticks do
// nothing, so enabled schedules pause without being disabled or deleted and
// resume on the next tick after a license is activated. Slots that fell due
// while paused are skipped, not run late: each such schedule waits for its
// next slot.
func NewRunner(db *database.DB, gw *tunnel.Gateway, secret string, pro func() bool) *Runner {
	return &Runner{
		db:      db,
		gateway: gw,
		secret:  secret,
		pro:     pro,
		stopCh:  make(chan struct{}),
		running: make(map[string]bool),
		slots:   make(chan struct{}, maxConcurrent),
	}
}

// Start begins the runner goroutine that ticks every 30 seconds.
func (r *Runner) Start() {
	go func() {
		defer safe.Recover("scheduler")
		slog.Info("Schedule runner started", "interval", tickInterval)
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-r.stopCh:
				slog.Info("Schedule runner stopped")
				return
			case <-ticker.C:
				r.tick()
			}
		}
	}()
}

// Stop signals the runner goroutine to stop.
func (r *Runner) Stop() {
	close(r.stopCh)
}

// tick starts every due schedule that is not already running, up to
// maxConcurrent runs in flight, and returns without waiting for them. A
// schedule left out because all slots are busy stays due and starts on a
// later tick.
func (r *Runner) tick() {
	if r.pro == nil || !r.pro() {
		r.paused = true
		return
	}
	resuming := r.paused
	r.paused = false
	schedules, err := r.db.GetEnabledSchedules()
	if err != nil {
		slog.Error("Failed to load enabled schedules", "error", err)
		return
	}

	now := time.Now().UTC()
	started := 0
	for _, s := range schedules {
		if s.NextRunAt == nil {
			continue
		}
		nextRun, err := time.Parse(time.RFC3339, *s.NextRunAt)
		if err != nil {
			continue
		}
		if nextRun.After(now) {
			continue
		}
		if r.isRunning(s.ID) {
			continue
		}
		if resuming {
			r.skipMissedRun(s, now)
			continue
		}
		if !r.start(s) {
			break
		}
		started++
	}

	if started > 0 {
		slog.Info("Started due scheduled jobs", "count", started)
	}
}

func (r *Runner) isRunning(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.running[id]
}

// start runs s in the background when a slot is free; false means all slots
// are busy.
func (r *Runner) start(s database.Schedule) bool {
	select {
	case r.slots <- struct{}{}:
	default:
		return false
	}
	r.mu.Lock()
	r.running[s.ID] = true
	r.mu.Unlock()
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		defer func() {
			r.mu.Lock()
			delete(r.running, s.ID)
			r.mu.Unlock()
			<-r.slots
		}()
		defer safe.Recover("scheduler-run")
		r.runSchedule(s)
	}()
	return true
}

// wait blocks until every started run has finished (tests and shutdown).
func (r *Runner) wait() {
	r.wg.Wait()
}

// skipMissedRun moves a schedule that fell due while Pro work was paused to
// its next slot without running it.
func (r *Runner) skipMissedRun(s database.Schedule, now time.Time) {
	next := cronexpr.ComputeNextRun(s.Cron, now)
	if err := r.db.SetScheduleNextRun(s.ID, next); err != nil {
		slog.Error("Failed to skip missed schedule run", "error", err, "schedule", s.ID)
		return
	}
	slog.Info("Skipped schedule run missed while paused", "schedule", s.ID, "name", s.Name)
}

func (r *Runner) runSchedule(schedule database.Schedule) {
	// Create a run record
	runID, err := r.db.CreateScheduleRun(schedule.ID, "running")
	if err != nil {
		slog.Error("Failed to create schedule run", "error", err, "schedule", schedule.ID)
	}

	start := time.Now()
	status := "success"
	var runError string
	rowCount := 0
	connectionID := ""

	defer func() {
		elapsed := int(time.Since(start).Milliseconds())

		// Update run record
		if runID != "" {
			r.db.UpdateScheduleRun(runID, status, rowCount, elapsed, runError)
		}

		// Update schedule status
		var nextRun *time.Time
		if schedule.Enabled {
			nextRun = cronexpr.ComputeNextRun(schedule.Cron, time.Now().UTC())
		}
		r.db.UpdateScheduleStatus(schedule.ID, status, runError, nextRun)

		// Audit log
		details := fmt.Sprintf("schedule=%s status=%s elapsed=%dms", schedule.Name, status, elapsed)
		r.db.CreateAuditLog(database.AuditLogParams{
			Action:       "schedule.run",
			ConnectionID: schedule.ConnectionID,
			Details:      &details,
		})

		slog.Info("Scheduled job completed",
			"schedule", schedule.ID,
			"name", schedule.Name,
			"status", status,
			"elapsed_ms", elapsed,
		)

		if status == "error" {
			fingerprint := fmt.Sprintf("schedule:%s:error", schedule.ID)
			payload := map[string]interface{}{
				"schedule_id":   schedule.ID,
				"schedule_name": schedule.Name,
				"run_id":        runID,
				"elapsed_ms":    elapsed,
				"error":         runError,
				"row_count":     rowCount,
			}
			connPtr := nullableConnectionID(connectionID)
			if _, alertErr := r.db.CreateAlertEvent(
				connPtr,
				alerts.EventTypeScheduleFailed,
				alerts.SeverityError,
				fmt.Sprintf("Scheduled query failed: %s", schedule.Name),
				runError,
				payload,
				fingerprint,
				runID,
			); alertErr != nil {
				slog.Warn("Failed to create schedule failure alert event", "schedule", schedule.ID, "error", alertErr)
			}
		} else if status == "success" {
			threshold := slowThresholdMs(schedule.TimeoutMs)
			if elapsed >= threshold {
				fingerprint := fmt.Sprintf("schedule:%s:slow", schedule.ID)
				payload := map[string]interface{}{
					"schedule_id":       schedule.ID,
					"schedule_name":     schedule.Name,
					"run_id":            runID,
					"elapsed_ms":        elapsed,
					"slow_threshold_ms": threshold,
					"timeout_ms":        schedule.TimeoutMs,
					"row_count":         rowCount,
				}
				connPtr := nullableConnectionID(connectionID)
				if _, alertErr := r.db.CreateAlertEvent(
					connPtr,
					alerts.EventTypeScheduleSlow,
					alerts.SeverityWarn,
					fmt.Sprintf("Scheduled query slow run: %s", schedule.Name),
					fmt.Sprintf("Run took %dms (threshold %dms)", elapsed, threshold),
					payload,
					fingerprint,
					runID,
				); alertErr != nil {
					slog.Warn("Failed to create schedule slow alert event", "schedule", schedule.ID, "error", alertErr)
				}
			}
		}
	}()

	// Fetch the saved query from SQLite
	savedQuery, err := r.db.GetSavedQueryByID(schedule.SavedQueryID)
	if err != nil {
		status = "error"
		runError = fmt.Sprintf("failed to fetch saved query: %v", err)
		return
	}
	if savedQuery == nil {
		status = "error"
		runError = "saved query not found"
		return
	}

	// Determine connection ID
	if schedule.ConnectionID != nil && *schedule.ConnectionID != "" {
		connectionID = *schedule.ConnectionID
	} else if savedQuery.ConnectionID != nil && *savedQuery.ConnectionID != "" {
		connectionID = *savedQuery.ConnectionID
	}

	if connectionID == "" {
		status = "error"
		runError = "no connection ID configured for schedule or saved query"
		return
	}

	// Check that the tunnel is online
	if !r.gateway.IsTunnelOnline(connectionID) {
		status = "error"
		runError = "tunnel not connected"
		return
	}

	// Find credentials from an active session for this connection
	user, password, credErr := r.findCredentials(connectionID)
	if credErr != nil {
		status = "error"
		runError = fmt.Sprintf("no credentials available: %v", credErr)
		return
	}

	// Execute the query
	timeout := time.Duration(schedule.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	result, execErr := r.gateway.ExecuteQuery(connectionID, savedQuery.Query, user, password, timeout)
	if execErr != nil {
		status = "error"
		runError = execErr.Error()
		return
	}

	rowCount = countRows(result)
}

func nullableConnectionID(connectionID string) *string {
	if connectionID == "" {
		return nil
	}
	id := connectionID
	return &id
}

// slowThresholdMs is 80% of the schedule's effective timeout (60 s when
// unset), with a 1 s floor so very short timeouts do not flag every run.
func slowThresholdMs(timeoutMs int) int {
	if timeoutMs <= 0 {
		timeoutMs = 60000
	}
	threshold := timeoutMs * 8 / 10
	if threshold < 1000 {
		threshold = 1000
	}
	return threshold
}

// findCredentials resolves the configured background account or session fallback.
func (r *Runner) findCredentials(connectionID string) (string, string, error) {
	return r.db.BackgroundCredentials(connectionID, "schedule", r.secret)
}

// countRows counts rows in a query result.
func countRows(result *tunnel.QueryResult) int {
	if result == nil || len(result.Data) == 0 {
		return 0
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(result.Data, &rows); err != nil {
		return 0
	}
	return len(rows)
}
