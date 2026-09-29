package handlers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
)

// The UI percent-encodes role keys, and SSO keys always hold ':' and '@'.
func TestRoleUsernameParamDecodes(t *testing.T) {
	cases := map[string]string{
		"sso%3Abob%40example.com": "sso:bob@example.com",
		"analyst":                 "analyst",
		"ana%40corp.com":          "ana@corp.com",
	}
	for raw, want := range cases {
		rctx := chi.NewRouteContext()
		rctx.URLParams.Add("username", raw)
		req := httptest.NewRequest(http.MethodPut, "/user-roles/x", nil)
		req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
		got, ok := roleUsernameParam(req)
		if !ok || got != want {
			t.Fatalf("%q: got %q ok=%v, want %q", raw, got, ok, want)
		}
	}

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("username", "%zz")
	req := httptest.NewRequest(http.MethodPut, "/user-roles/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))
	if _, ok := roleUsernameParam(req); ok {
		t.Fatal("malformed escape must be rejected")
	}
}
