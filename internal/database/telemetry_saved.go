package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// TelemetrySavedSearch is a named search on a telemetry source.
type TelemetrySavedSearch struct {
	ID           string  `json:"id"`
	ConnectionID string  `json:"connection_id"`
	Kind         string  `json:"kind"`
	Name         string  `json:"name"`
	Query        string  `json:"query"`
	RangePreset  string  `json:"range_preset"`
	SourceID     *string `json:"source_id"`
	CreatedBy    *string `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

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

const savedSearchCols = "id, connection_id, kind, name, query, range_preset, source_id, created_by, created_at, updated_at"

func scanSavedSearch(row interface {
	Scan(dest ...interface{}) error
}) (*TelemetrySavedSearch, error) {
	var s TelemetrySavedSearch
	var sourceID, createdBy sql.NullString
	if err := row.Scan(&s.ID, &s.ConnectionID, &s.Kind, &s.Name, &s.Query, &s.RangePreset, &sourceID, &createdBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.SourceID = nullStringToPtr(sourceID)
	s.CreatedBy = nullStringToPtr(createdBy)
	return &s, nil
}

// ListTelemetrySavedSearches returns the saved searches of a connection.
func (db *DB) ListTelemetrySavedSearches(connectionID string) ([]TelemetrySavedSearch, error) {
	rows, err := db.conn.Query("SELECT "+savedSearchCols+" FROM telemetry_saved_searches WHERE connection_id = ? ORDER BY kind, name COLLATE NOCASE", connectionID)
	if err != nil {
		return nil, fmt.Errorf("list telemetry saved searches: %w", err)
	}
	defer rows.Close()
	out := make([]TelemetrySavedSearch, 0)
	for rows.Next() {
		s, err := scanSavedSearch(rows)
		if err != nil {
			return nil, fmt.Errorf("scan telemetry saved search: %w", err)
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetTelemetrySavedSearch returns one saved search or nil.
func (db *DB) GetTelemetrySavedSearch(id string) (*TelemetrySavedSearch, error) {
	s, err := scanSavedSearch(db.conn.QueryRow("SELECT "+savedSearchCols+" FROM telemetry_saved_searches WHERE id = ?", id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get telemetry saved search: %w", err)
	}
	return s, nil
}

// CreateTelemetrySavedSearch stores a saved search and returns its id.
func (db *DB) CreateTelemetrySavedSearch(s *TelemetrySavedSearch) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	var sourceID interface{}
	if s.SourceID != nil && *s.SourceID != "" {
		sourceID = *s.SourceID
	}
	var createdBy interface{}
	if s.CreatedBy != nil && *s.CreatedBy != "" {
		createdBy = *s.CreatedBy
	}
	_, err := db.conn.Exec(
		"INSERT INTO telemetry_saved_searches ("+savedSearchCols+") VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		id, s.ConnectionID, s.Kind, s.Name, s.Query, s.RangePreset, sourceID, createdBy, now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create telemetry saved search: %w", err)
	}
	return id, nil
}

// UpdateTelemetrySavedSearch replaces the editable fields of a saved search.
func (db *DB) UpdateTelemetrySavedSearch(s *TelemetrySavedSearch) error {
	var sourceID interface{}
	if s.SourceID != nil && *s.SourceID != "" {
		sourceID = *s.SourceID
	}
	res, err := db.conn.Exec(
		"UPDATE telemetry_saved_searches SET kind = ?, name = ?, query = ?, range_preset = ?, source_id = ?, updated_at = ? WHERE id = ?",
		s.Kind, s.Name, s.Query, s.RangePreset, sourceID, time.Now().UTC().Format(time.RFC3339), s.ID,
	)
	if err != nil {
		return fmt.Errorf("update telemetry saved search: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTelemetrySavedSearch removes a saved search.
func (db *DB) DeleteTelemetrySavedSearch(id string) error {
	res, err := db.conn.Exec("DELETE FROM telemetry_saved_searches WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete telemetry saved search: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
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
