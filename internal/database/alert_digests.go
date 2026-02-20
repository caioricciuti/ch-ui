package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AlertRouteDigestWithDetails struct {
	ID                               string  `json:"id"`
	RouteID                          string  `json:"route_id"`
	RuleID                           string  `json:"rule_id"`
	ChannelID                        string  `json:"channel_id"`
	BucketStart                      string  `json:"bucket_start"`
	BucketEnd                        string  `json:"bucket_end"`
	EventType                        string  `json:"event_type"`
	Severity                         string  `json:"severity"`
	EventCount                       int     `json:"event_count"`
	EventIDsJSON                     string  `json:"event_ids_json"`
	TitlesJSON                       string  `json:"titles_json"`
	Status                           string  `json:"status"`
	AttemptCount                     int     `json:"attempt_count"`
	MaxAttempts                      int     `json:"max_attempts"`
	NextAttemptAt                    string  `json:"next_attempt_at"`
	LastError                        *string `json:"last_error"`
	CreatedAt                        string  `json:"created_at"`
	UpdatedAt                        string  `json:"updated_at"`
	SentAt                           *string `json:"sent_at"`
	RouteRecipientsJSON              string  `json:"route_recipients_json"`
	EscalationChannelID              *string `json:"escalation_channel_id"`
	EscalationRecipientsJSON         *string `json:"escalation_recipients_json"`
	EscalationAfterFailures          int     `json:"escalation_after_failures"`
	ChannelName                      string  `json:"channel_name"`
	ChannelType                      string  `json:"channel_type"`
	ChannelConfigEncrypted           string  `json:"channel_config_encrypted"`
	EscalationChannelName            *string `json:"escalation_channel_name"`
	EscalationChannelType            *string `json:"escalation_channel_type"`
	EscalationChannelConfigEncrypted *string `json:"escalation_channel_config_encrypted"`
}

func (db *DB) UpsertAlertRouteDigest(rule AlertRule, route AlertRuleRouteView, event AlertEvent, now time.Time) error {
	windowMins := route.DigestWindowMinutes
	if windowMins <= 0 {
		windowMins = 15
	}
	window := time.Duration(windowMins) * time.Minute
	bucketStart := now.UTC().Truncate(window)
	bucketEnd := bucketStart.Add(window)

	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin digest upsert: %w", err)
	}
	defer tx.Rollback()

	var (
		digestID     string
		eventCount   int
		eventIDsJSON string
		titlesJSON   string
	)
	err = tx.QueryRow(
		`SELECT id, event_count, event_ids_json, titles_json
		 FROM alert_route_digests
		 WHERE route_id = ? AND bucket_start = ? AND event_type = ? AND severity = ?`,
		route.ID, bucketStart.Format(time.RFC3339), event.EventType, event.Severity,
	).Scan(&digestID, &eventCount, &eventIDsJSON, &titlesJSON)
	switch err {
	case nil:
		ids := parseDigestStringArray(eventIDsJSON)
		if len(ids) < 200 {
			ids = append(ids, event.ID)
		}
		titles := parseDigestStringArray(titlesJSON)
		if len(titles) < 30 {
			titles = append(titles, strings.TrimSpace(event.Title))
		}
		idsPayload, _ := json.Marshal(ids)
		titlesPayload, _ := json.Marshal(titles)
		if _, err := tx.Exec(
			`UPDATE alert_route_digests
			 SET event_count = ?,
			     event_ids_json = ?,
			     titles_json = ?,
			     updated_at = ?
			 WHERE id = ?`,
			eventCount+1,
			string(idsPayload),
			string(titlesPayload),
			now.UTC().Format(time.RFC3339),
			digestID,
		); err != nil {
			return fmt.Errorf("update digest batch: %w", err)
		}
	case sql.ErrNoRows:
		digestID = uuid.NewString()
		maxAttempts := rule.MaxAttempts
		if maxAttempts <= 0 {
			maxAttempts = 5
		}
		idsPayload, _ := json.Marshal([]string{event.ID})
		titlesPayload, _ := json.Marshal([]string{strings.TrimSpace(event.Title)})
		if _, err := tx.Exec(
			`INSERT INTO alert_route_digests
			 (id, route_id, rule_id, channel_id, bucket_start, bucket_end, event_type, severity, event_count, event_ids_json, titles_json, status, attempt_count, max_attempts, next_attempt_at, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, 'collecting', 0, ?, ?, ?, ?)`,
			digestID,
			route.ID,
			rule.ID,
			route.ChannelID,
			bucketStart.Format(time.RFC3339),
			bucketEnd.Format(time.RFC3339),
			event.EventType,
			event.Severity,
			string(idsPayload),
			string(titlesPayload),
			maxAttempts,
			bucketEnd.Format(time.RFC3339),
			now.UTC().Format(time.RFC3339),
			now.UTC().Format(time.RFC3339),
		); err != nil {
			return fmt.Errorf("insert digest batch: %w", err)
		}
	default:
		return fmt.Errorf("load digest batch: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit digest upsert: %w", err)
	}
	return nil
}

func (db *DB) ListDueAlertRouteDigests(limit int) ([]AlertRouteDigestWithDetails, error) {
	if limit <= 0 {
		limit = 20
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := db.conn.Query(
		`SELECT
			d.id, d.route_id, d.rule_id, d.channel_id, d.bucket_start, d.bucket_end, d.event_type, d.severity, d.event_count,
			d.event_ids_json, d.titles_json, d.status, d.attempt_count, d.max_attempts, d.next_attempt_at, d.last_error, d.created_at, d.updated_at, d.sent_at,
			rr.recipients_json,
			rp.escalation_channel_id, rp.escalation_recipients_json, COALESCE(rp.escalation_after_failures, 0),
			c.name, c.channel_type, c.config_encrypted,
			ec.name, ec.channel_type, ec.config_encrypted
		 FROM alert_route_digests d
		 JOIN alert_rule_routes rr ON rr.id = d.route_id
		 LEFT JOIN alert_route_policies rp ON rp.route_id = rr.id
		 JOIN alert_channels c ON c.id = d.channel_id
		 LEFT JOIN alert_channels ec ON ec.id = rp.escalation_channel_id
		 WHERE d.status IN ('collecting', 'retrying')
		   AND d.bucket_end <= ?
		   AND d.next_attempt_at <= ?
		   AND d.attempt_count < d.max_attempts
		 ORDER BY d.bucket_end ASC
		 LIMIT ?`,
		now, now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due alert route digests: %w", err)
	}
	defer rows.Close()

	out := make([]AlertRouteDigestWithDetails, 0)
	for rows.Next() {
		var item AlertRouteDigestWithDetails
		var lastError, sentAt sql.NullString
		var escalationChannelID, escalationRecipientsJSON, escalationChannelName, escalationChannelType, escalationChannelConfig sql.NullString
		if err := rows.Scan(
			&item.ID, &item.RouteID, &item.RuleID, &item.ChannelID, &item.BucketStart, &item.BucketEnd, &item.EventType, &item.Severity, &item.EventCount,
			&item.EventIDsJSON, &item.TitlesJSON, &item.Status, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt, &lastError, &item.CreatedAt, &item.UpdatedAt, &sentAt,
			&item.RouteRecipientsJSON,
			&escalationChannelID, &escalationRecipientsJSON, &item.EscalationAfterFailures,
			&item.ChannelName, &item.ChannelType, &item.ChannelConfigEncrypted,
			&escalationChannelName, &escalationChannelType, &escalationChannelConfig,
		); err != nil {
			return nil, fmt.Errorf("scan due alert route digest: %w", err)
		}
		item.LastError = nullStringToPtr(lastError)
		item.SentAt = nullStringToPtr(sentAt)
		item.EscalationChannelID = nullStringToPtr(escalationChannelID)
		item.EscalationRecipientsJSON = nullStringToPtr(escalationRecipientsJSON)
		item.EscalationChannelName = nullStringToPtr(escalationChannelName)
		item.EscalationChannelType = nullStringToPtr(escalationChannelType)
		item.EscalationChannelConfigEncrypted = nullStringToPtr(escalationChannelConfig)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due alert route digests: %w", err)
	}
	return out, nil
}

func (db *DB) MarkAlertRouteDigestSending(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_route_digests
		 SET status = 'sending',
		     attempt_count = attempt_count + 1,
		     updated_at = ?
		 WHERE id = ?`,
		now, id,
	); err != nil {
		return fmt.Errorf("mark digest sending: %w", err)
	}
	return nil
}

func (db *DB) MarkAlertRouteDigestSent(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_route_digests
		 SET status = 'sent',
		     sent_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		now, now, id,
	); err != nil {
		return fmt.Errorf("mark digest sent: %w", err)
	}
	return nil
}

func (db *DB) MarkAlertRouteDigestRetry(id string, nextAttemptAt time.Time, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_route_digests
		 SET status = 'retrying',
		     next_attempt_at = ?,
		     last_error = ?,
		     updated_at = ?
		 WHERE id = ?`,
		nextAttemptAt.UTC().Format(time.RFC3339),
		nullableString(lastError),
		now,
		id,
	); err != nil {
		return fmt.Errorf("mark digest retry: %w", err)
	}
	return nil
}

func (db *DB) MarkAlertRouteDigestFailed(id string, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_route_digests
		 SET status = 'failed',
		     last_error = ?,
		     updated_at = ?
		 WHERE id = ?`,
		nullableString(lastError),
		now,
		id,
	); err != nil {
		return fmt.Errorf("mark digest failed: %w", err)
	}
	return nil
}

func parseDigestStringArray(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(values))
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}
