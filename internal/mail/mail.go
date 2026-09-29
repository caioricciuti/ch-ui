// Package mail sends one-off notifications through the providers CH-UI
// supports: SMTP, Resend and Brevo. It is core (Apache-2.0): free features such
// as dashboard share invites send mail, and Pro alerting builds on it.
package mail

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// Provider channel types, as stored on alert channels.
const (
	ChannelTypeSMTP   = "smtp"
	ChannelTypeResend = "resend"
	ChannelTypeBrevo  = "brevo"
)

// Sender sends mail. The zero value is ready to use.
type Sender struct {
	// HTTP is used for the Resend and Brevo APIs; nil means a client with a
	// 15 second timeout.
	HTTP *http.Client
}

func (s *Sender) client() *http.Client {
	if s != nil && s.HTTP != nil {
		return s.HTTP
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Send sends one message through the provider named by channelType and
// returns the provider message id when it reports one.
func Send(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	return (&Sender{}).Send(ctx, channelType, channelConfig, recipients, subject, body)
}

// Send sends one message through the provider named by channelType.
func (s *Sender) Send(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	switch strings.ToLower(channelType) {
	case ChannelTypeSMTP:
		return s.sendSMTP(ctx, channelConfig, recipients, subject, body)
	case ChannelTypeResend:
		return s.sendResend(ctx, channelConfig, recipients, subject, body)
	case ChannelTypeBrevo:
		return s.sendBrevo(ctx, channelConfig, recipients, subject, body)
	default:
		return "", fmt.Errorf("unsupported channel type: %s", channelType)
	}
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

func (s *Sender) sendSMTP(ctx context.Context, cfg map[string]interface{}, recipients []string, subject, body string) (string, error) {
	// net/smtp does not honor contexts. Bound both dialing and every protocol
	// operation, and close the socket promptly when the caller shuts down.
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	host := stringCfg(cfg, "host")
	fromEmail := stringCfg(cfg, "from_email")
	username := stringCfg(cfg, "username")
	password := stringCfg(cfg, "password")
	fromName := stringCfg(cfg, "from_name")
	if host == "" || fromEmail == "" {
		return "", fmt.Errorf("smtp config requires host and from_email")
	}

	port := intCfg(cfg, "port", 587)
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	useTLS := boolCfg(cfg, "use_tls", false)
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

	conn, err := (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, "tcp", addr)
	if err != nil {
		return "", fmt.Errorf("smtp dial: %w", err)
	}
	defer conn.Close()
	deadline, _ := ctx.Deadline() // WithTimeout above guarantees a deadline.
	if err := conn.SetDeadline(deadline); err != nil {
		return "", fmt.Errorf("smtp deadline: %w", err)
	}
	stopCancellation := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancellation()
	tlsConfig := &tls.Config{ServerName: host, InsecureSkipVerify: insecureSkipVerify}
	var transport net.Conn = conn
	if useTLS {
		tlsConn := tls.Client(conn, tlsConfig)
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return "", fmt.Errorf("smtp tls handshake: %w", err)
		}
		transport = tlsConn
	}
	client, err := smtp.NewClient(transport, host)
	if err != nil {
		return "", fmt.Errorf("smtp new client: %w", err)
	}
	defer client.Close()
	// Both legacy TCP paths upgraded when offered (the plain path used
	// smtp.SendMail, which performs opportunistic STARTTLS internally).
	if !useTLS {
		if ok, _ := client.Extension("STARTTLS"); ok {
			if err := client.StartTLS(tlsConfig); err != nil {
				return "", fmt.Errorf("smtp starttls: %w", err)
			}
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

func (s *Sender) sendResend(ctx context.Context, cfg map[string]interface{}, recipients []string, subject, body string) (string, error) {
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

	resp, err := s.client().Do(req)
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

func (s *Sender) sendBrevo(ctx context.Context, cfg map[string]interface{}, recipients []string, subject, body string) (string, error) {
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

	resp, err := s.client().Do(req)
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
