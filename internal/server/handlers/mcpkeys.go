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
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/mcpserver"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// MCPKeysHandler manages the bearer keys for the embedded MCP server.
// Admin-only: a key carries its own ClickHouse credentials.
type MCPKeysHandler struct {
	DB     *database.DB
	Config *config.Config
}

func (h *MCPKeysHandler) Routes(r chi.Router) {
	r.Get("/", h.list)
	r.Post("/", h.create)
	r.Post("/{id}/rotate", h.rotate)
	r.Delete("/{id}", h.revoke)
}

// maxKeyLifetimeDays bounds expires_in_days; 0 means the key never expires.
const maxKeyLifetimeDays = 3650

func (h *MCPKeysHandler) list(w http.ResponseWriter, r *http.Request) {
	keys, err := h.DB.ListMCPKeys()
	if err != nil {
		slog.Error("MCP keys: list failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to list MCP keys")
		return
	}
	if keys == nil {
		keys = []*database.MCPKey{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "keys": keys})
}

type createMCPKeyRequest struct {
	Name             string `json:"name"`
	ConnectionID     string `json:"connection_id"`
	CHUser           string `json:"ch_user"`
	CHPassword       string `json:"ch_password"`
	Scopes           string `json:"scopes"` // "read" (default) or "read_write"
	AllowedDatabases string `json:"allowed_databases"`
	ExpiresInDays    int    `json:"expires_in_days"` // 0 = never
}

func (h *MCPKeysHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createMCPKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.CHUser = strings.TrimSpace(req.CHUser)
	if req.Name == "" || req.ConnectionID == "" || req.CHUser == "" {
		writeError(w, http.StatusBadRequest, "name, connection_id and ch_user are required")
		return
	}
	conn, err := h.DB.GetConnectionByID(req.ConnectionID)
	if err != nil || conn == nil {
		writeError(w, http.StatusBadRequest, "Unknown connection")
		return
	}

	encrypted, err := crypto.Encrypt(req.CHPassword, h.Config.AppSecretKey)
	if err != nil {
		slog.Error("MCP keys: encrypt failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to encrypt credentials")
		return
	}

	plaintext, hash, prefix := mcpserver.GenerateKey()
	// The person, not the ClickHouse account: under SSO many people share
	// one service account.
	createdBy := ""
	if sess := middleware.GetSession(r); sess != nil {
		createdBy = middleware.Actor(sess)
	}
	scopes := req.Scopes
	if scopes != "read_write" {
		scopes = "read"
	}
	if req.ExpiresInDays < 0 || req.ExpiresInDays > maxKeyLifetimeDays {
		writeError(w, http.StatusBadRequest, "expires_in_days must be between 0 (never) and 3650")
		return
	}
	var expiresAt *string
	if req.ExpiresInDays > 0 {
		e := time.Now().UTC().AddDate(0, 0, req.ExpiresInDays).Format(time.RFC3339)
		expiresAt = &e
	}
	key, err := h.DB.CreateMCPKey(database.CreateMCPKeyParams{
		Name: req.Name, KeyHash: hash, KeyPrefix: prefix, ConnectionID: req.ConnectionID,
		CHUser: req.CHUser, CHPasswordEnc: encrypted, Scopes: scopes,
		AllowedDatabases: req.AllowedDatabases, CreatedBy: createdBy, ExpiresAt: expiresAt,
	})
	if err != nil {
		slog.Error("MCP keys: create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create MCP key")
		return
	}

	h.auditKey(r, "mcp.key.created", key.ID, fmt.Sprintf("key=%s name=%q prefix=%s connection=%s ch_user=%s scopes=%s allowed_databases=%q", key.ID, key.Name, prefix, req.ConnectionID, req.CHUser, scopes, req.AllowedDatabases), req.ConnectionID)

	// The plaintext key is returned exactly once, at creation.
	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"key":     key,
		"secret":  plaintext,
	})
}

// rotate issues a replacement key with the same binding and revokes the old
// one. The new plaintext is returned exactly once, like create.
func (h *MCPKeysHandler) rotate(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	plaintext, hash, prefix := mcpserver.GenerateKey()
	rotatedBy := ""
	if sess := middleware.GetSession(r); sess != nil {
		rotatedBy = middleware.Actor(sess)
	}
	key, err := h.DB.RotateMCPKey(id, hash, prefix, rotatedBy)
	if err != nil {
		writeError(w, http.StatusNotFound, "MCP key not found or already revoked")
		return
	}
	h.auditKey(r, "mcp.key.rotated", key.ID, fmt.Sprintf("old_key=%s new_key=%s name=%q prefix=%s", id, key.ID, key.Name, prefix), key.ConnectionID)
	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"key":     key,
		"secret":  plaintext,
	})
}

func (h *MCPKeysHandler) revoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	existing, _ := h.DB.GetMCPKeyByID(id)
	if err := h.DB.RevokeMCPKey(id); err != nil {
		writeError(w, http.StatusNotFound, "MCP key not found or already revoked")
		return
	}
	details, connID := "key="+id, ""
	if existing != nil {
		details = fmt.Sprintf("key=%s name=%q prefix=%s", id, existing.Name, existing.KeyPrefix)
		connID = existing.ConnectionID
	}
	h.auditKey(r, "mcp.key.revoked", id, details, connID)
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// auditKey records a key lifecycle event. A key carries ClickHouse
// credentials, so who created, rotated or revoked it belongs in the log.
func (h *MCPKeysHandler) auditKey(r *http.Request, action, keyID, details, connectionID string) {
	params := database.AuditLogParams{
		Action:    action,
		Details:   strPtr(details),
		IPAddress: strPtr(getClientIP(r)),
	}
	if connectionID != "" {
		params.ConnectionID = strPtr(connectionID)
	}
	if sess := middleware.GetSession(r); sess != nil {
		params.Username = strPtr(middleware.Actor(sess))
		params.ClickhouseUser = strPtr(sess.ClickhouseUser)
	}
	if err := h.DB.CreateAuditLog(params); err != nil {
		slog.Warn("MCP keys: audit log failed", "action", action, "key", keyID, "error", err)
	}
}
