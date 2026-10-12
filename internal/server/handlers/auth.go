package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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
	r.Post("/switch-connection", h.SwitchConnection)
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
	resp := map[string]interface{}{
		"password_login":    true,
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
	Kind       string  `json:"kind"`
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

	authSubject := ""
	if session.AuthSubject != nil {
		authSubject = *session.AuthSubject
	}
	roleKey := database.RoleKey(session.ClickhouseUser, authSubject)

	role := "viewer"
	overrideRole, roleErr := h.DB.GetUserRole(roleKey)
	if roleErr != nil {
		slog.Warn("Failed to resolve explicit role for session", "user", roleKey, "error", roleErr)
	} else if overrideRole != "" {
		role = overrideRole
	} else if session.UserRole != nil {
		role = *session.UserRole
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

	hidden := hiddenLoginConnectionID(h.DB)
	result := make([]connectionInfo, 0, len(connections))
	for _, c := range connections {
		if c.ID == hidden {
			continue
		}
		result = append(result, connectionInfo{
			ID:         c.ID,
			Name:       c.Name,
			Status:     c.Status,
			Online:     h.Gateway.IsTunnelOnline(c.ID),
			Kind:       connectionKind(c),
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

func (h *AuthHandler) SwitchConnection(w http.ResponseWriter, r *http.Request) {
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
	if strings.TrimSpace(req.Username) == "" {
		req.Username = existingSession.ClickhouseUser
	}
	req.Username = strings.TrimSpace(req.Username)

	if req.ConnectionID == "" {
		writeError(w, http.StatusBadRequest, "connection_id (or connectionId) is required")
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

	// Same budget as login: switching tests a username and password, so
	// without it this endpoint is an unthrottled password oracle.
	clientIP := getClientIP(r)
	attempt := &credentialAttempt{rl: h.RateLimiter}
	defer attempt.release()
	if !attempt.reserveIPAndUser(w, clientIP, req.Username, newConn.ID) {
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

	testResult, err := h.Gateway.TestConnection(newConn.ID, req.Username, req.Password, 15*time.Second)
	if err == nil && !testResult.Success {
		errMsg := testResult.Error
		if errMsg == "" {
			errMsg = "Invalid credentials"
		}
		err = errors.New(errMsg)
	}
	if err != nil {
		if classifyClickHouseAuthError(err.Error()) == chAuthUnreachable {
			slog.Info("Switch connection aborted: ClickHouse unreachable", "user", req.Username, "error", err)
			writeJSON(w, http.StatusServiceUnavailable, map[string]interface{}{
				"success": false,
				"error":   "Connection error",
				"message": sanitizeClickHouseAuthMessage(err.Error()),
			})
			return
		}
		attempt.failed()
		slog.Info("Switch connection failed: credentials rejected", "user", req.Username, "error", err)
		h.auditLoginFailure(req.Username, newConn, clientIP, "invalid credentials (switch connection)")
		writeJSON(w, http.StatusUnauthorized, map[string]interface{}{
			"success": false,
			"error":   "Authentication failed",
			"message": sanitizeClickHouseAuthMessage(err.Error()),
		})
		return
	}
	attempt.succeeded()

	role, roleNote := h.resolveUserRole(newConn.ID, req.Username, req.Password, clientIP)

	encryptedPwd, err := crypto.Encrypt(req.Password, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to encrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Internal server error")
		return
	}

	if err := h.DB.DeleteSession(cookie.Value); err != nil {
		slog.Error("Failed to delete old session", "error", err)
	}

	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(h.sessionDuration()).Format(time.RFC3339)

	_, err = h.DB.CreateSession(database.CreateSessionParams{
		ConnectionID:      newConn.ID,
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

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.sessionDuration().Seconds()),
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(r, h.Config),
		SameSite: http.SameSiteLaxMode,
	})

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "user.switch_connection",
		Username:     strPtr(req.Username),
		ConnectionID: strPtr(newConn.ID),
		Details:      strPtr(fmt.Sprintf("Switched to connection %s (role: %s)", newConn.Name, role)),
		IPAddress:    strPtr(clientIP),
	})

	slog.Info("User switched connection", "user", req.Username, "connection", newConn.Name, "role", role)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":            true,
		"user":               req.Username,
		"user_role":          role,
		"clickhouse_version": testResult.Version,
		"expires_at":         expiresAt,
		"connection": map[string]interface{}{
			"id":     newConn.ID,
			"name":   newConn.Name,
			"online": true,
		},
		"session": map[string]interface{}{
			"user":             req.Username,
			"role":             role,
			"roleNote":         roleNote,
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
	_, err = h.Gateway.ExecuteQuery(
		connectionID,
		"SELECT 1 FROM system.users LIMIT 1",
		username, password,
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

	result, err := h.Gateway.ExecuteQuery(
		connectionID,
		fmt.Sprintf("SELECT access_type FROM system.grants WHERE user_name = '%s'", escapeSingleQuotes(username)),
		username, password,
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
	manualRole, err := h.DB.GetUserRole(username)
	if err == nil && manualRole != "" {
		slog.Debug("Using manually assigned role", "user", username, "role", manualRole)
		return manualRole, ""
	}
	if err != nil {
		slog.Warn("Failed to read manual role override", "user", username, "error", err)
	}

	detectedRole, degraded := h.detectClickHouseRole(connectionID, username, password)
	if degraded {
		return detectedRole, "Could not verify your ClickHouse privileges (connection error) — defaulting to viewer for this session. Log in again to retry."
	}
	if detectedRole != "admin" {
		return detectedRole, ""
	}

	adminCount, err := h.DB.CountUsersWithRole("admin")
	if err != nil {
		slog.Warn("Failed to count admin overrides; denying implicit admin", "user", username, "error", err)
		return "viewer", ""
	}

	if adminCount == 0 {
		if err := h.DB.SetUserRole(username, "admin"); err != nil {
			slog.Warn("Failed to bootstrap first admin role", "user", username, "error", err)
			return "viewer", ""
		}

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
