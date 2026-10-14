// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package database

import "testing"

func TestSSOConfigAcceptUnverifiedEmailRoundTrip(t *testing.T) {
	db := openTestDB(t)

	// A config saved before the field existed has no key: it reads as false.
	if err := db.SetSetting(SettingSSOConfig, `{"enabled":false,"issuer_url":"https://idp.example.com"}`); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetSSOConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got.AcceptUnverifiedEmail {
		t.Fatal("legacy config must read AcceptUnverifiedEmail as false")
	}

	for _, want := range []bool{true, false} {
		cfg := SSOConfig{IssuerURL: "https://idp.example.com", AcceptUnverifiedEmail: want}
		if err := db.SetSSOConfig(cfg); err != nil {
			t.Fatal(err)
		}
		got, err := db.GetSSOConfig()
		if err != nil {
			t.Fatal(err)
		}
		if got.AcceptUnverifiedEmail != want {
			t.Fatalf("AcceptUnverifiedEmail = %v after saving %v", got.AcceptUnverifiedEmail, want)
		}
	}
}
