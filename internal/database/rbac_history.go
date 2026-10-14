// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Access history retention: rows not seen for this long are pruned, and a
// connection keeps at most this many rows. Its newest row is always kept.
const (
	RBACHistoryRetentionDays = 180
	RBACHistoryMaxRows       = 500
)

// rbacTimeFormat is fixed width so timestamps sort as text.
const rbacTimeFormat = "2006-01-02T15:04:05.000000Z"

// ErrRBACSnapshotNotFound is returned for an unknown snapshot id.
var ErrRBACSnapshotNotFound = errors.New("snapshot not found")

// RBACSnapshot is one access history row: a state seen on a connection by
// one ClickHouse user, from first_seen_at until last_seen_at.
type RBACSnapshot struct {
	ID           string `json:"id"`
	ConnectionID string `json:"connection_id"`
	Hash         string `json:"hash"`
	FirstSeenAt  string `json:"first_seen_at"`
	LastSeenAt   string `json:"last_seen_at"`
	SeenCount    int    `json:"seen_count"`
	Source       string `json:"source"`
	Actor        string `json:"actor"`
	CHUser       string `json:"ch_user"`
	Host         string `json:"host"`
	MultiNode    bool   `json:"multi_node"`
	PlanHash     string `json:"plan_hash"`
	Complete     bool   `json:"complete"`
	Drift        string `json:"drift"`
	// Summary is a JSON object; see accesshistory.Summary.
	Summary string `json:"summary"`
}

// RBACSnapshotInput is a state to record.
type RBACSnapshotInput struct {
	ConnectionID string
	Hash         string
	Source       string
	Actor        string
	CHUser       string
	Host         string
	MultiNode    bool
	PlanHash     string
	Complete     bool
	// Body is the gzip JSON of the state, stored once per hash.
	Body []byte
}

// RBACHistoryContext is what a classifier sees when a new row is about to
// be written. Previous is the newest complete row of the same ClickHouse
// user, with its body; Applies are the apply and revert rows of other users
// recorded since Previous was last seen.
type RBACHistoryContext struct {
	Previous     *RBACSnapshot
	PreviousBody []byte
	Applies      []RBACSnapshot
}

// RBACClassifier returns the drift and summary JSON of a new row.
type RBACClassifier func(RBACHistoryContext) (drift, summary string, err error)

const rbacSnapshotColumns = `id, connection_id, hash, first_seen_at, last_seen_at, seen_count, source, actor, ch_user, host,
	multi_node, plan_hash, complete, drift, summary`

func scanRBACSnapshot(r rowScanner, extra ...any) (RBACSnapshot, error) {
	var s RBACSnapshot
	var multi, complete int
	dest := append([]any{&s.ID, &s.ConnectionID, &s.Hash, &s.FirstSeenAt, &s.LastSeenAt, &s.SeenCount, &s.Source, &s.Actor, &s.CHUser, &s.Host,
		&multi, &s.PlanHash, &complete, &s.Drift, &s.Summary}, extra...)
	if err := r.Scan(dest...); err != nil {
		return s, err
	}
	s.MultiNode, s.Complete = multi != 0, complete != 0
	return s, nil
}

// RecordRBACSnapshot writes one observation of a state. When the newest row
// of the same connection and ClickHouse user has the same hash, only its
// last_seen_at and seen_count move; otherwise classify decides the drift
// and summary and a new row is inserted, reusing the body by hash. It
// returns the row and whether it is new.
func (db *DB) RecordRBACSnapshot(in RBACSnapshotInput, classify RBACClassifier) (*RBACSnapshot, bool, error) {
	return db.recordRBACSnapshotAt(in, classify, time.Now().UTC())
}

func (db *DB) recordRBACSnapshotAt(in RBACSnapshotInput, classify RBACClassifier, at time.Time) (*RBACSnapshot, bool, error) {
	now := at.UTC().Format(rbacTimeFormat)
	var out *RBACSnapshot
	inserted := false
	err := db.inImmediateTx(func(ctx context.Context, q sqlRunner) error {
		newest, err := scanRBACSnapshot(q.QueryRowContext(ctx,
			`SELECT `+rbacSnapshotColumns+` FROM rbac_snapshots WHERE connection_id = ? AND ch_user = ?
			 ORDER BY first_seen_at DESC, rowid DESC LIMIT 1`, in.ConnectionID, in.CHUser))
		switch {
		case err == nil && newest.Hash == in.Hash:
			if _, err := q.ExecContext(ctx, `UPDATE rbac_snapshots SET last_seen_at = ?, seen_count = seen_count + 1 WHERE id = ?`, now, newest.ID); err != nil {
				return fmt.Errorf("bump snapshot: %w", err)
			}
			newest.LastSeenAt = now
			newest.SeenCount++
			out = &newest
			return nil
		case err != nil && !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read newest snapshot: %w", err)
		}

		hc := RBACHistoryContext{}
		prev, err := scanRBACSnapshot(q.QueryRowContext(ctx,
			`SELECT `+rbacSnapshotColumns+` FROM rbac_snapshots WHERE connection_id = ? AND ch_user = ? AND complete = 1
			 ORDER BY first_seen_at DESC, rowid DESC LIMIT 1`, in.ConnectionID, in.CHUser))
		switch {
		case err == nil:
			hc.Previous = &prev
			if err := q.QueryRowContext(ctx, `SELECT body FROM rbac_snapshot_bodies WHERE hash = ?`, prev.Hash).Scan(&hc.PreviousBody); err != nil && !errors.Is(err, sql.ErrNoRows) {
				return fmt.Errorf("read previous body: %w", err)
			}
			rows, err := q.QueryContext(ctx,
				`SELECT `+rbacSnapshotColumns+` FROM rbac_snapshots WHERE connection_id = ? AND ch_user <> ?
				 AND source IN ('apply','revert') AND first_seen_at >= ? ORDER BY first_seen_at, rowid`,
				in.ConnectionID, in.CHUser, prev.LastSeenAt)
			if err != nil {
				return fmt.Errorf("read applies: %w", err)
			}
			for rows.Next() {
				a, err := scanRBACSnapshot(rows)
				if err != nil {
					rows.Close()
					return err
				}
				hc.Applies = append(hc.Applies, a)
			}
			rows.Close()
			if err := rows.Err(); err != nil {
				return err
			}
		case !errors.Is(err, sql.ErrNoRows):
			return fmt.Errorf("read previous snapshot: %w", err)
		}

		drift, summary := "none", "{}"
		if classify != nil {
			if drift, summary, err = classify(hc); err != nil {
				return err
			}
		}
		if _, err := q.ExecContext(ctx,
			`INSERT OR IGNORE INTO rbac_snapshot_bodies (hash, connection_id, body, bytes, created_at) VALUES (?, ?, ?, ?, ?)`,
			in.Hash, in.ConnectionID, in.Body, len(in.Body), now); err != nil {
			return fmt.Errorf("insert body: %w", err)
		}
		row := RBACSnapshot{
			ID: uuid.NewString(), ConnectionID: in.ConnectionID, Hash: in.Hash, FirstSeenAt: now, LastSeenAt: now, SeenCount: 1,
			Source: in.Source, Actor: in.Actor, CHUser: in.CHUser, Host: in.Host, MultiNode: in.MultiNode, PlanHash: in.PlanHash,
			Complete: in.Complete, Drift: drift, Summary: summary,
		}
		if _, err := q.ExecContext(ctx,
			`INSERT INTO rbac_snapshots (`+rbacSnapshotColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			row.ID, row.ConnectionID, row.Hash, row.FirstSeenAt, row.LastSeenAt, row.SeenCount, row.Source, row.Actor, row.CHUser, row.Host,
			boolInt(row.MultiNode), row.PlanHash, boolInt(row.Complete), row.Drift, row.Summary); err != nil {
			return fmt.Errorf("insert snapshot: %w", err)
		}
		if _, err := pruneRBACSnapshotsOn(ctx, q, at, in.ConnectionID); err != nil {
			return err
		}
		out, inserted = &row, true
		return nil
	})
	if err != nil {
		return nil, false, fmt.Errorf("record access snapshot: %w", err)
	}
	return out, inserted, nil
}

// RBACHistoryFilter selects history rows of one connection, newest first.
// Cursor is the NextCursor of the previous page.
type RBACHistoryFilter struct {
	ConnectionID string
	DriftOnly    bool
	Limit        int
	Cursor       string
}

// ListRBACSnapshots returns one page of history and the cursor of the next
// page ("" at the end).
func (db *DB) ListRBACSnapshots(f RBACHistoryFilter) ([]RBACSnapshot, string, error) {
	if f.Limit <= 0 || f.Limit > 200 {
		f.Limit = 50
	}
	where := []string{"connection_id = ?"}
	args := []any{f.ConnectionID}
	if f.DriftOnly {
		where = append(where, "drift IN ('outside','mixed')")
	}
	if f.Cursor != "" {
		at, rowid, ok := strings.Cut(f.Cursor, "|")
		n, err := strconv.ParseInt(rowid, 10, 64)
		if !ok || err != nil {
			return nil, "", fmt.Errorf("invalid cursor")
		}
		where = append(where, "(first_seen_at < ? OR (first_seen_at = ? AND rowid < ?))")
		args = append(args, at, at, n)
	}
	args = append(args, f.Limit+1)
	rows, err := db.conn.Query(`SELECT `+rbacSnapshotColumns+`, rowid FROM rbac_snapshots WHERE `+strings.Join(where, " AND ")+`
		ORDER BY first_seen_at DESC, rowid DESC LIMIT ?`, args...)
	if err != nil {
		return nil, "", fmt.Errorf("list access snapshots: %w", err)
	}
	defer rows.Close()
	out := []RBACSnapshot{}
	var rowids []int64
	for rows.Next() {
		var rowid int64
		s, err := scanRBACSnapshot(rows, &rowid)
		if err != nil {
			return nil, "", err
		}
		out = append(out, s)
		rowids = append(rowids, rowid)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > f.Limit {
		out = out[:f.Limit]
		next = out[f.Limit-1].FirstSeenAt + "|" + strconv.FormatInt(rowids[f.Limit-1], 10)
	}
	return out, next, nil
}

// GetRBACSnapshot returns one row of a connection with its body.
func (db *DB) GetRBACSnapshot(connectionID, id string) (*RBACSnapshot, []byte, error) {
	s, err := scanRBACSnapshot(db.conn.QueryRow(`SELECT `+rbacSnapshotColumns+` FROM rbac_snapshots WHERE id = ? AND connection_id = ?`, id, connectionID))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrRBACSnapshotNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("get access snapshot: %w", err)
	}
	var body []byte
	if err := db.conn.QueryRow(`SELECT body FROM rbac_snapshot_bodies WHERE hash = ?`, s.Hash).Scan(&body); err != nil {
		return nil, nil, fmt.Errorf("get access snapshot body: %w", err)
	}
	return &s, body, nil
}

// PreviousRBACSnapshot returns the row recorded before s by the same
// ClickHouse user, or ErrRBACSnapshotNotFound.
func (db *DB) PreviousRBACSnapshot(s *RBACSnapshot) (*RBACSnapshot, []byte, error) {
	var id string
	err := db.conn.QueryRow(`SELECT id FROM rbac_snapshots WHERE connection_id = ? AND ch_user = ?
		AND (first_seen_at < ? OR (first_seen_at = ? AND rowid < (SELECT rowid FROM rbac_snapshots WHERE id = ?)))
		ORDER BY first_seen_at DESC, rowid DESC LIMIT 1`, s.ConnectionID, s.CHUser, s.FirstSeenAt, s.FirstSeenAt, s.ID).Scan(&id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrRBACSnapshotNotFound
	}
	if err != nil {
		return nil, nil, fmt.Errorf("previous access snapshot: %w", err)
	}
	return db.GetRBACSnapshot(s.ConnectionID, id)
}

// CountRBACDrift counts rows with outside or mixed drift recorded since.
func (db *DB) CountRBACDrift(connectionID string, since time.Time) (int, error) {
	var n int
	err := db.conn.QueryRow(`SELECT COUNT(*) FROM rbac_snapshots WHERE connection_id = ? AND drift IN ('outside','mixed') AND first_seen_at >= ?`,
		connectionID, since.UTC().Format(rbacTimeFormat)).Scan(&n)
	if err != nil {
		return 0, fmt.Errorf("count access drift: %w", err)
	}
	return n, nil
}

// PruneRBACSnapshots applies access history retention to every connection:
// rows last seen more than RBACHistoryRetentionDays ago and rows beyond the
// newest RBACHistoryMaxRows go, but each connection's newest row always
// stays. Bodies no row points at are removed too. Recording a new row
// prunes its own connection the same way, so history stays bounded when the
// governance syncer is off.
func (db *DB) PruneRBACSnapshots(now time.Time) (int64, error) {
	return pruneRBACSnapshotsOn(context.Background(), db.conn, now, "")
}

func pruneRBACSnapshotsOn(ctx context.Context, q sqlRunner, now time.Time, connectionID string) (int64, error) {
	cutoff := now.UTC().AddDate(0, 0, -RBACHistoryRetentionDays).Format(rbacTimeFormat)
	scope, args := "", []any{}
	if connectionID != "" {
		scope, args = "WHERE connection_id = ?", append(args, connectionID)
	}
	args = append(args, RBACHistoryMaxRows, cutoff)
	res, err := q.ExecContext(ctx, `DELETE FROM rbac_snapshots WHERE id IN (
		SELECT id FROM (
			SELECT id, last_seen_at, ROW_NUMBER() OVER (PARTITION BY connection_id ORDER BY first_seen_at DESC, rowid DESC) AS rn
			FROM rbac_snapshots `+scope+`
		) WHERE rn > 1 AND (rn > ? OR last_seen_at < ?)
	)`, args...)
	if err != nil {
		return 0, fmt.Errorf("prune access snapshots: %w", err)
	}
	n, _ := res.RowsAffected()
	if _, err := q.ExecContext(ctx, `DELETE FROM rbac_snapshot_bodies WHERE hash NOT IN (SELECT hash FROM rbac_snapshots)`); err != nil {
		return n, fmt.Errorf("prune access snapshot bodies: %w", err)
	}
	return n, nil
}
