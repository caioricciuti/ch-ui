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
	"syscall"
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
	// scopeActions adds lifecycle actions (run models, start and stop
	// pipelines, run schedules and reports now) on top of write.
	scopeActions = "actions"
)

var supportedScopes = []string{scopeRead, scopeWrite, scopeActions}

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
		"resource_documentation":   "https://ch-ui.com/docs/mcp/",
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
		"service_documentation":                 "https://ch-ui.com/docs/mcp/",
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
		if isLoopbackHost(u.Hostname()) {
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

// errCIMDPrivateAddress is returned when the metadata host is, or resolves
// to, a loopback, private, link-local or otherwise non-public address.
var errCIMDPrivateAddress = errors.New("client_id metadata URL resolves to a private address")

// isNonPublicIP reports whether ip must never be fetched from on behalf of an
// unauthenticated client_id.
func isNonPublicIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsInterfaceLocalMulticast() ||
		ip.IsMulticast() || ip.IsUnspecified()
}

// cimdDialControl runs after DNS resolution, on the exact address the socket
// is about to connect to. Checking here instead of resolving up front closes
// the DNS rebinding gap (public answer for the check, private for the dial).
func cimdDialControl(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("client metadata dial: %w", err)
	}
	ip := net.ParseIP(host)
	if ip == nil || isNonPublicIP(ip) {
		return errCIMDPrivateAddress
	}
	return nil
}

// cimdHTTPClient fetches metadata documents: no proxy (a proxy would make
// the dialed address the proxy's), every dial checked by cimdDialControl,
// no redirects, 5 s overall.
var cimdHTTPClient = &http.Client{
	Timeout: 5 * time.Second,
	Transport: &http.Transport{
		Proxy: nil,
		DialContext: (&net.Dialer{
			Timeout: 5 * time.Second,
			Control: cimdDialControl,
		}).DialContext,
		TLSHandshakeTimeout:   5 * time.Second,
		ResponseHeaderTimeout: 5 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       30 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects are not followed for client metadata")
	},
}

// fetchClientMetadata GETs a Client ID Metadata Document. SSRF guards: https
// only, no redirects, public address checked at dial time, 5 s, 64 KB, and
// the document's client_id must equal its own URL.
func fetchClientMetadata(id string) (*database.OAuthClient, error) {
	u, err := url.Parse(id)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.Fragment != "" {
		return nil, errors.New("client_id metadata URL must be https without a fragment")
	}
	req, _ := http.NewRequest(http.MethodGet, id, nil)
	req.Header.Set("Accept", "application/json")
	resp, err := cimdHTTPClient.Do(req)
	if err != nil {
		if errors.Is(err, errCIMDPrivateAddress) {
			return nil, errCIMDPrivateAddress
		}
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
	if !redirectRegistered(client.RedirectURIs, redirectURI) {
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
// canonical space-separated string. Empty means read. actions is only valid
// together with write.
func normalizeScope(raw string) (string, error) {
	if strings.TrimSpace(raw) == "" {
		return scopeRead, nil
	}
	seen := map[string]bool{}
	for _, s := range strings.Fields(raw) {
		switch s {
		case scopeRead, scopeWrite, scopeActions:
			seen[s] = true
		default:
			return "", fmt.Errorf("unknown scope %q; supported: read, write, actions", s)
		}
	}
	if seen[scopeActions] && !seen[scopeWrite] {
		return "", fmt.Errorf("scope actions requires write")
	}
	out := []string{scopeRead}
	if seen[scopeWrite] {
		out = append(out, scopeWrite)
	}
	if seen[scopeActions] {
		out = append(out, scopeActions)
	}
	return strings.Join(out, " "), nil
}

// hasScope reports whether the space-separated scope string grants s.
func hasScope(scope, s string) bool {
	return containsExact(strings.Fields(scope), s)
}

// redirectRegistered reports whether the redirect_uri of an authorization
// request matches one the client registered. Everything compares exactly
// except http loopback URIs: a native client listens on whatever port is
// free, so RFC 8252 section 7.3 requires the server to ignore the port and
// still compare scheme, host and path. Claude Code registers
// http://localhost/callback and sends http://localhost:60146/callback.
func redirectRegistered(registered []string, candidate string) bool {
	if containsExact(registered, candidate) {
		return true
	}
	c, err := url.Parse(candidate)
	if err != nil || c.Scheme != "http" || !isLoopbackHost(c.Hostname()) {
		return false
	}
	for _, r := range registered {
		u, err := url.Parse(r)
		if err != nil || u.Scheme != "http" || !isLoopbackHost(u.Hostname()) {
			continue
		}
		if u.Hostname() == c.Hostname() && u.Path == c.Path && u.RawQuery == c.RawQuery {
			return true
		}
	}
	return false
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

// grantedScope is what a person's CH-UI role lets them grant of the scope a
// client asked for. Clients ask for everything they could use (Claude Code
// always sends read write actions), so a read-only person grants read and
// the consent page says which parts were left out.
func grantedScope(requested, role string) string {
	if role == "admin" || role == "analyst" {
		return requested
	}
	return scopeRead
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
		"granted":       strings.Fields(grantedScope(req.Scope, sess.UserRole)),
		"role":          sess.UserRole,
		"connection":    connName,
		"user":          middleware.Actor(sess),
		"expires_at":    req.ExpiresAt,
	})
}

func (h *OAuthHandler) consentApprove(w http.ResponseWriter, r *http.Request) {
	req := h.loadPending(w, r)
	if req == nil {
		return
	}
	sess := middleware.GetSession(r)
	scope := grantedScope(req.Scope, sess.UserRole)
	// An SSO person with a per-person ClickHouse user grants a reference to
	// that user, never a copy of its password: the password rotates at every
	// sign-in, and the tokens must stop working when the user is removed.
	chUser, chPasswordEnc, identityID := sess.ClickhouseUser, sess.EncryptedPassword, ""
	if sess.SSOIdentityID != "" {
		u, err := h.DB.GetSSOClickHouseUser(sess.SSOIdentityID, sess.ConnectionID)
		if err != nil {
			slog.Error("oauth: resolve per-person user failed", "error", err)
			writeError(w, http.StatusInternalServerError, "Could not issue authorization code")
			return
		}
		if u != nil {
			chUser, chPasswordEnc, identityID = u.CHUser, "", sess.SSOIdentityID
		}
	}
	code := oauthRandomToken()
	if err := h.DB.CreateOAuthCode(&database.OAuthCode{
		CodeHash:      hashToken(code),
		ClientID:      req.ClientID,
		ClientName:    req.ClientName,
		RedirectURI:   req.RedirectURI,
		Scope:         scope,
		CodeChallenge: req.CodeChallenge,
		ConnectionID:  sess.ConnectionID,
		CHUser:        chUser,
		CHPasswordEnc: chPasswordEnc,
		SSOIdentityID: identityID,
		Subject:       middleware.Actor(sess),
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
	subj := middleware.Actor(sess)
	details := "client: " + req.ClientName + ", scope: " + scope
	if scope != req.Scope {
		details += " (requested: " + req.Scope + ", role " + sess.UserRole + ")"
	}
	h.DB.CreateAuditLog(database.AuditLogParams{Action: "mcp.oauth.consent", Username: &subj, ClickhouseUser: &sess.ClickhouseUser, ConnectionID: &sess.ConnectionID, Details: strPtr(details)})
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
	// No issue-time role yet: the consenting session supplies it.
	h.issueTokens(w, c.ClientID, c.ClientName, c.Scope, c.ConnectionID, c.CHUser, c.CHPasswordEnc, c.SSOIdentityID, c.Subject, "")
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
	if old.SSOIdentityID != "" {
		// The grant references a per-person ClickHouse user; once that user
		// is locked or dropped the whole grant ends, refresh token included.
		u, err := h.DB.GetSSOClickHouseUser(old.SSOIdentityID, old.ConnectionID)
		if err != nil {
			oauthError(w, http.StatusInternalServerError, "server_error", "person lookup failed")
			return
		}
		if u == nil || u.Locked() {
			h.DB.RevokeOAuthRefreshToken(t.TokenHash)
			h.DB.RevokeMCPKey(old.ID)
			oauthError(w, http.StatusBadRequest, "invalid_grant", "the person behind this token no longer has access")
			return
		}
	}
	// Rotate: the old refresh token and access token die together. The
	// claim is one conditional UPDATE, so of two concurrent requests with
	// the same refresh token exactly one wins; the other is refused like a
	// reused token. Refresh tokens carry no family id, so reuse is refused
	// rather than revoking every token descended from the grant.
	claimed, err := h.claimRefreshToken(t.TokenHash, clientID)
	if err != nil {
		oauthError(w, http.StatusInternalServerError, "server_error", "refresh token update failed")
		return
	}
	if !claimed {
		slog.Warn("oauth: refresh token reused or raced", "client_id", clientID, "key_id", t.KeyID)
		oauthError(w, http.StatusBadRequest, "invalid_grant", "refresh token is unknown, revoked, or belongs to another client")
		return
	}
	h.DB.RevokeMCPKey(old.ID)
	h.issueTokens(w, old.ClientID, old.Name, t.Scope, old.ConnectionID, old.CHUser, old.CHPasswordEnc, old.SSOIdentityID, old.Subject, old.GrantRole)
}

// claimRefreshToken revokes a live refresh token and reports whether this
// call was the one that revoked it.
// TODO: move to internal/database next to RevokeOAuthRefreshToken.
func (h *OAuthHandler) claimRefreshToken(tokenHash, clientID string) (bool, error) {
	res, err := h.DB.Conn().Exec(
		`UPDATE oauth_refresh_tokens SET revoked_at = ? WHERE token_hash = ? AND client_id = ? AND revoked_at IS NULL`,
		time.Now().UTC().Format(time.RFC3339), tokenHash, clientID,
	)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return n == 1, nil
}

// issueTokens mints an access token (an mcp_keys row of kind oauth) and a
// refresh token, and writes the RFC 6749 token response. The person's role
// is resolved again first (grantRole is the role recorded on the previous
// token, "" on the first exchange): write scope that their role no longer
// allows is dropped from the new tokens for good. While nothing is known
// about their role (no override, session or recorded role) the scope is
// kept, and /mcp serves the token read-only until they sign in again.
// ssoIdentityID, when set, references the person's per-person ClickHouse
// user and chPasswordEnc is empty; see database.MCPKey.SSOIdentityID.
func (h *OAuthHandler) issueTokens(w http.ResponseWriter, clientID, clientName, scope, connectionID, chUser, chPasswordEnc, ssoIdentityID, subject, grantRole string) {
	role, known, err := h.DB.OAuthKeyRoleKnown(&database.MCPKey{Kind: "oauth", ConnectionID: connectionID, CHUser: chUser, Subject: subject, GrantRole: grantRole})
	if err != nil {
		slog.Warn("oauth: resolve role failed; issuing read scope", "subject", subject, "error", err)
	}
	if !known {
		role = ""
	}
	if strings.Contains(scope, scopeWrite) && (known || err != nil) && role != "admin" && role != "analyst" {
		slog.Info("oauth: write scope dropped, role no longer allows it", "subject", subject, "role", role, "client_id", clientID)
		scope = scopeRead
	}
	plaintext, hash, prefix := mcpserver.GenerateKey()
	exp := time.Now().UTC().Add(accessTokenTTL).Format(time.RFC3339)
	scopes := "read"
	if strings.Contains(scope, scopeWrite) {
		scopes = "read_write"
	}
	// Dropping write above drops actions with it: scope is then just read.
	allowActions := scopes == "read_write" && hasScope(scope, scopeActions)
	name := clientName
	if !strings.Contains(name, subject) {
		name = clientName + " (" + subject + ")"
	}
	key, err := h.DB.CreateMCPKey(database.CreateMCPKeyParams{
		Name: name, KeyHash: hash, KeyPrefix: prefix, ConnectionID: connectionID,
		CHUser: chUser, CHPasswordEnc: chPasswordEnc, SSOIdentityID: ssoIdentityID, Scopes: scopes, AllowActions: allowActions,
		CreatedBy: "oauth:" + subject, ExpiresAt: &exp,
		Kind: "oauth", Subject: subject, ClientID: clientID, GrantRole: role,
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
