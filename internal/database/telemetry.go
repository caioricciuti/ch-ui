package database

import (
	"fmt"
	"time"

	"github.com/google/uuid"
)

type TelemetryConfig struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	ConfigJSON   string `json:"config_json"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`
}

func (db *DB) GetTelemetryConfig(connectionID string) (*TelemetryConfig, error) {
	row := db.conn.QueryRow(
		`SELECT id, connection_id, config_json, created_at, updated_at
		 FROM telemetry_config WHERE connection_id = ?`, connectionID,
	)

	var c TelemetryConfig
	err := row.Scan(&c.ID, &c.ConnectionID, &c.ConfigJSON, &c.CreatedAt, &c.UpdatedAt)
	if err != nil {
		return nil, nil
	}
	return &c, nil
}

func (db *DB) SaveTelemetryConfig(connectionID, configJSON string) error {
	now := time.Now().UTC().Format(time.RFC3339)

	existing, _ := db.GetTelemetryConfig(connectionID)
	if existing != nil {
		_, err := db.conn.Exec(
			`UPDATE telemetry_config SET config_json = ?, updated_at = ? WHERE connection_id = ?`,
			configJSON, now, connectionID,
		)
		if err != nil {
			return fmt.Errorf("update telemetry config: %w", err)
		}
		return nil
	}

	id := uuid.NewString()
	_, err := db.conn.Exec(
		`INSERT INTO telemetry_config (id, connection_id, config_json, created_at, updated_at) VALUES (?, ?, ?, ?, ?)`,
		id, connectionID, configJSON, now, now,
	)
	if err != nil {
		return fmt.Errorf("create telemetry config: %w", err)
	}
	return nil
}
