package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Model represents a SQL model definition (dbt-like).
type Model struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	Description     string  `json:"description"`
	ConnectionID    string  `json:"connection_id"`
	TargetDatabase  string  `json:"target_database"`
	Materialization string  `json:"materialization"`
	SQLBody         string  `json:"sql_body"`
	TableEngine     string  `json:"table_engine"`
	OrderBy         string  `json:"order_by"`
	Status          string  `json:"status"`
	LastError       *string `json:"last_error"`
	LastRunAt       *string `json:"last_run_at"`
	CreatedBy       *string `json:"created_by"`
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
}

// ── Model CRUD ──────────────────────────────────────────────────────

// GetModelsByConnection returns all models for a connection.
func (db *DB) GetModelsByConnection(connectionID string) ([]Model, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, description, connection_id, target_database, materialization,
		        sql_body, table_engine, order_by, status, last_error, last_run_at,
		        created_by, created_at, updated_at
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
		        sql_body, table_engine, order_by, status, last_error, last_run_at,
		        created_by, created_at, updated_at
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
		        sql_body, table_engine, order_by, status, last_error, last_run_at,
		        created_by, created_at, updated_at
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
		        started_at, finished_at, triggered_by, created_at
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
		var finished, trigger sql.NullString
		if err := rows.Scan(&r.ID, &r.ConnID, &r.Status, &r.Total, &r.Succeeded,
			&r.Failed, &r.Skipped, &r.StartedAt, &finished, &trigger, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model run: %w", err)
		}
		r.FinishedAt = nullStringToPtr(finished)
		r.TriggeredBy = nullStringToPtr(trigger)
		runs = append(runs, r)
	}
	return runs, rows.Err()
}

// GetModelRunByID returns a single run.
func (db *DB) GetModelRunByID(id string) (*ModelRun, error) {
	row := db.conn.QueryRow(
		`SELECT id, connection_id, status, total_models, succeeded, failed, skipped,
		        started_at, finished_at, triggered_by, created_at
		 FROM model_runs WHERE id = ?`, id,
	)

	var r ModelRun
	var finished, trigger sql.NullString
	err := row.Scan(&r.ID, &r.ConnID, &r.Status, &r.Total, &r.Succeeded,
		&r.Failed, &r.Skipped, &r.StartedAt, &finished, &trigger, &r.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get model run: %w", err)
	}
	r.FinishedAt = nullStringToPtr(finished)
	r.TriggeredBy = nullStringToPtr(trigger)
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
		        error, started_at, finished_at, created_at
		 FROM model_run_results WHERE run_id = ? ORDER BY created_at ASC`, runID,
	)
	if err != nil {
		return nil, fmt.Errorf("get model run results: %w", err)
	}
	defer rows.Close()

	var results []ModelRunResult
	for rows.Next() {
		var r ModelRunResult
		var resolvedSQL, errStr, started, finished sql.NullString
		if err := rows.Scan(&r.ID, &r.RunID, &r.ModelID, &r.ModelName, &r.Status,
			&resolvedSQL, &r.ElapsedMs, &errStr, &started, &finished, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan model run result: %w", err)
		}
		r.ResolvedSQL = nullStringToPtr(resolvedSQL)
		r.Error = nullStringToPtr(errStr)
		r.StartedAt = nullStringToPtr(started)
		r.FinishedAt = nullStringToPtr(finished)
		results = append(results, r)
	}
	return results, rows.Err()
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

// ── Helpers ─────────────────────────────────────────────────────────

func scanModel(rows *sql.Rows) (Model, error) {
	var m Model
	var lastErr, lastRun, createdBy sql.NullString
	if err := rows.Scan(&m.ID, &m.Name, &m.Description, &m.ConnectionID,
		&m.TargetDatabase, &m.Materialization, &m.SQLBody, &m.TableEngine,
		&m.OrderBy, &m.Status, &lastErr, &lastRun, &createdBy,
		&m.CreatedAt, &m.UpdatedAt); err != nil {
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
		&m.OrderBy, &m.Status, &lastErr, &lastRun, &createdBy,
		&m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	m.LastError = nullStringToPtr(lastErr)
	m.LastRunAt = nullStringToPtr(lastRun)
	m.CreatedBy = nullStringToPtr(createdBy)
	return &m, nil
}
