package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/license"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// LicenseHandler handles license status and activation endpoints.
type LicenseHandler struct {
	DB     *database.DB
	Config *config.Config

	// validate replaces license.ValidateLicense in tests.
	validate func(string) *license.LicenseInfo
}

// GetLicense returns the current license status.
// GET /api/license
//
// The route needs no session (the login page reads it), so everyone gets the
// edition, validity, expiry and grace window; the customer name and license
// id go only to a signed-in admin.
func (h *LicenseHandler) GetLicense(w http.ResponseWriter, r *http.Request) {
	validate := h.validate
	if validate == nil {
		validate = license.ValidateLicense
	}
	info := validate(h.Config.LicenseJSON)
	if !h.isAdminRequest(r) {
		info.Customer = ""
		info.LicenseID = ""
	}
	writeJSON(w, http.StatusOK, info)
}

// isAdminRequest resolves the request's session with the same middleware
// the protected routes use, so the role logic lives in one place. A missing,
// expired or mismatched session is not an admin.
func (h *LicenseHandler) isAdminRequest(r *http.Request) bool {
	if c, err := r.Cookie(SessionCookie); err != nil || c.Value == "" {
		return false
	}
	admin := false
	probe := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		s := middleware.GetSession(r)
		admin = s != nil && s.UserRole == "admin"
	})
	middleware.Session(h.DB, nil)(probe).ServeHTTP(discardResponse{}, r)
	return admin
}

// discardResponse swallows the error the session middleware writes for an
// invalid session.
type discardResponse struct{}

func (discardResponse) Header() http.Header         { return http.Header{} }
func (discardResponse) Write(b []byte) (int, error) { return len(b), nil }
func (discardResponse) WriteHeader(int)             {}

// ActivateLicense activates a new license by validating and storing the signed JSON.
// POST /api/license/activate
func (h *LicenseHandler) ActivateLicense(w http.ResponseWriter, r *http.Request) {
	var body struct {
		License string `json:"license"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	licenseJSON := strings.TrimSpace(body.License)
	if licenseJSON == "" {
		writeError(w, http.StatusBadRequest, "License JSON is required")
		return
	}

	info := license.ValidateLicense(licenseJSON)
	if !info.Valid {
		writeError(w, http.StatusBadRequest, "Invalid or expired license")
		return
	}

	// Store in settings
	if err := h.DB.SetSetting("license_json", licenseJSON); err != nil {
		writeError(w, http.StatusInternalServerError, "Failed to save license")
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
		writeError(w, http.StatusInternalServerError, "Failed to remove license")
		return
	}

	h.Config.LicenseJSON = ""

	writeJSON(w, http.StatusOK, license.CommunityLicense())
}
