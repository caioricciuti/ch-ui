package database

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// borrowAuditInterval caps credential borrow audit rows at one per worker and
// connection per hour, so frequent background ticks do not flood the table.
// Every borrow still gets a debug log.
const borrowAuditInterval = time.Hour

var lastBorrowAudit sync.Map // worker + "|" + connectionID → time.Time

// BorrowSessionCredentials returns the ClickHouse user and password of the
// newest active session on connectionID whose password decrypts with secret.
// Background workers (schedules, models, pipelines, governance, cluster
// health, telemetry monitors) use it to run without a user in the loop.
//
// Each borrow is audited as "<worker>.credential_borrow" with the session id,
// at most once per worker and connection per hour.
func (db *DB) BorrowSessionCredentials(connectionID, worker, secret string) (string, string, error) {
	sessions, err := db.GetActiveSessionsByConnection(connectionID, 3)
	if err != nil {
		return "", "", fmt.Errorf("failed to load sessions: %w", err)
	}
	for _, sess := range sessions {
		password, err := crypto.Decrypt(sess.EncryptedPassword, secret)
		if err != nil {
			continue
		}
		db.auditCredentialBorrow(connectionID, worker, sess)
		return sess.ClickhouseUser, password, nil
	}
	return "", "", fmt.Errorf("no active sessions with valid credentials for connection %s", connectionID)
}

func (db *DB) auditCredentialBorrow(connectionID, worker string, sess Session) {
	slog.Debug("Background worker borrowed session credentials",
		"worker", worker, "connection", connectionID, "ch_user", sess.ClickhouseUser, "session_id", sess.ID)

	key := worker + "|" + connectionID
	now := time.Now()
	if last, ok := lastBorrowAudit.Load(key); ok {
		if t, ok := last.(time.Time); ok && now.Sub(t) < borrowAuditInterval {
			return
		}
	}
	lastBorrowAudit.Store(key, now)

	details := fmt.Sprintf(`{"session_id":%q,"purpose":"background_sync","worker":%q}`, sess.ID, worker)
	connID := connectionID
	user := sess.ClickhouseUser
	if err := db.CreateAuditLog(AuditLogParams{
		Action:       worker + ".credential_borrow",
		Username:     &user,
		ConnectionID: &connID,
		Details:      &details,
	}); err != nil {
		slog.Warn("Failed to write credential borrow audit log",
			"worker", worker, "connection", connectionID, "error", err)
	}
}
