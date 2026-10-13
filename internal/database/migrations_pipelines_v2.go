package database

import "fmt"

// migratePipelinesV2 adds persisted source checkpoints and the dead-letter
// counter. Idempotent: safe to run on every start.
func (db *DB) migratePipelinesV2() error {
	// Source positions saved after each written batch, so a restart resumes
	// where the last write ended. source_key names the position within the
	// source (e.g. "watermark", "s3:<prefix>", "kafka:<topic>/<partition>").
	if _, err := db.conn.Exec(`CREATE TABLE IF NOT EXISTS pipeline_checkpoints (
		pipeline_id TEXT NOT NULL REFERENCES pipelines(id) ON DELETE CASCADE,
		source_key TEXT NOT NULL,
		position TEXT NOT NULL,
		updated_at TEXT NOT NULL,
		PRIMARY KEY (pipeline_id, source_key)
	)`); err != nil {
		return fmt.Errorf("create pipeline_checkpoints: %w", err)
	}
	// Rows a pipeline sent to its dead-letter table, across runs.
	if err := db.ensureColumn("pipelines", "dlq_rows", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	return nil
}
