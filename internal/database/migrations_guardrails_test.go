package database

import "testing"

func TestGuardrailColumnsExistAfterMigrations(t *testing.T) {
	db := openTestDB(t)

	mustHaveColumn(t, db, "gov_policies", "enforcement_mode")
	mustHaveColumn(t, db, "gov_policy_violations", "detection_phase")
	mustHaveColumn(t, db, "gov_policy_violations", "request_endpoint")
}

func mustHaveColumn(t *testing.T, db *DB, tableName, columnName string) {
	t.Helper()

	rows, err := db.conn.Query("PRAGMA table_info(" + tableName + ")")
	if err != nil {
		t.Fatalf("inspect table %s: %v", tableName, err)
	}
	defer rows.Close()

	found := false
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull, pk int
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			t.Fatalf("scan pragma for %s: %v", tableName, err)
		}
		if name == columnName {
			found = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate pragma for %s: %v", tableName, err)
	}
	if !found {
		t.Fatalf("expected column %s on table %s", columnName, tableName)
	}
}
