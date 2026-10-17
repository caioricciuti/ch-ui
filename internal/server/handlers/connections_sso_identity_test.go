// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// ssoIdentityFixture mounts the admin-only SSO identity routes the way
// server.go does, with one connection "prod".
type ssoIdentityFixture struct {
	db     *database.DB
	cfg    *config.Config
	router chi.Router
	connID string
}

func newSSOIdentityFixture(t *testing.T) *ssoIdentityFixture {
	t.Helper()
	db, err := dbtest.Open(t, "conn-sso-identity.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: "sso-identity-token", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AppSecretKey: "sso-identity-test-secret"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProNone })

	h := &ConnectionsHandler{DB: db, Config: cfg}
	r := chi.NewRouter()
	r.Group(func(ar chi.Router) {
		ar.Use(middleware.RequireAdmin(db))
		ar.Put("/{id}/sso-account", h.SetSSOAccount)
		ar.Get("/{id}/sso-identity", h.GetSSOIdentity)
		ar.Put("/{id}/sso-identity", h.SetSSOIdentity)
	})
	return &ssoIdentityFixture{db: db, cfg: cfg, router: r, connID: connID}
}

func (f *ssoIdentityFixture) call(t *testing.T, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "198.51.100.40:5000"
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: role, AuthSubject: role + "@example.com"}))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

type ssoIdentityResponse struct {
	Config          database.SSOIdentityConfig `json:"config"`
	ProvisionerUser string                     `json:"provisioner_user"`
	ProvisionerSet  bool                       `json:"provisioner_set"`
}

func decodeSSOIdentity(t *testing.T, rec *httptest.ResponseRecorder) ssoIdentityResponse {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("got %d %s, want 200", rec.Code, rec.Body.String())
	}
	var out ssoIdentityResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	return out
}

const perPersonBody = `{"mode":"per_person","user_prefix":"sso_","host":"any","settings_profile":"analysts",
	"role_mappings":[{"group":"data","roles":["analyst_ro"]},{"group":"platform","roles":["analyst_ro","ops"]}],
	"fallback_roles":[],"lock_after_days":14,"drop_after_days":60}`

func TestSSOIdentityRequiresAdmin(t *testing.T) {
	f := newSSOIdentityFixture(t)
	for _, role := range []string{"viewer", "analyst"} {
		if rec := f.call(t, role, http.MethodGet, "/"+f.connID+"/sso-identity", ""); rec.Code != http.StatusForbidden {
			t.Fatalf("%s GET: %d, want 403", role, rec.Code)
		}
		if rec := f.call(t, role, http.MethodPut, "/"+f.connID+"/sso-identity", `{"mode":"shared"}`); rec.Code != http.StatusForbidden {
			t.Fatalf("%s PUT: %d, want 403", role, rec.Code)
		}
	}
}

func TestSSOIdentityGetDefaultsAndProvisioner(t *testing.T) {
	f := newSSOIdentityFixture(t)
	out := decodeSSOIdentity(t, f.call(t, "admin", http.MethodGet, "/"+f.connID+"/sso-identity", ""))
	want := database.DefaultSSOIdentityConfig()
	if out.Config.Mode != want.Mode || out.Config.UserPrefix != want.UserPrefix || out.Config.Host != want.Host ||
		out.Config.LockAfterDays != want.LockAfterDays || out.Config.DropAfterDays != want.DropAfterDays {
		t.Fatalf("default config: %+v, want %+v", out.Config, want)
	}
	if out.ProvisionerSet || out.ProvisionerUser != "" {
		t.Fatalf("no SSO account yet, got %+v", out)
	}

	if rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-account", `{"username":"chui_sso","password":"pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("set sso account: %d %s", rec.Code, rec.Body.String())
	}
	out = decodeSSOIdentity(t, f.call(t, "admin", http.MethodGet, "/"+f.connID+"/sso-identity", ""))
	if !out.ProvisionerSet || out.ProvisionerUser != "chui_sso" {
		t.Fatalf("SSO account not reported: %+v", out)
	}
	// Shared mode: the sso-account audit row calls it a service account.
	if a := findAudit(auditActions(t, f.db), "connection.sso_account.set", "service account set to chui_sso"); a == nil {
		t.Fatal("sso account audit row missing or not labelled service account")
	}

	if rec := f.call(t, "admin", http.MethodGet, "/missing/sso-identity", ""); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown connection GET: %d, want 404", rec.Code)
	}
}

func TestSSOIdentityPerPersonNeedsPro(t *testing.T) {
	f := newSSOIdentityFixture(t)
	rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-identity", perPersonBody)
	if rec.Code != http.StatusPaymentRequired || !strings.Contains(rec.Body.String(), "Pro license required") {
		t.Fatalf("per_person without Pro: %d %s, want 402", rec.Code, rec.Body.String())
	}
	out := decodeSSOIdentity(t, f.call(t, "admin", http.MethodGet, "/"+f.connID+"/sso-identity", ""))
	if out.Config.Mode != database.SSOModeShared {
		t.Fatalf("refused save changed the config: %+v", out.Config)
	}
	if a := findAudit(auditActions(t, f.db), "connection.sso_identity.updated", ""); a != nil {
		t.Fatalf("refused save wrote an audit row: %+v", a)
	}

	// Shared mode saves on the free edition.
	out = decodeSSOIdentity(t, f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-identity", `{"mode":"shared","user_prefix":"team-","lock_after_days":7,"drop_after_days":30}`))
	if out.Config.Mode != database.SSOModeShared || out.Config.UserPrefix != "team-" || out.Config.LockAfterDays != 7 || out.Config.DropAfterDays != 30 {
		t.Fatalf("shared save: %+v", out.Config)
	}
	if out.Config.Host != database.SSOHostAuto {
		t.Fatalf("empty host not defaulted: %q", out.Config.Host)
	}
}

func TestSSOIdentityValidation(t *testing.T) {
	f := newSSOIdentityFixture(t)
	f.cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	cases := []struct {
		name, body, wantErr string
	}{
		{"bad json", `{"mode":`, "Invalid request body"},
		{"bad mode", `{"mode":"magic"}`, "mode must be"},
		{"bad prefix", `{"mode":"per_person","user_prefix":"has space","role_mappings":[{"group":"g","roles":["r"]}]}`, "user_prefix must be"},
		{"mapping without roles", `{"mode":"per_person","role_mappings":[{"group":"data","roles":[]}]}`, `role mapping for group \"data\" has no roles`},
		{"mapping without group", `{"mode":"per_person","role_mappings":[{"group":"","roles":["r"]}]}`, "has no group"},
		{"per_person without any roles", `{"mode":"per_person"}`, "at least one group to role mapping or a fallback role"},
		{"bad host", `{"mode":"shared","host":"db.internal"}`, "is not an IP"},
		{"drop before lock", `{"mode":"shared","lock_after_days":30,"drop_after_days":10}`, "drop_after_days must not be smaller"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-identity", tc.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Fatalf("got %d %s, want 400 containing %q", rec.Code, rec.Body.String(), tc.wantErr)
			}
		})
	}
	if rec := f.call(t, "admin", http.MethodPut, "/missing/sso-identity", perPersonBody); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown connection PUT: %d %s, want 404", rec.Code, rec.Body.String())
	}
}

func TestSSOIdentityRoundTripAndAudit(t *testing.T) {
	f := newSSOIdentityFixture(t)
	f.cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })

	saved := decodeSSOIdentity(t, f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-identity", perPersonBody))
	read := decodeSSOIdentity(t, f.call(t, "admin", http.MethodGet, "/"+f.connID+"/sso-identity", ""))
	for _, got := range []database.SSOIdentityConfig{saved.Config, read.Config} {
		if got.Mode != database.SSOModePerPerson || got.UserPrefix != "sso_" || got.Host != database.SSOHostAny || got.SettingsProfile != "analysts" {
			t.Fatalf("config: %+v", got)
		}
		if got.LockAfterDays != 14 || got.DropAfterDays != 60 {
			t.Fatalf("lifecycle: %+v", got)
		}
		if len(got.RoleMappings) != 2 || got.RoleMappings[1].Group != "platform" || len(got.RoleMappings[1].Roles) != 2 {
			t.Fatalf("mappings: %+v", got.RoleMappings)
		}
	}
	stored, err := f.db.GetSSOIdentityConfig(f.connID)
	if err != nil || !stored.PerPerson() {
		t.Fatalf("stored config: %+v %v", stored, err)
	}

	a := findAudit(auditActions(t, f.db), "connection.sso_identity.updated", "mode set to per_person (user prefix sso_, 2 group mapping(s))")
	if a == nil {
		t.Fatal("audit row for the saved config missing")
	}
	if a.Username == nil || *a.Username != "admin@example.com" || a.ConnectionID == nil || *a.ConnectionID != f.connID {
		t.Fatalf("audit attribution wrong: %+v", a)
	}

	// Per-person mode: the sso-account audit row calls it the provisioner.
	if rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-account", `{"username":"chui_prov","password":"pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("set sso account: %d %s", rec.Code, rec.Body.String())
	}
	if a := findAudit(auditActions(t, f.db), "connection.sso_account.set", "provisioner set to chui_prov"); a == nil {
		t.Fatal("sso account audit row not labelled provisioner in per-person mode")
	}
	read = decodeSSOIdentity(t, f.call(t, "admin", http.MethodGet, "/"+f.connID+"/sso-identity", ""))
	if !read.ProvisionerSet || read.ProvisionerUser != "chui_prov" {
		t.Fatalf("provisioner not reported: %+v", read)
	}
}
