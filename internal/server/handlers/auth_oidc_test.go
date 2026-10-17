// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/oidc"
	"github.com/caioricciuti/ch-ui/internal/ssoidentity"
)

func TestOIDCRoleMapping(t *testing.T) {
	s := oidc.Settings{
		AdminGroups:   []string{"ch-ui-admins", "platform"},
		AnalystGroups: []string{"data-analysts"},
	}

	cases := []struct {
		name   string
		groups []string
		want   string
	}{
		{"admin wins", []string{"data-analysts", "platform"}, "admin"},
		{"analyst", []string{"data-analysts"}, "analyst"},
		{"case-insensitive admin", []string{"CH-UI-Admins"}, "admin"},
		{"unknown group -> viewer", []string{"random"}, "viewer"},
		{"no groups -> viewer", nil, "viewer"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := oidcRole(s, tc.groups); got != tc.want {
				t.Fatalf("oidcRole(%v) = %q, want %q", tc.groups, got, tc.want)
			}
		})
	}
}

func TestOIDCDomainAllowed(t *testing.T) {
	t.Run("no restriction allows all", func(t *testing.T) {
		if !oidcDomainAllowed(oidc.Settings{}, "anyone@example.com") {
			t.Fatal("expected all domains allowed when none configured")
		}
	})

	s := oidc.Settings{
		AllowedDomains: []string{"caioricciuti.com", "acme.io"},
	}
	cases := map[string]bool{
		"caio@caioricciuti.com": true,
		"x@ACME.IO":             true, // case-insensitive
		"evil@attacker.com":     false,
		"no-at-sign":            false,
	}
	for email, want := range cases {
		if got := oidcDomainAllowed(s, email); got != want {
			t.Errorf("oidcDomainAllowed(%q) = %v, want %v", email, got, want)
		}
	}
}

func TestSSOProvisionError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
		wantMsg    string
	}{
		{
			"no roles", &ssoidentity.NoRolesError{ConnectionID: "c1"}, http.StatusConflict, "sso_no_roles",
			"Your groups don't give you access to analytics. Ask an admin to map one of them in Admin > Connections > SSO.",
		},
		{
			"wrapped no roles", fmt.Errorf("sign in: %w", &ssoidentity.NoRolesError{ConnectionID: "c1"}), http.StatusConflict, "sso_no_roles",
			"Your groups don't give you access to analytics. Ask an admin to map one of them in Admin > Connections > SSO.",
		},
		{
			"user exists", &ssoidentity.UserExistsError{CHUser: "sso:alice@example.com"}, http.StatusConflict, "sso_user_exists",
			"A ClickHouse user named sso:alice@example.com already exists and was not created by CH-UI. An admin must remove or rename it before you can sign in.",
		},
		{
			"anything else", errors.New("CREATE USER failed: Code: 497"), http.StatusBadGateway, "sso_provisioning_failed",
			"SSO user provisioning failed on analytics; an admin can see the error in the audit log.",
		},
		{
			"shared mode race", ssoidentity.ErrSharedMode, http.StatusBadGateway, "sso_provisioning_failed",
			"SSO user provisioning failed on analytics; an admin can see the error in the audit log.",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			status, code, msg := ssoProvisionError(tc.err, "analytics")
			if status != tc.wantStatus || code != tc.wantCode || msg != tc.wantMsg {
				t.Fatalf("got (%d, %q, %q), want (%d, %q, %q)", status, code, msg, tc.wantStatus, tc.wantCode, tc.wantMsg)
			}
			if strings.Contains(msg, tc.err.Error()) && tc.wantCode == "sso_provisioning_failed" {
				t.Fatalf("raw error leaked into the user message: %q", msg)
			}
		})
	}
}
