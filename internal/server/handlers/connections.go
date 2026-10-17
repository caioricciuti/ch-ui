package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/embedded"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
	"github.com/caioricciuti/ch-ui/internal/tokens"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// ConnectionsHandler handles connection management routes.
type ConnectionsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	Agents  *embedded.Manager
	// RateLimiter is shared with AuthHandler. nil creates one on first use.
	RateLimiter *middleware.RateLimiter
	// Provisioner verifies per-person SSO provisioning accounts.
	Provisioner *ssoidentity.Provisioner

	limiterOnce sync.Once
}

// validateClickHouseURL checks that a direct connection target is a usable
// http(s) URL.
func validateClickHouseURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("clickhouse_url is required for direct connections")
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", fmt.Errorf("clickhouse_url must be a valid http:// or https:// URL")
	}
	return strings.TrimRight(raw, "/"), nil
}

// connectionResponse is a connection as any signed-in user sees it, with live
// status. Fields are listed explicitly: the tunnel token lets its holder
// replace the agent, so it is only returned by the admin token endpoints.
type connectionResponse struct {
	ID            string     `json:"id"`
	Name          string     `json:"name"`
	IsEmbedded    bool       `json:"is_embedded"`
	Type          string     `json:"type"`
	ClickHouseURL string     `json:"clickhouse_url,omitempty"`
	Status        string     `json:"status"`
	LastSeenAt    *string    `json:"last_seen_at"`
	CreatedAt     string     `json:"created_at"`
	Online        bool       `json:"online"`
	LastSeen      *time.Time `json:"last_seen,omitempty"`
	HostInfo      any        `json:"host_info,omitempty"`
}

// List returns all connections.
// GET /
func (h *ConnectionsHandler) List(w http.ResponseWriter, r *http.Request) {
	conns, err := h.DB.GetConnections()
	if err != nil {
		slog.Error("Failed to list connections", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connections")
		return
	}

	results := make([]connectionResponse, 0, len(conns))
	for _, c := range conns {
		results = append(results, h.buildConnectionResponse(c))
	}

	writeJSON(w, http.StatusOK, results)
}

// SetSSOAccount stores the connection's SSO ClickHouse account: in shared
// mode the service account every OIDC session queries as, in per-person mode
// the provisioner that creates and syncs each person's own user. Admin only.
// PUT /{id}/sso-account  { "username": "...", "password": "..." }
func (h *ConnectionsHandler) SetSSOAccount(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}

	encrypted, err := crypto.Encrypt(req.Password, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to encrypt SSO service account password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to store credentials")
		return
	}

	if err := h.DB.SetConnectionSSOAccount(id, req.Username, encrypted); err != nil {
		slog.Error("Failed to set SSO service account", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to save SSO service account")
		return
	}

	accountRole := "service account"
	if cfg, err := h.DB.GetSSOIdentityConfig(id); err == nil && cfg.PerPerson() {
		accountRole = "provisioner"
	}
	username, chUser := sessionActor(r)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "connection.sso_account.set",
		Username:       username,
		ClickhouseUser: chUser,
		ConnectionID:   strPtr(id),
		Details:        strPtr(fmt.Sprintf("SSO ClickHouse %s set to %s", accountRole, req.Username)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

// sessionActor returns the person and ClickHouse user behind the request's
// session for audit rows, nil when there is no session.
func sessionActor(r *http.Request) (username, chUser *string) {
	if session := middleware.GetSession(r); session != nil {
		return strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
	}
	return nil, nil
}

// GetSSOIdentity returns how SSO people are identified to ClickHouse on this
// connection: the saved identity config (shared mode by default) and whether
// the SSO account that mode relies on is set. Admin only.
// GET /{id}/sso-identity
func (h *ConnectionsHandler) GetSSOIdentity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}
	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	h.writeSSOIdentity(w, id)
}

// SetSSOIdentity saves the connection's SSO identity config. Per-person mode
// is a Pro feature; shared mode (the default) saves on every edition.
// PUT /{id}/sso-identity  body: database.SSOIdentityConfig as JSON
func (h *ConnectionsHandler) SetSSOIdentity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}
	var cfg database.SSOIdentityConfig
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&cfg); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	cfg.Normalize()
	if cfg.PerPerson() && !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	if err := h.DB.SetSSOIdentityConfig(id, cfg); err != nil {
		slog.Error("Failed to save SSO identity config", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to save SSO identity settings")
		return
	}

	username, chUser := sessionActor(r)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "connection.sso_identity.updated",
		Username:       username,
		ClickhouseUser: chUser,
		ConnectionID:   strPtr(id),
		Details:        strPtr(fmt.Sprintf("SSO identity mode set to %s (user prefix %s, %d group mapping(s))", cfg.Mode, cfg.UserPrefix, len(cfg.RoleMappings))),
		IPAddress:      strPtr(getClientIP(r)),
	})
	h.writeSSOIdentity(w, id)
}

func (h *ConnectionsHandler) writeSSOIdentity(w http.ResponseWriter, connID string) {
	cfg, err := h.DB.GetSSOIdentityConfig(connID)
	if err != nil {
		slog.Error("Failed to read SSO identity config", "error", err, "id", connID)
		writeError(w, http.StatusInternalServerError, "Failed to read SSO identity settings")
		return
	}
	ssoUser, ssoPassword, err := h.DB.GetConnectionSSOAccount(connID)
	if err != nil {
		slog.Error("Failed to read SSO account", "error", err, "id", connID)
		writeError(w, http.StatusInternalServerError, "Failed to read SSO identity settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{
		"config":           cfg,
		"provisioner_user": ssoUser,
		"provisioner_set":  ssoUser != "" && ssoPassword != "",
	})
}

// ssoIdentityProbeTimeout bounds one verify run: nine or so statements, each
// with its own statement timeout inside the provisioner.
const ssoIdentityProbeTimeout = 60 * time.Second

// ssoIdentityConfigForRequest returns the posted config, normalized, or the
// connection's saved one when the request carries none.
func (h *ConnectionsHandler) ssoIdentityConfigForRequest(connID string, posted *database.SSOIdentityConfig) (database.SSOIdentityConfig, error) {
	if posted == nil {
		return h.DB.GetSSOIdentityConfig(connID)
	}
	cfg := *posted
	cfg.Normalize()
	return cfg, nil
}

// SSOIdentitySetupSQL renders the admin setup script for a per-person
// config: the roles, the optional profile, the provisioning account and its
// grants. The config comes from the request so the admin can see the script
// for what they are about to save; it falls back to the saved config. Only
// text is produced, so every edition gets it. Admin only.
// POST /{id}/sso-identity/setup-sql  { "config": {...}, "provisioner_user": "chui_provisioner" }
func (h *ConnectionsHandler) SSOIdentitySetupSQL(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}
	var req struct {
		Config          *database.SSOIdentityConfig `json:"config"`
		ProvisionerUser string                      `json:"provisioner_user"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	cfg, err := h.ssoIdentityConfigForRequest(id, req.Config)
	if err != nil {
		slog.Error("Failed to read SSO identity config", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to read SSO identity settings")
		return
	}
	prov := strings.TrimSpace(req.ProvisionerUser)
	if prov == "" {
		if saved, _, err := h.DB.GetConnectionSSOAccount(id); err == nil {
			prov = saved
		}
	}
	if prov == "" {
		prov = "chui_provisioner"
	}
	writeJSON(w, http.StatusOK, map[string]interface{}{"sql": ssoidentity.SetupSQL(cfg, prov)})
}

// VerifySSOIdentity probes a provisioning account against a per-person
// config on the connection's live ClickHouse: sign-in, host detection, the
// SHOW USERS grant, a throw-away user under the prefix with every managed
// role, and that the account is refused outside the prefix. Nothing real is
// touched and the probe user is dropped again. The config and the account
// may come from the request (not saved yet) or fall back to the saved ones.
// Pro and admin only.
// POST /{id}/sso-identity/verify
//
//	{ "config": {...}, "provisioner": { "username": "...", "password": "..." } }
func (h *ConnectionsHandler) VerifySSOIdentity(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}
	var req struct {
		Config      *database.SSOIdentityConfig `json:"config"`
		Provisioner *struct {
			Username string `json:"username"`
			Password string `json:"password"`
		} `json:"provisioner"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	cfg, err := h.ssoIdentityConfigForRequest(id, req.Config)
	if err != nil {
		slog.Error("Failed to read SSO identity config", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to read SSO identity settings")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !cfg.PerPerson() {
		writeError(w, http.StatusBadRequest, "Verification applies to per-person mode; shared mode has no provisioner")
		return
	}

	var user, password string
	if req.Provisioner != nil && strings.TrimSpace(req.Provisioner.Username) != "" {
		user, password = strings.TrimSpace(req.Provisioner.Username), req.Provisioner.Password
	} else {
		savedUser, enc, err := h.DB.GetConnectionSSOAccount(id)
		if err != nil {
			slog.Error("Failed to read SSO account", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to read the provisioning account")
			return
		}
		if savedUser == "" || enc == "" {
			writeError(w, http.StatusBadRequest, "No provisioning account: save one on the connection or send its credentials to verify")
			return
		}
		password, err = crypto.Decrypt(enc, h.Config.AppSecretKey)
		if err != nil {
			slog.Error("Failed to decrypt SSO account password", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to read the provisioning account")
			return
		}
		user = savedUser
	}
	if !h.Gateway.IsTunnelOnline(id) {
		writeError(w, http.StatusServiceUnavailable, "Connection is offline")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), ssoIdentityProbeTimeout)
	defer cancel()
	checks := h.Provisioner.Probe(ctx, id, cfg, user, password)
	ok := ssoidentity.AllOK(checks)
	failed := 0
	for _, c := range checks {
		if !c.OK {
			failed++
		}
	}

	username, chUser := sessionActor(r)
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "connection.sso_identity.verified",
		Username:       username,
		ClickhouseUser: chUser,
		ConnectionID:   strPtr(id),
		Details:        strPtr(fmt.Sprintf("SSO provisioner %s verified: ok=%t, %d of %d check(s) failed", user, ok, failed, len(checks))),
		IPAddress:      strPtr(getClientIP(r)),
	})
	writeJSON(w, http.StatusOK, map[string]interface{}{"ok": ok, "checks": checks})
}

// Get returns a single connection by ID.
// GET /{id}
func (h *ConnectionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}

	writeJSON(w, http.StatusOK, h.buildConnectionResponse(*conn))
}

// Create creates a new connection. Two shapes:
//   - tunnel (default): {"name": "..."} — returns a token + agent setup steps.
//   - direct: {"name": "...", "type": "direct", "clickhouse_url": "http://host:8123"}
//     — starts an in-process connector immediately, no agent required.
//
// POST /
func (h *ConnectionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name          string `json:"name"`
		Type          string `json:"type"`
		ClickHouseURL string `json:"clickhouse_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "Connection name is required")
		return
	}

	connType := strings.TrimSpace(strings.ToLower(body.Type))
	if connType == "" {
		connType = database.ConnectionTypeTunnel
	}
	if connType != database.ConnectionTypeTunnel && connType != database.ConnectionTypeDirect {
		writeError(w, http.StatusBadRequest, "type must be \"direct\" or \"tunnel\"")
		return
	}

	chURL := ""
	if connType == database.ConnectionTypeDirect {
		validated, err := validateClickHouseURL(body.ClickHouseURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		chURL = validated
	}

	id, token, err := createConnectionRecord(h.DB, name, connType, chURL)
	if err != nil {
		slog.Error("Failed to create connection", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create connection")
		return
	}

	session := middleware.GetSession(r)
	var username, chUser *string
	if session != nil {
		username, chUser = strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "connection.created",
		Username:       username,
		ClickhouseUser: chUser,
		ConnectionID:   strPtr(id),
		Details:        strPtr(fmt.Sprintf("Created %s connection %q", connType, name)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil || conn == nil {
		slog.Error("Failed to retrieve created connection", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Connection created but failed to retrieve")
		return
	}

	if connType == database.ConnectionTypeDirect {
		startDirectConnector(h.Agents, *conn)
		writeJSON(w, http.StatusCreated, map[string]interface{}{
			"connection": h.buildConnectionResponse(*conn),
		})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"connection":         h.buildConnectionResponse(*conn),
		"tunnel_token":       token,
		"setup_instructions": getSetupInstructions(h.Config, token),
	})
}

// Update renames a connection and, for direct connections, changes the target
// ClickHouse URL (restarting the in-process connector). The embedded
// connection is managed by server config and cannot be edited here.
// PUT /{id}  { "name"?: "...", "clickhouse_url"?: "..." }
func (h *ConnectionsHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for update", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}
	if conn.IsEmbedded {
		writeError(w, http.StatusBadRequest, "The embedded connection is managed by server configuration (clickhouse_url / connection_name)")
		return
	}

	var body struct {
		Name          *string `json:"name"`
		ClickHouseURL *string `json:"clickhouse_url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	var changes []string
	if body.Name != nil {
		name := strings.TrimSpace(*body.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "Connection name cannot be empty")
			return
		}
		change, err := renameConnection(h.DB, conn, name)
		if err != nil {
			slog.Error("Failed to rename connection", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to rename connection")
			return
		}
		if change != "" {
			changes = append(changes, change)
		}
	}

	if body.ClickHouseURL != nil {
		if conn.Type != database.ConnectionTypeDirect {
			writeError(w, http.StatusBadRequest, "clickhouse_url can only be set on direct connections")
			return
		}
		validated, err := validateClickHouseURL(*body.ClickHouseURL)
		if err != nil {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		change, err := setConnectionURL(h.DB, conn, validated)
		if err != nil {
			slog.Error("Failed to update connection URL", "error", err, "id", id)
			writeError(w, http.StatusInternalServerError, "Failed to update connection URL")
			return
		}
		if change != "" {
			changes = append(changes, change)
			startDirectConnector(h.Agents, *conn) // restart with the new target
		}
	}

	if len(changes) > 0 {
		session := middleware.GetSession(r)
		var username, chUser *string
		if session != nil {
			username, chUser = strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
		}
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:         "connection.updated",
			Username:       username,
			ClickhouseUser: chUser,
			ConnectionID:   strPtr(id),
			Details:        strPtr(fmt.Sprintf("Updated connection: %s", strings.Join(changes, "; "))),
			IPAddress:      strPtr(getClientIP(r)),
		})
	}

	writeJSON(w, http.StatusOK, h.buildConnectionResponse(*conn))
}

// Delete deletes a connection by ID.
// DELETE /{id}
func (h *ConnectionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for deletion", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}

	// Stop the in-process connector before removing a direct connection.
	if conn.Type == database.ConnectionTypeDirect && h.Agents != nil {
		h.Agents.StopConnection(id)
	}

	if err := h.DB.DeleteConnection(id); err != nil {
		slog.Error("Failed to delete connection", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to delete connection")
		return
	}
	details := fmt.Sprintf("Deleted connection %q", conn.Name)
	if h.Gateway.DisconnectConnection(id) {
		details += "; live agent disconnected"
	}

	session := middleware.GetSession(r)
	var username, chUser *string
	if session != nil {
		username, chUser = strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "connection.deleted",
		Username:       username,
		ClickhouseUser: chUser,
		ConnectionID:   strPtr(id),
		Details:        strPtr(details),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "Connection deleted successfully"})
}

// TestConnection tests ClickHouse credentials against a connection. Admin
// only: it takes any username and password, so it shares the login rate
// limit and counts rejected credentials like a failed sign-in.
// POST /{id}/test
//
// 200 success, 400 credentials rejected, 403 not an admin, 404 unknown
// connection, 429 rate limited, 502 ClickHouse unreachable, 503 agent offline.
func (h *ConnectionsHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil || session.UserRole != "admin" {
		writeError(w, http.StatusForbidden, "Admin access required")
		return
	}

	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for test", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	username := strings.TrimSpace(body.Username)
	password := body.Password
	if username == "" {
		username = "default"
	}

	attempt := &credentialAttempt{rl: h.limiter()}
	defer attempt.release()
	if !attempt.reserveIPAndUser(w, getClientIP(r), username, id) {
		return
	}

	if !h.Gateway.IsTunnelOnline(id) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel is not connected. Please ensure the agent is running.")
		return
	}

	result, err := h.Gateway.TestConnection(id, username, password, 15*time.Second)
	if err == nil && !result.Success {
		msg := result.Error
		if msg == "" {
			msg = "Invalid credentials"
		}
		err = errors.New(msg)
	}
	if err != nil {
		slog.Warn("Connection test failed", "error", err, "id", id)
		if classifyClickHouseAuthError(err.Error()) == chAuthUnreachable {
			writeError(w, http.StatusBadGateway, sanitizeClickHouseAuthMessage(err.Error()))
			return
		}
		attempt.failed()
		writeError(w, http.StatusBadRequest, sanitizeClickHouseAuthMessage(err.Error()))
		return
	}
	attempt.succeeded()

	writeJSON(w, http.StatusOK, result)
}

// limiter returns the rate limiter shared with login. Attempts are stored in
// SQLite, so a fallback limiter (when none is wired) still sees the same
// lockouts; only in-flight reservations are per instance.
func (h *ConnectionsHandler) limiter() *middleware.RateLimiter {
	h.limiterOnce.Do(func() {
		if h.RateLimiter == nil {
			h.RateLimiter = middleware.NewRateLimiter(h.DB)
		}
	})
	return h.RateLimiter
}

// GetToken returns the tunnel token for a connection.
// GET /{id}/token
func (h *ConnectionsHandler) GetToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for token", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tunnel_token":       conn.TunnelToken,
		"setup_instructions": getSetupInstructions(h.Config, conn.TunnelToken),
	})
}

// RegenerateToken generates a new tunnel token for a connection.
// POST /{id}/regenerate-token
func (h *ConnectionsHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "Connection ID is required")
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for token regeneration", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connection")
		return
	}
	if conn == nil {
		writeError(w, http.StatusNotFound, "Connection not found")
		return
	}

	newToken := tokens.GenerateTunnelToken()

	if err := h.DB.UpdateConnectionToken(id, newToken); err != nil {
		slog.Error("Failed to regenerate token", "error", err, "id", id)
		writeError(w, http.StatusInternalServerError, "Failed to regenerate token")
		return
	}
	details := fmt.Sprintf("Regenerated token for connection %q", conn.Name)
	if h.Gateway.DisconnectConnection(id) {
		details += "; live agent disconnected"
	}
	if conn.Type == database.ConnectionTypeDirect {
		// The in-process connector holds the old token; restart it with the new one.
		conn.TunnelToken = newToken
		startDirectConnector(h.Agents, *conn)
	}

	session := middleware.GetSession(r)
	var username, chUser *string
	if session != nil {
		username, chUser = strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "connection.token_regenerated",
		Username:       username,
		ClickhouseUser: chUser,
		ConnectionID:   strPtr(id),
		Details:        strPtr(details),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tunnel_token":       newToken,
		"setup_instructions": getSetupInstructions(h.Config, newToken),
		"message":            "Token regenerated successfully. The previous token is now invalid and any agent using it was disconnected.",
	})
}

// createConnectionRecord stores a new connection with a fresh tunnel token.
// chURL must already be validated for direct connections. Shared by Create and
// first-run setup.
func createConnectionRecord(db *database.DB, name, connType, chURL string) (id, token string, err error) {
	token = tokens.GenerateTunnelToken()
	id, err = db.CreateConnection(database.CreateConnectionParams{
		Name:          name,
		TunnelToken:   token,
		Type:          connType,
		ClickHouseURL: chURL,
	})
	if err != nil {
		return "", "", err
	}
	return id, token, nil
}

// renameConnection sets conn's name when it differs and returns the audit
// change line, or "" when nothing changed. name must already be trimmed and
// non-empty.
func renameConnection(db *database.DB, conn *database.Connection, name string) (string, error) {
	if name == conn.Name {
		return "", nil
	}
	if err := db.UpdateConnectionName(conn.ID, name); err != nil {
		return "", err
	}
	change := fmt.Sprintf("renamed %q to %q", conn.Name, name)
	conn.Name = name
	return change, nil
}

// setConnectionURL points a direct connection at an already validated
// ClickHouse URL and returns the audit change line, or "" when the URL is
// unchanged. It does not restart the connector; callers do that.
func setConnectionURL(db *database.DB, conn *database.Connection, validated string) (string, error) {
	if validated == conn.ClickHouseURL {
		return "", nil
	}
	if err := db.UpdateConnectionClickHouseURL(conn.ID, validated); err != nil {
		return "", err
	}
	conn.ClickHouseURL = validated
	return fmt.Sprintf("clickhouse_url set to %s", validated), nil
}

// startDirectConnector starts, or restarts, the in-process connector for a
// direct connection. agents is nil when no manager is wired (tests).
func startDirectConnector(agents *embedded.Manager, conn database.Connection) {
	if agents != nil {
		agents.StartConnection(conn)
	}
}

// buildConnectionResponse enriches a Connection with live status from the gateway.
func (h *ConnectionsHandler) buildConnectionResponse(c database.Connection) connectionResponse {
	resp := connectionResponse{
		ID:            c.ID,
		Name:          c.Name,
		IsEmbedded:    c.IsEmbedded,
		Type:          c.Type,
		ClickHouseURL: c.ClickHouseURL,
		Status:        c.Status,
		LastSeenAt:    c.LastSeenAt,
		CreatedAt:     c.CreatedAt,
	}

	online, lastSeen := h.Gateway.GetTunnelStatus(c.ID)
	resp.Online = online
	if online && !lastSeen.IsZero() {
		resp.LastSeen = &lastSeen
	}

	if c.HostInfoJSON != nil && *c.HostInfoJSON != "" {
		var hostInfo database.HostInfo
		if err := json.Unmarshal([]byte(*c.HostInfoJSON), &hostInfo); err == nil {
			resp.HostInfo = hostInfo
		}
	}

	return resp
}

// getSetupInstructions returns the agent commands for a tunnel token. The
// server URL comes from app_url (http to ws, https to wss). A loopback or
// missing app_url keeps the <YOUR_SERVER_URL> placeholder: an agent on
// another machine cannot use localhost.
func getSetupInstructions(cfg *config.Config, token string) map[string]string {
	server := "<YOUR_SERVER_URL>"
	if cfg != nil {
		if ws := agentServerURL(cfg.AppURL); ws != "" {
			server = ws
		}
	}
	return map[string]string{
		"connect": fmt.Sprintf("ch-ui connect --url %s/connect --key %s", server, token),
		"service": fmt.Sprintf("ch-ui service install --url %s/connect --key %s", server, token),
	}
}

// agentServerURL converts app_url to the websocket base an agent dials, or
// "" when app_url is empty, unparsable or points at this machine.
func agentServerURL(appURL string) string {
	u, err := url.Parse(strings.TrimSpace(appURL))
	if err != nil || u.Host == "" {
		return ""
	}
	switch u.Scheme {
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return ""
	}
	host := u.Hostname()
	if host == "localhost" || host == "0.0.0.0" || host == "::1" || strings.HasPrefix(host, "127.") {
		return ""
	}
	u.RawQuery, u.Fragment = "", ""
	return strings.TrimRight(u.String(), "/")
}
