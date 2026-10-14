// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/mail"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

const (
	ChannelTypeSMTP   = mail.ChannelTypeSMTP
	ChannelTypeResend = mail.ChannelTypeResend
	ChannelTypeBrevo  = mail.ChannelTypeBrevo
)

const (
	EventTypePolicyViolation  = "policy.violation"
	EventTypeScheduleFailed   = "schedule.failed"
	EventTypeScheduleSlow     = "schedule.slow"
	EventTypeTelemetryMonitor = "telemetry.monitor"
	EventTypeAccessDrift      = "access.drift"
)

const (
	SeverityInfo     = "info"
	SeverityWarn     = "warn"
	SeverityError    = "error"
	SeverityCritical = "critical"
)

const (
	dispatchTickInterval = 8 * time.Second
	maxNewEventsPerTick  = 100
	maxJobsPerTick       = 30
)

type Dispatcher struct {
	db     *database.DB
	cfg    *config.Config
	stopCh chan struct{}
	http   *http.Client
	// webhookHTTP overrides the guarded webhook client (tests only).
	webhookHTTP *http.Client
	// allow reports whether alerts may be delivered now. It follows the
	// license at runtime and stays open through the grace window, like the
	// other background Pro workers, so monitors that keep firing in grace
	// still reach their channels.
	allow func() bool
}

func NewDispatcher(db *database.DB, cfg *config.Config) *Dispatcher {
	return &Dispatcher{
		db:     db,
		cfg:    cfg,
		stopCh: make(chan struct{}),
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
		allow: config.NewProGate("Alert dispatcher", cfg.ProAccess).Allow,
	}
}

func (d *Dispatcher) Start() {
	go func() {
		defer safe.Recover("alert-dispatcher")
		slog.Info("Alert dispatcher started", "interval", dispatchTickInterval)
		ticker := time.NewTicker(dispatchTickInterval)
		defer ticker.Stop()

		for {
			select {
			case <-d.stopCh:
				slog.Info("Alert dispatcher stopped")
				return
			case <-ticker.C:
				d.tick()
			}
		}
	}()
}

func (d *Dispatcher) Stop() {
	close(d.stopCh)
}

func (d *Dispatcher) tick() {
	if d.allow == nil || !d.allow() {
		return
	}
	d.materializeEventJobs()
	d.processDueJobs()
}

func (d *Dispatcher) materializeEventJobs() {
	now := time.Now().UTC()
	// Silences that ran out release what they held back, and expired acks
	// stop covering, before this tick's events are looked at.
	endLapsedSilences(d.db, now)
	expireAcks(d.db, now)

	events, err := d.db.ListNewAlertEvents(maxNewEventsPerTick)
	if err != nil {
		slog.Error("Alert dispatcher failed to list new events", "error", err)
		return
	}
	if len(events) == 0 {
		return
	}

	// Active silences and open acks, loaded once per tick. Without them no
	// event is delivered: it stays 'new' for the next tick.
	sup, err := loadSuppressor(d.db, now)
	if err != nil {
		slog.Error("Alert dispatcher failed to load silences and acks", "error", err)
		return
	}

	rules, err := d.db.ListEnabledAlertRules()
	if err != nil {
		slog.Error("Alert dispatcher failed to list enabled rules", "error", err)
		return
	}

	channelsByRule := make(map[string][]database.AlertRuleChannelView)
	for _, event := range events {
		if status, by := sup.decide(event); status != "" {
			if err := d.db.SuppressAlertEvent(event.ID, status, by); err != nil {
				slog.Warn("Alert dispatcher failed to mark event suppressed", "event", event.ID, "error", err)
			}
			continue
		}
		// complete stays true only when every matching rule was fully
		// materialized. Otherwise the event stays 'new' and is retried next
		// tick; jobs already created for it are not duplicated.
		complete := true
		for _, rule := range rules {
			if !ruleMatchesEvent(rule, event) {
				continue
			}
			bindings, ok := channelsByRule[rule.ID]
			if !ok {
				bindings, err = d.db.ListActiveAlertRuleChannels(rule.ID)
				if err != nil {
					slog.Error("Alert dispatcher failed to list active rule channels", "rule", rule.ID, "error", err)
					complete = false
					continue
				}
				channelsByRule[rule.ID] = bindings
			}
			for _, binding := range bindings {
				if len(binding.Recipients) == 0 && !channelNeedsNoRecipients(binding.ChannelType) {
					continue
				}
				exists, err := d.db.AlertDispatchJobExists(event.ID, rule.ID, binding.ChannelID)
				if err != nil {
					slog.Error("Alert dispatcher failed to check existing dispatch job", "event", event.ID, "rule", rule.ID, "error", err)
					complete = false
					continue
				}
				if exists {
					continue
				}
				if event.Fingerprint != nil && strings.TrimSpace(*event.Fingerprint) != "" && rule.CooldownSeconds > 0 {
					since := now.Add(-time.Duration(rule.CooldownSeconds) * time.Second)
					exists, err := d.db.HasRecentAlertDispatch(rule.ID, binding.ChannelID, *event.Fingerprint, since)
					if err != nil {
						slog.Warn("Alert dispatcher dedupe check failed", "rule", rule.ID, "channel", binding.ChannelID, "error", err)
					} else if exists {
						continue
					}
				}
				if _, err := d.db.CreateAlertDispatchJob(event.ID, rule.ID, binding.ChannelID, binding.RecipientsJSON, rule.MaxAttempts, now); err != nil {
					slog.Error("Alert dispatcher failed to create dispatch job", "event", event.ID, "rule", rule.ID, "channel", binding.ChannelID, "error", err)
					complete = false
				}
			}
		}

		if !complete {
			continue
		}
		if err := d.db.MarkAlertEventProcessed(event.ID); err != nil {
			slog.Warn("Alert dispatcher failed to mark event processed", "event", event.ID, "error", err)
		}
	}
}

// channelNeedsNoRecipients reports whether a channel type delivers without
// email recipients (webhooks post to their configured URL).
func channelNeedsNoRecipients(channelType string) bool {
	return strings.EqualFold(strings.TrimSpace(channelType), ChannelTypeWebhook)
}

func (d *Dispatcher) processDueJobs() {
	jobs, err := d.db.ListDueAlertDispatchJobs(maxJobsPerTick)
	if err != nil {
		slog.Error("Alert dispatcher failed to list due jobs", "error", err)
		return
	}
	if len(jobs) == 0 {
		return
	}

	for _, job := range jobs {
		claimed, err := d.db.ClaimAlertDispatchJob(job.ID, time.Now().UTC())
		if err != nil {
			slog.Warn("Alert dispatcher failed to claim job", "job", job.ID, "error", err)
			continue
		}
		if !claimed {
			continue
		}

		recipients := parseRecipients(job.RecipientsJSON)
		if len(recipients) == 0 && !channelNeedsNoRecipients(job.ChannelType) {
			_ = d.db.MarkAlertDispatchJobFailed(job.ID, "job has no recipients")
			continue
		}

		decrypted, err := crypto.Decrypt(job.ChannelConfigEncrypted, d.cfg.AppSecretKey)
		if err != nil {
			_ = d.db.MarkAlertDispatchJobFailed(job.ID, "decrypt channel config: "+err.Error())
			continue
		}

		var channelConfig map[string]interface{}
		if err := json.Unmarshal([]byte(decrypted), &channelConfig); err != nil {
			_ = d.db.MarkAlertDispatchJobFailed(job.ID, "parse channel config: "+err.Error())
			continue
		}

		subject := renderTemplate(coalesce(job.RuleSubjectTemplate,
			fmt.Sprintf("[CH-UI][%s][%s] %s", strings.ToUpper(job.EventSeverity), job.EventType, job.EventTitle)),
			job,
		)
		body := renderTemplate(coalesce(job.RuleBodyTemplate, defaultBody(job)), job)

		var providerMessageID string
		if channelNeedsNoRecipients(job.ChannelType) {
			providerMessageID, err = SendWebhook(context.Background(), d.webhookHTTP, channelConfig, d.webhookEvent(job))
		} else {
			providerMessageID, err = d.sendByChannelType(context.Background(), job.ChannelType, channelConfig, recipients, subject, body)
		}

		if err != nil {
			nextAttempt := job.AttemptCount + 1
			if nextAttempt >= job.MaxAttempts {
				_ = d.db.MarkAlertDispatchJobFailed(job.ID, err.Error())
				continue
			}
			backoff := retryBackoff(nextAttempt)
			_ = d.db.MarkAlertDispatchJobRetry(job.ID, time.Now().UTC().Add(backoff), err.Error())
			continue
		}

		if err := d.db.MarkAlertDispatchJobSent(job.ID, providerMessageID); err != nil {
			slog.Warn("Alert dispatcher failed to mark job sent", "job", job.ID, "error", err)
		}
	}
}

// webhookEvent builds the webhook body fields for a dispatch job.
func (d *Dispatcher) webhookEvent(job database.AlertDispatchJobWithDetails) WebhookEvent {
	ev := WebhookEvent{
		EventID:     job.EventID,
		EventType:   job.EventType,
		Severity:    job.EventSeverity,
		Title:       job.EventTitle,
		Message:     job.EventMessage,
		Fingerprint: coalesce(job.EventFingerprint, ""),
		Timestamp:   job.EventCreatedAt,
		Link:        EventLink(d.appURL(), job.EventPayloadJSON),
		RuleName:    job.RuleName,
	}
	if ev.Timestamp == "" {
		ev.Timestamp = job.CreatedAt
	}
	if job.EventPayloadJSON != nil && json.Valid([]byte(*job.EventPayloadJSON)) {
		ev.Payload = json.RawMessage(*job.EventPayloadJSON)
	}
	return ev
}

func (d *Dispatcher) appURL() string {
	if d.cfg == nil {
		return ""
	}
	return d.cfg.AppURL
}

// AlertsLink is the CH-UI page that lists alert events, or "" without an
// app URL.
func AlertsLink(appURL string) string {
	base := strings.TrimRight(strings.TrimSpace(appURL), "/")
	if base == "" {
		return ""
	}
	return base + "/governance?section=alerts"
}

// EventLink is the CH-UI page an event points at: the path its payload
// names in link_path, or the alert events page. "" without an app URL.
func EventLink(appURL string, payloadJSON *string) string {
	base := strings.TrimRight(strings.TrimSpace(appURL), "/")
	if base == "" {
		return ""
	}
	if payloadJSON != nil {
		var p struct {
			LinkPath string `json:"link_path"`
		}
		if json.Unmarshal([]byte(*payloadJSON), &p) == nil && strings.HasPrefix(p.LinkPath, "/") && !strings.HasPrefix(p.LinkPath, "//") {
			return base + p.LinkPath
		}
	}
	return AlertsLink(appURL)
}

// SendDirect sends a one-off notification without queueing.
func SendDirect(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	return mail.Send(ctx, channelType, channelConfig, recipients, subject, body)
}

func (d *Dispatcher) sendByChannelType(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	return (&mail.Sender{HTTP: d.http}).Send(ctx, channelType, channelConfig, recipients, subject, body)
}

func ruleMatchesEvent(rule database.AlertRule, event database.AlertEvent) bool {
	// A rule scoped to connections only takes their events; an event
	// without a connection matches only unscoped rules.
	if len(rule.ConnectionIDs) > 0 {
		if event.ConnectionID == nil {
			return false
		}
		conn := strings.TrimSpace(*event.ConnectionID)
		found := false
		for _, id := range rule.ConnectionIDs {
			if strings.TrimSpace(id) == conn {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	ruleType := strings.ToLower(strings.TrimSpace(rule.EventType))
	if !EventTypeMatches(ruleType, eventType) {
		return false
	}
	return severityRank(event.Severity) >= severityRank(rule.SeverityMin)
}

// EventTypeMatches reports whether a rule's event type selects an event
// type: "*" and "any" match everything, "health.*" every type under the
// "health." prefix, anything else only itself. Case and surrounding space
// are ignored.
func EventTypeMatches(ruleType, eventType string) bool {
	ruleType = strings.ToLower(strings.TrimSpace(ruleType))
	eventType = strings.ToLower(strings.TrimSpace(eventType))
	switch {
	case ruleType == "*" || ruleType == "any":
		return true
	case strings.HasSuffix(ruleType, ".*"):
		return strings.HasPrefix(eventType, strings.TrimSuffix(ruleType, "*"))
	default:
		return ruleType == eventType
	}
}

func severityRank(s string) int {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case SeverityInfo:
		return 1
	case SeverityWarn:
		return 2
	case SeverityError:
		return 3
	case SeverityCritical:
		return 4
	default:
		return 0
	}
}

func parseRecipients(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return []string{}
	}
	var vals []string
	if err := json.Unmarshal([]byte(raw), &vals); err != nil {
		return []string{}
	}
	out := make([]string, 0, len(vals))
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			out = append(out, v)
		}
	}
	return out
}

func defaultBody(job database.AlertDispatchJobWithDetails) string {
	var b strings.Builder
	b.WriteString("CH-UI Alert\n\n")
	b.WriteString("Type: " + job.EventType + "\n")
	b.WriteString("Severity: " + strings.ToUpper(job.EventSeverity) + "\n")
	b.WriteString("Title: " + job.EventTitle + "\n")
	b.WriteString("Message: " + job.EventMessage + "\n")
	b.WriteString("Channel: " + job.ChannelName + " (" + job.ChannelType + ")\n")
	if job.EventPayloadJSON != nil && strings.TrimSpace(*job.EventPayloadJSON) != "" {
		b.WriteString("\nPayload:\n")
		b.WriteString(*job.EventPayloadJSON)
	}
	return b.String()
}

func renderTemplate(tpl string, job database.AlertDispatchJobWithDetails) string {
	out := tpl
	repl := map[string]string{
		"{{event_type}}":   job.EventType,
		"{{severity}}":     job.EventSeverity,
		"{{title}}":        job.EventTitle,
		"{{message}}":      job.EventMessage,
		"{{channel_name}}": job.ChannelName,
		"{{channel_type}}": job.ChannelType,
		"{{payload_json}}": coalesce(job.EventPayloadJSON, ""),
		"{{created_at}}":   job.CreatedAt,
		"{{event_id}}":     job.EventID,
		"{{rule_name}}":    job.RuleName,
	}
	for key, val := range repl {
		out = strings.ReplaceAll(out, key, val)
	}
	return out
}

func retryBackoff(attempt int) time.Duration {
	if attempt <= 0 {
		return 10 * time.Second
	}
	base := 10 * time.Second
	multiplier := math.Pow(2, float64(attempt-1))
	d := time.Duration(multiplier * float64(base))
	if d > 30*time.Minute {
		return 30 * time.Minute
	}
	return d
}

func coalesce(v *string, fallback string) string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return fallback
	}
	return *v
}
