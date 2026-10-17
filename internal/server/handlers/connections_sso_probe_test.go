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
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const ssoProbeToken = "sso-probe-token"

// ssoProbeFixture mounts the setup-sql and verify routes like server.go
// does, with one connection "prod". With a responder the connection has a
// fake agent online; without one the tunnel is offline.
type ssoProbeFixture struct {
	db     *database.DB
	cfg    *config.Config
	router chi.Router
	connID string
	agent  *testutil.Agent
}

func newSSOProbeFixture(t *testing.T, respond func(tunnel.GatewayMessage) *tunnel.AgentMessage) *ssoProbeFixture {
	t.Helper()
	db, err := dbtest.Open(t, "conn-sso-probe.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connID, err := db.CreateConnection(database.CreateConnectionParams{Name: "prod", TunnelToken: ssoProbeToken, Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{AppSecretKey: "sso-probe-test-secret"}
	cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })

	f := &ssoProbeFixture{db: db, cfg: cfg, connID: connID}
	var gw *tunnel.Gateway
	if respond != nil {
		f.agent = testutil.NewAgent(t, db, ssoProbeToken, respond)
		gw = f.agent.Gateway
	} else {
		gw = tunnel.NewGateway(db)
		t.Cleanup(gw.Stop)
	}
	h := &ConnectionsHandler{DB: db, Config: cfg, Gateway: gw, Provisioner: &ssoidentity.Provisioner{DB: db, Gateway: gw, Secret: cfg.AppSecretKey}}
	r := chi.NewRouter()
	r.Group(func(ar chi.Router) {
		ar.Use(middleware.RequireAdmin(db))
		ar.Put("/{id}/sso-account", h.SetSSOAccount)
		ar.Post("/{id}/sso-identity/setup-sql", h.SSOIdentitySetupSQL)
		ar.Post("/{id}/sso-identity/verify", h.VerifySSOIdentity)
	})
	f.router = r
	return f
}

func (f *ssoProbeFixture) call(t *testing.T, role, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "198.51.100.41:5000"
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: role, AuthSubject: role + "@example.com"}))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

// probeAgent answers like a ClickHouse with a correctly scoped provisioner:
// the creates outside the prefix are denied, everything else succeeds.
func probeAgent(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	data := "[]"
	switch {
	case strings.Contains(msg.SQL, "currentUser()"):
		data = `[{"u":"` + msg.User + `"}]`
	case strings.Contains(msg.SQL, "system.users"):
		data = `[{"n":0}]`
	case strings.Contains(msg.SQL, "system.processes"):
		data = `[{"address":"10.9.8.7"}]`
	case strings.HasPrefix(msg.SQL, "CREATE USER `__chui_probe_outside_"), strings.HasPrefix(msg.SQL, "CREATE ROLE "):
		return &tunnel.AgentMessage{Type: "query_error", Error: "Code: 497. DB::Exception: Not enough privileges. (ACCESS_DENIED)"}
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data), Meta: json.RawMessage("[]")}
}

const probeConfigBody = `{"mode":"per_person","user_prefix":"sso_","host":"auto","settings_profile":"analysts",
	"role_mappings":[{"group":"data","roles":["analyst_ro"]},{"group":"platform","roles":["analyst_ro","ops"]}]}`

func TestSSOIdentitySetupSQL(t *testing.T) {
	f := newSSOProbeFixture(t, nil)
	path := "/" + f.connID + "/sso-identity/setup-sql"

	if rec := f.call(t, "viewer", http.MethodPost, path, `{"config":`+probeConfigBody+`}`); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer: %d, want 403", rec.Code)
	}
	if rec := f.call(t, "admin", http.MethodPost, "/missing/sso-identity/setup-sql", `{}`); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown connection: %d %s, want 404", rec.Code, rec.Body.String())
	}

	render := func(body string) string {
		t.Helper()
		rec := f.call(t, "admin", http.MethodPost, path, body)
		if rec.Code != http.StatusOK {
			t.Fatalf("setup-sql: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			SQL string `json:"sql"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || out.SQL == "" {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
		return out.SQL
	}

	// Posted config, no account saved: the default provisioner name.
	sql := render(`{"config":` + probeConfigBody + `}`)
	for _, want := range []string{
		"CREATE ROLE IF NOT EXISTS `analyst_ro`;",
		"CREATE ROLE IF NOT EXISTS `ops`;",
		"CREATE SETTINGS PROFILE IF NOT EXISTS `analysts`",
		"CREATE USER IF NOT EXISTS `chui_provisioner` IDENTIFIED WITH sha256_password BY '<choose a password>';",
		"GRANT CREATE USER, ALTER USER, DROP USER ON `sso_`* TO `chui_provisioner`;",
		"GRANT `analyst_ro`, `ops` TO `chui_provisioner` WITH ADMIN OPTION;",
	} {
		if !strings.Contains(sql, want) {
			t.Errorf("setup SQL lacks %q:\n%s", want, sql)
		}
	}

	// An explicit provisioner_user wins.
	if sql := render(`{"config":` + probeConfigBody + `,"provisioner_user":"my_prov"}`); !strings.Contains(sql, "TO `my_prov` WITH ADMIN OPTION") {
		t.Errorf("explicit provisioner_user ignored:\n%s", sql)
	}

	// With a saved SSO account and no posted config, both come from the DB.
	if rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-account", `{"username":"saved_prov","password":"pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("set sso account: %d %s", rec.Code, rec.Body.String())
	}
	saved := database.DefaultSSOIdentityConfig()
	saved.Mode = database.SSOModePerPerson
	saved.UserPrefix = "team-"
	saved.RoleMappings = []database.SSORoleMapping{{Group: "g", Roles: []string{"saved_role"}}}
	if err := f.db.SetSSOIdentityConfig(f.connID, saved); err != nil {
		t.Fatal(err)
	}
	sql = render(`{}`)
	if !strings.Contains(sql, "CREATE ROLE IF NOT EXISTS `saved_role`;") || !strings.Contains(sql, "ON `team-`* TO `saved_prov`;") {
		t.Errorf("saved config and account not used:\n%s", sql)
	}
	if a := findAudit(auditActions(t, f.db), "connection.sso_identity.verified", ""); a != nil {
		t.Fatalf("setup-sql wrote a verify audit row: %+v", a)
	}
}

func TestVerifySSOIdentityRefusals(t *testing.T) {
	f := newSSOProbeFixture(t, nil)
	path := "/" + f.connID + "/sso-identity/verify"
	withAccount := `{"config":` + probeConfigBody + `,"provisioner":{"username":"chui_prov","password":"pw"}}`

	if rec := f.call(t, "analyst", http.MethodPost, path, withAccount); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst: %d, want 403", rec.Code)
	}

	f.cfg.SetProAccessForTest(func() config.ProAccess { return config.ProNone })
	if rec := f.call(t, "admin", http.MethodPost, path, withAccount); rec.Code != http.StatusPaymentRequired || !strings.Contains(rec.Body.String(), "Pro license required") {
		t.Fatalf("without Pro: %d %s, want 402", rec.Code, rec.Body.String())
	}
	f.cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })

	if rec := f.call(t, "admin", http.MethodPost, "/missing/sso-identity/verify", withAccount); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown connection: %d, want 404", rec.Code)
	}
	for name, tc := range map[string]struct{ body, wantErr string }{
		"bad json":            {`{"config":`, "Invalid request body"},
		"invalid config":      {`{"config":{"mode":"per_person"},"provisioner":{"username":"p","password":"x"}}`, "at least one group to role mapping"},
		"shared mode":         {`{"config":{"mode":"shared"},"provisioner":{"username":"p","password":"x"}}`, "per-person mode"},
		"no account anywhere": {`{"config":` + probeConfigBody + `}`, "No provisioning account"},
		"saved config shared": {`{"provisioner":{"username":"p","password":"x"}}`, "per-person mode"},
	} {
		t.Run(name, func(t *testing.T) {
			rec := f.call(t, "admin", http.MethodPost, path, tc.body)
			if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), tc.wantErr) {
				t.Fatalf("got %d %s, want 400 containing %q", rec.Code, rec.Body.String(), tc.wantErr)
			}
		})
	}

	// Everything is in order but no agent is connected.
	if rec := f.call(t, "admin", http.MethodPost, path, withAccount); rec.Code != http.StatusServiceUnavailable || !strings.Contains(rec.Body.String(), "offline") {
		t.Fatalf("offline: %d %s, want 503", rec.Code, rec.Body.String())
	}
	if a := findAudit(auditActions(t, f.db), "connection.sso_identity.verified", ""); a != nil {
		t.Fatalf("a refused verify wrote an audit row: %+v", a)
	}
}

func TestVerifySSOIdentityRunsProbe(t *testing.T) {
	f := newSSOProbeFixture(t, probeAgent)
	path := "/" + f.connID + "/sso-identity/verify"

	decode := func(rec *httptest.ResponseRecorder) (bool, []ssoidentity.Check) {
		t.Helper()
		if rec.Code != http.StatusOK {
			t.Fatalf("verify: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			OK     bool                `json:"ok"`
			Checks []ssoidentity.Check `json:"checks"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
		return out.OK, out.Checks
	}

	// Account from the request body, before anything is saved.
	ok, checks := decode(f.call(t, "admin", http.MethodPost, path, `{"config":`+probeConfigBody+`,"provisioner":{"username":"posted_prov","password":"posted-pw"}}`))
	if !ok || len(checks) != 9 {
		t.Fatalf("ok = %v, checks = %+v", ok, checks)
	}
	if checks[0].Name != "Provisioner signs in" || checks[0].Detail != "signed in as posted_prov" || checks[len(checks)-1].Name != "Drops the probe user" {
		t.Fatalf("checks = %+v", checks)
	}
	var sawGrantOps bool
	for _, m := range f.agent.Messages() {
		if m.Type != "query" {
			continue
		}
		if m.User != "posted_prov" || m.Password != "posted-pw" {
			t.Fatalf("%q ran as %s, want the posted account", m.SQL, m.User)
		}
		if strings.HasPrefix(m.SQL, "GRANT `ops` TO `sso___chui_probe_") {
			sawGrantOps = true
		}
	}
	if !sawGrantOps {
		t.Fatal("the posted config's roles were not probed")
	}
	a := findAudit(auditActions(t, f.db), "connection.sso_identity.verified", "SSO provisioner posted_prov verified: ok=true, 0 of 9 check(s) failed")
	if a == nil {
		t.Fatal("verify audit row missing")
	}
	if a.Username == nil || *a.Username != "admin@example.com" || a.ConnectionID == nil || *a.ConnectionID != f.connID {
		t.Fatalf("audit attribution wrong: %+v", a)
	}

	// Saved account and saved config, decrypted server-side.
	if rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-account", `{"username":"saved_prov","password":"saved-pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("set sso account: %d %s", rec.Code, rec.Body.String())
	}
	var cfg database.SSOIdentityConfig
	if err := json.Unmarshal([]byte(probeConfigBody), &cfg); err != nil {
		t.Fatal(err)
	}
	if err := f.db.SetSSOIdentityConfig(f.connID, cfg); err != nil {
		t.Fatal(err)
	}
	before := len(f.agent.Messages())
	ok, checks = decode(f.call(t, "admin", http.MethodPost, path, `{}`))
	if !ok || len(checks) != 9 || checks[0].Detail != "signed in as saved_prov" {
		t.Fatalf("ok = %v, checks = %+v", ok, checks)
	}
	for _, m := range f.agent.Messages()[before:] {
		if m.Type == "query" && (m.User != "saved_prov" || m.Password != "saved-pw") {
			t.Fatalf("%q ran as %s, want the saved account", m.SQL, m.User)
		}
	}
}
