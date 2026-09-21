// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti. See LICENSE.BSL.

package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/caioricciuti/ch-ui/internal/performance"
	"github.com/google/uuid"
)

var PerformanceSchema = []string{
	`CREATE TABLE IF NOT EXISTS performance_investigations (
	 id TEXT PRIMARY KEY, connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
	 title TEXT NOT NULL, query_hash TEXT NOT NULL, database_name TEXT NOT NULL,
	 sample_query TEXT NOT NULL, owner TEXT NOT NULL DEFAULT '', status TEXT NOT NULL DEFAULT 'open'
	 CHECK(status IN ('open','monitoring','resolved')), baseline TEXT NOT NULL,
	 created_by TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, resolved_at TEXT NOT NULL DEFAULT '', revision INTEGER NOT NULL DEFAULT 1
	)`,
	`CREATE INDEX IF NOT EXISTS idx_performance_investigations_conn ON performance_investigations(connection_id, updated_at DESC)`,
	`CREATE TABLE IF NOT EXISTS performance_investigation_events (
	 id TEXT PRIMARY KEY, investigation_id TEXT NOT NULL REFERENCES performance_investigations(id) ON DELETE CASCADE,
	 kind TEXT NOT NULL, actor TEXT NOT NULL, note TEXT NOT NULL DEFAULT '', payload TEXT NOT NULL DEFAULT '{}', created_at TEXT NOT NULL
	)`,
	`CREATE INDEX IF NOT EXISTS idx_performance_events_investigation ON performance_investigation_events(investigation_id, created_at DESC)`,
	`CREATE TABLE IF NOT EXISTS performance_monitor (
	 connection_id TEXT PRIMARY KEY REFERENCES connections(id) ON DELETE CASCADE,
	 enabled INTEGER NOT NULL DEFAULT 0, last_scan_at TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT '',
	 report TEXT, report_at TEXT NOT NULL DEFAULT ''
	)`,
}

type PerformanceInvestigation struct {
	ID           string               `json:"id"`
	ConnectionID string               `json:"connection_id"`
	Title        string               `json:"title"`
	QueryHash    string               `json:"query_hash"`
	Database     string               `json:"database"`
	SampleQuery  string               `json:"sample_query"`
	Owner        string               `json:"owner"`
	Status       string               `json:"status"`
	Baseline     performance.Snapshot `json:"baseline"`
	CreatedBy    string               `json:"created_by"`
	CreatedAt    string               `json:"created_at"`
	UpdatedAt    string               `json:"updated_at"`
	ResolvedAt   string               `json:"resolved_at"`
	Revision     int64                `json:"revision"`
}

type PerformanceEvent struct {
	ID              string          `json:"id"`
	InvestigationID string          `json:"investigation_id"`
	Kind            string          `json:"kind"`
	Actor           string          `json:"actor"`
	Note            string          `json:"note"`
	Payload         json.RawMessage `json:"payload"`
	CreatedAt       string          `json:"created_at"`
}

var ErrPerformanceConflict = errors.New("investigation changed; reload before saving")

const performanceColumns = `id, connection_id, title, query_hash, database_name, sample_query, owner, status, baseline, created_by, created_at, updated_at, resolved_at, revision`

func scanPerformanceInvestigation(row interface{ Scan(...interface{}) error }) (*PerformanceInvestigation, error) {
	var item PerformanceInvestigation
	var baseline string
	if err := row.Scan(&item.ID, &item.ConnectionID, &item.Title, &item.QueryHash, &item.Database, &item.SampleQuery,
		&item.Owner, &item.Status, &baseline, &item.CreatedBy, &item.CreatedAt, &item.UpdatedAt, &item.ResolvedAt, &item.Revision); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(baseline), &item.Baseline); err != nil {
		return nil, fmt.Errorf("decode performance baseline: %w", err)
	}
	return &item, nil
}

func (db *DB) ListPerformanceInvestigations(connectionID string) ([]PerformanceInvestigation, error) {
	rows, err := db.conn.Query("SELECT "+performanceColumns+" FROM performance_investigations WHERE connection_id = ? ORDER BY updated_at DESC", connectionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []PerformanceInvestigation{}
	for rows.Next() {
		item, err := scanPerformanceInvestigation(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, *item)
	}
	return items, rows.Err()
}

// Every lookup/update includes the session's connection, even when the id is
// known. There is no unscoped getter for handlers to accidentally call.
func (db *DB) GetPerformanceInvestigation(connectionID, id string) (*PerformanceInvestigation, error) {
	item, err := scanPerformanceInvestigation(db.conn.QueryRow("SELECT "+performanceColumns+" FROM performance_investigations WHERE connection_id = ? AND id = ?", connectionID, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return item, err
}

func (db *DB) CreatePerformanceInvestigation(item *PerformanceInvestigation, note string) error {
	baseline, err := json.Marshal(item.Baseline)
	if err != nil {
		return err
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	item.ID, item.Status, item.Revision = uuid.NewString(), "open", 1
	item.CreatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	item.UpdatedAt = item.CreatedAt
	_, err = tx.Exec("INSERT INTO performance_investigations ("+performanceColumns+") VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)",
		item.ID, item.ConnectionID, item.Title, item.QueryHash, item.Database, item.SampleQuery, item.Owner, item.Status, string(baseline), item.CreatedBy, item.CreatedAt, item.UpdatedAt, "", item.Revision)
	if err != nil {
		return err
	}
	if err := insertPerformanceEvent(tx, item.ID, "baseline", item.CreatedBy, note, baseline, item.CreatedAt); err != nil {
		return err
	}
	return tx.Commit()
}

func insertPerformanceEvent(tx *sql.Tx, id, kind, actor, note string, payload []byte, now string) error {
	_, err := tx.Exec(`INSERT INTO performance_investigation_events (id, investigation_id, kind, actor, note, payload, created_at) VALUES (?,?,?,?,?,?,?)`,
		uuid.NewString(), id, kind, actor, note, string(payload), now)
	return err
}

// UpdatePerformanceInvestigation keeps the captured baseline immutable and
// records metadata and notes in the same transaction as the update.
func (db *DB) UpdatePerformanceInvestigation(item *PerformanceInvestigation, actor, note string) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(`UPDATE performance_investigations SET title=?, owner=?,
	 resolved_at=CASE WHEN ?='resolved' THEN CASE WHEN status='resolved' THEN resolved_at ELSE ? END ELSE '' END,
	 status=?, updated_at=?, revision=revision+1 WHERE connection_id=? AND id=? AND revision=?`,
		item.Title, item.Owner, item.Status, now, item.Status, now, item.ConnectionID, item.ID, item.Revision)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrPerformanceConflict
	}
	payload, _ := json.Marshal(map[string]string{"title": item.Title, "owner": item.Owner, "status": item.Status})
	if err := insertPerformanceEvent(tx, item.ID, "update", actor, note, payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) AddPerformanceComparison(connectionID, id, actor string, comparison performance.Comparison) error {
	payload, err := json.Marshal(comparison)
	if err != nil {
		return err
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	res, err := tx.Exec(`UPDATE performance_investigations SET updated_at=?, revision=revision+1 WHERE connection_id=? AND id=?`, now, connectionID, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if err := insertPerformanceEvent(tx, id, "comparison", actor, "", payload, now); err != nil {
		return err
	}
	return tx.Commit()
}

func (db *DB) ListPerformanceEvents(connectionID, id string) ([]PerformanceEvent, error) {
	rows, err := db.conn.Query(`SELECT e.id, e.investigation_id, e.kind, e.actor, e.note, e.payload, e.created_at
	 FROM performance_investigation_events e JOIN performance_investigations i ON i.id=e.investigation_id
	 WHERE i.connection_id=? AND i.id=? ORDER BY e.created_at DESC LIMIT 100`, connectionID, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	events := []PerformanceEvent{}
	for rows.Next() {
		var event PerformanceEvent
		var payload string
		if err := rows.Scan(&event.ID, &event.InvestigationID, &event.Kind, &event.Actor, &event.Note, &payload, &event.CreatedAt); err != nil {
			return nil, err
		}
		event.Payload = json.RawMessage(payload)
		events = append(events, event)
	}
	return events, rows.Err()
}

type PerformanceMonitor struct {
	ConnectionID string              `json:"connection_id"`
	Enabled      bool                `json:"enabled"`
	LastScanAt   string              `json:"last_scan_at"`
	LastError    string              `json:"last_error"`
	Report       *performance.Report `json:"report"`
	ReportAt     string              `json:"report_at"`
}

func (db *DB) GetPerformanceMonitor(connectionID string) (PerformanceMonitor, error) {
	item := PerformanceMonitor{ConnectionID: connectionID}
	var report sql.NullString
	err := db.conn.QueryRow(`SELECT enabled, last_scan_at, last_error, report, report_at FROM performance_monitor WHERE connection_id=?`, connectionID).
		Scan(&item.Enabled, &item.LastScanAt, &item.LastError, &report, &item.ReportAt)
	if errors.Is(err, sql.ErrNoRows) {
		return item, nil
	}
	if err != nil {
		return item, err
	}
	if report.Valid {
		if err := json.Unmarshal([]byte(report.String), &item.Report); err != nil {
			return item, err
		}
	}
	return item, nil
}

func (db *DB) SetPerformanceMonitor(connectionID string, enabled bool) error {
	_, err := db.conn.Exec(`INSERT INTO performance_monitor(connection_id,enabled) VALUES (?,?) ON CONFLICT(connection_id) DO UPDATE SET enabled=excluded.enabled`, connectionID, enabled)
	return err
}

func (db *DB) PerformanceMonitorConnections() ([]string, error) {
	rows, err := db.conn.Query(`SELECT connection_id FROM performance_monitor WHERE enabled=1 ORDER BY connection_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// A failed scan preserves the last successful report and its timestamp. Callers
// can distinguish stale results from a healthy scan with zero regressions.
func (db *DB) RecordPerformanceScan(connectionID string, report *performance.Report, scanError string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	if report == nil {
		_, err := db.conn.Exec(`UPDATE performance_monitor SET last_scan_at=?, last_error=? WHERE connection_id=?`, now, scanError, connectionID)
		return err
	}
	payload, err := json.Marshal(report)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`UPDATE performance_monitor SET last_scan_at=?, last_error='', report=?, report_at=? WHERE connection_id=?`, now, string(payload), now, connectionID)
	return err
}
