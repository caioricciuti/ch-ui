package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

type AlertChannel struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	ChannelType     string  `json:"channel_type"`
	ConfigEncrypted string  `json:"-"`
	IsActive        bool    `json:"is_active"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type AlertRule struct {
	ID              string  `json:"id"`
	Name            string  `json:"name"`
	EventType       string  `json:"event_type"`
	SeverityMin     string  `json:"severity_min"`
	Enabled         bool    `json:"enabled"`
	CooldownSeconds int     `json:"cooldown_seconds"`
	MaxAttempts     int     `json:"max_attempts"`
	SubjectTemplate *string `json:"subject_template"`
	BodyTemplate    *string `json:"body_template"`
	CreatedBy       *string `json:"created_by"`
	CreatedAt       string  `json:"created_at"`
	UpdatedAt       string  `json:"updated_at"`
}

type AlertRuleRoute struct {
	ID                       string   `json:"id"`
	RuleID                   string   `json:"rule_id"`
	ChannelID                string   `json:"channel_id"`
	Recipients               []string `json:"recipients"`
	RecipientsJSON           string   `json:"-"`
	IsActive                 bool     `json:"is_active"`
	DeliveryMode             string   `json:"delivery_mode"`
	DigestWindowMinutes      int      `json:"digest_window_minutes"`
	EscalationChannelID      *string  `json:"escalation_channel_id"`
	EscalationRecipients     []string `json:"escalation_recipients"`
	EscalationRecipientsJSON string   `json:"-"`
	EscalationAfterFailures  int      `json:"escalation_after_failures"`
	CreatedAt                string   `json:"created_at"`
	UpdatedAt                string   `json:"updated_at"`
}

type AlertRuleRouteView struct {
	AlertRuleRoute
	ChannelName           string  `json:"channel_name"`
	ChannelType           string  `json:"channel_type"`
	EscalationChannelName *string `json:"escalation_channel_name"`
	EscalationChannelType *string `json:"escalation_channel_type"`
}

type AlertEvent struct {
	ID           string  `json:"id"`
	ConnectionID *string `json:"connection_id"`
	EventType    string  `json:"event_type"`
	Severity     string  `json:"severity"`
	Title        string  `json:"title"`
	Message      string  `json:"message"`
	PayloadJSON  *string `json:"payload_json"`
	Fingerprint  *string `json:"fingerprint"`
	SourceRef    *string `json:"source_ref"`
	Status       string  `json:"status"`
	CreatedAt    string  `json:"created_at"`
	ProcessedAt  *string `json:"processed_at"`
}

type AlertDispatchJob struct {
	ID                string  `json:"id"`
	EventID           string  `json:"event_id"`
	RuleID            string  `json:"rule_id"`
	RouteID           string  `json:"route_id"`
	ChannelID         string  `json:"channel_id"`
	Status            string  `json:"status"`
	AttemptCount      int     `json:"attempt_count"`
	MaxAttempts       int     `json:"max_attempts"`
	NextAttemptAt     string  `json:"next_attempt_at"`
	LastError         *string `json:"last_error"`
	ProviderMessageID *string `json:"provider_message_id"`
	CreatedAt         string  `json:"created_at"`
	UpdatedAt         string  `json:"updated_at"`
	SentAt            *string `json:"sent_at"`
}

type AlertDispatchJobWithDetails struct {
	AlertDispatchJob
	EventType                        string  `json:"event_type"`
	EventSeverity                    string  `json:"event_severity"`
	EventTitle                       string  `json:"event_title"`
	EventMessage                     string  `json:"event_message"`
	EventPayloadJSON                 *string `json:"event_payload_json"`
	EventFingerprint                 *string `json:"event_fingerprint"`
	RuleName                         string  `json:"rule_name"`
	RuleCooldownSeconds              int     `json:"rule_cooldown_seconds"`
	RuleSubjectTemplate              *string `json:"rule_subject_template"`
	RuleBodyTemplate                 *string `json:"rule_body_template"`
	RouteRecipientsJSON              string  `json:"route_recipients_json"`
	RouteDeliveryMode                string  `json:"route_delivery_mode"`
	RouteDigestWindowMins            int     `json:"route_digest_window_minutes"`
	RouteEscalationChannelID         *string `json:"route_escalation_channel_id"`
	RouteEscalationRecipientsJSON    *string `json:"route_escalation_recipients_json"`
	RouteEscalationAfterFailures     int     `json:"route_escalation_after_failures"`
	ChannelName                      string  `json:"channel_name"`
	ChannelType                      string  `json:"channel_type"`
	ChannelConfigEncrypted           string  `json:"channel_config_encrypted"`
	EscalationChannelName            *string `json:"escalation_channel_name"`
	EscalationChannelType            *string `json:"escalation_channel_type"`
	EscalationChannelConfigEncrypted *string `json:"escalation_channel_config_encrypted"`
}

func (db *DB) CreateAlertChannel(name, channelType, encryptedConfig string, isActive bool, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		`INSERT INTO alert_channels (id, name, channel_type, config_encrypted, is_active, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, strings.TrimSpace(name), strings.TrimSpace(channelType), encryptedConfig, boolToInt(isActive), nullableString(createdBy), now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create alert channel: %w", err)
	}
	return id, nil
}

func (db *DB) UpdateAlertChannel(id, name, channelType string, encryptedConfig *string, isActive bool) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if encryptedConfig != nil {
		if _, err := db.conn.Exec(
			`UPDATE alert_channels
			 SET name = ?, channel_type = ?, config_encrypted = ?, is_active = ?, updated_at = ?
			 WHERE id = ?`,
			strings.TrimSpace(name), strings.TrimSpace(channelType), *encryptedConfig, boolToInt(isActive), now, id,
		); err != nil {
			return fmt.Errorf("update alert channel: %w", err)
		}
		return nil
	}

	if _, err := db.conn.Exec(
		`UPDATE alert_channels
		 SET name = ?, channel_type = ?, is_active = ?, updated_at = ?
		 WHERE id = ?`,
		strings.TrimSpace(name), strings.TrimSpace(channelType), boolToInt(isActive), now, id,
	); err != nil {
		return fmt.Errorf("update alert channel: %w", err)
	}
	return nil
}

func (db *DB) DeleteAlertChannel(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM alert_channels WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete alert channel: %w", err)
	}
	return nil
}

func (db *DB) GetAlertChannelByID(id string) (*AlertChannel, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, channel_type, config_encrypted, is_active, created_by, created_at, updated_at
		 FROM alert_channels WHERE id = ?`,
		id,
	)
	return scanAlertChannelRow(row)
}

func (db *DB) ListAlertChannels() ([]AlertChannel, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, channel_type, config_encrypted, is_active, created_by, created_at, updated_at
		 FROM alert_channels
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list alert channels: %w", err)
	}
	defer rows.Close()

	out := make([]AlertChannel, 0)
	for rows.Next() {
		channel, err := scanAlertChannel(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, channel)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert channels: %w", err)
	}
	return out, nil
}

func (db *DB) CreateAlertRule(name, eventType, severityMin string, enabled bool, cooldownSeconds, maxAttempts int, subjectTemplate, bodyTemplate, createdBy string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if cooldownSeconds < 0 {
		cooldownSeconds = 0
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	_, err := db.conn.Exec(
		`INSERT INTO alert_rules (id, name, event_type, severity_min, enabled, cooldown_seconds, max_attempts, subject_template, body_template, created_by, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, strings.TrimSpace(name), strings.TrimSpace(eventType), strings.TrimSpace(severityMin), boolToInt(enabled),
		cooldownSeconds, maxAttempts, nullableString(subjectTemplate), nullableString(bodyTemplate), nullableString(createdBy), now, now,
	)
	if err != nil {
		return "", fmt.Errorf("create alert rule: %w", err)
	}
	return id, nil
}

func (db *DB) UpdateAlertRule(id, name, eventType, severityMin string, enabled bool, cooldownSeconds, maxAttempts int, subjectTemplate, bodyTemplate string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if cooldownSeconds < 0 {
		cooldownSeconds = 0
	}
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if _, err := db.conn.Exec(
		`UPDATE alert_rules
		 SET name = ?, event_type = ?, severity_min = ?, enabled = ?, cooldown_seconds = ?, max_attempts = ?, subject_template = ?, body_template = ?, updated_at = ?
		 WHERE id = ?`,
		strings.TrimSpace(name), strings.TrimSpace(eventType), strings.TrimSpace(severityMin), boolToInt(enabled),
		cooldownSeconds, maxAttempts, nullableString(subjectTemplate), nullableString(bodyTemplate), now, id,
	); err != nil {
		return fmt.Errorf("update alert rule: %w", err)
	}
	return nil
}

func (db *DB) DeleteAlertRule(id string) error {
	if _, err := db.conn.Exec(`DELETE FROM alert_rules WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete alert rule: %w", err)
	}
	return nil
}

func (db *DB) GetAlertRuleByID(id string) (*AlertRule, error) {
	row := db.conn.QueryRow(
		`SELECT id, name, event_type, severity_min, enabled, cooldown_seconds, max_attempts, subject_template, body_template, created_by, created_at, updated_at
		 FROM alert_rules WHERE id = ?`,
		id,
	)
	return scanAlertRuleRow(row)
}

func (db *DB) ListAlertRules() ([]AlertRule, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, event_type, severity_min, enabled, cooldown_seconds, max_attempts, subject_template, body_template, created_by, created_at, updated_at
		 FROM alert_rules
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list alert rules: %w", err)
	}
	defer rows.Close()

	out := make([]AlertRule, 0)
	for rows.Next() {
		rule, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert rules: %w", err)
	}
	return out, nil
}

func (db *DB) ListEnabledAlertRules() ([]AlertRule, error) {
	rows, err := db.conn.Query(
		`SELECT id, name, event_type, severity_min, enabled, cooldown_seconds, max_attempts, subject_template, body_template, created_by, created_at, updated_at
		 FROM alert_rules
		 WHERE enabled = 1
		 ORDER BY created_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list enabled alert rules: %w", err)
	}
	defer rows.Close()

	out := make([]AlertRule, 0)
	for rows.Next() {
		rule, err := scanAlertRule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate enabled alert rules: %w", err)
	}
	return out, nil
}

func (db *DB) ReplaceAlertRuleRoutes(ruleID string, routes []AlertRuleRoute) error {
	tx, err := db.conn.Begin()
	if err != nil {
		return fmt.Errorf("begin replace alert routes: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM alert_rule_routes WHERE rule_id = ?`, ruleID); err != nil {
		return fmt.Errorf("clear alert routes: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	for _, route := range routes {
		recipientsJSON, err := json.Marshal(route.Recipients)
		if err != nil {
			return fmt.Errorf("marshal route recipients: %w", err)
		}
		id := route.ID
		if strings.TrimSpace(id) == "" {
			id = uuid.NewString()
		}
		if _, err := tx.Exec(
			`INSERT INTO alert_rule_routes (id, rule_id, channel_id, recipients_json, is_active, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			id, ruleID, route.ChannelID, string(recipientsJSON), boolToInt(route.IsActive), now, now,
		); err != nil {
			return fmt.Errorf("insert alert route: %w", err)
		}

		deliveryMode := strings.ToLower(strings.TrimSpace(route.DeliveryMode))
		if deliveryMode != "digest" {
			deliveryMode = "immediate"
		}
		digestWindow := route.DigestWindowMinutes
		if digestWindow < 0 {
			digestWindow = 0
		}
		escalationAfter := route.EscalationAfterFailures
		if escalationAfter < 0 {
			escalationAfter = 0
		}
		var escalationChannelID interface{}
		if route.EscalationChannelID != nil && strings.TrimSpace(*route.EscalationChannelID) != "" {
			escalationChannelID = strings.TrimSpace(*route.EscalationChannelID)
		}
		var escalationRecipients interface{}
		if len(route.EscalationRecipients) > 0 {
			payload, err := json.Marshal(route.EscalationRecipients)
			if err != nil {
				return fmt.Errorf("marshal escalation recipients: %w", err)
			}
			escalationRecipients = string(payload)
		}
		if _, err := tx.Exec(
			`INSERT INTO alert_route_policies
			 (route_id, delivery_mode, digest_window_minutes, escalation_channel_id, escalation_recipients_json, escalation_after_failures, created_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?)
			 ON CONFLICT(route_id) DO UPDATE SET
			   delivery_mode = excluded.delivery_mode,
			   digest_window_minutes = excluded.digest_window_minutes,
			   escalation_channel_id = excluded.escalation_channel_id,
			   escalation_recipients_json = excluded.escalation_recipients_json,
			   escalation_after_failures = excluded.escalation_after_failures,
			   updated_at = excluded.updated_at`,
			id, deliveryMode, digestWindow, escalationChannelID, escalationRecipients, escalationAfter, now, now,
		); err != nil {
			return fmt.Errorf("upsert alert route policy: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit replace alert routes: %w", err)
	}
	return nil
}

func (db *DB) ListAlertRuleRoutes(ruleID string) ([]AlertRuleRouteView, error) {
	rows, err := db.conn.Query(
		`SELECT rr.id, rr.rule_id, rr.channel_id, rr.recipients_json, rr.is_active, rr.created_at, rr.updated_at, c.name, c.channel_type,
		        COALESCE(rp.delivery_mode, 'immediate'),
				COALESCE(rp.digest_window_minutes, 0),
				rp.escalation_channel_id,
				rp.escalation_recipients_json,
				COALESCE(rp.escalation_after_failures, 0),
				ec.name,
				ec.channel_type
		 FROM alert_rule_routes rr
		 JOIN alert_channels c ON c.id = rr.channel_id
		 LEFT JOIN alert_route_policies rp ON rp.route_id = rr.id
		 LEFT JOIN alert_channels ec ON ec.id = rp.escalation_channel_id
		 WHERE rr.rule_id = ?
		 ORDER BY rr.created_at ASC`,
		ruleID,
	)
	if err != nil {
		return nil, fmt.Errorf("list alert rule routes: %w", err)
	}
	defer rows.Close()

	out := make([]AlertRuleRouteView, 0)
	for rows.Next() {
		var item AlertRuleRouteView
		var recipientsJSON string
		var active int
		var escalationChannelID, escalationRecipientsJSON, escalationChannelName, escalationChannelType sql.NullString
		if err := rows.Scan(
			&item.ID, &item.RuleID, &item.ChannelID, &recipientsJSON, &active, &item.CreatedAt, &item.UpdatedAt,
			&item.ChannelName, &item.ChannelType,
			&item.DeliveryMode, &item.DigestWindowMinutes, &escalationChannelID, &escalationRecipientsJSON, &item.EscalationAfterFailures,
			&escalationChannelName, &escalationChannelType,
		); err != nil {
			return nil, fmt.Errorf("scan alert rule route: %w", err)
		}
		item.IsActive = intToBool(active)
		item.RecipientsJSON = recipientsJSON
		item.Recipients = parseRecipientsJSON(recipientsJSON)
		item.EscalationChannelID = nullStringToPtr(escalationChannelID)
		item.EscalationRecipientsJSON = escalationRecipientsJSON.String
		item.EscalationRecipients = parseRecipientsJSON(escalationRecipientsJSON.String)
		item.EscalationChannelName = nullStringToPtr(escalationChannelName)
		item.EscalationChannelType = nullStringToPtr(escalationChannelType)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert rule routes: %w", err)
	}
	return out, nil
}

func (db *DB) ListActiveAlertRuleRoutes(ruleID string) ([]AlertRuleRouteView, error) {
	rows, err := db.conn.Query(
		`SELECT rr.id, rr.rule_id, rr.channel_id, rr.recipients_json, rr.is_active, rr.created_at, rr.updated_at, c.name, c.channel_type,
		        COALESCE(rp.delivery_mode, 'immediate'),
				COALESCE(rp.digest_window_minutes, 0),
				rp.escalation_channel_id,
				rp.escalation_recipients_json,
				COALESCE(rp.escalation_after_failures, 0),
				ec.name,
				ec.channel_type
		 FROM alert_rule_routes rr
		 JOIN alert_channels c ON c.id = rr.channel_id
		 LEFT JOIN alert_route_policies rp ON rp.route_id = rr.id
		 LEFT JOIN alert_channels ec ON ec.id = rp.escalation_channel_id
		 WHERE rr.rule_id = ? AND rr.is_active = 1 AND c.is_active = 1
		 ORDER BY rr.created_at ASC`,
		ruleID,
	)
	if err != nil {
		return nil, fmt.Errorf("list active alert rule routes: %w", err)
	}
	defer rows.Close()

	out := make([]AlertRuleRouteView, 0)
	for rows.Next() {
		var item AlertRuleRouteView
		var recipientsJSON string
		var active int
		var escalationChannelID, escalationRecipientsJSON, escalationChannelName, escalationChannelType sql.NullString
		if err := rows.Scan(
			&item.ID, &item.RuleID, &item.ChannelID, &recipientsJSON, &active, &item.CreatedAt, &item.UpdatedAt,
			&item.ChannelName, &item.ChannelType,
			&item.DeliveryMode, &item.DigestWindowMinutes, &escalationChannelID, &escalationRecipientsJSON, &item.EscalationAfterFailures,
			&escalationChannelName, &escalationChannelType,
		); err != nil {
			return nil, fmt.Errorf("scan active alert route: %w", err)
		}
		item.IsActive = intToBool(active)
		item.RecipientsJSON = recipientsJSON
		item.Recipients = parseRecipientsJSON(recipientsJSON)
		item.EscalationChannelID = nullStringToPtr(escalationChannelID)
		item.EscalationRecipientsJSON = escalationRecipientsJSON.String
		item.EscalationRecipients = parseRecipientsJSON(escalationRecipientsJSON.String)
		item.EscalationChannelName = nullStringToPtr(escalationChannelName)
		item.EscalationChannelType = nullStringToPtr(escalationChannelType)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active alert routes: %w", err)
	}
	return out, nil
}

func (db *DB) CreateAlertEvent(connectionID *string, eventType, severity, title, message string, payload interface{}, fingerprint, sourceRef string) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)

	var payloadJSON interface{}
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return "", fmt.Errorf("marshal alert payload: %w", err)
		}
		payloadJSON = string(data)
	}

	var connectionVal interface{}
	if connectionID != nil && strings.TrimSpace(*connectionID) != "" {
		connectionVal = strings.TrimSpace(*connectionID)
	}

	if _, err := db.conn.Exec(
		`INSERT INTO alert_events (id, connection_id, event_type, severity, title, message, payload_json, fingerprint, source_ref, status, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, 'new', ?)`,
		id, connectionVal, strings.TrimSpace(eventType), strings.TrimSpace(severity),
		strings.TrimSpace(title), strings.TrimSpace(message), payloadJSON, nullableString(fingerprint), nullableString(sourceRef), now,
	); err != nil {
		return "", fmt.Errorf("create alert event: %w", err)
	}
	return id, nil
}

func (db *DB) ListAlertEvents(limit int, eventType, status string) ([]AlertEvent, error) {
	if limit <= 0 {
		limit = 100
	}
	if limit > 1000 {
		limit = 1000
	}

	where := []string{"1=1"}
	args := make([]interface{}, 0, 4)
	if strings.TrimSpace(eventType) != "" {
		where = append(where, "event_type = ?")
		args = append(args, strings.TrimSpace(eventType))
	}
	if strings.TrimSpace(status) != "" {
		where = append(where, "status = ?")
		args = append(args, strings.TrimSpace(status))
	}
	args = append(args, limit)

	query := fmt.Sprintf(
		`SELECT id, connection_id, event_type, severity, title, message, payload_json, fingerprint, source_ref, status, created_at, processed_at
		 FROM alert_events
		 WHERE %s
		 ORDER BY created_at DESC
		 LIMIT ?`, strings.Join(where, " AND "),
	)

	rows, err := db.conn.Query(query, args...)
	if err != nil {
		return nil, fmt.Errorf("list alert events: %w", err)
	}
	defer rows.Close()

	out := make([]AlertEvent, 0)
	for rows.Next() {
		var item AlertEvent
		var connectionID, payloadJSON, fingerprint, sourceRef, processedAt sql.NullString
		if err := rows.Scan(
			&item.ID, &connectionID, &item.EventType, &item.Severity, &item.Title, &item.Message,
			&payloadJSON, &fingerprint, &sourceRef, &item.Status, &item.CreatedAt, &processedAt,
		); err != nil {
			return nil, fmt.Errorf("scan alert event: %w", err)
		}
		item.ConnectionID = nullStringToPtr(connectionID)
		item.PayloadJSON = nullStringToPtr(payloadJSON)
		item.Fingerprint = nullStringToPtr(fingerprint)
		item.SourceRef = nullStringToPtr(sourceRef)
		item.ProcessedAt = nullStringToPtr(processedAt)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alert events: %w", err)
	}
	return out, nil
}

func (db *DB) ListNewAlertEvents(limit int) ([]AlertEvent, error) {
	if limit <= 0 {
		limit = 50
	}

	rows, err := db.conn.Query(
		`SELECT id, connection_id, event_type, severity, title, message, payload_json, fingerprint, source_ref, status, created_at, processed_at
		 FROM alert_events
		 WHERE status = 'new'
		 ORDER BY created_at ASC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list new alert events: %w", err)
	}
	defer rows.Close()

	out := make([]AlertEvent, 0)
	for rows.Next() {
		var item AlertEvent
		var connectionID, payloadJSON, fingerprint, sourceRef, processedAt sql.NullString
		if err := rows.Scan(
			&item.ID, &connectionID, &item.EventType, &item.Severity, &item.Title, &item.Message,
			&payloadJSON, &fingerprint, &sourceRef, &item.Status, &item.CreatedAt, &processedAt,
		); err != nil {
			return nil, fmt.Errorf("scan new alert event: %w", err)
		}
		item.ConnectionID = nullStringToPtr(connectionID)
		item.PayloadJSON = nullStringToPtr(payloadJSON)
		item.Fingerprint = nullStringToPtr(fingerprint)
		item.SourceRef = nullStringToPtr(sourceRef)
		item.ProcessedAt = nullStringToPtr(processedAt)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate new alert events: %w", err)
	}
	return out, nil
}

func (db *DB) MarkAlertEventProcessed(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_events SET status = 'processed', processed_at = ? WHERE id = ?`,
		now, id,
	); err != nil {
		return fmt.Errorf("mark alert event processed: %w", err)
	}
	return nil
}

func (db *DB) HasRecentAlertDispatch(routeID, fingerprint string, since time.Time) (bool, error) {
	if strings.TrimSpace(fingerprint) == "" {
		return false, nil
	}
	var count int
	if err := db.conn.QueryRow(
		`SELECT COUNT(*)
		 FROM alert_dispatch_jobs j
		 JOIN alert_events e ON e.id = j.event_id
		 WHERE j.route_id = ?
		   AND e.fingerprint = ?
		   AND e.created_at >= ?
		   AND j.status IN ('queued', 'retrying', 'sending', 'sent')`,
		routeID, fingerprint, since.UTC().Format(time.RFC3339),
	).Scan(&count); err != nil {
		return false, fmt.Errorf("check recent alert dispatch: %w", err)
	}
	return count > 0, nil
}

func (db *DB) CreateAlertDispatchJob(eventID, ruleID, routeID, channelID string, maxAttempts int, nextAttemptAt time.Time) (string, error) {
	id := uuid.NewString()
	now := time.Now().UTC().Format(time.RFC3339)
	if maxAttempts <= 0 {
		maxAttempts = 5
	}
	if _, err := db.conn.Exec(
		`INSERT INTO alert_dispatch_jobs (id, event_id, rule_id, route_id, channel_id, status, attempt_count, max_attempts, next_attempt_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, 'queued', 0, ?, ?, ?, ?)`,
		id, eventID, ruleID, routeID, channelID, maxAttempts, nextAttemptAt.UTC().Format(time.RFC3339), now, now,
	); err != nil {
		return "", fmt.Errorf("create alert dispatch job: %w", err)
	}
	return id, nil
}

func (db *DB) ListDueAlertDispatchJobs(limit int) ([]AlertDispatchJobWithDetails, error) {
	if limit <= 0 {
		limit = 20
	}
	now := time.Now().UTC().Format(time.RFC3339)
	rows, err := db.conn.Query(
		`SELECT
			j.id, j.event_id, j.rule_id, j.route_id, j.channel_id, j.status, j.attempt_count, j.max_attempts, j.next_attempt_at, j.last_error, j.provider_message_id, j.created_at, j.updated_at, j.sent_at,
			e.event_type, e.severity, e.title, e.message, e.payload_json, e.fingerprint,
			r.name, r.cooldown_seconds, r.subject_template, r.body_template,
			rr.recipients_json,
			COALESCE(rp.delivery_mode, 'immediate'),
			COALESCE(rp.digest_window_minutes, 0),
			rp.escalation_channel_id,
			rp.escalation_recipients_json,
			COALESCE(rp.escalation_after_failures, 0),
			c.name, c.channel_type, c.config_encrypted,
			ec.name, ec.channel_type, ec.config_encrypted
		 FROM alert_dispatch_jobs j
		 JOIN alert_events e ON e.id = j.event_id
		 JOIN alert_rules r ON r.id = j.rule_id
		 JOIN alert_rule_routes rr ON rr.id = j.route_id
		 LEFT JOIN alert_route_policies rp ON rp.route_id = rr.id
		 JOIN alert_channels c ON c.id = j.channel_id
		 LEFT JOIN alert_channels ec ON ec.id = rp.escalation_channel_id
		 WHERE j.status IN ('queued', 'retrying')
		   AND j.attempt_count < j.max_attempts
		   AND j.next_attempt_at <= ?
		 ORDER BY j.next_attempt_at ASC
		 LIMIT ?`,
		now, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list due alert dispatch jobs: %w", err)
	}
	defer rows.Close()

	out := make([]AlertDispatchJobWithDetails, 0)
	for rows.Next() {
		var item AlertDispatchJobWithDetails
		var lastError, providerMessageID, sentAt sql.NullString
		var eventPayloadJSON, eventFingerprint, subjectTemplate, bodyTemplate sql.NullString
		var escalationChannelID, escalationRecipientsJSON, escalationChannelName, escalationChannelType, escalationChannelConfig sql.NullString
		if err := rows.Scan(
			&item.ID, &item.EventID, &item.RuleID, &item.RouteID, &item.ChannelID, &item.Status, &item.AttemptCount, &item.MaxAttempts, &item.NextAttemptAt, &lastError, &providerMessageID, &item.CreatedAt, &item.UpdatedAt, &sentAt,
			&item.EventType, &item.EventSeverity, &item.EventTitle, &item.EventMessage, &eventPayloadJSON, &eventFingerprint,
			&item.RuleName, &item.RuleCooldownSeconds, &subjectTemplate, &bodyTemplate,
			&item.RouteRecipientsJSON,
			&item.RouteDeliveryMode, &item.RouteDigestWindowMins, &escalationChannelID, &escalationRecipientsJSON, &item.RouteEscalationAfterFailures,
			&item.ChannelName, &item.ChannelType, &item.ChannelConfigEncrypted,
			&escalationChannelName, &escalationChannelType, &escalationChannelConfig,
		); err != nil {
			return nil, fmt.Errorf("scan due alert dispatch job: %w", err)
		}
		item.LastError = nullStringToPtr(lastError)
		item.ProviderMessageID = nullStringToPtr(providerMessageID)
		item.SentAt = nullStringToPtr(sentAt)
		item.EventPayloadJSON = nullStringToPtr(eventPayloadJSON)
		item.EventFingerprint = nullStringToPtr(eventFingerprint)
		item.RuleSubjectTemplate = nullStringToPtr(subjectTemplate)
		item.RuleBodyTemplate = nullStringToPtr(bodyTemplate)
		item.RouteEscalationChannelID = nullStringToPtr(escalationChannelID)
		item.RouteEscalationRecipientsJSON = nullStringToPtr(escalationRecipientsJSON)
		item.EscalationChannelName = nullStringToPtr(escalationChannelName)
		item.EscalationChannelType = nullStringToPtr(escalationChannelType)
		item.EscalationChannelConfigEncrypted = nullStringToPtr(escalationChannelConfig)
		out = append(out, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate due alert dispatch jobs: %w", err)
	}
	return out, nil
}

func (db *DB) MarkAlertDispatchJobSending(id string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_dispatch_jobs
		 SET status = 'sending',
		     attempt_count = attempt_count + 1,
		     updated_at = ?
		 WHERE id = ?`,
		now, id,
	); err != nil {
		return fmt.Errorf("mark alert dispatch sending: %w", err)
	}
	return nil
}

func (db *DB) MarkAlertDispatchJobSent(id, providerMessageID string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_dispatch_jobs
		 SET status = 'sent',
		     provider_message_id = ?,
		     sent_at = ?,
		     updated_at = ?
		 WHERE id = ?`,
		nullableString(providerMessageID), now, now, id,
	); err != nil {
		return fmt.Errorf("mark alert dispatch sent: %w", err)
	}
	return nil
}

func (db *DB) MarkAlertDispatchJobRetry(id string, nextAttemptAt time.Time, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_dispatch_jobs
		 SET status = 'retrying',
		     next_attempt_at = ?,
		     last_error = ?,
		     updated_at = ?
		 WHERE id = ?`,
		nextAttemptAt.UTC().Format(time.RFC3339), nullableString(lastError), now, id,
	); err != nil {
		return fmt.Errorf("mark alert dispatch retry: %w", err)
	}
	return nil
}

func (db *DB) MarkAlertDispatchJobFailed(id, lastError string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.conn.Exec(
		`UPDATE alert_dispatch_jobs
		 SET status = 'failed',
		     last_error = ?,
		     updated_at = ?
		 WHERE id = ?`,
		nullableString(lastError), now, id,
	); err != nil {
		return fmt.Errorf("mark alert dispatch failed: %w", err)
	}
	return nil
}

func scanAlertChannel(scanner interface {
	Scan(dest ...interface{}) error
}) (AlertChannel, error) {
	var item AlertChannel
	var configEncrypted, createdBy sql.NullString
	var isActive int
	if err := scanner.Scan(
		&item.ID, &item.Name, &item.ChannelType, &configEncrypted, &isActive, &createdBy, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return item, fmt.Errorf("scan alert channel: %w", err)
	}
	item.ConfigEncrypted = configEncrypted.String
	item.IsActive = intToBool(isActive)
	item.CreatedBy = nullStringToPtr(createdBy)
	return item, nil
}

func scanAlertChannelRow(row *sql.Row) (*AlertChannel, error) {
	var item AlertChannel
	var configEncrypted, createdBy sql.NullString
	var isActive int
	err := row.Scan(
		&item.ID, &item.Name, &item.ChannelType, &configEncrypted, &isActive, &createdBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan alert channel row: %w", err)
	}
	item.ConfigEncrypted = configEncrypted.String
	item.IsActive = intToBool(isActive)
	item.CreatedBy = nullStringToPtr(createdBy)
	return &item, nil
}

func scanAlertRule(scanner interface {
	Scan(dest ...interface{}) error
}) (AlertRule, error) {
	var item AlertRule
	var subjectTemplate, bodyTemplate, createdBy sql.NullString
	var enabled int
	if err := scanner.Scan(
		&item.ID, &item.Name, &item.EventType, &item.SeverityMin, &enabled, &item.CooldownSeconds, &item.MaxAttempts,
		&subjectTemplate, &bodyTemplate, &createdBy, &item.CreatedAt, &item.UpdatedAt,
	); err != nil {
		return item, fmt.Errorf("scan alert rule: %w", err)
	}
	item.Enabled = intToBool(enabled)
	item.SubjectTemplate = nullStringToPtr(subjectTemplate)
	item.BodyTemplate = nullStringToPtr(bodyTemplate)
	item.CreatedBy = nullStringToPtr(createdBy)
	return item, nil
}

func scanAlertRuleRow(row *sql.Row) (*AlertRule, error) {
	var item AlertRule
	var enabled int
	var subjectTemplate, bodyTemplate, createdBy sql.NullString
	err := row.Scan(
		&item.ID, &item.Name, &item.EventType, &item.SeverityMin, &enabled, &item.CooldownSeconds, &item.MaxAttempts,
		&subjectTemplate, &bodyTemplate, &createdBy, &item.CreatedAt, &item.UpdatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("scan alert rule row: %w", err)
	}
	item.Enabled = intToBool(enabled)
	item.SubjectTemplate = nullStringToPtr(subjectTemplate)
	item.BodyTemplate = nullStringToPtr(bodyTemplate)
	item.CreatedBy = nullStringToPtr(createdBy)
	return &item, nil
}

func parseRecipientsJSON(raw string) []string {
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
