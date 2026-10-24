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
// Admin-only: a key carries its own ClickHouse credentials, or runs as the
// admin's own per-person ClickHouse user.
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
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "keys": keys, "as_me": h.asMeUsers(r)})
}

// asMeUsers maps connection id to the signed-in person's per-person
// ClickHouse user there, for the connections where a new key can run as
// them. Empty outside per-person SSO.
func (h *MCPKeysHandler) asMeUsers(r *http.Request) map[string]string {
	out := map[string]string{}
	sess := middleware.GetSession(r)
	if sess == nil || sess.SSOIdentityID == "" {
		return out
	}
	users, err := h.DB.ListSSOClickHouseUsersForIdentity(sess.SSOIdentityID)
	if err != nil {
		slog.Warn("MCP keys: list per-person users failed", "error", err)
		return out
	}
	for _, u := range users {
		if !u.Locked() {
			out[u.ConnectionID] = u.CHUser
		}
	}
	return out
}

type createMCPKeyRequest struct {
	Name             string `json:"name"`
	ConnectionID     string `json:"connection_id"`
	CHUser           string `json:"ch_user"`
	CHPassword       string `json:"ch_password"`
	Scopes           string `json:"scopes"`        // "read" (default) or "read_write"
	AllowActions     bool   `json:"allow_actions"` // lifecycle action tools; requires read_write
	AllowedDatabases string `json:"allowed_databases"`
	ExpiresInDays    int    `json:"expires_in_days"` // 0 = never
	// AsMe runs the key as the signed-in person's per-person ClickHouse
	// user on the connection: no password is stored, the key follows the
	// live user and stops when it is locked or dropped. CHUser and
	// CHPassword are ignored.
	AsMe bool `json:"as_me"`
}

func (h *MCPKeysHandler) create(w http.ResponseWriter, r *http.Request) {
	var req createMCPKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	req.Name = strings.TrimSpace(req.Name)
	req.CHUser = strings.TrimSpace(req.CHUser)
	if req.Name == "" || req.ConnectionID == "" || (req.CHUser == "" && !req.AsMe) {
		writeError(w, http.StatusBadRequest, "name, connection_id and ch_user are required")
		return
	}
	conn, err := h.DB.GetConnectionByID(req.ConnectionID)
	if err != nil || conn == nil {
		writeError(w, http.StatusBadRequest, "Unknown connection")
		return
	}

	sess := middleware.GetSession(r)
	// The person, not the ClickHouse account: under SSO many people share
	// one service account.
	createdBy := ""
	if sess != nil {
		createdBy = middleware.Actor(sess)
	}

	// A run-as-me key references the person's per-person user, like an
	// OAuth grant: no password copy, and the role re-checked on every call.
	var encrypted, identityID, subject, grantRole string
	if req.AsMe {
		var u *database.SSOClickHouseUser
		if sess != nil && sess.SSOIdentityID != "" {
			u, err = h.DB.GetSSOClickHouseUser(sess.SSOIdentityID, req.ConnectionID)
			if err != nil {
				slog.Error("MCP keys: resolve per-person user failed", "error", err)
				writeError(w, http.StatusInternalServerError, "Failed to create MCP key")
				return
			}
		}
		if u == nil || u.Locked() {
			writeError(w, http.StatusBadRequest, "You have no per-person ClickHouse user on this connection; pick a ClickHouse account instead")
			return
		}
		req.CHUser, identityID, subject, grantRole = u.CHUser, sess.SSOIdentityID, createdBy, sess.UserRole
	} else {
		encrypted, err = crypto.Encrypt(req.CHPassword, h.Config.AppSecretKey)
		if err != nil {
			slog.Error("MCP keys: encrypt failed", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to encrypt credentials")
			return
		}
	}

	plaintext, hash, prefix := mcpserver.GenerateKey()
	scopes := req.Scopes
	if scopes != "read_write" {
		scopes = "read"
	}
	if req.AllowActions && scopes != "read_write" {
		writeError(w, http.StatusBadRequest, "allow_actions requires scopes read_write")
		return
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
		CHUser: req.CHUser, CHPasswordEnc: encrypted, SSOIdentityID: identityID, Scopes: scopes, AllowActions: req.AllowActions,
		AllowedDatabases: req.AllowedDatabases, CreatedBy: createdBy, ExpiresAt: expiresAt,
		Subject: subject, GrantRole: grantRole,
	})
	if err != nil {
		slog.Error("MCP keys: create failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to create MCP key")
		return
	}

	h.auditKey(r, "mcp.key.created", key.ID, fmt.Sprintf("key=%s name=%q prefix=%s connection=%s ch_user=%s as_me=%t scopes=%s allow_actions=%t allowed_databases=%q", key.ID, key.Name, prefix, req.ConnectionID, req.CHUser, req.AsMe, scopes, key.AllowActions, req.AllowedDatabases), req.ConnectionID)

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
