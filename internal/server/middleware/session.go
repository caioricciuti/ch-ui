package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// writeJSON writes a JSON response. Private copy of the handlers package
// helper: middleware cannot import handlers without an import cycle.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// writeError writes the canonical JSON error envelope:
// {"success": false, "error": message}. Keep in sync with handlers.writeError.
func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]interface{}{
		"success": false,
		"error":   message,
	})
}

// Session returns a middleware that validates the chui_session cookie.
func Session(db *database.DB, _ *tunnel.Gateway) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("chui_session")
			if err != nil || cookie.Value == "" {
				writeError(w, http.StatusUnauthorized, "Not authenticated")
				return
			}

			session, err := db.GetSession(cookie.Value)
			if err != nil {
				writeError(w, http.StatusInternalServerError, "Session lookup failed")
				return
			}
			if session == nil {
				writeError(w, http.StatusUnauthorized, "Session expired or invalid")
				return
			}

			role := "viewer"
			overrideRole, err := db.GetUserRole(session.ClickhouseUser)
			if err != nil {
				slog.Warn("Failed to resolve explicit user role", "user", session.ClickhouseUser, "error", err)
			} else if overrideRole != "" {
				role = overrideRole
			} else if session.UserRole != nil && *session.UserRole != "" {
				role = *session.UserRole
			}

			info := &SessionInfo{
				ID:                session.ID,
				ConnectionID:      session.ConnectionID,
				ClickhouseUser:    session.ClickhouseUser,
				EncryptedPassword: session.EncryptedPassword,
				UserRole:          role,
			}
			if session.AuthSubject != nil {
				info.AuthSubject = *session.AuthSubject
			}

			ctx := SetSession(r.Context(), info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin returns a middleware that requires admin role.
// RequireWriter allows admin and analyst roles through and blocks viewers (and
// unauthenticated requests). It gates mutations on shared CH-UI workspace
// objects — dashboards, pipelines, models, saved queries — so a read-only
// viewer cannot create, edit, or delete them. Data access remains governed by
// each user's own ClickHouse grants regardless of this role.
func RequireWriter() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := GetSession(r)
			if session == nil || (session.UserRole != "admin" && session.UserRole != "analyst") {
				writeError(w, http.StatusForbidden, "Write access required (viewer is read-only)")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin gates admin-only routes on the session's resolved role — the
// same SessionInfo.UserRole RequireWriter reads, which Session() already
// computes as: an explicit DB role override for session.ClickhouseUser if
// one exists, else the OIDC-derived per-session role, else "viewer". A raw
// db.IsUserRole(session.ClickhouseUser, "admin") lookup here (the previous
// implementation) breaks under SSO: every OIDC session shares one
// ClickHouse service account, so it could only ever grant or deny admin to
// all OIDC users at once, never distinguish between them by their actual
// IdP group.
func RequireAdmin(db *database.DB) func(http.Handler) http.Handler {
	_ = db // kept for call-site signature compatibility; role now comes from SessionInfo
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := GetSession(r)
			if session == nil || session.UserRole != "admin" {
				writeError(w, http.StatusForbidden, "Admin access required")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
