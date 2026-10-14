// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package oidc

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
)

func TestClaimsFromRawEmailVerified(t *testing.T) {
	cases := []struct {
		name         string
		value        interface{}
		set          bool
		wantVerified bool
		wantPresent  bool
	}{
		{"true", true, true, true, true},
		{"false", false, true, false, true},
		{"missing", nil, false, false, false},
		{"null counts as missing", nil, true, false, false},
		{"string true", "true", true, true, true},
		{"string TRUE", "TRUE", true, true, true},
		{"string false", "false", true, false, true},
		{"number is present, not verified", float64(1), true, false, true},
		{"object is present, not verified", map[string]interface{}{"v": true}, true, false, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := map[string]interface{}{"email": "a@example.com", "name": "A", "groups": []interface{}{"g1"}}
			if tc.set {
				raw["email_verified"] = tc.value
			}
			c := claimsFromRaw("sub-1", raw, "groups")
			if c.EmailVerified != tc.wantVerified || c.EmailVerifiedPresent != tc.wantPresent {
				t.Fatalf("got verified=%v present=%v, want verified=%v present=%v", c.EmailVerified, c.EmailVerifiedPresent, tc.wantVerified, tc.wantPresent)
			}
			if c.Subject != "sub-1" || c.Email != "a@example.com" || c.Name != "A" || len(c.Groups) != 1 {
				t.Fatalf("other claims not parsed: %+v", c)
			}
		})
	}
}

func TestSettingsFromConfigAcceptUnverifiedEmail(t *testing.T) {
	if SettingsFromConfig(&config.Config{}).AcceptUnverifiedEmail {
		t.Fatal("AcceptUnverifiedEmail must default to false")
	}
	if !SettingsFromConfig(&config.Config{OIDCAcceptUnverifiedEmail: true}).AcceptUnverifiedEmail {
		t.Fatal("AcceptUnverifiedEmail not carried from config")
	}
}
