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
