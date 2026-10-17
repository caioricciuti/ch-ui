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

	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// Consent by an SSO person with a per-person ClickHouse user stores a
// reference to that user on the code and on every token minted from it,
// never a copy of the password. Once the user is gone the access token is
// refused and the refresh token ends the grant.
func TestOAuthGrantOfPerPersonUserReferencesIt(t *testing.T) {
	var identityID string
	r, db, connID := oauthRigWithSession(t, func(connID, enc string) *middleware.SessionInfo {
		// The session's copy of the password is stale on purpose.
		return &middleware.SessionInfo{
			ID: "s1", ConnectionID: connID, ClickhouseUser: "sso:ana@x.com", EncryptedPassword: enc,
			UserRole: "analyst", AuthSubject: "ana@x.com", SSOIdentityID: identityID,
		}
	})
	ident, err := db.UpsertSSOIdentity("https://idp.example", "sub-ana", "ana@x.com", "Ana", []string{"analysts"})
	if err != nil {
		t.Fatal(err)
	}
	identityID = ident.ID
	live, err := crypto.Encrypt("live-pw", "oauth-test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{
		IdentityID: ident.ID, ConnectionID: connID, CHUser: "sso:ana@x.com", EncryptedPassword: live, Roles: []string{"analyst"},
	}); err != nil {
		t.Fatal(err)
	}

	rec := do(t, r, "POST", "/oauth/register", `{"client_name":"Claude","redirect_uris":["https://claude.ai/api/mcp/auth_callback"],"token_endpoint_auth_method":"none"}`, false)
	if rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var reg struct {
		ClientID string `json:"client_id"`
	}
	json.Unmarshal(rec.Body.Bytes(), &reg)
	verifier := "a-very-long-verifier-string-with-enough-entropy-1234567890"
	sum := sha256.Sum256([]byte(verifier))
	q := url.Values{
		"response_type": {"code"}, "client_id": {reg.ClientID},
		"redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}, "state": {"xyz"},
		"code_challenge": {base64.RawURLEncoding.EncodeToString(sum[:])}, "code_challenge_method": {"S256"},
		"scope": {"read"}, "resource": {"https://chui.example/mcp"},
	}
	rec = do(t, r, "GET", "/oauth/authorize?"+q.Encode(), "", false)
	if rec.Code != http.StatusFound {
		t.Fatalf("authorize: %d %s", rec.Code, rec.Body.String())
	}
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
	form := url.Values{"grant_type": {"authorization_code"}, "code": {cb.Query().Get("code")}, "code_verifier": {verifier}, "client_id": {reg.ClientID}, "redirect_uri": {"https://claude.ai/api/mcp/auth_callback"}}
	rec = do(t, r, "POST", "/oauth/token", form.Encode(), true)
	if rec.Code != http.StatusOK {
		t.Fatalf("token: %d %s", rec.Code, rec.Body.String())
	}
	var tok struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
	}
	json.Unmarshal(rec.Body.Bytes(), &tok)

	keys, err := db.ListMCPKeys()
	if err != nil || len(keys) != 1 {
		t.Fatalf("keys: %d %v", len(keys), err)
	}
	if k := keys[0]; k.SSOIdentityID != ident.ID || k.CHPasswordEnc != "" || k.CHUser != "sso:ana@x.com" || k.Kind != "oauth" || k.Subject != "ana@x.com" {
		t.Fatalf("access token row: %+v", k)
	}

	mcp := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest("POST", "/mcp", strings.NewReader(`{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json, text/event-stream")
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}
	if rec := mcp(tok.AccessToken); rec.Code != http.StatusOK {
		t.Fatalf("mcp with live user: %d %s", rec.Code, rec.Body.String())
	}

	// Refresh keeps the reference.
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
	keys, _ = db.ListMCPKeys()
	var current *database.MCPKey
	for _, k := range keys {
		if k.RevokedAt == nil {
			current = k
		}
	}
	if current == nil || current.SSOIdentityID != ident.ID || current.CHPasswordEnc != "" {
		t.Fatalf("refreshed token row: %+v", current)
	}

	// The user is dropped: the access token is refused and the refresh
	// token ends the grant.
	if err := db.DeleteSSOClickHouseUser(ident.ID, connID); err != nil {
		t.Fatal(err)
	}
	if rec := mcp(tok2.AccessToken); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "no longer has access") {
		t.Fatalf("mcp after drop: %d %s", rec.Code, rec.Body.String())
	}
	rf = url.Values{"grant_type": {"refresh_token"}, "refresh_token": {tok2.RefreshToken}, "client_id": {reg.ClientID}}
	rec = do(t, r, "POST", "/oauth/token", rf.Encode(), true)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "invalid_grant") || !strings.Contains(rec.Body.String(), "no longer has access") {
		t.Fatalf("refresh after drop: %d %s", rec.Code, rec.Body.String())
	}
	if k, _ := db.GetMCPKeyByID(current.ID); k == nil || k.RevokedAt == nil {
		t.Fatalf("the grant must be revoked after the refusal: %+v", k)
	}
	// A refused refresh token stays refused.
	if rec := do(t, r, "POST", "/oauth/token", rf.Encode(), true); rec.Code != http.StatusBadRequest {
		t.Fatalf("second refresh after drop: %d %s", rec.Code, rec.Body.String())
	}
}
