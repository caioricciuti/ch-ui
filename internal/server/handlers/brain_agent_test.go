// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// Approve and decline are writes on a Pro-only queue: they pass the gate only
// with an active license, like the tool path that creates approvals.
func TestBrainApprovalRoutesRequirePro(t *testing.T) {
	db, err := dbtest.Open(t, "brain.db")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	cases := []struct {
		access config.ProAccess
		want   int
	}{
		// Past the gate, an unknown approval is a 404 from the handler.
		{config.ProActive, http.StatusNotFound},
		{config.ProGrace, http.StatusPaymentRequired},
		{config.ProNone, http.StatusPaymentRequired},
	}
	for _, tc := range cases {
		for _, action := range []string{"approve", "decline"} {
			cfg := &config.Config{AppSecretKey: "test-secret"}
			access := tc.access
			cfg.SetProAccessForTest(func() config.ProAccess { return access })
			h := &BrainHandler{DB: db, Config: cfg}
			r := chi.NewRouter()
			h.registerAgentRoutes(r)

			req := httptest.NewRequest(http.MethodPost, "/approvals/missing-id/"+action, nil)
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ClickhouseUser: "alice", UserRole: "analyst"}))
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)
			if rec.Code != tc.want {
				t.Fatalf("access %v %s: status %d, want %d (body %s)", tc.access, action, rec.Code, tc.want, rec.Body.String())
			}
		}
	}
}
