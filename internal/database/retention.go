package database

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"time"
)

// SettingRetentionConfig is the settings key holding the retention config JSON.
const SettingRetentionConfig = "retention_config"

// retentionBatchSize bounds each DELETE so retention never holds the SQLite
// write lock for long stretches (WAL readers are unaffected, but other writers
// share the single-writer lock).
const retentionBatchSize = 5000

// retentionInterval is how often the background retention job runs.
const retentionInterval = 1 * time.Hour

// RetentionConfig holds the retention window, in days, for each covered
// append-only history table. 0 means "keep forever" (pruning disabled).
type RetentionConfig struct {
	AuditLogs         int `json:"audit_logs"`
	AlertEvents       int `json:"alert_events"`
	AlertDispatchJobs int `json:"alert_dispatch_jobs"`
	ScheduleRuns      int `json:"schedule_runs"`
	PipelineRuns      int `json:"pipeline_runs"`
	PipelineRunLogs   int `json:"pipeline_run_logs"`
	ModelRuns         int `json:"model_runs"`
	ModelRunResults   int `json:"model_run_results"`
	GitHubSyncLogs    int `json:"github_sync_logs"`
	GovSchemaChanges  int `json:"gov_schema_changes"`
	OperationsReports int `json:"operations_reports"`
	// IncidentAnnotations prunes deployment annotations by when they occurred.
	IncidentAnnotations int `json:"incident_annotations"`
	// ResolvedInvestigations prunes resolved performance investigations by
	// resolved_at; open and monitoring ones are never pruned.
	ResolvedInvestigations int `json:"resolved_investigations"`
}

// DefaultRetentionConfig returns the built-in retention windows.
func DefaultRetentionConfig() RetentionConfig {
	return RetentionConfig{
		AuditLogs:              90,
		AlertEvents:            60,
		AlertDispatchJobs:      30,
		ScheduleRuns:           60,
		PipelineRuns:           90,
		PipelineRunLogs:        30,
		ModelRuns:              90,
		ModelRunResults:        30,
		GitHubSyncLogs:         30,
		GovSchemaChanges:       180,
		OperationsReports:      180,
		IncidentAnnotations:    180,
		ResolvedInvestigations: 180,
	}
}

// Validate rejects nonsensical retention windows.
func (c RetentionConfig) Validate() error {
	for _, t := range retentionTargets {
		days := t.days(c)
		if days < 0 {
			return fmt.Errorf("retention for %s must be >= 0 days (0 disables pruning)", t.table)
		}
		if days > 3650 {
			return fmt.Errorf("retention for %s must be <= 3650 days", t.table)
		}
	}
	return nil
}

// RetentionStats records the outcome of the most recent retention run.
type RetentionStats struct {
	LastRunAt    string           `json:"last_run_at"`
	DurationMs   int64            `json:"duration_ms"`
	RowsDeleted  map[string]int64 `json:"rows_deleted"`
	TotalDeleted int64            `json:"total_deleted"`
	LastError    string           `json:"last_error,omitempty"`
}

// retentionTarget maps a history table to its timestamp column and the config
// field holding its retention window. Children with ON DELETE CASCADE parents
// (pipeline_run_logs -> pipeline_runs, model_run_results -> model_runs,
// alert_dispatch_jobs -> alert_events) are listed with their own shorter
// windows; cascades from parent pruning are harmless on top of that.
type retentionTarget struct {
	table  string
	column string
	days   func(RetentionConfig) int
	// filter is an extra constant SQL condition limiting which rows may go.
	filter string
}

// retentionTargets is the static list of covered tables. Table and column
// names are compile-time constants (never user input), so building SQL with
// fmt.Sprintf below is safe.
var retentionTargets = []retentionTarget{
	{"audit_logs", "created_at", func(c RetentionConfig) int { return c.AuditLogs }, ""},
	{"alert_dispatch_jobs", "created_at", func(c RetentionConfig) int { return c.AlertDispatchJobs }, ""},
	{"alert_events", "created_at", func(c RetentionConfig) int { return c.AlertEvents }, ""},
	{"schedule_runs", "created_at", func(c RetentionConfig) int { return c.ScheduleRuns }, ""},
	{"pipeline_run_logs", "created_at", func(c RetentionConfig) int { return c.PipelineRunLogs }, ""},
	{"pipeline_runs", "created_at", func(c RetentionConfig) int { return c.PipelineRuns }, ""},
	{"model_run_results", "created_at", func(c RetentionConfig) int { return c.ModelRunResults }, ""},
	{"model_runs", "created_at", func(c RetentionConfig) int { return c.ModelRuns }, ""},
	{"github_sync_logs", "created_at", func(c RetentionConfig) int { return c.GitHubSyncLogs }, ""},
	{"gov_schema_changes", "created_at", func(c RetentionConfig) int { return c.GovSchemaChanges }, ""},
	{"operations_reports", "created_at", func(c RetentionConfig) int { return c.OperationsReports }, ""},
	{"incident_deployment_annotations", "occurred_at", func(c RetentionConfig) int { return c.IncidentAnnotations }, ""},
	// Events cascade with their investigation.
	{"performance_investigations", "resolved_at", func(c RetentionConfig) int { return c.ResolvedInvestigations }, "status = 'resolved'"},
}

// GetRetentionConfig loads the stored retention config, falling back to the
// defaults for any key missing from the stored JSON (or when nothing is stored).
func (db *DB) GetRetentionConfig() (RetentionConfig, error) {
	cfg := DefaultRetentionConfig()
	raw, err := db.GetSetting(SettingRetentionConfig)
	if err != nil {
		return cfg, err
	}
	if raw == "" {
		return cfg, nil
	}
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return DefaultRetentionConfig(), fmt.Errorf("parse retention config: %w", err)
	}
	return cfg, nil
}

// SetRetentionConfig validates and persists the retention config as a single
// JSON blob in the settings table.
func (db *DB) SetRetentionConfig(cfg RetentionConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal retention config: %w", err)
	}
	return db.SetSetting(SettingRetentionConfig, string(data))
}

// RetentionStats returns a snapshot of the most recent retention run. The
// zero value (empty LastRunAt) means retention has not run yet.
func (db *DB) RetentionStats() RetentionStats {
	db.retentionMu.Lock()
	defer db.retentionMu.Unlock()
	stats := db.retentionStats
	// Copy the map so callers cannot race with the next run.
	if stats.RowsDeleted != nil {
		copied := make(map[string]int64, len(stats.RowsDeleted))
		for k, v := range stats.RowsDeleted {
			copied[k] = v
		}
		stats.RowsDeleted = copied
	}
	return stats
}

// StartRetentionJob launches the background goroutine that prunes history
// tables per the configured retention. It runs once immediately, then hourly,
// mirroring StartCleanupJobs.
func (db *DB) StartRetentionJob() {
	slog.Info("Starting data retention job", "interval", retentionInterval)
	go func() {
		db.RunRetention()
		ticker := time.NewTicker(retentionInterval)
		defer ticker.Stop()
		for range ticker.C {
			db.RunRetention()
		}
	}()
}

// RunRetention executes one retention pass: for each covered table it deletes
// rows older than the configured window in small batches, then reclaims freed
// pages via incremental vacuum. It records and returns per-table stats.
func (db *DB) RunRetention() RetentionStats {
	started := time.Now()
	stats := RetentionStats{
		LastRunAt:   started.UTC().Format(time.RFC3339),
		RowsDeleted: make(map[string]int64, len(retentionTargets)),
	}

	cfg, err := db.GetRetentionConfig()
	if err != nil {
		slog.Error("Retention: failed to load config, using defaults", "error", err)
		stats.LastError = err.Error()
	}

	for _, t := range retentionTargets {
		days := t.days(cfg)
		if days <= 0 {
			continue // 0 = keep forever
		}
		// Use the space-separated SQLite datetime format for the cutoff (like
		// governance pruning does): it sorts before the RFC3339 'T' form for
		// the same instant, so mixed-format rows are never deleted early.
		cutoff := started.UTC().AddDate(0, 0, -days).Format("2006-01-02 15:04:05")
		deleted, err := db.pruneTableBefore(t.table, t.column, t.filter, cutoff)
		stats.RowsDeleted[t.table] = deleted
		stats.TotalDeleted += deleted
		if err != nil {
			slog.Error("Retention prune failed", "table", t.table, "error", err)
			stats.LastError = fmt.Sprintf("%s: %v", t.table, err)
			continue
		}
		if deleted > 0 {
			slog.Info("Retention pruned", "table", t.table, "rows", deleted, "older_than", cutoff)
		}
	}

	// Reclaim freed pages a chunk at a time (requires auto_vacuum=INCREMENTAL,
	// which only takes effect on new databases or after a full VACUUM).
	if _, err := db.conn.Exec("PRAGMA incremental_vacuum(2000)"); err != nil {
		slog.Warn("Retention incremental vacuum failed", "error", err)
	}

	stats.DurationMs = time.Since(started).Milliseconds()

	db.retentionMu.Lock()
	db.retentionStats = stats
	db.retentionMu.Unlock()

	return stats
}

// pruneTableBefore deletes rows with column < cutoff (and matching filter,
// when set) in batches of retentionBatchSize, looping until no rows remain,
// so the write lock is released between batches. Rows with an empty column
// are never pruned. Table, column and filter come from retentionTargets only.
func (db *DB) pruneTableBefore(table, column, filter, cutoff string) (int64, error) {
	where := fmt.Sprintf("%s <> '' AND %s < ?", column, column)
	if filter != "" {
		where += " AND " + filter
	}
	query := fmt.Sprintf(
		`DELETE FROM %s WHERE rowid IN (SELECT rowid FROM %s WHERE %s LIMIT %d)`,
		table, table, where, retentionBatchSize,
	)
	var total int64
	for {
		res, err := db.conn.Exec(query, cutoff)
		if err != nil {
			return total, fmt.Errorf("prune %s: %w", table, err)
		}
		n, err := res.RowsAffected()
		if err != nil {
			return total, fmt.Errorf("prune %s rows affected: %w", table, err)
		}
		total += n
		if n < retentionBatchSize {
			return total, nil
		}
	}
}
