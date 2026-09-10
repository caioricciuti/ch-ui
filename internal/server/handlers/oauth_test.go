package handlers

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/mcpserver"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// oauthRig wires the OAuth handler, a fake logged-in session for the consent
// routes, and the MCP endpoint, so the whole flow runs in-process.
func oauthRig(t *testing.T) (*chi.Mux, *database.DB, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "oauth.db"))
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

	h := &OAuthHandler{DB: db, Config: cfg}
	r := chi.NewRouter()
	r.Get("/.well-known/oauth-protected-resource", h.ProtectedResourceMetadata)
	r.Get("/.well-known/oauth-authorization-server", h.AuthorizationServerMetadata)
	r.Get("/oauth/authorize", h.Authorize)
	r.Post("/oauth/token", h.Token)
	r.Post("/oauth/register", h.Register)
	r.Route("/api/oauth/consent", func(cr chi.Router) {
		cr.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
				ctx := middleware.SetSession(req.Context(), &middleware.SessionInfo{
					ID: "s1", ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: enc, UserRole: "analyst", AuthSubject: "alice@example.com",
				})
				next.ServeHTTP(w, req.WithContext(ctx))
			})
		})
		h.ConsentRoutes(cr)
	})
	r.Handle("/mcp", mcpserver.Handler(mcpserver.Deps{DB: db, Gateway: tunnel.NewGateway(db), Config: cfg}))
	return r, db, connID
}

func do(t *testing.T, r http.Handler, method, target string, body string, form bool) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	req.Host = "chui.example"
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
