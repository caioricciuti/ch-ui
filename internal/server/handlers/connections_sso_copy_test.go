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

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// A new connection takes another connection's SSO setup in one step: the
// settings and the account, with the encrypted password copied server-side.
func TestCopySSOIdentityFromAnotherConnection(t *testing.T) {
	f := newSSOIdentityFixture(t)
	f.cfg.SetProAccessForTest(func() config.ProAccess { return config.ProActive })
	h := &ConnectionsHandler{DB: f.db, Config: f.cfg}
	f.router.With(middleware.RequireAdmin(f.db)).Post("/{id}/sso-identity/copy-from", h.CopySSOIdentity)

	if rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-account", `{"username":"chui_prov","password":"prov-pw"}`); rec.Code != http.StatusOK {
		t.Fatalf("set account: %d %s", rec.Code, rec.Body.String())
	}
	cfg := `{"mode":"per_person","user_prefix":"sso:","host":"any","role_mappings":[{"group":"analysts","roles":["analyst"]}],"fallback_roles":["viewer"],"lock_after_days":30,"drop_after_days":90}`
	if rec := f.call(t, "admin", http.MethodPut, "/"+f.connID+"/sso-identity", cfg); rec.Code != http.StatusOK {
		t.Fatalf("set identity: %d %s", rec.Code, rec.Body.String())
	}
	search, err := f.db.CreateConnection(database.CreateConnectionParams{Name: "search", TunnelToken: "sso-copy-search", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	empty, err := f.db.CreateConnection(database.CreateConnectionParams{Name: "empty", TunnelToken: "sso-copy-empty", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}

	if rec := f.call(t, "viewer", http.MethodPost, "/"+search+"/sso-identity/copy-from", `{"source_id":"`+f.connID+`"}`); rec.Code != http.StatusForbidden {
		t.Fatalf("viewer copy: want 403, got %d", rec.Code)
	}
	for name, c := range map[string]struct{ target, source string }{
		"from itself":       {search, search},
		"from no account":   {search, empty},
		"missing source id": {search, ""},
	} {
		if rec := f.call(t, "admin", http.MethodPost, "/"+c.target+"/sso-identity/copy-from", `{"source_id":"`+c.source+`"}`); rec.Code != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d %s", name, rec.Code, rec.Body.String())
		}
	}

	rec := f.call(t, "admin", http.MethodPost, "/"+search+"/sso-identity/copy-from", `{"source_id":"`+f.connID+`"}`)
	got := decodeSSOIdentity(t, rec)
	if got.ProvisionerUser != "chui_prov" || !got.ProvisionerSet || got.Config.Mode != "per_person" || len(got.Config.RoleMappings) != 1 || got.Config.LockAfterDays != 30 {
		t.Fatalf("copied = %+v", got)
	}
	if body := rec.Body.String(); strings.Contains(body, "prov-pw") {
		t.Fatalf("password in response: %s", body)
	}
	_, srcEnc, _ := f.db.GetConnectionSSOAccount(f.connID)
	_, dstEnc, _ := f.db.GetConnectionSSOAccount(search)
	if dstEnc == "" || dstEnc != srcEnc {
		t.Fatal("the encrypted account password was not copied as is")
	}

	// Per-person setups stay Pro.
	f.cfg.SetProAccessForTest(func() config.ProAccess { return config.ProNone })
	if rec := f.call(t, "admin", http.MethodPost, "/"+empty+"/sso-identity/copy-from", `{"source_id":"`+f.connID+`"}`); rec.Code != http.StatusPaymentRequired {
		t.Fatalf("per-person copy without Pro: want 402, got %d", rec.Code)
	}
}

// The connections list says which connections lack an SSO account, and only
// while SSO sign-in is enabled.
func TestConnectionsListSSOSet(t *testing.T) {
	f := newSSOIdentityFixture(t)
	gw := tunnel.NewGateway(f.db)
	t.Cleanup(gw.Stop)
	h := &ConnectionsHandler{DB: f.db, Config: f.cfg, Gateway: gw}
	list := func() []map[string]any {
		t.Helper()
		rec := httptest.NewRecorder()
		h.List(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		var out []map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode: %v: %s", err, rec.Body.String())
		}
		return out
	}
	if _, ok := list()[0]["sso_set"]; ok {
		t.Fatal("sso_set listed while SSO is off")
	}
	f.cfg.OIDCIssuerURL, f.cfg.OIDCClientID, f.cfg.OIDCClientSecret, f.cfg.OIDCRedirectURL = "https://idp.example", "id", "secret", "https://chui.example/cb"
	if v, ok := list()[0]["sso_set"]; !ok || v != false {
		t.Fatalf("sso_set = %v (present %v), want false", v, ok)
	}
	if err := f.db.SetConnectionSSOAccount(f.connID, "svc", "enc"); err != nil {
		t.Fatal(err)
	}
	if v := list()[0]["sso_set"]; v != true {
		t.Fatalf("sso_set = %v, want true", v)
	}
}
