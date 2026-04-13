package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Setting keys for governance features.
const (
	SettingGovernanceSyncEnabled     = "governance.sync_enabled"
	SettingGovernanceUpgradeBanner   = "governance.upgrade_banner_dismissed"
	SettingGovernanceSyncUpdatedBy   = "governance.sync_updated_by"
	SettingGovernanceSyncUpdatedAt   = "governance.sync_updated_at"
)

// GovernanceSyncEnabled reports whether admins have opted in to the governance
// background sync. Unset keys default to false (opt-in semantics).
func (db *DB) GovernanceSyncEnabled() bool {
	v, _ := db.GetSetting(SettingGovernanceSyncEnabled)
	return strings.EqualFold(strings.TrimSpace(v), "true")
}

// SetGovernanceSyncEnabled stores the opt-in flag plus who/when toggled it.
func (db *DB) SetGovernanceSyncEnabled(enabled bool, actor string) error {
	val := "false"
	if enabled {
		val = "true"
	}
	if err := db.SetSetting(SettingGovernanceSyncEnabled, val); err != nil {
		return err
	}
	if err := db.SetSetting(SettingGovernanceSyncUpdatedBy, actor); err != nil {
		return err
	}
	return db.SetSetting(SettingGovernanceSyncUpdatedAt, time.Now().UTC().Format(time.RFC3339))
}

// GetSetting retrieves a setting value by key. Returns empty string if not found.
func (db *DB) GetSetting(key string) (string, error) {
	var value string
	err := db.conn.QueryRow("SELECT value FROM settings WHERE key = ?", key).Scan(&value)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get setting: %w", err)
	}
	return value, nil
}

// SetSetting sets or updates a setting value (upsert).
func (db *DB) SetSetting(key, value string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`INSERT INTO settings (key, value, updated_at) VALUES (?, ?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
		key, value, now,
	)
	if err != nil {
		return fmt.Errorf("set setting: %w", err)
	}
	return nil
}

// GetAllSettings retrieves all settings as a map.
func (db *DB) GetAllSettings() (map[string]string, error) {
	rows, err := db.conn.Query("SELECT key, value FROM settings")
	if err != nil {
		return nil, fmt.Errorf("get all settings: %w", err)
	}
	defer rows.Close()

	settings := make(map[string]string)
	for rows.Next() {
		var key, value string
		if err := rows.Scan(&key, &value); err != nil {
			return nil, fmt.Errorf("scan setting: %w", err)
		}
		settings[key] = value
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate setting rows: %w", err)
	}
	return settings, nil
}

// DeleteSetting removes a setting by key.
func (db *DB) DeleteSetting(key string) error {
	_, err := db.conn.Exec("DELETE FROM settings WHERE key = ?", key)
	if err != nil {
		return fmt.Errorf("delete setting: %w", err)
	}
	return nil
}
