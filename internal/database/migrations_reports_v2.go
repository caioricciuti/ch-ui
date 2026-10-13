// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import "fmt"

// migrateReportsV2 adds the tables behind scheduled report delivery: a
// subscription (a saved query or a dashboard on a cron), its target
// channels, and one delivery row per run with a status per target.
// Idempotent: safe to run on every start.
func (db *DB) migrateReportsV2() error {
	stmts := []struct{ name, sql string }{
		{"report_subscriptions", `CREATE TABLE IF NOT EXISTS report_subscriptions (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			source_type TEXT NOT NULL,
			saved_query_id TEXT REFERENCES saved_queries(id) ON DELETE CASCADE,
			dashboard_id TEXT REFERENCES dashboards(id) ON DELETE CASCADE,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			cron TEXT NOT NULL,
			timezone TEXT NOT NULL DEFAULT 'UTC',
			enabled INTEGER NOT NULL DEFAULT 1,
			format TEXT NOT NULL DEFAULT 'csv',
			row_limit INTEGER NOT NULL DEFAULT 1000,
			inline_rows INTEGER NOT NULL DEFAULT 50,
			max_bytes INTEGER NOT NULL DEFAULT 1048576,
			timeout_seconds INTEGER NOT NULL DEFAULT 60,
			time_range TEXT,
			variables_json TEXT,
			skip_if_empty INTEGER NOT NULL DEFAULT 0,
			subject_template TEXT,
			next_run_at TEXT,
			last_run_at TEXT,
			last_status TEXT,
			last_error TEXT,
			consecutive_failures INTEGER NOT NULL DEFAULT 0,
			created_by TEXT,
			created_at TEXT NOT NULL,
			updated_at TEXT NOT NULL
		)`},
		{"idx_report_subscriptions_next", `CREATE INDEX IF NOT EXISTS idx_report_subscriptions_next ON report_subscriptions(enabled, next_run_at)`},
		{"report_targets", `CREATE TABLE IF NOT EXISTS report_targets (
			subscription_id TEXT NOT NULL REFERENCES report_subscriptions(id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
			recipients_json TEXT NOT NULL DEFAULT '[]',
			PRIMARY KEY (subscription_id, channel_id)
		)`},
		{"report_deliveries", `CREATE TABLE IF NOT EXISTS report_deliveries (
			id TEXT PRIMARY KEY,
			subscription_id TEXT NOT NULL REFERENCES report_subscriptions(id) ON DELETE CASCADE,
			trigger TEXT NOT NULL DEFAULT 'schedule',
			status TEXT NOT NULL,
			rows INTEGER NOT NULL DEFAULT 0,
			bytes INTEGER NOT NULL DEFAULT 0,
			truncated INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			error TEXT,
			snapshot BLOB,
			ch_user TEXT,
			started_at TEXT NOT NULL,
			finished_at TEXT
		)`},
		{"idx_report_deliveries_sub", `CREATE INDEX IF NOT EXISTS idx_report_deliveries_sub ON report_deliveries(subscription_id, started_at)`},
		{"report_delivery_targets", `CREATE TABLE IF NOT EXISTS report_delivery_targets (
			delivery_id TEXT NOT NULL REFERENCES report_deliveries(id) ON DELETE CASCADE,
			channel_id TEXT NOT NULL,
			status TEXT NOT NULL,
			attempts INTEGER NOT NULL DEFAULT 0,
			next_attempt_at TEXT,
			last_error TEXT,
			provider_message_id TEXT,
			PRIMARY KEY (delivery_id, channel_id)
		)`},
		{"idx_report_delivery_targets_due", `CREATE INDEX IF NOT EXISTS idx_report_delivery_targets_due ON report_delivery_targets(status, next_attempt_at)`},
	}
	for _, s := range stmts {
		if _, err := db.conn.Exec(s.sql); err != nil {
			return fmt.Errorf("create %s: %w", s.name, err)
		}
	}
	return nil
}
