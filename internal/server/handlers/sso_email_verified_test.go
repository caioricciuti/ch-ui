// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/oidc"
)

// fakeIdP is a minimal OIDC provider: discovery, JWKS and a token endpoint
// that returns an RS256 ID token carrying whatever email_verified the test sets.
type fakeIdP struct {
	srv *httptest.Server
	key *rsa.PrivateKey

	mu     sync.Mutex
	extra  map[string]interface{}
	nonce  string
	client string
	// discoveryDown makes discovery fail, as when the IdP is unreachable
	// during a reload.
	discoveryDown bool
}

func newFakeIdP(t *testing.T) *fakeIdP {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{key: key, client: "ch-ui"}
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		idp.mu.Lock()
		down := idp.discoveryDown
		idp.mu.Unlock()
		if down {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
			return
		}
		base := idp.srv.URL
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"issuer":                                base,
			"authorization_endpoint":                base + "/authorize",
			"token_endpoint":                        base + "/token",
			"jwks_uri":                              base + "/jwks",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"keys": []map[string]string{{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": "k1",
			"n": base64.RawURLEncoding.EncodeToString(key.N.Bytes()),
			"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(key.E)).Bytes()),
		}}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"access_token": "at", "token_type": "Bearer", "expires_in": 300,
			"id_token": idp.idToken(t),
		})
	})
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	return idp
}

func (idp *fakeIdP) set(nonce string, extra map[string]interface{}) {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	idp.nonce, idp.extra = nonce, extra
}

func (idp *fakeIdP) idToken(t *testing.T) string {
	idp.mu.Lock()
	defer idp.mu.Unlock()
	now := time.Now().Unix()
	claims := map[string]interface{}{
		"iss": idp.srv.URL, "aud": idp.client, "sub": "sub-1",
		"iat": now, "exp": now + 300, "nonce": idp.nonce,
		"email": "admin@corp.example",
	}
	for k, v := range idp.extra {
		claims[k] = v
	}
	enc := func(v interface{}) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	signing := enc(map[string]string{"alg": "RS256", "typ": "JWT", "kid": "k1"}) + "." + enc(claims)
	sum := sha256.Sum256([]byte(signing))
	sig, err := rsa.SignPKCS1v15(rand.Reader, idp.key, crypto.SHA256, sum[:])
	if err != nil {
		t.Fatal(err)
	}
	return signing + "." + base64.RawURLEncoding.EncodeToString(sig)
}

func TestOIDCCallbackEmailVerified(t *testing.T) {
	idp := newFakeIdP(t)
	db, err := dbtest.Open(t, "oidc.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "sso", TunnelToken: "sso"})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(connID, "svc_sso", "enc"); err != nil {
		t.Fatal(err)
	}

	missing := map[string]interface{}{}
	cases := []struct {
		name    string
		accept  bool
		extra   map[string]interface{}
		wantOK  bool
		wantMsg string
	}{
		{"verified true", false, map[string]interface{}{"email_verified": true}, true, ""},
		{"verified false refused", false, map[string]interface{}{"email_verified": false}, false, "not verified"},
		{"verified false refused even when accepting missing", true, map[string]interface{}{"email_verified": false}, false, "not verified"},
		{"non-bool refused even when accepting missing", true, map[string]interface{}{"email_verified": 1}, false, "not verified"},
		{"missing refused by default", false, missing, false, "Accept emails without email_verified"},
		{"missing accepted when setting on", true, missing, true, ""},
	}
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mgr := oidc.NewManager()
			err := mgr.Reload(context.Background(), oidc.Settings{
				Enabled: true, IssuerURL: idp.srv.URL, ClientID: idp.client, ClientSecret: "s",
				RedirectURL: "http://ch-ui.test/api/auth/oidc/callback", ConnectionID: connID,
				AcceptUnverifiedEmail: tc.accept,
			})
			if err != nil {
				t.Fatal(err)
			}
			h := &AuthHandler{DB: db, Config: &config.Config{}, OIDC: mgr}

			nonce := "nonce-" + strconv.Itoa(i)
			idp.set(nonce, tc.extra)
			req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=c&state=st", nil)
			req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "st"})
			req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: nonce})
			req.AddCookie(&http.Cookie{Name: oidcVerifierCookie, Value: "verifier"})
			rr := httptest.NewRecorder()
			h.OIDCCallback(rr, req)

			loc := rr.Header().Get("Location")
			if tc.wantOK {
				if loc != "/" {
					t.Fatalf("expected login, redirected to %q", loc)
				}
				return
			}
			if !strings.HasPrefix(loc, "/login?sso_error=") {
				t.Fatalf("expected refusal, redirected to %q", loc)
			}
			msg, err := url.QueryUnescape(strings.TrimPrefix(loc, "/login?sso_error="))
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(msg, tc.wantMsg) {
				t.Fatalf("sso_error %q does not mention %q", msg, tc.wantMsg)
			}
			for _, c := range rr.Result().Cookies() {
				if c.Name == SessionCookie && c.Value != "" {
					t.Fatal("refused login still set a session cookie")
				}
			}
		})
	}

	// Each refusal is in the audit trail; no successful login for a refused case.
	logs, err := db.GetAuditLogsFiltered(100, "", "user.login_failed", "", "")
	if err != nil {
		t.Fatal(err)
	}
	refused := 0
	for _, l := range logs {
		if l.Details != nil && strings.Contains(*l.Details, "SSO login via OIDC refused") {
			refused++
		}
	}
	if refused != 4 {
		t.Fatalf("audited %d SSO refusals, want 4", refused)
	}
}

func TestSSOSettingsAcceptUnverifiedEmailRoundTrip(t *testing.T) {
	db, err := dbtest.Open(t, "sso-settings.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := &config.Config{AppSecretKey: "test-secret-key-for-sso-settings", AppURL: "http://ch-ui.test"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	h := &AdminHandler{DB: db, Config: cfg}

	get := func() ssoSettingsResponse {
		rr := httptest.NewRecorder()
		h.GetSSOSettings(rr, httptest.NewRequest(http.MethodGet, "/api/admin/sso", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET: %d %s", rr.Code, rr.Body)
		}
		var out ssoSettingsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	if get().Config.AcceptUnverifiedEmail {
		t.Fatal("setting must default to false")
	}

	for _, want := range []bool{true, false} {
		body := `{"enabled":false,"issuer_url":"https://idp.example.com","client_id":"c","client_secret":"s","accept_unverified_email":` + map[bool]string{true: "true", false: "false"}[want] + `}`
		rr := httptest.NewRecorder()
		h.UpdateSSOSettings(rr, httptest.NewRequest(http.MethodPut, "/api/admin/sso", strings.NewReader(body)))
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT: %d %s", rr.Code, rr.Body)
		}
		if got := get().Config.AcceptUnverifiedEmail; got != want {
			t.Fatalf("GET after PUT %v: %v", want, got)
		}
		s, err := LoadDBSSOSettings(db, cfg)
		if err != nil {
			t.Fatal(err)
		}
		if s.AcceptUnverifiedEmail != want {
			t.Fatalf("LoadDBSSOSettings after PUT %v: %v", want, s.AcceptUnverifiedEmail)
		}
	}

	// Env-managed SSO shows the env value read-only.
	envCfg := &config.Config{
		OIDCIssuerURL: "https://idp.example.com", OIDCClientID: "c", OIDCClientSecret: "s",
		OIDCRedirectURL: "http://ch-ui.test/api/auth/oidc/callback", OIDCAcceptUnverifiedEmail: true,
	}
	envCfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	h.Config = envCfg
	if out := get(); !out.EnvManaged || !out.Config.AcceptUnverifiedEmail {
		t.Fatalf("env view: %+v", out)
	}
}
