// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"bufio"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// emailFake stands in for one email provider and hands back the text body
// of the one message it receives.
type emailFake func(t *testing.T) (cfg map[string]interface{}, body func() string)

// smtpFake accepts one plain SMTP session and records the DATA section.
func smtpFake(t *testing.T) (map[string]interface{}, func() string) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	got := make(chan string, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(5 * time.Second))
		r := bufio.NewReader(conn)
		_, _ = fmt.Fprint(conn, "220 fake\r\n")
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			switch {
			case strings.HasPrefix(line, "DATA"):
				_, _ = fmt.Fprint(conn, "354 go\r\n")
				var b strings.Builder
				for {
					l, err := r.ReadString('\n')
					if err != nil || l == ".\r\n" {
						break
					}
					b.WriteString(l)
				}
				got <- b.String()
				_, _ = fmt.Fprint(conn, "250 ok\r\n")
			case strings.HasPrefix(line, "QUIT"):
				_, _ = fmt.Fprint(conn, "221 bye\r\n")
				return
			default:
				_, _ = fmt.Fprint(conn, "250 ok\r\n")
			}
		}
	}()
	host, port, _ := net.SplitHostPort(ln.Addr().String())
	cfg := map[string]interface{}{"host": host, "port": port, "from_email": "alerts@example.com"}
	return cfg, func() string { return receive(t, got) }
}

func receive(t *testing.T, got chan string) string {
	t.Helper()
	select {
	case s := <-got:
		return s
	case <-time.After(5 * time.Second):
		t.Fatal("no message reached the provider")
		return ""
	}
}

var emailFakes = map[string]emailFake{
	ChannelTypeSMTP: smtpFake,
}

// sendOneEmail sends one event through a rule with an optional body
// template and returns the text body the provider received.
func sendOneEmail(t *testing.T, channelType, appURL, eventType string, payload map[string]interface{}, bodyTemplate string) string {
	t.Helper()
	cfg, body := emailFakes[channelType](t)
	db := openTestDB(t)
	d := &Dispatcher{db: db, cfg: &config.Config{AppSecretKey: testSecret, AppURL: appURL}}
	channelID, err := db.CreateAlertChannel("mail", channelType, encryptConfig(t, cfg), true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("rule", "*", SeverityInfo, true, 0, 1, "", bodyTemplate, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{{ChannelID: channelID, Recipients: []string{"ops@example.com"}, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAlertEvent(nil, eventType, SeverityError, "Something happened", "details", payload, "", ""); err != nil {
		t.Fatal(err)
	}
	d.materializeEventJobs()
	d.processDueJobs()
	if status, _ := jobStatus(t, db); status != "sent" {
		var lastErr *string
		_ = db.Conn().QueryRow(`SELECT last_error FROM alert_dispatch_jobs`).Scan(&lastErr)
		t.Fatalf("job %s, last_error %v", status, lastErr)
	}
	return body()
}

// Every email channel carries a link to the page of the event: the payload
// link_path when set (access drift), else the alert events page.
func TestEmailBodyLinksToEvent(t *testing.T) {
	cases := []struct {
		eventType string
		payload   map[string]interface{}
		want      string
	}{
		{EventTypeScheduleFailed, map[string]interface{}{"schedule": "nightly"}, "Open in CH-UI: https://chui.example.com/governance?section=alerts\n"},
		{EventTypeAccessDrift, map[string]interface{}{"link_path": "/governance?section=access-history"}, "Open in CH-UI: https://chui.example.com/governance?section=access-history\n"},
		{EventTypeSQLAlert, map[string]interface{}{"link_path": "//evil.example/x"}, "Open in CH-UI: https://chui.example.com/governance?section=alerts\n"},
		{EventTypeIngestStalled, map[string]interface{}{"link_path": PipelineLinkPath("p1")}, "Open in CH-UI: https://chui.example.com/pipelines/p1\n"},
	}
	for channelType := range emailFakes {
		for _, c := range cases {
			t.Run(channelType+"/"+c.eventType, func(t *testing.T) {
				body := strings.ReplaceAll(sendOneEmail(t, channelType, "https://chui.example.com/", c.eventType, c.payload, ""), "\r\n", "\n")
				if !strings.Contains(body, c.want) {
					t.Fatalf("body lacks %q:\n%s", c.want, body)
				}
			})
		}
	}
}

// Without an app URL the link line is left out, not printed relative.
func TestEmailBodyWithoutAppURLHasNoLink(t *testing.T) {
	for channelType := range emailFakes {
		t.Run(channelType, func(t *testing.T) {
			body := sendOneEmail(t, channelType, "", EventTypeAccessDrift, map[string]interface{}{"link_path": "/governance?section=access-history"}, "")
			if strings.Contains(body, "Open in CH-UI") || !strings.Contains(body, "Type: "+EventTypeAccessDrift) {
				t.Fatalf("unexpected body:\n%s", body)
			}
		})
	}
}

func TestEmailTemplateLinkVariable(t *testing.T) {
	for channelType := range emailFakes {
		t.Run(channelType, func(t *testing.T) {
			body := sendOneEmail(t, channelType, "https://chui.example.com", EventTypeAccessDrift,
				map[string]interface{}{"link_path": "/governance?section=access-history"}, "See {{link}} for {{title}}")
			if want := "See https://chui.example.com/governance?section=access-history for Something happened"; !strings.Contains(body, want) {
				t.Fatalf("body lacks %q:\n%s", want, body)
			}
		})
	}
	job := database.AlertDispatchJobWithDetails{EventTitle: "t"}
	if got := renderTemplate("[{{link}}]", job, ""); got != "[]" {
		t.Fatalf("empty link renders as %q", got)
	}
}
