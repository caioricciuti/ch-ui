package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// ModelOptions are the per-materialization settings of a model beyond the
// engine and ORDER BY.
type ModelOptions struct {
	PartitionBy         string `json:"partition_by"`
	EngineArgs          string `json:"engine_args"`
	IncrementalStrategy string `json:"incremental_strategy"`
	UniqueKey           string `json:"unique_key"`
	WatermarkColumn     string `json:"watermark_column"`
}

// Options returns the model's options.
func (m Model) Options() ModelOptions {
	return ModelOptions{
		PartitionBy:         m.PartitionBy,
		EngineArgs:          m.EngineArgs,
		IncrementalStrategy: m.IncrementalStrategy,
		UniqueKey:           m.UniqueKey,
		WatermarkColumn:     m.WatermarkColumn,
	}
}

// SetModelOptions stores the model's options.
func (db *DB) SetModelOptions(id string, o ModelOptions) error {
	_, err := db.conn.Exec(
		`UPDATE models SET partition_by = ?, engine_args = ?, incremental_strategy = ?,
		        unique_key = ?, watermark_column = ?, updated_at = ?
		 WHERE id = ?`,
		o.PartitionBy, o.EngineArgs, o.IncrementalStrategy, o.UniqueKey, o.WatermarkColumn,
		time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return fmt.Errorf("set model options: %w", err)
	}
	return nil
}

// ── Data tests ──────────────────────────────────────────────────────

// ModelTest is one data test on a model's output.
type ModelTest struct {
	ID         string `json:"id"`
	ModelID    string `json:"model_id"`
	Kind       string `json:"kind"`
	ColumnName string `json:"column_name"`
	// Args is a JSON object; its keys depend on Kind.
	Args      string `json:"args"`
	Severity  string `json:"severity"`
	CreatedAt string `json:"created_at"`
	UpdatedAt string `json:"updated_at"`
}

// testTimeFormat has fixed-width fractions so timestamps sort as text.
const testTimeFormat = "2006-01-02T15:04:05.000000Z07:00"

const modelTestColumns = `id, model_id, kind, column_name, args, severity, created_at, updated_at`

func scanModelTests(rows *sql.Rows) ([]ModelTest, error) {
	defer rows.Close()
	var out []ModelTest
	for rows.Next() {
		var t ModelTest
		var created, updated sql.NullString
		if err := rows.Scan(&t.ID, &t.ModelID, &t.Kind, &t.ColumnName, &t.Args, &t.Severity, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan model test: %w", err)
		}
		t.CreatedAt, t.UpdatedAt = created.String, updated.String
		out = append(out, t)
	}
	return out, rows.Err()
}

// GetModelTests returns the tests of one model, oldest first.
func (db *DB) GetModelTests(modelID string) ([]ModelTest, error) {
	rows, err := db.conn.Query(`SELECT `+modelTestColumns+` FROM model_tests WHERE model_id = ? ORDER BY created_at ASC, id ASC`, modelID)
	if err != nil {
		return nil, fmt.Errorf("get model tests: %w", err)
	}
	return scanModelTests(rows)
}

// GetModelTestByID returns one test, or nil.
func (db *DB) GetModelTestByID(id string) (*ModelTest, error) {
	rows, err := db.conn.Query(`SELECT `+modelTestColumns+` FROM model_tests WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("get model test: %w", err)
	}
	tests, err := scanModelTests(rows)
	if err != nil || len(tests) == 0 {
		return nil, err
	}
	return &tests[0], nil
}

// CreateModelTest adds a test to a model.
func (db *DB) CreateModelTest(t ModelTest) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(testTimeFormat)
	if t.Args == "" {
		t.Args = "{}"
	}
	_, err := db.conn.Exec(
		`INSERT INTO model_tests (id, model_id, kind, column_name, args, severity, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, t.ModelID, t.Kind, t.ColumnName, t.Args, t.Severity, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create model test: %w", err)
	}
	return id, nil
}

// UpdateModelTest replaces a test's definition.
func (db *DB) UpdateModelTest(t ModelTest) error {
	if t.Args == "" {
		t.Args = "{}"
	}
	_, err := db.conn.Exec(
		`UPDATE model_tests SET kind = ?, column_name = ?, args = ?, severity = ?, updated_at = ? WHERE id = ?`,
		t.Kind, t.ColumnName, t.Args, t.Severity, time.Now().UTC().Format(testTimeFormat), t.ID,
	)
	if err != nil {
		return fmt.Errorf("update model test: %w", err)
	}
	return nil
}

// DeleteModelTest removes a test.
func (db *DB) DeleteModelTest(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM model_tests WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete model test: %w", err)
	}
	return nil
}

// ReplaceModelTests swaps a model's tests for the given list in one
// transaction (GitHub sync).
func (db *DB) ReplaceModelTests(modelID string, tests []ModelTest) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("replace model tests: %w", err)
	}
	defer tx.Rollback()
	if err := replaceModelTestsTx(tx, modelID, tests); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceModelTestsTx(tx *sql.Tx, modelID string, tests []ModelTest) error {
	if _, err := tx.Exec(`DELETE FROM model_tests WHERE model_id = ?`, modelID); err != nil {
		return fmt.Errorf("replace model tests: %w", err)
	}
	base := time.Now().UTC()
	for i, t := range tests {
		if t.Args == "" {
			t.Args = "{}"
		}
		// Distinct timestamps keep the file's order.
		ts := base.Add(time.Duration(i) * time.Microsecond).Format(testTimeFormat)
		if _, err := tx.Exec(
			`INSERT INTO model_tests (id, model_id, kind, column_name, args, severity, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
			uuid.NewString(), modelID, t.Kind, t.ColumnName, t.Args, t.Severity, ts, ts,
		); err != nil {
			return fmt.Errorf("replace model tests: %w", err)
		}
	}
	return nil
}

// UpdateModelRunResultTests records the data test outcome of a model in a run.
func (db *DB) UpdateModelRunResultTests(runID, modelID string, passed, failed int, details string) error {
	var d interface{}
	if details != "" {
		d = details
	}
	_, err := db.conn.Exec(
		`UPDATE model_run_results SET tests_passed = ?, tests_failed = ?, test_details = ? WHERE run_id = ? AND model_id = ?`,
		passed, failed, d, runID, modelID,
	)
	if err != nil {
		return fmt.Errorf("record model test results: %w", err)
	}
	return nil
}

// SetModelRunScope records which selector (and pipeline) a run was started for.
func (db *DB) SetModelRunScope(runID, selector, pipelineID string) error {
	var p interface{}
	if pipelineID != "" {
		p = pipelineID
	}
	if _, err := db.conn.Exec(`UPDATE model_runs SET selector = ?, pipeline_id = ? WHERE id = ?`, selector, p, runID); err != nil {
		return fmt.Errorf("record model run scope: %w", err)
	}
	return nil
}

// ── Named pipelines ─────────────────────────────────────────────────

// ModelPipeline is a named, stable group of models picked by a selector, with
// an optional cron schedule.
type ModelPipeline struct {
	ID               string  `json:"id"`
	ConnectionID     string  `json:"connection_id"`
	Name             string  `json:"name"`
	Selector         string  `json:"selector"`
	Cron             string  `json:"cron"`
	Enabled          bool    `json:"enabled"`
	Timezone         string  `json:"timezone"`
	LastRunAt        *string `json:"last_run_at"`
	NextRunAt        *string `json:"next_run_at"`
	LastStatus       *string `json:"last_status"`
	LastError        *string `json:"last_error"`
	LastRunID        *string `json:"last_run_id"`
	LegacyScheduleID *string `json:"-"`
	CreatedBy        *string `json:"created_by"`
	CreatedAt        string  `json:"created_at"`
	UpdatedAt        string  `json:"updated_at"`
}

const modelPipelineColumns = `id, connection_id, name, selector, cron, enabled, timezone, last_run_at, next_run_at,
	last_status, last_error, last_run_id, legacy_schedule_id, created_by, created_at, updated_at`

func scanModelPipelines(rows *sql.Rows) ([]ModelPipeline, error) {
	defer rows.Close()
	var out []ModelPipeline
	for rows.Next() {
		var p ModelPipeline
		var enabled int
		var lastRun, nextRun, lastStatus, lastErr, lastRunID, legacy, createdBy, created, updated sql.NullString
		if err := rows.Scan(&p.ID, &p.ConnectionID, &p.Name, &p.Selector, &p.Cron, &enabled, &p.Timezone,
			&lastRun, &nextRun, &lastStatus, &lastErr, &lastRunID, &legacy, &createdBy, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan model pipeline: %w", err)
		}
		p.Enabled = enabled == 1
		p.LastRunAt = nullStringToPtr(lastRun)
		p.NextRunAt = nullStringToPtr(nextRun)
		p.LastStatus = nullStringToPtr(lastStatus)
		p.LastError = nullStringToPtr(lastErr)
		p.LastRunID = nullStringToPtr(lastRunID)
		p.LegacyScheduleID = nullStringToPtr(legacy)
		p.CreatedBy = nullStringToPtr(createdBy)
		p.CreatedAt, p.UpdatedAt = created.String, updated.String
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetModelPipelines returns the pipelines of a connection by name.
func (db *DB) GetModelPipelines(connectionID string) ([]ModelPipeline, error) {
	rows, err := db.conn.Query(`SELECT `+modelPipelineColumns+` FROM model_pipelines WHERE connection_id = ? ORDER BY name ASC`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("get model pipelines: %w", err)
	}
	return scanModelPipelines(rows)
}

// GetModelPipelineByID returns one pipeline, or nil.
func (db *DB) GetModelPipelineByID(id string) (*ModelPipeline, error) {
	rows, err := db.conn.Query(`SELECT `+modelPipelineColumns+` FROM model_pipelines WHERE id = ?`, id)
	if err != nil {
		return nil, fmt.Errorf("get model pipeline: %w", err)
	}
	ps, err := scanModelPipelines(rows)
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	return &ps[0], nil
}

// GetModelPipelineByName returns one pipeline of a connection by name, or nil.
func (db *DB) GetModelPipelineByName(connectionID, name string) (*ModelPipeline, error) {
	rows, err := db.conn.Query(`SELECT `+modelPipelineColumns+` FROM model_pipelines WHERE connection_id = ? AND name = ?`, connectionID, name)
	if err != nil {
		return nil, fmt.Errorf("get model pipeline: %w", err)
	}
	ps, err := scanModelPipelines(rows)
	if err != nil || len(ps) == 0 {
		return nil, err
	}
	return &ps[0], nil
}

// GetEnabledModelPipelines returns enabled, scheduled pipelines.
func (db *DB) GetEnabledModelPipelines() ([]ModelPipeline, error) {
	rows, err := db.conn.Query(`SELECT ` + modelPipelineColumns + ` FROM model_pipelines WHERE enabled = 1 AND cron != ''`)
	if err != nil {
		return nil, fmt.Errorf("get enabled model pipelines: %w", err)
	}
	return scanModelPipelines(rows)
}

// CreateModelPipeline stores a new pipeline and returns its id.
func (db *DB) CreateModelPipeline(p ModelPipeline) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	var next, legacy, creator interface{}
	if p.NextRunAt != nil {
		next = *p.NextRunAt
	}
	if p.LegacyScheduleID != nil {
		legacy = *p.LegacyScheduleID
	}
	if p.CreatedBy != nil {
		creator = nullIfEmpty(*p.CreatedBy)
	}
	if p.Timezone == "" {
		p.Timezone = "UTC"
	}
	_, err := db.conn.Exec(
		`INSERT INTO model_pipelines (id, connection_id, name, selector, cron, enabled, timezone, next_run_at,
		                              legacy_schedule_id, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, p.ConnectionID, p.Name, p.Selector, p.Cron, boolToInt(p.Enabled), p.Timezone, next, legacy, creator, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create model pipeline: %w", err)
	}
	return id, nil
}

// UpdateModelPipeline saves a pipeline's definition and schedule.
func (db *DB) UpdateModelPipeline(p ModelPipeline) error {
	var next interface{}
	if p.NextRunAt != nil {
		next = *p.NextRunAt
	}
	if p.Timezone == "" {
		p.Timezone = "UTC"
	}
	_, err := db.conn.Exec(
		`UPDATE model_pipelines SET name = ?, selector = ?, cron = ?, enabled = ?, timezone = ?, next_run_at = ?, updated_at = ?
		 WHERE id = ?`,
		p.Name, p.Selector, p.Cron, boolToInt(p.Enabled), p.Timezone, next, time.Now().UTC().Format(time.RFC3339), p.ID,
	)
	if err != nil {
		return fmt.Errorf("update model pipeline: %w", err)
	}
	return nil
}

// DeleteModelPipeline removes a pipeline.
func (db *DB) DeleteModelPipeline(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM model_pipelines WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete model pipeline: %w", err)
	}
	return nil
}

// RecordModelPipelineRun stores the outcome of a pipeline run and its next due time.
func (db *DB) RecordModelPipelineRun(id, status, lastError, runID string, nextRunAt *string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	var next interface{}
	if nextRunAt != nil {
		next = *nextRunAt
	}
	_, err := db.conn.Exec(
		`UPDATE model_pipelines SET last_run_at = ?, last_status = ?, last_error = ?, last_run_id = ?, next_run_at = ?, updated_at = ?
		 WHERE id = ?`,
		now, status, nullIfEmpty(lastError), nullIfEmpty(runID), next, now, id,
	)
	if err != nil {
		return fmt.Errorf("record model pipeline run: %w", err)
	}
	return nil
}

// GetAllModelSchedules returns every legacy per-anchor schedule (for the
// one-time copy into model_pipelines).
func (db *DB) GetAllModelSchedules() ([]ModelSchedule, error) {
	rows, err := db.conn.Query(
		`SELECT id, connection_id, anchor_model_id, cron, enabled, last_run_at, next_run_at,
		        last_status, last_error, created_by, created_at, updated_at
		 FROM model_schedules`,
	)
	if err != nil {
		return nil, fmt.Errorf("get model schedules: %w", err)
	}
	defer rows.Close()
	var out []ModelSchedule
	for rows.Next() {
		s, err := scanModelSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ModelPipelineForLegacySchedule reports whether a legacy schedule was
// already copied into model_pipelines.
func (db *DB) ModelPipelineForLegacySchedule(scheduleID string) (bool, error) {
	var n int
	if err := db.conn.QueryRow(`SELECT COUNT(*) FROM model_pipelines WHERE legacy_schedule_id = ?`, scheduleID).Scan(&n); err != nil {
		return false, fmt.Errorf("check migrated schedule: %w", err)
	}
	return n > 0, nil
}
