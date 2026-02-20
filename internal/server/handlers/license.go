package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/license"
)

// LicenseHandler handles license status and activation endpoints.
type LicenseHandler struct {
	DB     *database.DB
	Config *config.Config
}

// GetLicense returns the current license status.
// GET /api/license
func (h *LicenseHandler) GetLicense(w http.ResponseWriter, r *http.Request) {
	info := license.ValidateLicense(h.Config.LicenseJSON)
	writeJSON(w, http.StatusOK, info)
}

// ActivateLicense activates a new license by validating and storing the signed JSON.
// POST /api/license/activate
func (h *LicenseHandler) ActivateLicense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		License string `json:"license"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid request body"})
		return
	}

	licenseJSON := strings.TrimSpace(body.License)
	if licenseJSON == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "License JSON is required"})
		return
	}

	info := license.ValidateLicense(licenseJSON)
	if !info.Valid {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "Invalid or expired license"})
		return
	}

	// Store in settings
	if err := h.DB.SetSetting("license_json", licenseJSON); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to save license"})
		return
	}

	// Update runtime config
	h.Config.LicenseJSON = licenseJSON

	writeJSON(w, http.StatusOK, info)
}

// DeactivateLicense removes the current license (downgrade to community).
// POST /api/license/deactivate
func (h *LicenseHandler) DeactivateLicense(w http.ResponseWriter, r *http.Request) {
	if err := h.DB.DeleteSetting("license_json"); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "Failed to remove license"})
		return
	}

	h.Config.LicenseJSON = ""

	writeJSON(w, http.StatusOK, license.CommunityLicense())
}
