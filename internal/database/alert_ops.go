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

// Alert event statuses beyond 'new' and 'processed': the event was held
// back by a silence or an acknowledgement and created no deliveries.
const (
	AlertEventSilenced     = "silenced"
	AlertEventAcknowledged = "acknowledged"
)

// ── Rule scope ───────────────────────────────────────────────────────────────

// SetAlertRuleConnections stores the connections a rule is limited to; nil
// or empty clears the scope (every connection).
func (db *DB) SetAlertRuleConnections(ruleID string, ids []string) error {
	var val interface{}
	if len(ids) > 0 {
		data, err := json.Marshal(ids)
		if err != nil {
			return fmt.Errorf("marshal rule connections: %w", err)
		}
		val = string(data)
	}
	if _, err := db.conn.Exec(`UPDATE alert_rules SET connection_ids = ? WHERE id = ?`, val, ruleID); err != nil {
		return fmt.Errorf("set alert rule connections: %w", err)
	}
	return nil
}

func parseConnectionIDs(v sql.NullString) []string {
	if !v.Valid || strings.TrimSpace(v.String) == "" {
		return nil
	}
	var ids []string
	if err := json.Unmarshal([]byte(v.String), &ids); err != nil || len(ids) == 0 {
		return nil
	}
	return ids
}

// MissingConnectionIDs returns the ids that name no connection.
func (db *DB) MissingConnectionIDs(ids []string) ([]string, error) {
	var missing []string
	for _, id := range ids {
		var n int
		if err := db.conn.QueryRow(`SELECT COUNT(*) FROM connections WHERE id = ?`, id).Scan(&n); err != nil {
			return nil, fmt.Errorf("check connection: %w", err)
		}
		if n == 0 {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

// ── Silences ─────────────────────────────────────────────────────────────────

// AlertSilence holds back the events its matchers select while its window
// is open. Every matcher that is set must match; at least one is set.
type AlertSilence struct {
	ID           string  `json:"id"`
	Reason       string  `json:"reason"`
	ConnectionID *string `json:"connection_id"`
	EventType    *string `json:"event_type"`
	SourceRef    *string `json:"source_ref"`
	Node         *string `json:"node"`
	StartsAt     string  `json:"starts_at"`
	EndsAt       string  `json:"ends_at"`
	CreatedBy    *string `json:"created_by"`
	CreatedAt    string  `json:"created_at"`
	ExpiredAt    *string `json:"expired_at"`
	ExpiredBy    *string `json:"expired_by"`
	Status       string  `json:"status"`
}

// Silence statuses, derived from the window.
const (
	SilenceActive    = "active"
	SilenceScheduled = "scheduled"
	SilenceExpired   = "expired"
)

// SilenceStatus is the status of a silence at now.
func SilenceStatus(s AlertSilence, now time.Time) string {
	if s.ExpiredAt != nil {
		return SilenceExpired
	}
	end, err := time.Parse(time.RFC3339, s.EndsAt)
	if err != nil || !now.Before(end) {
		return SilenceExpired
	}
	if start, err := time.Parse(time.RFC3339, s.StartsAt); err == nil && now.Before(start) {
		return SilenceScheduled
	}
	return SilenceActive
}

const silenceColumns = `id, reason, connection_id, event_type, source_ref, node, starts_at, ends_at, created_by, created_at, expired_at, expired_by`

func scanSilence(scan func(...interface{}) error) (AlertSilence, error) {
	var s AlertSilence
	var conn, et, ref, node, by, expAt, expBy sql.NullString
	if err := scan(&s.ID, &s.Reason, &conn, &et, &ref, &node, &s.StartsAt, &s.EndsAt, &by, &s.CreatedAt, &expAt, &expBy); err != nil {
		return s, err
	}
	s.ConnectionID, s.EventType, s.SourceRef, s.Node = strPtrOf(conn), strPtrOf(et), strPtrOf(ref), strPtrOf(node)
	s.CreatedBy, s.ExpiredAt, s.ExpiredBy = strPtrOf(by), strPtrOf(expAt), strPtrOf(expBy)
	s.Status = SilenceStatus(s, time.Now().UTC())
	return s, nil
}

func (db *DB) querySilences(where string, args ...interface{}) ([]AlertSilence, error) {
	rows, err := db.conn.Query(`SELECT `+silenceColumns+` FROM alert_silences `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert silences: %w", err)
	}
	defer rows.Close()
	out := []AlertSilence{}
	for rows.Next() {
		s, err := scanSilence(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan alert silence: %w", err)
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ListAlertSilences returns the newest silences first.
func (db *DB) ListAlertSilences(limit int) ([]AlertSilence, error) {
	if limit <= 0 || limit > 1000 {
		limit = 500
	}
	return db.querySilences(`ORDER BY created_at DESC, id LIMIT ?`, limit)
}

// ListActiveAlertSilences returns the silences whose window holds now.
func (db *DB) ListActiveAlertSilences(now time.Time) ([]AlertSilence, error) {
	ts := now.UTC().Format(time.RFC3339)
	return db.querySilences(`WHERE expired_at IS NULL AND starts_at <= ? AND ends_at > ? ORDER BY created_at`, ts, ts)
}

// ListLapsedAlertSilences returns silences whose window ended but that were
// not marked expired yet.
func (db *DB) ListLapsedAlertSilences(now time.Time) ([]AlertSilence, error) {
	return db.querySilences(`WHERE expired_at IS NULL AND ends_at <= ? ORDER BY ends_at`, now.UTC().Format(time.RFC3339))
}

// GetAlertSilence returns one silence, or nil.
func (db *DB) GetAlertSilence(id string) (*AlertSilence, error) {
	s, err := scanSilence(db.conn.QueryRow(`SELECT `+silenceColumns+` FROM alert_silences WHERE id = ?`, id).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get alert silence: %w", err)
	}
	return &s, nil
}

// CreateAlertSilence stores a silence and returns its id.
func (db *DB) CreateAlertSilence(s AlertSilence) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(`INSERT INTO alert_silences (`+silenceColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		id, strings.TrimSpace(s.Reason), nullStr(s.ConnectionID), nullStr(s.EventType), nullStr(s.SourceRef), nullStr(s.Node),
		s.StartsAt, s.EndsAt, nullStr(s.CreatedBy), now); err != nil {
		return "", fmt.Errorf("create alert silence: %w", err)
	}
	return id, nil
}

// UpdateAlertSilence replaces the reason, matchers and window of a silence
// that has not expired. It reports false when there was none to update.
func (db *DB) UpdateAlertSilence(s AlertSilence) (bool, error) {
	res, err := db.conn.Exec(`UPDATE alert_silences SET reason = ?, connection_id = ?, event_type = ?, source_ref = ?, node = ?,
		starts_at = ?, ends_at = ? WHERE id = ? AND expired_at IS NULL`,
		strings.TrimSpace(s.Reason), nullStr(s.ConnectionID), nullStr(s.EventType), nullStr(s.SourceRef), nullStr(s.Node),
		s.StartsAt, s.EndsAt, s.ID)
	if err != nil {
		return false, fmt.Errorf("update alert silence: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ExpireAlertSilence marks a silence expired at the given time. by is ""
// for a window that ran out. It reports false when it was already expired.
func (db *DB) ExpireAlertSilence(id, by string, at time.Time) (bool, error) {
	ts := at.UTC().Format(time.RFC3339)
	res, err := db.conn.Exec(`UPDATE alert_silences SET expired_at = ?, expired_by = ?,
		ends_at = CASE WHEN ends_at > ? THEN ? ELSE ends_at END
		WHERE id = ? AND expired_at IS NULL`, ts, nullableString(by), ts, ts, id)
	if err != nil {
		return false, fmt.Errorf("expire alert silence: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ── Event suppression ────────────────────────────────────────────────────────

// SuppressAlertEvent marks a still-new event silenced or acknowledged and
// records what held it back. It creates no deliveries.
func (db *DB) SuppressAlertEvent(id, status, by string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(`UPDATE alert_events SET status = ?, suppressed_by = ?, processed_at = ? WHERE id = ? AND status = 'new'`,
		status, by, now, id); err != nil {
		return fmt.Errorf("suppress alert event: %w", err)
	}
	return nil
}

// RequeueAlertEvent returns a silenced event to 'new' so the dispatcher
// delivers it once. It reports false when the event was not silenced.
func (db *DB) RequeueAlertEvent(id string) (bool, error) {
	res, err := db.conn.Exec(`UPDATE alert_events SET status = 'new', suppressed_by = NULL, processed_at = NULL
		WHERE id = ? AND status = 'silenced'`, id)
	if err != nil {
		return false, fmt.Errorf("requeue alert event: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// AlertEventsSuppressedBy lists the events a silence or ack held back,
// oldest first.
func (db *DB) AlertEventsSuppressedBy(ref string) ([]AlertEvent, error) {
	return db.queryEvents(`WHERE suppressed_by = ? ORDER BY created_at, id`, ref)
}

// IncidentAlertEvents lists the events of one incident of one alert, oldest
// first, by the alert_key and incident_id in their payloads.
func (db *DB) IncidentAlertEvents(alertKey, incidentID string) ([]AlertEvent, error) {
	return db.queryEvents(`WHERE json_valid(payload_json) AND json_extract(payload_json, '$.alert_key') = ?
		AND json_extract(payload_json, '$.incident_id') = ? ORDER BY created_at, id`, alertKey, incidentID)
}

// LatestAlertEventFor returns the newest event of an alert (by source_ref
// and the alert_key in its payload), or nil.
func (db *DB) LatestAlertEventFor(sourceRef, alertKey string) (*AlertEvent, error) {
	evs, err := db.queryEvents(`WHERE source_ref = ? AND json_valid(payload_json) AND json_extract(payload_json, '$.alert_key') = ?
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, sourceRef, alertKey)
	if err != nil || len(evs) == 0 {
		return nil, err
	}
	return &evs[0], nil
}

// GetAlertEvent returns one event, or nil.
func (db *DB) GetAlertEvent(id string) (*AlertEvent, error) {
	evs, err := db.queryEvents(`WHERE id = ?`, id)
	if err != nil || len(evs) == 0 {
		return nil, err
	}
	return &evs[0], nil
}

func (db *DB) queryEvents(where string, args ...interface{}) ([]AlertEvent, error) {
	rows, err := db.conn.Query(`SELECT id, connection_id, event_type, severity, title, message, payload_json, fingerprint,
		source_ref, status, created_at, processed_at, suppressed_by FROM alert_events `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("query alert events: %w", err)
	}
	defer rows.Close()
	out := []AlertEvent{}
	for rows.Next() {
		var item AlertEvent
		var connectionID, payloadJSON, fingerprint, sourceRef, processedAt, suppressedBy sql.NullString
		if err := rows.Scan(&item.ID, &connectionID, &item.EventType, &item.Severity, &item.Title, &item.Message,
			&payloadJSON, &fingerprint, &sourceRef, &item.Status, &item.CreatedAt, &processedAt, &suppressedBy); err != nil {
			return nil, fmt.Errorf("scan alert event: %w", err)
		}
		item.ConnectionID = nullStringToPtr(connectionID)
		item.PayloadJSON = nullStringToPtr(payloadJSON)
		item.Fingerprint = nullStringToPtr(fingerprint)
		item.SourceRef = nullStringToPtr(sourceRef)
		item.ProcessedAt = nullStringToPtr(processedAt)
		item.SuppressedBy = nullStringToPtr(suppressedBy)
		out = append(out, item)
	}
	return out, rows.Err()
}

// ── Acknowledgements ─────────────────────────────────────────────────────────

// AlertAck mutes the rest of one incident of one alert. At most one ack per
// alert key is open (cleared_at NULL).
type AlertAck struct {
	ID            string  `json:"id"`
	AlertKey      string  `json:"alert_key"`
	IncidentID    *string `json:"incident_id"`
	EventID       *string `json:"event_id"`
	ConnectionID  *string `json:"connection_id"`
	Severity      *string `json:"severity"`
	Comment       *string `json:"comment"`
	AckedBy       *string `json:"acked_by"`
	CreatedAt     string  `json:"created_at"`
	ExpiresAt     *string `json:"expires_at"`
	ClearedAt     *string `json:"cleared_at"`
	ClearedReason *string `json:"cleared_reason"`
}

const ackColumns = `id, alert_key, incident_id, event_id, connection_id, severity, comment, acked_by, created_at, expires_at, cleared_at, cleared_reason`

func scanAck(scan func(...interface{}) error) (AlertAck, error) {
	var a AlertAck
	var inc, ev, conn, sev, comment, by, exp, cleared, reason sql.NullString
	if err := scan(&a.ID, &a.AlertKey, &inc, &ev, &conn, &sev, &comment, &by, &a.CreatedAt, &exp, &cleared, &reason); err != nil {
		return a, err
	}
	a.IncidentID, a.EventID, a.ConnectionID, a.Severity = strPtrOf(inc), strPtrOf(ev), strPtrOf(conn), strPtrOf(sev)
	a.Comment, a.AckedBy, a.ExpiresAt = strPtrOf(comment), strPtrOf(by), strPtrOf(exp)
	a.ClearedAt, a.ClearedReason = strPtrOf(cleared), strPtrOf(reason)
	return a, nil
}

func (db *DB) queryAcks(where string, args ...interface{}) ([]AlertAck, error) {
	rows, err := db.conn.Query(`SELECT `+ackColumns+` FROM alert_acks `+where, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert acks: %w", err)
	}
	defer rows.Close()
	out := []AlertAck{}
	for rows.Next() {
		a, err := scanAck(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan alert ack: %w", err)
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// ListOpenAlertAcks returns every ack that is not cleared.
func (db *DB) ListOpenAlertAcks() ([]AlertAck, error) {
	return db.queryAcks(`WHERE cleared_at IS NULL ORDER BY created_at`)
}

// ListExpiredAlertAcks returns open acks whose expiry has passed.
func (db *DB) ListExpiredAlertAcks(now time.Time) ([]AlertAck, error) {
	return db.queryAcks(`WHERE cleared_at IS NULL AND expires_at IS NOT NULL AND expires_at <= ?`, now.UTC().Format(time.RFC3339))
}

// GetAlertAck returns one ack, or nil.
func (db *DB) GetAlertAck(id string) (*AlertAck, error) {
	a, err := scanAck(db.conn.QueryRow(`SELECT `+ackColumns+` FROM alert_acks WHERE id = ?`, id).Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get alert ack: %w", err)
	}
	return &a, nil
}

// CreateAlertAck stores an open ack, clearing (as 'replaced') the open ack
// the same alert key may already hold. It returns the new ack.
func (db *DB) CreateAlertAck(a AlertAck) (*AlertAck, error) {
	tx, err := db.conn.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback() //nolint:errcheck // rolled back unless committed
	a.ID = uuid.NewString()
	a.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.Exec(`UPDATE alert_acks SET cleared_at = ?, cleared_reason = 'replaced' WHERE alert_key = ? AND cleared_at IS NULL`,
		a.CreatedAt, a.AlertKey); err != nil {
		return nil, fmt.Errorf("replace alert ack: %w", err)
	}
	if _, err := tx.Exec(`INSERT INTO alert_acks (`+ackColumns+`) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, NULL, NULL)`,
		a.ID, a.AlertKey, nullStr(a.IncidentID), nullStr(a.EventID), nullStr(a.ConnectionID), nullStr(a.Severity),
		nullStr(a.Comment), nullStr(a.AckedBy), a.CreatedAt, nullStr(a.ExpiresAt)); err != nil {
		return nil, fmt.Errorf("create alert ack: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &a, nil
}

// ClearAlertAck closes an open ack. It reports false when it was already
// cleared or does not exist.
func (db *DB) ClearAlertAck(id, reason string, at time.Time) (bool, error) {
	res, err := db.conn.Exec(`UPDATE alert_acks SET cleared_at = ?, cleared_reason = ? WHERE id = ? AND cleared_at IS NULL`,
		at.UTC().Format(time.RFC3339), reason, id)
	if err != nil {
		return false, fmt.Errorf("clear alert ack: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ── Open incidents ───────────────────────────────────────────────────────────

// ListFiringHealthStates returns every firing health check state across
// connections.
func (db *DB) ListFiringHealthStates() ([]HealthAlertState, error) {
	rows, err := db.conn.Query(`SELECT ` + healthStateColumns + ` FROM health_alert_state WHERE state = 'firing' ORDER BY connection_id, check_kind, node`)
	if err != nil {
		return nil, fmt.Errorf("list firing health states: %w", err)
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

// LatestAlertIncidentID returns the incident id of the newest firing event
// of an alert, or "".
func (db *DB) LatestAlertIncidentID(sourceRef, alertKey string) (string, error) {
	var id sql.NullString
	err := db.conn.QueryRow(`SELECT json_extract(payload_json, '$.incident_id') FROM alert_events
		WHERE source_ref = ? AND json_valid(payload_json) AND json_extract(payload_json, '$.alert_key') = ?
		AND json_extract(payload_json, '$.status') = 'firing' AND json_extract(payload_json, '$.incident_id') IS NOT NULL
		ORDER BY created_at DESC, rowid DESC LIMIT 1`, sourceRef, alertKey).Scan(&id)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("latest alert incident: %w", err)
	}
	return id.String, nil
}
