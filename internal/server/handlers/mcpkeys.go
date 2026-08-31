package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

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
	r.Delete("/{id}", h.revoke)
}

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
	createdBy := ""
	if sess := middleware.GetSession(r); sess != nil {
		createdBy = sess.ClickhouseUser
	}
	scopes := req.Scopes
	if scopes != "read_write" {
		scopes = "read"
	}
	key, err := h.DB.CreateMCPKey(req.Name, hash, prefix, req.ConnectionID, req.CHUser, encrypted, scopes, req.AllowedDatabases, createdBy)
	if err != nil {
		slog.Error("MCP keys: create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create MCP key")
		return
	}

	// The plaintext key is returned exactly once, at creation.
	writeJSON(w, http.StatusCreated, map[string]any{
		"success": true,
		"key":     key,
		"secret":  plaintext,
	})
}

func (h *MCPKeysHandler) revoke(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := h.DB.RevokeMCPKey(id); err != nil {
		writeError(w, http.StatusNotFound, "MCP key not found or already revoked")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
