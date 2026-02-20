package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/license"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// ConnectionsHandler handles connection management routes.
type ConnectionsHandler struct {
	DB      *database.DB
	Gateway *tunnel.Gateway
	Config  *config.Config
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
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connections"})
		return
	}

	results := make([]connectionResponse, 0, len(conns))
	for _, c := range conns {
		results = append(results, h.buildConnectionResponse(c))
	}

	connJSON(w, http.StatusOK, results)
}

// Get returns a single connection by ID.
// GET /{id}
func (h *ConnectionsHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection ID is required"})
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connection"})
		return
	}
	if conn == nil {
		connJSON(w, http.StatusNotFound, map[string]string{"error": "Connection not found"})
		return
	}

	connJSON(w, http.StatusOK, h.buildConnectionResponse(*conn))
}

// Create creates a new connection.
// POST /
func (h *ConnectionsHandler) Create(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name string `json:"name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	name := strings.TrimSpace(body.Name)
	if name == "" {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection name is required"})
		return
	}

	token := license.GenerateTunnelToken()

	id, err := h.DB.CreateConnection(name, token, false)
	if err != nil {
		slog.Error("Failed to create connection", "error", err)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to create connection"})
		return
	}

	session := middleware.GetSession(r)
	var username *string
	if session != nil {
		username = strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "connection.created",
		Username:     username,
		ConnectionID: strPtr(id),
		Details:      strPtr(fmt.Sprintf("Created connection %q", name)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil || conn == nil {
		slog.Error("Failed to retrieve created connection", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Connection created but failed to retrieve"})
		return
	}

	connJSON(w, http.StatusCreated, map[string]interface{}{
		"connection":         conn,
		"tunnel_token":       token,
		"setup_instructions": getSetupInstructions(token),
	})
}

// Delete deletes a connection by ID.
// DELETE /{id}
func (h *ConnectionsHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection ID is required"})
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for deletion", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connection"})
		return
	}
	if conn == nil {
		connJSON(w, http.StatusNotFound, map[string]string{"error": "Connection not found"})
		return
	}

	if err := h.DB.DeleteConnection(id); err != nil {
		slog.Error("Failed to delete connection", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to delete connection"})
		return
	}

	session := middleware.GetSession(r)
	var username *string
	if session != nil {
		username = strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "connection.deleted",
		Username:     username,
		ConnectionID: strPtr(id),
		Details:      strPtr(fmt.Sprintf("Deleted connection %q", conn.Name)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	connJSON(w, http.StatusOK, map[string]string{"message": "Connection deleted successfully"})
}

// TestConnection tests a ClickHouse connection through the tunnel.
// POST /{id}/test
func (h *ConnectionsHandler) TestConnection(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection ID is required"})
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for test", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connection"})
		return
	}
	if conn == nil {
		connJSON(w, http.StatusNotFound, map[string]string{"error": "Connection not found"})
		return
	}

	if !h.Gateway.IsTunnelOnline(id) {
		connJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   "Tunnel is not connected. Please ensure the agent is running.",
		})
		return
	}

	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
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
		connJSON(w, http.StatusOK, map[string]interface{}{
			"success": false,
			"error":   err.Error(),
		})
		return
	}

	connJSON(w, http.StatusOK, result)
}

// GetToken returns the tunnel token for a connection.
// GET /{id}/token
func (h *ConnectionsHandler) GetToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection ID is required"})
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for token", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connection"})
		return
	}
	if conn == nil {
		connJSON(w, http.StatusNotFound, map[string]string{"error": "Connection not found"})
		return
	}

	connJSON(w, http.StatusOK, map[string]interface{}{
		"tunnel_token":       conn.TunnelToken,
		"setup_instructions": getSetupInstructions(conn.TunnelToken),
	})
}

// RegenerateToken generates a new tunnel token for a connection.
// POST /{id}/regenerate-token
func (h *ConnectionsHandler) RegenerateToken(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		connJSON(w, http.StatusBadRequest, map[string]string{"error": "Connection ID is required"})
		return
	}

	conn, err := h.DB.GetConnectionByID(id)
	if err != nil {
		slog.Error("Failed to get connection for token regeneration", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to retrieve connection"})
		return
	}
	if conn == nil {
		connJSON(w, http.StatusNotFound, map[string]string{"error": "Connection not found"})
		return
	}

	newToken := license.GenerateTunnelToken()

	if err := h.DB.UpdateConnectionToken(id, newToken); err != nil {
		slog.Error("Failed to regenerate token", "error", err, "id", id)
		connJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to regenerate token"})
		return
	}

	session := middleware.GetSession(r)
	var username *string
	if session != nil {
		username = strPtr(session.ClickhouseUser)
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "connection.token_regenerated",
		Username:     username,
		ConnectionID: strPtr(id),
		Details:      strPtr(fmt.Sprintf("Regenerated token for connection %q", conn.Name)),
		IPAddress:    strPtr(r.RemoteAddr),
	})

	connJSON(w, http.StatusOK, map[string]interface{}{
		"tunnel_token":       newToken,
		"setup_instructions": getSetupInstructions(newToken),
		"message":            "Token regenerated successfully. The previous token is now invalid.",
	})
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

// connJSON writes a JSON response.
func connJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}
