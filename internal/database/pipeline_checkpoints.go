package database

import (
	"fmt"
	"time"
)

// PipelineCheckpoint is a source position saved after a written batch.
type PipelineCheckpoint struct {
	SourceKey string `json:"source_key"`
	Position  string `json:"position"`
	UpdatedAt string `json:"updated_at"`
}

// GetPipelineCheckpoints returns a pipeline's saved source positions.
func (db *DB) GetPipelineCheckpoints(pipelineID string) ([]PipelineCheckpoint, error) {
	rows, err := db.conn.Query(
		`SELECT source_key, position, updated_at FROM pipeline_checkpoints WHERE pipeline_id = ? ORDER BY source_key`,
		pipelineID,
	)
	if err != nil {
		return nil, fmt.Errorf("get pipeline checkpoints: %w", err)
	}
	defer rows.Close()
	out := []PipelineCheckpoint{}
	for rows.Next() {
		var c PipelineCheckpoint
		if err := rows.Scan(&c.SourceKey, &c.Position, &c.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan pipeline checkpoint: %w", err)
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// PipelineCheckpointMap returns the saved positions keyed by source key.
func (db *DB) PipelineCheckpointMap(pipelineID string) (map[string]string, error) {
	list, err := db.GetPipelineCheckpoints(pipelineID)
	if err != nil {
		return nil, err
	}
	m := make(map[string]string, len(list))
	for _, c := range list {
		m[c.SourceKey] = c.Position
	}
	return m, nil
}

// SavePipelineCheckpoint stores (or replaces) one source position.
func (db *DB) SavePipelineCheckpoint(pipelineID, sourceKey, position string) error {
	_, err := db.conn.Exec(
		`INSERT INTO pipeline_checkpoints (pipeline_id, source_key, position, updated_at) VALUES (?, ?, ?, ?)
		 ON CONFLICT(pipeline_id, source_key) DO UPDATE SET position = excluded.position, updated_at = excluded.updated_at`,
		pipelineID, sourceKey, position, time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("save pipeline checkpoint: %w", err)
	}
	return nil
}

// DeletePipelineCheckpoints forgets every saved position of a pipeline, so
// its next start reads the source from the beginning.
func (db *DB) DeletePipelineCheckpoints(pipelineID string) (int64, error) {
	res, err := db.conn.Exec(`DELETE FROM pipeline_checkpoints WHERE pipeline_id = ?`, pipelineID)
	if err != nil {
		return 0, fmt.Errorf("delete pipeline checkpoints: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

// AddPipelineDLQRows adds n to the rows a pipeline sent to its dead-letter table.
func (db *DB) AddPipelineDLQRows(pipelineID string, n int64) error {
	_, err := db.conn.Exec(`UPDATE pipelines SET dlq_rows = dlq_rows + ? WHERE id = ?`, n, pipelineID)
	if err != nil {
		return fmt.Errorf("add pipeline dlq rows: %w", err)
	}
	return nil
}

// GetPipelineDLQRows returns the rows a pipeline sent to its dead-letter table.
func (db *DB) GetPipelineDLQRows(pipelineID string) (int64, error) {
	var n int64
	if err := db.conn.QueryRow(`SELECT COALESCE(dlq_rows, 0) FROM pipelines WHERE id = ?`, pipelineID).Scan(&n); err != nil {
		return 0, fmt.Errorf("get pipeline dlq rows: %w", err)
	}
	return n, nil
}
