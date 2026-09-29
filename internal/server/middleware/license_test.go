// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
)

func proCfg(access config.ProAccess) *config.Config {
	cfg := &config.Config{}
	cfg.SetProAccessForTest(func() config.ProAccess { return access })
	return cfg
}

func serveGate(gate func(*config.Config) func(http.Handler) http.Handler, access config.ProAccess, method string) *httptest.ResponseRecorder {
	h := gate(proCfg(access))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, "/x", nil))
	return rec
}

func TestRequirePro(t *testing.T) {
	cases := []struct {
		access config.ProAccess
		method string
		want   int
	}{
		{config.ProActive, http.MethodGet, http.StatusOK},
		{config.ProActive, http.MethodPost, http.StatusOK},
		{config.ProGrace, http.MethodGet, http.StatusOK},
		{config.ProGrace, http.MethodHead, http.StatusOK},
		{config.ProGrace, http.MethodPost, http.StatusPaymentRequired},
		{config.ProGrace, http.MethodDelete, http.StatusPaymentRequired},
		{config.ProNone, http.MethodGet, http.StatusPaymentRequired},
		{config.ProNone, http.MethodPost, http.StatusPaymentRequired},
	}
	for _, tc := range cases {
		if rec := serveGate(RequirePro, tc.access, tc.method); rec.Code != tc.want {
			t.Errorf("RequirePro access=%v %s: got %d, want %d", tc.access, tc.method, rec.Code, tc.want)
		}
	}
}

func TestRequireProRead(t *testing.T) {
	cases := []struct {
		access config.ProAccess
		method string
		want   int
		grace  bool
	}{
		{config.ProActive, http.MethodGet, http.StatusOK, false},
		{config.ProActive, http.MethodPost, http.StatusOK, false},
		{config.ProGrace, http.MethodGet, http.StatusOK, true},
		{config.ProGrace, http.MethodPost, http.StatusOK, true},
		{config.ProNone, http.MethodGet, http.StatusPaymentRequired, false},
		{config.ProNone, http.MethodPost, http.StatusPaymentRequired, false},
	}
	for _, tc := range cases {
		rec := serveGate(RequireProRead, tc.access, tc.method)
		if rec.Code != tc.want {
			t.Errorf("RequireProRead access=%v %s: got %d, want %d", tc.access, tc.method, rec.Code, tc.want)
		}
		if got := rec.Header().Get("X-CH-UI-License-Status") == "grace"; got != tc.grace {
			t.Errorf("RequireProRead access=%v %s: grace header %v, want %v", tc.access, tc.method, got, tc.grace)
		}
	}
}
