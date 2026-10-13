// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ChannelTypeWebhook POSTs a JSON payload to a URL. The "format" config
// option shapes the body for Slack or Microsoft Teams incoming webhooks.
const ChannelTypeWebhook = "webhook"

// Webhook body formats.
const (
	WebhookFormatGeneric = "generic"
	WebhookFormatSlack   = "slack"
	WebhookFormatTeams   = "teams"
)

// Webhook headers. The signature is hex HMAC-SHA256 over
// "<timestamp>.<body>" with the channel's signing secret.
const (
	WebhookTimestampHeader = "X-CHUI-Timestamp"
	WebhookSignatureHeader = "X-CHUI-Signature"
)

// Webhook config keys. url, secret_header_value and signing_secret live in the
// encrypted channel config; the two secrets are redacted on read.
const (
	WebhookCfgURL               = "url"
	WebhookCfgFormat            = "format"
	WebhookCfgSecretHeaderName  = "secret_header_name"
	WebhookCfgSecretHeaderValue = "secret_header_value"
	WebhookCfgSigningSecret     = "signing_secret"
	WebhookCfgAllowHTTP         = "allow_http"
	WebhookCfgAllowPrivate      = "allow_private_network"
)

// WebhookSecretKeys are the config keys that hold secrets.
var WebhookSecretKeys = []string{WebhookCfgSecretHeaderValue, WebhookCfgSigningSecret}

// WebhookEvent is the generic JSON body, and the input for Slack and Teams.
type WebhookEvent struct {
	EventID     string          `json:"event_id,omitempty"`
	EventType   string          `json:"event_type"`
	Severity    string          `json:"severity"`
	Title       string          `json:"title"`
	Message     string          `json:"message"`
	Fingerprint string          `json:"fingerprint,omitempty"`
	Timestamp   string          `json:"timestamp"`
	Link        string          `json:"link,omitempty"`
	RuleName    string          `json:"rule_name,omitempty"`
	Payload     json.RawMessage `json:"payload,omitempty"`
}

var errWebhookPrivateAddress = errors.New("webhook URL resolves to a private or loopback address; enable \"Allow private network\" on the channel to permit it")

// ValidateWebhookConfig checks the URL, format and header options. The URL
// must be https unless allow_http is set, and must not be a literal private
// or loopback address unless allow_private_network is set. Hostnames are
// checked again at dial time, after DNS resolution.
func ValidateWebhookConfig(cfg map[string]interface{}) error {
	raw := cfgString(cfg, WebhookCfgURL)
	if raw == "" {
		return fmt.Errorf("webhook config requires url")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.Hostname() == "" {
		return fmt.Errorf("webhook url is not a valid absolute URL")
	}
	switch u.Scheme {
	case "https":
	case "http":
		if !cfgBool(cfg, WebhookCfgAllowHTTP) {
			return fmt.Errorf("webhook url must use https (enable \"Allow plain HTTP\" to override)")
		}
	default:
		return fmt.Errorf("webhook url must use https")
	}
	if u.User != nil {
		return fmt.Errorf("webhook url must not contain credentials; use the secret header instead")
	}
	if !cfgBool(cfg, WebhookCfgAllowPrivate) {
		host := u.Hostname()
		if ip := net.ParseIP(host); ip != nil && isNonPublicIP(ip) {
			return errWebhookPrivateAddress
		}
		if strings.EqualFold(host, "localhost") || strings.HasSuffix(strings.ToLower(host), ".localhost") {
			return errWebhookPrivateAddress
		}
	}
	switch cfgString(cfg, WebhookCfgFormat) {
	case "", WebhookFormatGeneric, WebhookFormatSlack, WebhookFormatTeams:
	default:
		return fmt.Errorf("webhook format must be generic, slack, or teams")
	}
	if name := cfgString(cfg, WebhookCfgSecretHeaderName); name != "" {
		if !validHeaderName(name) {
			return fmt.Errorf("webhook secret header name is not a valid HTTP header name")
		}
		switch strings.ToLower(name) {
		case "host", "content-type", "content-length", strings.ToLower(WebhookSignatureHeader), strings.ToLower(WebhookTimestampHeader):
			return fmt.Errorf("webhook secret header cannot be %s", name)
		}
	}
	return nil
}

// SendWebhook POSTs ev to the channel URL in the configured format. A nil
// client gets the guarded default: no proxy, no redirects, every dial checked
// against private addresses unless the channel allows them, 15 s.
func SendWebhook(ctx context.Context, client *http.Client, cfg map[string]interface{}, ev WebhookEvent) (string, error) {
	if err := ValidateWebhookConfig(cfg); err != nil {
		return "", err
	}
	body, err := webhookBody(cfgString(cfg, WebhookCfgFormat), ev)
	if err != nil {
		return "", err
	}
	if client == nil {
		client = webhookClient(cfgBool(cfg, WebhookCfgAllowPrivate))
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfgString(cfg, WebhookCfgURL), bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("build webhook request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "CH-UI-Alerts/1")
	if name := cfgString(cfg, WebhookCfgSecretHeaderName); name != "" {
		req.Header.Set(name, cfgString(cfg, WebhookCfgSecretHeaderValue))
	}
	if secret := cfgString(cfg, WebhookCfgSigningSecret); secret != "" {
		ts := strconv.FormatInt(time.Now().Unix(), 10)
		req.Header.Set(WebhookTimestampHeader, ts)
		req.Header.Set(WebhookSignatureHeader, "sha256="+SignWebhook(secret, ts, body))
	}
	resp, err := client.Do(req)
	if err != nil {
		if errors.Is(err, errWebhookPrivateAddress) {
			return "", errWebhookPrivateAddress
		}
		return "", fmt.Errorf("webhook request failed: %w", err)
	}
	defer resp.Body.Close()
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return "", fmt.Errorf("webhook returned HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(snippet)))
	}
	return "", nil
}

// SignWebhook returns hex HMAC-SHA256 of "<timestamp>.<body>".
func SignWebhook(secret, timestamp string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(timestamp))
	mac.Write([]byte("."))
	mac.Write(body)
	return hex.EncodeToString(mac.Sum(nil))
}

func webhookBody(format string, ev WebhookEvent) ([]byte, error) {
	heading := fmt.Sprintf("[%s] %s", strings.ToUpper(ev.Severity), ev.Title)
	switch format {
	case WebhookFormatSlack:
		text := "*" + heading + "*\n" + ev.Message
		if ev.Link != "" {
			text += "\n<" + ev.Link + "|Open in CH-UI>"
		}
		return json.Marshal(map[string]string{"text": text})
	case WebhookFormatTeams:
		card := map[string]interface{}{
			"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
			"type":    "AdaptiveCard",
			"version": "1.4",
			"body": []map[string]interface{}{
				{"type": "TextBlock", "text": heading, "weight": "Bolder", "size": "Medium", "wrap": true},
				{"type": "TextBlock", "text": ev.Message, "wrap": true},
				{"type": "FactSet", "facts": []map[string]string{
					{"title": "Event", "value": ev.EventType},
					{"title": "Time", "value": ev.Timestamp},
				}},
			},
		}
		if ev.Link != "" {
			card["actions"] = []map[string]string{{"type": "Action.OpenUrl", "title": "Open in CH-UI", "url": ev.Link}}
		}
		return json.Marshal(map[string]interface{}{
			"type": "message",
			"attachments": []map[string]interface{}{
				{"contentType": "application/vnd.microsoft.card.adaptive", "content": card},
			},
		})
	default:
		return json.Marshal(ev)
	}
}

// isNonPublicIP mirrors the OAuth metadata fetcher's guard.
func isNonPublicIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified()
}

// webhookDialControl runs on the resolved address, which closes the DNS
// rebinding gap between validation and dial.
func webhookDialControl(_, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("webhook dial: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || isNonPublicIP(ip) {
		return errWebhookPrivateAddress
	}
	return nil
}

func webhookClient(allowPrivate bool) *http.Client {
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		dialer.Control = webhookDialControl
	}
	return &http.Client{
		Timeout: 15 * time.Second,
		Transport: &http.Transport{
			Proxy:                 nil,
			DialContext:           dialer.DialContext,
			TLSHandshakeTimeout:   10 * time.Second,
			ResponseHeaderTimeout: 10 * time.Second,
			MaxIdleConns:          4,
			IdleConnTimeout:       30 * time.Second,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("webhook redirects are not followed")
		},
	}
}

func validHeaderName(name string) bool {
	if name == "" {
		return false
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || strings.ContainsRune("!#$%&'*+-.^_`|~", r)
		if !ok {
			return false
		}
	}
	return true
}

func cfgString(cfg map[string]interface{}, key string) string {
	v, ok := cfg[key]
	if !ok || v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return strings.TrimSpace(s)
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

func cfgBool(cfg map[string]interface{}, key string) bool {
	switch v := cfg[key].(type) {
	case bool:
		return v
	case string:
		s := strings.ToLower(strings.TrimSpace(v))
		return s == "true" || s == "1" || s == "yes"
	case float64:
		return v != 0
	default:
		return false
	}
}
