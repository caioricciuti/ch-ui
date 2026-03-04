package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/langfuse"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// GetLangfuseConfig returns the current Langfuse configuration (secret key masked).
func (h *AdminHandler) GetLangfuseConfig(w http.ResponseWriter, r *http.Request) {
	publicKey, _ := h.DB.GetSetting("langfuse.public_key")
	baseURL, _ := h.DB.GetSetting("langfuse.base_url")
	encryptedSecret, _ := h.DB.GetSetting("langfuse.secret_key")

	if baseURL == "" {
		baseURL = "https://cloud.langfuse.com"
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":        true,
		"public_key":     publicKey,
		"base_url":       baseURL,
		"has_secret_key": encryptedSecret != "",
		"enabled":        h.Langfuse.IsEnabled(),
	})
}

// UpdateLangfuseConfig saves Langfuse configuration and reconfigures the client.
func (h *AdminHandler) UpdateLangfuseConfig(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	var body struct {
		PublicKey string `json:"publicKey"`
		SecretKey string `json:"secretKey"`
		BaseURL   string `json:"baseUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	publicKey := strings.TrimSpace(body.PublicKey)
	secretKey := strings.TrimSpace(body.SecretKey)
	baseURL := strings.TrimSpace(body.BaseURL)

	if publicKey == "" {
		writeError(w, http.StatusBadRequest, "Public key is required")
		return
	}

	// Save public key
	if err := h.DB.SetSetting("langfuse.public_key", publicKey); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save public key")
		return
	}

	// Save base URL
	if baseURL == "" {
		baseURL = "https://cloud.langfuse.com"
	}
	if err := h.DB.SetSetting("langfuse.base_url", baseURL); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save base URL")
		return
	}

	// Save secret key (only if provided — allows partial update)
	if secretKey != "" {
		encrypted, err := crypto.Encrypt(secretKey, h.Config.AppSecretKey)
		if err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to encrypt secret key")
			return
		}
		if err := h.DB.SetSetting("langfuse.secret_key", encrypted); err != nil {
			writeError(w, http.StatusInternalServerError, "Failed to save secret key")
			return
		}
	}

	// Reconfigure the live client
	cfg, err := loadLangfuseConfig(h.DB, h.Config.AppSecretKey)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to reload config")
		return
	}
	h.Langfuse.Reconfigure(cfg)

	// Audit log
	actor := "unknown"
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "langfuse.config.updated",
		Username: strPtr(actor),
		Details:  strPtr("base_url=" + baseURL),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": h.Langfuse.IsEnabled(),
	})
}

// DeleteLangfuseConfig removes all Langfuse settings and disables the client.
func (h *AdminHandler) DeleteLangfuseConfig(w http.ResponseWriter, r *http.Request) {
	session := middleware.GetSession(r)

	_ = h.DB.DeleteSetting("langfuse.public_key")
	_ = h.DB.DeleteSetting("langfuse.secret_key")
	_ = h.DB.DeleteSetting("langfuse.base_url")

	h.Langfuse.Reconfigure(langfuse.Config{})

	actor := "unknown"
	if session != nil {
		actor = session.ClickhouseUser
	}
	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:   "langfuse.config.deleted",
		Username: strPtr(actor),
	})

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success": true,
		"enabled": false,
	})
}

// TestLangfuseConnection verifies credentials against the Langfuse API.
func (h *AdminHandler) TestLangfuseConnection(w http.ResponseWriter, r *http.Request) {
	var body struct {
		PublicKey string `json:"publicKey"`
		SecretKey string `json:"secretKey"`
		BaseURL   string `json:"baseUrl"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	publicKey := strings.TrimSpace(body.PublicKey)
	secretKey := strings.TrimSpace(body.SecretKey)
	baseURL := strings.TrimSpace(body.BaseURL)

	// If secret key not provided, use stored one
	if secretKey == "" {
		encryptedSecret, _ := h.DB.GetSetting("langfuse.secret_key")
		if encryptedSecret != "" {
			decrypted, err := crypto.Decrypt(encryptedSecret, h.Config.AppSecretKey)
			if err == nil {
				secretKey = decrypted
			}
		}
	}

	if publicKey == "" || secretKey == "" {
		writeError(w, http.StatusBadRequest, "Public key and secret key are required")
		return
	}

	cfg := langfuse.Config{
		PublicKey: publicKey,
		SecretKey: secretKey,
		BaseURL:   baseURL,
	}

	if err := h.Langfuse.TestConnection(cfg); err != nil {
		writeJSON(w, http.StatusOK, map[string]interface{}{
			"success":   true,
			"connected": false,
			"error":     err.Error(),
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"success":   true,
		"connected": true,
	})
}

// loadLangfuseConfig reads Langfuse configuration from the settings table.
func loadLangfuseConfig(db *database.DB, appSecretKey string) (langfuse.Config, error) {
	var cfg langfuse.Config

	publicKey, err := db.GetSetting("langfuse.public_key")
	if err != nil {
		return cfg, err
	}
	cfg.PublicKey = publicKey

	encryptedSecret, err := db.GetSetting("langfuse.secret_key")
	if err != nil {
		return cfg, err
	}
	if encryptedSecret != "" {
		decrypted, err := crypto.Decrypt(encryptedSecret, appSecretKey)
		if err != nil {
			return cfg, err
		}
		cfg.SecretKey = decrypted
	}

	baseURL, err := db.GetSetting("langfuse.base_url")
	if err != nil {
		return cfg, err
	}
	cfg.BaseURL = baseURL
	cfg.NormalizeBaseURL()

	return cfg, nil
}
