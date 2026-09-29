// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
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
	SettingGovernanceQueryHarvestKey = "governance.query_harvest_mode"
)

// Query harvest modes: "auto" harvests a connection's query log only when at
// least one policy exists for it, "always" harvests unconditionally, "off"
// disables query-log harvesting.
const (
	QueryHarvestModeAuto   = "auto"
	QueryHarvestModeAlways = "always"
	QueryHarvestModeOff    = "off"
)

// QueryHarvestMode returns the configured query-harvest mode, defaulting to
// "auto" when unset or invalid.
func (db *DB) QueryHarvestMode() string {
	v, _ := db.GetSetting(SettingGovernanceQueryHarvestKey)
	switch strings.ToLower(strings.TrimSpace(v)) {
	case QueryHarvestModeAlways:
		return QueryHarvestModeAlways
	case QueryHarvestModeOff:
		return QueryHarvestModeOff
	default:
		return QueryHarvestModeAuto
	}
}

// SetQueryHarvestMode validates and persists the query-harvest mode.
func (db *DB) SetQueryHarvestMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case QueryHarvestModeAuto, QueryHarvestModeAlways, QueryHarvestModeOff:
		return db.SetSetting(SettingGovernanceQueryHarvestKey, mode)
	default:
		return fmt.Errorf("query_harvest_mode must be auto, always, or off")
	}
}

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
