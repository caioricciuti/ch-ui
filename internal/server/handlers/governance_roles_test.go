// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/governance"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func newGovernanceRoleRouter(t *testing.T) (*database.DB, *governance.Store, string, string, http.Handler) {
	t.Helper()
	db, err := dbtest.Open(t, "gov-roles.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connA, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	connB, err := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"})
	if err != nil {
		t.Fatal(err)
	}
	store := governance.NewStore(db)
	h := &GovernanceHandler{DB: db, Config: &config.Config{AppSecretKey: "test-secret"}, Store: store}
	return db, store, connA, connB, h.Routes()
}

func serveAs(router http.Handler, role, connectionID, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: role, ConnectionID: connectionID}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestGovernanceAlertAndTagWritesAreAdminOnly(t *testing.T) {
	_, _, connA, _, router := newGovernanceRoleRouter(t)
	writes := []struct{ method, path string }{
		{http.MethodPost, "/alerts/channels"},
		{http.MethodPut, "/alerts/channels/x"},
		{http.MethodDelete, "/alerts/channels/x"},
		{http.MethodPost, "/alerts/channels/x/test"},
		{http.MethodPost, "/alerts/rules"},
		{http.MethodPut, "/alerts/rules/x"},
		{http.MethodDelete, "/alerts/rules/x"},
		{http.MethodPost, "/tags"},
		{http.MethodDelete, "/tags/x"},
	}
	for _, role := range []string{"viewer", "analyst"} {
		for _, w := range writes {
			if rec := serveAs(router, role, connA, w.method, w.path, `{}`); rec.Code != http.StatusForbidden {
				t.Errorf("%s %s %s: status %d, want 403", role, w.method, w.path, rec.Code)
			}
		}
	}
	for _, w := range writes {
		if rec := serveAs(router, "admin", connA, w.method, w.path, `{}`); rec.Code == http.StatusForbidden {
			t.Errorf("admin %s %s: got 403", w.method, w.path)
		}
	}
	for _, path := range []string{"/alerts/channels", "/alerts/rules", "/alerts/events"} {
		if rec := serveAs(router, "viewer", connA, http.MethodGet, path, ""); rec.Code == http.StatusForbidden {
			t.Errorf("viewer GET %s: got 403, reads stay open", path)
		}
	}
}

func TestGovernanceTagDeleteStaysOnConnection(t *testing.T) {
	_, store, connA, connB, router := newGovernanceRoleRouter(t)
	id, err := store.CreateTag(connB, "table", "db", "t", "", governance.TagPII, "admin")
	if err != nil {
		t.Fatal(err)
	}
	if rec := serveAs(router, "admin", connA, http.MethodDelete, "/tags/"+id, ""); rec.Code != http.StatusNotFound {
		t.Fatalf("delete from another connection: status %d, want 404", rec.Code)
	}
	if rec := serveAs(router, "admin", connB, http.MethodDelete, "/tags/"+id, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete on own connection: status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}

func TestGovernancePolicyStaysOnConnection(t *testing.T) {
	_, store, connA, connB, router := newGovernanceRoleRouter(t)
	id, err := store.CreatePolicy(connB, "pii", "", "table", "db", "t", "", "pii_reader", "warn", "warn", "admin")
	if err != nil {
		t.Fatal(err)
	}
	update := `{"name":"renamed","required_role":"pii_reader","severity":"warn","enforcement_mode":"warn"}`
	for _, c := range []struct{ method, body string }{
		{http.MethodGet, ""},
		{http.MethodPut, update},
		{http.MethodDelete, ""},
	} {
		if rec := serveAs(router, "admin", connA, c.method, "/policies/"+id, c.body); rec.Code != http.StatusNotFound {
			t.Errorf("%s from another connection: status %d, want 404", c.method, rec.Code)
		}
	}
	p, err := store.GetPolicyByID(id)
	if err != nil || p == nil {
		t.Fatalf("policy gone after cross-connection requests: %v", err)
	}
	if p.Name != "pii" {
		t.Errorf("cross-connection update changed the name to %q", p.Name)
	}

	if rec := serveAs(router, "admin", connB, http.MethodGet, "/policies/"+id, ""); rec.Code != http.StatusOK {
		t.Errorf("get on own connection: status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "admin", connB, http.MethodPut, "/policies/"+id, update); rec.Code != http.StatusOK {
		t.Errorf("update on own connection: status %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if rec := serveAs(router, "admin", connB, http.MethodDelete, "/policies/"+id, ""); rec.Code != http.StatusOK {
		t.Errorf("delete on own connection: status %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
