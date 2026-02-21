package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
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
}

// Routes returns a chi.Router with all auth routes mounted.
func (h *AuthHandler) Routes(r chi.Router) {
	r.Post("/login", h.Login)
	r.Post("/logout", h.Logout)
	r.Get("/session", h.Session)
	r.Get("/connections", h.Connections)
	r.Post("/switch-connection", h.SwitchConnection)
}

// ---------- request / response types ----------

type loginRequest struct {
	Username          string `json:"username"`
	Password          string `json:"password"`
	ConnectionID      string `json:"connectionId"`
	ConnectionIDSnake string `json:"connection_id"`
}

type switchConnectionRequest struct {
	ConnectionID      string `json:"connectionId"`
	ConnectionIDSnake string `json:"connection_id"`
	Username          string `json:"username"`
	Password          string `json:"password"`
}

type connectionInfo struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Status     string  `json:"status"`
	Online     bool    `json:"online"`
	LastSeenAt *string `json:"last_seen_at"`
	CreatedAt  string  `json:"created_at"`
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

// ---------- POST /login ----------

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	if strings.TrimSpace(req.Username) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Username is required"})
		return
	}
	req.ConnectionID = req.resolvedConnectionID()

	// --- Rate limiting ---
	clientIP := getClientIP(r)
	ipKey := fmt.Sprintf("ip:%s", clientIP)
	userKey := fmt.Sprintf("user:%s", req.Username)

	ipResult := h.RateLimiter.CheckAuthRateLimit(ipKey, "ip", MaxAttemptsPerIP, RateLimitWindow)
	if !ipResult.Allowed {
		retrySeconds := int(ipResult.RetryAfter.Seconds())
		slog.Warn("IP rate limited", "ip", clientIP, "retryAfter", retrySeconds)
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":      "Too many login attempts from this IP",
			"retryAfter": retrySeconds,
		})
		return
	}

	userResult := h.RateLimiter.CheckAuthRateLimit(userKey, "user", MaxAttemptsPerUser, RateLimitWindow)
	if !userResult.Allowed {
		retrySeconds := int(userResult.RetryAfter.Seconds())
		slog.Warn("User rate limited", "user", req.Username, "retryAfter", retrySeconds)
		writeJSON(w, http.StatusTooManyRequests, map[string]interface{}{
			"error":      "Too many login attempts for this user",
			"retryAfter": retrySeconds,
		})
		return
	}

	// --- Resolve connection ---
	connections, err := h.DB.GetConnections()
	if err != nil {
		slog.Error("Failed to get connections", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connections"})
		return
	}

	if len(connections) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{
			"error":   "No connections available",
			"message": "No connections are configured. Please set up an agent first.",
		})
		return
	}

	var conn *database.Connection
	if req.ConnectionID != "" {
		for i := range connections {
			if connections[i].ID == req.ConnectionID {
				conn = &connections[i]
				break
			}
		}
		if conn == nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection not found"})
			return
		}
	} else {
		conn = &connections[0]
	}

	// --- Check tunnel is online (retry up to 3 times) ---
	online := false
	for attempt := 0; attempt < 3; attempt++ {
		if h.Gateway.IsTunnelOnline(conn.ID) {
			online = true
			break
		}
		if attempt < 2 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if !online {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   "Connection offline",
			"message": "The tunnel agent for this connection is not online. Please check that the agent is running.",
		})
		return
	}

	// --- Test ClickHouse credentials ---
	testResult, err := h.Gateway.TestConnection(conn.ID, req.Username, req.Password, 15*time.Second)
	if err != nil {
		h.RateLimiter.RecordAttempt(ipKey, "ip")
		h.RateLimiter.RecordAttempt(userKey, "user")
		slog.Info("Login failed: connection test error", "user", req.Username, "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "Authentication failed",
			"message": fmt.Sprintf("Could not authenticate with ClickHouse: %s", err.Error()),
		})
		return
	}
	if !testResult.Success {
		h.RateLimiter.RecordAttempt(ipKey, "ip")
		h.RateLimiter.RecordAttempt(userKey, "user")
		errMsg := testResult.Error
		if errMsg == "" {
			errMsg = "Invalid credentials"
		}
		slog.Info("Login failed: bad credentials", "user", req.Username)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "Authentication failed",
			"message": errMsg,
		})
		return
	}

	// --- Resolve CH-UI role ---
	role := h.resolveUserRole(conn.ID, req.Username, req.Password, clientIP)

	// --- Encrypt password and create session ---
	encryptedPwd, err := crypto.Encrypt(req.Password, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to encrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}

	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(SessionDuration).Format(time.RFC3339)

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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
		return
	}

	// --- Set cookie ---
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionDuration.Seconds()),
		HttpOnly: true,
		Secure:   shouldUseSecureCookie(r, h.Config),
		SameSite: http.SameSiteLaxMode,
	})

	// --- Reset rate limits on success ---
	h.RateLimiter.ResetLimit(ipKey)
	h.RateLimiter.ResetLimit(userKey)

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
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:    "user.logout",
			Username:  strPtr(session.ClickhouseUser),
			IPAddress: strPtr(clientIP),
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Session lookup failed"})
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

	role := "viewer"
	overrideRole, roleErr := h.DB.GetUserRole(session.ClickhouseUser)
	if roleErr != nil {
		slog.Warn("Failed to resolve explicit role for session", "user", session.ClickhouseUser, "error", roleErr)
	} else if overrideRole != "" {
		role = overrideRole
	} else if session.UserRole != nil {
		role = *session.UserRole
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"authenticated": true,
		"user":          session.ClickhouseUser,
		"user_role":     role,
		"expires_at":    session.ExpiresAt,
		"connection": map[string]interface{}{
			"id":     session.ConnectionID,
			"name":   connName,
			"online": connOnline,
		},
		"session": map[string]interface{}{
			"user":             session.ClickhouseUser,
			"role":             role,
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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connections"})
		return
	}

	result := make([]connectionInfo, 0, len(connections))
	for _, c := range connections {
		result = append(result, connectionInfo{
			ID:         c.ID,
			Name:       c.Name,
			Status:     c.Status,
			Online:     h.Gateway.IsTunnelOnline(c.ID),
			LastSeenAt: c.LastSeenAt,
			CreatedAt:  c.CreatedAt,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"connections": result,
	})
}

// ---------- POST /switch-connection ----------

func (h *AuthHandler) SwitchConnection(w http.ResponseWriter, r *http.Request) {
	cookie, err := r.Cookie(SessionCookie)
	if err != nil || cookie.Value == "" {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	existingSession, err := h.DB.GetSession(cookie.Value)
	if err != nil || existingSession == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Session expired or invalid"})
		return
	}

	var req switchConnectionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	req.ConnectionID = req.resolvedConnectionID()
	if strings.TrimSpace(req.Username) == "" {
		req.Username = existingSession.ClickhouseUser
	}

	if req.ConnectionID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "connection_id (or connectionId) is required"})
		return
	}

	newConn, err := h.DB.GetConnectionByID(req.ConnectionID)
	if err != nil {
		slog.Error("Failed to get connection", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connection"})
		return
	}
	if newConn == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection not found"})
		return
	}

	// Check tunnel is online (retry up to 3 times).
	online := false
	for attempt := 0; attempt < 3; attempt++ {
		if h.Gateway.IsTunnelOnline(newConn.ID) {
			online = true
			break
		}
		if attempt < 2 {
			time.Sleep(500 * time.Millisecond)
		}
	}
	if !online {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{
			"error":   "Connection offline",
			"message": "The tunnel agent for this connection is not online. Please check that the agent is running.",
		})
		return
	}

	testResult, err := h.Gateway.TestConnection(newConn.ID, req.Username, req.Password, 15*time.Second)
	if err != nil {
		slog.Info("Switch connection failed: test error", "user", req.Username, "error", err)
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "Authentication failed",
			"message": fmt.Sprintf("Could not authenticate with ClickHouse: %s", err.Error()),
		})
		return
	}
	if !testResult.Success {
		errMsg := testResult.Error
		if errMsg == "" {
			errMsg = "Invalid credentials"
		}
		writeJSON(w, http.StatusUnauthorized, map[string]string{
			"error":   "Authentication failed",
			"message": errMsg,
		})
		return
	}

	clientIP := getClientIP(r)
	role := h.resolveUserRole(newConn.ID, req.Username, req.Password, clientIP)

	encryptedPwd, err := crypto.Encrypt(req.Password, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to encrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Internal server error"})
		return
	}

	if err := h.DB.DeleteSession(cookie.Value); err != nil {
		slog.Error("Failed to delete old session", "error", err)
	}

	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(SessionDuration).Format(time.RFC3339)

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
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create session"})
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(SessionDuration.Seconds()),
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

func (h *AuthHandler) detectClickHouseRole(connectionID, username, password string) string {
	var err error
	_, err = h.Gateway.ExecuteQuery(
		connectionID,
		"SELECT 1 FROM system.users LIMIT 1",
		username, password,
		10*time.Second,
	)
	if err == nil {
		slog.Debug("Role detected as admin (system.users accessible)", "user", username)
		return "admin"
	}

	errStr := err.Error()

	if !isPermissionError(errStr) {
		slog.Debug("Role defaulting to viewer (non-permission error from system.users)", "user", username, "error", errStr)
		return "viewer"
	}

	result, err := h.Gateway.ExecuteQuery(
		connectionID,
		fmt.Sprintf("SELECT access_type FROM system.grants WHERE user_name = '%s'", escapeSingleQuotes(username)),
		username, password,
		10*time.Second,
	)
	if err != nil {
		slog.Debug("Role defaulting to viewer (system.grants query failed)", "user", username, "error", err)
		return "viewer"
	}

	role := classifyGrants(result)
	slog.Debug("Role detected from grants", "user", username, "role", role)
	return role
}

func (h *AuthHandler) resolveUserRole(connectionID, username, password, clientIP string) string {
	manualRole, err := h.DB.GetUserRole(username)
	if err == nil && manualRole != "" {
		slog.Debug("Using manually assigned role", "user", username, "role", manualRole)
		return manualRole
	}
	if err != nil {
		slog.Warn("Failed to read manual role override", "user", username, "error", err)
	}

	detectedRole := h.detectClickHouseRole(connectionID, username, password)
	if detectedRole != "admin" {
		return detectedRole
	}

	adminCount, err := h.DB.CountUsersWithRole("admin")
	if err != nil {
		slog.Warn("Failed to count admin overrides; denying implicit admin", "user", username, "error", err)
		return "viewer"
	}

	if adminCount == 0 {
		if err := h.DB.SetUserRole(username, "admin"); err != nil {
			slog.Warn("Failed to bootstrap first admin role", "user", username, "error", err)
			return "viewer"
		}

		_ = h.DB.CreateAuditLog(database.AuditLogParams{
			Action:       "user_role.bootstrap_admin",
			Username:     strPtr(username),
			Details:      strPtr("Automatically granted first explicit admin role from ClickHouse admin login"),
			IPAddress:    strPtr(clientIP),
			ConnectionID: strPtr(connectionID),
		})

		slog.Info("Bootstrapped first explicit admin role", "user", username)
		return "admin"
	}

	slog.Info("Ignoring implicit ClickHouse admin privileges without explicit CH-UI admin override", "user", username)
	return "viewer"
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
	return strings.ReplaceAll(s, "'", "\\'")
}

// ---------- helpers ----------

func getClientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		parts := strings.SplitN(xff, ",", 2)
		ip := strings.TrimSpace(parts[0])
		if ip != "" {
			return ip
		}
	}

	if xri := r.Header.Get("X-Real-IP"); xri != "" {
		return strings.TrimSpace(xri)
	}

	addr := r.RemoteAddr
	if idx := strings.LastIndex(addr, ":"); idx != -1 {
		return addr[:idx]
	}
	return addr
}
