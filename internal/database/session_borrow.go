package database

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/internal/crypto"
)

// borrowAuditInterval caps credential borrow audit rows at one per worker and
// connection per hour, so frequent background ticks do not flood the table.
// Every borrow still gets a debug log.
const borrowAuditInterval = time.Hour

var lastBorrowAudit sync.Map // worker + "|" + connectionID → time.Time

// ErrNoBorrowableSession means no live session on the connection can lend
// its credentials to a background worker.
var ErrNoBorrowableSession = errors.New("no active sessions with valid credentials")

// stableBorrowSessionLimit bounds how many live sessions a stable borrow
// looks at when choosing an account.
const stableBorrowSessionLimit = 200

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
		if password, ok := db.borrowable(connectionID, sess, secret); ok {
			db.auditCredentialBorrow(connectionID, worker, sess)
			return sess.ClickhouseUser, password, nil
		}
	}
	return "", "", fmt.Errorf("%w for connection %s", ErrNoBorrowableSession, connectionID)
}

// BorrowSessionCredentialsStable is BorrowSessionCredentials for workers whose
// results depend on who runs them (governance reads what the account can
// see). Instead of the newest session it keeps preferUser while that user
// has a live session, and otherwise picks deterministically: sessions with
// the admin app role first, then by ClickHouse user name, then newest.
func (db *DB) BorrowSessionCredentialsStable(connectionID, worker, secret, preferUser string) (string, string, error) {
	sessions, err := db.GetActiveSessionsByConnection(connectionID, stableBorrowSessionLimit)
	if err != nil {
		return "", "", fmt.Errorf("failed to load sessions: %w", err)
	}
	isAdmin := func(s Session) bool { return s.UserRole != nil && *s.UserRole == "admin" }
	// Sessions arrive newest first; a stable sort keeps that as the last tie-break.
	sort.SliceStable(sessions, func(i, j int) bool {
		a, b := sessions[i], sessions[j]
		if pa, pb := preferUser != "" && a.ClickhouseUser == preferUser, preferUser != "" && b.ClickhouseUser == preferUser; pa != pb {
			return pa
		}
		if aa, ab := isAdmin(a), isAdmin(b); aa != ab {
			return aa
		}
		return a.ClickhouseUser < b.ClickhouseUser
	})
	for _, sess := range sessions {
		if password, ok := db.borrowable(connectionID, sess, secret); ok {
			db.auditCredentialBorrow(connectionID, worker, sess)
			return sess.ClickhouseUser, password, nil
		}
	}
	return "", "", fmt.Errorf("%w for connection %s", ErrNoBorrowableSession, connectionID)
}

// borrowable returns the session's password when a background worker may run
// as it. A per-person SSO user belongs to that person: a job must never run
// as them. Shared accounts and password logins may be borrowed.
func (db *DB) borrowable(connectionID string, sess Session, secret string) (string, bool) {
	if owner, err := db.SSOClickHouseUserOwner(connectionID, sess.ClickhouseUser); err != nil || owner != "" {
		return "", false
	}
	password, err := crypto.Decrypt(sess.EncryptedPassword, secret)
	if err != nil {
		return "", false
	}
	return password, true
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
	chUser := sess.ClickhouseUser
	// Name the person whose session was borrowed (the SSO email when there is
	// one); the ClickHouse account goes in ch_user.
	person := chUser
	if sess.AuthSubject != nil && *sess.AuthSubject != "" {
		person = *sess.AuthSubject
	}
	if err := db.CreateAuditLog(AuditLogParams{
		Action:         worker + ".credential_borrow",
		Username:       &person,
		ClickhouseUser: &chUser,
		ConnectionID:   &connID,
		Details:        &details,
	}); err != nil {
		slog.Warn("Failed to write credential borrow audit log",
			"worker", worker, "connection", connectionID, "error", err)
	}
}
