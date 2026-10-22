// Package mail sends one-off notifications over SMTP. It is core
// (Apache-2.0): free features such as dashboard share invites send mail, and
// Pro alerting builds on it.
package mail

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/smtp"
	"strconv"
	"strings"
	"time"
)

// ChannelTypeSMTP is the email channel type, as stored on alert channels.
const ChannelTypeSMTP = "smtp"

// RemovedChannelType reports whether channelType names an email API provider
// CH-UI used to support. Such channels stay in the database, deactivated,
// until an admin deletes them or recreates them as SMTP or webhook.
func RemovedChannelType(channelType string) bool {
	switch strings.ToLower(strings.TrimSpace(channelType)) {
	case "resend", "brevo":
		return true
	}
	return false
}

// Sender sends mail. The zero value is ready to use.
type Sender struct{}

// Send sends one message through the provider named by channelType and
// returns the provider message id when it reports one.
func Send(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	return (&Sender{}).Send(ctx, channelType, channelConfig, recipients, subject, body)
}

// SendMessage sends one message with the default Sender.
func SendMessage(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, msg Message) (string, error) {
	return (&Sender{}).SendMessage(ctx, channelType, channelConfig, recipients, msg)
}

// Send sends one plain-text message through the provider named by
// channelType. It is SendMessage with a text body only.
func (s *Sender) Send(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, subject, body string) (string, error) {
	return s.SendMessage(ctx, channelType, channelConfig, recipients, Message{Subject: subject, Text: body})
}

// SendMessage sends one message, with an optional HTML alternative and
// attachments, through the provider named by channelType and returns the
// provider message id when it reports one.
func (s *Sender) SendMessage(ctx context.Context, channelType string, channelConfig map[string]interface{}, recipients []string, msg Message) (string, error) {
	switch strings.ToLower(channelType) {
	case ChannelTypeSMTP:
		return s.sendSMTPMessage(ctx, channelConfig, recipients, msg)
	default:
		if RemovedChannelType(channelType) {
			return "", fmt.Errorf("%s channels are no longer supported: recreate this channel as SMTP or webhook", strings.ToLower(strings.TrimSpace(channelType)))
		}
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
	return s.sendSMTPMessage(ctx, cfg, recipients, Message{Subject: subject, Text: body})
}

func (s *Sender) sendSMTPMessage(ctx context.Context, cfg map[string]interface{}, recipients []string, m Message) (string, error) {
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

	msg, err := BuildMIME(fromHeader, recipients, m)
	if err != nil {
		return "", err
	}

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
