// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
)

// Read-only Pro routes that use POST stay open in the grace window, while Pro
// writes keep returning 402 there. Without a license everything is 402.
func TestProReadRoutesInGrace(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "admin", UserRole: "admin", Token: "test-session", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	access := config.ProGrace
	cfg := &config.Config{DevMode: true, AppSecretKey: "test-only"}
	cfg.SetProAccessForTest(func() config.ProAccess { return access })
	s := New(cfg, db, nil, nil)
	defer s.gateway.Stop()

	reads := []string{
		"/api/telemetry/traces/search",
		"/api/telemetry/traces/histogram",
		"/api/telemetry/traces/facets",
		"/api/telemetry/metrics/query",
		"/api/telemetry/service-map",
		"/api/schema-compare/compare",
	}
	writes := []struct{ method, path string }{
		{"POST", "/api/telemetry/monitors"},
		{"POST", "/api/schedules/"},
		{"POST", "/api/operations-reports/generate"},
	}
	serve := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{}`))
		req.AddCookie(&http.Cookie{Name: "chui_session", Value: "test-session"})
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		return rec
	}

	for _, path := range reads {
		rec := serve("POST", path)
		if rec.Code == http.StatusPaymentRequired || rec.Code == http.StatusNotFound || rec.Code == http.StatusMethodNotAllowed {
			t.Errorf("grace POST %s: got %d, want it to reach the handler: %s", path, rec.Code, rec.Body.String())
		}
		if rec.Header().Get("X-CH-UI-License-Status") != "grace" {
			t.Errorf("grace POST %s: missing grace header", path)
		}
	}
	if rec := serve("GET", "/api/schema-compare/connections"); rec.Code != http.StatusOK {
		t.Errorf("grace GET /api/schema-compare/connections: got %d: %s", rec.Code, rec.Body.String())
	}
	for _, w := range writes {
		if rec := serve(w.method, w.path); rec.Code != http.StatusPaymentRequired {
			t.Errorf("grace %s %s: got %d, want 402", w.method, w.path, rec.Code)
		}
	}

	access = config.ProNone
	for _, path := range reads {
		if rec := serve("POST", path); rec.Code != http.StatusPaymentRequired {
			t.Errorf("no license POST %s: got %d, want 402", path, rec.Code)
		}
	}
	if rec := serve("GET", "/api/schema-compare/connections"); rec.Code != http.StatusPaymentRequired {
		t.Errorf("no license GET /api/schema-compare/connections: got %d, want 402", rec.Code)
	}
}

// The access control editor is admin only. In grace its reads and the plan
// still work while apply returns 402; without a license all of it is 402.
func TestRBACRouteGating(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "rbac-routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for token, role := range map[string]string{"admin-session": "admin", "viewer-session": "viewer"} {
		if _, err := db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: role, UserRole: role, Token: token, ExpiresAt: expires}); err != nil {
			t.Fatal(err)
		}
	}
	access := config.ProGrace
	cfg := &config.Config{DevMode: true, AppSecretKey: "test-only"}
	cfg.SetProAccessForTest(func() config.ProAccess { return access })
	s := New(cfg, db, nil, nil)
	defer s.gateway.Stop()

	serve := func(token, method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(`{"ops":[]}`))
		req.AddCookie(&http.Cookie{Name: "chui_session", Value: token})
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		return rec
	}
	routes := []struct{ method, path string }{
		{"GET", "/api/rbac/snapshot"},
		{"GET", "/api/rbac/entity/role/analyst"},
		{"POST", "/api/rbac/plan"},
		{"POST", "/api/rbac/apply"},
	}
	reached := func(code int) bool {
		return code != http.StatusPaymentRequired && code != http.StatusForbidden && code != http.StatusNotFound && code != http.StatusMethodNotAllowed
	}

	for _, rt := range routes {
		if rec := serve("viewer-session", rt.method, rt.path); rec.Code != http.StatusForbidden {
			t.Errorf("viewer %s %s: got %d, want 403", rt.method, rt.path, rec.Code)
		}
	}
	for _, rt := range routes[:3] {
		if rec := serve("admin-session", rt.method, rt.path); !reached(rec.Code) {
			t.Errorf("grace %s %s: got %d, want it to reach the handler", rt.method, rt.path, rec.Code)
		}
	}
	if rec := serve("admin-session", "POST", "/api/rbac/apply"); rec.Code != http.StatusPaymentRequired {
		t.Errorf("grace apply: got %d, want 402", rec.Code)
	}

	access = config.ProNone
	for _, rt := range routes {
		if rec := serve("admin-session", rt.method, rt.path); rec.Code != http.StatusPaymentRequired {
			t.Errorf("no license %s %s: got %d, want 402", rt.method, rt.path, rec.Code)
		}
	}
	access = config.ProActive
	if rec := serve("admin-session", "POST", "/api/rbac/apply"); !reached(rec.Code) {
		t.Errorf("active apply: got %d, want it to reach the handler", rec.Code)
	}
}
