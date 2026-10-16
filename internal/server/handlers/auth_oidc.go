// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/oauth2"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/oidc"
)

const (
	oidcStateCookie    = "chui_oidc_state"
	oidcNonceCookie    = "chui_oidc_nonce"
	oidcVerifierCookie = "chui_oidc_pkce"
	oidcFlowMaxAge     = 10 * time.Minute
)

// errSSOOnly answers a ClickHouse username/password sign-in while SSO-only
// sign-in is in force.
const errSSOOnly = "Password sign-in is turned off on this server. Sign in with SSO."

// ssoOnlyEnforced reports whether SSO-only sign-in is in force: the saved
// SSO settings ask for it, their provider is loaded, SSO is licensed, and the
// break-glass switch (CHUI_ALLOW_PASSWORD_LOGIN) is off. When SSO is down for
// any reason, password sign-in stays available.
func ssoOnlyEnforced(cfg *config.Config, mgr *oidc.Manager) bool {
	if mgr == nil || cfg == nil || cfg.AllowPasswordLogin {
		return false
	}
	if cfg.ProAccess() == config.ProNone {
		return false
	}
	_, enforced := mgr.SSOOnly()
	return enforced
}

func (h *AuthHandler) passwordLoginBlocked() bool {
	return ssoOnlyEnforced(h.Config, h.OIDC)
}

// RevokePasswordSessions signs out every session that did not come from SSO
// when SSO-only sign-in is in force, so password sessions end at once rather
// than at expiry. Called at startup and after each SSO settings save; it does
// nothing while SSO-only is not enforced.
func RevokePasswordSessions(db *database.DB, cfg *config.Config, mgr *oidc.Manager, actor, ip string) {
	if !ssoOnlyEnforced(cfg, mgr) {
		return
	}
	n, err := db.DeletePasswordSessions()
	if err != nil {
		slog.Error("SSO only: could not sign out password sessions", "error", err)
		return
	}
	if n == 0 {
		return
	}
	slog.Info("SSO only: signed out password sessions", "count", n)
	_ = db.CreateAuditLog(database.AuditLogParams{
		Action:    "sso.password_sessions_revoked",
		Username:  strPtr(actor),
		Details:   strPtr(fmt.Sprintf("SSO only is in force: signed out %d password session(s)", n)),
		IPAddress: strPtr(ip),
	})
}

// oidcCurrent returns the OIDC provider to sign in with and the latest saved
// settings, whose access rules apply even if they could not be reloaded. On
// refusal it returns the message for the login page and the logged error.
func (h *AuthHandler) oidcCurrent() (*oidc.Provider, oidc.Settings, string, error) {
	if h.OIDC == nil {
		return nil, oidc.Settings{}, "SSO is not configured", fmt.Errorf("no active OIDC provider")
	}
	provider, settings, err := h.OIDC.Current()
	if err != nil {
		return nil, settings, "SSO is unavailable: its settings changed and the identity provider could not be loaded; ask an admin to check Admin > SSO", err
	}
	if provider == nil {
		return nil, settings, "SSO is not configured", fmt.Errorf("no active OIDC provider")
	}
	return provider, settings, "", nil
}

// OIDCLogin starts the SSO flow: generate state + nonce + PKCE code verifier,
// stash them in short-lived cookies, and redirect the browser to the identity
// provider with an S256 code challenge.
//
// GET /api/auth/oidc/login
func (h *AuthHandler) OIDCLogin(w http.ResponseWriter, r *http.Request) {
	provider, _, userMsg, err := h.oidcCurrent()
	if provider == nil {
		h.oidcFail(w, r, userMsg, err)
		return
	}

	state, err := randomToken()
	if err != nil {
		h.oidcFail(w, r, "could not start SSO", err)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		h.oidcFail(w, r, "could not start SSO", err)
		return
	}
	// PKCE (RFC 7636): the S256 challenge goes in the authorization URL; the
	// verifier stays with the browser in an HttpOnly cookie and is sent back to
	// the IdP only at code exchange, binding the code to this login attempt.
	verifier := oauth2.GenerateVerifier()

	secure := shouldUseSecureCookie(r, h.Config)
	h.setFlowCookie(w, oidcStateCookie, state, secure)
	h.setFlowCookie(w, oidcNonceCookie, nonce, secure)
	h.setFlowCookie(w, oidcVerifierCookie, verifier, secure)

	http.Redirect(w, r, provider.AuthCodeURL(state, nonce, verifier), http.StatusFound)
}

// OIDCCallback completes the SSO flow: validate state, exchange the code
// (with the PKCE verifier), verify the ID token, map the person to a role and
// to the connection's service account, and create a CH-UI session.
//
// GET /api/auth/oidc/callback
func (h *AuthHandler) OIDCCallback(w http.ResponseWriter, r *http.Request) {
	// The latest saved settings decide who may sign in and with which role,
	// not the ones the loaded provider was built from.
	provider, settings, userMsg, err := h.oidcCurrent()
	if provider == nil {
		h.oidcFail(w, r, userMsg, err)
		return
	}

	q := r.URL.Query()
	if errParam := q.Get("error"); errParam != "" {
		h.oidcFail(w, r, "identity provider returned an error", fmt.Errorf("%s: %s", errParam, q.Get("error_description")))
		return
	}

	// CSRF: the state query param must match the state cookie.
	stateCookie, err := r.Cookie(oidcStateCookie)
	if err != nil || stateCookie.Value == "" || stateCookie.Value != q.Get("state") {
		h.oidcFail(w, r, "invalid SSO state", fmt.Errorf("state mismatch"))
		return
	}
	nonceCookie, err := r.Cookie(oidcNonceCookie)
	if err != nil || nonceCookie.Value == "" {
		h.oidcFail(w, r, "invalid SSO session", fmt.Errorf("missing nonce"))
		return
	}
	verifierCookie, err := r.Cookie(oidcVerifierCookie)
	if err != nil || verifierCookie.Value == "" {
		h.oidcFail(w, r, "invalid SSO session", fmt.Errorf("missing PKCE verifier"))
		return
	}
	// One-shot: clear the flow cookies regardless of outcome.
	secure := shouldUseSecureCookie(r, h.Config)
	h.clearFlowCookie(w, oidcStateCookie, secure)
	h.clearFlowCookie(w, oidcNonceCookie, secure)
	h.clearFlowCookie(w, oidcVerifierCookie, secure)

	claims, err := provider.Exchange(r.Context(), q.Get("code"), nonceCookie.Value, verifierCookie.Value)
	if err != nil {
		h.oidcFail(w, r, "SSO verification failed", err)
		return
	}

	email := strings.ToLower(strings.TrimSpace(claims.Email))
	if email == "" {
		h.oidcFail(w, r, "your identity provider did not return an email", fmt.Errorf("empty email claim"))
		return
	}
	// The email decides the allowed domain and the person's identity, so it
	// must be one the IdP has verified. Without this, an IdP that allows
	// self-signup lets anyone claim a colleague's address.
	if userMsg, reason, ok := oidcEmailVerified(settings, claims); !ok {
		h.auditSSOLoginRefused(r, email, reason)
		h.oidcFail(w, r, userMsg, fmt.Errorf("%s: %s", reason, email))
		return
	}
	if !oidcDomainAllowed(settings, email) {
		h.oidcFail(w, r, "your email domain is not allowed", fmt.Errorf("domain not allowed: %s", email))
		return
	}

	// Resolve the target connection and its ClickHouse service account.
	connID, err := h.oidcConnectionID(settings)
	if err != nil {
		h.oidcFail(w, r, "no connection configured for SSO", err)
		return
	}
	saUser, saEncryptedPw, err := h.DB.GetConnectionSSOAccount(connID)
	if err != nil || saUser == "" || saEncryptedPw == "" {
		h.oidcFail(w, r, "SSO is not finished being set up (no ClickHouse service account on the connection)", fmt.Errorf("missing sso service account for %s", connID))
		return
	}

	role := oidcRole(settings, claims.Groups)

	token := uuid.NewString()
	expiresAt := time.Now().UTC().Add(h.sessionDuration()).Format(time.RFC3339)
	if _, err := h.DB.CreateSession(database.CreateSessionParams{
		ConnectionID:      connID,
		ClickhouseUser:    saUser,
		EncryptedPassword: saEncryptedPw,
		Token:             token,
		ExpiresAt:         expiresAt,
		UserRole:          role,
		AuthSubject:       email,
	}); err != nil {
		h.oidcFail(w, r, "could not create session", err)
		return
	}

	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		MaxAge:   int(h.sessionDuration().Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})

	h.DB.CreateAuditLog(database.AuditLogParams{
		Action:       "user.login",
		Username:     strPtr(email),
		ConnectionID: strPtr(connID),
		Details:      strPtr(fmt.Sprintf("SSO login via OIDC (role: %s, ch_account: %s)", role, saUser)),
		IPAddress:    strPtr(getClientIP(r)),
	})
	slog.Info("SSO login", "user", email, "role", role, "connection", connID)
	closeSetupOnAdminLogin(h.DB, h.Setup, role, email, getClientIP(r))

	http.Redirect(w, r, "/", http.StatusFound)
}

// oidcRole maps the person's IdP groups to a CH-UI role. Users in none of the
// configured groups default to viewer.
func oidcRole(s oidc.Settings, groups []string) string {
	inAny := func(set []string) bool {
		for _, g := range groups {
			for _, want := range set {
				if strings.EqualFold(strings.TrimSpace(g), strings.TrimSpace(want)) {
					return true
				}
			}
		}
		return false
	}
	if inAny(s.AdminGroups) {
		return "admin"
	}
	if inAny(s.AnalystGroups) {
		return "analyst"
	}
	return "viewer"
}

// oidcEmailVerified decides whether the ID token's email may be trusted. An
// explicit email_verified=false is always refused. A missing claim is refused
// unless the admin turned on AcceptUnverifiedEmail for an IdP that never sends
// it. On refusal it returns the message shown to the person and a coarse
// reason for the logs.
func oidcEmailVerified(s oidc.Settings, c *oidc.Claims) (userMsg, reason string, ok bool) {
	if c.EmailVerifiedPresent {
		if c.EmailVerified {
			return "", "", true
		}
		return "your identity provider says this email address is not verified", "email_verified is false", false
	}
	if s.AcceptUnverifiedEmail {
		return "", "", true
	}
	return `your identity provider did not say whether this email address is verified; an admin can enable "Accept emails without email_verified" in the SSO settings if the provider never sends that claim`, "email_verified claim missing", false
}

// auditSSOLoginRefused records an SSO login refused after the ID token
// verified, with the same detail the failure log already carries.
func (h *AuthHandler) auditSSOLoginRefused(r *http.Request, email, reason string) {
	_ = h.DB.CreateAuditLog(database.AuditLogParams{
		Action:    "user.login_failed",
		Username:  strPtr(email),
		Details:   strPtr("SSO login via OIDC refused (" + reason + ")"),
		IPAddress: strPtr(getClientIP(r)),
	})
}

func oidcDomainAllowed(s oidc.Settings, email string) bool {
	if len(s.AllowedDomains) == 0 {
		return true
	}
	at := strings.LastIndex(email, "@")
	if at < 0 {
		return false
	}
	domain := email[at+1:]
	for _, d := range s.AllowedDomains {
		if strings.EqualFold(domain, strings.TrimSpace(d)) {
			return true
		}
	}
	return false
}

// oidcConnectionID returns the connection SSO sessions should use: the
// configured one, or the embedded connection, or the first connection.
func (h *AuthHandler) oidcConnectionID(s oidc.Settings) (string, error) {
	if id := strings.TrimSpace(s.ConnectionID); id != "" {
		return id, nil
	}
	conns, err := h.DB.GetConnections()
	if err != nil {
		return "", err
	}
	for _, c := range conns {
		if c.IsEmbedded {
			return c.ID, nil
		}
	}
	if len(conns) > 0 {
		return conns[0].ID, nil
	}
	return "", fmt.Errorf("no connections exist")
}

func (h *AuthHandler) setFlowCookie(w http.ResponseWriter, name, value string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/api/auth/oidc",
		MaxAge:   int(oidcFlowMaxAge.Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (h *AuthHandler) clearFlowCookie(w http.ResponseWriter, name string, secure bool) {
	http.SetCookie(w, &http.Cookie{
		Name:     name,
		Value:    "",
		Path:     "/api/auth/oidc",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

// oidcFail logs the underlying error and redirects to the login page with a
// short, non-sensitive reason.
func (h *AuthHandler) oidcFail(w http.ResponseWriter, r *http.Request, userMsg string, err error) {
	slog.Warn("OIDC SSO failed", "reason", userMsg, "error", err)
	http.Redirect(w, r, "/login?sso_error="+url.QueryEscape(userMsg), http.StatusFound)
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
