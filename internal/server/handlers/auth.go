package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/embedded"
	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/caioricciuti/ch-ui/internal/version"
)

// Session and rate-limit constants.
const (
	SessionCookie      = "chui_session"
	SessionDuration    = 7 * 24 * time.Hour
	RateLimitWindow    = 15 * time.Minute
	MaxAttemptsPerIP   = 5
	MaxAttemptsPerUser = 3
)

// errReservedUsername refuses a password login whose username starts with
// "sso:", the prefix CH-UI keys SSO people's roles on.
const errReservedUsername = `ClickHouse usernames starting with "sso:" cannot sign in to CH-UI with a password. Use a different ClickHouse user.`

// AuthHandler implements the authentication HTTP endpoints.
type AuthHandler struct {
	DB          *database.DB
	Gateway     *tunnel.Gateway
	RateLimiter *middleware.RateLimiter
	Config      *config.Config
	OIDC        *oidc.Manager // holds the active OIDC provider; may be inactive
	// Setup serves first-run setup (POST /setup). nil disables it.
	Setup *SetupHandler
	// Agents starts connectors for connections created by sign-in with a
	// ClickHouse URL. nil in tests.
	Agents *embedded.Manager
	// Provisioner creates per-person ClickHouse users for SSO people on
	// connections in per-person mode. nil refuses per-person sign-ins.
	Provisioner *ssoidentity.Provisioner

	// loginURLMu serialises find-or-create for sign-in with a ClickHouse URL.
	loginURLMu sync.Mutex
}

// sessionDuration returns the configured session lifetime (session_max_age /
// SESSION_MAX_AGE), falling back to the 7-day default.
func (h *AuthHandler) sessionDuration() time.Duration {
	if h.Config != nil && h.Config.SessionMaxAge > 0 {
		return time.Duration(h.Config.SessionMaxAge) * time.Second
	}
	return SessionDuration
}

// Routes returns a chi.Router with all auth routes mounted.
func (h *AuthHandler) Routes(r chi.Router) {
	r.Get("/config", h.AuthConfig)
	r.Post("/login", h.Login)
	r.Post("/logout", h.Logout)
	r.Get("/session", h.Session)
	r.Get("/connections", h.Connections)
	// Per-IP request cap on top of the shared credential budget, which only
	// counts rejected passwords.
	r.With(middleware.IPRateLimit(20, time.Minute)).Post("/switch-connection", h.SwitchConnection)
	if h.Setup != nil {
		r.Post("/setup", h.Setup.Setup)
	}

	if h.OIDC != nil {
		// SSO is a Pro feature: RequirePro returns 402 without an active license
		// (GET is still allowed during the read-only grace window, so SSO login
		// keeps working for the grace period rather than locking users out).
		// The routes are always registered; the handlers fail gracefully when no
		// provider is active, so admin-saved (DB) SSO config can come online
		// without a restart.
		r.With(middleware.RequirePro(h.Config)).Get("/oidc/login", h.OIDCLogin)
		r.With(middleware.RequirePro(h.Config)).Get("/oidc/callback", h.OIDCCallback)
	}
}

// AuthConfig reports which authentication methods are available, so the
// (unauthenticated) login page can render the right options. SSO is only
// advertised when configured AND licensed (Pro, including the grace window).
//
// GET /api/auth/config
func (h *AuthHandler) AuthConfig(w http.ResponseWriter, r *http.Request) {
	oidcEnabled := h.OIDC != nil && h.OIDC.Active() && h.Config != nil && h.Config.ProAccess() != config.ProNone
	ssoOnly := h.passwordLoginBlocked()
	resp := map[string]interface{}{
		"password_login":    !ssoOnly,
		"sso_only":          ssoOnly,
		"oidc_enabled":      oidcEnabled,
		"setup_open":        h.Setup.Open(),
		"login_url_allowed": h.loginURLAllowed(),
	}
	if oidcEnabled {
		resp["oidc_login_url"] = "/api/auth/oidc/login"
	}
	writeJSON(w, http.StatusOK, resp)
}

// ---------- request / response types ----------

type loginRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	ConnectionID      string `json:"connectionId"`
	ConnectionIDSnake string `json:"connection_id"`
	// ClickHouseURL signs in against a URL instead of a saved connection.
	// Only accepted when allow_login_url is on.
	ClickHouseURL string `json:"clickhouse_url"`
}

type switchConnectionRequest struct {
	ConnectionID      string `json:"connectionId"`
	ConnectionIDSnake string `json:"connection_id"`
	Username          string `json:"username"`
	Password          string `json:"password"`
}

type connectionInfo struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
	Online bool   `json:"online"`
	// Kind is "direct" when CH-UI itself connects to ClickHouse (embedded or
	// direct connections) and "agent" when a ch-ui connect agent does. The
	// login page words its help by it.
	Kind string `json:"kind"`
	// SSOReady is set only for a caller signed in through SSO: whether the
	// connection has the SSO service account a switch needs.
	SSOReady   *bool   `json:"sso_ready,omitempty"`
	LastSeenAt *string `json:"last_seen_at"`
	CreatedAt  string  `json:"created_at"`
}

// connectionKind reports "direct" or "agent" for c; see connectionInfo.Kind.
func connectionKind(c database.Connection) string {
	if c.IsEmbedded || c.Type == database.ConnectionTypeDirect {
		return "direct"
	}
	return "agent"
}

func (r loginRequest) resolvedConnectionID() string {
	if id := strings.TrimSpace(r.ConnectionID); id != "" {
		return id
	}
	return strings.TrimSpace(r.ConnectionIDSnake)
}

func (r switchConnectionRequest) resolvedConnectionID() string {
	if id := strings.TrimSpace(r.ConnectionID); id != "" {
		return id
	}
	return strings.TrimSpace(r.ConnectionIDSnake)
}

func normalizeRateLimitUsername(username string) string {
	return strings.ToLower(strings.TrimSpace(username))
}

func userRateLimitKey(username, connectionID string) string {
	return fmt.Sprintf("user:%s:%s", normalizeRateLimitUsername(username), strings.TrimSpace(connectionID))
}

// chAuthErrorKind classifies the error behind a failed login attempt. The
// agent reports a password ClickHouse rejected and a ClickHouse it never
// reached through the same channel (handleTestResult sends both to
// ErrorCh), so the login handler has to tell them apart before it counts an
// attempt against the rate limiter.
type chAuthErrorKind int

const (
	// chAuthRejected: ClickHouse answered and said no.
	chAuthRejected chAuthErrorKind = iota
	// chAuthUnreachable: no answer at all. Not evidence of a bad password.
	chAuthUnreachable
	// chAuthUnknown: could not tell. Treated as a rejection, so an
	// unrecognised message can never be used to dodge the lockout.
	chAuthUnknown
)

// The connector reports ClickHouse's own answers as "ClickHouse error: <body>"
// (non-200 status). Anything carrying one of these markers came back from
// ClickHouse, so it can never be a connection problem.
var chAnsweredMarkers = []string{"clickhouse error", "db::exception"}

// Markers of a ClickHouse that rejected the credentials.
var chRejectedMarkers = []string{
	"authenticat", "credential", "password", "unauthorized", "access denied",
	"unknown user", "no user with such name",
}

// Markers of a request that never got an answer from ClickHouse.
var chUnreachableMarkers = []string{
	"no such host", "connection refused", "refused", "no route", "connection reset",
	"broken pipe", "network is unreachable", "network", "malformed http",
	"server gave http response to https client", "tls", "x509", "certificate",
	"timeout", "timed out", "deadline", "tunnel", "not connected", "disconnected",
	"unreachable", "connection test failed",
}

// eofPattern matches a bare EOF (the server closed the connection), not
// words that merely contain the letters.
var eofPattern = regexp.MustCompile(`(^|[^a-z])eof($|[^a-z])`)

// quotedURLPattern finds the request URL Go's HTTP client quotes in its
// errors: Post "http://host:8123/?default_format=JSON": dial tcp ...
var quotedURLPattern = regexp.MustCompile(`"(https?://[^"\s]+)"`)

func containsAny(s string, needles []string) bool {
	for _, n := range needles {
		if strings.Contains(s, n) {
			return true
		}
	}
	return false
}

func classifyClickHouseAuthError(raw string) chAuthErrorKind {
	msg := strings.ToLower(strings.TrimSpace(raw))
	if msg == "" {
		return chAuthRejected
	}
	// The quoted URL is the target, not the failure: a host named
	// "auth.internal" must not turn a refused connection into a rejection.
	scrubbed := quotedURLPattern.ReplaceAllString(msg, "")
	if containsAny(scrubbed, chAnsweredMarkers) {
		if containsAny(scrubbed, chRejectedMarkers) {
			return chAuthRejected
		}
		return chAuthUnknown
	}
	if containsAny(scrubbed, chUnreachableMarkers) || eofPattern.MatchString(scrubbed) {
		return chAuthUnreachable
	}
	if containsAny(scrubbed, chRejectedMarkers) {
		return chAuthRejected
	}
	return chAuthUnknown
}

// clickHouseTarget returns scheme://host[:port] of the URL quoted in a
// connection error, or "" when there is none. The query string is dropped.
func clickHouseTarget(raw string) string {
	m := quotedURLPattern.FindStringSubmatch(raw)
	if m == nil {
		return ""
	}
	u, err := url.Parse(m[1])
	if err != nil || u.Host == "" {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

// describeClickHouseConnectionError turns a connection failure into one
// sentence naming the target and the likely cause.
func describeClickHouseConnectionError(raw string) string {
	msg := strings.ToLower(raw)
	target := clickHouseTarget(raw)
	where := "ClickHouse"
	if target != "" {
		where = target
	}
	nativePort := false
	if u, err := url.Parse(target); err == nil && u.Port() == "9000" {
		nativePort = true
	}

	var cause string
	switch {
	case strings.Contains(msg, "tunnel not connected") || strings.Contains(msg, "tunnel disconnected"):
		return "Connection to ClickHouse failed: the agent for this connection is not connected. Start it and retry."
	case strings.Contains(msg, "malformed http") ||
		(nativePort && (eofPattern.MatchString(msg) || strings.Contains(msg, "connection reset"))):
		cause = "the server did not answer HTTP. Port 9000 is the native protocol; use the HTTP port 8123 (8443 for HTTPS)."
	case strings.Contains(msg, "server gave http response to https client"):
		cause = "the server speaks plain HTTP. Use http:// instead of https://."
	case strings.Contains(msg, "no such host"):
		cause = "the host name does not resolve. Check the host; inside Docker use host.docker.internal or the service name, not localhost."
	case strings.Contains(msg, "refused"):
		cause = "nothing is listening there. Check that ClickHouse is running and the port is its HTTP port (8123). Inside Docker, localhost is the container itself; use host.docker.internal."
	case strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") || strings.Contains(msg, "tls"):
		cause = "the TLS handshake failed. Use https:// with the TLS port (8443) and a certificate the server trusts, or http:// with 8123."
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "timed out") || strings.Contains(msg, "deadline"):
		cause = "the connection timed out. Check that the host is reachable from the CH-UI server and no firewall blocks the port."
	case eofPattern.MatchString(msg) || strings.Contains(msg, "connection reset") || strings.Contains(msg, "broken pipe"):
		cause = "the server closed the connection. Check that the port is the ClickHouse HTTP port (8123), not the native port (9000)."
	default:
		cause = "no answer from ClickHouse. Check the URL and that ClickHouse is running."
	}
	return fmt.Sprintf("Connection to ClickHouse failed: could not reach %s: %s", where, cause)
}

func sanitizeClickHouseAuthMessage(raw string) string {
	switch classifyClickHouseAuthError(raw) {
	case chAuthRejected:
		return "Invalid credentials"
	case chAuthUnreachable:
		return describeClickHouseConnectionError(raw)
	default:
		return "Authentication failed"
	}
}

// credentialAttempt holds the rate-limit slots one credential check reserved.
// Every endpoint that tests a username and password against ClickHouse
// (login, switch connection, connection test) goes through it, so they share
// one budget per IP and per user.
type credentialAttempt struct {
	rl    *middleware.RateLimiter
	held  []credentialSlot
	ended bool
}

type credentialSlot struct{ key, limitType string }

// reserve checks key's limit and holds a slot. When the limit is reached it
// writes the 429 response and returns false.
func (a *credentialAttempt) reserve(w http.ResponseWriter, key, limitType string, maxAttempts int, refusal string) bool {
	res := a.rl.CheckAuthRateLimit(key, limitType, maxAttempts, RateLimitWindow)
	if !res.Allowed {
		retrySeconds := int(res.RetryAfter.Seconds())
		slog.Warn("Credential check rate limited", "key", key, "retryAfter", retrySeconds)
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"success":    false,
			"error":      refusal,
			"retryAfter": retrySeconds,
		})
		return false
	}
	a.held = append(a.held, credentialSlot{key, limitType})
	return true
}

// reserveIPAndUser reserves the IP slot, then the user slot for
// username on connID.
func (a *credentialAttempt) reserveIPAndUser(w http.ResponseWriter, clientIP, username, connID string) bool {
	if !a.reserve(w, "ip:"+clientIP, "ip", MaxAttemptsPerIP, "Too many login attempts from this IP") {
		return false
	}
	return a.reserve(w, userRateLimitKey(username, connID), "user", MaxAttemptsPerUser, "Too many login attempts for this user")
}

// failed counts a rejected credential against every held slot.
func (a *credentialAttempt) failed() {
	for _, s := range a.held {
		a.rl.RecordAttempt(s.key, s.limitType)
	}
	a.held, a.ended = nil, true
}

// succeeded clears the limits of every held slot.
func (a *credentialAttempt) succeeded() {
	for _, s := range a.held {
		a.rl.ResetLimit(s.key)
	}
	a.held, a.ended = nil, true
}

// release gives back slots not consumed by failed or succeeded. Defer it
// right after creating the attempt so early returns never leak a slot.
func (a *credentialAttempt) release() {
	if a.ended {
		return
	}
	for _, s := range a.held {
		a.rl.Release(s.key)
	}
	a.held = nil
}

func shouldUseSecureCookie(r *http.Request, cfg *config.Config) bool {
	// Direct TLS request (no proxy)
	if r != nil && r.TLS != nil {
		return true
	}
	// Reverse proxy forwarding HTTPS
	if r != nil && strings.EqualFold(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")), "https") {
		return true
	}
	// Fallback to configured public app URL scheme.
	if cfg != nil && strings.TrimSpace(cfg.AppURL) != "" {
		if parsed, err := url.Parse(cfg.AppURL); err == nil {
			return strings.EqualFold(parsed.Scheme, "https")
		}
	}
	return false
}

// auditLoginFailure records a failed authentication attempt in the immutable
// audit trail. Security reviews and SIEM ingestion require failed logins (not
// just successes) to detect brute-force and credential-stuffing activity. The
// password is never logged — only the attempted username, connection, source
// IP, and a coarse reason.
func (h *AuthHandler) auditLoginFailure(username string, conn *database.Connection, clientIP, reason string) {
	connID, connName := "", "unknown"
	if conn != nil {
		connID = conn.ID
		connName = conn.Name
	}
	_ = h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "user.login_failed",
		Username:     strPtr(username),
		ConnectionID: strPtr(connID),
		Details:      strPtr(fmt.Sprintf("Failed login via connection %s (%s)", connName, reason)),
		IPAddress:    strPtr(clientIP),
	})
}

// ---------- POST /login ----------

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if h.passwordLoginBlocked() {
		writeError(w, http.StatusForbidden, errSSOOnly)
		return
	}

	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "Username is required")
		return
	}
	// "sso:<email>" is how CH-UI names SSO people; a ClickHouse user named
	// that way could pass for one.
	if database.IsReservedRoleUsername(req.Username) {
		writeError(w, http.StatusBadRequest, errReservedUsername)
		return
	}
	req.ConnectionID = req.resolvedConnectionID()
	req.ClickHouseURL = strings.TrimSpace(req.ClickHouseURL)
	if req.ClickHouseURL != "" {
		if req.ConnectionID != "" {
			writeError(w, http.StatusBadRequest, errLoginURLAndConn)
			return
		}
		if !h.loginURLAllowed() {
			writeError(w, http.StatusForbidden, errLoginURLDisabled)
			return
		}
	}

	// --- Rate limiting ---
	clientIP := getClientIP(r)
	attempt := &credentialAttempt{rl: h.RateLimiter}
	defer attempt.release()
	if !attempt.reserve(w, "ip:"+clientIP, "ip", MaxAttemptsPerIP, "Too many login attempts from this IP") {
		return
	}

	// --- Resolve connection ---
	var conn *database.Connection
	if req.ClickHouseURL != "" {
		chURL, err := validateSetupClickHouseURL(req.ClickHouseURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		conn, _, err = h.loginURLConnection(chURL, req.Username, clientIP)
		if errors.Is(err, errLoginURLCapReached) {
			slog.Warn("Login URL connection cap reached", "ip", clientIP, "cap", loginURLMaxConnections)
			writeError(w, http.StatusTooManyRequests, errLoginURLCapped)
			return
		}
		if err != nil {
			slog.Error("Failed to resolve login URL connection", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to prepare connection")
			return
		}
	}

	if conn == nil {
		connections, err := h.DB.GetConnections()
		if err != nil {
			slog.Error("Failed to get connections", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve connections")
			return
		}

		if len(connections) == 0 {
			writeJSON(w, http.StatusBadRequest, map[string]interface{}{
				"success": false,
				"error":   "No connections available",
				"message": "No connections are configured. Please set up an agent first.",
			})
			return
		}

		if req.ConnectionID != "" {
			for i := range connections {
				if connections[i].ID == req.ConnectionID {
					conn = &connections[i]
					break
				}
			}
			if conn == nil {
				writeError(w, http.StatusBadRequest, "Connection not found")
				return
			}
		} else {
			conn = &connections[0]
			if hidden := hiddenLoginConnectionID(h.DB); hidden != "" && len(connections) > 1 && conn.ID == hidden {
				conn = &connections[1]
			}
		}
	}

	if !attempt.reserve(w, userRateLimitKey(req.Username, conn.ID), "user", MaxAttemptsPerUser, "Too many login attempts for this user") {
		return
	}

	// --- Check tunnel is online (retry up to 3 times) ---
	if !waitTunnelOnline(h.Gateway, conn.ID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"success": false,
			"error":   "Connection offline",
			"message": "The tunnel agent for this connection is not online. Please check that the agent is running.",
		})
		return
	}

	// --- Test ClickHouse credentials ---
	testResult, err := h.Gateway.TestConnection(conn.ID, req.Username, req.Password, 15*time.Second)
	if err == nil && !testResult.Success {
		errMsg := testResult.Error
		if errMsg == "" {
			errMsg = "Invalid credentials"
		}
		err = errors.New(errMsg)
	}
	if err != nil {
		// A ClickHouse that never answered is not a failed password.
		// Counting it locks a healthy account out for RateLimitWindow
		// every time the database, the agent or the network hiccups.
		// Rejections and anything unrecognised still count.
		if classifyClickHouseAuthError(err.Error()) == chAuthUnreachable {
			slog.Info("Login aborted: ClickHouse unreachable", "user", req.Username, "error", err)
			h.auditLoginFailure(req.Username, conn, clientIP, "clickhouse unreachable")
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"success": false,
				"error":   "Connection error",
				"message": sanitizeClickHouseAuthMessage(err.Error()),
			})
			return
		}
		attempt.failed()
		slog.Info("Login failed: credentials rejected", "user", req.Username, "error", err)
		h.auditLoginFailure(req.Username, conn, clientIP, "invalid credentials")
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Authentication failed",
			"message": sanitizeClickHouseAuthMessage(err.Error()),
		})
		return
	}

	// --- Resolve CH-UI role ---
	role, roleNote := h.resolveUserRole(conn.ID, req.Username, req.Password, clientIP)

	// --- Encrypt password and create session ---
	encryptedPwd, err := crypto.Encrypt(req.Password, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to encrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(h.sessionDuration()).Format(time.RFC3339)

	_, err = h.DB.CreateSession(database.CreateSessionParams{
		ConnectionID:      conn.ID,
		ClickhouseUser:    req.Username,
		EncryptedPassword: encryptedPwd,
		Token:             token,
		ExpiresAt:         expiresAt,
		UserRole:          role,
	})
	if err != nil {
		slog.Error("Failed to create session", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	// --- First admin login closes first-run setup for good ---
	closeSetupOnAdminLogin(h.DB, h.Setup, role, req.Username, clientIP)

	// --- Set cookie ---
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.sessionDuration().Seconds()),
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(r, h.Config),
		SameSite: http.SameSiteLaxMode,
	})

	// --- Reset rate limits on success ---
	attempt.succeeded()

	// --- Audit log ---
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "user.login",
		Username:     strPtr(req.Username),
		ConnectionID: strPtr(conn.ID),
		Details:      strPtr(fmt.Sprintf("Login via connection %s (role: %s, version: %s)", conn.Name, role, testResult.Version)),
		IPAddress:    strPtr(clientIP),
	})

	slog.Info("User logged in", "user", req.Username, "role", role, "connection", conn.Name)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"user":               req.Username,
		"user_role":          role,
		"clickhouse_version": testResult.Version,
		"expires_at":         expiresAt,
		"connection": map[string]interface{}{
			"id":     conn.ID,
			"name":   conn.Name,
			"online": true,
		},
		"session": map[string]interface{}{
			"user":             req.Username,
			"role":             role,
			"roleNote":         roleNote,
			"connectionId":     conn.ID,
			"connectionName":   conn.Name,
			"connectionOnline": true,
			"expiresAt":        expiresAt,
			"version":          testResult.Version,
			"appVersion":       version.Version,
		},
	})
}

// ---------- POST /logout ----------

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
		return
	}

	session, _ := h.DB.GetSession(cookie.Value)

	if err := h.DB.DeleteSession(cookie.Value); err != nil {
		slog.Error("Failed to delete session", "error", err)
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(r, h.Config),
		SameSite: http.SameSiteLaxMode,
	})

	if session != nil {
		clientIP := getClientIP(r)
		// The stored session carries AuthSubject as a pointer; map it onto
		// SessionInfo so attribution goes through the one Actor rule.
		info := &middleware.SessionInfo{ClickhouseUser: session.ClickhouseUser}
		if session.AuthSubject != nil {
			info.AuthSubject = *session.AuthSubject
		}
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:         "user.logout",
			Username:       strPtr(middleware.Actor(info)),
			ClickhouseUser: &session.ClickhouseUser,
			IPAddress:      strPtr(clientIP),
		})
		slog.Info("User logged out", "user", session.ClickhouseUser)
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// ---------- GET /session ----------

func (h *AuthHandler) Session(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
		return
	}

	session, err := h.DB.GetSession(cookie.Value)
	if err != nil {
		slog.Error("Failed to get session", "error", err)
		writeError(w, http.StatusInternalServerError, "Session lookup failed")
		return
	}
	if session == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     SessionCookie,
			Value:    "",
			Path:     "/",
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   shouldUseSecureCookie(r, h.Config),
			SameSite: http.SameSiteLaxMode,
		})
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"authenticated": false,
		})
		return
	}

	connOnline := h.Gateway.IsTunnelOnline(session.ConnectionID)
	connName := ""
	tc, _ := h.DB.GetConnectionByID(session.ConnectionID)
	if tc != nil {
		connName = tc.Name
	}

	role, roleErr := h.DB.SessionRole(session)
	if roleErr != nil {
		slog.Warn("Failed to resolve explicit role for session", "user", session.ClickhouseUser, "connection", session.ConnectionID, "error", roleErr)
	}

	// For SSO sessions, show the person's identity (email) rather than the
	// shared ClickHouse service account they query through.
	displayUser := session.ClickhouseUser
	viaSSO := false
	if session.AuthSubject != nil && *session.AuthSubject != "" {
		displayUser = *session.AuthSubject
		viaSSO = true
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"user":          displayUser,
		"user_role":     role,
		"expires_at":    session.ExpiresAt,
		"via_sso":       viaSSO,
		"connection": map[string]interface{}{
			"id":     session.ConnectionID,
			"name":   connName,
			"online": connOnline,
		},
		"session": map[string]interface{}{
			"user":             displayUser,
			"role":             role,
			"viaSSO":           viaSSO,
			"connectionId":     session.ConnectionID,
			"connectionName":   connName,
			"connectionOnline": connOnline,
			"expiresAt":        session.ExpiresAt,
			"appVersion":       version.Version,
		},
	})
}

// ---------- GET /connections ----------

func (h *AuthHandler) Connections(w http.ResponseWriter, r *http.Request) {
	connections, err := h.DB.GetConnections()
	if err != nil {
		slog.Error("Failed to get connections", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connections")
		return
	}

	// The connection switcher of an SSO person disables connections it
	// cannot switch to. Anyone else (and the sign-in page) never sees it.
	viaSSO := false
	if cookie, err := r.Cookie(SessionCookie); err == nil && cookie.Value != "" {
		if s, err := h.DB.GetSession(cookie.Value); err == nil && s != nil && s.AuthSubject != nil && *s.AuthSubject != "" {
			viaSSO = true
		}
	}

	hidden := hiddenLoginConnectionID(h.DB)
	result := make([]connectionInfo, 0, len(connections))
	for _, c := range connections {
		if c.ID == hidden {
			continue
		}
		var ssoReady *bool
		if viaSSO {
			saUser, saPassword, err := h.DB.GetConnectionSSOAccount(c.ID)
			ready := err == nil && saUser != "" && saPassword != ""
			ssoReady = &ready
		}
		result = append(result, connectionInfo{
			ID:         c.ID,
			Name:       c.Name,
			Status:     c.Status,
			Online:     h.Gateway.IsTunnelOnline(c.ID),
			Kind:       connectionKind(c),
			SSOReady:   ssoReady,
			LastSeenAt: c.LastSeenAt,
			CreatedAt:  c.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"connections": result,
	})
}

// waitTunnelOnline reports whether connID's tunnel is online, retrying up to
// three times half a second apart (a connector may be reconnecting).
func waitTunnelOnline(gw *tunnel.Gateway, connID string) bool {
	for i := 0; i < 3; i++ {
		if gw.IsTunnelOnline(connID) {
			return true
		}
		if i < 2 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	return false
}

// ---------- POST /switch-connection ----------

// isJSONRequest reports whether r declares a JSON body. HTML forms cannot
// send application/json, so requiring it keeps cross-site form posts out.
func isJSONRequest(r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	return err == nil && mediaType == "application/json"
}

// sameOriginRequest reports whether r comes from CH-UI itself: no Origin
// header (not a browser cross-site call), the host the request was sent to,
// or an origin the CORS policy allows.
func (h *AuthHandler) sameOriginRequest(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	if u, err := url.Parse(origin); err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host) {
		return true
	}
	cfg := middleware.CORSConfig{}
	if h.Config != nil {
		cfg = middleware.CORSConfig{AllowedOrigins: h.Config.AllowedOrigins, AppURL: h.Config.AppURL, DevMode: h.Config.DevMode}
	}
	return middleware.OriginAllowed(cfg, origin)
}

// clickHouseEndpoint returns the lower-cased host and port CH-UI dials for
// a direct (or embedded) connection. ok is false for agent connections,
// whose ClickHouse is whatever the remote agent points at, and for a URL
// without a host.
func clickHouseEndpoint(c *database.Connection) (host, port string, ok bool) {
	if c == nil || c.Type != database.ConnectionTypeDirect {
		return "", "", false
	}
	u, err := url.Parse(strings.TrimSpace(c.ClickHouseURL))
	if err != nil || u.Hostname() == "" {
		return "", "", false
	}
	port = u.Port()
	if port == "" {
		switch strings.ToLower(u.Scheme) {
		case "http":
			port = "8123"
		case "https":
			port = "8443"
		default:
			return "", "", false
		}
	}
	return strings.ToLower(u.Hostname()), port, true
}

// sameClickHouseServer reports whether target reaches the same ClickHouse
// server as the connection currentID, so the session's stored password may
// be reused there. Only the same connection, or two direct connections with
// the same host and port, qualify; an agent or an unknown endpoint never
// does.
func (h *AuthHandler) sameClickHouseServer(currentID string, target *database.Connection) bool {
	if target == nil {
		return false
	}
	if currentID != "" && currentID == target.ID {
		return true
	}
	current, err := h.DB.GetConnectionByID(currentID)
	if err != nil || current == nil {
		return false
	}
	curHost, curPort, ok := clickHouseEndpoint(current)
	if !ok {
		return false
	}
	host, port, ok := clickHouseEndpoint(target)
	return ok && host == curHost && port == curPort
}

// SwitchConnection moves the caller's session to another connection.
//
// Password sessions test a ClickHouse user and password on the target. An
// empty password for the session's own ClickHouse user reuses the stored
// one, but only when the target is the same ClickHouse server (see
// sameClickHouseServer); any other target gets 401 with needs_password and
// reason different_server before anything is sent to it. When the target
// rejects a reused password the answer is 401 with needs_password and
// the attempt is not counted against the lockout (it was not a guess). SSO
// sessions ignore the request credentials and use the target connection's
// SSO service account, keeping the person's identity and role. While
// SSO-only sign-in is in force, password sessions get 403.
//
// The old session is replaced in one transaction (a fresh token, so session
// fixation protection holds); a session that changed under the request is a
// 409 and leaves the caller signed in.
func (h *AuthHandler) SwitchConnection(w http.ResponseWriter, r *http.Request) {
	if !isJSONRequest(r) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return
	}
	if !h.sameOriginRequest(r) {
		writeError(w, http.StatusForbidden, "Cross-origin request refused")
		return
	}

	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	existingSession, err := h.DB.GetSession(cookie.Value)
	if err != nil || existingSession == nil {
		writeError(w, http.StatusUnauthorized, "Session expired or invalid")
		return
	}

	var req switchConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.ConnectionID = req.resolvedConnectionID()
	if req.ConnectionID == "" {
		writeError(w, http.StatusBadRequest, "connection_id (or connectionId) is required")
		return
	}
	// The embedded connection setup replaced is hidden from sign-in; it is
	// not a switch target either.
	if hidden := hiddenLoginConnectionID(h.DB); hidden != "" && req.ConnectionID == hidden {
		writeError(w, http.StatusBadRequest, "Connection not available")
		return
	}

	newConn, err := h.DB.GetConnectionByID(req.ConnectionID)
	if err != nil {
		slog.Error("Failed to get connection", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if newConn == nil {
		writeError(w, http.StatusBadRequest, "Connection not found")
		return
	}

	info := &middleware.SessionInfo{ClickhouseUser: existingSession.ClickhouseUser}
	if existingSession.AuthSubject != nil {
		info.AuthSubject = *existingSession.AuthSubject
	}
	if existingSession.SSOIdentityID != nil {
		info.SSOIdentityID = *existingSession.SSOIdentityID
	}
	actor := middleware.Actor(info)
	viaSSO := info.AuthSubject != ""
	// A password session (one from before SSO-only, or break-glass since
	// turned off) cannot sign in to another connection with a password.
	if !viaSSO && h.passwordLoginBlocked() {
		writeError(w, http.StatusForbidden, errSSOOnly)
		return
	}

	var username, password, encryptedPwd string
	reused := false
	// perPerson is set when the target connection gives every SSO person
	// their own ClickHouse user. That user is provisioned below, once the
	// tunnel is known to be online, so an offline agent reports as offline
	// rather than as a provisioning failure.
	perPerson := false
	if viaSSO {
		identCfg, err := h.DB.GetSSOIdentityConfig(newConn.ID)
		if err != nil {
			slog.Error("Failed to read SSO identity config", "connection", newConn.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
			return
		}
		perPerson = identCfg.PerPerson()
		switch {
		case perPerson && info.SSOIdentityID == "":
			// A session from before per-person mode (or from a release that
			// did not record identities) carries no identity to provision.
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   "sso_reauth_required",
				"message": "Sign in again to use this connection.",
			})
			return
		case perPerson:
			// Provisioned after the tunnel check below.
		default:
			saUser, saEncrypted, err := h.DB.GetConnectionSSOAccount(newConn.ID)
			if err != nil {
				slog.Error("Failed to read SSO service account", "connection", newConn.ID, "error", err)
				writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
				return
			}
			if saUser == "" || saEncrypted == "" {
				writeJSON(w, http.StatusConflict, map[string]interface{}{
					"success": false,
					"error":   "sso_not_configured",
					"message": "SSO is not set up on this connection (no ClickHouse service account).",
				})
				return
			}
			saPassword, err := crypto.Decrypt(saEncrypted, h.Config.AppSecretKey)
			if err != nil {
				slog.Error("Failed to decrypt SSO service account", "connection", newConn.ID, "error", err)
				writeError(w, http.StatusInternalServerError, "Internal server error")
				return
			}
			username, password, encryptedPwd = saUser, saPassword, saEncrypted
		}
	} else {
		username = strings.TrimSpace(req.Username)
		if username == "" {
			username = existingSession.ClickhouseUser
		}
		password = req.Password
		if password == "" && username == existingSession.ClickhouseUser {
			// The stored password belongs to the current ClickHouse server.
			// Another server (or an agent run by someone else) must never
			// receive it: ask for that server's password instead, without
			// testing anything or recording a failure against the target.
			if !h.sameClickHouseServer(existingSession.ConnectionID, newConn) {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"success":        false,
					"error":          "Authentication failed",
					"reason":         "different_server",
					"message":        fmt.Sprintf("Different server, enter the password for %s.", username),
					"needs_password": true,
				})
				return
			}
			stored, err := crypto.Decrypt(existingSession.EncryptedPassword, h.Config.AppSecretKey)
			if err != nil {
				writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
					"success":        false,
					"error":          "Authentication failed",
					"message":        "Enter your password for this connection.",
					"needs_password": true,
				})
				return
			}
			password, reused = stored, true
		}
	}

	// Same budget as login: switching tests a username and password, so
	// without it this endpoint is an unthrottled password oracle. SSO people
	// are limited per person, not per shared service account.
	rateUser := username
	if viaSSO {
		rateUser = info.AuthSubject
	}
	clientIP := getClientIP(r)
	attempt := &credentialAttempt{rl: h.RateLimiter}
	defer attempt.release()
	if !attempt.reserveIPAndUser(w, clientIP, rateUser, newConn.ID) {
		return
	}

	// Check tunnel is online (retry up to 3 times).
	if !waitTunnelOnline(h.Gateway, newConn.ID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
			"success": false,
			"error":   "Connection offline",
			"message": "The tunnel agent for this connection is not online. Please check that the agent is running.",
		})
		return
	}

	if perPerson {
		// Create or re-sync the person's own ClickHouse user on the target.
		// Never fall back to the shared account when this fails.
		var res *ssoidentity.Result
		if h.Provisioner == nil {
			err = errors.New("no SSO provisioner configured")
		} else {
			res, err = h.Provisioner.SignInIdentity(r.Context(), newConn.ID, info.SSOIdentityID)
		}
		if err != nil {
			status, code, msg := ssoProvisionError(err, newConn.Name)
			slog.Warn("Switch connection failed: SSO user provisioning", "user", actor, "connection", newConn.Name, "error", err)
			h.auditLoginFailure(actor, newConn, clientIP, "SSO user provisioning failed (switch connection): "+err.Error())
			writeJSON(w, status, map[string]interface{}{
				"success": false,
				"error":   code,
				"message": msg,
			})
			return
		}
		password, err = crypto.Decrypt(res.EncryptedPassword, h.Config.AppSecretKey)
		if err != nil {
			slog.Error("Failed to decrypt provisioned SSO user password", "connection", newConn.ID, "error", err)
			writeError(w, http.StatusInternalServerError, "Internal server error")
			return
		}
		username, encryptedPwd = res.CHUser, res.EncryptedPassword
	}

	testResult, err := h.Gateway.TestConnection(newConn.ID, username, password, 15*time.Second)
	if err == nil && !testResult.Success {
		errMsg := testResult.Error
		if errMsg == "" {
			errMsg = "Invalid credentials"
		}
		err = errors.New(errMsg)
	}
	if err != nil {
		if classifyClickHouseAuthError(err.Error()) == chAuthUnreachable {
			slog.Info("Switch connection aborted: ClickHouse unreachable", "user", actor, "error", err)
			h.auditLoginFailure(actor, newConn, clientIP, "clickhouse unreachable (switch connection)")
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"success": false,
				"error":   "Connection error",
				"message": sanitizeClickHouseAuthMessage(err.Error()),
			})
			return
		}
		switch {
		case perPerson:
			// The user was just created or re-synced by the provisioner; a
			// refusal is a setup problem (host rule, profile), never counted.
			slog.Warn("Switch connection failed: provisioned SSO user rejected", "user", actor, "connection", newConn.Name, "error", err)
			h.auditLoginFailure(actor, newConn, clientIP, "provisioned sso user rejected (switch connection): "+sanitizeClickHouseAuthMessage(err.Error()))
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   "sso_user_rejected",
				"message": "ClickHouse rejected the user CH-UI provisioned for you on this connection. Ask an admin to check the audit log.",
			})
		case viaSSO:
			// A stored service account the target rejects is a setup
			// problem, not a guess by the caller: never counted.
			slog.Warn("Switch connection failed: SSO service account rejected", "user", actor, "connection", newConn.Name, "error", err)
			h.auditLoginFailure(actor, newConn, clientIP, "sso service account rejected (switch connection)")
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"success": false,
				"error":   "sso_account_rejected",
				"message": "ClickHouse rejected this connection's SSO service account. Ask an admin to check it.",
			})
		case reused:
			// The stored password is the one that already signed this person
			// in; a target that refuses it is not a guess either.
			slog.Info("Switch connection: current password rejected by target", "user", actor, "connection", newConn.Name)
			h.auditLoginFailure(actor, newConn, clientIP, "current password rejected (switch connection)")
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success":        false,
				"error":          "Authentication failed",
				"message":        "Invalid credentials",
				"needs_password": true,
			})
		default:
			attempt.failed()
			slog.Info("Switch connection failed: credentials rejected", "user", actor, "error", err)
			h.auditLoginFailure(actor, newConn, clientIP, "invalid credentials (switch connection)")
			writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
				"success": false,
				"error":   "Authentication failed",
				"message": sanitizeClickHouseAuthMessage(err.Error()),
			})
		}
		return
	}
	attempt.succeeded()

	var role, roleNote string
	if viaSSO {
		// The person's role came from the IdP at sign-in; it does not depend
		// on the connection.
		role = "viewer"
		if existingSession.UserRole != nil && *existingSession.UserRole != "" {
			role = *existingSession.UserRole
		}
	} else {
		role, roleNote = h.resolveUserRole(newConn.ID, username, password, clientIP)
		encryptedPwd, err = crypto.Encrypt(password, h.Config.AppSecretKey)
		if err != nil {
			slog.Error("Failed to encrypt password", "error", err)
			writeError(w, http.StatusInternalServerError, "Internal server error")
			return
		}
	}

	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(h.sessionDuration()).Format(time.RFC3339)

	_, err = h.DB.RotateSession(cookie.Value, database.CreateSessionParams{
		ConnectionID:      newConn.ID,
		ClickhouseUser:    username,
		EncryptedPassword: encryptedPwd,
		Token:             token,
		ExpiresAt:         expiresAt,
		UserRole:          role,
		AuthSubject:       info.AuthSubject,
		SSOIdentityID:     info.SSOIdentityID,
	})
	if errors.Is(err, database.ErrSessionChanged) {
		writeJSON(w, http.StatusConflict, map[string]interface{}{
			"success": false,
			"error":   "session_changed",
			"message": "Your session changed while switching. Reload and try again.",
		})
		return
	}
	if err != nil {
		slog.Error("Failed to rotate session", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}
	if viaSSO {
		// Report the role the new session acts with: the person's override,
		// capped on a connection created from the login page.
		if s, err := h.DB.GetSession(token); err == nil && s != nil {
			role, _ = h.DB.SessionRole(s)
		}
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.sessionDuration().Seconds()),
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(r, h.Config),
		SameSite: http.SameSiteLaxMode,
	})

	oldName := existingSession.ConnectionID
	if oldConn, err := h.DB.GetConnectionByID(existingSession.ConnectionID); err == nil && oldConn != nil {
		oldName = oldConn.Name
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "user.switch_connection",
		Username:       strPtr(actor),
		ClickhouseUser: strPtr(username),
		ConnectionID:   strPtr(newConn.ID),
		Details:        strPtr(fmt.Sprintf("Switched connection from %s to %s (role: %s)", oldName, newConn.Name, role)),
		IPAddress:      strPtr(clientIP),
	})

	slog.Info("User switched connection", "user", actor, "from", oldName, "to", newConn.Name, "role", role)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"user":               actor,
		"user_role":          role,
		"clickhouse_version": testResult.Version,
		"expires_at":         expiresAt,
		"via_sso":            viaSSO,
		"connection": map[string]interface{}{
			"id":     newConn.ID,
			"name":   newConn.Name,
			"online": true,
		},
		"session": map[string]interface{}{
			"user":             actor,
			"role":             role,
			"roleNote":         roleNote,
			"viaSSO":           viaSSO,
			"connectionId":     newConn.ID,
			"connectionName":   newConn.Name,
			"connectionOnline": true,
			"expiresAt":        expiresAt,
			"version":          testResult.Version,
			"appVersion":       version.Version,
		},
	})
}

// ---------- ClickHouse role detection ----------

// detectClickHouseRole classifies the ClickHouse user as admin/analyst/viewer.
// degraded=true means the classification came from a connection-level failure
// (tunnel offline, timeout) rather than the user's actual grants.
func (h *AuthHandler) detectClickHouseRole(connectionID, username, password string) (role string, degraded bool) {
	var err error
	_, err = h.Gateway.ExecuteQueryWithSettingsCtx(
		context.Background(),
		connectionID,
		"SELECT 1 FROM system.users LIMIT 1",
		username, password,
		nil,
		10*time.Second,
	)
	if err == nil {
		slog.Debug("Role detected as admin (system.users accessible)", "user", username)
		return "admin", false
	}

	errStr := err.Error()

	if !isPermissionError(errStr) {
		slog.Warn("Role detection degraded: could not verify privileges, defaulting to viewer", "user", username, "error", errStr)
		return "viewer", true
	}

	result, err := h.Gateway.ExecuteQueryWithSettingsCtx(
		context.Background(),
		connectionID,
		fmt.Sprintf("SELECT access_type FROM system.grants WHERE user_name = '%s'", escapeSingleQuotes(username)),
		username, password,
		nil,
		10*time.Second,
	)
	if err != nil {
		if isPermissionError(err.Error()) {
			slog.Debug("Role detected as viewer (system.grants not readable)", "user", username)
			return "viewer", false
		}
		slog.Warn("Role detection degraded: system.grants query failed, defaulting to viewer", "user", username, "error", err)
		return "viewer", true
	}

	role = classifyGrants(result)
	slog.Debug("Role detected from grants", "user", username, "role", role)
	return role, false
}

// resolveUserRole returns the effective app role plus a human-readable note
// when the outcome is surprising (implicit admin ignored, degraded detection)
// so the UI can tell the user instead of silently downgrading them.
func (h *AuthHandler) resolveUserRole(connectionID, username, password, clientIP string) (string, string) {
	// A connection created from the login page may point at a server the
	// caller runs, so it never carries an override, never bootstraps the
	// first admin, and never grants more than analyst.
	loginURL, err := h.DB.IsLoginURLConnection(connectionID)
	if err != nil {
		slog.Warn("Failed to read login URL connections; treating connection as one", "connection", connectionID, "error", err)
	}

	if !loginURL {
		manualRole, err := h.DB.GetUserRole(username, connectionID)
		if err == nil && manualRole != "" {
			slog.Debug("Using manually assigned role", "user", username, "connection", connectionID, "role", manualRole)
			return manualRole, ""
		}
		if err != nil {
			slog.Warn("Failed to read manual role override", "user", username, "connection", connectionID, "error", err)
		}
	}

	detectedRole, degraded := h.detectClickHouseRole(connectionID, username, password)
	if degraded {
		return detectedRole, "Could not verify your ClickHouse privileges (connection error) — defaulting to viewer for this session. Log in again to retry."
	}
	if loginURL {
		if detectedRole == "admin" {
			slog.Info("Capping role on a connection created from the login page", "user", username, "connection", connectionID)
			return database.CapLoginURLRole(detectedRole), "Connections added from the login page cannot grant CH-UI admin, so you are signed in as analyst. Ask an admin to sign in on a saved connection to manage CH-UI."
		}
		return detectedRole, ""
	}
	if detectedRole != "admin" {
		return detectedRole, ""
	}

	bootstrapped, err := h.DB.BootstrapFirstAdmin(username, connectionID)
	if err != nil {
		slog.Warn("Failed to bootstrap first admin role", "user", username, "error", err)
		return "viewer", ""
	}

	if bootstrapped {

		_ = h.DB.CreateAuditLog(database.AuditLogParams{
			Action:       "user_role.bootstrap_admin",
			Username:     strPtr(username),
			Details:      strPtr("Automatically granted first explicit admin role from ClickHouse admin login"),
			IPAddress:    strPtr(clientIP),
			ConnectionID: strPtr(connectionID),
		})

		slog.Info("Bootstrapped first explicit admin role", "user", username)
		return "admin", ""
	}

	slog.Info("Ignoring implicit ClickHouse admin privileges without explicit CH-UI admin override", "user", username)
	return "viewer", "Your ClickHouse user has admin privileges, but CH-UI admin is already assigned to another user. Ask an existing admin to grant you the admin role in Admin → Users, or run \"ch-ui reset-admin\" on the server."
}

func classifyGrants(result *tunnel.QueryResult) string {
	if result == nil || len(result.Data) == 0 {
		return "viewer"
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal(result.Data, &rows); err != nil {
		return "viewer"
	}

	adminGrants := map[string]bool{
		"ALL": true, "CREATE": true, "CREATE DATABASE": true,
		"CREATE TABLE": true, "ALTER": true, "DROP": true, "SYSTEM": true,
	}
	analystGrants := map[string]bool{
		"INSERT": true, "DELETE": true, "ALTER TABLE": true,
		"CREATE TEMPORARY TABLE": true,
	}

	hasAdmin := false
	hasAnalyst := false

	for _, row := range rows {
		accessType, ok := row["access_type"].(string)
		if !ok {
			continue
		}
		upper := strings.ToUpper(strings.TrimSpace(accessType))
		if adminGrants[upper] {
			hasAdmin = true
		}
		if analystGrants[upper] {
			hasAnalyst = true
		}
	}

	if hasAdmin {
		return "admin"
	}
	if hasAnalyst {
		return "analyst"
	}
	return "viewer"
}

func isPermissionError(errStr string) bool {
	lower := strings.ToLower(errStr)
	return strings.Contains(lower, "access_denied") ||
		strings.Contains(lower, "access denied") ||
		strings.Contains(lower, "not enough privileges") ||
		strings.Contains(lower, "permission denied") ||
		strings.Contains(lower, "code: 497")
}

func escapeSingleQuotes(s string) string {
	// ClickHouse uses '' (doubled single-quote) to escape, not \'
	// Also escape backslashes to prevent escape-sequence bypasses
	s = strings.ReplaceAll(s, "\\", "\\\\")
	return strings.ReplaceAll(s, "'", "''")
}

// ---------- helpers ----------

// getClientIP is the address rate limits and audit entries attribute the
// request to. Forwarding headers count only through a trusted proxy
// (config trusted_proxies); see middleware.RealIP.
func getClientIP(r *http.Request) string {
	return middleware.ClientIP(r)
}
