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
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/dashboards"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/mail"
	"github.com/caioricciuti/ch-ui/internal/queryproc"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const (
	tickInterval = 30 * time.Second
	// queryGrace lets ClickHouse's own max_execution_time error arrive
	// before the gateway gives up.
	queryGrace = 5 * time.Second
	// sendTimeout bounds one send to one target.
	sendTimeout = 60 * time.Second
	// dashboardRowCap bounds one panel result.
	dashboardRowCap = 10000
	pruneInterval   = time.Hour
)

// ErrBusy means MaxConcurrent runs are already in flight, or this report is
// already running.
var ErrBusy = errors.New("report runs are busy; try again in a minute")

// Credentials are the ClickHouse account a run uses.
type Credentials struct {
	User, Password string
}

// Executor runs SQL on a connection; satisfied by *tunnel.Gateway.
type Executor interface {
	ExecuteQueryWithSettingsCtx(ctx context.Context, connectionID, sql, user, password string, settings map[string]string, timeout time.Duration) (*tunnel.QueryResult, error)
	IsTunnelOnline(connectionID string) bool
}

// Guardrails evaluates a query against the governance policies; satisfied
// by *governance.GuardrailService.
type Guardrails interface {
	EvaluateQuery(connectionID, user, queryText, requestEndpoint string) (governance.GuardrailDecision, error)
}

// Sender delivers a snapshot to one channel.
type Sender interface {
	SendEmail(ctx context.Context, channelType string, cfg map[string]interface{}, recipients []string, msg mail.Message) (string, error)
	SendWebhook(ctx context.Context, cfg map[string]interface{}, rep alerts.ReportWebhook) (string, error)
}

type channelSender struct{}

func (channelSender) SendEmail(ctx context.Context, channelType string, cfg map[string]interface{}, recipients []string, msg mail.Message) (string, error) {
	return mail.SendMessage(ctx, channelType, cfg, recipients, msg)
}

func (channelSender) SendWebhook(ctx context.Context, cfg map[string]interface{}, rep alerts.ReportWebhook) (string, error) {
	return alerts.SendReportWebhook(ctx, (*http.Client)(nil), cfg, rep)
}

// Runner runs due report subscriptions and retries their sends.
type Runner struct {
	db     *database.DB
	exec   Executor
	guard  Guardrails
	secret string
	appURL string
	pro    func() bool

	// Send delivers to channels; tests replace it.
	Send Sender
	// Now is the clock; tests replace it.
	Now func() time.Time

	mu        sync.Mutex
	running   map[string]bool
	slots     chan struct{}
	wg        sync.WaitGroup
	paused    bool
	stopCh    chan struct{}
	done      chan struct{}
	lastPrune time.Time
	sending   sync.Mutex
}

// NewRunner wires a runner. pro reports whether Pro work may run now (see
// config.ProGate); while it returns false, or is nil, ticks do nothing, and
// slots that fell due meanwhile are skipped, not run late. guard may be
// nil.
func NewRunner(db *database.DB, exec Executor, guard Guardrails, secret, appURL string, pro func() bool) *Runner {
	return &Runner{
		db: db, exec: exec, guard: guard, secret: secret, appURL: strings.TrimRight(strings.TrimSpace(appURL), "/"), pro: pro,
		Send: channelSender{}, Now: time.Now,
		running: map[string]bool{}, slots: make(chan struct{}, MaxConcurrent),
	}
}

// Start begins the ticker; idempotent.
func (r *Runner) Start() {
	r.mu.Lock()
	if r.stopCh != nil {
		r.mu.Unlock()
		return
	}
	r.stopCh = make(chan struct{})
	r.done = make(chan struct{})
	stopCh, done := r.stopCh, r.done
	r.mu.Unlock()
	// A send cut short by a restart is tried again.
	if err := r.db.ResetStaleReportTargets(); err != nil {
		slog.Warn("Reports: failed to requeue interrupted sends", "error", err)
	}
	go func() {
		defer close(done)
		defer safe.Recover("reports")
		slog.Info("Report runner started", "interval", tickInterval)
		ticker := time.NewTicker(tickInterval)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				slog.Info("Report runner stopped")
				return
			case <-ticker.C:
				r.Tick()
			}
		}
	}()
}

// Stop ends the ticker and waits for runs in flight.
func (r *Runner) Stop() {
	r.mu.Lock()
	stopCh, done := r.stopCh, r.done
	r.stopCh = nil
	r.mu.Unlock()
	if stopCh == nil {
		return
	}
	close(stopCh)
	<-done
	r.wg.Wait()
}

// Wait blocks until every started run has finished.
func (r *Runner) Wait() { r.wg.Wait() }

// Tick starts due subscriptions, retries due sends and prunes old history.
func (r *Runner) Tick() {
	if r.pro == nil || !r.pro() {
		r.paused = true
		return
	}
	resuming := r.paused
	r.paused = false
	now := r.Now().UTC()
	r.prune(now)

	subs, err := r.db.ListReportSubscriptions(true)
	if err != nil {
		slog.Error("Reports: failed to list subscriptions", "error", err)
		return
	}
	for i := range subs {
		s := subs[i]
		if s.NextRunAt == nil {
			if err := r.db.SetReportNextRun(s.ID, NextRun(&s, now)); err != nil {
				slog.Warn("Reports: failed to set next run", "report", s.ID, "error", err)
			}
			continue
		}
		next, err := time.Parse(time.RFC3339, *s.NextRunAt)
		if err != nil || next.After(now) {
			continue
		}
		if resuming {
			// Missed while the license was inactive: wait for the next slot.
			if err := r.db.SetReportNextRun(s.ID, NextRun(&s, now)); err != nil {
				slog.Warn("Reports: failed to skip missed slot", "report", s.ID, "error", err)
			}
			slog.Info("Skipped report slot missed while paused", "report", s.ID, "name", s.Name)
			continue
		}
		if !r.acquire(s.ID) {
			continue
		}
		r.wg.Add(1)
		go func() {
			defer r.wg.Done()
			defer r.release(s.ID)
			defer safe.Recover("report-run")
			ctx := context.Background()
			if _, err := r.run(ctx, &s, "schedule"); err != nil {
				slog.Debug("Report run failed", "report", s.ID, "error", err)
			}
		}()
	}
	r.DeliverDue(context.Background())
}

// acquire takes a run slot for id; false when the report is already running
// or every slot is busy.
func (r *Runner) acquire(id string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.running[id] {
		return false
	}
	select {
	case r.slots <- struct{}{}:
	default:
		return false
	}
	r.running[id] = true
	return true
}

func (r *Runner) release(id string) {
	r.mu.Lock()
	delete(r.running, id)
	r.mu.Unlock()
	<-r.slots
}

func (r *Runner) prune(now time.Time) {
	if !r.lastPrune.IsZero() && now.Sub(r.lastPrune) < pruneInterval {
		return
	}
	r.lastPrune = now
	cleared, deleted, err := r.db.PruneReportDeliveries(now.Add(-SnapshotRetention), now.Add(-HistoryRetention))
	if err != nil {
		slog.Warn("Reports: prune failed", "error", err)
		return
	}
	if cleared > 0 || deleted > 0 {
		slog.Info("Reports: pruned delivery history", "snapshots_cleared", cleared, "deliveries_deleted", deleted)
	}
}

// Credentials resolves the dedicated report account of a connection.
// Session mode is refused: a report must not borrow whichever person
// happens to be signed in.
func (r *Runner) Credentials(connectionID string) (Credentials, error) {
	account, err := r.db.GetBackgroundCredential(connectionID, Worker)
	if err != nil {
		return Credentials{}, err
	}
	if account.Mode != "service_account" {
		return Credentials{}, fmt.Errorf("reports need a dedicated report background account on this connection (Admin, Connections)")
	}
	user, password, err := r.db.BackgroundCredentials(connectionID, Worker, r.secret)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{User: user, Password: password}, nil
}

// SendNow runs a subscription at once and sends it, whatever its schedule.
func (r *Runner) SendNow(ctx context.Context, s *database.ReportSubscription) (*database.ReportDelivery, error) {
	if !r.acquire(s.ID) {
		return nil, ErrBusy
	}
	defer r.release(s.ID)
	id, err := r.run(ctx, s, "manual")
	if id == "" {
		return nil, err
	}
	d, gerr := r.db.GetReportDelivery(id)
	if gerr != nil {
		return nil, gerr
	}
	return d, err
}

// Preview renders a subscription with the report account and sends
// nothing.
func (r *Runner) Preview(ctx context.Context, s *database.ReportSubscription) (*Snapshot, error) {
	if !r.acquire(s.ID) {
		return nil, ErrBusy
	}
	defer r.release(s.ID)
	creds, err := r.Credentials(s.ConnectionID)
	if err != nil {
		return nil, err
	}
	return r.Build(ctx, s, creds)
}

func (r *Runner) link(s *database.ReportSubscription) string {
	if r.appURL == "" {
		return ""
	}
	if s.SourceType == SourceDashboard && s.DashboardID != nil {
		return r.appURL + "/dashboards/" + *s.DashboardID
	}
	return r.appURL + "/schedules?section=reports"
}

// run executes one subscription: the query, then a snapshot, then a first
// send to every target. It returns the delivery id.
func (r *Runner) run(ctx context.Context, s *database.ReportSubscription, trigger string) (string, error) {
	start := r.Now().UTC()
	if trigger == "schedule" {
		if err := r.db.SetReportNextRun(s.ID, NextRun(s, start)); err != nil {
			slog.Warn("Reports: failed to set next run", "report", s.ID, "error", err)
		}
	}
	creds, credErr := r.Credentials(s.ConnectionID)
	deliveryID, err := r.db.CreateReportDelivery(s.ID, trigger, creds.User, start)
	if err != nil {
		return "", err
	}
	var snap *Snapshot
	runErr := credErr
	if runErr == nil {
		snap, runErr = r.Build(ctx, s, creds)
	}
	duration := r.Now().Sub(start).Milliseconds()
	if runErr != nil {
		msg := oneLine(runErr.Error(), 1000)
		if err := r.db.FinishReportDelivery(deliveryID, database.ReportDeliveryResult{Status: "failed", DurationMs: duration, Error: msg, Finished: true}, r.Now()); err != nil {
			slog.Warn("Reports: failed to record failed run", "report", s.ID, "error", err)
		}
		r.recordFailure(s, deliveryID, "failed", msg)
		r.emitFailed(s, deliveryID, msg)
		return deliveryID, runErr
	}
	if snap.Empty && s.SkipIfEmpty {
		if err := r.db.FinishReportDelivery(deliveryID, database.ReportDeliveryResult{Status: "skipped", DurationMs: duration, Finished: true}, r.Now()); err != nil {
			slog.Warn("Reports: failed to record skipped run", "report", s.ID, "error", err)
		}
		if _, err := r.db.RecordReportOutcome(s.ID, "skipped", "", false, true, false, r.Now()); err != nil {
			slog.Warn("Reports: failed to record outcome", "report", s.ID, "error", err)
		}
		return deliveryID, nil
	}
	raw, err := json.Marshal(snap)
	if err != nil {
		return deliveryID, err
	}
	targets := make([]string, 0, len(s.Targets))
	for _, t := range s.Targets {
		targets = append(targets, t.ChannelID)
	}
	if err := r.db.FinishReportDelivery(deliveryID, database.ReportDeliveryResult{
		Status: "sending", Rows: snap.RowCount, Bytes: snap.Bytes(), Truncated: snap.Truncated, DurationMs: duration,
		Snapshot: raw, Targets: targets,
	}, r.Now()); err != nil {
		return deliveryID, err
	}
	if _, err := r.db.RecordReportOutcome(s.ID, "sending", "", false, false, false, r.Now()); err != nil {
		slog.Warn("Reports: failed to record outcome", "report", s.ID, "error", err)
	}
	r.deliver(ctx, deliveryID)
	return deliveryID, nil
}

// Build runs the subscription's query (or panel queries) as creds and
// renders the snapshot. Nothing is stored or sent.
func (r *Runner) Build(ctx context.Context, s *database.ReportSubscription, creds Credentials) (*Snapshot, error) {
	if !r.exec.IsTunnelOnline(s.ConnectionID) {
		return nil, fmt.Errorf("the connection's agent is not connected")
	}
	ctx, cancel := context.WithTimeout(ctx, time.Duration(s.TimeoutSeconds)*time.Second+queryGrace)
	defer cancel()
	if s.SourceType == SourceDashboard {
		return r.buildDashboard(ctx, s, creds)
	}
	return r.buildQuery(ctx, s, creds)
}

// querySettings are sent with every report query. readonly=1 refuses
// writes and setting changes inside the SQL. A user whose profile is already
// read only refuses the other settings; the gateway then drops them, and the
// LIMIT wrap still bounds the result.
func querySettings(timeoutSeconds, maxRows, maxBytes int) map[string]string {
	return map[string]string{
		"readonly":             "1",
		"max_execution_time":   strconv.Itoa(timeoutSeconds),
		"max_result_rows":      strconv.Itoa(maxRows),
		"max_result_bytes":     strconv.Itoa(maxBytes),
		"result_overflow_mode": "break",
		"log_comment":          LogComment,
	}
}

// resultBytes bounds the result ClickHouse sends: generous next to the
// attachment cap, since JSON is larger than CSV, and never above 64 MiB.
func resultBytes(maxBytes int) int {
	n := maxBytes * 8
	if n < 8<<20 {
		n = 8 << 20
	}
	if n > 64<<20 {
		n = 64 << 20
	}
	return n
}

func (r *Runner) checkGuardrails(connectionID, user, sql string) error {
	if r.guard == nil {
		return nil
	}
	decision, err := r.guard.EvaluateQuery(connectionID, user, sql, GuardrailEndpoint)
	if err != nil {
		return fmt.Errorf("evaluate guardrails: %w", err)
	}
	if !decision.Allowed {
		name := "a governance policy"
		if decision.Block != nil && decision.Block.PolicyName != "" {
			name = "policy " + decision.Block.PolicyName
		}
		return fmt.Errorf("blocked by %s", name)
	}
	return nil
}

func (r *Runner) buildQuery(ctx context.Context, s *database.ReportSubscription, creds Credentials) (*Snapshot, error) {
	if s.SavedQueryID == nil {
		return nil, fmt.Errorf("no saved query")
	}
	q, err := r.db.GetSavedQueryByID(*s.SavedQueryID)
	if err != nil {
		return nil, fmt.Errorf("load saved query: %w", err)
	}
	if q == nil {
		return nil, fmt.Errorf("the saved query was deleted")
	}
	sql, err := ValidateSQL(q.Query)
	if err != nil {
		return nil, err
	}
	params, err := SavedQueryParams(q)
	if err != nil {
		return nil, err
	}
	if err := r.checkGuardrails(s.ConnectionID, creds.User, sql); err != nil {
		return nil, err
	}
	limit := s.RowLimit + 1
	settings := querySettings(s.TimeoutSeconds, limit, resultBytes(s.MaxBytes))
	for name, value := range params {
		settings["param_"+name] = value
	}
	wrapped := fmt.Sprintf("SELECT * FROM (\n%s\n) LIMIT %d", sql, limit)
	res, err := r.exec.ExecuteQueryWithSettingsCtx(ctx, s.ConnectionID, wrapped, creds.User, creds.Password, settings, time.Duration(s.TimeoutSeconds)*time.Second+queryGrace)
	if err != nil {
		return nil, fmt.Errorf("query failed: %w", err)
	}
	t, err := ParseResult(res, s.RowLimit)
	if err != nil {
		return nil, err
	}
	return RenderQuery(s, t, r.link(s), r.Now()), nil
}

func (r *Runner) buildDashboard(ctx context.Context, s *database.ReportSubscription, creds Credentials) (*Snapshot, error) {
	if s.DashboardID == nil {
		return nil, fmt.Errorf("no dashboard")
	}
	d, err := r.db.GetDashboardByID(*s.DashboardID)
	if err != nil {
		return nil, fmt.Errorf("load dashboard: %w", err)
	}
	if d == nil {
		return nil, fmt.Errorf("the dashboard was deleted")
	}
	panels, err := r.db.GetPanelsByDashboard(d.ID)
	if err != nil {
		return nil, fmt.Errorf("load panels: %w", err)
	}
	SortPanels(panels)
	rawVars, err := r.db.GetDashboardVariables(d.ID)
	if err != nil {
		return nil, fmt.Errorf("load variables: %w", err)
	}
	vars := dashboards.DecodeStored(rawVars)
	timeRange := s.TimeRange
	if timeRange == "" {
		timeRange = DefaultTimeRange
	}
	now := r.Now()
	tr := &queryproc.TimeRange{Type: "relative", From: timeRange, To: "now"}
	from, to, _ := queryproc.BoundsAt(tr, now)
	// Absolute bounds, so every panel covers the same instants.
	fixed := &queryproc.TimeRange{Type: "absolute", From: from.UTC().Format(time.RFC3339), To: to.UTC().Format(time.RFC3339)}

	summary := DashboardSummary{Name: d.Name, TimeRange: timeRange, From: from, To: to, Variables: map[string][]string{}}
	queried := 0
	failed := 0
	for _, p := range panels {
		if PanelKind(p) == "text" || strings.TrimSpace(p.Query) == "" {
			continue
		}
		if queried == MaxDashboardPanels {
			summary.Skipped++
			continue
		}
		queried++
		t, err := r.runPanel(ctx, s, creds, p, vars, fixed, summary.Variables)
		if err != nil {
			failed++
			if ctx.Err() != nil {
				err = fmt.Errorf("the report timed out after %d seconds", s.TimeoutSeconds)
			}
		}
		summary.Results = append(summary.Results, DashboardPanelResult{Panel: p, Table: t, Err: err})
	}
	if queried > 0 && failed == queried {
		return nil, fmt.Errorf("every panel query failed; first error: %v", summary.Results[0].Err)
	}
	return RenderDashboard(s, summary, r.link(s), now), nil
}

// runPanel resolves the variables a panel uses (saved values, else
// defaults), expands its macros for the fixed range and runs it.
func (r *Runner) runPanel(ctx context.Context, s *database.ReportSubscription, creds Credentials, p database.Panel, vars []dashboards.Variable, tr *queryproc.TimeRange, used map[string][]string) (Table, error) {
	defs := dashboards.Needed(vars, p.Query)
	run := func(v dashboards.Variable, parents map[string][]string, parentTypes map[string]string) ([]dashboards.Option, string, error) {
		processed := queryproc.ProcessQueryVariables(queryproc.ProcessorOptions{Query: v.Query, TimeRange: tr, MaxDataPoints: 1000, Vars: parents, VarTypes: parentTypes})
		if len(processed.Errors) > 0 {
			return nil, "", fmt.Errorf("variable %s: %s", v.Name, strings.Join(processed.Errors, "; "))
		}
		sql := strings.TrimSpace(processed.Query)
		if err := r.checkGuardrails(s.ConnectionID, creds.User, sql); err != nil {
			return nil, "", fmt.Errorf("variable %s: %w", v.Name, err)
		}
		res, err := r.exec.ExecuteQueryWithSettingsCtx(ctx, s.ConnectionID, sql, creds.User, creds.Password,
			querySettings(s.TimeoutSeconds, dashboards.MaxOptions, 8<<20), time.Duration(s.TimeoutSeconds)*time.Second+queryGrace)
		if err != nil {
			return nil, "", fmt.Errorf("variable %s: %w", v.Name, err)
		}
		t, err := ParseResult(res, dashboards.MaxOptions)
		if err != nil {
			return nil, "", err
		}
		out := make([]dashboards.Option, 0, len(t.Rows))
		for _, row := range t.Rows {
			if len(row) > 0 {
				out = append(out, dashboards.Option{Value: row[0], Label: row[0]})
			}
		}
		// At the cap the list may be cut: "All" becomes a subquery instead.
		all := ""
		if len(out) >= dashboards.MaxOptions && len(t.Columns) > 0 {
			all = dashboards.AllSubquery(sql, t.Columns[0])
		}
		return out, all, nil
	}
	values, varTypes, err := dashboards.Resolve(defs, s.Variables, false, run)
	if err != nil {
		return Table{}, err
	}
	for k, v := range values {
		if varTypes[k] == queryproc.VarTypeSubquery {
			v = []string{"All"}
		}
		used[k] = v
	}
	processed := queryproc.ProcessQueryVariables(queryproc.ProcessorOptions{
		Query: p.Query, TimeRange: tr, MaxDataPoints: 1000, Vars: values, VarTypes: varTypes,
	})
	if len(processed.Errors) > 0 {
		return Table{}, errors.New(strings.Join(processed.Errors, "; "))
	}
	sql := strings.TrimSpace(processed.Query)
	if err := r.checkGuardrails(s.ConnectionID, creds.User, sql); err != nil {
		return Table{}, err
	}
	settings := querySettings(s.TimeoutSeconds, dashboardRowCap, 16<<20)
	settings["date_time_output_format"] = "iso"
	res, err := r.exec.ExecuteQueryWithSettingsCtx(ctx, s.ConnectionID, sql, creds.User, creds.Password, settings, time.Duration(s.TimeoutSeconds)*time.Second+queryGrace)
	if err != nil {
		return Table{}, err
	}
	return ParseResult(res, dashboardRowCap)
}

// DeliverDue retries every send whose time has come.
func (r *Runner) DeliverDue(ctx context.Context) {
	if r.pro == nil || !r.pro() {
		return
	}
	if !r.sending.TryLock() {
		return
	}
	defer r.sending.Unlock()
	due, err := r.db.DueReportDeliveryTargets(r.Now(), 50)
	if err != nil {
		slog.Warn("Reports: failed to list due sends", "error", err)
		return
	}
	seen := map[string]bool{}
	for _, t := range due {
		if seen[t.DeliveryID] {
			continue
		}
		seen[t.DeliveryID] = true
		r.deliver(ctx, t.DeliveryID)
	}
}

// deliver sends a delivery's snapshot to each of its waiting targets that
// is due, then settles the delivery when none is left waiting.
func (r *Runner) deliver(ctx context.Context, deliveryID string) {
	d, err := r.db.GetReportDelivery(deliveryID)
	if err != nil || d == nil {
		return
	}
	s, err := r.db.GetReportSubscription(d.SubscriptionID)
	if err != nil || s == nil {
		return
	}
	var snap Snapshot
	if len(d.Snapshot) == 0 || json.Unmarshal(d.Snapshot, &snap) != nil {
		for _, t := range d.Targets {
			if t.Status == database.ReportTargetQueued || t.Status == database.ReportTargetRetry {
				_ = r.db.CompleteReportDeliveryTarget(d.ID, t.ChannelID, database.ReportTargetFailed, t.Attempts, nil, "the stored report content is no longer available", "")
			}
		}
		r.settle(s, d)
		return
	}
	now := r.Now().UTC()
	for _, t := range d.Targets {
		if t.Status != database.ReportTargetQueued && t.Status != database.ReportTargetRetry {
			continue
		}
		if t.NextAttemptAt != nil {
			if at, err := time.Parse(time.RFC3339, *t.NextAttemptAt); err == nil && at.After(now) {
				continue
			}
		}
		claimed, err := r.db.ClaimReportDeliveryTarget(d.ID, t.ChannelID)
		if err != nil || !claimed {
			continue
		}
		msgID, sendErr := r.sendTarget(ctx, s, t.ChannelID, &snap)
		attempts := t.Attempts + 1
		switch {
		case sendErr == nil:
			err = r.db.CompleteReportDeliveryTarget(d.ID, t.ChannelID, database.ReportTargetSent, attempts, nil, "", msgID)
		case attempts >= SendAttempts:
			err = r.db.CompleteReportDeliveryTarget(d.ID, t.ChannelID, database.ReportTargetFailed, attempts, nil, oneLine(sendErr.Error(), 500), "")
		default:
			next := r.Now().Add(Backoff(attempts))
			err = r.db.CompleteReportDeliveryTarget(d.ID, t.ChannelID, database.ReportTargetRetry, attempts, &next, oneLine(sendErr.Error(), 500), "")
		}
		if err != nil {
			slog.Warn("Reports: failed to record send", "delivery", d.ID, "channel", t.ChannelID, "error", err)
		}
		if sendErr != nil {
			slog.Warn("Report send failed", "report", s.ID, "channel", t.ChannelID, "attempt", attempts, "error", sendErr)
		}
	}
	r.settle(s, d)
}

// Backoff is the wait before attempt n+1 of a send: 1, 5, 15 then 30
// minutes.
func Backoff(attempt int) time.Duration {
	switch attempt {
	case 1:
		return time.Minute
	case 2:
		return 5 * time.Minute
	case 3:
		return 15 * time.Minute
	default:
		return 30 * time.Minute
	}
}

func (r *Runner) sendTarget(ctx context.Context, s *database.ReportSubscription, channelID string, snap *Snapshot) (string, error) {
	var target *database.ReportTarget
	for i := range s.Targets {
		if s.Targets[i].ChannelID == channelID {
			target = &s.Targets[i]
		}
	}
	if target == nil {
		return "", fmt.Errorf("the channel was removed from this report")
	}
	channel, err := r.db.GetAlertChannelByID(channelID)
	if err != nil {
		return "", err
	}
	if channel == nil || !channel.IsActive {
		return "", fmt.Errorf("the channel is missing or disabled")
	}
	raw, err := crypto.Decrypt(channel.ConfigEncrypted, r.secret)
	if err != nil {
		return "", fmt.Errorf("decrypt channel config: %w", err)
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return "", fmt.Errorf("parse channel config: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, sendTimeout)
	defer cancel()
	if strings.EqualFold(channel.ChannelType, alerts.ChannelTypeWebhook) {
		return r.Send.SendWebhook(ctx, cfg, snap.Webhook())
	}
	if len(target.Recipients) == 0 {
		return "", fmt.Errorf("no recipients for this email channel")
	}
	return r.Send.SendEmail(ctx, channel.ChannelType, cfg, target.Recipients, snap.Message())
}

// settle records the delivery's final outcome on its subscription once no
// send is waiting: failures count toward the auto-disable.
func (r *Runner) settle(s *database.ReportSubscription, d *database.ReportDelivery) {
	status, sent, failed, done, err := r.db.SettleReportDelivery(d.ID, r.Now())
	if err != nil {
		slog.Warn("Reports: failed to settle delivery", "delivery", d.ID, "error", err)
		return
	}
	if !done {
		return
	}
	if status == "failed" {
		r.recordFailure(s, d.ID, "failed", fmt.Sprintf("every send failed (%d channels)", failed))
		return
	}
	msg := ""
	if failed > 0 {
		msg = fmt.Sprintf("%d of %d channels failed", failed, sent+failed)
	}
	if _, err := r.db.RecordReportOutcome(s.ID, status, msg, false, true, false, r.Now()); err != nil {
		slog.Warn("Reports: failed to record outcome", "report", s.ID, "error", err)
	}
	recipients := 0
	for _, t := range d.Targets {
		for _, target := range s.Targets {
			if target.ChannelID != t.ChannelID {
				continue
			}
			if strings.EqualFold(target.ChannelType, alerts.ChannelTypeWebhook) {
				recipients++
			} else {
				recipients += len(target.Recipients)
			}
		}
	}
	details, _ := json.Marshal(map[string]interface{}{
		"report": s.ID, "name": s.Name, "delivery": d.ID, "trigger": d.Trigger, "status": status,
		"rows": d.Rows, "recipients": recipients, "channels_sent": sent, "channels_failed": failed,
	})
	detail := string(details)
	conn := s.ConnectionID
	if err := r.db.CreateAuditLog(database.AuditLogParams{Action: "report.delivered", ConnectionID: &conn, Details: &detail}); err != nil {
		slog.Warn("Reports: audit failed", "report", s.ID, "error", err)
	}
}

// recordFailure counts a failed run and turns the subscription off after
// MaxConsecutiveFailures in a row.
func (r *Runner) recordFailure(s *database.ReportSubscription, deliveryID, status, msg string) {
	n, err := r.db.RecordReportOutcome(s.ID, status, msg, true, true, false, r.Now())
	if err != nil {
		slog.Warn("Reports: failed to record outcome", "report", s.ID, "error", err)
		return
	}
	if n < MaxConsecutiveFailures {
		return
	}
	if err := r.db.DisableReportSubscription(s.ID); err != nil {
		slog.Warn("Reports: failed to disable", "report", s.ID, "error", err)
		return
	}
	conn := s.ConnectionID
	title := fmt.Sprintf("Report %s disabled after %d failed runs", s.Name, n)
	message := fmt.Sprintf("Report %s failed %d times in a row and was turned off. Last error: %s", s.Name, n, msg)
	payload := map[string]interface{}{"report_id": s.ID, "name": s.Name, "delivery_id": deliveryID, "failures": n, "error": msg}
	fp := "report:" + s.ID + ":disabled:" + strconv.FormatInt(r.Now().Unix(), 10)
	if _, err := r.db.CreateAlertEvent(&conn, alerts.EventTypeReportDisabled, alerts.SeverityWarn, title, message, payload, fp, s.ID); err != nil {
		slog.Warn("Reports: failed to create disabled event", "report", s.ID, "error", err)
	}
	details, _ := json.Marshal(map[string]interface{}{"report": s.ID, "name": s.Name, "failures": n})
	detail := string(details)
	if err := r.db.CreateAuditLog(database.AuditLogParams{Action: "report.auto_disabled", ConnectionID: &conn, Details: &detail}); err != nil {
		slog.Warn("Reports: audit failed", "report", s.ID, "error", err)
	}
}

func (r *Runner) emitFailed(s *database.ReportSubscription, deliveryID, msg string) {
	conn := s.ConnectionID
	payload := map[string]interface{}{"report_id": s.ID, "name": s.Name, "delivery_id": deliveryID, "error": msg}
	title := "Report failed: " + s.Name
	if _, err := r.db.CreateAlertEvent(&conn, alerts.EventTypeReportFailed, alerts.SeverityError, title, msg, payload, "report:"+s.ID+":error", deliveryID); err != nil {
		slog.Warn("Reports: failed to create failure event", "report", s.ID, "error", err)
	}
}
