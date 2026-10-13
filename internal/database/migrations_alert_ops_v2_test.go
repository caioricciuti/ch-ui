package database

import (
	"testing"
	"time"
)

// TestMigrateAlertOpsV2Idempotent runs the migration again on a migrated
// database and on one that predates its columns, keeping stored rows, and
// checks the open-ack index and the silence cascade.
func TestMigrateAlertOpsV2Idempotent(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	silenceID, err := db.CreateAlertSilence(AlertSilence{Reason: "upgrade", ConnectionID: &connID,
		StartsAt: now.Format(time.RFC3339), EndsAt: now.Add(time.Hour).Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	ruleID, err := db.CreateAlertRule("r", "*", "info", true, 0, 3, "", "", "admin")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetAlertRuleConnections(ruleID, []string{connID}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.migrateAlertOpsV2(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if err := db.runMigrations(); err != nil {
		t.Fatalf("full rerun: %v", err)
	}

	// An install from before the columns existed gains them on the next start.
	mustExec(t, db, `ALTER TABLE alert_events DROP COLUMN suppressed_by`)
	if err := db.migrateAlertOpsV2(); err != nil {
		t.Fatal(err)
	}
	if has, err := db.columnExists("alert_events", "suppressed_by"); err != nil || !has {
		t.Fatalf("suppressed_by missing after migration: %v %v", has, err)
	}

	rule, err := db.GetAlertRuleByID(ruleID)
	if err != nil || rule == nil || len(rule.ConnectionIDs) != 1 || rule.ConnectionIDs[0] != connID {
		t.Fatalf("rule scope lost: %+v %v", rule, err)
	}
	if err := db.SetAlertRuleConnections(ruleID, nil); err != nil {
		t.Fatal(err)
	}
	if rule, _ := db.GetAlertRuleByID(ruleID); rule.ConnectionIDs != nil {
		t.Fatalf("clearing the scope must store NULL: %+v", rule.ConnectionIDs)
	}
	if s, err := db.GetAlertSilence(silenceID); err != nil || s == nil || s.Status != SilenceActive {
		t.Fatalf("stored silence lost: %+v %v", s, err)
	}

	// One open ack per alert key; a new ack replaces the open one.
	first, err := db.CreateAlertAck(AlertAck{AlertKey: "sql:a"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.conn.Exec(`INSERT INTO alert_acks (id, alert_key, created_at) VALUES ('dup', 'sql:a', ?)`, now.Format(time.RFC3339)); err == nil {
		t.Fatal("a second open ack for one alert key was accepted")
	}
	if _, err := db.CreateAlertAck(AlertAck{AlertKey: "sql:a"}); err != nil {
		t.Fatal(err)
	}
	if a, _ := db.GetAlertAck(first.ID); a.ClearedReason == nil || *a.ClearedReason != "replaced" {
		t.Fatalf("first ack not replaced: %+v", a)
	}

	// Deleting the connection removes its silences.
	if err := db.DeleteConnection(connID); err != nil {
		t.Fatal(err)
	}
	if n := countRows(t, db, "alert_silences"); n != 0 {
		t.Errorf("alert_silences keeps %d rows after the connection is deleted", n)
	}
}
