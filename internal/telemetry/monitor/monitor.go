// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package monitor

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/telemetry/query"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Severity values and the alert event type are spelled out here rather
// than imported from the alerts package: that package imports
// internal/database, which imports this one, so the import would be a
// cycle. Keep these in step with the constants declared there.
const (
	severityInfo     = "info"
	severityWarn     = "warn"
	severityError    = "error"
	severityCritical = "critical"

	eventTypeTelemetryMonitor = "telemetry.monitor"
)

const (
	monitorTick        = 30 * time.Second
	monitorTimeout     = 25 * time.Second
	MinMonitorInterval = 30
	MinMonitorWindow   = 60

	// EvaluationDelay shifts every window back from now: collectors batch
	// and ClickHouse inserts land late, so the last minute is still
	// filling. A window ending at now undercounts and a "less than"
	// monitor fires on rows that are only late.
	EvaluationDelay = 60 * time.Second

	// monitorWorkers bounds how many monitors evaluate at once, so one
	// slow query does not hold up every monitor behind it and a long list
	// does not flood ClickHouse.
	monitorWorkers = 4
)

// lastGood remembers each monitor's last ok/firing outcome across error
// runs (last_state only holds the latest, "error"), so firing -> error ->
// firing does not raise a second firing event. Shared by every runner,
// including the one-off runner behind the Run now button. Process memory
// only: after a restart, firing after an error emits once.
var lastGood sync.Map // monitor id -> "ok" | "firing"

// Credentials are the ClickHouse credentials an evaluation runs with.
type Credentials struct {
	User, Password string
}

// QueryExecutor runs SQL on a connection; satisfied by *tunnel.Gateway.
type QueryExecutor interface {
	ExecuteQuery(connectionID, sql, user, password string, timeout time.Duration) (*tunnel.QueryResult, error)
}

// MonitorRunner evaluates enabled monitors using each connection's configured
// background account, falling back to active sessions only in session mode.
type MonitorRunner struct {
	db      *database.DB
	gateway QueryExecutor
	secret  string
	pro     func() bool

	mu      sync.Mutex
	running bool
	stopCh  chan struct{}
	ticking sync.Mutex // held while a tick runs; a slow tick skips the next
}

// NewMonitorRunner wires a runner; call Start to begin ticking. pro reports
// whether Pro work may run now (see config.ProGate); while it returns false,
// or is nil, the background tick evaluates nothing, so enabled monitors pause
// and resume on their own. Evaluate itself is not gated: its only other
// caller is an HTTP route that is already behind RequirePro.
func NewMonitorRunner(db *database.DB, gw *tunnel.Gateway, secret string, pro func() bool) *MonitorRunner {
	return &MonitorRunner{db: db, gateway: gw, secret: secret, pro: pro}
}

// Start begins the background loop; idempotent.
func (r *MonitorRunner) Start() {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.stopCh = make(chan struct{})
	r.running = true
	stopCh := r.stopCh
	r.mu.Unlock()

	go func() {
		defer safe.Recover("telemetry-monitors")
		slog.Info("Telemetry monitor runner started", "tick", monitorTick)
		ticker := time.NewTicker(monitorTick)
		defer ticker.Stop()
		for {
			select {
			case <-stopCh:
				slog.Info("Telemetry monitor runner stopped")
				return
			case <-ticker.C:
				r.tick()
			}
		}
	}()
}

// Stop ends the background loop; idempotent.
func (r *MonitorRunner) Stop() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.running {
		return
	}
	close(r.stopCh)
	r.running = false
}

func (r *MonitorRunner) tick() {
	if r.pro == nil || !r.pro() {
		return
	}
	if !r.ticking.TryLock() {
		slog.Debug("Telemetry monitors: previous tick still running, skipping")
		return
	}
	defer r.ticking.Unlock()
	monitors, err := r.db.ListTelemetryMonitors("")
	if err != nil {
		slog.Error("Telemetry monitors: failed to list", "error", err)
		return
	}
	now := time.Now()
	creds := map[string]*Credentials{}
	type job struct {
		m *database.TelemetryMonitor
		c Credentials
	}
	var jobs []job
	for i := range monitors {
		m := &monitors[i]
		if !m.Enabled || !monitorDue(m, now) {
			continue
		}
		c, ok := creds[m.ConnectionID]
		if !ok {
			found, err := r.findCredentials(m.ConnectionID)
			if err != nil {
				slog.Debug("Telemetry monitors: no credentials", "connection", m.ConnectionID, "error", err)
				creds[m.ConnectionID] = nil
				continue
			}
			c = &found
			creds[m.ConnectionID] = c
		}
		if c == nil {
			continue
		}
		jobs = append(jobs, job{m: m, c: *c})
	}
	queue := make(chan job)
	var wg sync.WaitGroup
	for w := 0; w < monitorWorkers && w < len(jobs); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range queue {
				func() {
					defer safe.Recover("telemetry-monitor-" + j.m.ID)
					if _, _, err := r.Evaluate(j.m, j.c); err != nil {
						slog.Warn("Telemetry monitor evaluation failed", "monitor", j.m.ID, "error", err)
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

// monitorDue reports whether the interval has elapsed since the last run.
func monitorDue(m *database.TelemetryMonitor, now time.Time) bool {
	if m.LastRunAt == nil {
		return true
	}
	last, err := time.Parse(time.RFC3339, *m.LastRunAt)
	if err != nil {
		return true
	}
	interval := m.IntervalSeconds
	if interval < MinMonitorInterval {
		interval = MinMonitorInterval
	}
	return now.Sub(last) >= time.Duration(interval)*time.Second
}

func (r *MonitorRunner) findCredentials(connectionID string) (Credentials, error) {
	user, password, err := r.db.BackgroundCredentials(connectionID, "telemetry.monitor", r.secret)
	if err != nil {
		return Credentials{}, err
	}
	return Credentials{User: user, Password: password}, nil
}

// ValidateMonitor checks a monitor's fields before storing it.
func ValidateMonitor(m *database.TelemetryMonitor) error {
	if strings.TrimSpace(m.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if m.Kind != string(telemetry.KindLogs) && m.Kind != string(telemetry.KindTraces) {
		return fmt.Errorf("kind must be logs or traces")
	}
	if strings.TrimSpace(m.SourceID) == "" {
		return fmt.Errorf("source_id is required")
	}
	if m.WindowSeconds < MinMonitorWindow || m.WindowSeconds > 7*24*3600 {
		return fmt.Errorf("window_seconds must be between %d and %d", MinMonitorWindow, 7*24*3600)
	}
	if m.IntervalSeconds < MinMonitorInterval || m.IntervalSeconds > 24*3600 {
		return fmt.Errorf("interval_seconds must be between %d and %d", MinMonitorInterval, 24*3600)
	}
	switch m.Comparator {
	case "gt", "gte", "lt", "lte":
	default:
		return fmt.Errorf("comparator must be gt, gte, lt or lte")
	}
	switch m.Severity {
	case severityInfo, severityWarn, severityError, severityCritical:
	default:
		return fmt.Errorf("severity must be info, warn, error or critical")
	}
	return nil
}

// Compare applies a comparator (gt, gte, lt, lte, eq or ne) to a value.
// Monitors accept only the first four; SQL alerts use all six.
func Compare(comparator string, value, threshold float64) bool {
	switch comparator {
	case "gt":
		return value > threshold
	case "gte":
		return value >= threshold
	case "lt":
		return value < threshold
	case "lte":
		return value <= threshold
	case "eq":
		return value == threshold
	case "ne":
		return value != threshold
	default:
		return false
	}
}

// ShouldEmit reports whether a firing evaluation deserves a new alert
// event. Only the transition into firing does. Emitting on every
// evaluation would raise an event every interval for as long as the
// condition holds, and push a notification through every attached
// channel that often: a 30 s monitor left firing for a day is close to
// 3,000 emails. The audit log in Evaluate already works this way.
//
// An error run sits between two real outcomes, so after one lastState is
// "error" and lastGood (the outcome before the error) decides: firing,
// error, firing is still one incident and emits nothing new.
func ShouldEmit(firing bool, lastState, lastGood string) bool {
	if !firing {
		return false
	}
	if lastState == "error" {
		return lastGood != "firing"
	}
	return lastState != "firing"
}

// ShouldEmitError reports whether a failed evaluation deserves an alert
// event: only the transition into error, for the same reason as above.
func ShouldEmitError(lastState string) bool {
	return lastState != "error"
}

// FingerprintBucket aligns a firing to the monitor's interval so repeated
// evaluations within one interval share a fingerprint (dispatcher de-dupe)
// and a new interval produces a fresh event.
func FingerprintBucket(m *database.TelemetryMonitor, now time.Time) string {
	interval := m.IntervalSeconds
	if interval < MinMonitorInterval {
		interval = MinMonitorInterval
	}
	bucket := now.Unix() / int64(interval)
	return "telemetry.monitor:" + m.ID + ":" + strconv.FormatInt(bucket, 10)
}

// EvaluationWindow is the [from, to] a monitor counts over at now: the
// window length ending EvaluationDelay ago.
func EvaluationWindow(m *database.TelemetryMonitor, now time.Time) (time.Time, time.Time) {
	end := now.Add(-EvaluationDelay)
	return end.Add(-time.Duration(m.WindowSeconds) * time.Second), end
}

// CountSQL builds the count query for a monitor over EvaluationWindow.
func CountSQL(m *database.TelemetryMonitor, src *telemetry.Source, columns map[string]string, now time.Time) (string, error) {
	if src.ConnectionID != m.ConnectionID {
		return "", fmt.Errorf("source belongs to another connection")
	}
	start, end := EvaluationWindow(m, now)
	from := start.UTC().Format(time.RFC3339Nano)
	to := end.UTC().Format(time.RFC3339Nano)
	switch src.Kind {
	case telemetry.KindLogs:
		if m.Kind != string(telemetry.KindLogs) || src.Logs == nil {
			return "", fmt.Errorf("monitor kind does not match the source")
		}
		where, err := telemetry.LogsWhere(src, columns, telemetry.LogsParams{From: from, To: to, Q: m.Query})
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("SELECT count() AS c FROM %s.%s WHERE %s %s", telemetry.Quote(src.Database), telemetry.Quote(src.Table), where, telemetry.QuerySettings), nil
	case telemetry.KindTraces:
		if m.Kind != string(telemetry.KindTraces) || src.Traces == nil {
			return "", fmt.Errorf("monitor kind does not match the source")
		}
		where, err := tracesCountWhere(src, columns, from, to, m.Query)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("SELECT count() AS c FROM %s.%s WHERE %s %s", telemetry.Quote(src.Database), telemetry.Quote(src.Table), where, telemetry.QuerySettings), nil
	default:
		return "", fmt.Errorf("monitors support logs and traces sources")
	}
}

// compileTraces compiles a search against a traces mapping.
func compileTraces(q string, m *telemetry.TracesMapping, columns map[string]string) (string, error) {
	return query.Compile(q, m.QuerySchema(columns))
}

// tracesCountWhere is a minimal span filter: range + compiled search.
func tracesCountWhere(src *telemetry.Source, columns map[string]string, from, to, q string) (string, error) {
	m := src.Traces
	conds := []string{}
	lo, err := telemetry.TimeBound(m.Timestamp, ">=", from)
	if err != nil {
		return "", err
	}
	hi, err := telemetry.TimeBound(m.Timestamp, "<=", to)
	if err != nil {
		return "", err
	}
	conds = append(conds, lo, hi)
	if strings.TrimSpace(q) != "" {
		w, err := compileTraces(q, m, columns)
		if err != nil {
			return "", err
		}
		if w != "1" {
			conds = append(conds, "("+w+")")
		}
	}
	return strings.Join(conds, " AND "), nil
}

// Evaluate runs a monitor once with the given credentials, records the
// outcome and emits an alert event on firing. It returns the value and
// whether it fired. Errors are recorded on the monitor and returned.
func (r *MonitorRunner) Evaluate(m *database.TelemetryMonitor, c Credentials) (float64, bool, error) {
	now := time.Now()
	fail := func(err error) (float64, bool, error) {
		msg := err.Error()
		if len(msg) > 500 {
			msg = msg[:500]
		}
		if rerr := r.db.RecordTelemetryMonitorRun(m.ID, nil, "error", msg); rerr != nil {
			slog.Warn("Telemetry monitors: failed to record error", "monitor", m.ID, "error", rerr)
		}
		if m.LastState == "ok" || m.LastState == "firing" {
			lastGood.Store(m.ID, m.LastState)
		}
		if ShouldEmitError(m.LastState) {
			r.emitError(m, msg, now)
		}
		return 0, false, err
	}
	src, err := r.db.GetTelemetrySource(m.SourceID)
	if err != nil {
		return fail(fmt.Errorf("load source: %w", err))
	}
	if src == nil {
		return fail(fmt.Errorf("source %s not found", m.SourceID))
	}
	columns, err := r.describe(m.ConnectionID, c, src)
	if err != nil {
		return fail(err)
	}
	sqlText, err := CountSQL(m, src, columns, now)
	if err != nil {
		return fail(err)
	}
	res, err := r.gateway.ExecuteQuery(m.ConnectionID, sqlText, c.User, c.Password, monitorTimeout)
	if err != nil {
		return fail(fmt.Errorf("query failed: %w", err))
	}
	value, err := firstCount(res)
	if err != nil {
		return fail(err)
	}
	firing := Compare(m.Comparator, value, m.Threshold)
	state := "ok"
	if firing {
		state = "firing"
	}
	if err := r.db.RecordTelemetryMonitorRun(m.ID, &value, state, ""); err != nil {
		slog.Warn("Telemetry monitors: failed to record run", "monitor", m.ID, "error", err)
	}
	prevGood := m.LastState
	if m.LastState == "error" {
		prevGood = ""
		if v, ok := lastGood.Load(m.ID); ok {
			prevGood, _ = v.(string)
		}
	}
	lastGood.Store(m.ID, state)
	if ShouldEmit(firing, m.LastState, prevGood) {
		r.emit(m, value, now)
	}
	if state != m.LastState {
		action := "telemetry.monitor.recovered"
		if firing {
			action = "telemetry.monitor.fired"
		}
		details := fmt.Sprintf(`{"monitor":%q,"name":%q,"value":%s,"threshold":%s}`, m.ID, m.Name,
			strconv.FormatFloat(value, 'f', -1, 64), strconv.FormatFloat(m.Threshold, 'f', -1, 64))
		conn := m.ConnectionID
		if err := r.db.CreateAuditLog(database.AuditLogParams{Action: action, ConnectionID: &conn, Details: &details}); err != nil {
			slog.Warn("Telemetry monitors: audit failed", "monitor", m.ID, "error", err)
		}
	}
	return value, firing, nil
}

func (r *MonitorRunner) emit(m *database.TelemetryMonitor, value float64, now time.Time) {
	title := fmt.Sprintf("%s: %s %s %s", m.Name, strconv.FormatFloat(value, 'f', -1, 64), comparatorWord(m.Comparator), strconv.FormatFloat(m.Threshold, 'f', -1, 64))
	message := fmt.Sprintf("%s matched %s %s rows in the %s ending %s ago (threshold %s %s).",
		m.Name, strconv.FormatFloat(value, 'f', -1, 64), m.Kind, (time.Duration(m.WindowSeconds) * time.Second).String(),
		EvaluationDelay.String(), comparatorWord(m.Comparator), strconv.FormatFloat(m.Threshold, 'f', -1, 64))
	payload := map[string]interface{}{
		"monitor_id": m.ID, "name": m.Name, "kind": m.Kind, "source_id": m.SourceID, "query": m.Query,
		"window_seconds": m.WindowSeconds, "value": value, "threshold": m.Threshold, "comparator": m.Comparator,
	}
	conn := m.ConnectionID
	if _, err := r.db.CreateAlertEvent(&conn, eventTypeTelemetryMonitor, m.Severity, title, message, payload, FingerprintBucket(m, now), m.ID); err != nil {
		slog.Warn("Telemetry monitors: failed to create alert event", "monitor", m.ID, "error", err)
	}
}

// emitError raises an alert event for a monitor that could not evaluate:
// a broken query or a lost source otherwise looks exactly like a quiet
// "ok" until someone opens the page. Same event type and source ref as a
// firing, its own fingerprint so the two never de-duplicate each other.
func (r *MonitorRunner) emitError(m *database.TelemetryMonitor, msg string, now time.Time) {
	title := fmt.Sprintf("%s: evaluation failed", m.Name)
	message := fmt.Sprintf("%s could not be evaluated: %s", m.Name, msg)
	payload := map[string]interface{}{
		"monitor_id": m.ID, "name": m.Name, "kind": m.Kind, "source_id": m.SourceID, "query": m.Query, "error": msg,
	}
	conn := m.ConnectionID
	fp := "telemetry.monitor.error:" + strings.TrimPrefix(FingerprintBucket(m, now), "telemetry.monitor:")
	if _, err := r.db.CreateAlertEvent(&conn, eventTypeTelemetryMonitor, severityWarn, title, message, payload, fp, m.ID); err != nil {
		slog.Warn("Telemetry monitors: failed to create error alert event", "monitor", m.ID, "error", err)
	}
}

func comparatorWord(c string) string {
	switch c {
	case "gt":
		return ">"
	case "gte":
		return ">="
	case "lt":
		return "<"
	case "lte":
		return "<="
	}
	return c
}

func (r *MonitorRunner) describe(connectionID string, c Credentials, src *telemetry.Source) (map[string]string, error) {
	if src.Kind == telemetry.KindMetrics || !telemetry.ValidIdent(src.Database) || !telemetry.ValidIdent(src.Table) {
		return nil, fmt.Errorf("monitors support logs and traces sources")
	}
	res, err := r.gateway.ExecuteQuery(connectionID, fmt.Sprintf("DESCRIBE TABLE %s.%s", telemetry.Quote(src.Database), telemetry.Quote(src.Table)), c.User, c.Password, monitorTimeout)
	if err != nil {
		return nil, fmt.Errorf("describe %s.%s: %w", src.Database, src.Table, err)
	}
	var rows []map[string]interface{}
	if res != nil && len(res.Data) > 0 {
		if err := json.Unmarshal(res.Data, &rows); err != nil {
			return nil, fmt.Errorf("parse describe: %w", err)
		}
	}
	cols := map[string]string{}
	for _, row := range rows {
		name, _ := row["name"].(string)
		typ, _ := row["type"].(string)
		if name != "" {
			cols[name] = typ
		}
	}
	if len(cols) == 0 {
		return nil, fmt.Errorf("table %s.%s has no columns or does not exist", src.Database, src.Table)
	}
	return cols, nil
}

// firstCount reads the single count() cell of a result.
func firstCount(res *tunnel.QueryResult) (float64, error) {
	if res == nil || len(res.Data) == 0 {
		return 0, nil
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(res.Data, &rows); err != nil {
		return 0, fmt.Errorf("parse count: %w", err)
	}
	if len(rows) == 0 {
		return 0, nil
	}
	switch v := rows[0]["c"].(type) {
	case float64:
		return v, nil
	case string:
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return 0, fmt.Errorf("parse count %q", v)
		}
		return f, nil
	case json.Number:
		f, _ := v.Float64()
		return f, nil
	default:
		return 0, fmt.Errorf("unexpected count value %v", rows[0]["c"])
	}
}
