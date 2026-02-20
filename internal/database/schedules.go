package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Schedule represents a scheduled query.
type Schedule struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	SavedQueryID string  `json:"saved_query_id"`
	ConnectionID *string `json:"connection_id"`
	Cron         string  `json:"cron"`
	Timezone     string  `json:"timezone"`
	Enabled      bool    `json:"enabled"`
	TimeoutMs    int     `json:"timeout_ms"`
	LastRunAt    *string `json:"last_run_at"`
	NextRunAt    *string `json:"next_run_at"`
	LastStatus   *string `json:"last_status"`
	LastError    *string `json:"last_error"`
	CreatedBy    *string `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

// ScheduleRun represents a single execution of a scheduled query.
type ScheduleRun struct {
	ID           string  `json:"id"`
	ScheduleID   string  `json:"schedule_id"`
	StartedAt    string  `json:"started_at"`
	FinishedAt   *string `json:"finished_at"`
	Status       string  `json:"status"`
	RowsAffected int     `json:"rows_affected"`
	ElapsedMs    int     `json:"elapsed_ms"`
	Error        *string `json:"error"`
	CreatedAt    string  `json:"created_at"`
}

// GetSchedules retrieves all schedules.
func (db *DB) GetSchedules() ([]Schedule, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, saved_query_id, connection_id, cron, timezone, enabled, timeout_ms,
		        last_run_at, next_run_at, last_status, last_error, created_by, created_at, updated_at
		 FROM schedules ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("get schedules: %w", err)
	}
	defer rows.Close()

	var schedules []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedule rows: %w", err)
	}
	return schedules, nil
}

// GetEnabledSchedules retrieves all enabled schedules.
func (db *DB) GetEnabledSchedules() ([]Schedule, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, saved_query_id, connection_id, cron, timezone, enabled, timeout_ms,
		        last_run_at, next_run_at, last_status, last_error, created_by, created_at, updated_at
		 FROM schedules WHERE enabled = 1 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("get enabled schedules: %w", err)
	}
	defer rows.Close()

	var schedules []Schedule
	for rows.Next() {
		s, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		schedules = append(schedules, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled schedule rows: %w", err)
	}
	return schedules, nil
}

// GetScheduleByID retrieves a schedule by ID.
func (db *DB) GetScheduleByID(id string) (*Schedule, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, saved_query_id, connection_id, cron, timezone, enabled, timeout_ms,
		        last_run_at, next_run_at, last_status, last_error, created_by, created_at, updated_at
		 FROM schedules WHERE id = ?`, id,
	)

	var s Schedule
	var connID, lastRun, nextRun, lastStatus, lastError, createdBy sql.NullString
	var enabled int

	err := row.Scan(&s.ID, &s.Name, &s.SavedQueryID, &connID, &s.Cron, &s.Timezone, &enabled, &s.TimeoutMs,
		&lastRun, &nextRun, &lastStatus, &lastError, &createdBy, &s.CreatedAt, &s.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get schedule by id: %w", err)
	}

	s.Enabled = enabled == 1
	s.ConnectionID = nullStringToPtr(connID)
	s.LastRunAt = nullStringToPtr(lastRun)
	s.NextRunAt = nullStringToPtr(nextRun)
	s.LastStatus = nullStringToPtr(lastStatus)
	s.LastError = nullStringToPtr(lastError)
	s.CreatedBy = nullStringToPtr(createdBy)
	return &s, nil
}

// CreateSchedule creates a new schedule and returns its ID.
func (db *DB) CreateSchedule(name, savedQueryID, connectionID, cron, timezone, createdBy string, timeoutMs int) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var connID, creator interface{}
	if connectionID != "" {
		connID = connectionID
	}
	if createdBy != "" {
		creator = createdBy
	}
	if timezone == "" {
		timezone = "UTC"
	}
	if timeoutMs <= 0 {
		timeoutMs = 60000
	}

	_, err := db.conn.Exec(
		`INSERT INTO schedules (id, name, saved_query_id, connection_id, cron, timezone, enabled, timeout_ms, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?)`,
		id, name, savedQueryID, connID, cron, timezone, timeoutMs, creator, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create schedule: %w", err)
	}
	return id, nil
}

// UpdateSchedule updates a schedule.
func (db *DB) UpdateSchedule(id, name, cron, timezone string, enabled bool, timeoutMs int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	enabledInt := 0
	if enabled {
		enabledInt = 1
	}

	_, err := db.conn.Exec(
		`UPDATE schedules SET name = ?, cron = ?, timezone = ?, enabled = ?, timeout_ms = ?, updated_at = ? WHERE id = ?`,
		name, cron, timezone, enabledInt, timeoutMs, now, id,
	)
	if err != nil {
		return fmt.Errorf("update schedule: %w", err)
	}
	return nil
}

// UpdateScheduleStatus updates the last run info for a schedule.
func (db *DB) UpdateScheduleStatus(id, status, lastError string, nextRunAt *time.Time) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var errVal, nextVal interface{}
	if lastError != "" {
		errVal = lastError
	}
	if nextRunAt != nil {
		nextVal = nextRunAt.UTC().Format(time.RFC3339)
	}

	_, err := db.conn.Exec(
		`UPDATE schedules SET last_run_at = ?, last_status = ?, last_error = ?, next_run_at = ?, updated_at = ? WHERE id = ?`,
		now, status, errVal, nextVal, now, id,
	)
	if err != nil {
		return fmt.Errorf("update schedule status: %w", err)
	}
	return nil
}

// DeleteSchedule deletes a schedule and all its runs (cascade).
func (db *DB) DeleteSchedule(id string) error {
	_, err := db.conn.Exec("DELETE FROM schedules WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	return nil
}

// CreateScheduleRun creates a new schedule run record and returns its ID.
func (db *DB) CreateScheduleRun(scheduleID, status string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		`INSERT INTO schedule_runs (id, schedule_id, started_at, status, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, scheduleID, now, status, now,
	)
	if err != nil {
		return "", fmt.Errorf("create schedule run: %w", err)
	}
	return id, nil
}

// UpdateScheduleRun updates a schedule run with results.
func (db *DB) UpdateScheduleRun(id, status string, rowsAffected, elapsedMs int, runError string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var errVal interface{}
	if runError != "" {
		errVal = runError
	}

	_, err := db.conn.Exec(
		`UPDATE schedule_runs SET finished_at = ?, status = ?, rows_affected = ?, elapsed_ms = ?, error = ? WHERE id = ?`,
		now, status, rowsAffected, elapsedMs, errVal, id,
	)
	if err != nil {
		return fmt.Errorf("update schedule run: %w", err)
	}
	return nil
}

// GetScheduleRuns retrieves runs for a schedule, most recent first.
func (db *DB) GetScheduleRuns(scheduleID string, limit, offset int) ([]ScheduleRun, error) {
	if limit <= 0 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	rows, err := db.conn.Query(
		`SELECT id, schedule_id, started_at, finished_at, status, rows_affected, elapsed_ms, error, created_at
		 FROM schedule_runs WHERE schedule_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?`,
		scheduleID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get schedule runs: %w", err)
	}
	defer rows.Close()

	var runs []ScheduleRun
	for rows.Next() {
		var r ScheduleRun
		var finishedAt, runError sql.NullString
		if err := rows.Scan(&r.ID, &r.ScheduleID, &r.StartedAt, &finishedAt, &r.Status, &r.RowsAffected, &r.ElapsedMs, &runError, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan schedule run: %w", err)
		}
		r.FinishedAt = nullStringToPtr(finishedAt)
		r.Error = nullStringToPtr(runError)
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedule run rows: %w", err)
	}
	return runs, nil
}

// scanSchedule is a helper for scanning schedule rows.
func scanSchedule(rows *sql.Rows) (Schedule, error) {
	var s Schedule
	var connID, lastRun, nextRun, lastStatus, lastError, createdBy sql.NullString
	var enabled int

	err := rows.Scan(&s.ID, &s.Name, &s.SavedQueryID, &connID, &s.Cron, &s.Timezone, &enabled, &s.TimeoutMs,
		&lastRun, &nextRun, &lastStatus, &lastError, &createdBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, fmt.Errorf("scan schedule: %w", err)
	}

	s.Enabled = enabled == 1
	s.ConnectionID = nullStringToPtr(connID)
	s.LastRunAt = nullStringToPtr(lastRun)
	s.NextRunAt = nullStringToPtr(nextRun)
	s.LastStatus = nullStringToPtr(lastStatus)
	s.LastError = nullStringToPtr(lastError)
	s.CreatedBy = nullStringToPtr(createdBy)
	return s, nil
}
