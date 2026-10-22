// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"fmt"
	"testing"
	"time"
)

func historyConn(t *testing.T, db *DB, name string) string {
	t.Helper()
	id, err := db.CreateConnection(CreateConnectionParams{Name: name, TunnelToken: name + "-token"})
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func historyInput(conn, user, hash string) RBACSnapshotInput {
	return RBACSnapshotInput{ConnectionID: conn, Hash: hash, Source: "view", CHUser: user, Complete: true, Body: []byte("body-" + hash)}
}

func historyCount(t *testing.T, db *DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.conn.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func TestRBACHistoryDedupAndBodyReuse(t *testing.T) {
	db := openTestDB(t)
	conn := historyConn(t, db, "hist")

	first, created, err := db.RecordRBACSnapshot(historyInput(conn, "admin", "h1"), nil)
	if err != nil || !created {
		t.Fatalf("first record: created=%v err=%v", created, err)
	}
	again, created, err := db.RecordRBACSnapshot(historyInput(conn, "admin", "h1"), func(RBACHistoryContext) (string, string, error) {
		t.Fatal("an unchanged state must not be classified")
		return "", "", nil
	})
	if err != nil || created || again.ID != first.ID || again.SeenCount != 2 {
		t.Fatalf("same hash should bump: created=%v row=%+v err=%v", created, again, err)
	}

	var seen RBACHistoryContext
	if _, created, err = db.RecordRBACSnapshot(historyInput(conn, "admin", "h2"), func(hc RBACHistoryContext) (string, string, error) {
		seen = hc
		return "outside", `{"added":{"grant":1}}`, nil
	}); err != nil || !created {
		t.Fatalf("new hash: created=%v err=%v", created, err)
	}
	if seen.Previous == nil || seen.Previous.ID != first.ID || string(seen.PreviousBody) != "body-h1" {
		t.Fatalf("classifier context = %+v", seen)
	}
	// Back to h1: a new row, the body is reused.
	if _, created, err = db.RecordRBACSnapshot(historyInput(conn, "admin", "h1"), nil); err != nil || !created {
		t.Fatalf("back to h1: created=%v err=%v", created, err)
	}
	// Another ClickHouse user is compared only with its own rows.
	if _, created, err = db.RecordRBACSnapshot(historyInput(conn, "gov", "h1"), func(hc RBACHistoryContext) (string, string, error) {
		if hc.Previous != nil {
			t.Fatalf("gov has no previous row, got %+v", hc.Previous)
		}
		return "none", "{}", nil
	}); err != nil || !created {
		t.Fatalf("other user: created=%v err=%v", created, err)
	}

	if n := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE connection_id = ?`, conn); n != 4 {
		t.Fatalf("rows = %d, want 4", n)
	}
	if n := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshot_bodies`); n != 2 {
		t.Fatalf("bodies = %d, want 2", n)
	}

	rows, next, err := db.ListRBACSnapshots(RBACHistoryFilter{ConnectionID: conn, Limit: 3})
	if err != nil || len(rows) != 3 || next == "" || rows[0].CHUser != "gov" {
		t.Fatalf("page 1: %d rows next=%q err=%v", len(rows), next, err)
	}
	rest, next, err := db.ListRBACSnapshots(RBACHistoryFilter{ConnectionID: conn, Limit: 3, Cursor: next})
	if err != nil || len(rest) != 1 || next != "" || rest[0].ID != first.ID {
		t.Fatalf("page 2: %+v next=%q err=%v", rest, next, err)
	}
	drift, _, err := db.ListRBACSnapshots(RBACHistoryFilter{ConnectionID: conn, DriftOnly: true})
	if err != nil || len(drift) != 1 || drift[0].Hash != "h2" {
		t.Fatalf("drift only: %+v err=%v", drift, err)
	}
	if n, err := db.CountRBACDrift(conn, time.Now().Add(-time.Hour)); err != nil || n != 1 {
		t.Fatalf("drift count = %d err=%v", n, err)
	}

	prev, body, err := db.PreviousRBACSnapshot(&drift[0])
	if err != nil || prev.ID != first.ID || string(body) != "body-h1" {
		t.Fatalf("previous: %+v err=%v", prev, err)
	}
	if _, _, err := db.GetRBACSnapshot("other-connection", first.ID); err != ErrRBACSnapshotNotFound {
		t.Fatalf("another connection's row must not load: %v", err)
	}
}

func TestRBACHistoryAppliesFromOtherUsers(t *testing.T) {
	db := openTestDB(t)
	conn := historyConn(t, db, "applies")
	base := time.Now().UTC().Add(-time.Hour)
	if _, _, err := db.recordRBACSnapshotAt(historyInput(conn, "gov", "h1"), nil, base); err != nil {
		t.Fatal(err)
	}
	apply := historyInput(conn, "admin", "h2")
	apply.Source = "apply"
	if _, _, err := db.recordRBACSnapshotAt(apply, nil, base.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	var applies []RBACSnapshot
	if _, _, err := db.recordRBACSnapshotAt(historyInput(conn, "gov", "h2"), func(hc RBACHistoryContext) (string, string, error) {
		applies = hc.Applies
		return "none", "{}", nil
	}, base.Add(2*time.Minute)); err != nil {
		t.Fatal(err)
	}
	if len(applies) != 1 || applies[0].Source != "apply" || applies[0].CHUser != "admin" {
		t.Fatalf("applies = %+v", applies)
	}
}

func TestRBACHistoryRetention(t *testing.T) {
	db := openTestDB(t)
	old := historyConn(t, db, "old")
	busy := historyConn(t, db, "busy")
	now := time.Now().UTC()

	// Every row of old is past retention; only its newest survives.
	for i, days := range []int{200, 190, 181} {
		if _, _, err := db.recordRBACSnapshotAt(historyInput(old, "admin", fmt.Sprintf("old-%d", i)), nil, now.AddDate(0, 0, -days)); err != nil {
			t.Fatal(err)
		}
	}
	// busy has more rows than the cap, all recent.
	tx, err := db.conn.Begin()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < RBACHistoryMaxRows+5; i++ {
		at := now.Add(time.Duration(i-RBACHistoryMaxRows-5) * time.Minute).Format(rbacTimeFormat)
		if _, err := tx.Exec(`INSERT INTO rbac_snapshots (id, connection_id, hash, first_seen_at, last_seen_at, source) VALUES (?, ?, 'busy', ?, ?, 'sync')`,
			fmt.Sprintf("busy-%03d", i), busy, at, at); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(`INSERT INTO rbac_snapshot_bodies (hash, connection_id, body, bytes, created_at) VALUES ('busy', ?, x'00', 1, '')`, busy); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	n, err := db.PruneRBACSnapshots(now)
	if err != nil {
		t.Fatal(err)
	}
	if n != 2+5 {
		t.Fatalf("pruned %d rows, want 7", n)
	}
	rows, _, err := db.ListRBACSnapshots(RBACHistoryFilter{ConnectionID: old})
	if err != nil || len(rows) != 1 || rows[0].Hash != "old-2" {
		t.Fatalf("old connection keeps its newest row: %+v err=%v", rows, err)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE connection_id = ?`, busy); got != RBACHistoryMaxRows {
		t.Fatalf("busy rows = %d", got)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE id = 'busy-000'`); got != 0 {
		t.Fatal("the oldest busy row should be pruned")
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshot_bodies WHERE hash IN ('old-0', 'old-1')`); got != 0 {
		t.Fatalf("orphan bodies left: %d", got)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshot_bodies WHERE hash IN ('old-2', 'busy')`); got != 2 {
		t.Fatalf("bodies in use removed: %d left", got)
	}

	// Recording a new row prunes its connection too, so history stays
	// bounded without the governance syncer.
	if _, created, err := db.RecordRBACSnapshot(historyInput(busy, "admin", "fresh"), nil); err != nil || !created {
		t.Fatalf("record: created=%v err=%v", created, err)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE connection_id = ?`, busy); got != RBACHistoryMaxRows {
		t.Fatalf("busy rows after record = %d", got)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE hash = 'fresh'`); got != 1 {
		t.Fatal("the new row must be kept")
	}
}

func TestRBACHistoryRetentionFollowsConfig(t *testing.T) {
	db := openTestDB(t)
	conn := historyConn(t, db, "cfg")
	now := time.Now().UTC()

	// 0 keeps rows forever, both when a row is recorded and in the sweep.
	cfg := DefaultRetentionConfig()
	cfg.RBACSnapshots = 0
	if err := db.SetRetentionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	for i, days := range []int{400, 300, 100} {
		if _, _, err := db.recordRBACSnapshotAt(historyInput(conn, "admin", fmt.Sprintf("h-%d", i)), nil, now.AddDate(0, 0, -days)); err != nil {
			t.Fatal(err)
		}
	}
	if n, err := db.PruneRBACSnapshots(now); err != nil || n != 0 {
		t.Fatalf("retention 0 pruned %d rows, err=%v", n, err)
	}
	if _, _, err := db.RecordRBACSnapshot(historyInput(conn, "admin", "h-new"), nil); err != nil {
		t.Fatal(err)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE connection_id = ?`, conn); got != 4 {
		t.Fatalf("rows with retention 0 = %d, want 4", got)
	}

	// A 200 day window drops the 400 and 300 day old rows.
	cfg.RBACSnapshots = 200
	if err := db.SetRetentionConfig(cfg); err != nil {
		t.Fatal(err)
	}
	if n, err := db.PruneRBACSnapshots(now); err != nil || n != 2 {
		t.Fatalf("200 day window pruned %d rows, want 2, err=%v", n, err)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE hash IN ('h-2', 'h-new')`); got != 2 {
		t.Fatalf("recent rows kept = %d, want 2", got)
	}
}

func TestRBACHistoryCascadesOnConnectionDelete(t *testing.T) {
	db := openTestDB(t)
	conn := historyConn(t, db, "gone")
	if _, _, err := db.RecordRBACSnapshot(historyInput(conn, "admin", "h1"), nil); err != nil {
		t.Fatal(err)
	}
	if err := db.DeleteConnection(conn); err != nil {
		t.Fatal(err)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshots WHERE connection_id = ?`, conn); got != 0 {
		t.Fatalf("rows left after delete: %d", got)
	}
	if _, err := db.PruneRBACSnapshots(time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := historyCount(t, db, `SELECT COUNT(*) FROM rbac_snapshot_bodies`); got != 0 {
		t.Fatalf("bodies left after prune: %d", got)
	}
}
