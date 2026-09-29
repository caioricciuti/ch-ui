// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"database/sql"
	"fmt"
	"time"
)

// ── Brain approvals (Pro feature) ──────────────────────────────────

type BrainApproval struct {
	ID          string  `json:"id"`
	ChatID      string  `json:"chat_id"`
	MessageID   string  `json:"message_id"`
	ToolCallID  string  `json:"tool_call_id"`
	ToolName    string  `json:"tool_name"`
	ArgsJSON    string  `json:"args_json"`
	Status      string  `json:"status"`
	RequestedBy *string `json:"requested_by"`
	DecidedBy   *string `json:"decided_by"`
	DecidedAt   *string `json:"decided_at"`
	CreatedAt   string  `json:"created_at"`
}

func (db *DB) CreateBrainApproval(id, chatID, messageID, toolCallID, toolName, argsJSON, requestedBy string) error {
	var reqBy interface{}
	if requestedBy != "" {
		reqBy = requestedBy
	}
	_, err := db.conn.Exec(
		`INSERT INTO brain_approvals (id, chat_id, message_id, tool_call_id, tool_name, args_json, status, requested_by, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, 'pending', ?, ?)`,
		id, chatID, messageID, toolCallID, toolName, argsJSON, reqBy,
		time.Now().UTC().Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("create brain approval: %w", err)
	}
	return nil
}

func (db *DB) MarkBrainApprovalDecided(id, status, decidedBy string) (bool, error) {
	var by interface{}
	if decidedBy != "" {
		by = decidedBy
	}
	res, err := db.conn.Exec(
		`UPDATE brain_approvals SET status = ?, decided_by = ?, decided_at = ?
		 WHERE id = ? AND status = 'pending'`,
		status, by, time.Now().UTC().Format(time.RFC3339), id,
	)
	if err != nil {
		return false, fmt.Errorf("mark brain approval decided: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// DecideBrainApprovalAs records a decision on a pending approval, but only
// for the person who requested it (the chat owner, middleware.Actor). It
// returns false when the approval does not exist, is not pending, or belongs
// to someone else.
func (db *DB) DecideBrainApprovalAs(id, status, actor string) (bool, error) {
	if actor == "" {
		return false, nil
	}
	res, err := db.conn.Exec(
		`UPDATE brain_approvals SET status = ?, decided_by = ?, decided_at = ?
		 WHERE id = ? AND status = 'pending' AND requested_by = ?`,
		status, actor, time.Now().UTC().Format(time.RFC3339), id, actor,
	)
	if err != nil {
		return false, fmt.Errorf("decide brain approval: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (db *DB) GetBrainApprovalByID(id string) (*BrainApproval, error) {
	row := db.conn.QueryRow(
		`SELECT id, chat_id, message_id, tool_call_id, tool_name, args_json, status,
		        requested_by, decided_by, decided_at, created_at
		 FROM brain_approvals WHERE id = ?`,
		id,
	)
	var a BrainApproval
	var reqBy, decBy, decAt sql.NullString
	if err := row.Scan(&a.ID, &a.ChatID, &a.MessageID, &a.ToolCallID, &a.ToolName, &a.ArgsJSON,
		&a.Status, &reqBy, &decBy, &decAt, &a.CreatedAt); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, fmt.Errorf("get brain approval: %w", err)
	}
	a.RequestedBy = nullStringToPtr(reqBy)
	a.DecidedBy = nullStringToPtr(decBy)
	a.DecidedAt = nullStringToPtr(decAt)
	return &a, nil
}

func (db *DB) ListBrainApprovals(status string, limit int) ([]BrainApproval, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	var (
		rows *sql.Rows
		err  error
	)
	if status == "" {
		rows, err = db.conn.Query(
			`SELECT id, chat_id, message_id, tool_call_id, tool_name, args_json, status,
			        requested_by, decided_by, decided_at, created_at
			 FROM brain_approvals ORDER BY created_at DESC LIMIT ?`,
			limit,
		)
	} else {
		rows, err = db.conn.Query(
			`SELECT id, chat_id, message_id, tool_call_id, tool_name, args_json, status,
			        requested_by, decided_by, decided_at, created_at
			 FROM brain_approvals WHERE status = ? ORDER BY created_at DESC LIMIT ?`,
			status, limit,
		)
	}
	if err != nil {
		return nil, fmt.Errorf("list brain approvals: %w", err)
	}
	defer rows.Close()
	out := []BrainApproval{}
	for rows.Next() {
		var a BrainApproval
		var reqBy, decBy, decAt sql.NullString
		if err := rows.Scan(&a.ID, &a.ChatID, &a.MessageID, &a.ToolCallID, &a.ToolName, &a.ArgsJSON,
			&a.Status, &reqBy, &decBy, &decAt, &a.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan brain approval: %w", err)
		}
		a.RequestedBy = nullStringToPtr(reqBy)
		a.DecidedBy = nullStringToPtr(decBy)
		a.DecidedAt = nullStringToPtr(decAt)
		out = append(out, a)
	}
	return out, nil
}

func (db *DB) SweepStaleBrainApprovals(maxAge time.Duration) (int64, error) {
	cutoff := time.Now().UTC().Add(-maxAge).Format(time.RFC3339)
	res, err := db.conn.Exec(
		`UPDATE brain_approvals SET status = 'interrupted', decided_at = ?
		 WHERE status = 'pending' AND created_at < ?`,
		time.Now().UTC().Format(time.RFC3339), cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("sweep brain approvals: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}
