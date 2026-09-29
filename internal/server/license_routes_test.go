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

// Activating or removing the license changes what every user can do, so only
// admins may. Any signed-in user could before, viewers included.
func TestLicenseChangesRequireAdmin(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(time.Hour).UTC().Format(time.RFC3339)
	for _, s := range []database.CreateSessionParams{
		{ConnectionID: conn, ClickhouseUser: "viewer", UserRole: "viewer", Token: "viewer-session", ExpiresAt: expires},
		{ConnectionID: conn, ClickhouseUser: "analyst", UserRole: "analyst", Token: "analyst-session", ExpiresAt: expires},
		{ConnectionID: conn, ClickhouseUser: "admin", UserRole: "admin", Token: "admin-session", ExpiresAt: expires},
	} {
		if _, err := db.CreateSession(s); err != nil {
			t.Fatal(err)
		}
	}
	s := New(&config.Config{DevMode: true, AppSecretKey: "test-only"}, db, nil, nil)
	defer s.gateway.Stop()

	for _, path := range []string{"/api/license/activate", "/api/license/deactivate"} {
		for token, want := range map[string]int{
			"":                http.StatusUnauthorized,
			"viewer-session":  http.StatusForbidden,
			"analyst-session": http.StatusForbidden,
		} {
			req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"license":"{}"}`))
			if token != "" {
				req.AddCookie(&http.Cookie{Name: "chui_session", Value: token})
			}
			rec := httptest.NewRecorder()
			s.router.ServeHTTP(rec, req)
			if rec.Code != want {
				t.Errorf("POST %s as %q: got %d, want %d: %s", path, token, rec.Code, want, rec.Body.String())
			}
		}
	}

	// An admin reaches the handlers: deactivate succeeds, and activate
	// rejects the bogus license itself (400), not the caller.
	for path, want := range map[string]int{"/api/license/deactivate": http.StatusOK, "/api/license/activate": http.StatusBadRequest} {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{"license":"{}"}`))
		req.AddCookie(&http.Cookie{Name: "chui_session", Value: "admin-session"})
		rec := httptest.NewRecorder()
		s.router.ServeHTTP(rec, req)
		if rec.Code != want {
			t.Errorf("POST %s as admin: got %d, want %d: %s", path, rec.Code, want, rec.Body.String())
		}
	}
}
