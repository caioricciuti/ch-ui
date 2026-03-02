package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Pipeline represents a data ingestion pipeline.
type Pipeline struct {
	ID            string  `json:"id"`
	Name          string  `json:"name"`
	Description   *string `json:"description"`
	ConnectionID  string  `json:"connection_id"`
	Status        string  `json:"status"`
	Config        string  `json:"config"`
	CreatedBy     *string `json:"created_by"`
	LastStartedAt *string `json:"last_started_at"`
	LastStoppedAt *string `json:"last_stopped_at"`
	LastError     *string `json:"last_error"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`
}

// PipelineNode represents a node in a pipeline graph.
type PipelineNode struct {
	ID              string  `json:"id"`
	PipelineID      string  `json:"pipeline_id"`
	NodeType        string  `json:"node_type"`
	Label           string  `json:"label"`
	PositionX       float64 `json:"position_x"`
	PositionY       float64 `json:"position_y"`
	ConfigEncrypted string  `json:"config_encrypted"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

// PipelineEdge represents a connection between two nodes.
type PipelineEdge struct {
	ID           string  `json:"id"`
	PipelineID   string  `json:"pipeline_id"`
	SourceNodeID string  `json:"source_node_id"`
	TargetNodeID string  `json:"target_node_id"`
	SourceHandle *string `json:"source_handle"`
	TargetHandle *string `json:"target_handle"`
	CreatedAt    string  `json:"created_at"`
}

// PipelineRun represents an execution run of a pipeline.
type PipelineRun struct {
	ID           string  `json:"id"`
	PipelineID   string  `json:"pipeline_id"`
	Status       string  `json:"status"`
	StartedAt    string  `json:"started_at"`
	FinishedAt   *string `json:"finished_at"`
	RowsIngested int64   `json:"rows_ingested"`
	BytesIngested int64  `json:"bytes_ingested"`
	ErrorsCount  int64   `json:"errors_count"`
	LastError    *string `json:"last_error"`
	MetricsJSON  string  `json:"metrics_json"`
	CreatedAt    string  `json:"created_at"`
}

// PipelineRunLog represents a log entry for a pipeline run.
type PipelineRunLog struct {
	ID        string `json:"id"`
	RunID     string `json:"run_id"`
	Level     string `json:"level"`
	Message   string `json:"message"`
	CreatedAt string `json:"created_at"`
}

// ── Pipeline CRUD ──────────────────────────────────────────────────

// GetPipelines retrieves all pipelines.
func (db *DB) GetPipelines() ([]Pipeline, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, description, connection_id, status, config,
		        created_by, last_started_at, last_stopped_at, last_error,
		        created_at, updated_at
		 FROM pipelines ORDER BY updated_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("get pipelines: %w", err)
	}
	defer rows.Close()

	var pipelines []Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		pipelines = append(pipelines, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pipeline rows: %w", err)
	}
	return pipelines, nil
}

// GetPipelineByID retrieves a pipeline by ID.
func (db *DB) GetPipelineByID(id string) (*Pipeline, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, description, connection_id, status, config,
		        created_by, last_started_at, last_stopped_at, last_error,
		        created_at, updated_at
		 FROM pipelines WHERE id = ?`, id,
	)

	var p Pipeline
	var desc, createdBy, lastStarted, lastStopped, lastErr sql.NullString
	err := row.Scan(&p.ID, &p.Name, &desc, &p.ConnectionID, &p.Status, &p.Config,
		&createdBy, &lastStarted, &lastStopped, &lastErr,
		&p.CreatedAt, &p.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get pipeline by id: %w", err)
	}
	p.Description = nullStringToPtr(desc)
	p.CreatedBy = nullStringToPtr(createdBy)
	p.LastStartedAt = nullStringToPtr(lastStarted)
	p.LastStoppedAt = nullStringToPtr(lastStopped)
	p.LastError = nullStringToPtr(lastErr)
	return &p, nil
}

// GetPipelinesByStatus retrieves pipelines with a given status.
func (db *DB) GetPipelinesByStatus(status string) ([]Pipeline, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, description, connection_id, status, config,
		        created_by, last_started_at, last_stopped_at, last_error,
		        created_at, updated_at
		 FROM pipelines WHERE status = ? ORDER BY updated_at DESC`, status,
	)
	if err != nil {
		return nil, fmt.Errorf("get pipelines by status: %w", err)
	}
	defer rows.Close()

	var pipelines []Pipeline
	for rows.Next() {
		p, err := scanPipeline(rows)
		if err != nil {
			return nil, err
		}
		pipelines = append(pipelines, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pipeline rows: %w", err)
	}
	return pipelines, nil
}

// CreatePipeline creates a new pipeline and returns its ID.
func (db *DB) CreatePipeline(name, description, connectionID, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var desc, creator interface{}
	if description != "" {
		desc = description
	}
	if createdBy != "" {
		creator = createdBy
	}

	_, err := db.conn.Exec(
		`INSERT INTO pipelines (id, name, description, connection_id, status, config, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, 'draft', '{}', ?, ?, ?)`,
		id, name, desc, connectionID, creator, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create pipeline: %w", err)
	}
	return id, nil
}

// UpdatePipeline updates a pipeline's name and description.
func (db *DB) UpdatePipeline(id, name, description string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var desc interface{}
	if description != "" {
		desc = description
	}

	_, err := db.conn.Exec(
		"UPDATE pipelines SET name = ?, description = ?, updated_at = ? WHERE id = ?",
		name, desc, now, id,
	)
	if err != nil {
		return fmt.Errorf("update pipeline: %w", err)
	}
	return nil
}

// UpdatePipelineStatus updates a pipeline's status and optional error/timestamp fields.
func (db *DB) UpdatePipelineStatus(id, status, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var errVal interface{}
	if lastError != "" {
		errVal = lastError
	}

	var startedAt, stoppedAt interface{}
	switch status {
	case "running", "starting":
		startedAt = now
	case "stopped", "error":
		stoppedAt = now
	}

	_, err := db.conn.Exec(
		`UPDATE pipelines SET status = ?, last_error = ?,
		 last_started_at = COALESCE(?, last_started_at),
		 last_stopped_at = COALESCE(?, last_stopped_at),
		 updated_at = ? WHERE id = ?`,
		status, errVal, startedAt, stoppedAt, now, id,
	)
	if err != nil {
		return fmt.Errorf("update pipeline status: %w", err)
	}
	return nil
}

// DeletePipeline deletes a pipeline and all related data (cascade).
func (db *DB) DeletePipeline(id string) error {
	_, err := db.conn.Exec("DELETE FROM pipelines WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete pipeline: %w", err)
	}
	return nil
}

// ── Pipeline Graph ─────────────────────────────────────────────────

// SavePipelineGraph atomically replaces all nodes and edges for a pipeline.
func (db *DB) SavePipelineGraph(pipelineID string, nodes []PipelineNode, edges []PipelineEdge, viewportConfig string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin graph transaction: %w", err)
	}
	defer tx.Rollback()

	now := time.Now().UTC().Format(time.RFC3339)

	// Delete existing nodes and edges (edges cascade from nodes)
	if _, err := tx.Exec("DELETE FROM pipeline_edges WHERE pipeline_id = ?", pipelineID); err != nil {
		return fmt.Errorf("delete old edges: %w", err)
	}
	if _, err := tx.Exec("DELETE FROM pipeline_nodes WHERE pipeline_id = ?", pipelineID); err != nil {
		return fmt.Errorf("delete old nodes: %w", err)
	}

	// Insert nodes
	for _, n := range nodes {
		nodeID := n.ID
		if nodeID == "" {
			nodeID = uuid.NewString()
		}
		_, err := tx.Exec(
			`INSERT INTO pipeline_nodes (id, pipeline_id, node_type, label, position_x, position_y, config_encrypted, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			nodeID, pipelineID, n.NodeType, n.Label, n.PositionX, n.PositionY, n.ConfigEncrypted, now, now,
		)
		if err != nil {
			return fmt.Errorf("insert node %s: %w", nodeID, err)
		}
	}

	// Insert edges
	for _, e := range edges {
		edgeID := e.ID
		if edgeID == "" {
			edgeID = uuid.NewString()
		}
		var srcHandle, tgtHandle interface{}
		if e.SourceHandle != nil {
			srcHandle = *e.SourceHandle
		}
		if e.TargetHandle != nil {
			tgtHandle = *e.TargetHandle
		}
		_, err := tx.Exec(
			`INSERT INTO pipeline_edges (id, pipeline_id, source_node_id, target_node_id, source_handle, target_handle, created_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			edgeID, pipelineID, e.SourceNodeID, e.TargetNodeID, srcHandle, tgtHandle, now,
		)
		if err != nil {
			return fmt.Errorf("insert edge %s: %w", edgeID, err)
		}
	}

	// Update pipeline config (viewport) and updated_at
	if viewportConfig != "" {
		if _, err := tx.Exec("UPDATE pipelines SET config = ?, updated_at = ? WHERE id = ?", viewportConfig, now, pipelineID); err != nil {
			return fmt.Errorf("update pipeline config: %w", err)
		}
	} else {
		if _, err := tx.Exec("UPDATE pipelines SET updated_at = ? WHERE id = ?", now, pipelineID); err != nil {
			return fmt.Errorf("update pipeline updated_at: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit graph transaction: %w", err)
	}
	return nil
}

// GetPipelineGraph retrieves all nodes and edges for a pipeline.
func (db *DB) GetPipelineGraph(pipelineID string) ([]PipelineNode, []PipelineEdge, error) {
	// Nodes
	nodeRows, err := db.conn.Query(
		`SELECT id, pipeline_id, node_type, label, position_x, position_y, config_encrypted, created_at, updated_at
		 FROM pipeline_nodes WHERE pipeline_id = ? ORDER BY created_at ASC`, pipelineID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("get pipeline nodes: %w", err)
	}
	defer nodeRows.Close()

	var nodes []PipelineNode
	for nodeRows.Next() {
		var n PipelineNode
		if err := nodeRows.Scan(&n.ID, &n.PipelineID, &n.NodeType, &n.Label,
			&n.PositionX, &n.PositionY, &n.ConfigEncrypted,
			&n.CreatedAt, &n.UpdatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan pipeline node: %w", err)
		}
		nodes = append(nodes, n)
	}
	if err := nodeRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate node rows: %w", err)
	}

	// Edges
	edgeRows, err := db.conn.Query(
		`SELECT id, pipeline_id, source_node_id, target_node_id, source_handle, target_handle, created_at
		 FROM pipeline_edges WHERE pipeline_id = ? ORDER BY created_at ASC`, pipelineID,
	)
	if err != nil {
		return nil, nil, fmt.Errorf("get pipeline edges: %w", err)
	}
	defer edgeRows.Close()

	var edges []PipelineEdge
	for edgeRows.Next() {
		var e PipelineEdge
		var srcHandle, tgtHandle sql.NullString
		if err := edgeRows.Scan(&e.ID, &e.PipelineID, &e.SourceNodeID, &e.TargetNodeID,
			&srcHandle, &tgtHandle, &e.CreatedAt); err != nil {
			return nil, nil, fmt.Errorf("scan pipeline edge: %w", err)
		}
		e.SourceHandle = nullStringToPtr(srcHandle)
		e.TargetHandle = nullStringToPtr(tgtHandle)
		edges = append(edges, e)
	}
	if err := edgeRows.Err(); err != nil {
		return nil, nil, fmt.Errorf("iterate edge rows: %w", err)
	}

	return nodes, edges, nil
}

// ── Pipeline Runs ──────────────────────────────────────────────────

// CreatePipelineRun creates a new run record and returns its ID.
func (db *DB) CreatePipelineRun(pipelineID, status string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	_, err := db.conn.Exec(
		`INSERT INTO pipeline_runs (id, pipeline_id, status, started_at, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		id, pipelineID, status, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create pipeline run: %w", err)
	}
	return id, nil
}

// UpdatePipelineRun updates a run's status and metrics.
func (db *DB) UpdatePipelineRun(id, status string, rowsIngested, bytesIngested, errorsCount int64, lastError, metricsJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	var errVal interface{}
	if lastError != "" {
		errVal = lastError
	}
	var finished interface{}
	if status != "running" {
		finished = now
	}
	if metricsJSON == "" {
		metricsJSON = "{}"
	}

	_, err := db.conn.Exec(
		`UPDATE pipeline_runs SET status = ?, finished_at = ?, rows_ingested = ?, bytes_ingested = ?,
		 errors_count = ?, last_error = ?, metrics_json = ? WHERE id = ?`,
		status, finished, rowsIngested, bytesIngested, errorsCount, errVal, metricsJSON, id,
	)
	if err != nil {
		return fmt.Errorf("update pipeline run: %w", err)
	}
	return nil
}

// GetPipelineRuns retrieves runs for a pipeline with limit/offset.
func (db *DB) GetPipelineRuns(pipelineID string, limit, offset int) ([]PipelineRun, error) {
	rows, err := db.conn.Query(
		`SELECT id, pipeline_id, status, started_at, finished_at, rows_ingested, bytes_ingested,
		        errors_count, last_error, metrics_json, created_at
		 FROM pipeline_runs WHERE pipeline_id = ? ORDER BY started_at DESC LIMIT ? OFFSET ?`,
		pipelineID, limit, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("get pipeline runs: %w", err)
	}
	defer rows.Close()

	var runs []PipelineRun
	for rows.Next() {
		var r PipelineRun
		var finished, lastErr sql.NullString
		if err := rows.Scan(&r.ID, &r.PipelineID, &r.Status, &r.StartedAt, &finished,
			&r.RowsIngested, &r.BytesIngested, &r.ErrorsCount, &lastErr,
			&r.MetricsJSON, &r.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline run: %w", err)
		}
		r.FinishedAt = nullStringToPtr(finished)
		r.LastError = nullStringToPtr(lastErr)
		runs = append(runs, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate run rows: %w", err)
	}
	return runs, nil
}

// ── Pipeline Run Logs ──────────────────────────────────────────────

// CreatePipelineRunLog creates a log entry for a pipeline run.
func (db *DB) CreatePipelineRunLog(runID, level, message string) error {
	id := uuid.NewString()

	_, err := db.conn.Exec(
		`INSERT INTO pipeline_run_logs (id, run_id, level, message, created_at)
		 VALUES (?, ?, ?, ?, CURRENT_TIMESTAMP)`,
		id, runID, level, message,
	)
	if err != nil {
		return fmt.Errorf("create pipeline run log: %w", err)
	}
	return nil
}

// GetPipelineRunLogs retrieves logs for a pipeline run.
func (db *DB) GetPipelineRunLogs(runID string, limit int) ([]PipelineRunLog, error) {
	if limit <= 0 {
		limit = 200
	}

	rows, err := db.conn.Query(
		`SELECT id, run_id, level, message, created_at
		 FROM pipeline_run_logs WHERE run_id = ? ORDER BY created_at DESC LIMIT ?`,
		runID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("get pipeline run logs: %w", err)
	}
	defer rows.Close()

	var logs []PipelineRunLog
	for rows.Next() {
		var l PipelineRunLog
		if err := rows.Scan(&l.ID, &l.RunID, &l.Level, &l.Message, &l.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline run log: %w", err)
		}
		logs = append(logs, l)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate log rows: %w", err)
	}
	return logs, nil
}

// ── Helpers ────────────────────────────────────────────────────────

// scanPipeline scans a pipeline row from a *sql.Rows.
func scanPipeline(rows *sql.Rows) (Pipeline, error) {
	var p Pipeline
	var desc, createdBy, lastStarted, lastStopped, lastErr sql.NullString
	if err := rows.Scan(&p.ID, &p.Name, &desc, &p.ConnectionID, &p.Status, &p.Config,
		&createdBy, &lastStarted, &lastStopped, &lastErr,
		&p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, fmt.Errorf("scan pipeline: %w", err)
	}
	p.Description = nullStringToPtr(desc)
	p.CreatedBy = nullStringToPtr(createdBy)
	p.LastStartedAt = nullStringToPtr(lastStarted)
	p.LastStoppedAt = nullStringToPtr(lastStopped)
	p.LastError = nullStringToPtr(lastErr)
	return p, nil
}
