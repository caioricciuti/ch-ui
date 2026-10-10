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

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/embedded"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tokens"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// ConnectionsHandler handles connection management routes.
type ConnectionsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
	Agents  *embedded.Manager
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

// connectionResponse extends Connection with live status information.
type connectionResponse struct {
	database.Connection
	Online   bool       `json:"online"`
	LastSeen *time.Time `json:"last_seen,omitempty"`
	HostInfo any        `json:"host_info,omitempty"`
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

// SetSSOAccount stores the ClickHouse service account that OIDC SSO sessions on
// this connection will use for queries. Admin only.
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

	var username, chUser *string
	if session := middleware.GetSession(r); session != nil {
		username, chUser = strPtr(middleware.Actor(session)), strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:         "connection.sso_account.set",
		Username:       username,
		ClickhouseUser: chUser,
		ConnectionID:   strPtr(id),
		Details:        strPtr(fmt.Sprintf("SSO ClickHouse service account set to %s", req.Username)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
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
			"connection": conn,
		})
		return
	}

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"connection":         conn,
		"tunnel_token":       token,
		"setup_instructions": getSetupInstructions(token),
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
		Details:        strPtr(fmt.Sprintf("Deleted connection %q", conn.Name)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]string{"message": "Connection deleted successfully"})
}

// TestConnection tests a ClickHouse connection through the tunnel.
// POST /{id}/test
func (h *ConnectionsHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
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

	if !h.Gateway.IsTunnelOnline(id) {
		writeError(w, http.StatusOK, "Tunnel is not connected. Please ensure the agent is running.")
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

	result, err := h.Gateway.TestConnection(id, username, password, 15*time.Second)
	if err != nil {
		slog.Warn("Connection test failed", "error", err, "id", id)
		writeError(w, http.StatusOK, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
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
		"setup_instructions": getSetupInstructions(conn.TunnelToken),
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
		Details:        strPtr(fmt.Sprintf("Regenerated token for connection %q", conn.Name)),
		IPAddress:      strPtr(getClientIP(r)),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"tunnel_token":       newToken,
		"setup_instructions": getSetupInstructions(newToken),
		"message":            "Token regenerated successfully. The previous token is now invalid.",
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
		Connection: c,
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

// getSetupInstructions returns setup instructions for connecting a tunnel.
func getSetupInstructions(token string) map[string]string {
	return map[string]string{
		"connect": fmt.Sprintf("ch-ui connect --url <YOUR_SERVER_URL>/connect --key %s", token),
		"service": fmt.Sprintf("ch-ui service install --url <YOUR_SERVER_URL>/connect --key %s", token),
	}
}
