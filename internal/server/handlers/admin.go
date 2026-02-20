package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// AdminHandler handles admin-only routes for ClickHouse management.
// All routes require the admin role, enforced by middleware.RequireAdmin.
type AdminHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
}

// Routes registers all admin routes on the given chi.Router.
func (h *AdminHandler) Routes(r chi.Router) {
	r.Use(middleware.RequireAdmin(h.DB))

	r.Get("/audit-logs", h.GetAuditLogs)
	r.Get("/users", h.GetUsers)
	r.Get("/user-roles", h.GetUserRoles)
	r.Put("/user-roles/{username}", h.SetUserRole)
	r.Delete("/user-roles/{username}", h.DeleteUserRole)
	r.Get("/connections", h.GetConnections)
	r.Get("/stats", h.GetStats)
	r.Get("/clickhouse-users", h.GetClickHouseUsers)
	r.Post("/clickhouse-users", h.CreateClickHouseUser)
	r.Put("/clickhouse-users/{username}/password", h.UpdateClickHouseUserPassword)
	r.Delete("/clickhouse-users/{username}", h.DeleteClickHouseUser)
	r.Get("/clickhouse-query-log", h.GetClickHouseQueryLog)

	// Alerting admin management
	r.Get("/alerts/channels", h.ListAlertChannels)
	r.Post("/alerts/channels", h.CreateAlertChannel)
	r.Put("/alerts/channels/{id}", h.UpdateAlertChannel)
	r.Delete("/alerts/channels/{id}", h.DeleteAlertChannel)
	r.Post("/alerts/channels/{id}/test", h.TestAlertChannel)
	r.Get("/alerts/rules", h.ListAlertRules)
	r.Post("/alerts/rules", h.CreateAlertRule)
	r.Put("/alerts/rules/{id}", h.UpdateAlertRule)
	r.Delete("/alerts/rules/{id}", h.DeleteAlertRule)
	r.Get("/alerts/events", h.ListAlertEvents)

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
}

// ---------- GET /audit-logs ----------

func (h *AdminHandler) GetAuditLogs(w http.ResponseWriter, r *http.Request) {
	limit := 100
	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	timeRange := strings.TrimSpace(r.URL.Query().Get("timeRange"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	username := strings.TrimSpace(r.URL.Query().Get("username"))
	search := strings.TrimSpace(r.URL.Query().Get("search"))

	logs, err := h.DB.GetAuditLogsFiltered(limit, timeRange, action, username, search)
	if err != nil {
		slog.Error("Failed to get audit logs", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve audit logs"})
		return
	}

	if logs == nil {
		logs = []database.AuditLog{}
	}

	type auditLogResponse struct {
		database.AuditLog
		ParsedDetails interface{} `json:"parsed_details,omitempty"`
	}

	results := make([]auditLogResponse, 0, len(logs))
	for _, log := range logs {
		entry := auditLogResponse{AuditLog: log}
		if log.Details != nil && *log.Details != "" {
			var parsed interface{}
			if err := json.Unmarshal([]byte(*log.Details), &parsed); err == nil {
				entry.ParsedDetails = parsed
			}
		}
		results = append(results, entry)
	}

	writeJSON(w, http.StatusOK, results)
}

// ---------- GET /users ----------

func (h *AdminHandler) GetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := h.DB.GetUsers()
	if err != nil {
		slog.Error("Failed to get users", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve users"})
		return
	}

	roleOverrides, err := h.DB.GetAllUserRoles()
	if err != nil {
		slog.Error("Failed to get user role overrides", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve user role overrides"})
		return
	}

	userMap := make(map[string]database.SessionUser, len(users)+len(roleOverrides))
	for _, u := range users {
		userMap[u.Username] = u
	}
	for _, ov := range roleOverrides {
		if _, exists := userMap[ov.Username]; exists {
			continue
		}
		userMap[ov.Username] = database.SessionUser{
			Username:     ov.Username,
			UserRole:     ov.Role,
			LastLogin:    "",
			SessionCount: 0,
		}
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

	existsMap, err := h.fetchCurrentClickHouseUsers(r)
	if err != nil {
		slog.Warn("Admin users: failed to compare with current ClickHouse users", "error", err)
		// Fallback to app-local users only if ClickHouse comparison fails.
		if appUsers == nil {
			appUsers = []database.SessionUser{}
		}
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"users": appUsers,
			"sync":  map[string]bool{"clickhouse_user_check": false},
		})
		return
	}

	type responseUser struct {
		database.SessionUser
		ExistsInClickHouse bool `json:"exists_in_clickhouse"`
	}

	filtered := make([]responseUser, 0, len(appUsers))
	for _, u := range appUsers {
		exists := existsMap[u.Username]
		if !includeStale && !exists {
			continue
		}
		filtered = append(filtered, responseUser{
			SessionUser:        u,
			ExistsInClickHouse: exists,
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

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		"SELECT name FROM system.users ORDER BY name FORMAT JSON",
		session.ClickhouseUser,
		password,
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

func (h *AdminHandler) GetUserRoles(w http.ResponseWriter, r *http.Request) {
	roles, err := h.DB.GetAllUserRoles()
	if err != nil {
		slog.Error("Failed to get user roles", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve user roles"})
		return
	}

	if roles == nil {
		roles = []database.UserRole{}
	}

	writeJSON(w, http.StatusOK, roles)
}

// ---------- PUT /user-roles/{username} ----------

func (h *AdminHandler) SetUserRole(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	username := chi.URLParam(r, "username")
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Username is required"})
		return
	}

	var body struct {
		Role string `json:"role"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}
	body.Role = strings.ToLower(strings.TrimSpace(body.Role))

	validRoles := map[string]bool{"admin": true, "analyst": true, "viewer": true}
	if !validRoles[body.Role] {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Role must be one of: admin, analyst, viewer"})
		return
	}

	isTargetAdmin, err := h.DB.IsUserRole(username, "admin")
	if err != nil {
		slog.Error("Failed checking current role assignment", "error", err, "user", username)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to validate current role"})
		return
	}
	if isTargetAdmin && body.Role != "admin" {
		adminCount, err := h.DB.CountUsersWithRole("admin")
		if err != nil {
			slog.Error("Failed counting admins", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to validate admin safety rule"})
			return
		}
		if adminCount <= 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Cannot remove the last admin. Assign another admin first.",
			})
			return
		}
	}

	if err := h.DB.SetUserRole(username, body.Role); err != nil {
		slog.Error("Failed to set user role", "error", err, "user", username)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to set user role"})
		return
	}
	if err := h.DB.SetSessionsUserRole(username, body.Role); err != nil {
		slog.Warn("Failed to refresh active session roles after role update", "error", err, "user", username)
	}

	var actorName *string
	if session != nil {
		actorName = strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "user_role.set",
		Username:  actorName,
		Details:   strPtr(fmt.Sprintf("Set role for %q to %s", username, body.Role)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message":  "User role updated",
		"username": username,
		"role":     body.Role,
	})
}

// ---------- DELETE /user-roles/{username} ----------

func (h *AdminHandler) DeleteUserRole(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	username := chi.URLParam(r, "username")
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Username is required"})
		return
	}

	isTargetAdmin, err := h.DB.IsUserRole(username, "admin")
	if err != nil {
		slog.Error("Failed checking current role assignment", "error", err, "user", username)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to validate current role"})
		return
	}
	if isTargetAdmin {
		adminCount, err := h.DB.CountUsersWithRole("admin")
		if err != nil {
			slog.Error("Failed counting admins", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to validate admin safety rule"})
			return
		}
		if adminCount <= 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{
				"error": "Cannot remove the last admin. Assign another admin first.",
			})
			return
		}
	}

	if err := h.DB.DeleteUserRole(username); err != nil {
		slog.Error("Failed to delete user role", "error", err, "user", username)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete user role"})
		return
	}
	if err := h.DB.SetSessionsUserRole(username, "viewer"); err != nil {
		slog.Warn("Failed to refresh active session roles after role override removal", "error", err, "user", username)
	}

	var actorName *string
	if session != nil {
		actorName = strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "user_role.deleted",
		Username:  actorName,
		Details:   strPtr(fmt.Sprintf("Removed role override for %q", username)),
		IPAddress: strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]string{
		"message":  "User role override removed",
		"username": username,
	})
}

// ---------- GET /connections ----------

func (h *AdminHandler) GetConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.DB.GetConnections()
	if err != nil {
		slog.Error("Failed to list connections", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connections"})
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
	users, err := h.DB.GetUsers()
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	query := "SELECT name, storage, auth_type, host_ip, host_names, default_roles_all, default_roles_list, default_roles_except FROM system.users ORDER BY name FORMAT JSON"

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID,
		query,
		session.ClickhouseUser,
		password,
		30*time.Second,
	)
	if err != nil {
		slog.Warn("Failed to query system.users", "error", err, "connection", session.ConnectionID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	var body struct {
		Name         string   `json:"name"`
		Password     string   `json:"password"`
		AuthType     string   `json:"auth_type"`
		DefaultRoles []string `json:"default_roles"`
		IfNotExists  *bool    `json:"if_not_exists"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "name is required"})
		return
	}

	authType := strings.TrimSpace(strings.ToLower(body.AuthType))
	if authType == "" {
		if strings.TrimSpace(body.Password) == "" {
			authType = "no_password"
		} else {
			authType = "sha256_password"
		}
	}
	switch authType {
	case "no_password", "plaintext_password", "sha256_password", "double_sha1_password":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "auth_type must be one of: no_password, plaintext_password, sha256_password, double_sha1_password"})
		return
	}
	if authType != "no_password" && strings.TrimSpace(body.Password) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password is required for selected auth_type"})
		return
	}

	allRoles, roleNames, parseErr := parseDefaultRolesInput(body.DefaultRoles)
	if parseErr != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": parseErr.Error()})
		return
	}

	var createSQL strings.Builder
	createSQL.WriteString("CREATE USER ")
	if body.IfNotExists == nil || *body.IfNotExists {
		createSQL.WriteString("IF NOT EXISTS ")
	}
	createSQL.WriteString(escapeIdentifier(name))
	createSQL.WriteString(buildClickHouseCreateAuthClause(authType, body.Password))
	createSQLStr := createSQL.String()
	executedCommands := []string{createSQLStr}

	if _, err := h.Gateway.ExecuteQuery(session.ConnectionID, createSQLStr, session.ClickhouseUser, password, 30*time.Second); err != nil {
		slog.Warn("Failed to create ClickHouse user", "error", err, "connection", session.ConnectionID, "name", name)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("%s\n\nCommand:\n%s", err.Error(), createSQLStr)})
		return
	}

	escapedRoles := make([]string, 0, len(roleNames))
	for _, role := range roleNames {
		escapedRoles = append(escapedRoles, escapeIdentifier(role))
	}

	// Apply role membership/default role as follow-up statements for broad ClickHouse compatibility.
	if len(escapedRoles) > 0 {
		grantSQL := "GRANT " + strings.Join(escapedRoles, ", ") + " TO " + escapeIdentifier(name)
		executedCommands = append(executedCommands, grantSQL)
		if _, err := h.Gateway.ExecuteQuery(session.ConnectionID, grantSQL, session.ClickhouseUser, password, 30*time.Second); err != nil {
			slog.Warn("ClickHouse user created but role grant failed", "error", err, "connection", session.ConnectionID, "name", name)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("user created but failed to grant roles: %v\n\nCommand:\n%s", err, grantSQL)})
			return
		}
	}

	if allRoles || len(escapedRoles) > 0 {
		defaultRoleClause := "ALL"
		if !allRoles {
			defaultRoleClause = strings.Join(escapedRoles, ", ")
		}
		alterSQL := "ALTER USER " + escapeIdentifier(name) + " DEFAULT ROLE " + defaultRoleClause
		executedCommands = append(executedCommands, alterSQL)
		if _, err := h.Gateway.ExecuteQuery(session.ConnectionID, alterSQL, session.ClickhouseUser, password, 30*time.Second); err != nil {
			slog.Warn("ClickHouse user created but default role assignment failed", "error", err, "connection", session.ConnectionID, "name", name)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("user created but failed to set default role: %v\n\nCommand:\n%s", err, alterSQL)})
			return
		}
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "admin.clickhouse_user.created",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("name=%s auth_type=%s", name, authType)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"success":  true,
		"name":     name,
		"command":  createSQLStr,
		"commands": executedCommands,
	})
}

// ---------- PUT /clickhouse-users/{username}/password ----------

func (h *AdminHandler) UpdateClickHouseUserPassword(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	username := strings.TrimSpace(chi.URLParam(r, "username"))
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}

	var body struct {
		Password string `json:"password"`
		AuthType string `json:"auth_type"`
		IfExists *bool  `json:"if_exists"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	authType := strings.TrimSpace(strings.ToLower(body.AuthType))
	if authType == "" {
		authType = "sha256_password"
	}
	switch authType {
	case "no_password", "plaintext_password", "sha256_password", "double_sha1_password":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "auth_type must be one of: no_password, plaintext_password, sha256_password, double_sha1_password"})
		return
	}
	if authType != "no_password" && strings.TrimSpace(body.Password) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "password is required for selected auth_type"})
		return
	}

	ifExists := body.IfExists == nil || *body.IfExists
	var b strings.Builder
	b.WriteString("ALTER USER ")
	if ifExists {
		b.WriteString("IF EXISTS ")
	}
	b.WriteString(escapeIdentifier(username))
	b.WriteString(buildClickHouseAlterAuthClause(authType, body.Password))

	if _, err := h.Gateway.ExecuteQuery(session.ConnectionID, b.String(), session.ClickhouseUser, password, 30*time.Second); err != nil {
		slog.Warn("Failed to update ClickHouse user password", "error", err, "connection", session.ConnectionID, "name", username)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("%s\n\nCommand:\n%s", err.Error(), b.String())})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "admin.clickhouse_user.password_changed",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("name=%s auth_type=%s", username, authType)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"command": b.String(),
	})
}

func buildClickHouseCreateAuthClause(authType, password string) string {
	switch authType {
	case "no_password":
		return ""
	case "plaintext_password":
		return " IDENTIFIED BY '" + escapeLiteral(password) + "'"
	default:
		return " IDENTIFIED WITH " + authType + " BY '" + escapeLiteral(password) + "'"
	}
}

func buildClickHouseAlterAuthClause(authType, password string) string {
	switch authType {
	case "no_password":
		return " IDENTIFIED WITH no_password"
	case "plaintext_password":
		return " IDENTIFIED BY '" + escapeLiteral(password) + "'"
	default:
		return " IDENTIFIED WITH " + authType + " BY '" + escapeLiteral(password) + "'"
	}
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
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}
	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	username := strings.TrimSpace(chi.URLParam(r, "username"))
	if username == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "username is required"})
		return
	}
	if username == session.ClickhouseUser {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cannot delete current session user"})
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
	sql += escapeIdentifier(username)

	if _, err := h.Gateway.ExecuteQuery(session.ConnectionID, sql, session.ClickhouseUser, password, 30*time.Second); err != nil {
		slog.Warn("Failed to delete ClickHouse user", "error", err, "connection", session.ConnectionID, "name", username)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("%s\n\nCommand:\n%s", err.Error(), sql)})
		return
	}

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "admin.clickhouse_user.deleted",
		Username:     strPtr(session.ClickhouseUser),
		ConnectionID: strPtr(session.ConnectionID),
		Details:      strPtr(fmt.Sprintf("name=%s", username)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"command": sql,
	})
}

// ---------- GET /clickhouse-query-log ----------

var timeRangeDurations = map[string]string{
	"5m": "5 MINUTE", "15m": "15 MINUTE", "30m": "30 MINUTE",
	"1h": "1 HOUR", "6h": "6 HOUR", "12h": "12 HOUR",
	"24h": "24 HOUR", "3d": "3 DAY", "7d": "7 DAY",
}

func (h *AdminHandler) GetClickHouseQueryLog(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)
	if session == nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "Not authenticated"})
		return
	}

	if !h.Gateway.IsTunnelOnline(session.ConnectionID) {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "Tunnel is offline"})
		return
	}

	password, err := crypto.Decrypt(session.EncryptedPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("Failed to decrypt password", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to decrypt credentials"})
		return
	}

	timeRange := r.URL.Query().Get("timeRange")
	limitStr := r.URL.Query().Get("limit")
	offsetStr := r.URL.Query().Get("offset")
	search := r.URL.Query().Get("search")
	queryKind := r.URL.Query().Get("queryKind")
	status := r.URL.Query().Get("status")

	limit := 100
	if limitStr != "" {
		if parsed, err := strconv.Atoi(limitStr); err == nil && parsed > 0 {
			limit = parsed
		}
	}
	if limit > 1000 {
		limit = 1000
	}

	offset := 0
	if offsetStr != "" {
		if parsed, err := strconv.Atoi(offsetStr); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	var prewhereConditions []string
	var whereConditions []string

	if timeRange != "" {
		if duration, ok := timeRangeDurations[timeRange]; ok {
			prewhereConditions = append(prewhereConditions,
				fmt.Sprintf("event_time >= now() - INTERVAL %s", duration))
		}
	}

	if search != "" {
		escaped := escapeString(search)
		whereConditions = append(whereConditions,
			fmt.Sprintf("(query ILIKE '%%%s%%' OR user ILIKE '%%%s%%')", escaped, escaped))
	}

	if queryKind != "" {
		normalized := strings.ToLower(strings.TrimSpace(queryKind))
		if normalized != "" && normalized != "all" {
			escaped := escapeString(normalized)
			whereConditions = append(whereConditions,
				fmt.Sprintf("lowerUTF8(query_kind) = '%s'", escaped))
		}
	}

	if status != "" {
		switch status {
		case "success":
			whereConditions = append(whereConditions, "exception_code = 0")
		case "error":
			whereConditions = append(whereConditions, "exception_code != 0")
		}
	}

	sql := `SELECT
		type, event_time, query_start_time, query_duration_ms,
		read_rows, read_bytes, written_rows, written_bytes,
		result_rows, result_bytes, memory_usage,
		query, query_kind, user, exception_code, exception,
		is_initial_query, databases, tables
	FROM system.query_log`

	if len(prewhereConditions) > 0 {
		sql += "\nPREWHERE " + strings.Join(prewhereConditions, " AND ")
	}
	if len(whereConditions) > 0 {
		sql += "\nWHERE " + strings.Join(whereConditions, " AND ")
	}
	sql += "\nORDER BY event_time DESC"
	sql += fmt.Sprintf("\nLIMIT %d OFFSET %d", limit, offset)
	sql += "\nFORMAT JSON"

	result, err := h.Gateway.ExecuteQuery(
		session.ConnectionID, sql,
		session.ClickhouseUser, password,
		60*time.Second,
	)
	if err != nil {
		slog.Warn("Failed to query system.query_log", "error", err, "connection", session.ConnectionID)

		if shouldFallbackToQueryThreadLog(err) {
			fallbackSQL := strings.Replace(sql, "system.query_log", "system.query_thread_log", 1)
			result, err = h.Gateway.ExecuteQuery(
				session.ConnectionID, fallbackSQL,
				session.ClickhouseUser, password,
				60*time.Second,
			)
			if err != nil {
				slog.Warn("Fallback to query_thread_log also failed", "error", err)
				writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
				return
			}
		} else {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
			return
		}
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"data": result.Data,
		"meta": result.Meta,
	})
}

// ---------- Helpers ----------

func escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

func shouldFallbackToQueryThreadLog(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "system.query_log") && strings.Contains(msg, "unknown_table") {
		return true
	}
	if strings.Contains(msg, "unknown table expression identifier") && strings.Contains(msg, "system.query_log") {
		return true
	}
	return false
}
