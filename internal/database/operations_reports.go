// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti. See LICENSE.BSL.

package database

import (
	"database/sql"
	"encoding/json"
	"time"

	"github.com/google/uuid"
)

// OperationsReportSchema is additive: existing deployments keep reports off.
var OperationsReportSchema = []string{
	`CREATE TABLE IF NOT EXISTS operations_report_settings (
		connection_id TEXT PRIMARY KEY REFERENCES connections(id) ON DELETE CASCADE,
		config_json TEXT NOT NULL, next_run_at TEXT NOT NULL, revision TEXT NOT NULL DEFAULT '',
		last_attempt_at TEXT NOT NULL DEFAULT '', last_error TEXT NOT NULL DEFAULT ''
	)`,
	`CREATE TABLE IF NOT EXISTS operations_reports (
		id TEXT PRIMARY KEY, connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
		schedule_key TEXT, created_at TEXT NOT NULL, created_by TEXT NOT NULL,
		payload_json TEXT NOT NULL, body_text TEXT NOT NULL,
		channel_id TEXT NOT NULL DEFAULT '', recipients_json TEXT NOT NULL DEFAULT '[]',
		delivery_status TEXT NOT NULL DEFAULT 'not_requested', attempts INTEGER NOT NULL DEFAULT 0,
		next_attempt_at TEXT NOT NULL DEFAULT '', delivery_error TEXT NOT NULL DEFAULT '',
		scheduled_delivery INTEGER NOT NULL DEFAULT 0, schedule_revision TEXT NOT NULL DEFAULT '',
		UNIQUE(connection_id, schedule_key)
	)`,
	`CREATE INDEX IF NOT EXISTS idx_operations_reports_conn_time ON operations_reports(connection_id, created_at)`,
}

type OperationsReportSettings struct {
	Revision      string   `json:"-"`
	ConnectionID  string   `json:"connection_id"`
	Enabled       bool     `json:"enabled"`
	Weekday       int      `json:"weekday"` // Sunday=0; schedule uses UTC.
	Hour          int      `json:"hour"`
	ChannelID     string   `json:"channel_id"`
	Recipients    []string `json:"recipients"`
	NextRunAt     string   `json:"next_run_at"`
	LastAttemptAt string   `json:"last_attempt_at"`
	LastError     string   `json:"last_error"`
}

type OperationsReport struct {
	ScheduledDelivery bool            `json:"-"`
	ScheduleRevision  string          `json:"-"`
	ID                string          `json:"id"`
	ConnectionID      string          `json:"connection_id"`
	CreatedAt         string          `json:"created_at"`
	CreatedBy         string          `json:"created_by"`
	Payload           json.RawMessage `json:"payload"`
	Body              string          `json:"body"`
	DeliveryStatus    string          `json:"delivery_status"`
	DeliveryError     string          `json:"delivery_error"`
	Attempts          int             `json:"attempts"`
	ChannelID         string          `json:"-"`
	RecipientsJSON    string          `json:"-"`
	NextAttemptAt     string          `json:"-"`
}

func (db *DB) GetOperationsReportSettings(conn string) (OperationsReportSettings, error) {
	s := OperationsReportSettings{ConnectionID: conn, Weekday: 1, Hour: 9, Recipients: []string{}}
	var raw string
	err := db.conn.QueryRow(`SELECT config_json, next_run_at, last_attempt_at, last_error, revision FROM operations_report_settings WHERE connection_id=?`, conn).Scan(&raw, &s.NextRunAt, &s.LastAttemptAt, &s.LastError, &s.Revision)
	if err == sql.ErrNoRows {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	var config OperationsReportSettings
	if err := json.Unmarshal([]byte(raw), &config); err != nil {
		return s, err
	}
	s.Enabled, s.Weekday, s.Hour, s.ChannelID, s.Recipients = config.Enabled, config.Weekday, config.Hour, config.ChannelID, config.Recipients
	return s, nil
}

func (db *DB) SaveOperationsReportSettings(s OperationsReportSettings) error {
	raw, err := json.Marshal(s)
	if err != nil {
		return err
	}
	tx, err := db.conn.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	_, err = tx.Exec(`INSERT INTO operations_report_settings(connection_id,config_json,next_run_at,revision) VALUES(?,?,?,?)
		ON CONFLICT(connection_id) DO UPDATE SET config_json=excluded.config_json,next_run_at=excluded.next_run_at,revision=excluded.revision,last_attempt_at='',last_error=''`, s.ConnectionID, string(raw), s.NextRunAt, uuid.NewString())
	if err != nil {
		return err
	}
	{
		_, err = tx.Exec(`UPDATE operations_reports SET delivery_status='canceled' WHERE connection_id=? AND scheduled_delivery=1 AND delivery_status IN ('queued','retry')`, s.ConnectionID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (db *DB) OperationsReportAttempt(conn, revision, next, message string, now time.Time) error {
	_, err := db.conn.Exec(`UPDATE operations_report_settings SET next_run_at=?,last_attempt_at=?,last_error=? WHERE connection_id=? AND revision=?`, next, now.UTC().Format(time.RFC3339), message, conn, revision)
	return err
}

func (db *DB) OperationsReportForSchedule(conn, key string) (*OperationsReport, error) {
	return scanOperationsReport(db.conn.QueryRow(`SELECT `+operationsReportColumns+` FROM operations_reports WHERE connection_id=? AND schedule_key=?`, conn, key))
}

const operationsReportColumns = `id,connection_id,created_at,created_by,payload_json,body_text,delivery_status,delivery_error,attempts,channel_id,recipients_json,next_attempt_at,scheduled_delivery,schedule_revision`

// This predicate is evaluated in the same SQLite statement as queue changes.
const currentReportSchedule = `EXISTS(SELECT 1 FROM operations_report_settings s WHERE s.connection_id=operations_reports.connection_id AND s.revision=operations_reports.schedule_revision AND json_extract(s.config_json,'$.enabled')=1)`

func scanOperationsReport(row interface{ Scan(...any) error }) (*OperationsReport, error) {
	r := &OperationsReport{}
	var raw string
	err := row.Scan(&r.ID, &r.ConnectionID, &r.CreatedAt, &r.CreatedBy, &raw, &r.Body, &r.DeliveryStatus, &r.DeliveryError, &r.Attempts, &r.ChannelID, &r.RecipientsJSON, &r.NextAttemptAt, &r.ScheduledDelivery, &r.ScheduleRevision)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	r.Payload = json.RawMessage(raw)
	return r, nil
}

func (db *DB) SaveOperationsReport(r OperationsReport, scheduleKey string) (*OperationsReport, error) {
	if r.ID == "" {
		r.ID = uuid.NewString()
	}
	if r.CreatedAt == "" {
		r.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if r.DeliveryStatus == "" {
		r.DeliveryStatus = "not_requested"
	}
	if r.RecipientsJSON == "" {
		r.RecipientsJSON = "[]"
	}
	var key any
	if scheduleKey != "" {
		key = scheduleKey
	}
	_, err := db.conn.Exec(`INSERT INTO operations_reports(id,connection_id,schedule_key,created_at,created_by,payload_json,body_text,channel_id,recipients_json,delivery_status,next_attempt_at,scheduled_delivery,schedule_revision)
		VALUES(?,?,?,?,?,?,?,?,?, CASE WHEN ? AND NOT EXISTS(SELECT 1 FROM operations_report_settings WHERE connection_id=? AND revision=? AND json_extract(config_json,'$.enabled')=1) THEN 'canceled' ELSE ? END,?,?,?) ON CONFLICT(connection_id,schedule_key) DO NOTHING`, r.ID, r.ConnectionID, key, r.CreatedAt, r.CreatedBy, string(r.Payload), r.Body, r.ChannelID, r.RecipientsJSON, r.ScheduledDelivery, r.ConnectionID, r.ScheduleRevision, r.DeliveryStatus, r.CreatedAt, r.ScheduledDelivery, r.ScheduleRevision)
	if err != nil {
		return nil, err
	}
	if scheduleKey != "" {
		return db.OperationsReportForSchedule(r.ConnectionID, scheduleKey)
	}
	return &r, nil
}

func (db *DB) ListOperationsReports(conn string, limit int) ([]OperationsReport, error) {
	if limit < 1 || limit > 104 {
		limit = 52
	}
	rows, err := db.conn.Query(`SELECT `+operationsReportColumns+` FROM operations_reports WHERE connection_id=? ORDER BY created_at DESC,id DESC LIMIT ?`, conn, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OperationsReport{}
	for rows.Next() {
		r, err := scanOperationsReport(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

func (db *DB) GetOperationsReport(conn, id string) (*OperationsReport, error) {
	return scanOperationsReport(db.conn.QueryRow(`SELECT `+operationsReportColumns+` FROM operations_reports WHERE connection_id=? AND id=?`, conn, id))
}

func (db *DB) QueueOperationsReport(conn, id, channel string, recipients []string) error {
	raw, err := json.Marshal(recipients)
	if err != nil {
		return err
	}
	_, err = db.conn.Exec(`UPDATE operations_reports SET channel_id=?,recipients_json=?,delivery_status='queued',scheduled_delivery=0,attempts=0,delivery_error='',next_attempt_at=?
		WHERE connection_id=? AND id=? AND delivery_status NOT IN ('queued','retry','sending')`, channel, string(raw), time.Now().UTC().Format(time.RFC3339), conn, id)
	return err
}

func (db *DB) DueOperationsReportDeliveries(now time.Time) ([]OperationsReport, error) {
	if _, err := db.conn.Exec(`UPDATE operations_reports SET delivery_status='canceled' WHERE scheduled_delivery=1 AND NOT `+currentReportSchedule+` AND (delivery_status IN ('queued','retry') OR (delivery_status='sending' AND next_attempt_at<=?))`, now.UTC().Format(time.RFC3339)); err != nil {
		return nil, err
	}
	_, err := db.conn.Exec(`UPDATE operations_reports SET delivery_status='failed',delivery_error='Delivery interrupted after the final attempt; retry manually.' WHERE delivery_status='sending' AND attempts>=5 AND next_attempt_at<=?`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	rows, err := db.conn.Query(`SELECT `+operationsReportColumns+` FROM operations_reports WHERE delivery_status IN ('queued','retry','sending') AND next_attempt_at<=? AND attempts<5 ORDER BY next_attempt_at LIMIT 20`, now.UTC().Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []OperationsReport{}
	for rows.Next() {
		r, e := scanOperationsReport(rows)
		if e != nil {
			return nil, e
		}
		out = append(out, *r)
	}
	return out, rows.Err()
}

// Claim persists a lease before delivery; a crash can be retried after it expires.
func (db *DB) ClaimOperationsReportDelivery(id string, now time.Time) (bool, error) {
	res, err := db.conn.Exec(`UPDATE operations_reports SET delivery_status='sending',attempts=attempts+1,next_attempt_at=?
		WHERE id=? AND delivery_status IN ('queued','retry','sending') AND next_attempt_at<=? AND attempts<5 AND (scheduled_delivery=0 OR `+currentReportSchedule+`)`, now.UTC().Add(5*time.Minute).Format(time.RFC3339), id, now.UTC().Format(time.RFC3339))
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

func (db *DB) CompleteOperationsReportDelivery(id, status, message string, next time.Time) error {
	_, err := db.conn.Exec(`UPDATE operations_reports SET delivery_status=CASE WHEN ?='retry' AND scheduled_delivery=1 AND NOT `+currentReportSchedule+` THEN 'canceled' ELSE ? END,delivery_error=?,next_attempt_at=? WHERE id=?`, status, status, message, next.UTC().Format(time.RFC3339), id)
	return err
}
