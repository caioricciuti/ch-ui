// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import "fmt"

// migrateRBACHistoryV2 adds access history: point-in-time copies of a
// connection's ClickHouse access control. Bodies are stored once per state
// hash; snapshot rows point at them. Idempotent: safe to run on every start.
func (db *DB) migrateRBACHistoryV2() error {
	stmts := []struct{ name, sql string }{
		// body is the gzip-compressed JSON of rbac.State: roles, grants,
		// policies, quotas and profiles. It never holds passwords or auth
		// data. Bodies with no snapshot row left are pruned by retention.
		{"rbac_snapshot_bodies", `CREATE TABLE IF NOT EXISTS rbac_snapshot_bodies (
			hash TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL,
			body BLOB NOT NULL,
			bytes INTEGER NOT NULL,
			created_at TEXT NOT NULL
		)`},
		// One row per distinct state seen in a row by one ClickHouse user.
		// Seeing the same state again bumps last_seen_at and seen_count.
		{"rbac_snapshots", `CREATE TABLE IF NOT EXISTS rbac_snapshots (
			id TEXT PRIMARY KEY,
			connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
			hash TEXT NOT NULL,
			first_seen_at TEXT NOT NULL,
			last_seen_at TEXT NOT NULL,
			seen_count INTEGER NOT NULL DEFAULT 1,
			source TEXT NOT NULL CHECK (source IN ('apply','revert','sync','view','manual')),
			actor TEXT NOT NULL DEFAULT '',
			ch_user TEXT NOT NULL DEFAULT '',
			host TEXT NOT NULL DEFAULT '',
			multi_node INTEGER NOT NULL DEFAULT 0,
			plan_hash TEXT NOT NULL DEFAULT '',
			complete INTEGER NOT NULL DEFAULT 1,
			drift TEXT NOT NULL DEFAULT 'none' CHECK (drift IN ('none','outside','mixed')),
			summary TEXT NOT NULL DEFAULT '{}'
		)`},
		{"idx_rbac_snapshots_conn_time", `CREATE INDEX IF NOT EXISTS idx_rbac_snapshots_conn_time ON rbac_snapshots(connection_id, first_seen_at DESC)`},
		{"idx_rbac_snapshots_hash", `CREATE INDEX IF NOT EXISTS idx_rbac_snapshots_hash ON rbac_snapshots(hash)`},
	}
	for _, s := range stmts {
		if _, err := db.conn.Exec(s.sql); err != nil {
			return fmt.Errorf("rbac history v2: create %s: %w", s.name, err)
		}
	}
	return nil
}
