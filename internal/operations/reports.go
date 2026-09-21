// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti. See LICENSE.BSL.

// Package operations creates persisted weekly reports and delivers configured
// email copies independently of generation, so delivery retries reuse a snapshot.
package operations

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/mail"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/alerts"
	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/performance"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

type Storage struct {
	Nodes         []string `json:"nodes"`
	Bytes         float64  `json:"bytes"`
	PreviousBytes *float64 `json:"previous_bytes,omitempty"`
	PreviousAt    string   `json:"previous_at,omitempty"`
	ChangeBytes   *float64 `json:"change_bytes,omitempty"`
}

type Payload struct {
	CollectionUser         string                `json:"collection_user"`
	ConnectionName         string                `json:"connection_name"`
	Start                  string                `json:"start"`
	End                    string                `json:"end"`
	Coverage               string                `json:"coverage"`
	Compared               int                   `json:"compared"`
	Insufficient           int                   `json:"insufficient"`
	RegressionCount        int                   `json:"regression_count"`
	Regressions            []performance.Pattern `json:"regressions"`
	Workloads              []performance.Pattern `json:"workloads"`
	Failures               int64                 `json:"failures"`
	ResolvedInvestigations int                   `json:"resolved_investigations"`
	Storage                *Storage              `json:"storage,omitempty"`
	Warnings               []string              `json:"warnings"`
}

func ValidateSettings(s database.OperationsReportSettings) error {
	if s.Weekday < 0 || s.Weekday > 6 || s.Hour < 0 || s.Hour > 23 {
		return fmt.Errorf("choose a weekday and UTC hour from 0 to 23")
	}
	if len(s.Recipients) > 20 {
		return fmt.Errorf("at most 20 recipients are supported")
	}
	for _, address := range s.Recipients {
		parsed, err := mail.ParseAddress(address)
		if err != nil || parsed.Address != address || strings.ContainsAny(address, "\r\n") || len(address) > 254 {
			return fmt.Errorf("recipients must be plain email addresses")
		}
	}
	if (s.ChannelID != "") != (len(s.Recipients) > 0) {
		return fmt.Errorf("choose both an email channel and recipients, or leave both empty for in-app reports")
	}
	return nil
}

// NextWeekly is strictly after the supplied instant; all schedules use UTC.
func NextWeekly(now time.Time, weekday, hour int) time.Time {
	now = now.UTC()
	days := (weekday - int(now.Weekday()) + 7) % 7
	next := time.Date(now.Year(), now.Month(), now.Day(), hour, 0, 0, 0, time.UTC).AddDate(0, 0, days)
	if !next.After(now) {
		next = next.AddDate(0, 0, 7)
	}
	return next
}

type Runner struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	mu      sync.Mutex
	cancel  context.CancelFunc
	done    chan struct{}
	isPro   func() bool
	// Send can be replaced in tests. Production uses the existing email channels.
	Send func(context.Context, string, map[string]interface{}, []string, string, string) (string, error)
}

func NewRunner(db *database.DB, gw *tunnel.Gateway, cfg *config.Config) *Runner {
	return &Runner{DB: db, Gateway: gw, Config: cfg, Send: alerts.SendDirect, isPro: cfg.IsPro}
}

func (r *Runner) Start() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cancel != nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	r.cancel = cancel
	done := make(chan struct{})
	r.done = done
	go func() {
		defer close(done)
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		r.Tick(ctx, time.Now())
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				r.Tick(ctx, now)
			}
		}
	}()
}

func (r *Runner) Stop() {
	r.mu.Lock()
	cancel, done := r.cancel, r.done
	r.mu.Unlock()
	if cancel != nil {
		cancel()
		<-done
		r.mu.Lock()
		if r.done == done {
			r.cancel = nil
			r.done = nil
		}
		r.mu.Unlock()
	}
}

func (r *Runner) Tick(ctx context.Context, now time.Time) {
	if !r.isPro() {
		return
	}
	connections, err := r.DB.GetConnections()
	if err != nil {
		slog.Warn("Reports: list connections", "error", err)
		return
	}
	for _, conn := range connections {
		if ctx.Err() != nil || !r.isPro() {
			return
		}
		s, err := r.DB.GetOperationsReportSettings(conn.ID)
		if err != nil || !s.Enabled {
			continue
		}
		due, err := time.Parse(time.RFC3339, s.NextRunAt)
		if err != nil || due.After(now) {
			continue
		}
		if last, err := time.Parse(time.RFC3339, s.LastAttemptAt); err == nil && now.Sub(last) < 15*time.Minute {
			continue
		}
		existing, err := r.DB.OperationsReportForSchedule(conn.ID, s.NextRunAt)
		if err == nil && existing == nil {
			var account database.BackgroundCredential
			account, err = r.DB.GetBackgroundCredential(conn.ID, "operations.report")
			if err == nil && account.Mode != "service_account" {
				err = fmt.Errorf("configure a dedicated Weekly operations reports account in Admin → Connections")
			}
			if err == nil {
				var user, password string
				user, password, err = r.DB.BackgroundCredentials(conn.ID, "operations.report", r.Config.AppSecretKey)
				if err == nil {
					_, err = r.Generate(ctx, conn.ID, user, password, "weekly schedule", s.NextRunAt, now, s)
				}
			}
		}
		next, message := s.NextRunAt, ""
		if err != nil {
			message = "Report generation failed; check the background account, connection and system-table permissions."
			slog.Warn("Weekly report generation failed", "connection", conn.ID, "error", err)
		} else {
			next = NextWeekly(now, s.Weekday, s.Hour).Format(time.RFC3339)
		}
		if err := r.DB.OperationsReportAttempt(conn.ID, s.Revision, next, message, now); err != nil {
			slog.Warn("Reports: update schedule", "error", err)
		}
	}
	r.DeliverDue(ctx, now)
}

func (r *Runner) Generate(ctx context.Context, conn, user, password, actor, scheduleKey string, now time.Time, s database.OperationsReportSettings) (*database.OperationsReport, error) {
	if !r.isPro() {
		return nil, fmt.Errorf("an active Pro license is required")
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	c, err := r.DB.GetConnectionByID(conn)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, fmt.Errorf("connection not found")
	}
	exec := func(sql string) ([]map[string]interface{}, error) {
		if !r.isPro() {
			return nil, fmt.Errorf("an active Pro license is required")
		}
		result, err := r.Gateway.ExecuteQueryWithSettingsCtx(ctx, conn, sql, user, password, map[string]string{
			"readonly": "1", "max_execution_time": "25", "max_memory_usage": "536870912", "max_threads": "2", "max_result_rows": "1001", "result_overflow_mode": "throw", "log_comment": "ch-ui:operations-report",
		}, 30*time.Second)
		if err != nil {
			return nil, err
		}
		if result == nil {
			return nil, fmt.Errorf("empty query response")
		}
		var rows []map[string]interface{}
		if err = json.Unmarshal(result.Data, &rows); err != nil {
			return nil, err
		}
		return rows, nil
	}
	cluster := ""
	warnings := []string{}
	if rows, e := exec(clusterhealth.ResolveClusterQuery); e == nil && len(rows) > 0 {
		if value, ok := rows[0]["cluster"].(string); ok && clusterhealth.IsValidClusterName(value) {
			cluster = value
		}
	} else if e != nil {
		warnings = append(warnings, "Cluster discovery unavailable; this report covers the connected node only.")
	}
	metrics, err := performance.Collect(exec, cluster, now, "7d")
	if err != nil && cluster != "" {
		// Discovery does not imply permission to read remote nodes. Keep the
		// whole report within one explicitly labeled scope when falling back.
		cluster = ""
		warnings = append(warnings, "Cluster-wide query logs unavailable; this report covers the connected node only. Check remote-read grants and node availability for cluster coverage.")
		metrics, err = performance.Collect(exec, cluster, now, "7d")
	}
	if err != nil {
		return nil, fmt.Errorf("query-log analysis: %w", err)
	}
	p := BuildPayload(c.Name, metrics)
	p.CollectionUser = user
	p.Warnings = append(p.Warnings, warnings...)
	source := "system.parts"
	if cluster != "" {
		source = fmt.Sprintf("clusterAllReplicas('%s', system.parts)", cluster)
	}
	rows, storageErr := exec("SELECT toString(sum(bytes_on_disk)) AS bytes, arraySort(groupUniqArray(hostName())) AS nodes FROM " + source + " WHERE active FORMAT JSON")
	if storageErr != nil {
		p.Warnings = append(p.Warnings, "Storage unavailable; grant SELECT on system.parts to include physical storage growth.")
	} else if len(rows) > 0 {
		value, e := strconv.ParseFloat(fmt.Sprint(rows[0]["bytes"]), 64)
		if e == nil && value >= 0 {
			p.Storage = &Storage{Bytes: value}
			if nodes, ok := rows[0]["nodes"].([]interface{}); ok {
				for _, node := range nodes {
					if name, ok := node.(string); ok {
						p.Storage.Nodes = append(p.Storage.Nodes, name)
					}
				}
			}
			previous, e := r.DB.ListOperationsReports(conn, 1)
			if e == nil && len(previous) > 0 {
				var old Payload
				if json.Unmarshal(previous[0].Payload, &old) == nil && old.Storage != nil && old.Coverage == p.Coverage && old.CollectionUser == p.CollectionUser && len(p.Storage.Nodes) > 0 && strings.Join(old.Storage.Nodes, "\x00") == strings.Join(p.Storage.Nodes, "\x00") {
					delta := value - old.Storage.Bytes
					p.Storage.PreviousBytes = &old.Storage.Bytes
					p.Storage.ChangeBytes = &delta
					p.Storage.PreviousAt = previous[0].CreatedAt
				}
			}
		}
	}
	// Investigations are connection scoped; count recorded resolutions, not
	// inferred improvements. This table is installed with the same migration.
	if e := r.DB.Conn().QueryRow(`SELECT count(*) FROM performance_investigations WHERE connection_id=? AND status='resolved' AND julianday(resolved_at)>=julianday(?) AND julianday(resolved_at)<julianday(?)`, conn, p.Start, p.End).Scan(&p.ResolvedInvestigations); e != nil {
		p.Warnings = append(p.Warnings, "Resolved investigation count unavailable.")
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	report := database.OperationsReport{ConnectionID: conn, CreatedAt: now.UTC().Format(time.RFC3339), CreatedBy: actor, Payload: raw, Body: Render(p)}
	if scheduleKey != "" && s.ChannelID != "" {
		report.ScheduledDelivery = true
		report.ScheduleRevision = s.Revision
		recipients, _ := json.Marshal(s.Recipients)
		report.ChannelID = s.ChannelID
		report.RecipientsJSON = string(recipients)
		report.DeliveryStatus = "queued"
	}
	return r.DB.SaveOperationsReport(report, scheduleKey)
}

func BuildPayload(name string, report performance.Report) Payload {
	p := Payload{ConnectionName: name, Start: report.Current.Start.Format(time.RFC3339), End: report.Current.End.Format(time.RFC3339), Coverage: report.Coverage,
		Compared: report.Compared, Insufficient: report.Insufficient, RegressionCount: len(report.Regressions), Regressions: []performance.Pattern{}, Workloads: []performance.Pattern{}, Warnings: []string{}}
	if report.Truncated {
		p.Warnings = append(p.Warnings, "Query pattern limit reached; counts and rankings cover returned patterns only.")
	}
	p.Warnings = append(p.Warnings, "Resource measurements cover logged initial queries, not an invoice. Active-part storage includes replicas, excludes inactive/detached parts, and may double-count shared storage. Growth compares the last report from the same account and coverage; changes to that account's grants may affect visibility.")
	for _, pattern := range report.Patterns {
		p.Failures += pattern.Current.Failures
	}
	for i, pattern := range report.Regressions {
		if i == 20 {
			break
		}
		p.Regressions = append(p.Regressions, pattern)
	}
	patterns := append([]performance.Pattern(nil), report.Patterns...)
	sort.SliceStable(patterns, func(i, j int) bool {
		return patterns[i].Current.MeanCPUMS*float64(patterns[i].Current.Runs) > patterns[j].Current.MeanCPUMS*float64(patterns[j].Current.Runs)
	})
	for i, pattern := range patterns {
		if i == 10 {
			break
		}
		p.Workloads = append(p.Workloads, pattern)
	}
	return p
}

func Render(p Payload) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Weekly operations report · %s\n%s → %s (UTC)\n\n%d regressed query patterns; %d patterns compared; %d with insufficient observations.\n%d failed query executions among observed patterns.\n%d investigations marked resolved.\n\n", p.ConnectionName, p.Start, p.End, p.RegressionCount, p.Compared, p.Insufficient, p.Failures, p.ResolvedInvestigations)
	if len(p.Regressions) > 0 {
		b.WriteString("Regressions:\n")
		for _, pattern := range p.Regressions {
			fmt.Fprintf(&b, "• %s / %s: %d recent executions\n", pattern.Database, pattern.Hash, pattern.Current.Runs)
			for _, change := range pattern.Changes {
				if !change.Regressed {
					continue
				}
				label, unit, scale := change.Metric, "ms", float64(1)
				switch change.Metric {
				case "p95_ms":
					label = "p95 latency"
				case "mean_memory_bytes":
					label, unit, scale = "mean memory", "MiB", 1<<20
				case "mean_read_bytes":
					label, unit, scale = "mean read volume", "MiB", 1<<20
				}
				fmt.Fprintf(&b, "  %s: %.1f → %.1f %s\n", label, change.Before/scale, change.After/scale, unit)
			}
		}
	}
	if len(p.Workloads) > 0 {
		b.WriteString("\nHighest measured CPU workloads:\n")
		for _, pattern := range p.Workloads {
			fmt.Fprintf(&b, "• %s / %s: %.1f CPU seconds; %d executions\n", pattern.Database, pattern.Hash, pattern.Current.MeanCPUMS*float64(pattern.Current.Runs)/1000, pattern.Current.Runs)
		}
	}
	if p.Storage != nil {
		fmt.Fprintf(&b, "\nActive-part storage (including replicas): %.2f GiB", p.Storage.Bytes/(1<<30))
		if p.Storage.ChangeBytes != nil {
			fmt.Fprintf(&b, " (%+.2f GiB since %s)", *p.Storage.ChangeBytes/(1<<30), p.Storage.PreviousAt)
		}
		b.WriteString("\n")
		if len(p.Storage.Nodes) > 0 {
			fmt.Fprintf(&b, "Storage nodes: %s\n", strings.Join(p.Storage.Nodes, ", "))
		}
	}
	fmt.Fprintf(&b, "\nQuery-log coverage: %s\n", p.Coverage)
	for _, warning := range p.Warnings {
		fmt.Fprintf(&b, "• %s\n", warning)
	}
	b.WriteString("\nOpen Performance and Reports in your CH-UI instance for the saved findings.\n")
	return b.String()
}

func (r *Runner) DeliverDue(ctx context.Context, now time.Time) {
	if !r.isPro() {
		return
	}
	reports, err := r.DB.DueOperationsReportDeliveries(now)
	if err != nil {
		slog.Warn("Reports: load delivery queue", "error", err)
		return
	}
	for _, report := range reports {
		if ctx.Err() != nil || !r.isPro() {
			return
		}
		claimed, e := r.DB.ClaimOperationsReportDelivery(report.ID, now)
		if e != nil || !claimed {
			continue
		}
		e = r.deliver(ctx, report)
		status, message := "sent", ""
		if e != nil {
			status = "retry"
			message = "Email delivery failed. Check the configured channel; retry is automatic."
			if report.Attempts+1 >= 5 {
				status = "failed"
			}
			slog.Warn("Report email failed", "report", report.ID, "error", e)
		}
		next := now.Add(time.Duration((report.Attempts+1)*5) * time.Minute)
		if e := r.DB.CompleteOperationsReportDelivery(report.ID, status, message, next); e != nil {
			slog.Warn("Reports: save delivery status", "error", e)
		}
	}
}

func (r *Runner) deliver(ctx context.Context, report database.OperationsReport) error {
	channel, err := r.DB.GetAlertChannelByID(report.ChannelID)
	if err != nil {
		return err
	}
	if channel == nil || !channel.IsActive {
		return fmt.Errorf("email channel is missing or disabled")
	}
	raw, err := crypto.Decrypt(channel.ConfigEncrypted, r.Config.AppSecretKey)
	if err != nil {
		return err
	}
	var cfg map[string]interface{}
	if err = json.Unmarshal([]byte(raw), &cfg); err != nil {
		return err
	}
	var recipients []string
	if err = json.Unmarshal([]byte(report.RecipientsJSON), &recipients); err != nil {
		return err
	}
	if err = ValidateSettings(database.OperationsReportSettings{ChannelID: report.ChannelID, Recipients: recipients}); err != nil {
		return err
	}
	if len(recipients) == 0 {
		return fmt.Errorf("no recipients")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	_, err = r.Send(ctx, channel.ChannelType, cfg, recipients, "[CH-UI] Weekly operations report", report.Body)
	return err
}
