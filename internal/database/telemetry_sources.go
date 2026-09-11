package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/caioricciuti/ch-ui/internal/telemetry"
)

const telemetrySourceCols = "id, connection_id, kind, name, database_name, table_name, config_json, enabled, created_by, created_at, updated_at"

func scanTelemetrySource(row interface {
	Scan(dest ...interface{}) error
}) (*telemetry.Source, error) {
	var s telemetry.Source
	var kind, cfg string
	var createdBy sql.NullString
	var enabled int
	if err := row.Scan(&s.ID, &s.ConnectionID, &kind, &s.Name, &s.Database, &s.Table, &cfg, &enabled, &createdBy, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, err
	}
	s.Kind = telemetry.Kind(kind)
	s.Enabled = enabled == 1
	s.CreatedBy = createdBy.String
	if err := s.UnmarshalConfig(cfg); err != nil {
		return nil, fmt.Errorf("decode telemetry source %s: %w", s.ID, err)
	}
	return &s, nil
}

// ListTelemetrySources returns the sources configured for a connection.
func (db *DB) ListTelemetrySources(connectionID string) ([]telemetry.Source, error) {
	rows, err := db.conn.Query(
		"SELECT "+telemetrySourceCols+" FROM telemetry_sources WHERE connection_id = ? ORDER BY kind, name COLLATE NOCASE",
		connectionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list telemetry sources: %w", err)
	}
	defer rows.Close()
	out := make([]telemetry.Source, 0)
	for rows.Next() {
		s, err := scanTelemetrySource(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetTelemetrySource returns one source or nil.
func (db *DB) GetTelemetrySource(id string) (*telemetry.Source, error) {
	row := db.conn.QueryRow("SELECT "+telemetrySourceCols+" FROM telemetry_sources WHERE id = ?", id)
	s, err := scanTelemetrySource(row)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get telemetry source: %w", err)
	}
	return s, nil
}

// CreateTelemetrySource stores a validated source and returns its id.
func (db *DB) CreateTelemetrySource(s *telemetry.Source) (string, error) {
	cfg, err := s.MarshalConfig()
	if err != nil {
		return "", fmt.Errorf("encode telemetry source: %w", err)
	}
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	enabled := 0
	if s.Enabled {
		enabled = 1
	}
	_, err = db.conn.Exec(
		`INSERT INTO telemetry_sources (`+telemetrySourceCols+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, s.ConnectionID, string(s.Kind), s.Name, s.Database, s.Table, cfg, enabled, nullIfEmpty(s.CreatedBy), now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create telemetry source: %w", err)
	}
	return id, nil
}

// UpdateTelemetrySource replaces every editable field of a source.
func (db *DB) UpdateTelemetrySource(s *telemetry.Source) error {
	cfg, err := s.MarshalConfig()
	if err != nil {
		return fmt.Errorf("encode telemetry source: %w", err)
	}
	enabled := 0
	if s.Enabled {
		enabled = 1
	}
	res, err := db.conn.Exec(
		`UPDATE telemetry_sources SET kind = ?, name = ?, database_name = ?, table_name = ?, config_json = ?, enabled = ?, updated_at = ? WHERE id = ?`,
		string(s.Kind), s.Name, s.Database, s.Table, cfg, enabled, time.Now().UTC().Format(time.RFC3339), s.ID,
	)
	if err != nil {
		return fmt.Errorf("update telemetry source: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// DeleteTelemetrySource removes a source.
func (db *DB) DeleteTelemetrySource(id string) error {
	res, err := db.conn.Exec("DELETE FROM telemetry_sources WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete telemetry source: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

func nullIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}

// MigrateTelemetryConfigToSources turns the old single-table telemetry
// config (one row per connection) into a logs source, once.
func (db *DB) MigrateTelemetryConfigToSources() error {
	rows, err := db.conn.Query("SELECT connection_id, config_json FROM telemetry_config")
	if err != nil {
		return nil // table may not exist on a fresh install
	}
	defer rows.Close()
	type old struct{ conn, cfg string }
	var olds []old
	for rows.Next() {
		var o old
		if err := rows.Scan(&o.conn, &o.cfg); err != nil {
			return err
		}
		olds = append(olds, o)
	}
	for _, o := range olds {
		existing, err := db.ListTelemetrySources(o.conn)
		if err != nil || len(existing) > 0 {
			continue
		}
		var cfg struct {
			LogsDatabase string `json:"logsDatabase"`
			LogsTable    string `json:"logsTable"`
		}
		_ = json.Unmarshal([]byte(o.cfg), &cfg)
		if cfg.LogsDatabase == "" {
			cfg.LogsDatabase = "default"
		}
		if cfg.LogsTable == "" {
			cfg.LogsTable = "otel_logs"
		}
		m := telemetry.DefaultLogsMapping()
		src := &telemetry.Source{
			ConnectionID: o.conn, Kind: telemetry.KindLogs, Name: "Logs",
			Database: cfg.LogsDatabase, Table: cfg.LogsTable, Logs: &m, Enabled: true,
		}
		if err := src.Validate(); err != nil {
			continue
		}
		if _, err := db.CreateTelemetrySource(src); err != nil {
			return err
		}
	}
	return nil
}
