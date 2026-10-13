package database

import "testing"

// TestMigrateAlertsV2Idempotent runs the alerting migration again on a
// migrated database, and on one that predates the free_bytes_floor column,
// keeping the stored rows.
func TestMigrateAlertsV2Idempotent(t *testing.T) {
	db := openTestDB(t)
	connID, err := db.CreateConnection(CreateConnectionParams{Name: "c", TunnelToken: "tok"})
	if err != nil {
		t.Fatal(err)
	}
	crit := 90.0
	if err := db.SaveHealthAlertSettings(connID, []HealthAlertSetting{{CheckKind: "disk", Enabled: true, CritThreshold: &crit, ForEvaluations: 2, ResolveEvaluations: 2}}); err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateSQLAlert(&SQLAlert{ConnectionID: connID, Name: "a", SQL: "SELECT 1", Mode: "value", Comparator: "gt", Severity: "warn", IntervalSeconds: 300, TimeoutSeconds: 30, ForEvaluations: 1, ResolveEvaluations: 1, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := db.migrateAlertsV2(); err != nil {
			t.Fatalf("run %d: %v", i, err)
		}
	}
	if err := db.runMigrations(); err != nil {
		t.Fatalf("full rerun: %v", err)
	}

	// An install from before the column existed gains it on the next start.
	mustExec(t, db, `ALTER TABLE health_alert_settings DROP COLUMN free_bytes_floor`)
	if has, _ := db.columnExists("health_alert_settings", "free_bytes_floor"); has {
		t.Fatal("column not dropped")
	}
	if err := db.migrateAlertsV2(); err != nil {
		t.Fatal(err)
	}
	if has, err := db.columnExists("health_alert_settings", "free_bytes_floor"); err != nil || !has {
		t.Fatalf("free_bytes_floor missing after migration: %v %v", has, err)
	}

	settings, err := db.ListHealthAlertSettings(connID)
	if err != nil || settings["disk"].CritThreshold == nil || *settings["disk"].CritThreshold != 90 {
		t.Fatalf("stored setting lost: %+v %v", settings, err)
	}
	if a, err := db.GetSQLAlert(id); err != nil || a == nil || a.Name != "a" {
		t.Fatalf("stored alert lost: %+v %v", a, err)
	}
	if err := db.InsertAlertEvaluations([]AlertEvaluation{{Source: "sql", SourceID: id, ConnectionID: connID, State: "ok"}}); err != nil {
		t.Fatal(err)
	}
	if n, err := db.PruneAlertEvaluations("9999-01-01T00:00:00Z"); err != nil || n != 1 {
		t.Fatalf("prune: %d %v", n, err)
	}

	// Deleting the connection removes its alerting rows.
	if err := db.DeleteConnection(connID); err != nil {
		t.Fatal(err)
	}
	for _, table := range []string{"health_alert_settings", "sql_alerts"} {
		if n := countRows(t, db, table); n != 0 {
			t.Errorf("%s keeps %d rows after the connection is deleted", table, n)
		}
	}
}
