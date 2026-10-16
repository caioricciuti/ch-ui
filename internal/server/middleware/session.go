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

// ConnectionHeader carries the connection id the UI believes its session is
// on. Session rejects a request whose header disagrees with the session.
const ConnectionHeader = "X-CHUI-Connection"

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

			// A tab that still shows another connection (the session was
			// switched in a different tab) names the connection it thinks it
			// is on. Refuse instead of running its request on the new one.
			if want := r.Header.Get(ConnectionHeader); want != "" && want != session.ConnectionID {
				writeError(w, http.StatusConflict, "connection_changed")
				return
			}

			authSubject := ""
			if session.AuthSubject != nil {
				authSubject = *session.AuthSubject
			}
			identityID := ""
			if session.SSOIdentityID != nil {
				identityID = *session.SSOIdentityID
			}
			// SSO people share one ClickHouse account, so overrides are looked
			// up per person, never by the shared account name. A ClickHouse
			// user's override counts only on the connection it was set for.
			role, err := db.SessionRole(session)
			if err != nil {
				slog.Warn("Failed to resolve explicit user role", "user", database.RoleKey(session.ClickhouseUser, authSubject), "connection", session.ConnectionID, "error", err)
			}

			info := &SessionInfo{
				ID:                session.ID,
				ConnectionID:      session.ConnectionID,
				ClickhouseUser:    session.ClickhouseUser,
				EncryptedPassword: session.EncryptedPassword,
				UserRole:          role,
				AuthSubject:       authSubject,
				SSOIdentityID:     identityID,
			}

			ctx := SetSession(r.Context(), info)
			// Every query this request runs is tagged with the feature and the
			// person, so system.query_log can tell people apart even when
			// ClickHouse sees a shared account.
			ctx = tunnel.WithTag(ctx, QueryTagFor(r.URL.Path, info))
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// RequireWriter allows admin and analyst roles through and blocks viewers (and
// unauthenticated requests). It gates mutations on shared CH-UI workspace
// objects — dashboards, pipelines, models, saved queries — so a read-only
// viewer cannot create, edit, or delete them. Interactive queries use the
// caller's ClickHouse grants. Shared jobs can instead delegate the grants of
// an administrator-configured background account to workspace writers.
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
