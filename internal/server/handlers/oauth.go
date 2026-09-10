package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/mcpserver"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// OAuthHandler makes CH-UI an OAuth 2.1 authorization server for its own MCP
// endpoint, so MCP clients that only speak OAuth (claude.ai connectors,
// ChatGPT, and the OAuth path of Claude Code, Cursor, VS Code) can connect as
// the signed-in person instead of with a shared admin-created key.
//
// Flow: client registers (DCR) or brings a Client ID Metadata Document;
// /oauth/authorize validates the request and parks it; the SPA consent page
// (session required) approves it; /oauth/token exchanges the code (PKCE
// S256 mandatory) for an access token that is an mcp_keys row of kind
// "oauth" bound to the approving session's connection and ClickHouse
// credentials, plus a rotating refresh token. Public clients only.
type OAuthHandler struct {
	DB     *database.DB
	Config *config.Config

	cimdMu    sync.Mutex
	cimdCache map[string]cimdEntry
}

const (
	oauthRequestTTL = 10 * time.Minute
	oauthCodeTTL    = 10 * time.Minute
	accessTokenTTL  = time.Hour
	refreshTokenTTL = 30 * 24 * time.Hour

	scopeRead  = "read"
	scopeWrite = "write"
)

var supportedScopes = []string{scopeRead, scopeWrite}

// ---- metadata ----

// ProtectedResourceMetadata serves RFC 9728 for /mcp.
func (h *OAuthHandler) ProtectedResourceMetadata(w http.ResponseWriter, r *http.Request) {
	base := mcpserver.BaseURL(r, h.Config)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"resource":                 base + "/mcp",
		"authorization_servers":    []string{base},
		"scopes_supported":         supportedScopes,
		"bearer_methods_supported": []string{"header"},
		"resource_name":            "CH-UI MCP server",
		"resource_documentation":   "https://github.com/caioricciuti/ch-ui/blob/main/docs/mcp.md",
	})
}

// AuthorizationServerMetadata serves RFC 8414.
func (h *OAuthHandler) AuthorizationServerMetadata(w http.ResponseWriter, r *http.Request) {
	base := mcpserver.BaseURL(r, h.Config)
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Cache-Control", "public, max-age=300")
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer":                                base,
		"authorization_endpoint":                base + "/oauth/authorize",
		"token_endpoint":                        base + "/oauth/token",
		"registration_endpoint":                 base + "/oauth/register",
		"response_types_supported":              []string{"code"},
		"response_modes_supported":              []string{"query"},
		"grant_types_supported":                 []string{"authorization_code", "refresh_token"},
		"code_challenge_methods_supported":      []string{"S256"},
		"token_endpoint_auth_methods_supported": []string{"none"},
		"scopes_supported":                      supportedScopes,
		"client_id_metadata_document_supported": true,
		"service_documentation":                 "https://github.com/caioricciuti/ch-ui/blob/main/docs/mcp.md",
	})
}

// ---- dynamic client registration (RFC 7591, public clients) ----

type clientRegistration struct {
	ClientName              string   `json:"client_name"`
	RedirectURIs            []string `json:"redirect_uris"`
	TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	GrantTypes              []string `json:"grant_types"`
	ResponseTypes           []string `json:"response_types"`
	ClientURI               string   `json:"client_uri"`
}

func (h *OAuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req clientRegistration
	if err := json.NewDecoder(io.LimitReader(r.Body, 64<<10)).Decode(&req); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "body must be JSON client metadata")
		return
	}
	if req.TokenEndpointAuthMethod != "" && req.TokenEndpointAuthMethod != "none" {
		oauthError(w, http.StatusBadRequest, "invalid_client_metadata", "only public clients (token_endpoint_auth_method=none) are supported")
		return
	}
	if len(req.RedirectURIs) == 0 || len(req.RedirectURIs) > 10 {
		oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", "between 1 and 10 redirect_uris are required")
		return
	}
	for _, u := range req.RedirectURIs {
		if err := validateRedirectURI(u); err != nil {
			oauthError(w, http.StatusBadRequest, "invalid_redirect_uri", err.Error())
			return
		}
	}
	name := strings.TrimSpace(req.ClientName)
	if name == "" {
		name = "MCP client"
	}
	if len(name) > 100 {
		name = name[:100]
	}
	c, err := h.DB.CreateOAuthClient(name, req.RedirectURIs)
	if err != nil {
		slog.Error("oauth: register failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "could not register client")
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{
		"client_id":                  c.ID,
		"client_name":                c.Name,
		"redirect_uris":              c.RedirectURIs,
		"token_endpoint_auth_method": "none",
		"grant_types":                []string{"authorization_code", "refresh_token"},
		"response_types":             []string{"code"},
		"client_id_issued_at":        time.Now().Unix(),
	})
}

// validateRedirectURI accepts https URLs and http loopback URLs (native
// clients per RFC 8252). Custom schemes (cursor://, vscode://) are accepted
// too since desktop MCP clients use them.
func validateRedirectURI(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return fmt.Errorf("redirect_uri %q is not an absolute URL", raw)
	}
	if u.Fragment != "" {
		return fmt.Errorf("redirect_uri must not contain a fragment")
	}
	switch u.Scheme {
	case "https":
		return nil
	case "http":
		host := u.Hostname()
		if host == "localhost" || host == "127.0.0.1" || host == "::1" {
			return nil
		}
		return fmt.Errorf("http redirect_uri is only allowed for loopback hosts")
	default:
		if u.Host == "" && u.Opaque == "" && u.Path == "" {
			return fmt.Errorf("redirect_uri %q is malformed", raw)
		}
		return nil // custom scheme
	}
}

// ---- Client ID Metadata Documents (SEP-991) ----

type cimdEntry struct {
	client   *database.OAuthClient
	fetched  time.Time
	fetchErr error
}

// resolveClient returns the client for client_id: a registered id, or an
// https URL whose JSON document describes the client.
func (h *OAuthHandler) resolveClient(id string) (*database.OAuthClient, error) {
	if strings.HasPrefix(id, "https://") {
		return h.fetchCIMD(id)
	}
	c, err := h.DB.GetOAuthClient(id)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errors.New("unknown client_id")
	}
	return c, nil
}

func (h *OAuthHandler) fetchCIMD(id string) (*database.OAuthClient, error) {
	h.cimdMu.Lock()
	if h.cimdCache == nil {
		h.cimdCache = map[string]cimdEntry{}
	}
	if e, ok := h.cimdCache[id]; ok && time.Since(e.fetched) < 10*time.Minute {
		h.cimdMu.Unlock()
		return e.client, e.fetchErr
	}
	h.cimdMu.Unlock()

	client, err := fetchClientMetadata(id)
	h.cimdMu.Lock()
	h.cimdCache[id] = cimdEntry{client: client, fetched: time.Now(), fetchErr: err}
	if len(h.cimdCache) > 256 {
		for k, e := range h.cimdCache {
			if time.Since(e.fetched) > 10*time.Minute {
				delete(h.cimdCache, k)
			}
		}
	}
	h.cimdMu.Unlock()
	return client, err
}

// fetchClientMetadata GETs a Client ID Metadata Document. SSRF guards: https
// only, no redirects, public host, 5 s, 64 KB, and the document's client_id
// must equal its own URL.
func fetchClientMetadata(id string) (*database.OAuthClient, error) {
	u, err := url.Parse(id)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Fragment != "" {
		return nil, errors.New("client_id metadata URL must be https without a fragment")
	}
	if ips, err := net.LookupIP(u.Hostname()); err == nil {
		for _, ip := range ips {
			if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() {
				return nil, errors.New("client_id metadata URL resolves to a private address")
			}
		}
	}
	httpClient := &http.Client{
		Timeout: 5 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirects are not followed for client metadata")
		},
	}
	req, _ := http.NewRequest(http.MethodGet, id, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch client metadata: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("client metadata returned HTTP %d", resp.StatusCode)
	}
	var doc struct {
		ClientID                string   `json:"client_id"`
		ClientName              string   `json:"client_name"`
		RedirectURIs            []string `json:"redirect_uris"`
		TokenEndpointAuthMethod string   `json:"token_endpoint_auth_method"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&doc); err != nil {
		return nil, fmt.Errorf("client metadata is not valid JSON: %w", err)
	}
	if doc.ClientID != id {
		return nil, errors.New("client metadata client_id does not match its URL")
	}
	if doc.TokenEndpointAuthMethod != "" && doc.TokenEndpointAuthMethod != "none" {
		return nil, errors.New("only public clients are supported")
	}
	if len(doc.RedirectURIs) == 0 {
		return nil, errors.New("client metadata has no redirect_uris")
	}
	for _, ru := range doc.RedirectURIs {
		if err := validateRedirectURI(ru); err != nil {
			return nil, err
		}
	}
	name := strings.TrimSpace(doc.ClientName)
	if name == "" {
		name = u.Host
	}
	return &database.OAuthClient{ID: id, Name: name, RedirectURIs: doc.RedirectURIs}, nil
}

// ---- authorize ----

// Authorize validates the request and sends the browser to the consent page.
func (h *OAuthHandler) Authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	clientID := q.Get("client_id")
	redirectURI := q.Get("redirect_uri")
	if clientID == "" || redirectURI == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "client_id and redirect_uri are required")
		return
	}
	client, err := h.resolveClient(clientID)
	if err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_client", err.Error())
		return
	}
	if !containsExact(client.RedirectURIs, redirectURI) {
		// Never redirect to an unregistered URI.
		oauthError(w, http.StatusBadRequest, "invalid_request", "redirect_uri is not registered for this client")
		return
	}
	state := q.Get("state")
	redirectErr := func(code, desc string) {
		u, _ := url.Parse(redirectURI)
		v := u.Query()
		v.Set("error", code)
		v.Set("error_description", desc)
		if state != "" {
			v.Set("state", state)
		}
		u.RawQuery = v.Encode()
		http.Redirect(w, r, u.String(), http.StatusFound)
	}
	if q.Get("response_type") != "code" {
		redirectErr("unsupported_response_type", "response_type must be code")
		return
	}
	challenge := q.Get("code_challenge")
	if challenge == "" || q.Get("code_challenge_method") != "S256" {
		redirectErr("invalid_request", "PKCE with code_challenge_method=S256 is required")
		return
	}
	if res := q.Get("resource"); res != "" && res != mcpserver.BaseURL(r, h.Config)+"/mcp" {
		redirectErr("invalid_target", "resource must be "+mcpserver.BaseURL(r, h.Config)+"/mcp")
		return
	}
	scope, err := normalizeScope(q.Get("scope"))
	if err != nil {
		redirectErr("invalid_scope", err.Error())
		return
	}

	req := &database.OAuthRequest{
		ClientID:      client.ID,
		ClientName:    client.Name,
		RedirectURI:   redirectURI,
		Scope:         scope,
		State:         state,
		CodeChallenge: challenge,
		ExpiresAt:     time.Now().UTC().Add(oauthRequestTTL).Format(time.RFC3339),
	}
	if err := h.DB.CreateOAuthRequest(req); err != nil {
		slog.Error("oauth: store request failed", "error", err)
		redirectErr("server_error", "could not store the authorization request")
		return
	}
	http.Redirect(w, r, mcpserver.BaseURL(r, h.Config)+"/oauth/consent?request="+url.QueryEscape(req.ID), http.StatusFound)
}

// normalizeScope validates the requested scopes and returns them as a
// canonical space-separated string. Empty means read.
func normalizeScope(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return scopeRead, nil
	}
	seen := map[string]bool{}
	for _, s := range strings.Fields(raw) {
		switch s {
		case scopeRead, scopeWrite:
			seen[s] = true
		default:
			return "", fmt.Errorf("unknown scope %q; supported: read, write", s)
		}
	}
	out := []string{scopeRead}
	if seen[scopeWrite] {
		out = append(out, scopeWrite)
	}
	return strings.Join(out, " "), nil
}

func containsExact(list []string, v string) bool {
	for _, s := range list {
		if s == v {
			return true
		}
	}
	return false
}

// ---- consent (session required, called by the SPA) ----

func (h *OAuthHandler) ConsentRoutes(r chi.Router) {
	r.Get("/{id}", h.consentInfo)
	r.Post("/{id}/approve", h.consentApprove)
	r.Post("/{id}/deny", h.consentDeny)
}

func (h *OAuthHandler) loadPending(w http.ResponseWriter, r *http.Request) *database.OAuthRequest {
	req, err := h.DB.GetOAuthRequest(chi.URLParam(r, "id"))
	if err != nil || req == nil {
		writeError(w, http.StatusNotFound, "Authorization request not found or expired; start again from your MCP client")
		return nil
	}
	if exp, err := time.Parse(time.RFC3339, req.ExpiresAt); err != nil || time.Now().After(exp) {
		h.DB.DeleteOAuthRequest(req.ID)
		writeError(w, http.StatusGone, "Authorization request expired; start again from your MCP client")
		return nil
	}
	return req
}

func (h *OAuthHandler) consentInfo(w http.ResponseWriter, r *http.Request) {
	req := h.loadPending(w, r)
	if req == nil {
		return
	}
	sess := middleware.GetSession(r)
	connName := sess.ConnectionID
	if conn, err := h.DB.GetConnectionByID(sess.ConnectionID); err == nil && conn != nil {
		connName = conn.Name
	}
	ru, _ := url.Parse(req.RedirectURI)
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
		"client_name":   req.ClientName,
		"client_id":     req.ClientID,
		"redirect_host": ru.Host,
		"scopes":        strings.Fields(req.Scope),
		"connection":    connName,
		"user":          subjectOf(sess),
		"expires_at":    req.ExpiresAt,
	})
}

func subjectOf(s *middleware.SessionInfo) string {
	if s.AuthSubject != "" {
		return s.AuthSubject
	}
	return s.ClickhouseUser
}

func (h *OAuthHandler) consentApprove(w http.ResponseWriter, r *http.Request) {
	req := h.loadPending(w, r)
	if req == nil {
		return
	}
	sess := middleware.GetSession(r)
	if strings.Contains(req.Scope, scopeWrite) && sess.UserRole != "admin" && sess.UserRole != "analyst" {
		writeError(w, http.StatusForbidden, "Your role is read-only in CH-UI, so you cannot grant write scope")
		return
	}
	code := oauthRandomToken()
	if err := h.DB.CreateOAuthCode(&database.OAuthCode{
		CodeHash:      hashToken(code),
		ClientID:      req.ClientID,
		ClientName:    req.ClientName,
		RedirectURI:   req.RedirectURI,
		Scope:         req.Scope,
		CodeChallenge: req.CodeChallenge,
		ConnectionID:  sess.ConnectionID,
		CHUser:        sess.ClickhouseUser,
		CHPasswordEnc: sess.EncryptedPassword,
		Subject:       subjectOf(sess),
		ExpiresAt:     time.Now().UTC().Add(oauthCodeTTL).Format(time.RFC3339),
	}); err != nil {
		slog.Error("oauth: create code failed", "error", err)
		writeError(w, http.StatusInternalServerError, "Could not issue authorization code")
		return
	}
	h.DB.DeleteOAuthRequest(req.ID)
	u, _ := url.Parse(req.RedirectURI)
	v := u.Query()
	v.Set("code", code)
	if req.State != "" {
		v.Set("state", req.State)
	}
	u.RawQuery = v.Encode()
	subj := subjectOf(sess)
	h.DB.CreateAuditLog(database.AuditLogParams{Action: "mcp.oauth.consent", Username: &subj, ConnectionID: &sess.ConnectionID, Details: strPtr("client: " + req.ClientName + ", scope: " + req.Scope)})
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "redirect_url": u.String()})
}

func (h *OAuthHandler) consentDeny(w http.ResponseWriter, r *http.Request) {
	req := h.loadPending(w, r)
	if req == nil {
		return
	}
	h.DB.DeleteOAuthRequest(req.ID)
	u, _ := url.Parse(req.RedirectURI)
	v := u.Query()
	v.Set("error", "access_denied")
	v.Set("error_description", "the user denied the request")
	if req.State != "" {
		v.Set("state", req.State)
	}
	u.RawQuery = v.Encode()
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "redirect_url": u.String()})
}

// ---- token ----

func (h *OAuthHandler) Token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		oauthError(w, http.StatusBadRequest, "invalid_request", "body must be application/x-www-form-urlencoded")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	h.DB.PurgeExpiredOAuthState()
	switch r.PostForm.Get("grant_type") {
	case "authorization_code":
		h.tokenFromCode(w, r)
	case "refresh_token":
		h.tokenFromRefresh(w, r)
	default:
		oauthError(w, http.StatusBadRequest, "unsupported_grant_type", "grant_type must be authorization_code or refresh_token")
	}
}

func (h *OAuthHandler) tokenFromCode(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	code, verifier, clientID, redirectURI := f.Get("code"), f.Get("code_verifier"), f.Get("client_id"), f.Get("redirect_uri")
	if code == "" || verifier == "" || clientID == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "code, code_verifier and client_id are required")
		return
	}
	c, err := h.DB.ConsumeOAuthCode(hashToken(code))
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "code lookup failed")
		return
	}
	if c == nil {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "authorization code is unknown, expired, or already used")
		return
	}
	if exp, err := time.Parse(time.RFC3339, c.ExpiresAt); err != nil || time.Now().After(exp) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "authorization code expired")
		return
	}
	if subtle.ConstantTimeCompare([]byte(c.ClientID), []byte(clientID)) != 1 {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "client_id does not match the authorization code")
		return
	}
	if redirectURI != "" && redirectURI != c.RedirectURI {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "redirect_uri does not match the authorization request")
		return
	}
	if res := f.Get("resource"); res != "" && res != mcpserver.BaseURL(r, h.Config)+"/mcp" {
		oauthError(w, http.StatusBadRequest, "invalid_target", "resource must be "+mcpserver.BaseURL(r, h.Config)+"/mcp")
		return
	}
	sum := sha256.Sum256([]byte(verifier))
	if subtle.ConstantTimeCompare([]byte(base64.RawURLEncoding.EncodeToString(sum[:])), []byte(c.CodeChallenge)) != 1 {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "PKCE verification failed")
		return
	}
	h.issueTokens(w, c.ClientID, c.ClientName, c.Scope, c.ConnectionID, c.CHUser, c.CHPasswordEnc, c.Subject)
}

func (h *OAuthHandler) tokenFromRefresh(w http.ResponseWriter, r *http.Request) {
	f := r.PostForm
	raw, clientID := f.Get("refresh_token"), f.Get("client_id")
	if raw == "" || clientID == "" {
		oauthError(w, http.StatusBadRequest, "invalid_request", "refresh_token and client_id are required")
		return
	}
	t, err := h.DB.GetOAuthRefreshToken(hashToken(raw))
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "refresh token lookup failed")
		return
	}
	if t == nil || t.RevokedAt != nil || t.ClientID != clientID {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is unknown, revoked, or belongs to another client")
		return
	}
	if exp, err := time.Parse(time.RFC3339, t.ExpiresAt); err != nil || time.Now().After(exp) {
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token expired")
		return
	}
	old, err := h.DB.GetMCPKeyByID(t.KeyID)
	if err != nil || old == nil || old.RevokedAt != nil {
		h.DB.RevokeOAuthRefreshToken(t.TokenHash)
		oauthError(w, http.StatusBadRequest, "invalid_grant", "the grant behind this refresh token was revoked")
		return
	}
	// Rotate: the old refresh token and access token die together.
	h.DB.RevokeOAuthRefreshToken(t.TokenHash)
	h.DB.RevokeMCPKey(old.ID)
	h.issueTokens(w, old.ClientID, old.Name, t.Scope, old.ConnectionID, old.CHUser, old.CHPasswordEnc, old.Subject)
}

// issueTokens mints an access token (an mcp_keys row of kind oauth) and a
// refresh token, and writes the RFC 6749 token response.
func (h *OAuthHandler) issueTokens(w http.ResponseWriter, clientID, clientName, scope, connectionID, chUser, chPasswordEnc, subject string) {
	plaintext, hash, prefix := mcpserver.GenerateKey()
	exp := time.Now().UTC().Add(accessTokenTTL).Format(time.RFC3339)
	scopes := "read"
	if strings.Contains(scope, scopeWrite) {
		scopes = "read_write"
	}
	name := clientName
	if !strings.Contains(name, subject) {
		name = clientName + " (" + subject + ")"
	}
	key, err := h.DB.CreateMCPKey(database.CreateMCPKeyParams{
		Name: name, KeyHash: hash, KeyPrefix: prefix, ConnectionID: connectionID,
		CHUser: chUser, CHPasswordEnc: chPasswordEnc, Scopes: scopes,
		CreatedBy: "oauth:" + subject, ExpiresAt: &exp,
		Kind: "oauth", Subject: subject, ClientID: clientID,
	})
	if err != nil {
		slog.Error("oauth: create access token failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "could not issue access token")
		return
	}
	refresh := oauthRandomToken()
	if err := h.DB.CreateOAuthRefreshToken(&database.OAuthRefreshToken{
		TokenHash: hashToken(refresh), KeyID: key.ID, ClientID: clientID, Scope: scope,
		ExpiresAt: time.Now().UTC().Add(refreshTokenTTL).Format(time.RFC3339),
	}); err != nil {
		slog.Error("oauth: create refresh token failed", "error", err)
		oauthError(w, http.StatusInternalServerError, "server_error", "could not issue refresh token")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"access_token":  plaintext,
		"token_type":    "Bearer",
		"expires_in":    int(accessTokenTTL.Seconds()),
		"refresh_token": refresh,
		"scope":         scope,
	})
}

// ---- helpers ----

func oauthRandomToken() string {
	b := make([]byte, 32)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func hashToken(s string) string {
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])
}

func oauthError(w http.ResponseWriter, status int, code, desc string) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, status, map[string]any{"error": code, "error_description": desc})
}
