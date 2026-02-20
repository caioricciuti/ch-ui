package database

import (
	"path/filepath"
	"testing"
)

func openTestDB(t *testing.T) *DB {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := Open(dbPath)
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

func insertAuditLogAt(t *testing.T, db *DB, action, username, details, ip, createdAtExpr string) {
	t.Helper()
	_, err := db.conn.Exec(
		`INSERT INTO audit_logs (id, action, username, connection_id, details, ip_address, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, `+createdAtExpr+`)`,
		action+"-id-"+username, action, username, "conn-1", details, ip,
	)
	if err != nil {
		t.Fatalf("insert audit log: %v", err)
	}
}

func TestGetAuditLogsFiltered_TimeRangeActionUsernameSearch(t *testing.T) {
	db := openTestDB(t)

	insertAuditLogAt(t, db, "connection.created", "alice", "Created warehouse connection", "10.0.0.10", "datetime('now', '-5 minutes')")
	insertAuditLogAt(t, db, "connection.deleted", "bob", "Deleted old tunnel", "10.0.0.20", "datetime('now', '-2 hours')")
	insertAuditLogAt(t, db, "user.login", "alice", "Login success", "10.0.0.11", "datetime('now', '-10 minutes')")

	rows, err := db.GetAuditLogsFiltered(100, "1h", "connection.created", "alice", "warehouse")
	if err != nil {
		t.Fatalf("GetAuditLogsFiltered: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("expected 1 row, got %d", len(rows))
	}
	if rows[0].Action != "connection.created" {
		t.Fatalf("unexpected action: %s", rows[0].Action)
	}
	if rows[0].Username == nil || *rows[0].Username != "alice" {
		t.Fatalf("unexpected username: %+v", rows[0].Username)
	}
}

func TestGetAuditLogsFiltered_SearchMatchesMultipleFieldsCaseInsensitive(t *testing.T) {
	db := openTestDB(t)

	insertAuditLogAt(t, db, "connection.created", "alice", "Created connection for ETL", "10.0.0.10", "datetime('now', '-5 minutes')")
	insertAuditLogAt(t, db, "user.login", "charlie", "Login success", "10.0.0.21", "datetime('now', '-4 minutes')")

	rows, err := db.GetAuditLogsFiltered(100, "", "", "", "etl")
	if err != nil {
		t.Fatalf("GetAuditLogsFiltered search details: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "connection.created" {
		t.Fatalf("expected details match on connection.created, got %+v", rows)
	}

	rows, err = db.GetAuditLogsFiltered(100, "", "", "", "CHARLIE")
	if err != nil {
		t.Fatalf("GetAuditLogsFiltered search username: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "user.login" {
		t.Fatalf("expected username match on user.login, got %+v", rows)
	}

	rows, err = db.GetAuditLogsFiltered(100, "", "", "", "10.0.0.10")
	if err != nil {
		t.Fatalf("GetAuditLogsFiltered search ip: %v", err)
	}
	if len(rows) != 1 || rows[0].Action != "connection.created" {
		t.Fatalf("expected ip match on connection.created, got %+v", rows)
	}
}
