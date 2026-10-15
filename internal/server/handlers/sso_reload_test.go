// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/oidc"
)

// A reload that fails still puts the saved access rules in force, and a
// reload that changed the identity provider stops SSO instead of signing
// people in through the old one.
func TestOIDCCallbackEnforcesSettingsAfterFailedReload(t *testing.T) {
	idp := newFakeIdP(t)
	db, err := dbtest.Open(t, "oidc-reload.db")
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
	base := oidc.Settings{
		Enabled: true, IssuerURL: idp.srv.URL, ClientID: idp.client, ClientSecret: "s",
		RedirectURL: "http://ch-ui.test/api/auth/oidc/callback", ConnectionID: connID,
	}
	mgr := oidc.NewManager()
	if err := mgr.Reload(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	h := &AuthHandler{DB: db, Config: cfg, OIDC: mgr}

	callback := func(nonce string) string {
		t.Helper()
		idp.set(nonce, map[string]interface{}{"email_verified": true})
		req := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/callback?code=c&state=st", nil)
		req.AddCookie(&http.Cookie{Name: oidcStateCookie, Value: "st"})
		req.AddCookie(&http.Cookie{Name: oidcNonceCookie, Value: nonce})
		req.AddCookie(&http.Cookie{Name: oidcVerifierCookie, Value: "verifier"})
		rr := httptest.NewRecorder()
		h.OIDCCallback(rr, req)
		loc := rr.Header().Get("Location")
		if loc == "/" {
			return ""
		}
		msg, _ := url.QueryUnescape(strings.TrimPrefix(loc, "/login?sso_error="))
		return msg
	}
	if msg := callback("n1"); msg != "" {
		t.Fatalf("baseline login refused: %s", msg)
	}

	idp.mu.Lock()
	idp.discoveryDown = true
	idp.mu.Unlock()

	// Same provider, new access rule: enforced although the reload failed.
	restricted := base
	restricted.AllowedDomains = []string{"other.example"}
	if err := mgr.Reload(context.Background(), restricted); err == nil {
		t.Fatal("reload should fail while discovery is down")
	}
	if msg := callback("n2"); !strings.Contains(msg, "domain is not allowed") {
		t.Fatalf("saved allowed_domains not enforced after failed reload: %q", msg)
	}

	// Different client: refuse instead of using the old provider.
	moved := base
	moved.ClientID = "another-client"
	if err := mgr.Reload(context.Background(), moved); err == nil {
		t.Fatal("reload should fail while discovery is down")
	}
	if msg := callback("n3"); !strings.Contains(msg, "SSO is unavailable") {
		t.Fatalf("stale provider must not sign people in: %q", msg)
	}
	rr := httptest.NewRecorder()
	h.OIDCLogin(rr, httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil))
	if loc := rr.Header().Get("Location"); !strings.HasPrefix(loc, "/login?sso_error=") {
		t.Fatalf("login with a stale provider should be refused, redirected to %q", loc)
	}

	// The admin view keeps showing why, on GET.
	admin := &AdminHandler{DB: db, Config: cfg, OIDCManager: mgr}
	rr = httptest.NewRecorder()
	admin.GetSSOSettings(rr, httptest.NewRequest(http.MethodGet, "/api/admin/sso", nil))
	var view ssoSettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Active || view.ReloadError == "" {
		t.Fatalf("GET should report inactive with the reload error: %+v", view)
	}

	// Discovery back: a reload restores SSO and clears the error.
	idp.mu.Lock()
	idp.discoveryDown = false
	idp.mu.Unlock()
	if err := mgr.Reload(context.Background(), base); err != nil {
		t.Fatal(err)
	}
	if msg := callback("n4"); msg != "" {
		t.Fatalf("login after a good reload refused: %s", msg)
	}
	if mgr.LastError() != nil {
		t.Fatalf("LastError after a good reload: %v", mgr.LastError())
	}
}
