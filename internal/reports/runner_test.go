// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package reports

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/dashboards"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/mail"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const testSecret = "test-secret"

type fakeExec struct {
	mu       sync.Mutex
	calls    []string
	settings []map[string]string
	users    []string
	result   *tunnel.QueryResult
	err      error
}

func (f *fakeExec) ExecuteQueryWithSettingsCtx(_ context.Context, _, sql, user, _ string, settings map[string]string, _ time.Duration) (*tunnel.QueryResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, sql)
	f.settings = append(f.settings, settings)
	f.users = append(f.users, user)
	return f.result, f.err
}

func (f *fakeExec) IsTunnelOnline(string) bool { return true }

func (f *fakeExec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

type sent struct {
	channelType string
	recipients  []string
	msg         mail.Message
	hook        alerts.ReportWebhook
}

type fakeSender struct {
	mu       sync.Mutex
	failures int
	sent     []sent
	attempts int
}

func (f *fakeSender) SendEmail(_ context.Context, channelType string, _ map[string]interface{}, recipients []string, msg mail.Message) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	if f.failures > 0 {
		f.failures--
		return "", errors.New("smtp 451 try later")
	}
	f.sent = append(f.sent, sent{channelType: channelType, recipients: recipients, msg: msg})
	return "msg-1", nil
}

func (f *fakeSender) SendWebhook(_ context.Context, _ map[string]interface{}, rep alerts.ReportWebhook) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.attempts++
	f.sent = append(f.sent, sent{channelType: "webhook", hook: rep})
	return "", nil
}

type denyGuard struct{}

func (denyGuard) EvaluateQuery(_, _, _, endpoint string) (governance.GuardrailDecision, error) {
	if endpoint != GuardrailEndpoint {
		return governance.GuardrailDecision{}, errors.New("wrong endpoint " + endpoint)
	}
	return governance.GuardrailDecision{Allowed: false, Block: &governance.GuardrailBlock{PolicyName: "no-pii"}}, nil
}

type fixture struct {
	db     *database.DB
	conn   string
	exec   *fakeExec
	send   *fakeSender
	runner *Runner
	sub    *database.ReportSubscription
	now    time.Time
	access config.ProAccess
}

func newFixture(t *testing.T, mode string) *fixture {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	f := &fixture{db: db, conn: conn, now: time.Date(2026, 10, 13, 8, 0, 0, 0, time.UTC), access: config.ProActive}
	if mode == "service_account" {
		enc, err := crypto.Encrypt("pw", testSecret)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: Worker, Mode: mode, Username: "reporter", EncryptedPassword: enc}); err != nil {
			t.Fatal(err)
		}
	}
	q, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT n FROM t WHERE x = {x:UInt8}", Parameters: `{"x":"1"}`, ConnectionID: conn})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ := json.Marshal(map[string]string{"host": "smtp.example.com", "from_email": "r@example.com"})
	encCfg, err := crypto.Encrypt(string(cfg), testSecret)
	if err != nil {
		t.Fatal(err)
	}
	channel, err := db.CreateAlertChannel("mail", "smtp", encCfg, true, "admin")
	if err != nil {
		t.Fatal(err)
	}
	s := database.ReportSubscription{
		Name: "Daily", SourceType: SourceSavedQuery, SavedQueryID: &q, ConnectionID: conn, Cron: "0 8 * * *", Timezone: "UTC",
		Enabled: true, Format: FormatBoth, Targets: []database.ReportTarget{{ChannelID: channel, Recipients: []string{"ops@example.com"}}},
	}
	if err := Normalize(&s); err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateReportSubscription(s)
	if err != nil {
		t.Fatal(err)
	}
	f.sub, _ = db.GetReportSubscription(id)
	f.exec = &fakeExec{result: &tunnel.QueryResult{
		Meta: json.RawMessage(`[{"name":"n","type":"UInt64"}]`),
		Data: json.RawMessage(`[{"n":"1"},{"n":"2"}]`),
	}}
	f.send = &fakeSender{}
	gate := config.NewProGate("Scheduled report delivery", func() config.ProAccess { return f.access })
	f.runner = NewRunner(db, f.exec, nil, testSecret, "https://chui.example.com", gate.Allow)
	f.runner.Send = f.send
	f.runner.Now = func() time.Time { return f.now }
	return f
}

func (f *fixture) makeDue(t *testing.T) {
	t.Helper()
	past := f.now.Add(-time.Minute).Format(time.RFC3339)
	if _, err := f.db.Conn().Exec(`UPDATE report_subscriptions SET next_run_at = ? WHERE id = ?`, past, f.sub.ID); err != nil {
		t.Fatal(err)
	}
}

func (f *fixture) deliveries(t *testing.T) []database.ReportDelivery {
	t.Helper()
	list, err := f.db.ListReportDeliveries(f.sub.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	return list
}

func TestTickFollowsLicenseAndSkipsMissedSlots(t *testing.T) {
	f := newFixture(t, "service_account")
	f.access = config.ProNone
	f.makeDue(t)
	f.runner.Tick()
	f.runner.Wait()
	if n := f.exec.count(); n != 0 {
		t.Fatalf("ProNone ran %d queries", n)
	}
	// Resume: the slot missed while paused moves to the future, no run.
	f.access = config.ProGrace
	f.runner.Tick()
	f.runner.Wait()
	if n := f.exec.count(); n != 0 {
		t.Fatalf("resume ran the missed slot")
	}
	s, _ := f.db.GetReportSubscription(f.sub.ID)
	if s.NextRunAt == nil {
		t.Fatal("no next run after resume")
	}
	if next, _ := time.Parse(time.RFC3339, *s.NextRunAt); !next.After(f.now) {
		t.Fatalf("next run not moved forward: %s", *s.NextRunAt)
	}
	if !s.Enabled || s.LastRunAt != nil {
		t.Fatalf("a skipped slot is not a run: %+v", s)
	}
	// Running again: the due slot runs and sends.
	f.makeDue(t)
	f.runner.Tick()
	f.runner.Wait()
	if f.exec.count() != 1 || len(f.send.sent) != 1 {
		t.Fatalf("active run: %d queries, %d sends", f.exec.count(), len(f.send.sent))
	}
	settings := f.exec.settings[0]
	for k, v := range map[string]string{"readonly": "1", "result_overflow_mode": "break", "log_comment": LogComment, "param_x": "1", "max_result_rows": "1001", "max_execution_time": "60"} {
		if settings[k] != v {
			t.Errorf("setting %s = %q, want %q", k, settings[k], v)
		}
	}
	if !strings.Contains(f.exec.calls[0], "LIMIT 1001") || f.exec.users[0] != "reporter" {
		t.Fatalf("query %q as %q", f.exec.calls[0], f.exec.users[0])
	}
	msg := f.send.sent[0].msg
	if len(msg.Attachments) != 1 || !strings.Contains(msg.Text, "returned 2 rows") {
		t.Fatalf("message %+v", msg)
	}
}

func TestSessionModeRefused(t *testing.T) {
	f := newFixture(t, "session")
	_, err := f.runner.SendNow(context.Background(), f.sub)
	if err == nil || !strings.Contains(err.Error(), "dedicated") {
		t.Fatalf("session mode must be refused, got %v", err)
	}
	if f.exec.count() != 0 || len(f.send.sent) != 0 {
		t.Fatal("nothing may run or send without the dedicated account")
	}
	d := f.deliveries(t)
	if len(d) != 1 || d[0].Status != "failed" {
		t.Fatalf("deliveries %+v", d)
	}
	events, err := f.db.ListAlertEvents(10, alerts.EventTypeReportFailed, "")
	if err != nil || len(events) != 1 || events[0].Fingerprint == nil || *events[0].Fingerprint != "report:"+f.sub.ID+":error" {
		t.Fatalf("failure event %+v %v", events, err)
	}
}

func TestRetriesReuseSnapshot(t *testing.T) {
	f := newFixture(t, "service_account")
	f.send.failures = 2
	d, err := f.runner.SendNow(context.Background(), f.sub)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "sending" || d.Targets[0].Status != database.ReportTargetRetry || d.Targets[0].Attempts != 1 {
		t.Fatalf("after first failure: %+v %+v", d, d.Targets)
	}
	// The query result changes; retries must still send the stored one.
	f.exec.result = &tunnel.QueryResult{Meta: json.RawMessage(`[{"name":"n","type":"UInt64"}]`), Data: json.RawMessage(`[{"n":"99"}]`)}
	for i := 0; i < 2; i++ {
		f.now = f.now.Add(Backoff(i+1) + time.Second)
		f.runner.DeliverDue(context.Background())
	}
	if f.exec.count() != 1 {
		t.Fatalf("retries ran the query again: %d queries", f.exec.count())
	}
	if f.send.attempts != 3 || len(f.send.sent) != 1 {
		t.Fatalf("attempts %d sent %d", f.send.attempts, len(f.send.sent))
	}
	if !strings.Contains(f.send.sent[0].msg.Text, "returned 2 rows") {
		t.Fatal("retry did not reuse the snapshot")
	}
	got, _ := f.db.GetReportDelivery(d.ID)
	if got.Status != "delivered" || got.FinishedAt == nil || got.Targets[0].Attempts != 3 {
		t.Fatalf("delivery %+v %+v", got, got.Targets)
	}
	s, _ := f.db.GetReportSubscription(f.sub.ID)
	if s.LastStatus == nil || *s.LastStatus != "delivered" || s.ConsecutiveFailures != 0 {
		t.Fatalf("subscription %+v", s)
	}
	var n int
	_ = f.db.Conn().QueryRow(`SELECT count(*) FROM audit_logs WHERE action = 'report.delivered'`).Scan(&n)
	if n != 1 {
		t.Fatalf("report.delivered audits: %d", n)
	}
}

func TestSendGivesUpAfterFiveAttempts(t *testing.T) {
	f := newFixture(t, "service_account")
	f.send.failures = 100
	d, err := f.runner.SendNow(context.Background(), f.sub)
	if err != nil {
		t.Fatal(err)
	}
	for i := 1; i < SendAttempts+2; i++ {
		f.now = f.now.Add(Backoff(i) + time.Second)
		f.runner.DeliverDue(context.Background())
	}
	got, _ := f.db.GetReportDelivery(d.ID)
	if got.Status != "failed" || got.Targets[0].Status != database.ReportTargetFailed || got.Targets[0].Attempts != SendAttempts {
		t.Fatalf("delivery %+v %+v", got, got.Targets)
	}
	if f.send.attempts != SendAttempts {
		t.Fatalf("attempts %d", f.send.attempts)
	}
	s, _ := f.db.GetReportSubscription(f.sub.ID)
	if s.ConsecutiveFailures != 1 {
		t.Fatalf("a failed delivery counts as a failure: %d", s.ConsecutiveFailures)
	}
}

func TestAutoDisableAfterTenFailures(t *testing.T) {
	f := newFixture(t, "service_account")
	f.exec.err = errors.New("Code: 60. Table t does not exist")
	for i := 0; i < MaxConsecutiveFailures; i++ {
		s, _ := f.db.GetReportSubscription(f.sub.ID)
		if !s.Enabled {
			t.Fatalf("disabled after only %d failures", i)
		}
		if _, err := f.runner.SendNow(context.Background(), s); err == nil {
			t.Fatal("expected a query failure")
		}
	}
	s, _ := f.db.GetReportSubscription(f.sub.ID)
	if s.Enabled || s.ConsecutiveFailures != MaxConsecutiveFailures {
		t.Fatalf("not disabled: enabled=%v failures=%d", s.Enabled, s.ConsecutiveFailures)
	}
	if len(f.send.sent) != 0 {
		t.Fatal("a failed query must send nothing")
	}
	events, _ := f.db.ListAlertEvents(10, alerts.EventTypeReportDisabled, "")
	if len(events) != 1 {
		t.Fatalf("disabled events: %d", len(events))
	}
}

func TestGuardrailBlocksBeforeExecution(t *testing.T) {
	f := newFixture(t, "service_account")
	f.runner.guard = denyGuard{}
	_, err := f.runner.SendNow(context.Background(), f.sub)
	if err == nil || !strings.Contains(err.Error(), "no-pii") {
		t.Fatalf("want a guardrail block, got %v", err)
	}
	if f.exec.count() != 0 || len(f.send.sent) != 0 {
		t.Fatal("blocked query reached ClickHouse or a channel")
	}
}

func TestSkipIfEmpty(t *testing.T) {
	f := newFixture(t, "service_account")
	f.exec.result = &tunnel.QueryResult{Meta: json.RawMessage(`[{"name":"n","type":"UInt64"}]`), Data: json.RawMessage(`[]`)}
	if _, err := f.db.Conn().Exec(`UPDATE report_subscriptions SET skip_if_empty = 1 WHERE id = ?`, f.sub.ID); err != nil {
		t.Fatal(err)
	}
	s, _ := f.db.GetReportSubscription(f.sub.ID)
	d, err := f.runner.SendNow(context.Background(), s)
	if err != nil {
		t.Fatal(err)
	}
	if d.Status != "skipped" || len(f.send.sent) != 0 {
		t.Fatalf("status %s, %d sends", d.Status, len(f.send.sent))
	}
}

func TestPreviewSendsNothing(t *testing.T) {
	f := newFixture(t, "service_account")
	snap, err := f.runner.Preview(context.Background(), f.sub)
	if err != nil {
		t.Fatal(err)
	}
	if snap.RowCount != 2 || len(f.send.sent) != 0 || len(f.deliveries(t)) != 0 {
		t.Fatalf("preview stored or sent: rows=%d sends=%d", snap.RowCount, len(f.send.sent))
	}
}

func TestSavedQueryParamsNeedDefaults(t *testing.T) {
	params := `{"a":"1"}`
	q := &database.SavedQuery{Query: "SELECT {a:UInt8}, {b:String}, '{c:String}'", Parameters: &params}
	if _, err := SavedQueryParams(q); err == nil || !strings.Contains(err.Error(), "b") || strings.Contains(err.Error(), "c") {
		t.Fatalf("want missing b only, got %v", err)
	}
}

// "All" of a query variable whose options reach the cap runs as a subquery
// over the options query, never as the first MaxOptions values, and the
// summary names it All.
func TestRunPanelAllPastTheOptionsCap(t *testing.T) {
	f := newFixture(t, "service_account")
	rows := make([]string, dashboards.MaxOptions)
	for i := range rows {
		rows[i] = fmt.Sprintf(`{"host":"h%d"}`, i)
	}
	f.exec.result = &tunnel.QueryResult{
		Meta: json.RawMessage(`[{"name":"host","type":"String"}]`),
		Data: json.RawMessage("[" + strings.Join(rows, ",") + "]"),
	}
	vars := []dashboards.Variable{{Name: "host", Type: dashboards.TypeQuery, Query: "SELECT host FROM logs", Multi: true, IncludeAll: true}}
	s := *f.sub
	s.Variables = map[string][]string{"host": {dashboards.AllValue}}
	p := database.Panel{Query: "SELECT count() AS n FROM logs WHERE host IN ($host)"}
	used := map[string][]string{}
	if _, err := f.runner.runPanel(context.Background(), &s, Credentials{User: "reporter"}, p, vars, nil, used); err != nil {
		t.Fatal(err)
	}
	if f.exec.count() != 2 {
		t.Fatalf("want the options query and the panel query, got %v", f.exec.calls)
	}
	if want := "WHERE host IN (SELECT `host` FROM (\nSELECT host FROM logs\n))"; !strings.Contains(f.exec.calls[1], want) {
		t.Fatalf("panel query %q\nwant it to contain %q", f.exec.calls[1], want)
	}
	if !reflect.DeepEqual(used["host"], []string{"All"}) {
		t.Fatalf("summary values %v", used["host"])
	}
}
