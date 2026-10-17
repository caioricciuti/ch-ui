package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/mcpserver"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// oauthRig wires the OAuth handler, a fake logged-in session for the consent
// routes, and the MCP endpoint, so the whole flow runs in-process.
func oauthRig(t *testing.T) (*chi.Mux, *database.DB, string) {
	t.Helper()
	return oauthRigWithSession(t, func(connID, enc string) *middleware.SessionInfo {
		return &middleware.SessionInfo{
			ID: "s1", ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: enc, UserRole: "analyst", AuthSubject: "alice@example.com",
		}
	})
}

// oauthRigWithSession is oauthRig with the consenting session built by
// session, from the connection id and the encrypted password "chpass".
func oauthRigWithSession(t *testing.T, session func(connID, enc string) *middleware.SessionInfo) (*chi.Mux, *database.DB, string) {
	t.Helper()
	db, err := dbtest.Open(t, "oauth.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: "cht_00000000000000000000000000000000", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}
	cfg := &config.Config{AppSecretKey: "oauth-test-secret", AppURL: "https://chui.example"}
	enc, _ := crypto.Encrypt("chpass", cfg.AppSecretKey)
	// The stored session behind the fake one below: token issue reads the
	// person's role from it.
	if _, err := db.CreateSession(database.CreateSessionParams{
		ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: enc, Token: "oauth-rig-session",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), UserRole: "analyst", AuthSubject: "alice@example.com",
	}); err != nil {
		t.Fatalf("create session: %v", err)
	}

	h := &OAuthHandler{DB: db, Config: cfg}
	r := chi.NewRouter()
	trusted, err := cfg.TrustedProxyPrefixes()
	if err != nil {
		t.Fatal(err)
	}
	r.Use(middleware.RealIP(trusted))
	r.Get("/.well-known/oauth-protected-resource", h.ProtectedResourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", h.AuthorizationServerMetadata)
	r.Get("/oauth/authorize", h.Authorize)
	r.Post("/oauth/token", h.Token)
	r.Post("/oauth/register", h.Register)
	r.Route("/api/oauth/consent", func(cr chi.Router) {
		cr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := middleware.SetSession(req.Context(), session(connID, enc))
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		h.ConsentRoutes(cr)
	})
	gw := tunnel.NewGateway(db)
	r.Handle("/mcp", mcpserver.Handler(mcpserver.Deps{DB: db, Gateway: gw, Config: cfg, ModelRunner: models.NewRunner(db, gw, cfg.AppSecretKey)}))
	return r, db, connID
}

func do(t *testing.T, r http.Handler, method, target string, body string, form bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "chui.example"
	req.RemoteAddr = "127.0.0.1:40000"
	req.Header.Set("X-Forwarded-Proto", "https")
	if form {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func TestOAuthMetadata(t *testing.T) {
	r, _, _ := oauthRig(t)
	rec := do(t, r, "GET", "/.well-known/oauth-protected-resource", "", false)
	var prm map[string]any
	json.Unmarshal(rec.Body.Bytes(), &prm)
	if prm["resource"] != "https://chui.example/mcp" {
		t.Errorf("resource: %v", prm["resource"])
	}
	rec = do(t, r, "GET", "/.well-known/oauth-authorization-server", "", false)
	var as map[string]any
	json.Unmarshal(rec.Body.Bytes(), &as)
	if as["issuer"] != "https://chui.example" || as["token_endpoint"] != "https://chui.example/oauth/token" {
		t.Errorf("as metadata: %v", as)
	}
	if as["client_id_metadata_document_supported"] != true {
		t.Error("CIMD support should be advertised")
	}
}

func TestOAuthFullFlow(t *testing.T) {
	r, db, _ := oauthRig(t)

	// 1. DCR
	rec := do(t, r, "POST", "/oauth/register", `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"token_endpoint_auth_method":"none"}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &reg)

	// 2. authorize -> consent redirect
	verifier := "a-very-long-verifier-string-with-enough-entropy-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	challenge := base64.RawURLEncoding.EncodeToString(sum[:])
	q := url.Values{
		"response_type": {"code"}, "client_id": {reg.ClientID},
		"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "state": {"xyz"},
		"code_challenge": {challenge}, "code_challenge_method": {"S256"},
		"scope": {"read write"}, "resource": {"https://chui.example/mcp"},
	}
	rec = do(t, r, "GET", "/oauth/authorize?"+q.Encode(), "", false)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize: %d %s", rec.Code, rec.Body.String())
	}
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if loc.Path != "/oauth/consent" {
		t.Fatalf("expected consent redirect, got %s", loc)
	}
	reqID := loc.Query().Get("request")

	// 3. consent info + approve (session-bound)
	rec = do(t, r, "GET", "/api/oauth/consent/"+reqID, "", false)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"user":"alice@example.com"`) {
		t.Fatalf("consent info: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, r, "POST", "/api/oauth/consent/"+reqID+"/approve", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	var appr struct {
		RedirectURL string `json:"redirect_url"`
	}
	json.Unmarshal(rec.Body.Bytes(), &appr)
	cb, _ := url.Parse(appr.RedirectURL)
	code := cb.Query().Get("code")
	if code == "" || cb.Query().Get("state") != "xyz" || cb.Host != "claude.ai" {
		t.Fatalf("bad callback url: %s", appr.RedirectURL)
	}

	// 4. PKCE mismatch is refused, and does not burn... actually it does
	// consume the code (one-time), which is the safe behaviour. Test with a
	// fresh code for the failure case below; here exchange for real.
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {verifier}, "client_id": {reg.ClientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "resource": {"https://chui.example/mcp"}}
	rec = do(t, r, "POST", "/oauth/token", form.Encode(), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
		ExpiresIn    int    `json:"expires_in"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tok)
	if !strings.HasPrefix(tok.AccessToken, "chm_") || tok.RefreshToken == "" || tok.Scope != "read write" || tok.ExpiresIn != 3600 {
		t.Fatalf("token response: %+v", tok)
	}

	// 5. the code is single-use
	rec = do(t, r, "POST", "/oauth/token", form.Encode(), true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("code reuse should fail: %d %s", rec.Code, rec.Body.String())
	}

	// 6. the access token works on /mcp and lists write tools (scope write)
	mreq := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	mreq.Header.Set("Content-Type", "application/json")
	mreq.Header.Set("Accept", "application/json, text/event-stream")
	mreq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	mrec := httptest.NewRecorder()
	r.ServeHTTP(mrec, mreq)
	if mrec.Code != http.StatusOK || !strings.Contains(mrec.Body.String(), "save_query") {
		t.Fatalf("mcp with oauth token: %d %s", mrec.Code, mrec.Body.String())
	}
	keys, _ := db.ListMCPKeys()
	if len(keys) != 1 || keys[0].Kind != "oauth" || keys[0].Subject != "alice@example.com" || keys[0].ClientID != reg.ClientID || keys[0].Scopes != "read_write" {
		t.Fatalf("access token row: %+v", keys[0])
	}

	// 7. refresh rotates both tokens; the old access token dies
	rf := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok.RefreshToken}, "client_id": {reg.ClientID}}
	rec = do(t, r, "POST", "/oauth/token", rf.Encode(), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("refresh: %d %s", rec.Code, rec.Body.String())
	}
	var tok2 struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tok2)
	if tok2.AccessToken == tok.AccessToken || tok2.RefreshToken == tok.RefreshToken {
		t.Error("refresh must rotate tokens")
	}
	mreq.Header.Set("Authorization", "Bearer "+tok.AccessToken)
	mrec = httptest.NewRecorder()
	r.ServeHTTP(mrec, mreq)
	if mrec.Code != http.StatusUnauthorized {
		t.Errorf("old access token after refresh: want 401, got %d", mrec.Code)
	}
	if !strings.Contains(mrec.Header().Get("WWW-Authenticate"), "resource_metadata=") {
		t.Errorf("401 should point at resource metadata: %s", mrec.Header().Get("WWW-Authenticate"))
	}
	rec = do(t, r, "POST", "/oauth/token", rf.Encode(), true)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("old refresh token reuse should fail: %d", rec.Code)
	}
}

func TestOAuthAuthorizeRejections(t *testing.T) {
	r, _, _ := oauthRig(t)
	rec := do(t, r, "POST", "/oauth/register", `{"client_name":"x","redirect_uris":["https://app.example/cb"]}`, false)
	var reg struct {
		ClientID string `json:"client_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &reg)

	// unregistered redirect_uri: 400, never a redirect
	q := url.Values{"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {"https://evil.example/cb"}, "code_challenge": {"c"}, "code_challenge_method": {"S256"}}
	if rec := do(t, r, "GET", "/oauth/authorize?"+q.Encode(), "", false); rec.Code != http.StatusBadRequest {
		t.Errorf("unregistered redirect: want 400, got %d", rec.Code)
	}
	// missing PKCE: error redirect to the registered URI
	q = url.Values{"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {"https://app.example/cb"}, "state": {"s"}}
	rec = do(t, r, "GET", "/oauth/authorize?"+q.Encode(), "", false)
	loc := rec.Header().Get("Location")
	if rec.Code != http.StatusFound || !strings.HasPrefix(loc, "https://app.example/cb?") || !strings.Contains(loc, "error=invalid_request") || !strings.Contains(loc, "state=s") {
		t.Errorf("missing pkce: %d %s", rec.Code, loc)
	}
	// unknown scope
	q.Set("code_challenge", "c")
	q.Set("code_challenge_method", "S256")
	q.Set("scope", "admin")
	rec = do(t, r, "GET", "/oauth/authorize?"+q.Encode(), "", false)
	if !strings.Contains(rec.Header().Get("Location"), "error=invalid_scope") {
		t.Errorf("unknown scope: %s", rec.Header().Get("Location"))
	}
	// actions without write
	q.Set("scope", "read actions")
	rec = do(t, r, "GET", "/oauth/authorize?"+q.Encode(), "", false)
	if !strings.Contains(rec.Header().Get("Location"), "error=invalid_scope") {
		t.Errorf("actions without write: %s", rec.Header().Get("Location"))
	}
	// DCR rejects non-loopback http and confidential clients
	if rec := do(t, r, "POST", "/oauth/register", `{"redirect_uris":["http://app.example/cb"]}`, false); rec.Code != http.StatusBadRequest {
		t.Errorf("http redirect: want 400, got %d", rec.Code)
	}
	if rec := do(t, r, "POST", "/oauth/register", `{"redirect_uris":["https://app.example/cb"],"token_endpoint_auth_method":"client_secret_basic"}`, false); rec.Code != http.StatusBadRequest {
		t.Errorf("confidential client: want 400, got %d", rec.Code)
	}
	if rec := do(t, r, "POST", "/oauth/register", `{"redirect_uris":["http://127.0.0.1:53000/callback","cursor://anysphere.cursor-retrieval/oauth/callback"]}`, false); rec.Code != http.StatusCreated {
		t.Errorf("loopback + custom scheme: want 201, got %d %s", rec.Code, rec.Body.String())
	}
}

func TestOAuthPKCEMismatchAndExpiry(t *testing.T) {
	r, db, connID := oauthRig(t)
	// Seed a code directly, then exchange with the wrong verifier.
	code := "seeded-code"
	if err := db.CreateOAuthCode(&database.OAuthCode{
		CodeHash: hashToken(code), ClientID: "c1", ClientName: "c", RedirectURI: "https://app.example/cb", Scope: "read",
		CodeChallenge: "not-the-right-challenge", ConnectionID: connID, CHUser: "alice", CHPasswordEnc: "x", Subject: "alice",
		ExpiresAt: time.Now().UTC().Add(time.Minute).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"grant_type": {"authorization_code"}, "code": {code}, "code_verifier": {"whatever"}, "client_id": {"c1"}}
	rec := do(t, r, "POST", "/oauth/token", form.Encode(), true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "PKCE") {
		t.Errorf("pkce mismatch: %d %s", rec.Code, rec.Body.String())
	}
	// expired code
	expired := "expired-code"
	db.CreateOAuthCode(&database.OAuthCode{
		CodeHash: hashToken(expired), ClientID: "c1", ClientName: "c", RedirectURI: "https://app.example/cb", Scope: "read",
		CodeChallenge: "c", ConnectionID: connID, CHUser: "alice", CHPasswordEnc: "x", Subject: "alice",
		ExpiresAt: time.Now().UTC().Add(-time.Minute).Format(time.RFC3339),
	})
	form.Set("code", expired)
	rec = do(t, r, "POST", "/oauth/token", form.Encode(), true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") {
		t.Errorf("expired code: %d %s", rec.Code, rec.Body.String())
	}
}

func TestValidateRedirectURI(t *testing.T) {
	ok := []string{"https://claude.ai/cb", "http://localhost:1234/cb", "http://127.0.0.1/cb", "vscode://redhat.vscode/callback", "cursor://anysphere/cb"}
	for _, u := range ok {
		if err := validateRedirectURI(u); err != nil {
			t.Errorf("%s should be accepted: %v", u, err)
		}
	}
	bad := []string{"http://app.example/cb", "https://app.example/cb#frag", "not a url", ""}
	for _, u := range bad {
		if err := validateRedirectURI(u); err == nil {
			t.Errorf("%s should be rejected", u)
		}
	}
}

// Two refreshes racing with the same refresh token: exactly one may win.
// The check and the revoke are one conditional UPDATE, so the loser sees a
// used token instead of minting a second live token pair.
func TestOAuthRefreshTokenRaceIssuesOnce(t *testing.T) {
	r, db, connID := oauthRig(t)
	key, err := db.CreateMCPKey(database.CreateMCPKeyParams{
		Name: "c (alice)", KeyHash: "h-race", KeyPrefix: "chm_race", ConnectionID: connID,
		CHUser: "alice", CHPasswordEnc: "x", Scopes: "read", CreatedBy: "oauth:alice",
		Kind: "oauth", Subject: "alice", ClientID: "client-race",
	})
	if err != nil {
		t.Fatal(err)
	}
	raw := "refresh-race-token"
	if err := db.CreateOAuthRefreshToken(&database.OAuthRefreshToken{
		TokenHash: hashToken(raw), KeyID: key.ID, ClientID: "client-race", Scope: "read",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	form := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {raw}, "client_id": {"client-race"}}.Encode()

	const n = 8
	codes := make(chan int, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			<-start
			codes <- do(t, r, "POST", "/oauth/token", form, true).Code
		}()
	}
	close(start)
	ok := 0
	for i := 0; i < n; i++ {
		if <-codes == http.StatusOK {
			ok++
		}
	}
	if ok != 1 {
		t.Fatalf("want exactly one successful refresh, got %d", ok)
	}
	live := 0
	keys, _ := db.ListMCPKeys()
	for _, k := range keys {
		if k.RevokedAt == nil {
			live++
		}
	}
	if live != 1 {
		t.Errorf("want one live access token after the race, got %d", live)
	}
}

func TestClaimRefreshTokenOnlyOnce(t *testing.T) {
	_, db, connID := oauthRig(t)
	key, err := db.CreateMCPKey(database.CreateMCPKeyParams{
		Name: "c", KeyHash: "h-claim", KeyPrefix: "chm_claim", ConnectionID: connID,
		CHUser: "alice", CHPasswordEnc: "x", Scopes: "read", Kind: "oauth", Subject: "alice", ClientID: "client-claim",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.CreateOAuthRefreshToken(&database.OAuthRefreshToken{
		TokenHash: "th-claim", KeyID: key.ID, ClientID: "client-claim", Scope: "read",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
	}); err != nil {
		t.Fatal(err)
	}
	h := &OAuthHandler{DB: db}
	if ok, err := h.claimRefreshToken("th-claim", "other-client"); err != nil || ok {
		t.Fatalf("another client must not claim the token: ok=%v err=%v", ok, err)
	}
	if ok, err := h.claimRefreshToken("th-claim", "client-claim"); err != nil || !ok {
		t.Fatalf("first claim: ok=%v err=%v", ok, err)
	}
	if ok, err := h.claimRefreshToken("th-claim", "client-claim"); err != nil || ok {
		t.Fatalf("second claim must lose: ok=%v err=%v", ok, err)
	}
}

// oauthWriteGrant runs DCR, authorize, consent and the code exchange for
// "read write" and returns the token response and the client id.
func oauthWriteGrant(t *testing.T, r http.Handler) (access, refresh, clientID string) {
	t.Helper()
	return oauthGrant(t, r, "read write")
}

// oauthGrant is oauthWriteGrant for any scope the consent accepts.
func oauthGrant(t *testing.T, r http.Handler, scope string) (access, refresh, clientID string) {
	t.Helper()
	rec := do(t, r, "POST", "/oauth/register", `{"client_name":"Claude","redirect_uris":["https://claude.ai/cb"]}`, false)
	var reg struct {
		ClientID string `json:"client_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &reg)
	verifier := "a-very-long-verifier-string-with-enough-entropy-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type": {"code"}, "client_id": {reg.ClientID}, "redirect_uri": {"https://claude.ai/cb"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"}, "scope": {scope},
	}
	rec = do(t, r, "GET", "/oauth/authorize?"+q.Encode(), "", false)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	rec = do(t, r, "POST", "/api/oauth/consent/"+loc.Query().Get("request")+"/approve", "", false)
	if rec.Code != http.StatusOK {
		t.Fatalf("approve: %d %s", rec.Code, rec.Body.String())
	}
	var appr struct {
		RedirectURL string `json:"redirect_url"`
	}
	json.Unmarshal(rec.Body.Bytes(), &appr)
	cb, _ := url.Parse(appr.RedirectURL)
	form := url.Values{"grant_type": {"authorization_code"}, "code": {cb.Query().Get("code")}, "code_verifier": {verifier}, "client_id": {reg.ClientID}}
	rec = do(t, r, "POST", "/oauth/token", form.Encode(), true)
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tok)
	if rec.Code != http.StatusOK || tok.Scope != scope {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	return tok.AccessToken, tok.RefreshToken, reg.ClientID
}

func mcpToolList(t *testing.T, r http.Handler, access string) string {
	t.Helper()
	req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	req.Header.Set("Authorization", "Bearer "+access)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list: %d %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

// A person demoted after granting write scope loses the write tools on the
// next MCP request, and a refresh drops write scope for good.
func TestOAuthWriteScopeFollowsRole(t *testing.T) {
	r, db, _ := oauthRig(t)
	access, refresh, clientID := oauthWriteGrant(t, r)
	if !strings.Contains(mcpToolList(t, r, access), `"save_query"`) {
		t.Fatal("analyst with write scope should see save_query")
	}

	if err := db.SetUserRole("sso:alice@example.com", "", "viewer"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mcpToolList(t, r, access), `"save_query"`) {
		t.Fatal("demoted person must not see write tools on the live access token")
	}

	rf := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}
	rec := do(t, r, "POST", "/oauth/token", rf.Encode(), true)
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		Scope        string `json:"scope"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tok)
	if rec.Code != http.StatusOK || tok.Scope != "read" {
		t.Fatalf("refresh after demotion: want 200 with scope read, got %d %s", rec.Code, rec.Body.String())
	}
	k, err := db.GetMCPKeyByHash(mcpserver.HashKey(tok.AccessToken))
	if err != nil || k == nil || k.Scopes != "read" {
		t.Fatalf("refreshed key: %+v %v", k, err)
	}

	// Restoring the role does not bring write back on this grant: the
	// person has to consent again.
	if err := db.DeleteUserRole("sso:alice@example.com", ""); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mcpToolList(t, r, tok.AccessToken), `"save_query"`) {
		t.Fatal("downgraded grant must stay read-only")
	}
	rf.Set("refresh_token", tok.RefreshToken)
	rec = do(t, r, "POST", "/oauth/token", rf.Encode(), true)
	if !strings.Contains(rec.Body.String(), `"scope":"read"`) {
		t.Fatalf("second refresh: %d %s", rec.Code, rec.Body.String())
	}
}

// Write scope survives the person's sign-out: the role recorded when the
// token was issued stands in until an admin changes it. With the role
// unknown the token is read-only, and a refresh keeps the scope so the
// next sign-in brings write back.
func TestOAuthWriteScopeWithoutLiveSession(t *testing.T) {
	r, db, connID := oauthRig(t)
	access, refresh, clientID := oauthWriteGrant(t, r)
	if err := db.DeleteSession("oauth-rig-session"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mcpToolList(t, r, access), `"save_query"`) {
		t.Fatal("issue-time analyst role should keep write tools without a live session")
	}
	if err := db.ClearOAuthGrantRoles("sso:alice@example.com", connID); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mcpToolList(t, r, access), `"save_query"`) {
		t.Fatal("cleared grant role with no live session must be read-only")
	}

	rf := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}
	rec := do(t, r, "POST", "/oauth/token", rf.Encode(), true)
	var tok struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tok)
	if rec.Code != http.StatusOK || tok.Scope != "read write" {
		t.Fatalf("refresh with an unknown role should keep the scope: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(mcpToolList(t, r, tok.AccessToken), `"save_query"`) {
		t.Fatal("unknown role must stay read-only after refresh")
	}
	if _, err := db.CreateSession(database.CreateSessionParams{
		ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: "enc", Token: "signed-in-again",
		ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), UserRole: "analyst", AuthSubject: "alice@example.com",
	}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(mcpToolList(t, r, tok.AccessToken), `"save_query"`) {
		t.Fatal("signing in again as analyst should bring the write tools back")
	}
}

// The actions scope adds the lifecycle tools on top of write. The consent
// screen lists it, and a demotion takes it away with write on the live
// token and for good on refresh.
func TestOAuthActionsScopeFollowsRole(t *testing.T) {
	r, db, _ := oauthRig(t)
	plain, _, _ := oauthWriteGrant(t, r)
	if strings.Contains(mcpToolList(t, r, plain), `"run_model"`) {
		t.Fatal("write scope alone must not bring run_model")
	}

	access, refresh, clientID := oauthGrant(t, r, "read write actions")
	k, err := db.GetMCPKeyByHash(mcpserver.HashKey(access))
	if err != nil || k == nil || k.Scopes != "read_write" || !k.AllowActions {
		t.Fatalf("actions grant key: %+v %v", k, err)
	}
	if !strings.Contains(mcpToolList(t, r, access), `"run_model"`) {
		t.Fatal("analyst with actions scope should see run_model")
	}

	if err := db.SetUserRole("sso:alice@example.com", "", "viewer"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mcpToolList(t, r, access), `"run_model"`) {
		t.Fatal("demoted person must lose the action tools on the live token")
	}
	rf := url.Values{"grant_type": {"refresh_token"}, "refresh_token": {refresh}, "client_id": {clientID}}
	rec := do(t, r, "POST", "/oauth/token", rf.Encode(), true)
	var tok struct {
		AccessToken string `json:"access_token"`
		Scope       string `json:"scope"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tok)
	if rec.Code != http.StatusOK || tok.Scope != "read" {
		t.Fatalf("refresh after demotion: %d %s", rec.Code, rec.Body.String())
	}
	if k, _ := db.GetMCPKeyByHash(mcpserver.HashKey(tok.AccessToken)); k == nil || k.AllowActions {
		t.Fatalf("refreshed key must not carry actions: %+v", k)
	}
}
