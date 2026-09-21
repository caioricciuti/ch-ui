// SPDX-License-Identifier: BUSL-1.1
package database

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"
)

// IncidentTimelineSchema is installed by the normal database migrations.
var IncidentTimelineSchema = []string{
	`CREATE TABLE IF NOT EXISTS incident_deployment_annotations (
		id TEXT PRIMARY KEY,
		connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
		occurred_at TEXT NOT NULL,
		title TEXT NOT NULL,
		details TEXT NOT NULL DEFAULT '',
		created_by TEXT NOT NULL,
		created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_incident_annotations_conn_time ON incident_deployment_annotations(connection_id, occurred_at)`,
}

type IncidentAnnotation struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	OccurredAt   string `json:"occurred_at"`
	Title        string `json:"title"`
	Details      string `json:"details"`
	CreatedBy    string `json:"created_by"`
	CreatedAt    string `json:"created_at"`
}

func (db *DB) CreateIncidentAnnotation(ctx context.Context, annotation IncidentAnnotation) (IncidentAnnotation, error) {
	annotation.ID = uuid.NewString()
	annotation.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.conn.ExecContext(ctx, `INSERT INTO incident_deployment_annotations
		(id, connection_id, occurred_at, title, details, created_by, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		annotation.ID, annotation.ConnectionID, annotation.OccurredAt, annotation.Title, annotation.Details, annotation.CreatedBy, annotation.CreatedAt)
	return annotation, err
}

func (db *DB) DeleteIncidentAnnotation(ctx context.Context, connectionID, id, actor string, admin bool) error {
	result, err := db.conn.ExecContext(ctx, `DELETE FROM incident_deployment_annotations WHERE connection_id = ? AND id = ? AND (? OR created_by = ?)`, connectionID, id, admin, actor)
	if err != nil {
		return err
	}
	n, err := result.RowsAffected()
	if err == nil && n == 0 {
		return sql.ErrNoRows
	}
	return err
}
