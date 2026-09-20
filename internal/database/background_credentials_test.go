package database

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

func TestBackgroundCredentialLifecycle(t *testing.T) {
	db := openTestDB(t)
	const secret = "background-test-secret"
	conn, err := db.CreateConnection(CreateConnectionParams{Name: "jobs", TunnelToken: "jobs-token"})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := crypto.Encrypt("first-password", secret)
	if err != nil {
		t.Fatal(err)
	}
	for _, worker := range BackgroundWorkers() {
		t.Run(worker, func(t *testing.T) {
			if _, _, err := db.BackgroundCredentials(conn, worker, secret); err == nil {
				t.Fatal("missing account and session must fail")
			}
			c := BackgroundCredential{Worker: worker, Mode: "service_account", Username: "job-reader", EncryptedPassword: enc}
			if err := db.SetBackgroundCredential(conn, c); err != nil {
				t.Fatal(err)
			}
			u, p, err := db.BackgroundCredentials(conn, worker, secret)
			if err != nil || u != "job-reader" || p != "first-password" {
				t.Fatalf("account without sessions: user=%s err=%v", u, err)
			}
			stored, err := db.GetBackgroundCredential(conn, worker)
			if err != nil {
				t.Fatal(err)
			}
			body, _ := json.Marshal(stored)
			if strings.Contains(string(body), "password") || strings.Contains(string(body), enc) {
				t.Fatal("password serialized")
			}
			rotated, err := crypto.Encrypt("rotated-password", secret)
			if err != nil {
				t.Fatal(err)
			}
			c.EncryptedPassword = rotated
			if err := db.SetBackgroundCredential(conn, c); err != nil {
				t.Fatal(err)
			}
			_, p, err = db.BackgroundCredentials(conn, worker, secret)
			if err != nil || p != "rotated-password" {
				t.Fatalf("rotation not applied: %v", err)
			}
		})
	}
	// Even with a usable session, corrupt service credentials and explicit
	// revocation must not switch the worker to that person's permissions.
	_, err = db.CreateSession(CreateSessionParams{ConnectionID: conn, ClickhouseUser: "human", EncryptedPassword: enc,
		Token: "human-token", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, BackgroundCredential{Worker: "schedule", Mode: "service_account", Username: "broken", EncryptedPassword: "corrupt"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.BackgroundCredentials(conn, "schedule", secret); err == nil {
		t.Fatal("corrupt account fell back to session")
	}
	if err := db.SetBackgroundCredential(conn, BackgroundCredential{Worker: "schedule", Mode: "disabled"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.BackgroundCredentials(conn, "schedule", secret); err == nil {
		t.Fatal("disabled account fell back to session")
	}
	c, _ := db.GetBackgroundCredential(conn, "schedule")
	if c.Username != "" || c.EncryptedPassword != "" {
		t.Fatal("revocation retained credentials")
	}
	if err := db.SetBackgroundCredential(conn, BackgroundCredential{Worker: "schedule", Mode: "session"}); err != nil {
		t.Fatal(err)
	}
	u, _, err := db.BackgroundCredentials(conn, "schedule", secret)
	if err != nil || u != "human" {
		t.Fatalf("explicit session mode: %s %v", u, err)
	}
	if err := db.SetBackgroundCredential(conn, BackgroundCredential{Worker: "unknown", Mode: "disabled"}); err == nil {
		t.Fatal("unknown worker accepted")
	}
	if err := db.DeleteConnection(conn); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.Conn().QueryRow("SELECT count(*) FROM background_credentials WHERE connection_id = ?", conn).Scan(&count); err != nil || count != 0 {
		t.Fatalf("cascade: count=%d err=%v", count, err)
	}
}

func TestBackgroundCredentialAuditConcurrentAndRotation(t *testing.T) {
	db := openTestDB(t)
	conn, err := db.CreateConnection(CreateConnectionParams{Name: "audit", TunnelToken: "audit-token"})
	if err != nil {
		t.Fatal(err)
	}
	enc, _ := crypto.Encrypt("password", "secret")
	c := BackgroundCredential{Worker: "telemetry.monitor", Mode: "service_account", Username: "monitor-reader", EncryptedPassword: enc}
	if err := db.SetBackgroundCredential(conn, c); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 12 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, _, err := db.BackgroundCredentials(conn, c.Worker, "secret"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	logs, err := db.GetAuditLogs(100)
	if err != nil {
		t.Fatal(err)
	}
	if len(logs) != 1 || logs[0].Action != "telemetry.monitor.credential_use" {
		t.Fatalf("expected one account use audit, got %d", len(logs))
	}
	if err := db.SetBackgroundCredential(conn, c); err != nil {
		t.Fatal(err)
	}
	if _, _, err := db.BackgroundCredentials(conn, c.Worker, "secret"); err != nil {
		t.Fatal(err)
	}
	logs, _ = db.GetAuditLogs(100)
	if len(logs) != 2 {
		t.Fatalf("rotation must be audited immediately, got %d rows", len(logs))
	}
}
