// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// oidcCallbackPath is the fixed callback route the IdP redirects back to.
const oidcCallbackPath = "/api/auth/oidc/callback"

// oidcRedirectURL computes the full callback URL from an admin-configured
// redirect base, falling back to the server's AppURL when the base is empty.
func oidcRedirectURL(base string, cfg *config.Config) string {
	b := strings.TrimSpace(base)
	if b == "" {
		b = strings.TrimSpace(cfg.AppURL)
	}
	return strings.TrimRight(b, "/") + oidcCallbackPath
}

// LoadDBSSOSettings converts the stored admin SSO config into runtime OIDC
// settings, decrypting the client secret with the app secret key.
func LoadDBSSOSettings(db *database.DB, cfg *config.Config) (oidc.Settings, error) {
	stored, err := db.GetSSOConfig()
	if err != nil {
		return oidc.Settings{}, err
	}
	secret := ""
	if stored.ClientSecretEnc != "" {
		secret, err = crypto.Decrypt(stored.ClientSecretEnc, cfg.AppSecretKey)
		if err != nil {
			return oidc.Settings{}, err
		}
	}
	return oidc.Settings{
		Enabled:               stored.Enabled,
		IssuerURL:             stored.IssuerURL,
		ClientID:              stored.ClientID,
		ClientSecret:          secret,
		RedirectURL:           oidcRedirectURL(stored.RedirectBaseURL, cfg),
		AllowedDomains:        stored.AllowedDomains,
		AdminGroups:           stored.AdminGroups,
		AnalystGroups:         stored.AnalystGroups,
		ViewerGroups:          stored.ViewerGroups,
		AcceptUnverifiedEmail: stored.AcceptUnverifiedEmail,
		SSOOnly:               stored.SSOOnly,
		Source:                oidc.SourceDB,
	}, nil
}

// ResolveOIDCSettings returns the effective SSO settings following the
// precedence rule: env/YAML config wins; the DB (admin UI) config applies
// only when no env config is set.
func ResolveOIDCSettings(cfg *config.Config, db *database.DB) (oidc.Settings, bool) {
	env := oidc.SettingsFromConfig(cfg)
	dbSettings, err := LoadDBSSOSettings(db, cfg)
	if err != nil {
		slog.Error("SSO: failed to load DB config, ignoring it", "error", err)
		dbSettings = oidc.Settings{}
	}
	return oidc.Resolve(env, dbSettings)
}

// ssoConfigView is the secret-free view of the SSO config returned to the
// admin UI. The client secret itself is never returned — only has_secret.
type ssoConfigView struct {
	Enabled               bool     `json:"enabled"`
	IssuerURL             string   `json:"issuer_url"`
	ClientID              string   `json:"client_id"`
	HasSecret             bool     `json:"has_secret"`
	RedirectBaseURL       string   `json:"redirect_base_url"`
	RedirectURL           string   `json:"redirect_url"`
	AdminGroups           []string `json:"admin_groups"`
	AnalystGroups         []string `json:"analyst_groups"`
	ViewerGroups          []string `json:"viewer_groups"`
	AllowedDomains        []string `json:"allowed_domains"`
	AcceptUnverifiedEmail bool     `json:"accept_unverified_email"`
	SSOOnly               bool     `json:"sso_only"`
}

// ssoSettingsResponse is the shape returned by GET/PUT /api/admin/sso.
type ssoSettingsResponse struct {
	// EnvManaged is true when OIDC is configured via env/YAML; the UI shows the
	// config read-only in that case because env always wins over the DB config.
	EnvManaged bool `json:"env_managed"`
	// Active reports whether an OIDC provider is currently initialized and
	// serving /api/auth/oidc/login.
	Active bool          `json:"active"`
	Config ssoConfigView `json:"config"`
	// ReloadError carries the discovery error when a save persisted fine but
	// the provider could not be (re)initialized from the new settings.
	ReloadError string `json:"reload_error,omitempty"`
	// SSOOnlyForced is true when OIDC_SSO_ONLY / oidc_sso_only turns SSO-only
	// on from the server config; the UI shows the toggle on and locked.
	SSOOnlyForced bool `json:"sso_only_forced"`
	// SSOOnlyEnforced reports whether password sign-in is off right now: SSO
	// only is set, the provider is loaded and break-glass is off.
	SSOOnlyEnforced bool `json:"sso_only_enforced"`
	// BreakGlass is true when CHUI_ALLOW_PASSWORD_LOGIN keeps password
	// sign-in on whatever the SSO-only setting says.
	BreakGlass bool `json:"break_glass"`
	// ViaSSO is true when the caller signed in through SSO, which turning
	// SSO-only on requires.
	ViaSSO bool `json:"via_sso"`
}

// ssoUpdateRequest is the PUT /api/admin/sso body. ClientSecret is optional:
// blank keeps the currently stored (encrypted) secret.
type ssoUpdateRequest struct {
	Enabled               bool     `json:"enabled"`
	IssuerURL             string   `json:"issuer_url"`
	ClientID              string   `json:"client_id"`
	ClientSecret          string   `json:"client_secret"`
	RedirectBaseURL       string   `json:"redirect_base_url"`
	AdminGroups           []string `json:"admin_groups"`
	AnalystGroups         []string `json:"analyst_groups"`
	ViewerGroups          []string `json:"viewer_groups"`
	AllowedDomains        []string `json:"allowed_domains"`
	AcceptUnverifiedEmail bool     `json:"accept_unverified_email"`
	SSOOnly               bool     `json:"sso_only"`
}

// errSSOOnlyNeedsSSOSession refuses turning SSO-only on from a password
// session, so nobody locks password sign-in out without having proven that
// SSO sign-in works.
const errSSOOnlyNeedsSSOSession = "Sign in with SSO first to prove it works, then turn on SSO only."

// GetSSOSettings returns the current SSO configuration (secret redacted).
//
// GET /api/admin/sso
func (h *AdminHandler) GetSSOSettings(w http.ResponseWriter, r *http.Request) {
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}
	resp, err := h.buildSSOSettingsResponse(r)
	if err != nil {
		slog.Error("Failed to load SSO config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load SSO settings")
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

// UpdateSSOSettings persists the admin-managed SSO config and re-initializes
// the OIDC provider in place, so changes apply without a server restart.
// Rejected when OIDC is configured via env/YAML (env always wins).
//
// PUT /api/admin/sso
func (h *AdminHandler) UpdateSSOSettings(w http.ResponseWriter, r *http.Request) {
	if !h.Config.IsPro() {
		writeError(w, http.StatusPaymentRequired, "Pro license required")
		return
	}
	if h.Config.OIDCEnabled() {
		writeError(w, http.StatusConflict, "SSO is configured via environment/config file — those settings take precedence. Edit the server config to change SSO.")
		return
	}

	var req ssoUpdateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	current, err := h.DB.GetSSOConfig()
	if err != nil {
		slog.Error("Failed to load SSO config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load SSO settings")
		return
	}

	next := database.SSOConfig{
		Enabled:               req.Enabled,
		IssuerURL:             strings.TrimSpace(req.IssuerURL),
		ClientID:              strings.TrimSpace(req.ClientID),
		ClientSecretEnc:       current.ClientSecretEnc, // blank secret in the request keeps the stored one
		RedirectBaseURL:       strings.TrimSpace(req.RedirectBaseURL),
		AdminGroups:           cleanList(req.AdminGroups),
		AnalystGroups:         cleanList(req.AnalystGroups),
		ViewerGroups:          cleanList(req.ViewerGroups),
		AllowedDomains:        cleanList(req.AllowedDomains),
		AcceptUnverifiedEmail: req.AcceptUnverifiedEmail,
		SSOOnly:               req.SSOOnly,
	}
	if h.Config.OIDCSSOOnly {
		// Forced on by the server config: the stored value is not the admin
		// page's to change.
		next.SSOOnly = current.SSOOnly
	}
	if next.SSOOnly && !current.SSOOnly && !sessionViaSSO(r) {
		writeError(w, http.StatusBadRequest, errSSOOnlyNeedsSSOSession)
		return
	}
	if secret := strings.TrimSpace(req.ClientSecret); secret != "" {
		enc, err := crypto.Encrypt(secret, h.Config.AppSecretKey)
		if err != nil {
			slog.Error("Failed to encrypt SSO client secret", "error", err)
			writeError(w, http.StatusInternalServerError, "Failed to encrypt client secret")
			return
		}
		next.ClientSecretEnc = enc
	}

	if err := h.DB.SetSSOConfig(next); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	actor, chUser := "unknown", ""
	if session := middleware.GetSession(r); session != nil {
		actor, chUser = middleware.Actor(session), session.ClickhouseUser
	}
	// Audit without any secret material.
	audited := next
	audited.ClientSecretEnc = ""
	if details, err := json.Marshal(audited); err == nil {
		h.DB.CreateAuditLog(database.AuditLogParams{
			Action:         "sso.config_update",
			Username:       strPtr(actor),
			ClickhouseUser: strPtr(chUser),
			Details:        strPtr(string(details)),
			IPAddress:      strPtr(getClientIP(r)),
		})
	}

	// Apply immediately: re-init the provider from the new settings (or drop
	// it when SSO was disabled). No server restart needed.
	reloadErr := h.applySSOSettings(r.Context())
	RevokePasswordSessions(h.DB, h.Config, h.OIDCManager, actor, getClientIP(r))

	resp, err := h.buildSSOSettingsResponse(r)
	if err != nil {
		slog.Error("Failed to load SSO config", "error", err)
		writeError(w, http.StatusInternalServerError, "Failed to load SSO settings")
		return
	}
	if reloadErr != nil && resp.ReloadError == "" {
		resp.ReloadError = reloadErr.Error()
	}
	writeJSON(w, http.StatusOK, resp)
}

// applySSOSettings re-resolves the effective settings and swaps the live
// provider: reload on a complete enabled config, disable otherwise.
func (h *AdminHandler) applySSOSettings(ctx context.Context) error {
	if h.OIDCManager == nil {
		return nil
	}
	settings, ok := ResolveOIDCSettings(h.Config, h.DB)
	if !ok {
		h.OIDCManager.Disable()
		return nil
	}
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := h.OIDCManager.Reload(rctx, settings); err != nil {
		slog.Error("SSO provider reload failed", "issuer", settings.IssuerURL, "error", err)
		if settings.SSOOnly {
			slog.Warn("SSO only is set but SSO could not be loaded; password sign-in stays available until it loads")
		}
		return err
	}
	slog.Info("SSO provider reloaded", "issuer", settings.IssuerURL, "source", settings.Source)
	return nil
}

func (h *AdminHandler) buildSSOSettingsResponse(r *http.Request) (ssoSettingsResponse, error) {
	resp, err := h.ssoSettingsView()
	if err != nil {
		return resp, err
	}
	resp.BreakGlass = h.Config.AllowPasswordLogin
	resp.SSOOnlyForced = h.Config.OIDCSSOOnly
	resp.ViaSSO = sessionViaSSO(r)
	if h.OIDCManager != nil && !resp.BreakGlass {
		_, resp.SSOOnlyEnforced = h.OIDCManager.SSOOnly()
	}
	// Keep a failed reload visible after the save that caused it, until a
	// later save or restart loads the provider.
	if h.OIDCManager != nil {
		if p, _, curErr := h.OIDCManager.Current(); p == nil || curErr != nil {
			resp.Active = false
		}
		if reloadErr := h.OIDCManager.LastError(); reloadErr != nil {
			resp.ReloadError = reloadErr.Error()
		}
	}
	return resp, nil
}

func (h *AdminHandler) ssoSettingsView() (ssoSettingsResponse, error) {
	active := h.OIDCManager != nil && h.OIDCManager.Active()

	if h.Config.OIDCEnabled() {
		// Env config wins: show it read-only (secret never leaves the server).
		return ssoSettingsResponse{
			EnvManaged: true,
			Active:     active,
			Config: ssoConfigView{
				Enabled:               true,
				IssuerURL:             h.Config.OIDCIssuerURL,
				ClientID:              h.Config.OIDCClientID,
				HasSecret:             true,
				RedirectURL:           h.Config.OIDCRedirectURL,
				AdminGroups:           emptyIfNil(h.Config.OIDCAdminGroups),
				AnalystGroups:         emptyIfNil(h.Config.OIDCAnalystGroups),
				ViewerGroups:          []string{},
				AllowedDomains:        emptyIfNil(h.Config.OIDCAllowedDomains),
				AcceptUnverifiedEmail: h.Config.OIDCAcceptUnverifiedEmail,
				SSOOnly:               h.Config.OIDCSSOOnly,
			},
		}, nil
	}

	stored, err := h.DB.GetSSOConfig()
	if err != nil {
		return ssoSettingsResponse{}, err
	}
	return ssoSettingsResponse{
		EnvManaged: false,
		Active:     active,
		Config: ssoConfigView{
			Enabled:               stored.Enabled,
			IssuerURL:             stored.IssuerURL,
			ClientID:              stored.ClientID,
			HasSecret:             stored.ClientSecretEnc != "",
			RedirectBaseURL:       stored.RedirectBaseURL,
			RedirectURL:           oidcRedirectURL(stored.RedirectBaseURL, h.Config),
			AdminGroups:           emptyIfNil(stored.AdminGroups),
			AnalystGroups:         emptyIfNil(stored.AnalystGroups),
			ViewerGroups:          emptyIfNil(stored.ViewerGroups),
			AllowedDomains:        emptyIfNil(stored.AllowedDomains),
			AcceptUnverifiedEmail: stored.AcceptUnverifiedEmail,
			SSOOnly:               stored.SSOOnly || h.Config.OIDCSSOOnly,
		},
	}, nil
}

// sessionViaSSO reports whether the caller's session came from SSO sign-in.
func sessionViaSSO(r *http.Request) bool {
	s := middleware.GetSession(r)
	return s != nil && s.AuthSubject != ""
}

// cleanList trims entries and drops empties.
func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, v := range in {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}

func emptyIfNil(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}
