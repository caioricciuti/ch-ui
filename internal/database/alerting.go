// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// ── Health check settings ────────────────────────────────────────────────────

// HealthAlertSetting is a stored override of one built-in health check for
// a connection. Nil thresholds mean the level is off.
type HealthAlertSetting struct {
	ConnectionID       string   `json:"connection_id"`
	CheckKind          string   `json:"check_kind"`
	Enabled            bool     `json:"enabled"`
	WarnThreshold      *float64 `json:"warn_threshold"`
	CritThreshold      *float64 `json:"crit_threshold"`
	FreeBytesFloor     *float64 `json:"free_bytes_floor"`
	ForEvaluations     int      `json:"for_evaluations"`
	ResolveEvaluations int      `json:"resolve_evaluations"`
	UpdatedAt          *string  `json:"updated_at"`
}

// ListHealthAlertSettings returns the stored overrides of a connection,
// keyed by check kind. Checks without a row run with their defaults.
func (db *DB) ListHealthAlertSettings(connectionID string) (map[string]HealthAlertSetting, error) {
	rows, err := db.conn.Query(`SELECT connection_id, check_kind, enabled, warn_threshold, crit_threshold,
		free_bytes_floor, for_evaluations, resolve_evaluations, updated_at
		FROM health_alert_settings WHERE connection_id = ?`, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list health alert settings: %w", err)
	}
	defer rows.Close()
	out := map[string]HealthAlertSetting{}
	for rows.Next() {
		var s HealthAlertSetting
		var enabled int
		var warn, crit, floor sql.NullFloat64
		var updated sql.NullString
		if err := rows.Scan(&s.ConnectionID, &s.CheckKind, &enabled, &warn, &crit, &floor,
			&s.ForEvaluations, &s.ResolveEvaluations, &updated); err != nil {
			return nil, fmt.Errorf("scan health alert setting: %w", err)
		}
		s.Enabled = enabled != 0
		s.WarnThreshold = floatPtr(warn)
		s.CritThreshold = floatPtr(crit)
		s.FreeBytesFloor = floatPtr(floor)
		if updated.Valid {
			s.UpdatedAt = &updated.String
		}
		out[s.CheckKind] = s
	}
	return out, rows.Err()
}

// SaveHealthAlertSettings replaces the given checks' overrides in one
// transaction.
func (db *DB) SaveHealthAlertSettings(connectionID string, settings []HealthAlertSetting) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless committed
	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range settings {
		if _, err := tx.Exec(`INSERT INTO health_alert_settings
			(connection_id, check_kind, enabled, warn_threshold, crit_threshold, free_bytes_floor, for_evaluations, resolve_evaluations, updated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
			ON CONFLICT(connection_id, check_kind) DO UPDATE SET enabled = excluded.enabled,
			warn_threshold = excluded.warn_threshold, crit_threshold = excluded.crit_threshold,
			free_bytes_floor = excluded.free_bytes_floor, for_evaluations = excluded.for_evaluations,
			resolve_evaluations = excluded.resolve_evaluations, updated_at = excluded.updated_at`,
			connectionID, s.CheckKind, boolInt(s.Enabled), nullFloat(s.WarnThreshold), nullFloat(s.CritThreshold),
			nullFloat(s.FreeBytesFloor), s.ForEvaluations, s.ResolveEvaluations, now); err != nil {
			return fmt.Errorf("save health alert setting %s: %w", s.CheckKind, err)
		}
	}
	return tx.Commit()
}

// ── Health check state ───────────────────────────────────────────────────────

// HealthAlertState is the state machine of one (connection, check, node).
type HealthAlertState struct {
	ConnectionID string   `json:"connection_id"`
	CheckKind    string   `json:"check_kind"`
	Node         string   `json:"node"`
	State        string   `json:"state"`
	Severity     *string  `json:"severity"`
	BreachCount  int      `json:"breach_count"`
	OkCount      int      `json:"ok_count"`
	LastValue    *float64 `json:"last_value"`
	Since        *string  `json:"since"`
	LastEventID  *string  `json:"last_event_id"`
	UpdatedAt    *string  `json:"updated_at"`
}

const healthStateColumns = `connection_id, check_kind, node, state, severity, breach_count, ok_count, last_value, since, last_event_id, updated_at`

func scanHealthState(scan func(...interface{}) error) (HealthAlertState, error) {
	var s HealthAlertState
	var sev, since, eventID, updated sql.NullString
	var value sql.NullFloat64
	if err := scan(&s.ConnectionID, &s.CheckKind, &s.Node, &s.State, &sev, &s.BreachCount, &s.OkCount,
		&value, &since, &eventID, &updated); err != nil {
		return s, err
	}
	s.Severity = strPtrOf(sev)
	s.Since = strPtrOf(since)
	s.LastEventID = strPtrOf(eventID)
	s.UpdatedAt = strPtrOf(updated)
	s.LastValue = floatPtr(value)
	return s, nil
}

// ListHealthAlertStates returns every stored state of a connection. With
// activeOnly, only pending and firing rows.
func (db *DB) ListHealthAlertStates(connectionID string, activeOnly bool) ([]HealthAlertState, error) {
	q := `SELECT ` + healthStateColumns + ` FROM health_alert_state WHERE connection_id = ?`
	if activeOnly {
		q += ` AND state IN ('pending', 'firing')`
	}
	q += ` ORDER BY check_kind, node`
	rows, err := db.conn.Query(q, connectionID)
	if err != nil {
		return nil, fmt.Errorf("list health alert states: %w", err)
	}
	defer rows.Close()
	out := []HealthAlertState{}
	for rows.Next() {
		s, err := scanHealthState(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan health alert state: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// SaveHealthAlertStates upserts states in one transaction. A state of a
// check that is disabled on its connection is not written, so a poll that
// started before the check was turned off cannot bring its rows back.
func (db *DB) SaveHealthAlertStates(states []HealthAlertState) error {
	if len(states) == 0 {
		return nil
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless committed
	now := time.Now().UTC().Format(time.RFC3339)
	for _, s := range states {
		if _, err := tx.Exec(`INSERT INTO health_alert_state (`+healthStateColumns+`)
			SELECT ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?
			WHERE NOT EXISTS (SELECT 1 FROM health_alert_settings
				WHERE connection_id = ? AND check_kind = ? AND enabled = 0)
			ON CONFLICT(connection_id, check_kind, node) DO UPDATE SET state = excluded.state,
			severity = excluded.severity, breach_count = excluded.breach_count, ok_count = excluded.ok_count,
			last_value = excluded.last_value, since = excluded.since, last_event_id = excluded.last_event_id,
			updated_at = excluded.updated_at`,
			s.ConnectionID, s.CheckKind, s.Node, s.State, nullStr(s.Severity), s.BreachCount, s.OkCount,
			nullFloat(s.LastValue), nullStr(s.Since), nullStr(s.LastEventID), now,
			s.ConnectionID, s.CheckKind); err != nil {
			return fmt.Errorf("save health alert state: %w", err)
		}
	}
	return tx.Commit()
}

// DeleteHealthAlertStates clears the state of one check on a connection,
// for a check that was turned off. The caller raises the resolved events of
// the incidents it held first.
func (db *DB) DeleteHealthAlertStates(connectionID, checkKind string) error {
	if _, err := db.conn.Exec(`DELETE FROM health_alert_state WHERE connection_id = ? AND check_kind = ?`, connectionID, checkKind); err != nil {
		return fmt.Errorf("delete health alert state: %w", err)
	}
	return nil
}

// DeleteHealthAlertState clears the state of one check on one node, for a
// node that left the cluster.
func (db *DB) DeleteHealthAlertState(connectionID, checkKind, node string) error {
	if _, err := db.conn.Exec(`DELETE FROM health_alert_state WHERE connection_id = ? AND check_kind = ? AND node = ?`,
		connectionID, checkKind, node); err != nil {
		return fmt.Errorf("delete health alert state: %w", err)
	}
	return nil
}

// ── SQL alerts ───────────────────────────────────────────────────────────────

// SQLAlert is a user-defined SQL threshold alert.
type SQLAlert struct {
	ID                 string   `json:"id"`
	ConnectionID       string   `json:"connection_id"`
	Name               string   `json:"name"`
	SQL                string   `json:"sql"`
	Mode               string   `json:"mode"`
	Comparator         string   `json:"comparator"`
	Threshold          float64  `json:"threshold"`
	Severity           string   `json:"severity"`
	IntervalSeconds    int      `json:"interval_seconds"`
	TimeoutSeconds     int      `json:"timeout_seconds"`
	ForEvaluations     int      `json:"for_evaluations"`
	ResolveEvaluations int      `json:"resolve_evaluations"`
	Enabled            bool     `json:"enabled"`
	State              string   `json:"state"`
	BreachCount        int      `json:"breach_count"`
	OkCount            int      `json:"ok_count"`
	ErrorCount         int      `json:"error_count"`
	LastRunAt          *string  `json:"last_run_at"`
	LastValue          *float64 `json:"last_value"`
	LastError          *string  `json:"last_error"`
	CreatedBy          *string  `json:"created_by"`
	CreatedAt          string   `json:"created_at"`
	UpdatedAt          string   `json:"updated_at"`
}

const sqlAlertColumns = `id, connection_id, name, sql, mode, comparator, threshold, severity, interval_seconds,
	timeout_seconds, for_evaluations, resolve_evaluations, enabled, state, breach_count, ok_count, error_count,
	last_run_at, last_value, last_error, created_by, created_at, updated_at`

func scanSQLAlert(scan func(...interface{}) error) (SQLAlert, error) {
	var a SQLAlert
	var enabled int
	var lastRun, lastErr, createdBy sql.NullString
	var lastValue sql.NullFloat64
	err := scan(&a.ID, &a.ConnectionID, &a.Name, &a.SQL, &a.Mode, &a.Comparator, &a.Threshold, &a.Severity,
		&a.IntervalSeconds, &a.TimeoutSeconds, &a.ForEvaluations, &a.ResolveEvaluations, &enabled, &a.State,
		&a.BreachCount, &a.OkCount, &a.ErrorCount, &lastRun, &lastValue, &lastErr, &createdBy, &a.CreatedAt, &a.UpdatedAt)
	a.Enabled = enabled != 0
	a.LastRunAt = strPtrOf(lastRun)
	a.LastValue = floatPtr(lastValue)
	a.LastError = strPtrOf(lastErr)
	a.CreatedBy = strPtrOf(createdBy)
	return a, err
}

// ListSQLAlerts returns the SQL alerts of a connection, or of every
// connection when connectionID is empty.
func (db *DB) ListSQLAlerts(connectionID string) ([]SQLAlert, error) {
	q := `SELECT ` + sqlAlertColumns + ` FROM sql_alerts`
	args := []interface{}{}
	if connectionID != "" {
		q += ` WHERE connection_id = ?`
		args = append(args, connectionID)
	}
	q += ` ORDER BY name COLLATE NOCASE`
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list sql alerts: %w", err)
	}
	defer rows.Close()
	out := []SQLAlert{}
	for rows.Next() {
		a, err := scanSQLAlert(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan sql alert: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetSQLAlert returns one SQL alert, or nil when it does not exist.
func (db *DB) GetSQLAlert(id string) (*SQLAlert, error) {
	a, err := scanSQLAlert(db.conn.QueryRow(`SELECT `+sqlAlertColumns+` FROM sql_alerts WHERE id = ?`, id).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get sql alert: %w", err)
	}
	return &a, nil
}

// CreateSQLAlert stores a new SQL alert in the ok state.
func (db *DB) CreateSQLAlert(a *SQLAlert) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(`INSERT INTO sql_alerts (id, connection_id, name, sql, mode, comparator, threshold, severity,
		interval_seconds, timeout_seconds, for_evaluations, resolve_evaluations, enabled, state, created_by, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'ok', ?, ?, ?)`,
		id, a.ConnectionID, strings.TrimSpace(a.Name), a.SQL, a.Mode, a.Comparator, a.Threshold, a.Severity,
		a.IntervalSeconds, a.TimeoutSeconds, a.ForEvaluations, a.ResolveEvaluations, boolInt(a.Enabled), nullStr(a.CreatedBy), now, now)
	if err != nil {
		return "", fmt.Errorf("create sql alert: %w", err)
	}
	return id, nil
}

// ErrSQLAlertStateChanged is returned by UpdateSQLAlert when the alert's
// state moved since it was loaded: reload it and try again.
var ErrSQLAlertStateChanged = errors.New("sql alert state changed")

// UpdateSQLAlert replaces the definition of a SQL alert. A changed query or
// condition starts over: counters reset and the state returns to ok, so the
// old incident does not carry over to a different alert. A disabled alert
// holds no incident either: its state returns to ok, its errors are kept.
// The write only applies while the state is still a.State, so the caller
// knows which incident it ended; otherwise ErrSQLAlertStateChanged.
func (db *DB) UpdateSQLAlert(a *SQLAlert, resetState bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	q := `UPDATE sql_alerts SET name = ?, sql = ?, mode = ?, comparator = ?, threshold = ?, severity = ?,
		interval_seconds = ?, timeout_seconds = ?, for_evaluations = ?, resolve_evaluations = ?, enabled = ?, updated_at = ?`
	if resetState {
		q += `, state = 'ok', breach_count = 0, ok_count = 0, error_count = 0, last_error = NULL`
	} else {
		q += `, error_count = CASE WHEN ? = 1 THEN 0 ELSE error_count END`
		if !a.Enabled {
			q += `, state = 'ok', breach_count = 0, ok_count = 0`
		}
	}
	q += ` WHERE id = ? AND state = ?`
	args := []interface{}{strings.TrimSpace(a.Name), a.SQL, a.Mode, a.Comparator, a.Threshold, a.Severity,
		a.IntervalSeconds, a.TimeoutSeconds, a.ForEvaluations, a.ResolveEvaluations, boolInt(a.Enabled), now}
	if !resetState {
		args = append(args, boolInt(a.Enabled))
	}
	args = append(args, a.ID, a.State)
	res, err := db.conn.Exec(q, args...)
	if err != nil {
		return fmt.Errorf("update sql alert: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := db.conn.QueryRow(`SELECT COUNT(*) FROM sql_alerts WHERE id = ?`, a.ID).Scan(&exists); err != nil {
			return fmt.Errorf("update sql alert: %w", err)
		}
		if exists > 0 {
			return ErrSQLAlertStateChanged
		}
		return sql.ErrNoRows
	}
	return nil
}

// DeleteSQLAlert removes a SQL alert and its evaluation history.
func (db *DB) DeleteSQLAlert(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM alert_evaluations WHERE source = 'sql' AND source_id = ?`, id); err != nil {
		return fmt.Errorf("delete sql alert evaluations: %w", err)
	}
	res, err := db.conn.Exec(`DELETE FROM sql_alerts WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete sql alert: %w", err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// SQLAlertRun is the outcome of one evaluation, written back to the alert.
type SQLAlertRun struct {
	State       string
	BreachCount int
	OkCount     int
	ErrorCount  int
	Value       *float64
	Error       string
	Disable     bool
}

// RecordSQLAlertRun stores an evaluation's outcome on the alert. The last
// value is kept across error runs. The write only applies while the alert is
// still as loaded (a): an edit, a disable, a delete or another run in the
// meantime wins, and applied is false so the caller raises no event.
func (db *DB) RecordSQLAlertRun(a *SQLAlert, run SQLAlertRun) (applied bool, err error) {
	now := time.Now().UTC().Format(time.RFC3339)
	q := `UPDATE sql_alerts SET state = ?, breach_count = ?, ok_count = ?, error_count = ?, last_run_at = ?,
		last_error = ?, last_value = COALESCE(?, last_value)`
	if run.Disable {
		q += `, enabled = 0`
	}
	q += ` WHERE id = ? AND updated_at = ? AND state = ? AND breach_count = ? AND ok_count = ? AND error_count = ? AND enabled = ?`
	res, err := db.conn.Exec(q, run.State, run.BreachCount, run.OkCount, run.ErrorCount, now,
		nullableString(run.Error), nullFloat(run.Value),
		a.ID, a.UpdatedAt, a.State, a.BreachCount, a.OkCount, a.ErrorCount, boolInt(a.Enabled))
	if err != nil {
		return false, fmt.Errorf("record sql alert run: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// RevertSQLAlertRun undoes a run RecordSQLAlertRun applied, for when the
// run's alert event could not be written: the alert returns to before (as
// loaded for the run, including its last run time), so the next tick
// evaluates it again. It only applies while the alert still holds run.
func (db *DB) RevertSQLAlertRun(before *SQLAlert, run SQLAlertRun) (bool, error) {
	enabledAfter := before.Enabled && !run.Disable
	res, err := db.conn.Exec(`UPDATE sql_alerts SET state = ?, breach_count = ?, ok_count = ?, error_count = ?,
		last_run_at = ?, last_error = ?, last_value = ?, enabled = ?
		WHERE id = ? AND updated_at = ? AND state = ? AND breach_count = ? AND ok_count = ? AND error_count = ? AND enabled = ?`,
		before.State, before.BreachCount, before.OkCount, before.ErrorCount,
		nullStr(before.LastRunAt), nullStr(before.LastError), nullFloat(before.LastValue), boolInt(before.Enabled),
		before.ID, before.UpdatedAt, run.State, run.BreachCount, run.OkCount, run.ErrorCount, boolInt(enabledAfter))
	if err != nil {
		return false, fmt.Errorf("revert sql alert run: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// SetSQLAlertSkipped stores why an alert could not run (no account for its
// connection) as its last error, without counting a run: state, counters
// and last run time stay, so the alert stays due and runs once the cause is
// fixed. It only applies while the alert is still as loaded (a).
func (db *DB) SetSQLAlertSkipped(a *SQLAlert, reason string) (bool, error) {
	res, err := db.conn.Exec(`UPDATE sql_alerts SET last_error = ?
		WHERE id = ? AND updated_at = ? AND state = ? AND breach_count = ? AND ok_count = ? AND error_count = ? AND enabled = ?`,
		nullableString(reason), a.ID, a.UpdatedAt, a.State, a.BreachCount, a.OkCount, a.ErrorCount, boolInt(a.Enabled))
	if err != nil {
		return false, fmt.Errorf("record skipped sql alert: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ── Evaluation history ───────────────────────────────────────────────────────

// AlertEvaluation is one recorded evaluation of a health check node or a SQL
// alert.
type AlertEvaluation struct {
	ID           int64    `json:"id"`
	Source       string   `json:"source"`
	SourceID     string   `json:"source_id"`
	ConnectionID string   `json:"connection_id"`
	Node         string   `json:"node"`
	Value        *float64 `json:"value"`
	State        string   `json:"state"`
	Error        string   `json:"error"`
	DurationMs   int64    `json:"duration_ms"`
	EvaluatedAt  string   `json:"evaluated_at"`
}

// InsertAlertEvaluations appends history rows in one transaction.
func (db *DB) InsertAlertEvaluations(evals []AlertEvaluation) error {
	if len(evals) == 0 {
		return nil
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless committed
	for _, e := range evals {
		at := e.EvaluatedAt
		if at == "" {
			at = time.Now().UTC().Format(time.RFC3339)
		}
		if _, err := tx.Exec(`INSERT INTO alert_evaluations
			(source, source_id, connection_id, node, value, state, error, duration_ms, evaluated_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			e.Source, e.SourceID, e.ConnectionID, e.Node, nullFloat(e.Value), e.State, e.Error, e.DurationMs, at); err != nil {
			return fmt.Errorf("insert alert evaluation: %w", err)
		}
	}
	return tx.Commit()
}

// LastAlertEvaluationAt returns when the last history row of a source (and
// node) was written, or "" when there is none.
func (db *DB) LastAlertEvaluationAt(source, sourceID, node string) (string, error) {
	var at sql.NullString
	err := db.conn.QueryRow(`SELECT MAX(evaluated_at) FROM alert_evaluations WHERE source = ? AND source_id = ? AND node = ?`,
		source, sourceID, node).Scan(&at)
	if err != nil {
		return "", err
	}
	return at.String, nil
}

// ListAlertEvaluations returns the newest history rows of a source on a
// connection. An empty sourceID lists every row of the source.
func (db *DB) ListAlertEvaluations(connectionID, source, sourceID string, limit int) ([]AlertEvaluation, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT id, source, source_id, connection_id, node, value, state, error, duration_ms, evaluated_at
		FROM alert_evaluations WHERE connection_id = ?`
	args := []interface{}{connectionID}
	if source != "" {
		q += ` AND source = ?`
		args = append(args, source)
	}
	if sourceID != "" {
		q += ` AND source_id = ?`
		args = append(args, sourceID)
	}
	q += ` ORDER BY evaluated_at DESC, id DESC LIMIT ?`
	args = append(args, limit)
	rows, err := db.conn.Query(q, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert evaluations: %w", err)
	}
	defer rows.Close()
	out := []AlertEvaluation{}
	for rows.Next() {
		var e AlertEvaluation
		var value sql.NullFloat64
		if err := rows.Scan(&e.ID, &e.Source, &e.SourceID, &e.ConnectionID, &e.Node, &value, &e.State, &e.Error,
			&e.DurationMs, &e.EvaluatedAt); err != nil {
			return nil, fmt.Errorf("scan alert evaluation: %w", err)
		}
		e.Value = floatPtr(value)
		out = append(out, e)
	}
	return out, rows.Err()
}

// PruneAlertEvaluations deletes history rows older than before (RFC3339).
func (db *DB) PruneAlertEvaluations(before string) (int64, error) {
	res, err := db.conn.Exec(`DELETE FROM alert_evaluations WHERE evaluated_at < ?`, before)
	if err != nil {
		return 0, fmt.Errorf("prune alert evaluations: %w", err)
	}
	return res.RowsAffected()
}

// ── helpers ──────────────────────────────────────────────────────────────────

func floatPtr(v sql.NullFloat64) *float64 {
	if !v.Valid {
		return nil
	}
	f := v.Float64
	return &f
}

func strPtrOf(v sql.NullString) *string {
	if !v.Valid {
		return nil
	}
	s := v.String
	return &s
}

func nullFloat(v *float64) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func nullStr(v *string) interface{} {
	if v == nil {
		return nil
	}
	return *v
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
