package database

import (
	"errors"
	"strings"
	"testing"
)

var errInjected = errors.New("injected failure")

// failAt makes the named rebuild failpoint return errInjected for the rest of
// the test.
func failAt(t *testing.T, point string) {
	t.Helper()
	rebuildFailpoint = func(p string) error {
		if p == point {
			return errInjected
		}
		return nil
	}
	t.Cleanup(func() { rebuildFailpoint = nil })
}

func TestMigrateModelSchedulesAnchorRollsBackOnFailure(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	modelID, err := db.CreateModel(connID, "a_model", "", "default", "view", "SELECT 1", "", "", "admin")
	if err != nil {
		t.Fatalf("CreateModel: %v", err)
	}

	// Put back the pre-anchor schema with one schedule in it.
	mustExec(t, db, `DROP TABLE model_schedules`)
	mustExec(t, db, `CREATE TABLE model_schedules (
		id TEXT PRIMARY KEY,
		connection_id TEXT NOT NULL REFERENCES connections(id) ON DELETE CASCADE,
		cron TEXT NOT NULL,
		enabled INTEGER NOT NULL DEFAULT 1,
		last_run_at TEXT,
		next_run_at TEXT,
		last_status TEXT,
		last_error TEXT,
		created_by TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	mustExec(t, db, `INSERT INTO model_schedules (id, connection_id, cron) VALUES ('sched-1', ?, '0 * * * *')`, connID)

	failAt(t, "model_schedules: copied")
	if err := db.migrateModelSchedulesAnchor(); !errors.Is(err, errInjected) {
		t.Fatalf("migrateModelSchedulesAnchor err = %v, want injected failure", err)
	}

	// Original table, schema and row intact; no half-built leftovers.
	if has, err := db.columnExists("model_schedules", "anchor_model_id"); err != nil || has {
		t.Fatalf("anchor_model_id present after rollback (has=%v, err=%v)", has, err)
	}
	if exists, err := db.tableExists("model_schedules_old"); err != nil || exists {
		t.Fatalf("model_schedules_old left behind (exists=%v, err=%v)", exists, err)
	}
	var cron string
	if err := db.conn.QueryRow(`SELECT cron FROM model_schedules WHERE id = 'sched-1'`).Scan(&cron); err != nil || cron != "0 * * * *" {
		t.Fatalf("original row lost: cron=%q err=%v", cron, err)
	}
	// The write lock was released: the pool can write again.
	mustExec(t, db, `UPDATE model_schedules SET last_status = 'ok' WHERE id = 'sched-1'`)

	// The next start retries and completes.
	rebuildFailpoint = nil
	if err := db.migrateModelSchedulesAnchor(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	var anchor, status string
	if err := db.conn.QueryRow(`SELECT anchor_model_id, last_status FROM model_schedules WHERE id = 'sched-1'`).Scan(&anchor, &status); err != nil {
		t.Fatalf("read migrated row: %v", err)
	}
	if anchor != modelID || status != "ok" {
		t.Fatalf("migrated row: anchor=%q status=%q, want %q ok", anchor, status, modelID)
	}
}

func TestMigrateAlertRoutesRollsBackOnFailure(t *testing.T) {
	db := openTestDB(t)
	channelID, err := db.CreateAlertChannel("legacy-mail", "resend", "enc", true, "admin")
	if err != nil {
		t.Fatalf("CreateAlertChannel: %v", err)
	}
	ruleID, err := db.CreateAlertRule("legacy rule", "schedule.failed", "warn", true, 0, 5, "", "", "admin")
	if err != nil {
		t.Fatalf("CreateAlertRule: %v", err)
	}
	eventID, err := db.CreateAlertEvent(nil, "schedule.failed", "error", "boom", "boom", nil, "fp-legacy", "")
	if err != nil {
		t.Fatalf("CreateAlertEvent: %v", err)
	}

	mustExec(t, db, `CREATE TABLE alert_rule_routes (
		id TEXT PRIMARY KEY,
		rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
		recipients_json TEXT NOT NULL,
		is_active INTEGER DEFAULT 1,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP
	)`)
	mustExec(t, db, `DROP TABLE alert_dispatch_jobs`)
	mustExec(t, db, `CREATE TABLE alert_dispatch_jobs (
		id TEXT PRIMARY KEY,
		event_id TEXT NOT NULL REFERENCES alert_events(id) ON DELETE CASCADE,
		rule_id TEXT NOT NULL REFERENCES alert_rules(id) ON DELETE CASCADE,
		route_id TEXT NOT NULL REFERENCES alert_rule_routes(id) ON DELETE CASCADE,
		channel_id TEXT NOT NULL REFERENCES alert_channels(id) ON DELETE CASCADE,
		status TEXT NOT NULL DEFAULT 'queued',
		attempt_count INTEGER DEFAULT 0,
		max_attempts INTEGER DEFAULT 5,
		next_attempt_at TEXT NOT NULL,
		last_error TEXT,
		provider_message_id TEXT,
		created_at TEXT DEFAULT CURRENT_TIMESTAMP,
		updated_at TEXT DEFAULT CURRENT_TIMESTAMP,
		sent_at TEXT
	)`)
	mustExec(t, db,
		`INSERT INTO alert_rule_routes (id, rule_id, channel_id, recipients_json, is_active)
		 VALUES ('route-1', ?, ?, '["a@example.com"]', 1)`, ruleID, channelID)
	mustExec(t, db,
		`INSERT INTO alert_dispatch_jobs (id, event_id, rule_id, route_id, channel_id, status, next_attempt_at)
		 VALUES ('job-1', ?, ?, 'route-1', ?, 'queued', '2025-01-01T00:00:00Z')`, eventID, ruleID, channelID)

	failAt(t, "alert_dispatch_jobs: copied")
	if err := db.migrateAlertRoutesToRuleChannels(); !errors.Is(err, errInjected) {
		t.Fatalf("migrateAlertRoutesToRuleChannels err = %v, want injected failure", err)
	}

	// Legacy schema and rows intact; step 1 bindings rolled back with the rest.
	if has, err := db.columnExists("alert_dispatch_jobs", "route_id"); err != nil || !has {
		t.Fatalf("alert_dispatch_jobs lost route_id (has=%v, err=%v)", has, err)
	}
	if exists, err := db.tableExists("alert_dispatch_jobs_new"); err != nil || exists {
		t.Fatalf("alert_dispatch_jobs_new left behind (exists=%v, err=%v)", exists, err)
	}
	if got := countRows(t, db, "alert_rule_routes"); got != 1 {
		t.Fatalf("alert_rule_routes rows = %d, want 1", got)
	}
	if got := countRows(t, db, "alert_dispatch_jobs"); got != 1 {
		t.Fatalf("alert_dispatch_jobs rows = %d, want 1", got)
	}
	if got := countRows(t, db, "alert_rule_channels"); got != 0 {
		t.Fatalf("alert_rule_channels rows = %d, want 0 after rollback", got)
	}

	// The next start retries and completes.
	rebuildFailpoint = nil
	if err := db.migrateAlertRoutesToRuleChannels(); err != nil {
		t.Fatalf("retry: %v", err)
	}
	var recipients string
	if err := db.conn.QueryRow(`SELECT recipients_json FROM alert_dispatch_jobs WHERE id = 'job-1'`).Scan(&recipients); err != nil || !strings.Contains(recipients, "a@example.com") {
		t.Fatalf("migrated job recipients = %q, err %v", recipients, err)
	}
	if exists, err := db.tableExists("alert_rule_routes"); err != nil || exists {
		t.Fatalf("legacy alert_rule_routes not dropped (exists=%v, err=%v)", exists, err)
	}
}
