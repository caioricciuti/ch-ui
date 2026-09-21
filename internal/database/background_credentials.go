package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// BackgroundWorkers returns the independently configured unattended workers.
func BackgroundWorkers() []string {
	return []string{"schedule", "model", "pipeline", "governance", "cluster_health", "telemetry.monitor", "performance", "operations.report"}
}

func ValidBackgroundWorker(worker string) bool {
	for _, known := range BackgroundWorkers() {
		if worker == known {
			return true
		}
	}
	return false
}

// BackgroundCredential never serializes the stored password, even encrypted.
type BackgroundCredential struct {
	Worker            string `json:"worker"`
	Mode              string `json:"mode"`
	Username          string `json:"username,omitempty"`
	EncryptedPassword string `json:"-"`
	UpdatedAt         string `json:"updated_at,omitempty"`
}

func (db *DB) GetBackgroundCredential(connectionID, worker string) (BackgroundCredential, error) {
	c := BackgroundCredential{Worker: worker, Mode: "session"}
	if !ValidBackgroundWorker(worker) {
		return c, fmt.Errorf("unknown background worker")
	}
	err := db.conn.QueryRow(`SELECT mode, username, encrypted_password, updated_at
		FROM background_credentials WHERE connection_id = ? AND worker = ?`, connectionID, worker).
		Scan(&c.Mode, &c.Username, &c.EncryptedPassword, &c.UpdatedAt)
	if err == sql.ErrNoRows {
		return c, nil
	}
	return c, err
}

// SetBackgroundCredential rotates atomically. Disabled rows erase credentials
// and deliberately prevent session fallback. Session mode is an explicit opt-in.
func (db *DB) SetBackgroundCredential(connectionID string, c BackgroundCredential) error {
	if !ValidBackgroundWorker(c.Worker) {
		return fmt.Errorf("unknown background worker")
	}
	conn, err := db.GetConnectionByID(connectionID)
	if err != nil {
		return err
	}
	if conn == nil {
		return sql.ErrNoRows
	}
	switch c.Mode {
	case "session":
		_, err = db.conn.Exec("DELETE FROM background_credentials WHERE connection_id = ? AND worker = ?", connectionID, c.Worker)
		return err
	case "disabled":
		c.Username, c.EncryptedPassword = "", ""
	case "service_account":
		c.Username = strings.TrimSpace(c.Username)
		if c.Username == "" || c.EncryptedPassword == "" {
			return fmt.Errorf("service account credentials are required")
		}
	default:
		return fmt.Errorf("mode must be session, service_account or disabled")
	}
	_, err = db.conn.Exec(`INSERT INTO background_credentials
		(connection_id, worker, mode, username, encrypted_password, updated_at) VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(connection_id, worker) DO UPDATE SET mode = excluded.mode,
		username = excluded.username, encrypted_password = excluded.encrypted_password, updated_at = excluded.updated_at`,
		connectionID, c.Worker, c.Mode, c.Username, c.EncryptedPassword, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// BackgroundCredentials resolves fresh credentials for each run. A configured
// account that cannot decrypt never falls back to a human session.
func (db *DB) BackgroundCredentials(connectionID, worker, secret string) (string, string, error) {
	c, err := db.GetBackgroundCredential(connectionID, worker)
	if err != nil {
		return "", "", fmt.Errorf("load background credentials: %w", err)
	}
	switch c.Mode {
	case "session":
		return db.BorrowSessionCredentials(connectionID, worker, secret)
	case "disabled":
		return "", "", fmt.Errorf("background execution disabled for %s", worker)
	case "service_account":
		password, err := crypto.Decrypt(c.EncryptedPassword, secret)
		if err != nil {
			return "", "", fmt.Errorf("cannot decrypt background account for %s", worker)
		}
		db.auditBackgroundCredential(connectionID, c)
		return c.Username, password, nil
	default:
		return "", "", fmt.Errorf("invalid background credential mode")
	}
}

type backgroundAuditKey struct{ connection, worker string }
type backgroundAuditEntry struct {
	revision string
	at       time.Time
}

func (db *DB) auditBackgroundCredential(connectionID string, c BackgroundCredential) {
	slog.Debug("Background worker used service account", "worker", c.Worker, "connection", connectionID, "ch_user", c.Username)
	db.backgroundAuditMu.Lock()
	defer db.backgroundAuditMu.Unlock()
	key := backgroundAuditKey{connectionID, c.Worker}
	now := time.Now()
	if last, ok := db.backgroundAudits[key]; ok && last.revision == c.UpdatedAt && now.Sub(last.at) < time.Hour {
		return
	}
	details, _ := json.Marshal(map[string]string{"source": "service_account", "worker": c.Worker, "credential_updated_at": c.UpdatedAt})
	detail := string(details)
	if err := db.CreateAuditLog(AuditLogParams{
		Action: c.Worker + ".credential_use", Username: &c.Username, ConnectionID: &connectionID, Details: &detail,
	}); err != nil {
		slog.Warn("Failed to audit background account use", "worker", c.Worker, "connection", connectionID, "error", err)
		return
	}
	if db.backgroundAudits == nil {
		db.backgroundAudits = make(map[backgroundAuditKey]backgroundAuditEntry)
	}
	db.backgroundAudits[key] = backgroundAuditEntry{revision: c.UpdatedAt, at: now}
}
