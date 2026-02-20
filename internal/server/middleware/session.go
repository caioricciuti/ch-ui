package middleware

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

// Session returns a middleware that validates the chui_session cookie.
func Session(db *database.DB, gw *tunnel.Gateway) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			cookie, err := r.Cookie("chui_session")
			if err != nil || cookie.Value == "" {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
				return
			}

			session, err := db.GetSession(cookie.Value)
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Session lookup failed"})
				return
			}
			if session == nil {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Session expired or invalid"})
				return
			}

			// Check tunnel is online
			if !gw.IsTunnelOnline(session.ConnectionID) {
				writeJSON(w, http.StatusServiceUnavailable, map[string]string{
					"error":   "Tunnel offline",
					"message": "The database connection tunnel is currently offline. Please check the agent.",
				})
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

			ctx := SetSession(r.Context(), info)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireAdmin returns a middleware that requires admin role.
func RequireAdmin(db *database.DB) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			session := GetSession(r)
			if session == nil {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin access required"})
				return
			}

			isAdmin, err := db.IsUserRole(session.ClickhouseUser, "admin")
			if err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Role check failed"})
				return
			}
			if !isAdmin {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "Admin access required"})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
