package database

import (
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Model represents a SQL model definition (dbt-like).
type Model struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Description     string `json:"description"`
	ConnectionID    string `json:"connection_id"`
	TargetDatabase  string `json:"target_database"`
	Materialization string `json:"materialization"`
	SQLBody         string `json:"sql_body"`
	TableEngine     string `json:"table_engine"`
	OrderBy         string `json:"order_by"`
	// PartitionBy is the PARTITION BY expression for MergeTree-family tables.
	PartitionBy string `json:"partition_by"`
	// EngineArgs is the engine's argument list, without the parentheses
	// (for ReplacingMergeTree, the version column).
	EngineArgs string `json:"engine_args"`
	// IncrementalStrategy is 'append' or 'delete_insert' for incremental models.
	IncrementalStrategy string `json:"incremental_strategy"`
	// UniqueKey is the key delete_insert replaces rows by (one column or a
	// comma-separated list).
	UniqueKey string `json:"unique_key"`
	// WatermarkColumn limits incremental runs to rows newer than the target's
	// current maximum of this column.
	WatermarkColumn string  `json:"watermark_column"`
	Status          string  `json:"status"`
	LastError       *string `json:"last_error"`
	LastRunAt       *string `json:"last_run_at"`
	CreatedBy       *string `json:"created_by"`
	Source          string  `json:"source"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// ModelRun represents a batch execution of models.
type ModelRun struct {
	ID          string  `json:"id"`
	ConnID      string  `json:"connection_id"`
	Status      string  `json:"status"`
	Total       int     `json:"total_models"`
	Succeeded   int     `json:"succeeded"`
	Failed      int     `json:"failed"`
	Skipped     int     `json:"skipped"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  *string `json:"finished_at"`
	TriggeredBy *string `json:"triggered_by"`
	CreatedAt   string  `json:"created_at"`
	Selector    string  `json:"selector"`
	PipelineID  *string `json:"pipeline_id"`
}

// ModelRunResult represents per-model results within a run.
type ModelRunResult struct {
	ID          string  `json:"id"`
	RunID       string  `json:"run_id"`
	ModelID     string  `json:"model_id"`
	ModelName   string  `json:"model_name"`
	Status      string  `json:"status"`
	ResolvedSQL *string `json:"resolved_sql"`
	ElapsedMs   int64   `json:"elapsed_ms"`
	Error       *string `json:"error"`
	StartedAt   *string `json:"started_at"`
	FinishedAt  *string `json:"finished_at"`
	CreatedAt   string  `json:"created_at"`
	TestsPassed int     `json:"tests_passed"`
	TestsFailed int     `json:"tests_failed"`
	// TestDetails is a JSON array of per-test outcomes, or nil.
	TestDetails *string `json:"test_details"`
}

// ── Model CRUD ──────────────────────────────────────────────────────

// GetModelsByConnection returns all models for a connection.
func (db *DB) GetModelsByConnection(connectionID string) ([]Model, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, description, connection_id, target_database, materialization,
		        sql_body, table_engine, order_by, partition_by, engine_args,
		        incremental_strategy, unique_key, watermark_column, status, last_error, last_run_at,
		        created_by, source, created_at, updated_at
		 FROM models WHERE connection_id = ? ORDER BY name ASC`, connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("get models: %w", err)
	}
	defer rows.Close()

	var models []Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate model rows: %w", err)
	}
	return models, nil
}

// GetModelByID returns a single model by ID.
func (db *DB) GetModelByID(id string) (*Model, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, description, connection_id, target_database, materialization,
		        sql_body, table_engine, order_by, partition_by, engine_args,
		        incremental_strategy, unique_key, watermark_column, status, last_error, last_run_at,
		        created_by, source, created_at, updated_at
		 FROM models WHERE id = ?`, id,
	)
	m, err := scanModelRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model by id: %w", err)
	}
	return m, nil
}

// GetModelByName returns a model by connection and name.
func (db *DB) GetModelByName(connectionID, name string) (*Model, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, description, connection_id, target_database, materialization,
		        sql_body, table_engine, order_by, partition_by, engine_args,
		        incremental_strategy, unique_key, watermark_column, status, last_error, last_run_at,
		        created_by, source, created_at, updated_at
		 FROM models WHERE connection_id = ? AND name = ?`, connectionID, name,
	)
	m, err := scanModelRow(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model by name: %w", err)
	}
	return m, nil
}

// CreateModel creates a new model.
func (db *DB) CreateModel(connectionID, name, description, targetDB, materialization, sqlBody, tableEngine, orderBy, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var creator interface{}
	if createdBy != "" {
		creator = createdBy
	}

	_, err := db.conn.Exec(
		`INSERT INTO models (id, name, description, connection_id, target_database, materialization,
		                     sql_body, table_engine, order_by, status, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?, ?, ?)`,
		id, name, description, connectionID, targetDB, materialization,
		sqlBody, tableEngine, orderBy, creator, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create model: %w", err)
	}
	return id, nil
}

// UpdateModel updates an existing model.
func (db *DB) UpdateModel(id, name, description, targetDB, materialization, sqlBody, tableEngine, orderBy string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		`UPDATE models SET name = ?, description = ?, target_database = ?, materialization = ?,
		        sql_body = ?, table_engine = ?, order_by = ?, updated_at = ?
		 WHERE id = ?`,
		name, description, targetDB, materialization, sqlBody, tableEngine, orderBy, now, id,
	)
	if err != nil {
		return fmt.Errorf("update model: %w", err)
	}
	return nil
}

// ModelSave is one atomic model write for SaveModel.
type ModelSave struct {
	// Model is the definition and options to store. An empty ID creates the
	// model in Model.ConnectionID; Source and CreatedBy are used only then
	// (an empty Source stores 'manual').
	Model Model
	// ReplaceTests swaps the model's tests for Tests.
	ReplaceTests bool
	Tests        []ModelTest
	// RenameInSelector, when set and an update changes the model's name, is
	// applied to the selector of every pipeline on the model's connection. It
	// returns the rewritten selector and whether anything changed.
	RenameInSelector func(selector, oldName, newName string) (string, bool)
}

// SaveModel creates or updates a model with its options, optionally replaces
// its tests, and on a rename rewrites pipeline selectors, all in one
// transaction. Returns the model's id.
func (db *DB) SaveModel(s ModelSave) (string, error) {
	m := s.Model
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.conn.Begin()
	if err != nil {
		return "", fmt.Errorf("save model: %w", err)
	}
	defer tx.Rollback()

	if m.ID == "" {
		m.ID = uuid.NewString()
		source := m.Source
		if source == "" {
			source = "manual"
		}
		var creator interface{}
		if m.CreatedBy != nil && *m.CreatedBy != "" {
			creator = *m.CreatedBy
		}
		if _, err := tx.Exec(
			`INSERT INTO models (id, name, description, connection_id, target_database, materialization,
			                     sql_body, table_engine, order_by, partition_by, engine_args,
			                     incremental_strategy, unique_key, watermark_column,
			                     status, source, created_by, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'draft', ?, ?, ?, ?)`,
			m.ID, m.Name, m.Description, m.ConnectionID, m.TargetDatabase, m.Materialization,
			m.SQLBody, m.TableEngine, m.OrderBy, m.PartitionBy, m.EngineArgs,
			m.IncrementalStrategy, m.UniqueKey, m.WatermarkColumn, source, creator, now, now,
		); err != nil {
			return "", fmt.Errorf("create model: %w", err)
		}
	} else {
		var oldName, connectionID string
		err := tx.QueryRow(`SELECT name, connection_id FROM models WHERE id = ?`, m.ID).Scan(&oldName, &connectionID)
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("update model: model %s not found", m.ID)
		}
		if err != nil {
			return "", fmt.Errorf("update model: %w", err)
		}
		if _, err := tx.Exec(
			`UPDATE models SET name = ?, description = ?, target_database = ?, materialization = ?,
			        sql_body = ?, table_engine = ?, order_by = ?, partition_by = ?, engine_args = ?,
			        incremental_strategy = ?, unique_key = ?, watermark_column = ?, updated_at = ?
			 WHERE id = ?`,
			m.Name, m.Description, m.TargetDatabase, m.Materialization,
			m.SQLBody, m.TableEngine, m.OrderBy, m.PartitionBy, m.EngineArgs,
			m.IncrementalStrategy, m.UniqueKey, m.WatermarkColumn, now, m.ID,
		); err != nil {
			return "", fmt.Errorf("update model: %w", err)
		}
		if oldName != m.Name && s.RenameInSelector != nil {
			if err := renamePipelineSelectors(tx, connectionID, oldName, m.Name, now, s.RenameInSelector); err != nil {
				return "", err
			}
		}
	}

	if s.ReplaceTests {
		if err := replaceModelTestsTx(tx, m.ID, s.Tests); err != nil {
			return "", err
		}
	}
	if err := tx.Commit(); err != nil {
		return "", fmt.Errorf("save model: %w", err)
	}
	return m.ID, nil
}

// renamePipelineSelectors rewrites the connection's pipeline selectors that
// name oldName.
func renamePipelineSelectors(tx *sql.Tx, connectionID, oldName, newName, now string, rename func(selector, oldName, newName string) (string, bool)) error {
	rows, err := tx.Query(`SELECT id, selector FROM model_pipelines WHERE connection_id = ?`, connectionID)
	if err != nil {
		return fmt.Errorf("rename in pipelines: %w", err)
	}
	type change struct{ id, selector string }
	var changes []change
	for rows.Next() {
		var id, selector string
		if err := rows.Scan(&id, &selector); err != nil {
			rows.Close()
			return fmt.Errorf("rename in pipelines: %w", err)
		}
		if next, ok := rename(selector, oldName, newName); ok {
			changes = append(changes, change{id, next})
		}
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("rename in pipelines: %w", err)
	}
	rows.Close()
	for _, c := range changes {
		if _, err := tx.Exec(`UPDATE model_pipelines SET selector = ?, updated_at = ? WHERE id = ?`, c.selector, now, c.id); err != nil {
			return fmt.Errorf("rename in pipelines: %w", err)
		}
	}
	return nil
}

// DeleteModel removes a model by ID.
func (db *DB) DeleteModel(id string) error {
	_, err := db.conn.Exec("DELETE FROM models WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete model: %w", err)
	}
	return nil
}

// UpdateModelStatus updates a model's status and last error.
func (db *DB) UpdateModelStatus(id, status, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var errVal interface{}
	if lastError != "" {
		errVal = lastError
	}

	_, err := db.conn.Exec(
		"UPDATE models SET status = ?, last_error = ?, last_run_at = ?, updated_at = ? WHERE id = ?",
		status, errVal, now, now, id,
	)
	if err != nil {
		return fmt.Errorf("update model status: %w", err)
	}
	return nil
}

// ── Model Runs ──────────────────────────────────────────────────────

// CreateModelRun creates a new run record.
func (db *DB) CreateModelRun(connectionID string, totalModels int, triggeredBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var trigger interface{}
	if triggeredBy != "" {
		trigger = triggeredBy
	}

	_, err := db.conn.Exec(
		`INSERT INTO model_runs (id, connection_id, status, total_models, started_at, triggered_by, created_at)
		 VALUES (?, ?, 'running', ?, ?, ?, ?)`,
		id, connectionID, totalModels, now, trigger, now,
	)
	if err != nil {
		return "", fmt.Errorf("create model run: %w", err)
	}
	return id, nil
}

// FinalizeModelRun marks a run as complete.
func (db *DB) FinalizeModelRun(id, status string, succeeded, failed, skipped int) error {
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		`UPDATE model_runs SET status = ?, succeeded = ?, failed = ?, skipped = ?, finished_at = ? WHERE id = ?`,
		status, succeeded, failed, skipped, now, id,
	)
	if err != nil {
		return fmt.Errorf("finalize model run: %w", err)
	}
	return nil
}

// GetModelRuns returns recent runs for a connection.
func (db *DB) GetModelRuns(connectionID string, limit, offset int) ([]ModelRun, error) {
	rows, err := db.conn.Query(
		`SELECT id, connection_id, status, total_models, succeeded, failed, skipped,
		        started_at, finished_at, triggered_by, created_at, selector, pipeline_id
		 FROM model_runs WHERE connection_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?`,
		connectionID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get model runs: %w", err)
	}
	defer rows.Close()

	var runs []ModelRun
	for rows.Next() {
		var r ModelRun
		var finished, trigger, pipeline sql.NullString
		if err := rows.Scan(&r.ID, &r.ConnID, &r.Status, &r.Total, &r.Succeeded,
			&r.Failed, &r.Skipped, &r.StartedAt, &finished, &trigger, &r.CreatedAt, &r.Selector, &pipeline); err != nil {
			return nil, fmt.Errorf("scan model run: %w", err)
		}
		r.FinishedAt = nullStringToPtr(finished)
		r.TriggeredBy = nullStringToPtr(trigger)
		r.PipelineID = nullStringToPtr(pipeline)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// GetModelRunByID returns a single run.
func (db *DB) GetModelRunByID(id string) (*ModelRun, error) {
	row := db.conn.QueryRow(
		`SELECT id, connection_id, status, total_models, succeeded, failed, skipped,
		        started_at, finished_at, triggered_by, created_at, selector, pipeline_id
		 FROM model_runs WHERE id = ?`, id,
	)

	var r ModelRun
	var finished, trigger, pipeline sql.NullString
	err := row.Scan(&r.ID, &r.ConnID, &r.Status, &r.Total, &r.Succeeded,
		&r.Failed, &r.Skipped, &r.StartedAt, &finished, &trigger, &r.CreatedAt, &r.Selector, &pipeline)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model run: %w", err)
	}
	r.FinishedAt = nullStringToPtr(finished)
	r.TriggeredBy = nullStringToPtr(trigger)
	r.PipelineID = nullStringToPtr(pipeline)
	return &r, nil
}

// ── Model Run Results ───────────────────────────────────────────────

// CreateModelRunResult creates a pending result record for a model in a run.
func (db *DB) CreateModelRunResult(runID, modelID, modelName string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		`INSERT INTO model_run_results (id, run_id, model_id, model_name, status, created_at)
		 VALUES (?, ?, ?, ?, 'pending', ?)`,
		id, runID, modelID, modelName, now,
	)
	if err != nil {
		return "", fmt.Errorf("create model run result: %w", err)
	}
	return id, nil
}

// UpdateModelRunResult updates the result for a specific model in a run.
func (db *DB) UpdateModelRunResult(runID, modelID, status, resolvedSQL string, elapsedMs int64, errMsg string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var sqlVal, errVal interface{}
	if resolvedSQL != "" {
		sqlVal = resolvedSQL
	}
	if errMsg != "" {
		errVal = errMsg
	}

	_, err := db.conn.Exec(
		`UPDATE model_run_results SET status = ?, resolved_sql = ?, elapsed_ms = ?, error = ?,
		        started_at = COALESCE(started_at, ?), finished_at = ?
		 WHERE run_id = ? AND model_id = ?`,
		status, sqlVal, elapsedMs, errVal, now, now, runID, modelID,
	)
	if err != nil {
		return fmt.Errorf("update model run result: %w", err)
	}
	return nil
}

// GetModelRunResults returns all results for a run.
func (db *DB) GetModelRunResults(runID string) ([]ModelRunResult, error) {
	rows, err := db.conn.Query(
		`SELECT id, run_id, model_id, model_name, status, resolved_sql, elapsed_ms,
		        error, started_at, finished_at, created_at, tests_passed, tests_failed, test_details
		 FROM model_run_results WHERE run_id = ? ORDER BY created_at ASC`, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("get model run results: %w", err)
	}
	defer rows.Close()

	var results []ModelRunResult
	for rows.Next() {
		r, err := scanModelRunResult(rows)
		if err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, rows.Err()
}

// GetLatestModelRunResult returns the model's most recent per-run result,
// or nil when it never ran.
func (db *DB) GetLatestModelRunResult(modelID string) (*ModelRunResult, error) {
	row := db.conn.QueryRow(
		`SELECT id, run_id, model_id, model_name, status, resolved_sql, elapsed_ms,
		        error, started_at, finished_at, created_at, tests_passed, tests_failed, test_details
		 FROM model_run_results WHERE model_id = ? ORDER BY created_at DESC, rowid DESC LIMIT 1`, modelID,
	)
	r, err := scanModelRunResult(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func scanModelRunResult(row rowScanner) (ModelRunResult, error) {
	var r ModelRunResult
	var resolvedSQL, errStr, started, finished, details sql.NullString
	if err := row.Scan(&r.ID, &r.RunID, &r.ModelID, &r.ModelName, &r.Status,
		&resolvedSQL, &r.ElapsedMs, &errStr, &started, &finished, &r.CreatedAt,
		&r.TestsPassed, &r.TestsFailed, &details); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return r, err
		}
		return r, fmt.Errorf("scan model run result: %w", err)
	}
	r.ResolvedSQL = nullStringToPtr(resolvedSQL)
	r.Error = nullStringToPtr(errStr)
	r.StartedAt = nullStringToPtr(started)
	r.FinishedAt = nullStringToPtr(finished)
	r.TestDetails = nullStringToPtr(details)
	return r, nil
}

// HasRunningModelRun checks if there's an active run for a connection.
func (db *DB) HasRunningModelRun(connectionID string) (bool, error) {
	var count int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM model_runs WHERE connection_id = ? AND status = 'running'",
		connectionID,
	).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("check running model run: %w", err)
	}
	return count > 0, nil
}

// HasSuccessfulModelRun reports whether the model has at least one successful
// run result on record.
func (db *DB) HasSuccessfulModelRun(modelID string) (bool, error) {
	var n int
	err := db.conn.QueryRow(
		"SELECT COUNT(*) FROM model_run_results WHERE model_id = ? AND status = 'success'", modelID,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("check model run history: %w", err)
	}
	return n > 0, nil
}

// MarkInterruptedModelRuns closes runs a previous process left running: their
// pending and running results become errors with the given message, and each
// run is finalized as an error with recounted totals. Call it once at startup,
// before any run can start. Returns the number of runs closed.
func (db *DB) MarkInterruptedModelRuns(message string) (int, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.conn.Begin()
	if err != nil {
		return 0, fmt.Errorf("mark interrupted model runs: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(
		`UPDATE model_run_results SET status = 'error', error = ?, finished_at = ?
		 WHERE status IN ('pending', 'running')
		   AND run_id IN (SELECT id FROM model_runs WHERE status = 'running')`,
		message, now,
	); err != nil {
		return 0, fmt.Errorf("mark interrupted model results: %w", err)
	}
	res, err := tx.Exec(
		`UPDATE model_runs SET status = 'error', finished_at = ?,
		        succeeded = (SELECT COUNT(*) FROM model_run_results r WHERE r.run_id = model_runs.id AND r.status = 'success'),
		        failed    = (SELECT COUNT(*) FROM model_run_results r WHERE r.run_id = model_runs.id AND r.status = 'error'),
		        skipped   = (SELECT COUNT(*) FROM model_run_results r WHERE r.run_id = model_runs.id AND r.status = 'skipped')
		 WHERE status = 'running'`,
		now,
	)
	if err != nil {
		return 0, fmt.Errorf("mark interrupted model runs: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("mark interrupted model runs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("mark interrupted model runs: %w", err)
	}
	return int(n), nil
}

// ── Helpers ─────────────────────────────────────────────────────────

func scanModel(rows *sql.Rows) (Model, error) {
	var m Model
	var lastErr, lastRun, createdBy sql.NullString
	if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.ConnectionID,
		&m.TargetDatabase, &m.Materialization, &m.SQLBody, &m.TableEngine,
		&m.OrderBy, &m.PartitionBy, &m.EngineArgs, &m.IncrementalStrategy,
		&m.UniqueKey, &m.WatermarkColumn, &m.Status, &lastErr, &lastRun, &createdBy,
		&m.Source, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return m, fmt.Errorf("scan model: %w", err)
	}
	m.LastError = nullStringToPtr(lastErr)
	m.LastRunAt = nullStringToPtr(lastRun)
	m.CreatedBy = nullStringToPtr(createdBy)
	return m, nil
}

func scanModelRow(row *sql.Row) (*Model, error) {
	var m Model
	var lastErr, lastRun, createdBy sql.NullString
	err := row.Scan(&m.ID, &m.Name, &m.Description, &m.ConnectionID,
		&m.TargetDatabase, &m.Materialization, &m.SQLBody, &m.TableEngine,
		&m.OrderBy, &m.PartitionBy, &m.EngineArgs, &m.IncrementalStrategy,
		&m.UniqueKey, &m.WatermarkColumn, &m.Status, &lastErr, &lastRun, &createdBy,
		&m.Source, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	m.LastError = nullStringToPtr(lastErr)
	m.LastRunAt = nullStringToPtr(lastRun)
	m.CreatedBy = nullStringToPtr(createdBy)
	return &m, nil
}

// SetModelSource updates the source field of a model.
func (db *DB) SetModelSource(id, source string) error {
	_, err := db.conn.Exec(`UPDATE models SET source = ?, updated_at = ? WHERE id = ?`,
		source, time.Now().UTC().Format(time.RFC3339), id)
	if err != nil {
		return fmt.Errorf("set model source: %w", err)
	}
	return nil
}

// GetModelsByConnectionAndSource returns models filtered by source.
func (db *DB) GetModelsByConnectionAndSource(connectionID, source string) ([]Model, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, description, connection_id, target_database, materialization,
		        sql_body, table_engine, order_by, partition_by, engine_args,
		        incremental_strategy, unique_key, watermark_column, status, last_error, last_run_at,
		        created_by, source, created_at, updated_at
		 FROM models WHERE connection_id = ? AND source = ? ORDER BY name ASC`, connectionID, source,
	)
	if err != nil {
		return nil, fmt.Errorf("get models by source: %w", err)
	}
	defer rows.Close()

	var models []Model
	for rows.Next() {
		m, err := scanModel(rows)
		if err != nil {
			return nil, err
		}
		models = append(models, m)
	}
	return models, rows.Err()
}

// ── Model Schedules ─────────────────────────────────────────────────

// ModelSchedule is a legacy per-anchor schedule. model_schedules is only read
// now, to copy its rows into model_pipelines (see GetAllModelSchedules).
type ModelSchedule struct {
	ID            string  `json:"id"`
	ConnectionID  string  `json:"connection_id"`
	AnchorModelID *string `json:"anchor_model_id"`
	Cron          string  `json:"cron"`
	Enabled       bool    `json:"enabled"`
	LastRunAt     *string `json:"last_run_at"`
	NextRunAt     *string `json:"next_run_at"`
	LastStatus    *string `json:"last_status"`
	LastError     *string `json:"last_error"`
	CreatedBy     *string `json:"created_by"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

func scanModelSchedule(rows *sql.Rows) (ModelSchedule, error) {
	var s ModelSchedule
	var enabled int
	var anchor, lastRun, nextRun, lastStatus, lastErr, createdBy sql.NullString
	if err := rows.Scan(&s.ID, &s.ConnectionID, &anchor, &s.Cron, &enabled,
		&lastRun, &nextRun, &lastStatus, &lastErr, &createdBy,
		&s.CreatedAt, &s.UpdatedAt); err != nil {
		return s, fmt.Errorf("scan model schedule: %w", err)
	}
	s.Enabled = enabled == 1
	s.AnchorModelID = nullStringToPtr(anchor)
	s.LastRunAt = nullStringToPtr(lastRun)
	s.NextRunAt = nullStringToPtr(nextRun)
	s.LastStatus = nullStringToPtr(lastStatus)
	s.LastError = nullStringToPtr(lastErr)
	s.CreatedBy = nullStringToPtr(createdBy)
	return s, nil
}
