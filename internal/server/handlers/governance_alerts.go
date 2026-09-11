// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

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

type alertRuleChannelPayload struct {
	ChannelID  string   `json:"channel_id"`
	Recipients []string `json:"recipients"`
	IsActive   *bool    `json:"is_active"`
}

type alertRuleResponse struct {
	database.AlertRule
	Channels []database.AlertRuleChannelView `json:"channels"`
}

func (h *GovernanceHandler) ListAlertChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := h.DB.ListAlertChannels()
	if err != nil {
		slog.Error("Failed to list alert channels", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list alert channels")
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

func (h *GovernanceHandler) CreateAlertChannel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body struct {
		Name        string                 `json:"name"`
		ChannelType string                 `json:"channel_type"`
		Config      map[string]interface{} `json:"config"`
		IsActive    *bool                  `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(body.Name)
	channelType := strings.ToLower(strings.TrimSpace(body.ChannelType))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !isSupportedChannelType(channelType) {
		writeError(w, http.StatusBadRequest, "channel_type must be smtp, resend, or brevo")
		return
	}
	if err := validateChannelConfig(channelType, body.Config, false); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	rawConfig, _ := json.Marshal(body.Config)
	encrypted, err := crypto.Encrypt(string(rawConfig), h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to encrypt alert channel config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to store alert channel config")
		return
	}

	isActive := true
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	id, err := h.DB.CreateAlertChannel(name, channelType, encrypted, isActive, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create alert channel", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create alert channel")
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

func (h *GovernanceHandler) UpdateAlertChannel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	channel, err := h.DB.GetAlertChannelByID(id)
	if err != nil {
		slog.Error("Failed to load alert channel", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load alert channel")
		return
	}
	if channel == nil {
		writeError(w, http.StatusNotFound, "Alert channel not found")
		return
	}

	var body struct {
		Name        *string                `json:"name"`
		ChannelType *string                `json:"channel_type"`
		Config      map[string]interface{} `json:"config"`
		IsActive    *bool                  `json:"is_active"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
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
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !isSupportedChannelType(channelType) {
		writeError(w, http.StatusBadRequest, "channel_type must be smtp, resend, or brevo")
		return
	}

	isActive := channel.IsActive
	if body.IsActive != nil {
		isActive = *body.IsActive
	}

	var encryptedConfig *string
	if body.Config != nil {
		if err := validateChannelConfig(channelType, body.Config, true); err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		rawConfig, _ := json.Marshal(body.Config)
		enc, err := crypto.Encrypt(string(rawConfig), h.Config.AppSecretKey)
		if err != nil {
			slog.Error("Failed to encrypt alert channel config", "id", id, "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to store alert channel config")
			return
		}
		encryptedConfig = &enc
	}

	if err := h.DB.UpdateAlertChannel(id, name, channelType, encryptedConfig, isActive); err != nil {
		slog.Error("Failed to update alert channel", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to update alert channel")
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

func (h *GovernanceHandler) DeleteAlertChannel(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	channel, err := h.DB.GetAlertChannelByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load alert channel")
		return
	}
	if channel == nil {
		writeError(w, http.StatusNotFound, "Alert channel not found")
		return
	}

	if err := h.DB.DeleteAlertChannel(id); err != nil {
		slog.Error("Failed to delete alert channel", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete alert channel")
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

func (h *GovernanceHandler) TestAlertChannel(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	channel, err := h.DB.GetAlertChannelByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load alert channel")
		return
	}
	if channel == nil {
		writeError(w, http.StatusNotFound, "Alert channel not found")
		return
	}

	var body struct {
		Recipients []string `json:"recipients"`
		Subject    string   `json:"subject"`
		Message    string   `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	recipients, err := validateRecipients(body.Recipients)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	decrypted, err := crypto.Decrypt(channel.ConfigEncrypted, h.Config.AppSecretKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to decrypt alert channel config")
		return
	}
	cfg := map[string]interface{}{}
	if err := json.Unmarshal([]byte(decrypted), &cfg); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to parse alert channel config")
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
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true, "provider_message_id": msgID})
}

func (h *GovernanceHandler) ListAlertRules(w http.ResponseWriter, r *http.Request) {
	rules, err := h.DB.ListAlertRules()
	if err != nil {
		slog.Error("Failed to list alert rules", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list alert rules")
		return
	}

	out := make([]alertRuleResponse, 0, len(rules))
	for _, rule := range rules {
		channels, err := h.DB.ListAlertRuleChannels(rule.ID)
		if err != nil {
			slog.Warn("Failed to load alert rule channels", "rule", rule.ID, "error", err)
			channels = []database.AlertRuleChannelView{}
		}
		out = append(out, alertRuleResponse{
			AlertRule: rule,
			Channels:  channels,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"rules": out})
}

func (h *GovernanceHandler) CreateAlertRule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	var body struct {
		Name            string                    `json:"name"`
		EventType       string                    `json:"event_type"`
		SeverityMin     string                    `json:"severity_min"`
		Enabled         *bool                     `json:"enabled"`
		CooldownSeconds *int                      `json:"cooldown_seconds"`
		MaxAttempts     *int                      `json:"max_attempts"`
		SubjectTemplate string                    `json:"subject_template"`
		BodyTemplate    string                    `json:"body_template"`
		Channels        []alertRuleChannelPayload `json:"channels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(body.Name)
	eventType := strings.ToLower(strings.TrimSpace(body.EventType))
	severityMin := strings.ToLower(strings.TrimSpace(body.SeverityMin))
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !isSupportedEventType(eventType) {
		writeError(w, http.StatusBadRequest, "event_type must be policy.violation, schedule.failed, schedule.slow, or *")
		return
	}
	if !isSupportedSeverity(severityMin) {
		writeError(w, http.StatusBadRequest, "severity_min must be info, warn, error, or critical")
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

	channels, err := h.validateRuleChannels(body.Channels)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	id, err := h.DB.CreateAlertRule(name, eventType, severityMin, enabled, cooldownSeconds, maxAttempts, body.SubjectTemplate, body.BodyTemplate, session.ClickhouseUser)
	if err != nil {
		slog.Error("Failed to create alert rule", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create alert rule")
		return
	}

	if err := h.DB.ReplaceAlertRuleChannels(id, channels); err != nil {
		_ = h.DB.DeleteAlertRule(id)
		slog.Error("Failed to create alert rule channels", "rule", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create alert rule channels")
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

func (h *GovernanceHandler) UpdateAlertRule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetAlertRuleByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load alert rule")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Alert rule not found")
		return
	}

	var body struct {
		Name            *string                    `json:"name"`
		EventType       *string                    `json:"event_type"`
		SeverityMin     *string                    `json:"severity_min"`
		Enabled         *bool                      `json:"enabled"`
		CooldownSeconds *int                       `json:"cooldown_seconds"`
		MaxAttempts     *int                       `json:"max_attempts"`
		SubjectTemplate *string                    `json:"subject_template"`
		BodyTemplate    *string                    `json:"body_template"`
		Channels        *[]alertRuleChannelPayload `json:"channels"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
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
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}
	if !isSupportedEventType(eventType) {
		writeError(w, http.StatusBadRequest, "event_type must be policy.violation, schedule.failed, schedule.slow, or *")
		return
	}
	if !isSupportedSeverity(severityMin) {
		writeError(w, http.StatusBadRequest, "severity_min must be info, warn, error, or critical")
		return
	}

	if err := h.DB.UpdateAlertRule(id, name, eventType, severityMin, enabled, cooldownSeconds, maxAttempts, subjectTemplate, bodyTemplate); err != nil {
		slog.Error("Failed to update alert rule", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to update alert rule")
		return
	}

	if body.Channels != nil {
		channels, err := h.validateRuleChannels(*body.Channels)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := h.DB.ReplaceAlertRuleChannels(id, channels); err != nil {
			slog.Error("Failed to replace alert rule channels", "rule", id, "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to update alert rule channels")
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

func (h *GovernanceHandler) DeleteAlertRule(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	id := chi.URLParam(r, "id")
	existing, err := h.DB.GetAlertRuleByID(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to load alert rule")
		return
	}
	if existing == nil {
		writeError(w, http.StatusNotFound, "Alert rule not found")
		return
	}
	if err := h.DB.DeleteAlertRule(id); err != nil {
		slog.Error("Failed to delete alert rule", "id", id, "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to delete alert rule")
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

func (h *GovernanceHandler) ListAlertEvents(w http.ResponseWriter, r *http.Request) {
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
		writeError(w, http.StatusInternalServerError, "Failed to list alert events")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"events": events})
}

func (h *GovernanceHandler) validateRuleChannels(payload []alertRuleChannelPayload) ([]database.AlertRuleChannel, error) {
	channels := make([]database.AlertRuleChannel, 0, len(payload))
	seen := make(map[string]bool, len(payload))
	for _, item := range payload {
		channelID := strings.TrimSpace(item.ChannelID)
		if channelID == "" {
			return nil, fmt.Errorf("channel_id is required")
		}
		if seen[channelID] {
			return nil, fmt.Errorf("channel %s is listed more than once", channelID)
		}
		seen[channelID] = true
		channel, err := h.DB.GetAlertChannelByID(channelID)
		if err != nil {
			return nil, fmt.Errorf("failed to load channel %s", channelID)
		}
		if channel == nil {
			return nil, fmt.Errorf("channel %s not found", channelID)
		}
		recipients, err := validateRecipients(item.Recipients)
		if err != nil {
			return nil, fmt.Errorf("channel %s: %w", channelID, err)
		}
		active := true
		if item.IsActive != nil {
			active = *item.IsActive
		}
		channels = append(channels, database.AlertRuleChannel{
			ChannelID:  channelID,
			Recipients: recipients,
			IsActive:   active,
		})
	}
	return channels, nil
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
	case "*", "any", alerts.EventTypePolicyViolation, alerts.EventTypeScheduleFailed, alerts.EventTypeScheduleSlow, alerts.EventTypeTelemetryMonitor:
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
