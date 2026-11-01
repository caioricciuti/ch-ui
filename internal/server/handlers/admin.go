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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	ghclient "github.com/caioricciuti/ch-ui/internal/github"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/safe"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// AdminHandler handles admin-only routes for ClickHouse management.
// All routes require the admin role, enforced by middleware.RequireAdmin.
type AdminHandler struct {
	DB           *database.DB
	Gateway      *tunnel.Gateway
	Config       *config.Config
	GovSyncer    *governance.Syncer
	GitHubSyncer *ghclient.Syncer
	OIDCManager  *oidc.Manager // live OIDC provider holder, reloaded on SSO config save
	// Provisioner drops the per-person ClickHouse users of an SSO person
	// whose access is removed.
	Provisioner *ssoidentity.Provisioner
}

// Routes registers all admin routes on the given chi.Router.
func (h *AdminHandler) Routes(r chi.Router) {
	r.Use(middleware.RequireAdmin(h.DB))

	r.Get("/users", h.GetUsers)
	r.Delete("/users/sso/{email}/access", h.RemoveSSOAccess)
	r.Get("/user-roles", h.GetUserRoles)
	r.Put("/user-roles/{username}", h.SetUserRole)
	r.Delete("/user-roles/{username}", h.DeleteUserRole)
	r.Get("/connections", h.GetConnections)
	r.Get("/stats", h.GetStats)
	r.Get("/clickhouse-users", h.GetClickHouseUsers)
	r.Post("/clickhouse-users", h.CreateClickHouseUser)
	r.Put("/clickhouse-users/{username}/password", h.UpdateClickHouseUserPassword)
	r.Delete("/clickhouse-users/{username}", h.DeleteClickHouseUser)

	// Brain admin management
	r.Get("/brain/providers", h.ListBrainProviders)
	r.Post("/brain/providers", h.CreateBrainProvider)
	r.Put("/brain/providers/{id}", h.UpdateBrainProvider)
	r.Delete("/brain/providers/{id}", h.DeleteBrainProvider)
	r.Post("/brain/providers/{id}/sync-models", h.SyncBrainProviderModels)
	r.Get("/brain/models", h.ListBrainModels)
	r.Put("/brain/models/{id}", h.UpdateBrainModel)
	r.Post("/brain/models/bulk", h.BulkUpdateBrainModels)
	r.Get("/brain/skills", h.ListBrainSkills)
	r.Post("/brain/skills", h.CreateBrainSkill)
	r.Put("/brain/skills/{id}", h.UpdateBrainSkill)
	h.registerBrainUsageRoutes(r)

	// Governance feature toggle
	r.Get("/governance/settings", h.GetGovernanceSettings)
	r.Put("/governance/settings", h.UpdateGovernanceSettings)

	// Data retention (SQLite history pruning)
	r.Get("/retention", h.GetRetentionSettings)
	r.Put("/retention", h.UpdateRetentionSettings)

	// SSO (OIDC) configuration (Pro)
	r.Get("/sso", h.GetSSOSettings)
	r.Put("/sso", h.UpdateSSOSettings)

	// GitHub model sync (Pro)
	r.Route("/github/{connectionId}", func(sub chi.Router) {
		sub.Get("/", h.GetGitHubIntegration)
		sub.Put("/", h.SaveGitHubIntegration)
		sub.Delete("/", h.DeleteGitHubIntegration)
		sub.Post("/test", h.TestGitHubConnection)
		sub.Post("/sync", h.TriggerGitHubSync)
		sub.Get("/logs", h.GetGitHubSyncLogs)
	})
}

// ---------- GET /users ----------

func (h *AdminHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	// A ClickHouse user's role is per connection, so the list is the
	// admin's current connection: its password users plus every SSO person.
	connectionID := ""
	if session := middleware.GetSession(r); session != nil {
		connectionID = session.ConnectionID
	}
	if connectionID == "" {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	users, err := h.DB.GetUsers(connectionID)
	if err != nil {
		slog.Error("Failed to get users", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve users")
		return
	}

	roleOverrides, err := h.DB.GetAllUserRoles()
	if err != nil {
		slog.Error("Failed to get user role overrides", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve user role overrides")
		return
	}

	userMap := make(map[string]database.SessionUser, len(users)+len(roleOverrides))
	for _, u := range users {
		userMap[u.Username] = u
	}
	for _, ov := range roleOverrides {
		if ov.ConnectionID != "" && ov.ConnectionID != connectionID {
			continue
		}
		if _, exists := userMap[ov.Username]; exists {
			continue
		}
		u := database.SessionUser{
			Username:       ov.Username,
			DisplayName:    ov.Username,
			ClickhouseUser: ov.Username,
			UserRole:       ov.Role,
			LastLogin:      "",
			SessionCount:   0,
		}
		if email, ok := strings.CutPrefix(ov.Username, database.SSORoleKeyPrefix); ok {
			u.DisplayName, u.ClickhouseUser, u.ViaSSO = email, "", true
		}
		userMap[ov.Username] = u
	}

	appUsers := make([]database.SessionUser, 0, len(userMap))
	for _, u := range userMap {
		appUsers = append(appUsers, u)
	}

	includeStale := false
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("include_stale"))) {
	case "1", "true", "yes":
		includeStale = true
	}

	// Per-person SSO: the ClickHouse users CH-UI created for each person,
	// keyed by email. Password users and people without one get [].
	chUsersByEmail, err := h.ssoClickHouseUsersByEmail()
	if err != nil {
		slog.Error("Failed to list per-person SSO users", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve users")
		return
	}
	provisioned := func(u database.SessionUser) []ssoClickHouseUserSummary {
		if u.ViaSSO {
			if list := chUsersByEmail[strings.ToLower(strings.TrimSpace(u.DisplayName))]; list != nil {
				return list
			}
		}
		return []ssoClickHouseUserSummary{}
	}

	existsMap, err := h.fetchCurrentClickHouseUsers(r)
	if err != nil {
		slog.Warn("Admin users: failed to compare with current ClickHouse users", "error", err)
		// Fallback to app-local users only if ClickHouse comparison fails.
		type fallbackUser struct {
			database.SessionUser
			ClickHouseUsers []ssoClickHouseUserSummary `json:"clickhouse_users"`
		}
		fallback := make([]fallbackUser, 0, len(appUsers))
		for _, u := range appUsers {
			fallback = append(fallback, fallbackUser{SessionUser: u, ClickHouseUsers: provisioned(u)})
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"users": fallback,
			"sync":  map[string]bool{"clickhouse_user_check": false},
		})
		return
	}

	type responseUser struct {
		database.SessionUser
		ExistsInClickHouse bool                       `json:"exists_in_clickhouse"`
		ClickHouseUsers    []ssoClickHouseUserSummary `json:"clickhouse_users"`
	}

	filtered := make([]responseUser, 0, len(appUsers))
	for _, u := range appUsers {
		// SSO people are not ClickHouse users (they query through the shared
		// service account), so they are never stale by this check.
		exists := u.ViaSSO || existsMap[u.ClickhouseUser]
		if !includeStale && !exists {
			continue
		}
		filtered = append(filtered, responseUser{
			SessionUser:        u,
			ExistsInClickHouse: exists,
			ClickHouseUsers:    provisioned(u),
		})
	}

	if filtered == nil {
		filtered = []responseUser{}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users": filtered,
		"sync": map[string]bool{
			"clickhouse_user_check": true,
		},
	})
}

func (h *AdminHandler) fetchCurrentClickHouseUsers(r *http.Request) (map[string]bool, error) {
	session := middleware.GetSession(r)
	if session == nil {
		return nil, fmt.Errorf("not authenticated")
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		return nil, fmt.Errorf("tunnel offline")
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials: %w", err)
	}

	result, err := h.Gateway.ExecuteQueryWithSettingsCtx(
		r.Context(),
		session.ConnectionID,
		"SELECT name FROM system.users ORDER BY name FORMAT JSON",
		session.ClickhouseUser,
		password,
		nil,
		20*time.Second,
	)
	if err != nil {
		return nil, err
	}

	var rows []map[string]interface{}
	if err := json.Unmarshal(result.Data, &rows); err != nil {
		return nil, fmt.Errorf("parse system.users result: %w", err)
	}

	exists := make(map[string]bool, len(rows))
	for _, row := range rows {
		if name, ok := row["name"].(string); ok && strings.TrimSpace(name) != "" {
			exists[name] = true
		}
	}
	return exists, nil
}

// ---------- GET /user-roles ----------

// Every override: SSO people's (connection_id "", all connections) and
// ClickHouse users' (one row per connection).
func (h *AdminHandler) GetUserRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.DB.GetAllUserRoles()
	if err != nil {
		slog.Error("Failed to get user roles", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve user roles")
		return
	}

	if roles == nil {
		roles = []database.UserRole{}
	}

	writeJSON(w, http.StatusOK, roles)
}

// roleUsernameParam returns the decoded {username} path parameter, for role
// and ClickHouse user routes alike. Names can hold ':' and '@' ("sso:<email>",
// or a ClickHouse user named like an email), which the UI percent-encodes;
// chi returns the raw escaped segment.
func roleUsernameParam(r *http.Request) (string, bool) {
	username, err := url.PathUnescape(chi.URLParam(r, "username"))
	if err != nil || strings.TrimSpace(username) == "" {
		return "", false
	}
	return username, true
}

// ---------- PUT /user-roles/{username} ----------

func (h *AdminHandler) SetUserRole(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	username, ok := roleUsernameParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Username is required")
		return
	}

	var body struct {
		Role string `json:"role"`
		// ConnectionID scopes a ClickHouse user's override; it defaults to
		// the admin's current connection and is ignored for "sso:<email>".
		ConnectionID string `json:"connection_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	body.Role = strings.ToLower(strings.TrimSpace(body.Role))

	validRoles := map[string]bool{"admin": true, "analyst": true, "viewer": true}
	if !validRoles[body.Role] {
		writeError(w, http.StatusBadRequest, "Role must be one of: admin, analyst, viewer")
		return
	}
	connectionID := roleConnectionID(session, username, body.ConnectionID)

	isTargetAdmin, err := h.DB.IsUserRole(username, connectionID, "admin")
	if err != nil {
		slog.Error("Failed checking current role assignment", "error", err, "user", username)
		writeError(w, http.StatusInternalServerError, "Failed to validate current role")
		return
	}
	if isTargetAdmin && body.Role != "admin" {
		adminCount, err := h.DB.CountUsersWithRole("admin")
		if err != nil {
			slog.Error("Failed counting admins", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to validate admin safety rule")
			return
		}
		if adminCount <= 1 {
			writeError(w, http.StatusBadRequest, "Cannot remove the last admin. Assign another admin first.")
			return
		}
	}

	if err := h.DB.SetUserRole(username, connectionID, body.Role); err != nil {
		if msg, ok := roleScopeError(err); ok {
			writeError(w, http.StatusBadRequest, msg)
			return
		}
		slog.Error("Failed to set user role", "error", err, "user", username)
		writeError(w, http.StatusInternalServerError, "Failed to set user role")
		return
	}
	if err := h.DB.SetSessionsUserRole(username, connectionID, body.Role); err != nil {
		slog.Warn("Failed to refresh active session roles after role update", "error", err, "user", username)
	}

	var actorName, chUser *string
	if session != nil {
		actorName, chUser = strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "user_role.set",
		Username:       actorName,
		ClickhouseUser: chUser,
		ConnectionID:   nonEmptyPtr(connectionID),
		Details:        strPtr(fmt.Sprintf("Set role for %q to %s %s", username, body.Role, h.roleScopeLabel(connectionID))),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message":       "User role updated",
		"username":      username,
		"role":          body.Role,
		"connection_id": connectionID,
	})
}

// ---------- DELETE /user-roles/{username} ----------

func (h *AdminHandler) DeleteUserRole(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	username, ok := roleUsernameParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "Username is required")
		return
	}

	connectionID := roleConnectionID(session, username, r.URL.Query().Get("connection_id"))

	isTargetAdmin, err := h.DB.IsUserRole(username, connectionID, "admin")
	if err != nil {
		slog.Error("Failed checking current role assignment", "error", err, "user", username)
		writeError(w, http.StatusInternalServerError, "Failed to validate current role")
		return
	}
	if isTargetAdmin {
		adminCount, err := h.DB.CountUsersWithRole("admin")
		if err != nil {
			slog.Error("Failed counting admins", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to validate admin safety rule")
			return
		}
		if adminCount <= 1 {
			writeError(w, http.StatusBadRequest, "Cannot remove the last admin. Assign another admin first.")
			return
		}
	}

	if err := h.DB.DeleteUserRole(username, connectionID); err != nil {
		slog.Error("Failed to delete user role", "error", err, "user", username)
		writeError(w, http.StatusInternalServerError, "Failed to delete user role")
		return
	}
	// Without the override the role is whatever sign-in derives (SSO group
	// mapping, ClickHouse privileges, the login-page cap), and the sessions'
	// cached role may be the override itself. Sign those sessions out so the
	// next sign-in computes it, and make the person's OAuth MCP tokens
	// follow that sign-in instead of the role they were issued with.
	ended, err := h.DB.DeleteSessionsForRoleKey(username, connectionID)
	if err != nil {
		slog.Warn("Failed to end sessions after role override removal", "error", err, "user", username)
	}
	if err := h.DB.ClearOAuthGrantRoles(username, connectionID); err != nil {
		slog.Warn("Failed to reset OAuth token roles after role override removal", "error", err, "user", username)
	}

	var actorName, chUser *string
	if session != nil {
		actorName, chUser = strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "user_role.deleted",
		Username:       actorName,
		ClickhouseUser: chUser,
		ConnectionID:   nonEmptyPtr(connectionID),
		Details:        strPtr(fmt.Sprintf("Removed role override for %q %s; %d session(s) signed out", username, h.roleScopeLabel(connectionID), ended)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]any{
		"message":        "User role override removed. Their role is worked out again at their next sign-in.",
		"username":       username,
		"connection_id":  connectionID,
		"sessions_ended": ended,
	})
}

// roleConnectionID is the connection a role change applies on: "" for an SSO
// person (all connections), else the requested connection or, when none is
// named, the admin's current one.
func roleConnectionID(session *middleware.SessionInfo, roleKey, requested string) string {
	if database.IsSSORoleKey(roleKey) {
		return ""
	}
	if requested = strings.TrimSpace(requested); requested != "" {
		return requested
	}
	if session != nil {
		return session.ConnectionID
	}
	return ""
}

// roleScopeError maps the scope errors of SetUserRole to a client message.
func roleScopeError(err error) (string, bool) {
	switch {
	case errors.Is(err, database.ErrRoleConnectionRequired):
		return "A connection is required to set a ClickHouse user's role", true
	case errors.Is(err, database.ErrRoleConnectionNotFound):
		return "Connection not found", true
	case errors.Is(err, database.ErrRoleLoginURLConnection):
		return "Connections added from the login page cannot carry role overrides", true
	}
	return "", false
}

// nonEmptyPtr is strPtr, or nil for "".
func nonEmptyPtr(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}

// roleScopeLabel describes where an override applies, for audit details.
func (h *AdminHandler) roleScopeLabel(connectionID string) string {
	if connectionID == "" {
		return "on all connections"
	}
	name := connectionID
	if c, err := h.DB.GetConnectionByID(connectionID); err == nil && c != nil {
		name = c.Name
	}
	return fmt.Sprintf("on connection %q", name)
}

// ---------- GET /connections ----------

func (h *AdminHandler) GetConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.DB.GetConnections()
	if err != nil {
		slog.Error("Failed to list connections", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to retrieve connections")
		return
	}

	type connInfo struct {
		ID        string  `json:"id"`
		Name      string  `json:"name"`
		Status    string  `json:"status"`
		Online    bool    `json:"online"`
		CreatedAt string  `json:"created_at"`
		LastSeen  *string `json:"last_seen_at,omitempty"`
	}

	results := make([]connInfo, 0, len(conns))
	for _, c := range conns {
		results = append(results, connInfo{
			ID:        c.ID,
			Name:      c.Name,
			Status:    c.Status,
			Online:    h.Gateway.IsTunnelOnline(c.ID),
			CreatedAt: c.CreatedAt,
			LastSeen:  c.LastSeenAt,
		})
	}

	writeJSON(w, http.StatusOK, results)
}

// ---------- GET /stats ----------

func (h *AdminHandler) GetStats(w http.ResponseWriter, r *http.Request) {
	users, err := h.DB.GetUsers("")
	if err != nil {
		slog.Error("Failed to get users for stats", "error", err)
		users = []database.SessionUser{}
	}

	conns, err := h.DB.GetConnections()
	if err != nil {
		slog.Error("Failed to get connections for stats", "error", err)
		conns = []database.Connection{}
	}

	onlineCount := 0
	for _, c := range conns {
		if h.Gateway.IsTunnelOnline(c.ID) {
			onlineCount++
		}
	}

	auditLogs, err := h.DB.GetAuditLogs(1000)
	if err != nil {
		slog.Error("Failed to get audit logs for stats", "error", err)
		auditLogs = []database.AuditLog{}
	}

	loginCount := 0
	queryCount := 0
	for _, log := range auditLogs {
		switch log.Action {
		case "user.login":
			loginCount++
		case "query.execute":
			queryCount++
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"users_count": len(users),
		"connections": len(conns),
		"online":      onlineCount,
		"login_count": loginCount,
		"query_count": queryCount,
	})
}

// ---------- GET /clickhouse-users ----------

func (h *AdminHandler) GetClickHouseUsers(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel is offline")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	query := "SELECT name, storage, auth_type, host_ip, host_names, default_roles_all, default_roles_list, default_roles_except FROM system.users ORDER BY name FORMAT JSON"

	result, err := h.Gateway.ExecuteQueryWithSettingsCtx(
		r.Context(),
		session.ConnectionID,
		query,
		session.ClickhouseUser,
		password,
		nil,
		30*time.Second,
	)
	if err != nil {
		slog.Warn("Failed to query system.users", "error", err, "connection", session.ConnectionID)
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": result.Data,
		"meta": result.Meta,
	})
}

// ---------- POST /clickhouse-users ----------

func (h *AdminHandler) CreateClickHouseUser(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel is offline")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	var body struct {
		Name string `json:"name"`
		// Auth carries a hash made in the browser; the server never sees
		// the password.
		Auth *chUserAuth `json:"auth"`
		// Password and AuthType were removed in v2.33.0 and are refused;
		// they are read only to say so.
		Password     string   `json:"password"`
		AuthType     string   `json:"auth_type"`
		DefaultRoles []string `json:"default_roles"`
		IfNotExists  *bool    `json:"if_not_exists"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "name is required")
		return
	}

	auth, errMsg := resolveChUserAuth(body.Auth, body.AuthType, body.Password, false)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	allRoles, roleNames, parseErr := parseDefaultRolesInput(body.DefaultRoles)
	if parseErr != nil {
		writeError(w, http.StatusBadRequest, parseErr.Error())
		return
	}

	// Always a plain CREATE USER: with IF NOT EXISTS an existing account
	// would silently pass and then receive the requested roles. The
	// if_not_exists field is accepted for compatibility and ignored.
	createSQLStr := buildCreateChUserSQL(name, auth)
	shownCreateSQL := auth.mask(createSQLStr)
	executedCommands := []string{shownCreateSQL}

	if _, err := h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), session.ConnectionID, createSQLStr, session.ClickhouseUser, password, nil, 30*time.Second); err != nil {
		if isAccessEntityExistsError(err) {
			writeError(w, http.StatusConflict, fmt.Sprintf("ClickHouse user %q already exists", name))
			return
		}
		errText := auth.mask(err.Error())
		slog.Warn("Failed to create ClickHouse user", "error", errText, "connection", session.ConnectionID, "name", name)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s\n\nCommand:\n%s", errText, shownCreateSQL))
		return
	}

	escapedRoles := make([]string, 0, len(roleNames))
	for _, role := range roleNames {
		escapedRoles = append(escapedRoles, safe.Identifier(role))
	}

	// Apply role membership/default role as follow-up statements for broad ClickHouse compatibility.
	if len(escapedRoles) > 0 {
		grantSQL := "GRANT " + strings.Join(escapedRoles, ", ") + " TO " + safe.Identifier(name)
		executedCommands = append(executedCommands, grantSQL)
		if _, err := h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), session.ConnectionID, grantSQL, session.ClickhouseUser, password, nil, 30*time.Second); err != nil {
			slog.Warn("ClickHouse user created but role grant failed", "error", err, "connection", session.ConnectionID, "name", name)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("user created but failed to grant roles: %v\n\nCommand:\n%s", err, grantSQL))
			return
		}
	}

	if allRoles || len(escapedRoles) > 0 {
		defaultRoleClause := "ALL"
		if !allRoles {
			defaultRoleClause = strings.Join(escapedRoles, ", ")
		}
		alterSQL := "ALTER USER " + safe.Identifier(name) + " DEFAULT ROLE " + defaultRoleClause
		executedCommands = append(executedCommands, alterSQL)
		if _, err := h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), session.ConnectionID, alterSQL, session.ClickhouseUser, password, nil, 30*time.Second); err != nil {
			slog.Warn("ClickHouse user created but default role assignment failed", "error", err, "connection", session.ConnectionID, "name", name)
			writeError(w, http.StatusBadGateway, fmt.Sprintf("user created but failed to set default role: %v\n\nCommand:\n%s", err, alterSQL))
			return
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "admin.clickhouse_user.created",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(fmt.Sprintf("name=%s auth_type=%s", name, auth.Method)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"name":     name,
		"command":  shownCreateSQL,
		"commands": executedCommands,
	})
}

// ---------- PUT /clickhouse-users/{username}/password ----------

func (h *AdminHandler) UpdateClickHouseUserPassword(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel is offline")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	username, ok := roleUsernameParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}

	var body struct {
		Auth *chUserAuth `json:"auth"`
		// Removed in v2.33.0 and refused; read only to say so.
		Password string `json:"password"`
		AuthType string `json:"auth_type"`
		IfExists *bool  `json:"if_exists"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	auth, errMsg := resolveChUserAuth(body.Auth, body.AuthType, body.Password, true)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	ifExists := body.IfExists == nil || *body.IfExists
	var b strings.Builder
	b.WriteString("ALTER USER ")
	if ifExists {
		b.WriteString("IF EXISTS ")
	}
	b.WriteString(safe.Identifier(username))
	b.WriteString(auth.clause(true))
	shownSQL := auth.mask(b.String())

	if _, err := h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), session.ConnectionID, b.String(), session.ClickhouseUser, password, nil, 30*time.Second); err != nil {
		errText := auth.mask(err.Error())
		slog.Warn("Failed to update ClickHouse user password", "error", errText, "connection", session.ConnectionID, "name", username)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s\n\nCommand:\n%s", errText, shownSQL))
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "admin.clickhouse_user.password_changed",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(fmt.Sprintf("name=%s auth_type=%s", username, auth.Method)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"command": shownSQL,
	})
}

// chUserAuth is how a ClickHouse user created or changed from Admin → Users
// signs in. Method is sha256_hash (Hash 64 hex, optional Salt),
// double_sha1_hash (40 hex), bcrypt_hash, or no_password. The browser makes
// the hash, so the server never sees a password; Hash and Salt are secrets
// all the same and are masked in everything returned, logged or audited.
type chUserAuth struct {
	Method string `json:"method"`
	Hash   string `json:"hash,omitempty"`
	Salt   string `json:"salt,omitempty"`
}

var (
	chUserHexRe    = regexp.MustCompile(`^[0-9a-fA-F]+$`)
	chUserSaltRe   = regexp.MustCompile(`^[A-Za-z0-9._~+/=-]{1,128}$`)
	chUserBcryptRe = regexp.MustCompile(`^\$2[aby]?\$[0-9]{2}\$[./A-Za-z0-9]{53}$`)
)

func (a chUserAuth) validate() error {
	switch a.Method {
	case "sha256_hash":
		if len(a.Hash) != 64 || !chUserHexRe.MatchString(a.Hash) {
			return fmt.Errorf("a sha256 hash is 64 hex characters")
		}
		if a.Salt != "" && !chUserSaltRe.MatchString(a.Salt) {
			return fmt.Errorf("the salt may use letters, digits and . _ ~ + / = - (at most 128)")
		}
	case "double_sha1_hash":
		if len(a.Hash) != 40 || !chUserHexRe.MatchString(a.Hash) {
			return fmt.Errorf("a double SHA-1 hash is 40 hex characters")
		}
	case "bcrypt_hash":
		if !chUserBcryptRe.MatchString(a.Hash) {
			return fmt.Errorf("a bcrypt hash looks like $2b$12$ followed by 53 characters")
		}
	case "no_password":
		if a.Hash != "" || a.Salt != "" {
			return fmt.Errorf("no_password takes no hash or salt")
		}
	default:
		return fmt.Errorf("auth.method must be one of: sha256_hash, double_sha1_hash, bcrypt_hash, no_password")
	}
	if a.Salt != "" && a.Method != "sha256_hash" {
		return fmt.Errorf("only sha256_hash takes a salt")
	}
	return nil
}

// clause is the IDENTIFIED clause, or "" for no_password on CREATE (the
// ClickHouse default). alter makes no_password explicit for ALTER USER.
func (a chUserAuth) clause(alter bool) string {
	if a.Method == "no_password" {
		if alter {
			return " IDENTIFIED WITH no_password"
		}
		return ""
	}
	s := " IDENTIFIED WITH " + a.Method + " BY '" + safe.Literal(a.Hash) + "'"
	if a.Salt != "" {
		s += " SALT '" + safe.Literal(a.Salt) + "'"
	}
	return s
}

// mask hides every secret of the auth in SQL or error text before it is
// returned to the browser or logged: IDENTIFIED BY and SALT literals become
// '***', and any other copy (ClickHouse may quote the statement back in an
// error) becomes ***.
func (a chUserAuth) mask(text string) string {
	for _, secret := range []string{a.Hash, a.Salt} {
		if secret != "" {
			text = strings.ReplaceAll(text, safe.Literal(secret), "***")
			text = strings.ReplaceAll(text, secret, "***")
		}
	}
	text = redactIdentifiedRe.ReplaceAllString(text, "$1'***'")
	return chUserSaltLiteralRe.ReplaceAllString(text, "$1'***'")
}

var chUserSaltLiteralRe = regexp.MustCompile(`(?i)(\bSALT\s+)('(?:[^'\\]|\\.)*')`)

// errRemovedPasswordFields answers a request that still sends the
// plaintext fields removed in v2.33.0. They are refused rather than
// ignored: ignoring them would create a user without a password.
const errRemovedPasswordFields = "password and auth_type were removed in v2.33.0: send auth with a hash made by the client (see /docs/admin#create-user)"

// resolveChUserAuth checks the auth of a create or password change and
// returns it, or an error message for a 400. A create without auth makes a
// user without a password, as before; a password change needs auth.
func resolveChUserAuth(hashed *chUserAuth, authType, password string, alter bool) (chUserAuth, string) {
	if password != "" || authType != "" {
		return chUserAuth{}, errRemovedPasswordFields
	}
	if hashed == nil {
		if alter {
			return chUserAuth{}, "auth is required"
		}
		return chUserAuth{Method: "no_password"}, ""
	}
	a := chUserAuth{Method: strings.TrimSpace(strings.ToLower(hashed.Method)), Hash: strings.TrimSpace(hashed.Hash), Salt: strings.TrimSpace(hashed.Salt)}
	if err := a.validate(); err != nil {
		return a, err.Error()
	}
	return a, ""
}

func parseDefaultRolesInput(input []string) (all bool, roles []string, err error) {
	seen := make(map[string]struct{}, len(input))
	for _, raw := range input {
		role := strings.TrimSpace(raw)
		if role == "" {
			continue
		}
		if strings.EqualFold(role, "ALL") {
			all = true
			continue
		}
		key := strings.ToLower(role)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		roles = append(roles, role)
	}
	if all && len(roles) > 0 {
		return false, nil, fmt.Errorf("default_roles cannot mix ALL with named roles")
	}
	return all, roles, nil
}

// ---------- DELETE /clickhouse-users/{username} ----------

func (h *AdminHandler) DeleteClickHouseUser(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeError(w, http.StatusServiceUnavailable, "Tunnel is offline")
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to decrypt credentials")
		return
	}

	username, ok := roleUsernameParam(r)
	if !ok {
		writeError(w, http.StatusBadRequest, "username is required")
		return
	}
	if username == session.ClickhouseUser {
		writeError(w, http.StatusBadRequest, "cannot delete current session user")
		return
	}

	ifExists := true
	if raw := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("if_exists"))); raw == "false" || raw == "0" {
		ifExists = false
	}

	sql := "DROP USER "
	if ifExists {
		sql += "IF EXISTS "
	}
	sql += safe.Identifier(username)

	if _, err := h.Gateway.ExecuteQueryWithSettingsCtx(r.Context(), session.ConnectionID, sql, session.ClickhouseUser, password, nil, 30*time.Second); err != nil {
		slog.Warn("Failed to delete ClickHouse user", "error", err, "connection", session.ConnectionID, "name", username)
		writeError(w, http.StatusBadGateway, fmt.Sprintf("%s\n\nCommand:\n%s", err.Error(), sql))
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "admin.clickhouse_user.deleted",
		Username:       strPtr(middleware.Actor(session)),
		ClickhouseUser: &session.ClickhouseUser,
		ConnectionID:   strPtr(session.ConnectionID),
		Details:        strPtr(fmt.Sprintf("name=%s", username)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"command": sql,
	})
}

// ---------- Helpers ----------

// buildCreateChUserSQL returns a plain CREATE USER statement (never IF NOT
// EXISTS), so creating a name that already exists fails instead of granting
// roles to someone else's account.
func buildCreateChUserSQL(name string, auth chUserAuth) string {
	return "CREATE USER " + safe.Identifier(name) + auth.clause(false)
}

// isAccessEntityExistsError reports ClickHouse error 493
// (ACCESS_ENTITY_ALREADY_EXISTS), returned by CREATE USER for a taken name.
func isAccessEntityExistsError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "ACCESS_ENTITY_ALREADY_EXISTS") || strings.Contains(msg, "Code: 493.")
}

func escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `''`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}
