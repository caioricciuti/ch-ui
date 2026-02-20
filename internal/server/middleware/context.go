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
