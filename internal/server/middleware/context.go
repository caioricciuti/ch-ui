package middleware

import (
	"context"
	"net/http"
)

type contextKey string

const (
	sessionKey contextKey = "session"
)

// SessionInfo holds session data stored in the request context.
type SessionInfo struct {
	ID                string
	ConnectionID      string
	ClickhouseUser    string
	EncryptedPassword string
	UserRole          string
	// AuthSubject is the human identity (OIDC email) when it differs from
	// ClickhouseUser; empty for password logins.
	AuthSubject string
}

// Actor is the person behind a session, for attribution and per-person data:
// the SSO email when there is one, else the ClickHouse user. SSO people share
// one ClickHouse service account, so ClickhouseUser alone cannot tell them
// apart. Use ClickhouseUser only for running SQL against ClickHouse.
func Actor(s *SessionInfo) string {
	if s == nil {
		return ""
	}
	if s.AuthSubject != "" {
		return s.AuthSubject
	}
	return s.ClickhouseUser
}

// SetSession stores the session in the request context.
func SetSession(ctx context.Context, session *SessionInfo) context.Context {
	return context.WithValue(ctx, sessionKey, session)
}

// GetSession retrieves the session from the request context.
func GetSession(r *http.Request) *SessionInfo {
	s, _ := r.Context().Value(sessionKey).(*SessionInfo)
	return s
}
