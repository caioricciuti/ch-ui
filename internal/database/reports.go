// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ReportSubscription is a saved query or a dashboard delivered on a cron to
// alert channels.
type ReportSubscription struct {
	ID                  string              `json:"id"`
	Name                string              `json:"name"`
	SourceType          string              `json:"source_type"`
	SavedQueryID        *string             `json:"saved_query_id"`
	DashboardID         *string             `json:"dashboard_id"`
	ConnectionID        string              `json:"connection_id"`
	Cron                string              `json:"cron"`
	Timezone            string              `json:"timezone"`
	Enabled             bool                `json:"enabled"`
	Format              string              `json:"format"`
	RowLimit            int                 `json:"row_limit"`
	InlineRows          int                 `json:"inline_rows"`
	MaxBytes            int                 `json:"max_bytes"`
	TimeoutSeconds      int                 `json:"timeout_seconds"`
	TimeRange           string              `json:"time_range"`
	Variables           map[string][]string `json:"variables"`
	SkipIfEmpty         bool                `json:"skip_if_empty"`
	SubjectTemplate     string              `json:"subject_template"`
	NextRunAt           *string             `json:"next_run_at"`
	LastRunAt           *string             `json:"last_run_at"`
	LastStatus          *string             `json:"last_status"`
	LastError           *string             `json:"last_error"`
	ConsecutiveFailures int                 `json:"consecutive_failures"`
	CreatedBy           *string             `json:"created_by"`
	CreatedAt           string              `json:"created_at"`
	UpdatedAt           string              `json:"updated_at"`
	Targets             []ReportTarget      `json:"targets"`
}

// ReportTarget is one channel a subscription delivers to. Recipients apply
// to email channels; webhooks post to their configured URL.
type ReportTarget struct {
	ChannelID   string   `json:"channel_id"`
	Recipients  []string `json:"recipients"`
	ChannelName string   `json:"channel_name,omitempty"`
	ChannelType string   `json:"channel_type,omitempty"`
}

// ReportDelivery is one run of a subscription.
type ReportDelivery struct {
	ID             string                 `json:"id"`
	SubscriptionID string                 `json:"subscription_id"`
	Trigger        string                 `json:"trigger"`
	Status         string                 `json:"status"`
	Rows           int                    `json:"rows"`
	Bytes          int                    `json:"bytes"`
	Truncated      bool                   `json:"truncated"`
	DurationMs     int64                  `json:"duration_ms"`
	Error          *string                `json:"error"`
	Snapshot       []byte                 `json:"-"`
	HasSnapshot    bool                   `json:"has_snapshot"`
	ChUser         *string                `json:"ch_user"`
	StartedAt      string                 `json:"started_at"`
	FinishedAt     *string                `json:"finished_at"`
	Targets        []ReportDeliveryTarget `json:"targets"`
}

// ReportDeliveryTarget is the send state of one delivery on one channel.
type ReportDeliveryTarget struct {
	DeliveryID        string  `json:"delivery_id"`
	ChannelID         string  `json:"channel_id"`
	ChannelName       string  `json:"channel_name"`
	Status            string  `json:"status"`
	Attempts          int     `json:"attempts"`
	NextAttemptAt     *string `json:"next_attempt_at"`
	LastError         *string `json:"last_error"`
	ProviderMessageID *string `json:"provider_message_id"`
}

// Report delivery target states. queued and retry wait for a send; sending
// is claimed by a runner; sent and failed are final.
const (
	ReportTargetQueued  = "queued"
	ReportTargetSending = "sending"
	ReportTargetRetry   = "retry"
	ReportTargetSent    = "sent"
	ReportTargetFailed  = "failed"
)

const reportSubscriptionColumns = `id, name, source_type, saved_query_id, dashboard_id, connection_id, cron, timezone, enabled,
	format, row_limit, inline_rows, max_bytes, timeout_seconds, time_range, variables_json, skip_if_empty, subject_template,
	next_run_at, last_run_at, last_status, last_error, consecutive_failures, created_by, created_at, updated_at`

func scanReportSubscription(scanner interface{ Scan(...interface{}) error }) (ReportSubscription, error) {
	var s ReportSubscription
	var savedQuery, dashboard, timeRange, vars, subject, next, last, status, lastErr, createdBy sql.NullString
	err := scanner.Scan(&s.ID, &s.Name, &s.SourceType, &savedQuery, &dashboard, &s.ConnectionID, &s.Cron, &s.Timezone, &s.Enabled,
		&s.Format, &s.RowLimit, &s.InlineRows, &s.MaxBytes, &s.TimeoutSeconds, &timeRange, &vars, &s.SkipIfEmpty, &subject,
		&next, &last, &status, &lastErr, &s.ConsecutiveFailures, &createdBy, &s.CreatedAt, &s.UpdatedAt)
	if err != nil {
		return s, err
	}
	s.SavedQueryID = nullStringToPtr(savedQuery)
	s.DashboardID = nullStringToPtr(dashboard)
	s.TimeRange = timeRange.String
	s.SubjectTemplate = subject.String
	s.NextRunAt = nullStringToPtr(next)
	s.LastRunAt = nullStringToPtr(last)
	s.LastStatus = nullStringToPtr(status)
	s.LastError = nullStringToPtr(lastErr)
	s.CreatedBy = nullStringToPtr(createdBy)
	s.Variables = map[string][]string{}
	if vars.Valid && strings.TrimSpace(vars.String) != "" {
		_ = json.Unmarshal([]byte(vars.String), &s.Variables)
	}
	s.Targets = []ReportTarget{}
	return s, nil
}

// ListReportSubscriptions returns every subscription with its targets.
// enabledOnly limits the list to enabled ones.
func (db *DB) ListReportSubscriptions(enabledOnly bool) ([]ReportSubscription, error) {
	q := `SELECT ` + reportSubscriptionColumns + ` FROM report_subscriptions`
	if enabledOnly {
		q += ` WHERE enabled = 1`
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := db.conn.Query(q)
	if err != nil {
		return nil, fmt.Errorf("list report subscriptions: %w", err)
	}
	defer rows.Close()
	out := []ReportSubscription{}
	for rows.Next() {
		s, err := scanReportSubscription(rows)
		if err != nil {
			return nil, fmt.Errorf("scan report subscription: %w", err)
		}
		out = append(out, s)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	targets, err := db.reportTargets("")
	if err != nil {
		return nil, err
	}
	for i := range out {
		if t, ok := targets[out[i].ID]; ok {
			out[i].Targets = t
		}
	}
	return out, nil
}

// GetReportSubscription returns one subscription with its targets, or nil.
func (db *DB) GetReportSubscription(id string) (*ReportSubscription, error) {
	s, err := scanReportSubscription(db.conn.QueryRow(`SELECT `+reportSubscriptionColumns+` FROM report_subscriptions WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get report subscription: %w", err)
	}
	targets, err := db.reportTargets(id)
	if err != nil {
		return nil, err
	}
	if t, ok := targets[id]; ok {
		s.Targets = t
	}
	return &s, nil
}

func (db *DB) reportTargets(subscriptionID string) (map[string][]ReportTarget, error) {
	q := `SELECT t.subscription_id, t.channel_id, t.recipients_json, COALESCE(c.name, ''), COALESCE(c.channel_type, '')
		FROM report_targets t LEFT JOIN alert_channels c ON c.id = t.channel_id`
	args := []interface{}{}
	if subscriptionID != "" {
		q += ` WHERE t.subscription_id = ?`
		args = append(args, subscriptionID)
	}
	q += ` ORDER BY c.name COLLATE NOCASE`
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list report targets: %w", err)
	}
	defer rows.Close()
	out := map[string][]ReportTarget{}
	for rows.Next() {
		var sub, recipients string
		var t ReportTarget
		if err := rows.Scan(&sub, &t.ChannelID, &recipients, &t.ChannelName, &t.ChannelType); err != nil {
			return nil, err
		}
		t.Recipients = parseRecipientsJSON(recipients)
		out[sub] = append(out[sub], t)
	}
	return out, rows.Err()
}

func nullIfBlank(s string) interface{} {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	return s
}

func reportVariablesJSON(vars map[string][]string) interface{} {
	if len(vars) == 0 {
		return nil
	}
	b, err := json.Marshal(vars)
	if err != nil {
		return nil
	}
	return string(b)
}

// CreateReportSubscription stores a subscription and its targets in one
// transaction and returns the new id.
func (db *DB) CreateReportSubscription(s ReportSubscription) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.conn.Begin()
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO report_subscriptions (id, name, source_type, saved_query_id, dashboard_id, connection_id, cron, timezone,
		enabled, format, row_limit, inline_rows, max_bytes, timeout_seconds, time_range, variables_json, skip_if_empty, subject_template,
		next_run_at, consecutive_failures, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 0, ?, ?, ?)`,
		id, s.Name, s.SourceType, s.SavedQueryID, s.DashboardID, s.ConnectionID, s.Cron, s.Timezone,
		s.Enabled, s.Format, s.RowLimit, s.InlineRows, s.MaxBytes, s.TimeoutSeconds, nullIfBlank(s.TimeRange), reportVariablesJSON(s.Variables),
		s.SkipIfEmpty, nullIfBlank(s.SubjectTemplate), s.NextRunAt, s.CreatedBy, now, now)
	if err != nil {
		return "", fmt.Errorf("create report subscription: %w", err)
	}
	if err := replaceReportTargets(tx, id, s.Targets); err != nil {
		return "", err
	}
	return id, tx.Commit()
}

// UpdateReportSubscription replaces the definition and targets of a
// subscription. Enabling it again clears the failure count.
func (db *DB) UpdateReportSubscription(s ReportSubscription) error {
	now := time.Now().UTC().Format(time.RFC3339)
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.Exec(`UPDATE report_subscriptions SET name = ?, source_type = ?, saved_query_id = ?, dashboard_id = ?, connection_id = ?,
		cron = ?, timezone = ?, consecutive_failures = CASE WHEN ? = 1 AND enabled = 0 THEN 0 ELSE consecutive_failures END,
		enabled = ?, format = ?, row_limit = ?, inline_rows = ?, max_bytes = ?, timeout_seconds = ?,
		time_range = ?, variables_json = ?, skip_if_empty = ?, subject_template = ?, next_run_at = ?, updated_at = ?
		WHERE id = ?`,
		s.Name, s.SourceType, s.SavedQueryID, s.DashboardID, s.ConnectionID, s.Cron, s.Timezone, s.Enabled,
		s.Enabled, s.Format, s.RowLimit, s.InlineRows, s.MaxBytes, s.TimeoutSeconds,
		nullIfBlank(s.TimeRange), reportVariablesJSON(s.Variables), s.SkipIfEmpty, nullIfBlank(s.SubjectTemplate), s.NextRunAt, now, s.ID)
	if err != nil {
		return fmt.Errorf("update report subscription: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	if err := replaceReportTargets(tx, s.ID, s.Targets); err != nil {
		return err
	}
	return tx.Commit()
}

func replaceReportTargets(tx *sql.Tx, id string, targets []ReportTarget) error {
	if _, err := tx.Exec(`DELETE FROM report_targets WHERE subscription_id = ?`, id); err != nil {
		return fmt.Errorf("clear report targets: %w", err)
	}
	for _, t := range targets {
		recipients := t.Recipients
		if recipients == nil {
			recipients = []string{}
		}
		raw, _ := json.Marshal(recipients)
		if _, err := tx.Exec(`INSERT INTO report_targets (subscription_id, channel_id, recipients_json) VALUES (?, ?, ?)`, id, t.ChannelID, string(raw)); err != nil {
			return fmt.Errorf("insert report target: %w", err)
		}
	}
	return nil
}

// DeleteReportSubscription removes a subscription, its targets and its
// delivery history.
func (db *DB) DeleteReportSubscription(id string) error {
	res, err := db.conn.Exec(`DELETE FROM report_subscriptions WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete report subscription: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SetReportNextRun moves a subscription to its next slot without recording
// a run.
func (db *DB) SetReportNextRun(id string, next *time.Time) error {
	var v interface{}
	if next != nil {
		v = next.UTC().Format(time.RFC3339)
	}
	_, err := db.conn.Exec(`UPDATE report_subscriptions SET next_run_at = ? WHERE id = ?`, v, id)
	return err
}

// RecordReportOutcome stores the result of a run on its subscription.
// failed counts a failure (reset otherwise, unless counted is false: a
// pending delivery does not change the count yet). disable turns the
// subscription off. It returns the new failure count.
func (db *DB) RecordReportOutcome(id, status, errMsg string, failed, counted, disable bool, at time.Time) (int, error) {
	var lastErr interface{}
	if errMsg != "" {
		lastErr = errMsg
	}
	expr := "consecutive_failures"
	switch {
	case failed:
		expr = "consecutive_failures + 1"
	case counted:
		expr = "0"
	}
	disableExpr := "enabled"
	if disable {
		disableExpr = "0"
	}
	_, err := db.conn.Exec(`UPDATE report_subscriptions SET last_run_at = ?, last_status = ?, last_error = ?,
		consecutive_failures = `+expr+`, enabled = `+disableExpr+` WHERE id = ?`,
		at.UTC().Format(time.RFC3339), status, lastErr, id)
	if err != nil {
		return 0, fmt.Errorf("record report outcome: %w", err)
	}
	var n int
	err = db.conn.QueryRow(`SELECT consecutive_failures FROM report_subscriptions WHERE id = ?`, id).Scan(&n)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return n, err
}

// DisableReportSubscription turns a subscription off.
func (db *DB) DisableReportSubscription(id string) error {
	_, err := db.conn.Exec(`UPDATE report_subscriptions SET enabled = 0 WHERE id = ?`, id)
	return err
}

// CreateReportDelivery records the start of a run.
func (db *DB) CreateReportDelivery(subscriptionID, trigger, chUser string, at time.Time) (string, error) {
	id := uuid.NewString()
	_, err := db.conn.Exec(`INSERT INTO report_deliveries (id, subscription_id, trigger, status, ch_user, started_at) VALUES (?, ?, ?, 'running', ?, ?)`,
		id, subscriptionID, trigger, nullIfBlank(chUser), at.UTC().Format(time.RFC3339))
	if err != nil {
		return "", fmt.Errorf("create report delivery: %w", err)
	}
	return id, nil
}

// ReportDeliveryResult is what a run stores on its delivery.
type ReportDeliveryResult struct {
	Status     string
	Rows       int
	Bytes      int
	Truncated  bool
	DurationMs int64
	Error      string
	Snapshot   []byte
	Finished   bool
	// Targets are created queued, in this order.
	Targets []string
}

// FinishReportDelivery stores the outcome of a run and queues its targets.
func (db *DB) FinishReportDelivery(id string, r ReportDeliveryResult, at time.Time) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var finished, errMsg interface{}
	if r.Finished {
		finished = at.UTC().Format(time.RFC3339)
	}
	if r.Error != "" {
		errMsg = r.Error
	}
	var snapshot interface{}
	if len(r.Snapshot) > 0 {
		snapshot = r.Snapshot
	}
	if _, err := tx.Exec(`UPDATE report_deliveries SET status = ?, rows = ?, bytes = ?, truncated = ?, duration_ms = ?, error = ?, snapshot = ?, finished_at = ? WHERE id = ?`,
		r.Status, r.Rows, r.Bytes, r.Truncated, r.DurationMs, errMsg, snapshot, finished, id); err != nil {
		return fmt.Errorf("finish report delivery: %w", err)
	}
	now := at.UTC().Format(time.RFC3339)
	for _, ch := range r.Targets {
		if _, err := tx.Exec(`INSERT OR IGNORE INTO report_delivery_targets (delivery_id, channel_id, status, attempts, next_attempt_at) VALUES (?, ?, 'queued', 0, ?)`, id, ch, now); err != nil {
			return fmt.Errorf("queue report target: %w", err)
		}
	}
	return tx.Commit()
}

const reportDeliveryColumns = `id, subscription_id, trigger, status, rows, bytes, truncated, duration_ms, error, snapshot IS NOT NULL, ch_user, started_at, finished_at`

func scanReportDelivery(scanner interface{ Scan(...interface{}) error }) (ReportDelivery, error) {
	var d ReportDelivery
	var errMsg, chUser, finished sql.NullString
	err := scanner.Scan(&d.ID, &d.SubscriptionID, &d.Trigger, &d.Status, &d.Rows, &d.Bytes, &d.Truncated, &d.DurationMs, &errMsg, &d.HasSnapshot, &chUser, &d.StartedAt, &finished)
	d.Error = nullStringToPtr(errMsg)
	d.ChUser = nullStringToPtr(chUser)
	d.FinishedAt = nullStringToPtr(finished)
	d.Targets = []ReportDeliveryTarget{}
	return d, err
}

// ListReportDeliveries returns the latest runs of a subscription, newest
// first, with their targets.
func (db *DB) ListReportDeliveries(subscriptionID string, limit int) ([]ReportDelivery, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := db.conn.Query(`SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE subscription_id = ? ORDER BY started_at DESC, rowid DESC LIMIT ?`, subscriptionID, limit)
	if err != nil {
		return nil, fmt.Errorf("list report deliveries: %w", err)
	}
	defer rows.Close()
	out := []ReportDelivery{}
	index := map[string]int{}
	for rows.Next() {
		d, err := scanReportDelivery(rows)
		if err != nil {
			return nil, err
		}
		index[d.ID] = len(out)
		out = append(out, d)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	if len(out) == 0 {
		return out, nil
	}
	trows, err := db.conn.Query(`SELECT t.delivery_id, t.channel_id, COALESCE(c.name, ''), t.status, t.attempts, t.next_attempt_at, t.last_error, t.provider_message_id
		FROM report_delivery_targets t JOIN report_deliveries d ON d.id = t.delivery_id LEFT JOIN alert_channels c ON c.id = t.channel_id
		WHERE d.subscription_id = ?`, subscriptionID)
	if err != nil {
		return nil, err
	}
	defer trows.Close()
	for trows.Next() {
		t, err := scanReportDeliveryTarget(trows)
		if err != nil {
			return nil, err
		}
		if i, ok := index[t.DeliveryID]; ok {
			out[i].Targets = append(out[i].Targets, t)
		}
	}
	return out, trows.Err()
}

func scanReportDeliveryTarget(scanner interface{ Scan(...interface{}) error }) (ReportDeliveryTarget, error) {
	var t ReportDeliveryTarget
	var next, lastErr, msgID sql.NullString
	err := scanner.Scan(&t.DeliveryID, &t.ChannelID, &t.ChannelName, &t.Status, &t.Attempts, &next, &lastErr, &msgID)
	t.NextAttemptAt = nullStringToPtr(next)
	t.LastError = nullStringToPtr(lastErr)
	t.ProviderMessageID = nullStringToPtr(msgID)
	return t, err
}

// GetReportDelivery returns one delivery with its snapshot and targets, or
// nil.
func (db *DB) GetReportDelivery(id string) (*ReportDelivery, error) {
	d, err := scanReportDelivery(db.conn.QueryRow(`SELECT `+reportDeliveryColumns+` FROM report_deliveries WHERE id = ?`, id))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get report delivery: %w", err)
	}
	if d.HasSnapshot {
		if err := db.conn.QueryRow(`SELECT snapshot FROM report_deliveries WHERE id = ?`, id).Scan(&d.Snapshot); err != nil {
			return nil, err
		}
	}
	rows, err := db.conn.Query(`SELECT t.delivery_id, t.channel_id, COALESCE(c.name, ''), t.status, t.attempts, t.next_attempt_at, t.last_error, t.provider_message_id
		FROM report_delivery_targets t LEFT JOIN alert_channels c ON c.id = t.channel_id WHERE t.delivery_id = ?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		t, err := scanReportDeliveryTarget(rows)
		if err != nil {
			return nil, err
		}
		d.Targets = append(d.Targets, t)
	}
	return &d, rows.Err()
}

// DueReportDeliveryTargets returns targets waiting for a send whose time has
// come, oldest first.
func (db *DB) DueReportDeliveryTargets(now time.Time, limit int) ([]ReportDeliveryTarget, error) {
	rows, err := db.conn.Query(`SELECT t.delivery_id, t.channel_id, COALESCE(c.name, ''), t.status, t.attempts, t.next_attempt_at, t.last_error, t.provider_message_id
		FROM report_delivery_targets t LEFT JOIN alert_channels c ON c.id = t.channel_id
		WHERE t.status IN ('queued', 'retry') AND (t.next_attempt_at IS NULL OR t.next_attempt_at <= ?)
		ORDER BY t.next_attempt_at LIMIT ?`, now.UTC().Format(time.RFC3339), limit)
	if err != nil {
		return nil, fmt.Errorf("list due report targets: %w", err)
	}
	defer rows.Close()
	out := []ReportDeliveryTarget{}
	for rows.Next() {
		t, err := scanReportDeliveryTarget(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// ClaimReportDeliveryTarget marks a waiting target as being sent; false
// means another runner has it or it is no longer waiting.
func (db *DB) ClaimReportDeliveryTarget(deliveryID, channelID string) (bool, error) {
	res, err := db.conn.Exec(`UPDATE report_delivery_targets SET status = 'sending' WHERE delivery_id = ? AND channel_id = ? AND status IN ('queued', 'retry')`, deliveryID, channelID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// CompleteReportDeliveryTarget stores the outcome of one send attempt.
func (db *DB) CompleteReportDeliveryTarget(deliveryID, channelID, status string, attempts int, next *time.Time, lastErr, providerID string) error {
	var nextV, errV, idV interface{}
	if next != nil {
		nextV = next.UTC().Format(time.RFC3339)
	}
	if lastErr != "" {
		errV = lastErr
	}
	if providerID != "" {
		idV = providerID
	}
	_, err := db.conn.Exec(`UPDATE report_delivery_targets SET status = ?, attempts = ?, next_attempt_at = ?, last_error = ?, provider_message_id = COALESCE(?, provider_message_id)
		WHERE delivery_id = ? AND channel_id = ?`, status, attempts, nextV, errV, idV, deliveryID, channelID)
	return err
}

// ResetStaleReportTargets puts targets left in 'sending' (a crash or a
// restart mid-send) back in the queue.
func (db *DB) ResetStaleReportTargets() error {
	_, err := db.conn.Exec(`UPDATE report_delivery_targets SET status = 'retry' WHERE status = 'sending'`)
	return err
}

// SettleReportDelivery works out a delivery's status from its targets. When
// no target is waiting it sets the final status and finished_at and returns
// done=true with the number of targets sent and failed.
func (db *DB) SettleReportDelivery(id string, at time.Time) (status string, sent, failed int, done bool, err error) {
	var pending int
	err = db.conn.QueryRow(`SELECT
		COALESCE(SUM(CASE WHEN status = 'sent' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status = 'failed' THEN 1 ELSE 0 END), 0),
		COALESCE(SUM(CASE WHEN status IN ('queued', 'retry', 'sending') THEN 1 ELSE 0 END), 0)
		FROM report_delivery_targets WHERE delivery_id = ?`, id).Scan(&sent, &failed, &pending)
	if err != nil {
		return "", 0, 0, false, err
	}
	switch {
	case pending > 0:
		status = "sending"
	case failed == 0:
		status = "delivered"
	case sent == 0:
		status = "failed"
	default:
		status = "partial"
	}
	if pending > 0 {
		_, err = db.conn.Exec(`UPDATE report_deliveries SET status = ? WHERE id = ?`, status, id)
		return status, sent, failed, false, err
	}
	res, err := db.conn.Exec(`UPDATE report_deliveries SET status = ?, finished_at = ? WHERE id = ? AND finished_at IS NULL`, status, at.UTC().Format(time.RFC3339), id)
	if err != nil {
		return status, sent, failed, false, err
	}
	n, _ := res.RowsAffected()
	// Only the call that finished it reports done, so the outcome is
	// recorded once.
	return status, sent, failed, n == 1, nil
}

// PruneReportDeliveries clears the stored snapshot of deliveries finished
// before snapshotCutoff and deletes deliveries started before
// historyCutoff. A zero historyCutoff keeps every delivery.
func (db *DB) PruneReportDeliveries(snapshotCutoff, historyCutoff time.Time) (cleared, deleted int64, err error) {
	res, err := db.conn.Exec(`UPDATE report_deliveries SET snapshot = NULL WHERE snapshot IS NOT NULL AND finished_at IS NOT NULL AND finished_at < ?`,
		snapshotCutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return 0, 0, err
	}
	cleared, _ = res.RowsAffected()
	if historyCutoff.IsZero() {
		return cleared, 0, nil
	}
	res, err = db.conn.Exec(`DELETE FROM report_deliveries WHERE started_at < ?`, historyCutoff.UTC().Format(time.RFC3339))
	if err != nil {
		return cleared, 0, err
	}
	deleted, _ = res.RowsAffected()
	return cleared, deleted, nil
}
