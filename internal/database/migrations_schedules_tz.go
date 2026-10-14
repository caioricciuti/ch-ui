// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"fmt"
	"time"

	"github.com/caioricciuti/ch-ui/internal/cronexpr"
)

// settingSchedulesZonedNextRun marks that next_run_at of scheduled query
// jobs has been recomputed in each job's timezone.
const settingSchedulesZonedNextRun = "schedules_zoned_next_run"

// migrateSchedulesZonedNextRun runs recomputeZonedScheduleRuns from now.
func (db *DB) migrateSchedulesZonedNextRun() error {
	return db.recomputeZonedScheduleRuns(time.Now())
}

// recomputeZonedScheduleRuns recomputes next_run_at, once, for enabled
// scheduled query jobs with a timezone other than UTC. Earlier versions read
// every job's cron in UTC whatever its timezone, so the stored next run is
// off by the zone's offset. Runs from now: a slot missed while the server was
// down is not run late for these jobs.
func (db *DB) recomputeZonedScheduleRuns(now time.Time) error {
	done, err := db.GetSetting(settingSchedulesZonedNextRun)
	if err != nil {
		return err
	}
	if done != "" {
		return nil
	}
	schedules, err := db.GetEnabledSchedules()
	if err != nil {
		return fmt.Errorf("schedules timezone migration: %w", err)
	}
	for _, s := range schedules {
		if s.Timezone == "" || s.Timezone == "UTC" {
			continue
		}
		next := cronexpr.ComputeNextRunInZone(s.Cron, s.Timezone, now)
		if err := db.SetScheduleNextRun(s.ID, next); err != nil {
			return fmt.Errorf("schedules timezone migration: %w", err)
		}
	}
	return db.SetSetting(settingSchedulesZonedNextRun, now.UTC().Format(time.RFC3339))
}
