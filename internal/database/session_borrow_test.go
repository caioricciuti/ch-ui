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

// An SSO session names the person in the borrow audit row; the shared
// ClickHouse account goes in ch_user.
func TestBorrowSessionCredentials_AuditNamesSSOPerson(t *testing.T) {
	db := openTestDB(t)
	const secret = "test-secret"

	connID, err := db.CreateConnection(CreateConnectionParams{
		Name:        "borrow-sso",
		TunnelToken: "cht_borrow_sso",
		Type:        ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("CreateConnection: %v", err)
	}
	enc, err := crypto.Encrypt("svc-pass", secret)
	if err != nil {
		t.Fatalf("Encrypt: %v", err)
	}
	if _, err := db.CreateSession(CreateSessionParams{
		ConnectionID:      connID,
		ClickhouseUser:    "svc_sso",
		EncryptedPassword: enc,
		Token:             "tok-borrow-sso",
		ExpiresAt:         time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
		AuthSubject:       "alice@example.com",
	}); err != nil {
		t.Fatalf("CreateSession: %v", err)
	}

	sessions, err := db.GetActiveSessionsByConnection(connID, 3)
	if err != nil {
		t.Fatalf("GetActiveSessionsByConnection: %v", err)
	}
	if len(sessions) != 1 || sessions[0].AuthSubject == nil || *sessions[0].AuthSubject != "alice@example.com" {
		t.Fatalf("auth_subject not loaded: %+v", sessions)
	}

	user, _, err := db.BorrowSessionCredentials(connID, "pipeline", secret)
	if err != nil {
		t.Fatalf("BorrowSessionCredentials: %v", err)
	}
	if user != "svc_sso" {
		t.Fatalf("borrowed user %q, want the ClickHouse account svc_sso", user)
	}

	logs, err := db.GetAuditLogs(100)
	if err != nil {
		t.Fatalf("GetAuditLogs: %v", err)
	}
	var found bool
	for _, l := range logs {
		if l.Action != "pipeline.credential_borrow" || l.ConnectionID == nil || *l.ConnectionID != connID {
			continue
		}
		found = true
		if l.Username == nil || *l.Username != "alice@example.com" {
			t.Errorf("audit username = %v, want alice@example.com", l.Username)
		}
		if l.ChUser == nil || *l.ChUser != "svc_sso" {
			t.Errorf("audit ch_user = %v, want svc_sso", l.ChUser)
		}
	}
	if !found {
		t.Fatal("no pipeline.credential_borrow audit row")
	}
}
