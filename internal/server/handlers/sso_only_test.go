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
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ssoOnlyManager returns a manager whose saved settings ask for SSO only.
// With a reachable IdP the provider loads; with up=false discovery fails, as
// when the IdP is down at startup.
func ssoOnlyManager(t *testing.T, up bool) *oidc.Manager {
	t.Helper()
	s := oidc.Settings{
		Enabled: true, ClientID: "c", ClientSecret: "s",
		RedirectURL: "http://ch-ui.test/api/auth/oidc/callback", SSOOnly: true,
	}
	mgr := oidc.NewManager()
	if up {
		idp := newFakeIdP(t)
		s.IssuerURL, s.ClientID = idp.srv.URL, idp.client
		if err := mgr.Reload(context.Background(), s); err != nil {
			t.Fatal(err)
		}
		return mgr
	}
	s.IssuerURL = "http://127.0.0.1:1" // nothing listens here
	if err := mgr.Reload(context.Background(), s); err == nil {
		t.Fatal("reload against an unreachable issuer should fail")
	}
	return mgr
}

func proConfig(cfg *config.Config) *config.Config {
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	return cfg
}

// loginAlice posts alice's right password to the fixture connection.
func loginAlice(h *AuthHandler, connID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"connection_id":"`+connID+`","username":"alice","password":"right"}`))
	req.Header.Set("Content-Type", "application/json")
	req.RemoteAddr = "198.51.100.40:5000"
	rec := httptest.NewRecorder()
	h.Login(rec, req)
	return rec
}

func authConfig(t *testing.T, h *AuthHandler) map[string]interface{} {
	t.Helper()
	rec := httptest.NewRecorder()
	h.AuthConfig(rec, httptest.NewRequest(http.MethodGet, "/config", nil))
	var out map[string]interface{}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	return out
}

func TestLoginSSOOnly(t *testing.T) {
	cases := []struct {
		name       string
		up         bool
		breakGlass bool
		pro        bool
		blocked    bool
	}{
		{"sso up", true, false, true, true},
		{"sso failed to load", false, false, true, false},
		{"break-glass", true, true, true, false},
		{"no license", true, false, false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h, _, connID := switchFixture(t, "right", "")
			h.OIDC = ssoOnlyManager(t, tc.up)
			h.Config.AllowPasswordLogin = tc.breakGlass
			if tc.pro {
				proConfig(h.Config)
			} else {
				h.Config.SetProAccessForTest(func() config.ProAccess { return config.ProNone })
			}

			rec := loginAlice(h, connID)
			if tc.blocked {
				if rec.Code != http.StatusForbidden || !strings.Contains(rec.Body.String(), "Sign in with SSO") {
					t.Fatalf("login: %d %s, want 403", rec.Code, rec.Body.String())
				}
			} else if rec.Code != http.StatusOK {
				t.Fatalf("login: %d %s, want 200", rec.Code, rec.Body.String())
			}

			cfg := authConfig(t, h)
			if cfg["sso_only"] != tc.blocked || cfg["password_login"] != !tc.blocked {
				t.Fatalf("auth config: %v", cfg)
			}
		})
	}
}

func TestSwitchConnectionSSOOnly(t *testing.T) {
	// A password session cannot switch with a password.
	h, _, connID := switchFixture(t, "right", "")
	h.OIDC = ssoOnlyManager(t, true)
	proConfig(h.Config)
	rec := postSwitch(h, `{"connection_id":"`+connID+`","username":"alice","password":"right"}`, nil)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("password switch: %d %s, want 403", rec.Code, rec.Body.String())
	}

	// An SSO session still switches through the service account.
	h, db, connID := switchFixture(t, "irrelevant", "alice@example.com")
	h.OIDC = ssoOnlyManager(t, true)
	proConfig(h.Config)
	enc, err := crypto.Encrypt("svc-pass", switchSecret)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetConnectionSSOAccount(connID, "svc_sso", enc); err != nil {
		t.Fatal(err)
	}
	rec = postSwitch(h, `{"connection_id":"`+connID+`"}`, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("sso switch: %d %s, want 200", rec.Code, rec.Body.String())
	}
}

func TestSSOSettingsSSOOnlyToggle(t *testing.T) {
	db, err := dbtest.Open(t, "sso-only.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := proConfig(&config.Config{AppSecretKey: "test-secret-key-for-sso-only", AppURL: "http://ch-ui.test"})
	h := &AdminHandler{DB: db, Config: cfg}

	put := func(session *middleware.SessionInfo, ssoOnly bool) *httptest.ResponseRecorder {
		body := `{"enabled":false,"issuer_url":"https://idp.example.com","client_id":"c","client_secret":"s","sso_only":` + map[bool]string{true: "true", false: "false"}[ssoOnly] + `}`
		req := httptest.NewRequest(http.MethodPut, "/api/admin/sso", strings.NewReader(body))
		if session != nil {
			req = req.WithContext(middleware.SetSession(req.Context(), session))
		}
		rr := httptest.NewRecorder()
		h.UpdateSSOSettings(rr, req)
		return rr
	}
	stored := func() bool {
		c, err := db.GetSSOConfig()
		if err != nil {
			t.Fatal(err)
		}
		return c.SSOOnly
	}
	password := &middleware.SessionInfo{ClickhouseUser: "default", UserRole: "admin"}
	sso := &middleware.SessionInfo{ClickhouseUser: "svc_sso", UserRole: "admin", AuthSubject: "admin@example.com"}

	if rr := put(password, true); rr.Code != http.StatusBadRequest || !strings.Contains(rr.Body.String(), "Sign in with SSO first") {
		t.Fatalf("password session turning SSO only on: %d %s, want 400", rr.Code, rr.Body)
	}
	if stored() {
		t.Fatal("refused toggle was saved")
	}
	rr := put(sso, true)
	if rr.Code != http.StatusOK {
		t.Fatalf("SSO session turning SSO only on: %d %s", rr.Code, rr.Body)
	}
	var view ssoSettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Config.SSOOnly || !view.ViaSSO || view.BreakGlass {
		t.Fatalf("view after turning on: %+v", view)
	}
	if !stored() {
		t.Fatal("SSO only not saved")
	}
	s, err := LoadDBSSOSettings(db, cfg)
	if err != nil || !s.SSOOnly {
		t.Fatalf("LoadDBSSOSettings: %+v %v", s, err)
	}

	// Already on: a password session (break-glass) may still save, and turn it off.
	if rr := put(password, true); rr.Code != http.StatusOK {
		t.Fatalf("saving with SSO only already on: %d %s", rr.Code, rr.Body)
	}
	if rr := put(password, false); rr.Code != http.StatusOK || stored() {
		t.Fatalf("turning SSO only off: %d %s", rr.Code, rr.Body)
	}

	// Env-managed SSO shows the env value read-only, and the break-glass flag.
	envCfg := proConfig(&config.Config{
		OIDCIssuerURL: "https://idp.example.com", OIDCClientID: "c", OIDCClientSecret: "s",
		OIDCRedirectURL: "http://ch-ui.test/api/auth/oidc/callback", OIDCSSOOnly: true, AllowPasswordLogin: true,
	})
	h.Config = envCfg
	rr = httptest.NewRecorder()
	h.GetSSOSettings(rr, httptest.NewRequest(http.MethodGet, "/api/admin/sso", nil))
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.EnvManaged || !view.Config.SSOOnly || !view.BreakGlass || view.SSOOnlyEnforced {
		t.Fatalf("env view: %+v", view)
	}
}

// seedSessions creates two password sessions and one SSO session on
// connID and returns a func counting the password ones still there.
func seedSessions(t *testing.T, db *database.DB, connID string) (passwordLeft func() int, ssoLeft func() bool) {
	t.Helper()
	exp := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, p := range []database.CreateSessionParams{
		{ConnectionID: connID, ClickhouseUser: "default", EncryptedPassword: "x", Token: "pw-1", ExpiresAt: exp, UserRole: "admin"},
		{ConnectionID: connID, ClickhouseUser: "bob", EncryptedPassword: "x", Token: "pw-2", ExpiresAt: exp, UserRole: "viewer"},
		{ConnectionID: connID, ClickhouseUser: "svc_sso", EncryptedPassword: "x", Token: "sso-1", ExpiresAt: exp, UserRole: "admin", AuthSubject: "admin@example.com"},
	} {
		if _, err := db.CreateSession(p); err != nil {
			t.Fatal(err)
		}
	}
	passwordLeft = func() int {
		n := 0
		for _, tok := range []string{"pw-1", "pw-2"} {
			if s, _ := db.GetSession(tok); s != nil {
				n++
			}
		}
		return n
	}
	ssoLeft = func() bool {
		s, _ := db.GetSession("sso-1")
		return s != nil
	}
	return passwordLeft, ssoLeft
}

func revokeAudit(t *testing.T, db *database.DB) *database.AuditLog {
	t.Helper()
	return findAudit(auditActions(t, db), "sso.password_sessions_revoked", "signed out 2 password session(s)")
}

// Turning SSO only on from the admin page signs out every password session
// at once and keeps the SSO ones, including the enabling admin's.
func TestSSOOnlyToggleRevokesPasswordSessions(t *testing.T) {
	idp := newFakeIdP(t)
	db, err := dbtest.Open(t, "sso-only-revoke.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "ch", TunnelToken: "revoke"})
	if err != nil {
		t.Fatal(err)
	}
	passwordLeft, ssoLeft := seedSessions(t, db, connID)
	cfg := proConfig(&config.Config{AppSecretKey: "test-secret-key-for-sso-only", AppURL: "http://ch-ui.test"})
	h := &AdminHandler{DB: db, Config: cfg, OIDCManager: oidc.NewManager()}

	put := func(ssoOnly bool) {
		t.Helper()
		body := `{"enabled":true,"issuer_url":"` + idp.srv.URL + `","client_id":"` + idp.client + `","client_secret":"s","sso_only":` + map[bool]string{true: "true", false: "false"}[ssoOnly] + `}`
		req := httptest.NewRequest(http.MethodPut, "/api/admin/sso", strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ClickhouseUser: "svc_sso", UserRole: "admin", AuthSubject: "admin@example.com"}))
		rr := httptest.NewRecorder()
		h.UpdateSSOSettings(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("PUT sso_only=%v: %d %s", ssoOnly, rr.Code, rr.Body)
		}
	}

	put(false)
	if passwordLeft() != 2 {
		t.Fatal("password sessions signed out without SSO only")
	}
	put(true)
	if n := passwordLeft(); n != 0 {
		t.Fatalf("%d password sessions left after SSO only went on", n)
	}
	if !ssoLeft() {
		t.Fatal("SSO session signed out")
	}
	if a := revokeAudit(t, db); a == nil || a.Username == nil || *a.Username != "admin@example.com" {
		t.Fatalf("revocation audit row missing or wrong: %+v", a)
	}
}

// At startup, OIDC_SSO_ONLY=true forces SSO only over an admin-configured SSO
// that leaves it off, and password sessions are signed out. Break-glass and a
// provider that failed to load leave them alone.
func TestRevokePasswordSessionsAtStartupWithEnvForce(t *testing.T) {
	idp := newFakeIdP(t)
	setup := func(t *testing.T, issuer string, cfg *config.Config) (*database.DB, *oidc.Manager, func() int) {
		t.Helper()
		db, err := dbtest.Open(t, "sso-only-startup.db")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "ch", TunnelToken: "startup"})
		if err != nil {
			t.Fatal(err)
		}
		enc, err := crypto.Encrypt("s", cfg.AppSecretKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetSSOConfig(database.SSOConfig{Enabled: true, IssuerURL: issuer, ClientID: idp.client, ClientSecretEnc: enc}); err != nil {
			t.Fatal(err)
		}
		passwordLeft, _ := seedSessions(t, db, connID)
		mgr := oidc.NewManager()
		settings, ok := ResolveOIDCSettings(cfg, db)
		if !ok || !settings.SSOOnly || settings.Source != oidc.SourceDB {
			t.Fatalf("env SSO only must force the DB config: %+v", settings)
		}
		_ = mgr.Reload(context.Background(), settings)
		return db, mgr, passwordLeft
	}
	newCfg := func(breakGlass bool) *config.Config {
		return proConfig(&config.Config{AppSecretKey: "test-secret-key-for-sso-only", AppURL: "http://ch-ui.test", OIDCSSOOnly: true, AllowPasswordLogin: breakGlass})
	}

	t.Run("forced", func(t *testing.T) {
		cfg := newCfg(false)
		db, mgr, passwordLeft := setup(t, idp.srv.URL, cfg)
		RevokePasswordSessions(db, cfg, mgr, "system", "")
		if n := passwordLeft(); n != 0 {
			t.Fatalf("%d password sessions left", n)
		}
		if revokeAudit(t, db) == nil {
			t.Fatal("revocation audit row missing")
		}
		// The admin page shows SSO only on and locked.
		h := &AdminHandler{DB: db, Config: cfg, OIDCManager: mgr}
		rr := httptest.NewRecorder()
		h.GetSSOSettings(rr, httptest.NewRequest(http.MethodGet, "/api/admin/sso", nil))
		var view ssoSettingsResponse
		if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
			t.Fatal(err)
		}
		if view.EnvManaged || !view.Config.SSOOnly || !view.SSOOnlyForced || !view.SSOOnlyEnforced {
			t.Fatalf("forced view: %+v", view)
		}
	})
	t.Run("break-glass", func(t *testing.T) {
		cfg := newCfg(true)
		db, mgr, passwordLeft := setup(t, idp.srv.URL, cfg)
		RevokePasswordSessions(db, cfg, mgr, "system", "")
		if passwordLeft() != 2 {
			t.Fatal("break-glass must keep password sessions")
		}
	})
	t.Run("provider down", func(t *testing.T) {
		cfg := newCfg(false)
		db, mgr, passwordLeft := setup(t, "http://127.0.0.1:1", cfg)
		RevokePasswordSessions(db, cfg, mgr, "system", "")
		if passwordLeft() != 2 {
			t.Fatal("SSO that failed to load must keep password sessions")
		}
	})
}

// With OIDC_SSO_ONLY forcing it, the admin page cannot change the stored
// value, and saving does not need an SSO session.
func TestSSOSettingsForcedSSOOnlyKeepsStoredValue(t *testing.T) {
	db, err := dbtest.Open(t, "sso-only-forced.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	cfg := proConfig(&config.Config{AppSecretKey: "test-secret-key-for-sso-only", AppURL: "http://ch-ui.test", OIDCSSOOnly: true})
	h := &AdminHandler{DB: db, Config: cfg}
	req := httptest.NewRequest(http.MethodPut, "/api/admin/sso", strings.NewReader(`{"enabled":false,"issuer_url":"https://idp.example.com","client_id":"c","client_secret":"s","sso_only":true}`))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ClickhouseUser: "default", UserRole: "admin"}))
	rr := httptest.NewRecorder()
	h.UpdateSSOSettings(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("PUT: %d %s", rr.Code, rr.Body)
	}
	stored, err := db.GetSSOConfig()
	if err != nil {
		t.Fatal(err)
	}
	if stored.SSOOnly {
		t.Fatal("forced SSO only leaked into the stored config")
	}
	var view ssoSettingsResponse
	if err := json.Unmarshal(rr.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Config.SSOOnly || !view.SSOOnlyForced {
		t.Fatalf("view: %+v", view)
	}
}
