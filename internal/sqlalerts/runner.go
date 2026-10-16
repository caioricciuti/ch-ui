// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package sqlalerts

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/telemetry/monitor"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const (
	// Worker is the background account SQL alerts run with. Like weekly
	// reports it must be a dedicated account: no session fallback.
	Worker = "sql_alert"
	// GuardrailEndpoint names SQL alert queries to the guardrails.
	GuardrailEndpoint = "sql_alert"

	tick    = 30 * time.Second
	workers = 4
	// historySampleInterval is how often a steady state is still recorded.
	historySampleInterval = time.Hour
	// queryGrace is added to the gateway wait on top of the alert timeout,
	// so ClickHouse's own max_execution_time error arrives first.
	queryGrace = 5 * time.Second
)

// lastHistory remembers when each alert last wrote an evaluation row, so a
// steady state is sampled once an hour. Shared by every runner.
var lastHistory sync.Map // alert id -> time.Time

// Credentials are the ClickHouse credentials an evaluation runs with.
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

// Runner evaluates enabled SQL alerts on their interval with each
// connection's dedicated sql_alert background account.
type Runner struct {
	db     *database.DB
	exec   Executor
	guard  Guardrails
	secret string
	pro    func() bool

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	done    chan struct{}
	ticking sync.Mutex // held while a tick runs; a slow tick skips the next
}

// NewRunner wires a runner; call Start to begin ticking. pro reports whether
// Pro work may run now (see config.ProGate); while it returns false, or is
// nil, ticks evaluate nothing. guard may be nil (no guardrails).
func NewRunner(db *database.DB, exec Executor, guard Guardrails, secret string, pro func() bool) *Runner {
	return &Runner{db: db, exec: exec, guard: guard, secret: secret, pro: pro}
}

// Start begins the background loop; idempotent.
func (r *Runner) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.stopCh = make(chan struct{})
	r.done = make(chan struct{})
	r.running = true
	stopCh, done := r.stopCh, r.done
	r.mu.Unlock()

	go func() {
		defer close(done)
		defer safe.Recover("sql-alerts")
		slog.Info("SQL alert runner started", "tick", tick)
		ticker := time.NewTicker(tick)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				slog.Info("SQL alert runner stopped")
				return
			case <-ticker.C:
				r.tick()
			}
		}
	}()
}

// Stop ends the background loop and waits for a running tick; idempotent.
func (r *Runner) Stop() {
	r.mu.Lock()
	if !r.running {
		r.mu.Unlock()
		return
	}
	close(r.stopCh)
	r.running = false
	done := r.done
	r.mu.Unlock()
	<-done
}

func (r *Runner) tick() {
	if r.pro == nil || !r.pro() {
		return
	}
	if !r.ticking.TryLock() {
		slog.Debug("SQL alerts: previous tick still running, skipping")
		return
	}
	defer r.ticking.Unlock()
	list, err := r.db.ListSQLAlerts("")
	if err != nil {
		slog.Error("SQL alerts: failed to list", "error", err)
		return
	}
	now := time.Now()
	pruneHistory(list)
	creds := map[string]*Credentials{}
	type job struct {
		a *database.SQLAlert
		c Credentials
	}
	var jobs []job
	for i := range list {
		a := &list[i]
		if !a.Enabled || !Due(a, now) {
			continue
		}
		c, ok := creds[a.ConnectionID]
		if !ok {
			// An offline tunnel is not an alert error: skip it so the
			// state and the error count stay as they are.
			if !r.exec.IsTunnelOnline(a.ConnectionID) {
				creds[a.ConnectionID] = nil
				continue
			}
			found, err := r.findCredentials(a.ConnectionID)
			if err != nil {
				slog.Debug("SQL alerts: no credentials", "connection", a.ConnectionID, "error", err)
				creds[a.ConnectionID] = nil
				continue
			}
			c = &found
			creds[a.ConnectionID] = c
		}
		if c == nil {
			continue
		}
		jobs = append(jobs, job{a: a, c: *c})
	}
	queue := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < workers && w < len(jobs); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				func() {
					defer safe.Recover("sql-alert-" + j.a.ID)
					if _, err := r.Evaluate(j.a, j.c); err != nil {
						slog.Debug("SQL alert evaluation failed", "alert", j.a.ID, "error", err)
					}
				}()
			}
		}()
	}
	for _, j := range jobs {
		queue <- j
	}
	close(queue)
	wg.Wait()
}

// pruneHistory forgets the history timestamps of alerts that no longer
// exist.
func pruneHistory(list []database.SQLAlert) {
	ids := make(map[string]bool, len(list))
	for _, a := range list {
		ids[a.ID] = true
	}
	lastHistory.Range(func(k, _ interface{}) bool {
		if id, _ := k.(string); !ids[id] {
			lastHistory.Delete(k)
		}
		return true
	})
}

// Due reports whether an alert's interval has elapsed since its last run.
func Due(a *database.SQLAlert, now time.Time) bool {
	if a.LastRunAt == nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, *a.LastRunAt)
	if err != nil {
		return true
	}
	interval := a.IntervalSeconds
	if interval < MinInterval {
		interval = MinInterval
	}
	return now.Sub(last) >= time.Duration(interval)*time.Second
}

// findCredentials resolves the dedicated sql_alert account. Session mode is
// refused: an alert must not borrow whichever person happens to be signed
// in.
func (r *Runner) findCredentials(connectionID string) (Credentials, error) {
	account, err := r.db.GetBackgroundCredential(connectionID, Worker)
	if err != nil {
		return Credentials{}, err
	}
	if account.Mode != "service_account" {
		return Credentials{}, fmt.Errorf("SQL alerts need a dedicated background account")
	}
	user, password, err := r.db.BackgroundCredentials(connectionID, Worker, r.secret)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{User: user, Password: password}, nil
}

// TestResult is the outcome of one run of a query.
type TestResult struct {
	Value      *float64                 `json:"value"`
	Rows       int                      `json:"rows"`
	Preview    []map[string]interface{} `json:"rows_preview"`
	Firing     bool                     `json:"firing"`
	DurationMs int64                    `json:"duration_ms"`
}

// Run executes an alert query once: guardrails, the wrapped SQL with the
// fixed settings, then extraction and the comparison. Nothing is stored.
// ctx carries the query tag written to system.query_log and any
// cancellation the caller has.
func (r *Runner) Run(ctx context.Context, connectionID string, c Credentials, sql, mode, comparator string, threshold float64, timeoutSeconds int) (TestResult, error) {
	start := time.Now()
	var out TestResult
	clean, err := ValidateSQL(sql)
	if err != nil {
		return out, err
	}
	if timeoutSeconds <= 0 {
		timeoutSeconds = DefaultTimeout
	}
	if r.guard != nil {
		decision, err := r.guard.EvaluateQuery(connectionID, c.User, clean, GuardrailEndpoint)
		if err != nil {
			return out, fmt.Errorf("evaluate guardrails: %w", err)
		}
		if !decision.Allowed {
			name := "a governance policy"
			if decision.Block != nil && decision.Block.PolicyName != "" {
				name = "policy " + decision.Block.PolicyName
			}
			return out, fmt.Errorf("blocked by %s", name)
		}
	}
	timeout := time.Duration(timeoutSeconds) * time.Second
	res, err := r.exec.ExecuteQueryWithSettingsCtx(ctx, connectionID, WrapSQL(clean), c.User, c.Password, QuerySettings(timeoutSeconds), timeout+queryGrace)
	out.DurationMs = time.Since(start).Milliseconds()
	if err != nil {
		return out, fmt.Errorf("query failed: %w", err)
	}
	ex, err := Extract(res, mode)
	out.Rows, out.Preview = ex.Rows, ex.Preview
	if err != nil {
		return out, err
	}
	v := ex.Value
	out.Value = &v
	out.Firing = monitor.Compare(comparator, v, threshold)
	return out, nil
}

// QueryTag names an alert's queries in system.query_log: the alert and the
// person who created it, since the sql_alert account is shared.
func QueryTag(a *database.SQLAlert) tunnel.QueryTag {
	tag := tunnel.QueryTag{Surface: "sql_alert"}
	if a.CreatedBy != nil {
		tag.Person = *a.CreatedBy
	}
	return tag.WithObject("alert", a.ID)
}

// Outcome is the result of Evaluate.
type Outcome struct {
	TestResult
	State    string `json:"state"`
	Disabled bool   `json:"disabled"`
}

// Evaluate runs an alert once with the given credentials, advances its state
// machine, stores the outcome and raises events on firing, resolve and
// auto-disable. Errors are stored on the alert and returned. When the alert
// was edited, disabled, deleted or evaluated by someone else while this run
// was in flight, the outcome is dropped and no event is raised.
func (r *Runner) Evaluate(a *database.SQLAlert, c Credentials) (Outcome, error) {
	now := time.Now()
	ctx := tunnel.WithTag(context.Background(), QueryTag(a))
	res, runErr := r.Run(ctx, a.ConnectionID, c, a.SQL, a.Mode, a.Comparator, a.Threshold, a.TimeoutSeconds)
	out := Outcome{TestResult: res, State: a.State}
	if runErr != nil {
		msg := runErr.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		errors := a.ErrorCount + 1
		disable := errors >= MaxConsecutiveErr
		run := database.SQLAlertRun{
			State: a.State, BreachCount: a.BreachCount, OkCount: a.OkCount, ErrorCount: errors, Error: msg, Disable: disable,
		}
		if disable {
			// A disabled alert holds no incident.
			run.State, run.BreachCount, run.OkCount = alerts.StateOK, 0, 0
		}
		applied, err := r.db.RecordSQLAlertRun(a, run)
		if err != nil {
			slog.Warn("SQL alerts: failed to record error", "alert", a.ID, "error", err)
		}
		if !applied {
			return out, runErr
		}
		if disable && a.State == alerts.StateFiring {
			// The incident's resolve gates the disable: without it the
			// alert stays as it was and the next tick tries again.
			if err := CloseIncident(r.db, a, fmt.Sprintf("it was disabled after %d failed runs", errors), now); err != nil {
				r.revert(a, run)
				return out, runErr
			}
		}
		r.record(a, nil, "error", msg, res.DurationMs, now, true)
		if disable {
			out.Disabled = true
			out.State = alerts.StateOK
			r.emitDisabled(a, msg, errors, now)
		}
		return out, runErr
	}

	breach := ""
	if res.Firing {
		breach = a.Severity
	}
	prevState := a.State
	if prevState == "" {
		prevState = alerts.StateOK
	}
	prev := alerts.Hysteresis{State: prevState, BreachCount: a.BreachCount, OkCount: a.OkCount}
	if prevState != alerts.StateOK {
		prev.Severity = a.Severity
	}
	next, transition := alerts.Step(prev, breach, false, a.ForEvaluations, a.ResolveEvaluations)
	run := database.SQLAlertRun{
		State: next.State, BreachCount: next.BreachCount, OkCount: next.OkCount, ErrorCount: 0, Value: res.Value,
	}
	applied, err := r.db.RecordSQLAlertRun(a, run)
	if err != nil {
		slog.Warn("SQL alerts: failed to record run", "alert", a.ID, "error", err)
	}
	if !applied {
		slog.Debug("SQL alerts: alert changed during evaluation, outcome dropped", "alert", a.ID)
		return out, nil
	}
	var emitErr error
	switch transition {
	case alerts.TransitionFiring, alerts.TransitionEscalated:
		emitErr = r.emit(a, *res.Value, false, now)
	case alerts.TransitionResolved:
		emitErr = r.emit(a, *res.Value, true, now)
	}
	if emitErr != nil {
		// No event, no transition: the next tick evaluates again.
		r.revert(a, run)
		return out, nil
	}
	out.State = next.State
	r.record(a, res.Value, next.State, "", res.DurationMs, now, next.State != prevState || a.ErrorCount > 0)
	if next.State != prevState && (next.State == alerts.StateFiring || prevState == alerts.StateFiring) {
		action := "sql_alert.fired"
		if next.State != alerts.StateFiring {
			action = "sql_alert.resolved"
		}
		details := fmt.Sprintf(`{"alert":%q,"name":%q,"value":%s,"threshold":%s}`, a.ID, a.Name,
			strconv.FormatFloat(*res.Value, 'f', -1, 64), strconv.FormatFloat(a.Threshold, 'f', -1, 64))
		conn := a.ConnectionID
		if err := r.db.CreateAuditLog(database.AuditLogParams{Action: action, ConnectionID: &conn, Details: &details}); err != nil {
			slog.Warn("SQL alerts: audit failed", "alert", a.ID, "error", err)
		}
	}
	return out, nil
}

// revert undoes an applied run whose alert event could not be written.
func (r *Runner) revert(a *database.SQLAlert, run database.SQLAlertRun) {
	if ok, err := r.db.RevertSQLAlertRun(a, run); err != nil || !ok {
		slog.Warn("SQL alerts: failed to revert a run without its event", "alert", a.ID, "reverted", ok, "error", err)
	}
}

// record writes an evaluation row on a change, an error, or once an hour.
func (r *Runner) record(a *database.SQLAlert, value *float64, state, msg string, durationMs int64, now time.Time, force bool) {
	if !force {
		if last, ok := lastHistory.Load(a.ID); ok {
			if t, _ := last.(time.Time); now.Sub(t) < historySampleInterval {
				return
			}
		}
	}
	lastHistory.Store(a.ID, now)
	if err := r.db.InsertAlertEvaluations([]database.AlertEvaluation{{
		Source: "sql", SourceID: a.ID, ConnectionID: a.ConnectionID, Value: value, State: state,
		Error: msg, DurationMs: durationMs, EvaluatedAt: now.UTC().Format(time.RFC3339),
	}}); err != nil {
		slog.Warn("SQL alerts: failed to record evaluation", "alert", a.ID, "error", err)
	}
}

// Fingerprint is the dispatcher de-dupe key of an alert's firing or resolve.
// It carries the time of the transition, so a new incident is never taken
// for a repeat of the last one inside a rule's cooldown.
func Fingerprint(id string, resolved bool, at time.Time) string {
	kind := "firing"
	if resolved {
		kind = "resolved"
	}
	return "sql:" + id + ":" + kind + ":" + strconv.FormatInt(at.UnixNano(), 10)
}

// AlertKey names a SQL alert for acknowledgements.
func AlertKey(id string) string {
	return "sql:" + id
}

// View presents an alert: a failing alert reads as "error" whatever its
// incident state, which is kept for when it runs again.
func View(a database.SQLAlert) database.SQLAlert {
	if a.ErrorCount > 0 {
		a.State = "error"
	}
	return a
}

// incidentID names the incident of a firing or resolve: a firing starts one
// at its own time; a resolve closes the one the last firing started.
func incidentID(db *database.DB, id string, resolved bool, now time.Time) string {
	if resolved {
		if inc, err := db.LatestAlertIncidentID(id, AlertKey(id)); err == nil && inc != "" {
			return inc
		}
	}
	return now.UTC().Format(time.RFC3339)
}

func (r *Runner) emit(a *database.SQLAlert, value float64, resolved bool, now time.Time) error {
	v := strconv.FormatFloat(value, 'f', -1, 64)
	t := strconv.FormatFloat(a.Threshold, 'f', -1, 64)
	subject := "value"
	if a.Mode == "rows" {
		subject = "rows"
	}
	payload := map[string]interface{}{
		"alert_id": a.ID, "name": a.Name, "mode": a.Mode, "comparator": a.Comparator,
		"threshold": a.Threshold, "value": value, "status": "firing",
		"alert_key": AlertKey(a.ID), "incident_id": incidentID(r.db, a.ID, resolved, now),
	}
	title := fmt.Sprintf("%s: %s %s %s %s", a.Name, subject, v, ComparatorSymbol(a.Comparator), t)
	message := fmt.Sprintf("SQL alert %s returned %s %s, which is %s %s.", a.Name, subject, v, ComparatorSymbol(a.Comparator), t)
	if resolved {
		payload["status"] = "resolved"
		title = "[RESOLVED] " + a.Name
		message = fmt.Sprintf("SQL alert %s is back to normal (%s %s).", a.Name, subject, v)
	}
	conn := a.ConnectionID
	if _, err := r.db.CreateAlertEvent(&conn, alerts.EventTypeSQLAlert, a.Severity, title, message, payload, Fingerprint(a.ID, resolved, now), a.ID); err != nil {
		slog.Warn("SQL alerts: failed to create alert event", "alert", a.ID, "error", err)
		return err
	}
	return nil
}

// CloseIncident raises the resolved event of a firing alert whose incident
// ends without a recovery: it was disabled, its query or condition changed,
// or it was deleted. reason completes "The incident was closed because ...".
// a is the alert as it was while firing.
func CloseIncident(db *database.DB, a *database.SQLAlert, reason string, now time.Time) error {
	payload := map[string]interface{}{
		"alert_id": a.ID, "name": a.Name, "mode": a.Mode, "comparator": a.Comparator,
		"threshold": a.Threshold, "status": "resolved", "reason": reason,
		"alert_key": AlertKey(a.ID), "incident_id": incidentID(db, a.ID, true, now),
	}
	title := "[RESOLVED] " + a.Name
	message := fmt.Sprintf("SQL alert %s is no longer firing: the incident was closed because %s.", a.Name, reason)
	conn := a.ConnectionID
	if _, err := db.CreateAlertEvent(&conn, alerts.EventTypeSQLAlert, a.Severity, title, message, payload, Fingerprint(a.ID, true, now), a.ID); err != nil {
		slog.Warn("SQL alerts: failed to create resolved event", "alert", a.ID, "error", err)
		return err
	}
	return nil
}

// emitDisabled raises the one event of an alert that turned itself off after
// MaxConsecutiveErr failed runs.
func (r *Runner) emitDisabled(a *database.SQLAlert, msg string, errors int, now time.Time) {
	title := fmt.Sprintf("%s: disabled after %d failed runs", a.Name, errors)
	message := fmt.Sprintf("SQL alert %s failed %d times in a row and was disabled. Last error: %s", a.Name, errors, msg)
	payload := map[string]interface{}{"alert_id": a.ID, "name": a.Name, "error": msg, "status": "disabled", "alert_key": AlertKey(a.ID)}
	conn := a.ConnectionID
	fp := "sql:" + a.ID + ":disabled:" + strconv.FormatInt(now.Unix(), 10)
	if _, err := r.db.CreateAlertEvent(&conn, alerts.EventTypeSQLAlert, alerts.SeverityWarn, title, message, payload, fp, a.ID); err != nil {
		slog.Warn("SQL alerts: failed to create disabled event", "alert", a.ID, "error", err)
	}
	details := fmt.Sprintf(`{"alert":%q,"name":%q,"errors":%d}`, a.ID, a.Name, errors)
	if err := r.db.CreateAuditLog(database.AuditLogParams{Action: "sql_alert.auto_disabled", ConnectionID: &conn, Details: &details}); err != nil {
		slog.Warn("SQL alerts: audit failed", "alert", a.ID, "error", err)
	}
}
