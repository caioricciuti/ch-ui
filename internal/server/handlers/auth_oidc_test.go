// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/oidc"
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
