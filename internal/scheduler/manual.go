// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package scheduler

import (
	"log/slog"
	"time"

	"github.com/caioricciuti/ch-ui/internal/cronexpr"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// ManualResult is the outcome of RunNow.
type ManualResult struct {
	RunID     string
	Status    string // "success" or "error"
	Error     string
	Rows      int
	ElapsedMs int64
}

// RunNow runs a schedule's query once, now, as the given ClickHouse user,
// and records it like a scheduled run: a run row, the schedule's last
// status and its next run. The caller picks the connection and the
// credentials (the person or MCP key asking) and writes its own audit row.
func RunNow(db *database.DB, gw *tunnel.Gateway, schedule *database.Schedule, query, connectionID, user, password string) ManualResult {
	timeout := time.Duration(schedule.TimeoutMs) * time.Millisecond
	if timeout <= 0 {
		timeout = 60 * time.Second
	}

	runID, err := db.CreateScheduleRun(schedule.ID, "running")
	if err != nil {
		slog.Error("Failed to create schedule run", "error", err, "schedule", schedule.ID)
	}

	start := time.Now()
	result, execErr := gw.ExecuteQuery(connectionID, query, user, password, timeout)
	out := ManualResult{RunID: runID, Status: "success", ElapsedMs: time.Since(start).Milliseconds()}
	if execErr != nil {
		out.Status = "error"
		out.Error = execErr.Error()
	} else {
		out.Rows = countRows(result)
	}

	if runID != "" {
		db.UpdateScheduleRun(runID, out.Status, out.Rows, int(out.ElapsedMs), out.Error)
	}
	var nextRun *time.Time
	if schedule.Enabled {
		nextRun = cronexpr.ComputeNextRunInZone(schedule.Cron, schedule.Timezone, time.Now().UTC())
	}
	db.UpdateScheduleStatus(schedule.ID, out.Status, out.Error, nextRun)
	return out
}
