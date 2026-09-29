// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TelemetryMonitor counts rows matching a search over a window and fires an
// alert event when the count crosses a threshold.
type TelemetryMonitor struct {
	ID              string   `json:"id"`
	ConnectionID    string   `json:"connection_id"`
	Name            string   `json:"name"`
	Kind            string   `json:"kind"`
	SourceID        string   `json:"source_id"`
	Query           string   `json:"query"`
	WindowSeconds   int      `json:"window_seconds"`
	IntervalSeconds int      `json:"interval_seconds"`
	Comparator      string   `json:"comparator"`
	Threshold       float64  `json:"threshold"`
	Severity        string   `json:"severity"`
	Enabled         bool     `json:"enabled"`
	LastRunAt       *string  `json:"last_run_at"`
	LastValue       *float64 `json:"last_value"`
	LastState       string   `json:"last_state"`
	LastError       *string  `json:"last_error"`
	CreatedBy       *string  `json:"created_by"`
	CreatedAt       string   `json:"created_at"`
	UpdatedAt       string   `json:"updated_at"`
}

const monitorCols = "id, connection_id, name, kind, source_id, query, window_seconds, interval_seconds, comparator, threshold, severity, enabled, last_run_at, last_value, last_state, last_error, created_by, created_at, updated_at"

func scanMonitor(row interface {
	Scan(dest ...interface{}) error
}) (*TelemetryMonitor, error) {
	var m TelemetryMonitor
	var lastRun, lastErr, createdBy sql.NullString
	var lastValue sql.NullFloat64
	var enabled int
	if err := row.Scan(&m.ID, &m.ConnectionID, &m.Name, &m.Kind, &m.SourceID, &m.Query, &m.WindowSeconds, &m.IntervalSeconds,
		&m.Comparator, &m.Threshold, &m.Severity, &enabled, &lastRun, &lastValue, &m.LastState, &lastErr, &createdBy, &m.CreatedAt, &m.UpdatedAt); err != nil {
		return nil, err
	}
	m.Enabled = enabled == 1
	m.LastRunAt = nullStringToPtr(lastRun)
	m.LastError = nullStringToPtr(lastErr)
	m.CreatedBy = nullStringToPtr(createdBy)
	if lastValue.Valid {
		v := lastValue.Float64
		m.LastValue = &v
	}
	return &m, nil
}

// ListTelemetryMonitors returns the monitors of a connection. An empty
// connection id returns every monitor (used by the background runner).
func (db *DB) ListTelemetryMonitors(connectionID string) ([]TelemetryMonitor, error) {
	var rows *sql.Rows
	var err error
	if connectionID == "" {
		rows, err = db.conn.Query("SELECT " + monitorCols + " FROM telemetry_monitors ORDER BY name COLLATE NOCASE")
	} else {
		rows, err = db.conn.Query("SELECT "+monitorCols+" FROM telemetry_monitors WHERE connection_id = ? ORDER BY name COLLATE NOCASE", connectionID)
	}
	if err != nil {
		return nil, fmt.Errorf("list telemetry monitors: %w", err)
	}
	defer rows.Close()
	out := make([]TelemetryMonitor, 0)
	for rows.Next() {
		m, err := scanMonitor(rows)
		if err != nil {
			return nil, fmt.Errorf("scan telemetry monitor: %w", err)
		}
		out = append(out, *m)
	}
	return out, rows.Err()
}

// GetTelemetryMonitor returns one monitor or nil.
func (db *DB) GetTelemetryMonitor(id string) (*TelemetryMonitor, error) {
	m, err := scanMonitor(db.conn.QueryRow("SELECT "+monitorCols+" FROM telemetry_monitors WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get telemetry monitor: %w", err)
	}
	return m, nil
}

// CreateTelemetryMonitor stores a monitor and returns its id.
func (db *DB) CreateTelemetryMonitor(m *TelemetryMonitor) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := 0
	if m.Enabled {
		enabled = 1
	}
	var createdBy interface{}
	if m.CreatedBy != nil && *m.CreatedBy != "" {
		createdBy = *m.CreatedBy
	}
	_, err := db.conn.Exec(
		`INSERT INTO telemetry_monitors (id, connection_id, name, kind, source_id, query, window_seconds, interval_seconds, comparator, threshold, severity, enabled, last_state, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ok', ?, ?, ?)`,
		id, m.ConnectionID, m.Name, m.Kind, m.SourceID, m.Query, m.WindowSeconds, m.IntervalSeconds, m.Comparator, m.Threshold, m.Severity, enabled, createdBy, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create telemetry monitor: %w", err)
	}
	return id, nil
}

// UpdateTelemetryMonitor replaces the editable fields of a monitor.
func (db *DB) UpdateTelemetryMonitor(m *TelemetryMonitor) error {
	enabled := 0
	if m.Enabled {
		enabled = 1
	}
	res, err := db.conn.Exec(
		`UPDATE telemetry_monitors SET name = ?, kind = ?, source_id = ?, query = ?, window_seconds = ?, interval_seconds = ?, comparator = ?, threshold = ?, severity = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		m.Name, m.Kind, m.SourceID, m.Query, m.WindowSeconds, m.IntervalSeconds, m.Comparator, m.Threshold, m.Severity, enabled, time.Now().UTC().Format(time.RFC3339), m.ID,
	)
	if err != nil {
		return fmt.Errorf("update telemetry monitor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTelemetryMonitor removes a monitor.
func (db *DB) DeleteTelemetryMonitor(id string) error {
	res, err := db.conn.Exec("DELETE FROM telemetry_monitors WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete telemetry monitor: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// RecordTelemetryMonitorRun stores the outcome of an evaluation. lastError
// is empty on success.
func (db *DB) RecordTelemetryMonitorRun(id string, value *float64, state, lastError string) error {
	var v interface{}
	if value != nil {
		v = *value
	}
	var errVal interface{}
	if lastError != "" {
		errVal = lastError
	}
	_, err := db.conn.Exec(
		"UPDATE telemetry_monitors SET last_run_at = ?, last_value = ?, last_state = ?, last_error = ? WHERE id = ?",
		time.Now().UTC().Format(time.RFC3339), v, state, errVal, id,
	)
	if err != nil {
		return fmt.Errorf("record telemetry monitor run: %w", err)
	}
	return nil
}
