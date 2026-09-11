package database

import (
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// TestBorrowSessionCredentials covers the credential borrow shared by the
// background workers: it returns the newest session that decrypts, audits the
// borrow as <worker>.credential_borrow, and writes at most one audit row per
// worker and connection per hour.
func TestBorrowSessionCredentials(t *testing.T) {
	db := openTestDB(t)
	const secret = "test-secret"

	connID, err := db.CreateConnection(CreateConnectionParams{
		Name:        "borrow-test",
		TunnelToken: "cht_borrow_test",
		Type:        ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}

	if _, _, err := db.BorrowSessionCredentials(connID, "schedule", secret); err == nil {
		t.Fatal("no sessions: want an error")
	}

	enc, err := crypto.Encrypt("s3cret", secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := db.CreateSession(CreateSessionParams{
		ConnectionID:      connID,
		ClickhouseUser:    "analyst",
		EncryptedPassword: enc,
		Token:             "tok-borrow-1",
		ExpiresAt:         time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	if _, _, err := db.BorrowSessionCredentials(connID, "schedule", "wrong-secret"); err == nil {
		t.Fatal("undecryptable session: want an error")
	}

	user, password, err := db.BorrowSessionCredentials(connID, "schedule", secret)
	if err != nil {
		t.Fatalf("BorrowSessionCredentials: %v", err)
	}
	if user != "analyst" || password != "s3cret" {
		t.Fatalf("got %q / %q, want analyst / s3cret", user, password)
	}

	// Same worker again within the hour: no second row. Another worker: its own row.
	if _, _, err := db.BorrowSessionCredentials(connID, "schedule", secret); err != nil {
		t.Fatalf("second borrow: %v", err)
	}
	if _, _, err := db.BorrowSessionCredentials(connID, "telemetry.monitor", secret); err != nil {
		t.Fatalf("monitor borrow: %v", err)
	}

	logs, err := db.GetAuditLogs(100)
	if err != nil {
		t.Fatalf("GetAuditLogs: %v", err)
	}
	counts := map[string]int{}
	for _, l := range logs {
		counts[l.Action]++
	}
	if counts["schedule.credential_borrow"] != 1 {
		t.Errorf("schedule.credential_borrow rows = %d, want 1", counts["schedule.credential_borrow"])
	}
	if counts["telemetry.monitor.credential_borrow"] != 1 {
		t.Errorf("telemetry.monitor.credential_borrow rows = %d, want 1", counts["telemetry.monitor.credential_borrow"])
	}
}
