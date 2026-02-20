package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

var emailRegex = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

type alertRuleRoutePayload struct {
	ChannelID               string   `json:"channel_id"`
	Recipients              []string `json:"recipients"`
	IsActive                *bool    `json:"is_active"`
	DeliveryMode            string   `json:"delivery_mode"`
	DigestWindowMinutes     *int     `json:"digest_window_minutes"`
	EscalationChannelID     *string  `json:"escalation_channel_id"`
	EscalationRecipients    []string `json:"escalation_recipients"`
	EscalationAfterFailures *int     `json:"escalation_after_failures"`
}

type alertRuleResponse struct {
	database.AlertRule
	Routes []database.AlertRuleRouteView `json:"routes"`
}

func (h *AdminHandler) ListAlertChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.DB.ListAlertChannels()
	if err != nil {
		slog.Error("Failed to list alert channels", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list alert channels"})
		return
	}

	type responseItem struct {
		database.AlertChannel
		Config    map[string]interface{} `json:"config"`
		HasSecret bool                   `json:"has_secret"`
	}

	out := make([]responseItem, 0, len(channels))
	for _, channel := range channels {
		decrypted, err := crypto.Decrypt(channel.ConfigEncrypted, h.Config.AppSecretKey)
		if err != nil {
			slog.Warn("Failed to decrypt alert channel config", "channel", channel.ID, "error", err)
			continue
		}
		cfg := map[string]interface{}{}
		if err := json.Unmarshal([]byte(decrypted), &cfg); err != nil {
			slog.Warn("Failed to parse alert channel config", "channel", channel.ID, "error", err)
			continue
		}
		sanitized, hasSecret := sanitizeChannelConfig(channel.ChannelType, cfg)
		out = append(out, responseItem{
			AlertChannel: channel,
			Config:       sanitized,
			HasSecret:    hasSecret,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"channels": out})
}

func (h *AdminHandler) CreateAlertChannel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Name        string                 `json:"name"`
		ChannelType string                 `json:"channel_type"`
		Config      map[string]interface{} `json:"config"`
		IsActive    *bool                  `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	channelType := strings.ToLower(strings.TrimSpace(body.ChannelType))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if !isSupportedChannelType(channelType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel_type must be smtp, resend, or brevo"})
		return
	}
	if err := validateChannelConfig(channelType, body.Config, false); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	rawConfig, _ := json.Marshal(body.Config)
	encrypted, err := crypto.Encrypt(string(rawConfig), h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to encrypt alert channel config", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to store alert channel config"})
		return
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	id, err := h.DB.CreateAlertChannel(name, channelType, encrypted, isActive, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create alert channel", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create alert channel"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "alerts.channel.created",
		Username:  strPtr(session.ClickhouseUser),
		Details:   strPtr(fmt.Sprintf("%s (%s)", name, channelType)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *AdminHandler) UpdateAlertChannel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	channel, err := h.DB.GetAlertChannelByID(id)
	if err != nil {
		slog.Error("Failed to load alert channel", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load alert channel"})
		return
	}
	if channel == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Alert channel not found"})
		return
	}

	var body struct {
		Name        *string                `json:"name"`
		ChannelType *string                `json:"channel_type"`
		Config      map[string]interface{} `json:"config"`
		IsActive    *bool                  `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := channel.Name
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	channelType := channel.ChannelType
	if body.ChannelType != nil {
		channelType = strings.ToLower(strings.TrimSpace(*body.ChannelType))
	}
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if !isSupportedChannelType(channelType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "channel_type must be smtp, resend, or brevo"})
		return
	}

	isActive := channel.IsActive
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	var encryptedConfig *string
	if body.Config != nil {
		if err := validateChannelConfig(channelType, body.Config, true); err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		rawConfig, _ := json.Marshal(body.Config)
		enc, err := crypto.Encrypt(string(rawConfig), h.Config.AppSecretKey)
		if err != nil {
			slog.Error("Failed to encrypt alert channel config", "id", id, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to store alert channel config"})
			return
		}
		encryptedConfig = &enc
	}

	if err := h.DB.UpdateAlertChannel(id, name, channelType, encryptedConfig, isActive); err != nil {
		slog.Error("Failed to update alert channel", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update alert channel"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "alerts.channel.updated",
		Username:  strPtr(session.ClickhouseUser),
		Details:   strPtr(fmt.Sprintf("%s (%s)", name, channelType)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) DeleteAlertChannel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	channel, err := h.DB.GetAlertChannelByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load alert channel"})
		return
	}
	if channel == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Alert channel not found"})
		return
	}

	if err := h.DB.DeleteAlertChannel(id); err != nil {
		slog.Error("Failed to delete alert channel", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete alert channel"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "alerts.channel.deleted",
		Username:  strPtr(session.ClickhouseUser),
		Details:   strPtr(fmt.Sprintf("%s (%s)", channel.Name, channel.ChannelType)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) TestAlertChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	channel, err := h.DB.GetAlertChannelByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load alert channel"})
		return
	}
	if channel == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Alert channel not found"})
		return
	}

	var body struct {
		Recipients []string `json:"recipients"`
		Subject    string   `json:"subject"`
		Message    string   `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	recipients, err := validateRecipients(body.Recipients)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	decrypted, err := crypto.Decrypt(channel.ConfigEncrypted, h.Config.AppSecretKey)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt alert channel config"})
		return
	}
	cfg := map[string]interface{}{}
	if err := json.Unmarshal([]byte(decrypted), &cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to parse alert channel config"})
		return
	}

	subject := strings.TrimSpace(body.Subject)
	if subject == "" {
		subject = "CH-UI Alert Channel Test"
	}
	message := strings.TrimSpace(body.Message)
	if message == "" {
		message = "This is a test notification from CH-UI."
	}

	msgID, err := alerts.SendDirect(context.Background(), channel.ChannelType, cfg, recipients, subject, message)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "provider_message_id": msgID})
}

func (h *AdminHandler) ListAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.DB.ListAlertRules()
	if err != nil {
		slog.Error("Failed to list alert rules", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list alert rules"})
		return
	}

	out := make([]alertRuleResponse, 0, len(rules))
	for _, rule := range rules {
		routes, err := h.DB.ListAlertRuleRoutes(rule.ID)
		if err != nil {
			slog.Warn("Failed to load alert rule routes", "rule", rule.ID, "error", err)
			routes = []database.AlertRuleRouteView{}
		}
		out = append(out, alertRuleResponse{
			AlertRule: rule,
			Routes:    routes,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"rules": out})
}

func (h *AdminHandler) CreateAlertRule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	var body struct {
		Name            string                  `json:"name"`
		EventType       string                  `json:"event_type"`
		SeverityMin     string                  `json:"severity_min"`
		Enabled         *bool                   `json:"enabled"`
		CooldownSeconds *int                    `json:"cooldown_seconds"`
		MaxAttempts     *int                    `json:"max_attempts"`
		SubjectTemplate string                  `json:"subject_template"`
		BodyTemplate    string                  `json:"body_template"`
		Routes          []alertRuleRoutePayload `json:"routes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	eventType := strings.ToLower(strings.TrimSpace(body.EventType))
	severityMin := strings.ToLower(strings.TrimSpace(body.SeverityMin))
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if !isSupportedEventType(eventType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "event_type must be policy.violation, schedule.failed, schedule.slow, or *"})
		return
	}
	if !isSupportedSeverity(severityMin) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "severity_min must be info, warn, error, or critical"})
		return
	}

	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	cooldownSeconds := 300
	if body.CooldownSeconds != nil {
		cooldownSeconds = *body.CooldownSeconds
	}
	maxAttempts := 5
	if body.MaxAttempts != nil {
		maxAttempts = *body.MaxAttempts
	}

	routes, err := h.validateRuleRoutes(body.Routes)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}

	id, err := h.DB.CreateAlertRule(name, eventType, severityMin, enabled, cooldownSeconds, maxAttempts, body.SubjectTemplate, body.BodyTemplate, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create alert rule", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create alert rule"})
		return
	}

	if err := h.DB.ReplaceAlertRuleRoutes(id, routes); err != nil {
		_ = h.DB.DeleteAlertRule(id)
		slog.Error("Failed to create alert rule routes", "rule", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create alert rule routes"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "alerts.rule.created",
		Username:  strPtr(session.ClickhouseUser),
		Details:   strPtr(name),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{"id": id, "success": true})
}

func (h *AdminHandler) UpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetAlertRuleByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load alert rule"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Alert rule not found"})
		return
	}

	var body struct {
		Name            *string                  `json:"name"`
		EventType       *string                  `json:"event_type"`
		SeverityMin     *string                  `json:"severity_min"`
		Enabled         *bool                    `json:"enabled"`
		CooldownSeconds *int                     `json:"cooldown_seconds"`
		MaxAttempts     *int                     `json:"max_attempts"`
		SubjectTemplate *string                  `json:"subject_template"`
		BodyTemplate    *string                  `json:"body_template"`
		Routes          *[]alertRuleRoutePayload `json:"routes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := existing.Name
	if body.Name != nil {
		name = strings.TrimSpace(*body.Name)
	}
	eventType := existing.EventType
	if body.EventType != nil {
		eventType = strings.ToLower(strings.TrimSpace(*body.EventType))
	}
	severityMin := existing.SeverityMin
	if body.SeverityMin != nil {
		severityMin = strings.ToLower(strings.TrimSpace(*body.SeverityMin))
	}
	enabled := existing.Enabled
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	cooldownSeconds := existing.CooldownSeconds
	if body.CooldownSeconds != nil {
		cooldownSeconds = *body.CooldownSeconds
	}
	maxAttempts := existing.MaxAttempts
	if body.MaxAttempts != nil {
		maxAttempts = *body.MaxAttempts
	}
	subjectTemplate := coalesceStringPtr(body.SubjectTemplate, existing.SubjectTemplate)
	bodyTemplate := coalesceStringPtr(body.BodyTemplate, existing.BodyTemplate)

	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}
	if !isSupportedEventType(eventType) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "event_type must be policy.violation, schedule.failed, schedule.slow, or *"})
		return
	}
	if !isSupportedSeverity(severityMin) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "severity_min must be info, warn, error, or critical"})
		return
	}

	if err := h.DB.UpdateAlertRule(id, name, eventType, severityMin, enabled, cooldownSeconds, maxAttempts, subjectTemplate, bodyTemplate); err != nil {
		slog.Error("Failed to update alert rule", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update alert rule"})
		return
	}

	if body.Routes != nil {
		routes, err := h.validateRuleRoutes(*body.Routes)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		if err := h.DB.ReplaceAlertRuleRoutes(id, routes); err != nil {
			slog.Error("Failed to replace alert rule routes", "rule", id, "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to update alert rule routes"})
			return
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "alerts.rule.updated",
		Username:  strPtr(session.ClickhouseUser),
		Details:   strPtr(name),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) DeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetAlertRuleByID(id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to load alert rule"})
		return
	}
	if existing == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "Alert rule not found"})
		return
	}
	if err := h.DB.DeleteAlertRule(id); err != nil {
		slog.Error("Failed to delete alert rule", "id", id, "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete alert rule"})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "alerts.rule.deleted",
		Username:  strPtr(session.ClickhouseUser),
		Details:   strPtr(existing.Name),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (h *AdminHandler) ListAlertEvents(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if raw := strings.TrimSpace(r.URL.Query().Get("limit")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			limit = n
		}
	}
	eventType := strings.TrimSpace(r.URL.Query().Get("event_type"))
	status := strings.TrimSpace(r.URL.Query().Get("status"))
	events, err := h.DB.ListAlertEvents(limit, eventType, status)
	if err != nil {
		slog.Error("Failed to list alert events", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to list alert events"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"events": events})
}

func (h *AdminHandler) validateRuleRoutes(payload []alertRuleRoutePayload) ([]database.AlertRuleRoute, error) {
	routes := make([]database.AlertRuleRoute, 0, len(payload))
	for _, item := range payload {
		channelID := strings.TrimSpace(item.ChannelID)
		if channelID == "" {
			return nil, fmt.Errorf("route channel_id is required")
		}
		channel, err := h.DB.GetAlertChannelByID(channelID)
		if err != nil {
			return nil, fmt.Errorf("failed to load channel %s", channelID)
		}
		if channel == nil {
			return nil, fmt.Errorf("channel %s not found", channelID)
		}
		recipients, err := validateRecipients(item.Recipients)
		if err != nil {
			return nil, fmt.Errorf("route channel %s: %w", channelID, err)
		}
		active := true
		if item.IsActive != nil {
			active = *item.IsActive
		}
		deliveryMode := strings.ToLower(strings.TrimSpace(item.DeliveryMode))
		if deliveryMode == "" {
			deliveryMode = "immediate"
		}
		if deliveryMode != "immediate" && deliveryMode != "digest" {
			return nil, fmt.Errorf("route channel %s: delivery_mode must be immediate or digest", channelID)
		}
		digestWindow := 0
		if item.DigestWindowMinutes != nil {
			digestWindow = *item.DigestWindowMinutes
		}
		if digestWindow < 0 || digestWindow > 1440 {
			return nil, fmt.Errorf("route channel %s: digest_window_minutes must be between 0 and 1440", channelID)
		}
		if deliveryMode == "digest" && digestWindow == 0 {
			digestWindow = 15
		}

		var escalationChannelID *string
		if item.EscalationChannelID != nil && strings.TrimSpace(*item.EscalationChannelID) != "" {
			escID := strings.TrimSpace(*item.EscalationChannelID)
			escalationChannel, err := h.DB.GetAlertChannelByID(escID)
			if err != nil {
				return nil, fmt.Errorf("route channel %s: failed to load escalation channel %s", channelID, escID)
			}
			if escalationChannel == nil {
				return nil, fmt.Errorf("route channel %s: escalation channel %s not found", channelID, escID)
			}
			escalationChannelID = &escID
		}
		escalationRecipients := []string{}
		if len(item.EscalationRecipients) > 0 {
			escalationRecipients, err = validateRecipients(item.EscalationRecipients)
			if err != nil {
				return nil, fmt.Errorf("route channel %s escalation_recipients: %w", channelID, err)
			}
		}
		escalationAfterFailures := 0
		if item.EscalationAfterFailures != nil {
			escalationAfterFailures = *item.EscalationAfterFailures
		}
		if escalationAfterFailures < 0 || escalationAfterFailures > 10 {
			return nil, fmt.Errorf("route channel %s: escalation_after_failures must be between 0 and 10", channelID)
		}
		routes = append(routes, database.AlertRuleRoute{
			ChannelID:               channelID,
			Recipients:              recipients,
			IsActive:                active,
			DeliveryMode:            deliveryMode,
			DigestWindowMinutes:     digestWindow,
			EscalationChannelID:     escalationChannelID,
			EscalationRecipients:    escalationRecipients,
			EscalationAfterFailures: escalationAfterFailures,
		})
	}
	return routes, nil
}

func sanitizeChannelConfig(channelType string, cfg map[string]interface{}) (map[string]interface{}, bool) {
	out := make(map[string]interface{}, len(cfg))
	for k, v := range cfg {
		out[k] = v
	}
	hasSecret := false
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case alerts.ChannelTypeSMTP:
		if _, ok := out["password"]; ok {
			hasSecret = strings.TrimSpace(fmt.Sprintf("%v", out["password"])) != ""
			out["password"] = ""
		}
	case alerts.ChannelTypeResend, alerts.ChannelTypeBrevo:
		if _, ok := out["api_key"]; ok {
			hasSecret = strings.TrimSpace(fmt.Sprintf("%v", out["api_key"])) != ""
			out["api_key"] = ""
		}
	}
	return out, hasSecret
}

func validateChannelConfig(channelType string, cfg map[string]interface{}, allowEmptySecret bool) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	get := func(key string) string {
		raw := strings.TrimSpace(fmt.Sprintf("%v", cfg[key]))
		if raw == "<nil>" {
			return ""
		}
		return raw
	}

	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case alerts.ChannelTypeSMTP:
		if get("host") == "" {
			return fmt.Errorf("smtp config requires host")
		}
		if get("from_email") == "" {
			return fmt.Errorf("smtp config requires from_email")
		}
		if !allowEmptySecret && get("username") != "" && get("password") == "" {
			return fmt.Errorf("smtp config requires password when username is set")
		}
	case alerts.ChannelTypeResend, alerts.ChannelTypeBrevo:
		if get("from_email") == "" {
			return fmt.Errorf("%s config requires from_email", channelType)
		}
		if !allowEmptySecret && get("api_key") == "" {
			return fmt.Errorf("%s config requires api_key", channelType)
		}
	default:
		return fmt.Errorf("unsupported channel type: %s", channelType)
	}
	return nil
}

func validateRecipients(values []string) ([]string, error) {
	if len(values) == 0 {
		return nil, fmt.Errorf("at least one recipient is required")
	}
	out := make([]string, 0, len(values))
	for _, raw := range values {
		email := strings.TrimSpace(strings.ToLower(raw))
		if email == "" {
			continue
		}
		if !emailRegex.MatchString(email) {
			return nil, fmt.Errorf("invalid recipient email: %s", raw)
		}
		out = append(out, email)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("at least one valid recipient is required")
	}
	return out, nil
}

func isSupportedChannelType(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case alerts.ChannelTypeSMTP, alerts.ChannelTypeResend, alerts.ChannelTypeBrevo:
		return true
	default:
		return false
	}
}

func isSupportedEventType(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "*", "any", alerts.EventTypePolicyViolation, alerts.EventTypeScheduleFailed, alerts.EventTypeScheduleSlow:
		return true
	default:
		return false
	}
}

func isSupportedSeverity(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case alerts.SeverityInfo, alerts.SeverityWarn, alerts.SeverityError, alerts.SeverityCritical:
		return true
	default:
		return false
	}
}

func coalesceStringPtr(v *string, fallback *string) string {
	if v != nil {
		return strings.TrimSpace(*v)
	}
	if fallback == nil {
		return ""
	}
	return strings.TrimSpace(*fallback)
}
