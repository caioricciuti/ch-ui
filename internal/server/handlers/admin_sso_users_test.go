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
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

const adminSSOSecret = "admin-sso-test-secret"

// adminSSOFixture has two per-person connections: "prod", whose fake agent
// is online and answers every statement, and "dr", which is offline. Ana
// (SSO) has a provisioned user on both and a session on each; root is a
// password admin on prod.
type adminSSOFixture struct {
	db        *database.DB
	router    chi.Router
	agent     *testutil.Agent
	prod, dr  string
	anaID     string
	adminSess *middleware.SessionInfo
}

func newAdminSSOFixture(t *testing.T) *adminSSOFixture {
	t.Helper()
	db, err := dbtest.Open(t, "admin-sso-users.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	mk := func(name, token string) string {
		id, err := db.CreateConnection(database.CreateConnectionParams{Name: name, TunnelToken: token, Type: database.ConnectionTypeTunnel})
		if err != nil {
			t.Fatal(err)
		}
		cfg := database.DefaultSSOIdentityConfig()
		cfg.Mode = database.SSOModePerPerson
		cfg.RoleMappings = []database.SSORoleMapping{{Group: "analysts", Roles: []string{"analyst"}}}
		if err := db.SetSSOIdentityConfig(id, cfg); err != nil {
			t.Fatal(err)
		}
		enc, err := crypto.Encrypt("prov-pw", adminSSOSecret)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetConnectionSSOAccount(id, "chui_prov", enc); err != nil {
			t.Fatal(err)
		}
		return id
	}
	prod, dr := mk("prod", "prod-token"), mk("dr", "dr-token")

	ana, err := db.UpsertSSOIdentity("https://idp.example", "sub-ana", "Ana@X.com", "Ana", []string{"analysts"})
	if err != nil {
		t.Fatal(err)
	}
	enc, err := crypto.Encrypt("ana-pw", adminSSOSecret)
	if err != nil {
		t.Fatal(err)
	}
	for _, conn := range []string{prod, dr} {
		if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{IdentityID: ana.ID, ConnectionID: conn, CHUser: "sso:ana@x.com", EncryptedPassword: enc, Roles: []string{"analyst"}}); err != nil {
			t.Fatal(err)
		}
	}
	if err := db.SetSSOClickHouseUserLocked(ana.ID, dr, true); err != nil {
		t.Fatal(err)
	}

	rootEnc, err := crypto.Encrypt("root-pw", adminSSOSecret)
	if err != nil {
		t.Fatal(err)
	}
	exp := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	for _, p := range []database.CreateSessionParams{
		{ConnectionID: prod, ClickhouseUser: "root", Token: "root-prod", UserRole: "admin", EncryptedPassword: rootEnc},
		{ConnectionID: prod, ClickhouseUser: "sso:ana@x.com", Token: "ana-prod", UserRole: "analyst", AuthSubject: "ana@x.com", SSOIdentityID: ana.ID, EncryptedPassword: enc},
		{ConnectionID: dr, ClickhouseUser: "sso:ana@x.com", Token: "ana-dr", UserRole: "analyst", AuthSubject: "ana@x.com", SSOIdentityID: ana.ID, EncryptedPassword: enc},
		{ConnectionID: prod, ClickhouseUser: "svc", Token: "bob-prod", UserRole: "viewer", AuthSubject: "bob@x.com", EncryptedPassword: enc},
	} {
		p.ExpiresAt = exp
		if _, err := db.CreateSession(p); err != nil {
			t.Fatal(err)
		}
	}

	agent := testutil.NewAgent(t, db, "prod-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		data := "[]"
		if strings.Contains(msg.SQL, "system.users") {
			data = `[{"name":"root"},{"name":"svc"},{"name":"sso:ana@x.com"}]`
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data), Meta: json.RawMessage("[]")}
	})
	cfg := &config.Config{AppSecretKey: adminSSOSecret}
	h := &AdminHandler{DB: db, Gateway: agent.Gateway, Config: cfg, Provisioner: &ssoidentity.Provisioner{DB: db, Gateway: agent.Gateway, Secret: adminSSOSecret}}
	r := chi.NewRouter()
	r.Route("/admin", h.Routes)
	return &adminSSOFixture{
		db: db, router: r, agent: agent, prod: prod, dr: dr, anaID: ana.ID,
		adminSess: &middleware.SessionInfo{ConnectionID: prod, ClickhouseUser: "root", UserRole: "admin", EncryptedPassword: rootEnc},
	}
}

func (f *adminSSOFixture) call(t *testing.T, sess *middleware.SessionInfo, method, path string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	req.RemoteAddr = "198.51.100.42:5000"
	req = req.WithContext(middleware.SetSession(req.Context(), sess))
	rec := httptest.NewRecorder()
	f.router.ServeHTTP(rec, req)
	return rec
}

type adminUsersResponse struct {
	Users []struct {
		database.SessionUser
		ExistsInClickHouse bool                       `json:"exists_in_clickhouse"`
		ClickHouseUsers    []ssoClickHouseUserSummary `json:"clickhouse_users"`
	} `json:"users"`
	Sync map[string]bool `json:"sync"`
}

func TestAdminUsersCarryProvisionedClickHouseUsers(t *testing.T) {
	f := newAdminSSOFixture(t)
	rec := f.call(t, f.adminSess, http.MethodGet, "/admin/users")
	if rec.Code != http.StatusOK {
		t.Fatalf("users: %d %s", rec.Code, rec.Body.String())
	}
	var out adminUsersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	if !out.Sync["clickhouse_user_check"] {
		t.Fatalf("ClickHouse comparison did not run: %s", rec.Body.String())
	}
	byKey := map[string]int{}
	for i, u := range out.Users {
		byKey[u.Username] = i
	}
	ana, ok := byKey["sso:ana@x.com"]
	if !ok {
		t.Fatalf("ana missing from %s", rec.Body.String())
	}
	got := out.Users[ana].ClickHouseUsers
	if len(got) != 2 {
		t.Fatalf("ana's clickhouse_users = %+v, want prod and dr", got)
	}
	byConn := map[string]ssoClickHouseUserSummary{}
	for _, u := range got {
		byConn[u.ConnectionID] = u
	}
	prod, dr := byConn[f.prod], byConn[f.dr]
	if prod.ConnectionName != "prod" || prod.CHUser != "sso:ana@x.com" || strings.Join(prod.Roles, ",") != "analyst" || prod.Locked || prod.LastSyncAt == "" || prod.LastSeenAt == "" {
		t.Errorf("prod entry = %+v", prod)
	}
	if dr.ConnectionName != "dr" || !dr.Locked {
		t.Errorf("dr entry = %+v, want locked", dr)
	}
	for _, key := range []string{"root", "sso:bob@x.com"} {
		i, ok := byKey[key]
		if !ok {
			t.Fatalf("%s missing from %s", key, rec.Body.String())
		}
		if u := out.Users[i].ClickHouseUsers; u == nil || len(u) != 0 {
			t.Errorf("%s clickhouse_users = %v, want []", key, u)
		}
	}
	if !strings.Contains(rec.Body.String(), `"clickhouse_users":[]`) {
		t.Fatalf("empty list must serialize as [] not null: %s", rec.Body.String())
	}
}

func TestAdminRemoveSSOAccess(t *testing.T) {
	f := newAdminSSOFixture(t)

	if rec := f.call(t, &middleware.SessionInfo{ConnectionID: f.prod, ClickhouseUser: "x", UserRole: "analyst"}, http.MethodDelete, "/admin/users/sso/ana%40x.com/access"); rec.Code != http.StatusForbidden {
		t.Fatalf("analyst: %d, want 403", rec.Code)
	}

	before := len(f.agent.Messages())
	rec := f.call(t, f.adminSess, http.MethodDelete, "/admin/users/sso/Ana%40X.com/access")
	if rec.Code != http.StatusOK {
		t.Fatalf("remove access: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Dropped       int                `json:"dropped"`
		Failed        []ssoAccessFailure `json:"failed"`
		SessionsEnded int                `json:"sessions_ended"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v: %s", err, rec.Body.String())
	}
	if out.Dropped != 1 || out.SessionsEnded != 2 || len(out.Failed) != 1 {
		t.Fatalf("response = %+v", out)
	}
	if fl := out.Failed[0]; fl.ConnectionID != f.dr || fl.ConnectionName != "dr" || !strings.Contains(fl.Error, "offline") {
		t.Fatalf("failed = %+v", fl)
	}

	var drops []string
	for _, m := range f.agent.Messages()[before:] {
		if m.Type == "query" {
			drops = append(drops, m.SQL)
			if m.User != "chui_prov" || m.Password != "prov-pw" {
				t.Errorf("%q ran as %s, want the provisioner", m.SQL, m.User)
			}
		}
	}
	if len(drops) != 1 || drops[0] != "DROP USER IF EXISTS `sso:ana@x.com`" {
		t.Fatalf("statements: %q", drops)
	}

	// prod's row is gone, dr's is kept for a retry.
	if row, err := f.db.GetSSOClickHouseUser(f.anaID, f.prod); err != nil || row != nil {
		t.Fatalf("prod row after drop = %+v, err = %v", row, err)
	}
	if row, err := f.db.GetSSOClickHouseUser(f.anaID, f.dr); err != nil || row == nil {
		t.Fatalf("dr row after failed drop = %+v, err = %v", row, err)
	}

	// Ana is signed out everywhere; bob and root are untouched.
	for token, wantAlive := range map[string]bool{"ana-prod": false, "ana-dr": false, "bob-prod": true, "root-prod": true} {
		s, err := f.db.GetSession(token)
		if err != nil {
			t.Fatal(err)
		}
		if (s != nil) != wantAlive {
			t.Errorf("session %s alive = %v, want %v", token, s != nil, wantAlive)
		}
	}

	logs := auditActions(t, f.db)
	a := findAudit(logs, "sso.access_removed", "Removed SSO access for ana@x.com: 1 ClickHouse user(s) dropped, 1 failed, 2 session(s) signed out")
	if a == nil {
		t.Fatalf("access removed audit row missing")
	}
	if a.Username == nil || *a.Username != "root" || a.IPAddress == nil || *a.IPAddress != "198.51.100.42" {
		t.Fatalf("audit attribution = %+v", a)
	}
	if d := findAudit(logs, "sso.user_dropped", "ana@x.com"); d == nil || d.Username == nil || *d.Username != "root" {
		t.Fatalf("provisioner drop audit row = %+v", d)
	}

	// Nothing left: a second call drops nothing and ends no sessions.
	rec = f.call(t, f.adminSess, http.MethodDelete, "/admin/users/sso/ana%40x.com/access")
	if rec.Code != http.StatusOK {
		t.Fatalf("second call: %d %s", rec.Code, rec.Body.String())
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Dropped != 0 || out.SessionsEnded != 0 || len(out.Failed) != 1 {
		t.Fatalf("second response = %+v", out)
	}
}
