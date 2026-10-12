// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package alerts

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
)

const testSecret = "test-secret"

func encryptConfig(t *testing.T, cfg map[string]interface{}) string {
	t.Helper()
	raw, _ := json.Marshal(cfg)
	enc, err := crypto.Encrypt(string(raw), testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

// seedJob creates a channel, a rule bound to it and one event, and
// materializes the dispatch job.
func seedJob(t *testing.T, db *database.DB, d *Dispatcher, channelType string, cfg map[string]interface{}, recipients []string, maxAttempts int) {
	t.Helper()
	channelID, err := db.CreateAlertChannel("ch-"+channelType, channelType, encryptConfig(t, cfg), true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("rule", "*", SeverityInfo, true, 300, maxAttempts, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{{ChannelID: channelID, Recipients: recipients, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAlertEvent(nil, EventTypeScheduleFailed, SeverityError, "Schedule failed", "nightly rollup failed", map[string]string{"schedule": "nightly"}, "fp-1", ""); err != nil {
		t.Fatal(err)
	}
	d.materializeEventJobs()
}

func expireLeases(t *testing.T, db *database.DB) {
	t.Helper()
	past := time.Now().UTC().Add(-time.Minute).Format(time.RFC3339)
	if _, err := db.Conn().Exec(`UPDATE alert_dispatch_jobs SET next_attempt_at = ? WHERE status = 'sending'`, past); err != nil {
		t.Fatal(err)
	}
}

func jobStatus(t *testing.T, db *database.DB) (string, int) {
	t.Helper()
	var status string
	var attempts int
	if err := db.Conn().QueryRow(`SELECT status, attempt_count FROM alert_dispatch_jobs`).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	return status, attempts
}

// A job left in 'sending' by a crash mid-delivery becomes due again once its
// lease expires, and at its final attempt it is failed so it stops
// suppressing its fingerprint.
func TestStuckSendingJobIsRetriedAfterLease(t *testing.T) {
	db := openTestDB(t)
	d := &Dispatcher{db: db, cfg: &config.Config{AppSecretKey: testSecret}}
	seedJob(t, db, d, ChannelTypeSMTP, map[string]interface{}{"host": "smtp.invalid", "from_email": "a@b.co"}, []string{"ops@example.com"}, 2)

	jobs, err := db.ListDueAlertDispatchJobs(10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("due jobs = %d, %v", len(jobs), err)
	}
	// Simulate a crash right after the claim: status stays 'sending'.
	if ok, err := db.ClaimAlertDispatchJob(jobs[0].ID, time.Now().UTC()); err != nil || !ok {
		t.Fatalf("claim: %v %v", ok, err)
	}
	if again, _ := db.ClaimAlertDispatchJob(jobs[0].ID, time.Now().UTC()); again {
		t.Fatal("a leased job must not be claimed twice")
	}
	if jobs, _ := db.ListDueAlertDispatchJobs(10); len(jobs) != 0 {
		t.Fatalf("job inside its lease must not be due, got %d", len(jobs))
	}

	expireLeases(t, db)
	jobs, err = db.ListDueAlertDispatchJobs(10)
	if err != nil || len(jobs) != 1 {
		t.Fatalf("expired sending job must be due again, got %d, %v", len(jobs), err)
	}
	if ok, err := db.ClaimAlertDispatchJob(jobs[0].ID, time.Now().UTC()); err != nil || !ok {
		t.Fatalf("reclaim: %v %v", ok, err)
	}
	if status, attempts := jobStatus(t, db); status != "sending" || attempts != 2 {
		t.Fatalf("after reclaim: %s/%d", status, attempts)
	}

	// Crash again at the final attempt: the expired job is failed and no
	// longer suppresses the fingerprint.
	expireLeases(t, db)
	if jobs, _ := db.ListDueAlertDispatchJobs(10); len(jobs) != 0 {
		t.Fatalf("job at max attempts must not be due, got %d", len(jobs))
	}
	if status, _ := jobStatus(t, db); status != "failed" {
		t.Fatalf("interrupted final attempt must be failed, got %s", status)
	}
	var ruleID, channelID string
	if err := db.Conn().QueryRow(`SELECT rule_id, channel_id FROM alert_dispatch_jobs`).Scan(&ruleID, &channelID); err != nil {
		t.Fatal(err)
	}
	recent, err := db.HasRecentAlertDispatch(ruleID, channelID, "fp-1", time.Now().Add(-time.Hour))
	if err != nil || recent {
		t.Fatalf("failed job must not suppress the fingerprint: %v %v", recent, err)
	}
}

// If the rule's channels cannot be listed, the event stays 'new' and is
// materialized on a later tick, exactly once.
func TestEventStaysUnprocessedWhenChannelListingFails(t *testing.T) {
	db := openTestDB(t)
	d := &Dispatcher{db: db}
	channelID, err := db.CreateAlertChannel("ops", ChannelTypeSMTP, "enc", true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("rule", "*", SeverityInfo, true, 0, 3, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.ReplaceAlertRuleChannels(ruleID, []database.AlertRuleChannel{{ChannelID: channelID, Recipients: []string{"ops@example.com"}, IsActive: true}}); err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateAlertEvent(nil, EventTypeScheduleFailed, SeverityError, "t", "m", nil, "", ""); err != nil {
		t.Fatal(err)
	}

	if _, err := db.Conn().Exec(`ALTER TABLE alert_rule_channels RENAME TO alert_rule_channels_off`); err != nil {
		t.Fatal(err)
	}
	d.materializeEventJobs()
	if pending, _ := db.ListNewAlertEvents(10); len(pending) != 1 {
		t.Fatalf("event must stay new after a channel listing error, %d new", len(pending))
	}

	if _, err := db.Conn().Exec(`ALTER TABLE alert_rule_channels_off RENAME TO alert_rule_channels`); err != nil {
		t.Fatal(err)
	}
	d.materializeEventJobs()
	d.materializeEventJobs()
	if pending, _ := db.ListNewAlertEvents(10); len(pending) != 0 {
		t.Fatalf("event must be processed after recovery, %d new", len(pending))
	}
	var n int
	if err := db.Conn().QueryRow(`SELECT COUNT(*) FROM alert_dispatch_jobs`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("expected exactly one job, got %d, %v", n, err)
	}
}

type captured struct {
	mu      sync.Mutex
	body    []byte
	headers http.Header
}

func webhookServer(t *testing.T) (*httptest.Server, *captured) {
	t.Helper()
	c := &captured{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		c.mu.Lock()
		c.body, c.headers = b, r.Header.Clone()
		c.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	return srv, c
}

// A webhook channel with no email recipients is dispatched, signed, carries
// the secret header and the generic JSON payload.
func TestWebhookChannelDispatch(t *testing.T) {
	srv, got := webhookServer(t)
	db := openTestDB(t)
	d := &Dispatcher{db: db, cfg: &config.Config{AppSecretKey: testSecret, AppURL: "https://chui.example.com/"}}
	seedJob(t, db, d, ChannelTypeWebhook, map[string]interface{}{
		WebhookCfgURL:               srv.URL + "/hook",
		WebhookCfgAllowHTTP:         true,
		WebhookCfgAllowPrivate:      true,
		WebhookCfgSecretHeaderName:  "X-Api-Key",
		WebhookCfgSecretHeaderValue: "k3y",
		WebhookCfgSigningSecret:     "s1gn",
	}, nil, 3)

	d.processDueJobs()

	if status, attempts := jobStatus(t, db); status != "sent" || attempts != 1 {
		var lastErr *string
		_ = db.Conn().QueryRow(`SELECT last_error FROM alert_dispatch_jobs`).Scan(&lastErr)
		t.Fatalf("job %s/%d, last_error %v", status, attempts, lastErr)
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	if got.headers.Get("X-Api-Key") != "k3y" {
		t.Fatalf("secret header missing: %v", got.headers)
	}
	ts := got.headers.Get(WebhookTimestampHeader)
	if want := "sha256=" + SignWebhook("s1gn", ts, got.body); got.headers.Get(WebhookSignatureHeader) != want {
		t.Fatalf("bad signature %q, want %q", got.headers.Get(WebhookSignatureHeader), want)
	}
	var ev WebhookEvent
	if err := json.Unmarshal(got.body, &ev); err != nil {
		t.Fatal(err)
	}
	if ev.EventType != EventTypeScheduleFailed || ev.Severity != SeverityError || ev.Title != "Schedule failed" ||
		ev.Message != "nightly rollup failed" || ev.Fingerprint != "fp-1" || ev.Timestamp == "" ||
		ev.Link != "https://chui.example.com/governance?section=alerts" || !strings.Contains(string(ev.Payload), "nightly") {
		t.Fatalf("unexpected payload: %s", got.body)
	}
}

func TestWebhookFormats(t *testing.T) {
	ev := WebhookEvent{EventType: "x", Severity: "warn", Title: "T", Message: "M", Link: "https://l"}
	slack, _ := webhookBody(WebhookFormatSlack, ev)
	var s map[string]string
	if err := json.Unmarshal(slack, &s); err != nil || !strings.Contains(s["text"], "*[WARN] T*") || !strings.Contains(s["text"], "<https://l|Open in CH-UI>") {
		t.Fatalf("slack body: %s", slack)
	}
	teams, _ := webhookBody(WebhookFormatTeams, ev)
	if !strings.Contains(string(teams), `"application/vnd.microsoft.card.adaptive"`) || !strings.Contains(string(teams), `"Action.OpenUrl"`) {
		t.Fatalf("teams body: %s", teams)
	}
}

func TestValidateWebhookConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  map[string]interface{}
		ok   bool
	}{
		{"https public", map[string]interface{}{"url": "https://hooks.slack.com/services/x"}, true},
		{"missing url", map[string]interface{}{}, false},
		{"http refused", map[string]interface{}{"url": "http://example.com/h"}, false},
		{"http allowed", map[string]interface{}{"url": "http://example.com/h", "allow_http": true}, true},
		{"loopback refused", map[string]interface{}{"url": "https://127.0.0.1/h"}, false},
		{"localhost refused", map[string]interface{}{"url": "https://localhost/h"}, false},
		{"private refused", map[string]interface{}{"url": "https://10.0.0.5/h"}, false},
		{"link-local refused", map[string]interface{}{"url": "https://169.254.169.254/latest"}, false},
		{"private allowed", map[string]interface{}{"url": "https://10.0.0.5/h", "allow_private_network": true}, true},
		{"credentials refused", map[string]interface{}{"url": "https://u:p@example.com/h"}, false},
		{"ftp refused", map[string]interface{}{"url": "ftp://example.com/h"}, false},
		{"bad format", map[string]interface{}{"url": "https://example.com", "format": "xml"}, false},
		{"bad header", map[string]interface{}{"url": "https://example.com", "secret_header_name": "X Bad"}, false},
		{"reserved header", map[string]interface{}{"url": "https://example.com", "secret_header_name": "Host"}, false},
	}
	for _, tc := range cases {
		if err := ValidateWebhookConfig(tc.cfg); (err == nil) != tc.ok {
			t.Errorf("%s: err=%v, want ok=%v", tc.name, err, tc.ok)
		}
	}
}

// The dial guard refuses resolved private addresses, which covers hostnames
// that resolve to internal services.
func TestWebhookDialGuard(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:443", "10.1.2.3:443", "[::1]:443", "169.254.169.254:80"} {
		if err := webhookDialControl("tcp", addr, nil); err == nil {
			t.Errorf("%s must be refused", addr)
		}
	}
	if err := webhookDialControl("tcp", net.JoinHostPort("93.184.216.34", "443"), nil); err != nil {
		t.Errorf("public address refused: %v", err)
	}
	// End to end: an http loopback URL with allow_http but without
	// allow_private_network is refused before any request is sent.
	srv, got := webhookServer(t)
	_, err := SendWebhook(t.Context(), nil, map[string]interface{}{"url": srv.URL, "allow_http": true}, WebhookEvent{Title: "t"})
	if err == nil {
		t.Fatal("loopback webhook without allow_private_network must fail")
	}
	got.mu.Lock()
	defer got.mu.Unlock()
	if got.body != nil {
		t.Fatal("request reached the private server")
	}
}
