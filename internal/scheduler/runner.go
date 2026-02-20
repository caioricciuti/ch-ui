package scheduler

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
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
	stopCh  chan struct{}
}

// NewRunner creates a new schedule runner.
func NewRunner(db *database.DB, gw *tunnel.Gateway, secret string) *Runner {
	return &Runner{
		db:      db,
		gateway: gw,
		secret:  secret,
		stopCh:  make(chan struct{}),
	}
}

// Start begins the runner goroutine that ticks every 30 seconds.
func (r *Runner) Start() {
	go func() {
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

// tick fetches due jobs from SQLite and executes them concurrently.
func (r *Runner) tick() {
	schedules, err := r.db.GetEnabledSchedules()
	if err != nil {
		slog.Error("Failed to load enabled schedules", "error", err)
		return
	}

	now := time.Now().UTC()
	var due []database.Schedule
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
		due = append(due, s)
	}

	if len(due) == 0 {
		return
	}

	slog.Info("Processing due scheduled jobs", "count", len(due))

	sem := make(chan struct{}, maxConcurrent)
	var wg sync.WaitGroup

	for _, schedule := range due {
		wg.Add(1)
		sem <- struct{}{}

		go func(s database.Schedule) {
			defer wg.Done()
			defer func() { <-sem }()
			r.runSchedule(s)
		}(schedule)
	}

	wg.Wait()
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
			nextRun = ComputeNextRun(schedule.Cron, time.Now().UTC())
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
			threshold := int(float64(maxInt(schedule.TimeoutMs, 60000)) * 0.8)
			if threshold < 5000 {
				threshold = 5000
			}
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

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// findCredentials looks for active session credentials for a connection.
func (r *Runner) findCredentials(connectionID string) (string, string, error) {
	sessions, err := r.db.GetActiveSessionsByConnection(connectionID, 3)
	if err != nil {
		return "", "", fmt.Errorf("failed to load sessions: %w", err)
	}

	for _, s := range sessions {
		password, err := crypto.Decrypt(s.EncryptedPassword, r.secret)
		if err != nil {
			continue
		}
		return s.ClickhouseUser, password, nil
	}

	return "", "", fmt.Errorf("no active sessions with valid credentials for connection %s", connectionID)
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
