package alerts

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

const (
	ChannelTypeSMTP   = "smtp"
	ChannelTypeResend = "resend"
	ChannelTypeBrevo  = "brevo"
)

const (
	EventTypePolicyViolation = "policy.violation"
	EventTypeScheduleFailed  = "schedule.failed"
	EventTypeScheduleSlow    = "schedule.slow"
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
}

func NewDispatcher(db *database.DB, cfg *config.Config) *Dispatcher {
	return &Dispatcher{
		db:     db,
		cfg:    cfg,
		stopCh: make(chan struct{}),
		http: &http.Client{
			Timeout: 15 * time.Second,
		},
	}
}

func (d *Dispatcher) Start() {
	go func() {
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
	if !d.cfg.IsPro() {
		return
	}
	d.materializeEventJobs()
	d.processDueJobs()
	d.processDueDigests()
}

func (d *Dispatcher) materializeEventJobs() {
	events, err := d.db.ListNewAlertEvents(maxNewEventsPerTick)
	if err != nil {
		slog.Error("Alert dispatcher failed to list new events", "error", err)
		return
	}
	if len(events) == 0 {
		return
	}

	rules, err := d.db.ListEnabledAlertRules()
	if err != nil {
		slog.Error("Alert dispatcher failed to list enabled rules", "error", err)
		return
	}

	routesByRule := make(map[string][]database.AlertRuleRouteView)
	now := time.Now().UTC()
	for _, event := range events {
		for _, rule := range rules {
			if !ruleMatchesEvent(rule, event) {
				continue
			}
			routes, ok := routesByRule[rule.ID]
			if !ok {
				routes, err = d.db.ListActiveAlertRuleRoutes(rule.ID)
				if err != nil {
					slog.Error("Alert dispatcher failed to list active routes", "rule", rule.ID, "error", err)
					continue
				}
				routesByRule[rule.ID] = routes
			}
			for _, route := range routes {
				if len(route.Recipients) == 0 {
					continue
				}
				deliveryMode := strings.ToLower(strings.TrimSpace(route.DeliveryMode))
				if deliveryMode == "digest" {
					if err := d.db.UpsertAlertRouteDigest(rule, route, event, now); err != nil {
						slog.Error("Alert dispatcher failed to upsert digest batch", "event", event.ID, "rule", rule.ID, "route", route.ID, "error", err)
					}
					continue
				}
				if event.Fingerprint != nil && strings.TrimSpace(*event.Fingerprint) != "" && rule.CooldownSeconds > 0 {
					since := now.Add(-time.Duration(rule.CooldownSeconds) * time.Second)
					exists, err := d.db.HasRecentAlertDispatch(route.ID, *event.Fingerprint, since)
					if err != nil {
						slog.Warn("Alert dispatcher dedupe check failed", "route", route.ID, "error", err)
					} else if exists {
						continue
					}
				}
				if _, err := d.db.CreateAlertDispatchJob(event.ID, rule.ID, route.ID, route.ChannelID, rule.MaxAttempts, now); err != nil {
					slog.Error("Alert dispatcher failed to create dispatch job", "event", event.ID, "rule", rule.ID, "route", route.ID, "error", err)
				}
			}
		}

		if err := d.db.MarkAlertEventProcessed(event.ID); err != nil {
			slog.Warn("Alert dispatcher failed to mark event processed", "event", event.ID, "error", err)
		}
	}
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
		if err := d.db.MarkAlertDispatchJobSending(job.ID); err != nil {
			slog.Warn("Alert dispatcher failed to mark job sending", "job", job.ID, "error", err)
			continue
		}

		recipients := parseRecipients(job.RouteRecipientsJSON)
		if len(recipients) == 0 {
			_ = d.db.MarkAlertDispatchJobFailed(job.ID, "route has no recipients")
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

		providerMessageID, err := d.sendByChannelType(context.Background(), job.ChannelType, channelConfig, recipients, subject, body)

		if err != nil {
			nextAttempt := job.AttemptCount + 1
			if nextAttempt >= job.MaxAttempts {
				failureMessage := err.Error()
				if escalationNote := d.tryEscalationForDispatchJob(job, subject, body, failureMessage, nextAttempt); escalationNote != "" {
					failureMessage = failureMessage + " | " + escalationNote
				}
				_ = d.db.MarkAlertDispatchJobFailed(job.ID, failureMessage)
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

func (d *Dispatcher) processDueDigests() {
	digests, err := d.db.ListDueAlertRouteDigests(maxJobsPerTick)
	if err != nil {
		slog.Error("Alert dispatcher failed to list due digests", "error", err)
		return
	}
	if len(digests) == 0 {
		return
	}

	for _, digest := range digests {
		if err := d.db.MarkAlertRouteDigestSending(digest.ID); err != nil {
			slog.Warn("Alert dispatcher failed to mark digest sending", "digest", digest.ID, "error", err)
			continue
		}

		recipients := parseRecipients(digest.RouteRecipientsJSON)
		if len(recipients) == 0 {
			_ = d.db.MarkAlertRouteDigestFailed(digest.ID, "digest route has no recipients")
			continue
		}

		decrypted, err := crypto.Decrypt(digest.ChannelConfigEncrypted, d.cfg.AppSecretKey)
		if err != nil {
			_ = d.db.MarkAlertRouteDigestFailed(digest.ID, "decrypt channel config: "+err.Error())
			continue
		}
		var channelConfig map[string]interface{}
		if err := json.Unmarshal([]byte(decrypted), &channelConfig); err != nil {
			_ = d.db.MarkAlertRouteDigestFailed(digest.ID, "parse channel config: "+err.Error())
			continue
		}

		subject := fmt.Sprintf("[CH-UI Digest][%s][%s] %d events", strings.ToUpper(digest.Severity), digest.EventType, digest.EventCount)
		body := renderDigestBody(digest)

		_, err = d.sendByChannelType(context.Background(), digest.ChannelType, channelConfig, recipients, subject, body)
		if err != nil {
			nextAttempt := digest.AttemptCount + 1
			if nextAttempt >= digest.MaxAttempts {
				failureMessage := err.Error()
				if escalationNote := d.tryEscalationForDigest(digest, subject, body, failureMessage, nextAttempt); escalationNote != "" {
					failureMessage = failureMessage + " | " + escalationNote
				}
				_ = d.db.MarkAlertRouteDigestFailed(digest.ID, failureMessage)
				continue
			}
			backoff := retryBackoff(nextAttempt)
			_ = d.db.MarkAlertRouteDigestRetry(digest.ID, time.Now().UTC().Add(backoff), err.Error())
			continue
		}

		if err := d.db.MarkAlertRouteDigestSent(digest.ID); err != nil {
			slog.Warn("Alert dispatcher failed to mark digest sent", "digest", digest.ID, "error", err)
		}
	}
}

// SendDirect sends a one-off notification without queueing.
func SendDirect(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	d := &Dispatcher{
		http: &http.Client{Timeout: 15 * time.Second},
	}
	return d.sendByChannelType(ctx, channelType, channelConfig, recipients, subject, body)
}

func (d *Dispatcher) sendByChannelType(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	switch strings.ToLower(channelType) {
	case ChannelTypeSMTP:
		return d.sendSMTP(ctx, channelConfig, recipients, subject, body)
	case ChannelTypeResend:
		return d.sendResend(ctx, channelConfig, recipients, subject, body)
	case ChannelTypeBrevo:
		return d.sendBrevo(ctx, channelConfig, recipients, subject, body)
	default:
		return "", fmt.Errorf("unsupported channel type: %s", channelType)
	}
}

func ruleMatchesEvent(rule database.AlertRule, event database.AlertEvent) bool {
	eventType := strings.ToLower(strings.TrimSpace(event.EventType))
	ruleType := strings.ToLower(strings.TrimSpace(rule.EventType))
	if ruleType != "*" && ruleType != "any" && ruleType != eventType {
		return false
	}
	return severityRank(event.Severity) >= severityRank(rule.SeverityMin)
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

func parseStringList(raw string) []string {
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

func (d *Dispatcher) tryEscalationForDispatchJob(job database.AlertDispatchJobWithDetails, subject, body, rootErr string, failedAttempt int) string {
	if job.RouteEscalationChannelID == nil || strings.TrimSpace(*job.RouteEscalationChannelID) == "" {
		return ""
	}
	if job.RouteEscalationAfterFailures > 0 && failedAttempt < job.RouteEscalationAfterFailures {
		return ""
	}
	if job.EscalationChannelType == nil || job.EscalationChannelConfigEncrypted == nil {
		return "escalation skipped: channel metadata unavailable"
	}
	recipients := parseRecipients(coalesce(job.RouteEscalationRecipientsJSON, ""))
	if len(recipients) == 0 {
		recipients = parseRecipients(job.RouteRecipientsJSON)
	}
	if len(recipients) == 0 {
		return "escalation skipped: no escalation recipients"
	}
	decrypted, err := crypto.Decrypt(*job.EscalationChannelConfigEncrypted, d.cfg.AppSecretKey)
	if err != nil {
		return "escalation decrypt failed: " + err.Error()
	}
	cfg := map[string]interface{}{}
	if err := json.Unmarshal([]byte(decrypted), &cfg); err != nil {
		return "escalation config parse failed: " + err.Error()
	}
	escalationSubject := "[ESCALATED] " + subject
	escalationBody := body + "\n\nEscalation reason:\n" + rootErr
	if _, err := d.sendByChannelType(context.Background(), *job.EscalationChannelType, cfg, recipients, escalationSubject, escalationBody); err != nil {
		return "escalation send failed: " + err.Error()
	}
	return "escalated via " + coalesce(job.EscalationChannelName, "channel")
}

func (d *Dispatcher) tryEscalationForDigest(digest database.AlertRouteDigestWithDetails, subject, body, rootErr string, failedAttempt int) string {
	if digest.EscalationChannelID == nil || strings.TrimSpace(*digest.EscalationChannelID) == "" {
		return ""
	}
	if digest.EscalationAfterFailures > 0 && failedAttempt < digest.EscalationAfterFailures {
		return ""
	}
	if digest.EscalationChannelType == nil || digest.EscalationChannelConfigEncrypted == nil {
		return "digest escalation skipped: channel metadata unavailable"
	}
	recipients := parseRecipients(coalesce(digest.EscalationRecipientsJSON, ""))
	if len(recipients) == 0 {
		recipients = parseRecipients(digest.RouteRecipientsJSON)
	}
	if len(recipients) == 0 {
		return "digest escalation skipped: no recipients"
	}
	decrypted, err := crypto.Decrypt(*digest.EscalationChannelConfigEncrypted, d.cfg.AppSecretKey)
	if err != nil {
		return "digest escalation decrypt failed: " + err.Error()
	}
	cfg := map[string]interface{}{}
	if err := json.Unmarshal([]byte(decrypted), &cfg); err != nil {
		return "digest escalation config parse failed: " + err.Error()
	}
	escalationSubject := "[ESCALATED] " + subject
	escalationBody := body + "\n\nEscalation reason:\n" + rootErr
	if _, err := d.sendByChannelType(context.Background(), *digest.EscalationChannelType, cfg, recipients, escalationSubject, escalationBody); err != nil {
		return "digest escalation send failed: " + err.Error()
	}
	return "digest escalated via " + coalesce(digest.EscalationChannelName, "channel")
}

func renderDigestBody(digest database.AlertRouteDigestWithDetails) string {
	titles := parseStringList(digest.TitlesJSON)
	var b strings.Builder
	b.WriteString("CH-UI Alert Digest\n\n")
	b.WriteString("Event type: " + digest.EventType + "\n")
	b.WriteString("Severity: " + strings.ToUpper(digest.Severity) + "\n")
	b.WriteString("Events in window: " + strconv.Itoa(digest.EventCount) + "\n")
	b.WriteString("Window: " + digest.BucketStart + " -> " + digest.BucketEnd + "\n")
	b.WriteString("Channel: " + digest.ChannelName + " (" + digest.ChannelType + ")\n")
	if len(titles) > 0 {
		b.WriteString("\nTitles:\n")
		for i, title := range titles {
			b.WriteString(fmt.Sprintf("%d. %s\n", i+1, title))
			if i >= 14 {
				remaining := len(titles) - (i + 1)
				if remaining > 0 {
					b.WriteString(fmt.Sprintf("... and %d more\n", remaining))
				}
				break
			}
		}
	}
	return b.String()
}

func coalesce(v *string, fallback string) string {
	if v == nil || strings.TrimSpace(*v) == "" {
		return fallback
	}
	return *v
}

func stringCfg(cfg map[string]interface{}, key string) string {
	v := strings.TrimSpace(fmt.Sprintf("%v", cfg[key]))
	if v == "<nil>" {
		return ""
	}
	return v
}

func boolCfg(cfg map[string]interface{}, key string, defaultVal bool) bool {
	raw, ok := cfg[key]
	if !ok {
		return defaultVal
	}
	switch v := raw.(type) {
	case bool:
		return v
	case float64:
		return v != 0
	case string:
		val := strings.ToLower(strings.TrimSpace(v))
		return val == "1" || val == "true" || val == "yes"
	default:
		return defaultVal
	}
}

func intCfg(cfg map[string]interface{}, key string, defaultVal int) int {
	raw, ok := cfg[key]
	if !ok {
		return defaultVal
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
			return n
		}
	}
	return defaultVal
}

func (d *Dispatcher) sendSMTP(ctx context.Context, cfg map[string]interface{}, recipients []string, subject, body string) (string, error) {
	host := stringCfg(cfg, "host")
	fromEmail := stringCfg(cfg, "from_email")
	username := stringCfg(cfg, "username")
	password := stringCfg(cfg, "password")
	fromName := stringCfg(cfg, "from_name")
	if host == "" || fromEmail == "" {
		return "", fmt.Errorf("smtp config requires host and from_email")
	}

	port := intCfg(cfg, "port", 587)
	addr := fmt.Sprintf("%s:%d", host, port)
	useTLS := boolCfg(cfg, "use_tls", false)
	startTLS := boolCfg(cfg, "starttls", !useTLS)
	insecureSkipVerify := boolCfg(cfg, "insecure_skip_verify", false)

	fromHeader := fromEmail
	if fromName != "" {
		fromHeader = fmt.Sprintf("%s <%s>", fromName, fromEmail)
	}

	msg := []byte("From: " + fromHeader + "\r\n" +
		"To: " + strings.Join(recipients, ",") + "\r\n" +
		"Subject: " + subject + "\r\n" +
		"MIME-Version: 1.0\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		body)

	var auth smtp.Auth
	if username != "" {
		auth = smtp.PlainAuth("", username, password, host)
	}

	if useTLS {
		conn, err := tls.Dial("tcp", addr, &tls.Config{
			ServerName:         host,
			InsecureSkipVerify: insecureSkipVerify,
		})
		if err != nil {
			return "", fmt.Errorf("smtp tls dial: %w", err)
		}
		defer conn.Close()

		client, err := smtp.NewClient(conn, host)
		if err != nil {
			return "", fmt.Errorf("smtp new client: %w", err)
		}
		defer client.Close()

		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return "", fmt.Errorf("smtp auth: %w", err)
			}
		}
		if err := client.Mail(fromEmail); err != nil {
			return "", fmt.Errorf("smtp mail: %w", err)
		}
		for _, rcpt := range recipients {
			if err := client.Rcpt(rcpt); err != nil {
				return "", fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
			}
		}
		w, err := client.Data()
		if err != nil {
			return "", fmt.Errorf("smtp data: %w", err)
		}
		if _, err := w.Write(msg); err != nil {
			_ = w.Close()
			return "", fmt.Errorf("smtp write: %w", err)
		}
		if err := w.Close(); err != nil {
			return "", fmt.Errorf("smtp close data: %w", err)
		}
		if err := client.Quit(); err != nil {
			return "", fmt.Errorf("smtp quit: %w", err)
		}
		return "smtp", nil
	}

	if startTLS {
		client, err := smtp.Dial(addr)
		if err != nil {
			return "", fmt.Errorf("smtp dial: %w", err)
		}
		defer client.Close()

		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(&tls.Config{
				ServerName:         host,
				InsecureSkipVerify: insecureSkipVerify,
			}); err != nil {
				return "", fmt.Errorf("smtp starttls: %w", err)
			}
		}
		if auth != nil {
			if err := client.Auth(auth); err != nil {
				return "", fmt.Errorf("smtp auth: %w", err)
			}
		}
		if err := client.Mail(fromEmail); err != nil {
			return "", fmt.Errorf("smtp mail: %w", err)
		}
		for _, rcpt := range recipients {
			if err := client.Rcpt(rcpt); err != nil {
				return "", fmt.Errorf("smtp rcpt %s: %w", rcpt, err)
			}
		}
		w, err := client.Data()
		if err != nil {
			return "", fmt.Errorf("smtp data: %w", err)
		}
		if _, err := w.Write(msg); err != nil {
			_ = w.Close()
			return "", fmt.Errorf("smtp write: %w", err)
		}
		if err := w.Close(); err != nil {
			return "", fmt.Errorf("smtp close data: %w", err)
		}
		if err := client.Quit(); err != nil {
			return "", fmt.Errorf("smtp quit: %w", err)
		}
		return "smtp", nil
	}

	if err := smtp.SendMail(addr, auth, fromEmail, recipients, msg); err != nil {
		return "", fmt.Errorf("smtp sendmail: %w", err)
	}
	return "smtp", nil
}

func (d *Dispatcher) sendResend(ctx context.Context, cfg map[string]interface{}, recipients []string, subject, body string) (string, error) {
	apiKey := stringCfg(cfg, "api_key")
	fromEmail := stringCfg(cfg, "from_email")
	fromName := stringCfg(cfg, "from_name")
	baseURL := stringCfg(cfg, "base_url")
	if baseURL == "" {
		baseURL = "https://api.resend.com"
	}
	if apiKey == "" || fromEmail == "" {
		return "", fmt.Errorf("resend config requires api_key and from_email")
	}

	from := fromEmail
	if fromName != "" {
		from = fmt.Sprintf("%s <%s>", fromName, fromEmail)
	}
	payload := map[string]interface{}{
		"from":    from,
		"to":      recipients,
		"subject": subject,
		"text":    body,
	}
	raw, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/emails", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("resend request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := d.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("resend send: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("resend error (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out struct {
		ID string `json:"id"`
	}
	_ = json.Unmarshal(data, &out)
	return out.ID, nil
}

func (d *Dispatcher) sendBrevo(ctx context.Context, cfg map[string]interface{}, recipients []string, subject, body string) (string, error) {
	apiKey := stringCfg(cfg, "api_key")
	fromEmail := stringCfg(cfg, "from_email")
	fromName := stringCfg(cfg, "from_name")
	baseURL := stringCfg(cfg, "base_url")
	if baseURL == "" {
		baseURL = "https://api.brevo.com"
	}
	if apiKey == "" || fromEmail == "" {
		return "", fmt.Errorf("brevo config requires api_key and from_email")
	}

	to := make([]map[string]string, 0, len(recipients))
	for _, r := range recipients {
		to = append(to, map[string]string{"email": r})
	}

	payload := map[string]interface{}{
		"sender": map[string]string{
			"name":  fromName,
			"email": fromEmail,
		},
		"to":          to,
		"subject":     subject,
		"textContent": body,
	}
	raw, _ := json.Marshal(payload)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, strings.TrimRight(baseURL, "/")+"/v3/smtp/email", bytes.NewReader(raw))
	if err != nil {
		return "", fmt.Errorf("brevo request: %w", err)
	}
	req.Header.Set("api-key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := d.http.Do(req)
	if err != nil {
		return "", fmt.Errorf("brevo send: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("brevo error (%d): %s", resp.StatusCode, strings.TrimSpace(string(data)))
	}

	var out struct {
		MessageID string `json:"messageId"`
	}
	_ = json.Unmarshal(data, &out)
	return out.MessageID, nil
}
