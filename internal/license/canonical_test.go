// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package license

import (
	"crypto/ed25519"
	"testing"
	"time"
)

// TestSignablePayloadCanonical locks the canonical byte format shared with the
// license server. The input and the golden vector are copied verbatim from
// TestSignablePayloadCanonical in the ch-ui-license repo (license_test.go). If
// either canonicalizer drifts, licenses stop verifying; change both repos and
// both copies of this vector together, never one.
func TestSignablePayloadCanonical(t *testing.T) {
	lf := LicenseFile{
		LicenseID:      "id-1",
		Edition:        "pro",
		Customer:       "Acme",
		IssuedAt:       "2026-01-01T00:00:00Z",
		ExpiresAt:      "2027-01-01T00:00:00Z",
		MaxConnections: 0,
	}
	want := `{"customer":"Acme","edition":"pro","expires_at":"2027-01-01T00:00:00Z","features":null,"issued_at":"2026-01-01T00:00:00Z","license_id":"id-1","max_connections":0}`
	if got := string(SignablePayload(lf)); got != want {
		t.Fatalf("canonical payload mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestValidateLicense_IssuedAtInFuture(t *testing.T) {
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name     string
		issuedAt string
		valid    bool
	}{
		{"past", time.Now().Add(-time.Hour).Format(time.RFC3339), true},
		{"within skew", time.Now().Add(MaxIssuedAtSkew - time.Hour).Format(time.RFC3339), true},
		{"beyond skew", time.Now().Add(MaxIssuedAtSkew + time.Hour).Format(time.RFC3339), false},
		{"empty is ignored", "", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			lf := baseLicense()
			lf.IssuedAt = tc.issuedAt
			lf.ExpiresAt = time.Now().AddDate(1, 0, 0).Format(time.RFC3339)
			info := validateLicenseWithKey(signLicense(t, priv, lf), pub)
			if info.Valid != tc.valid {
				t.Fatalf("issued_at %q: valid=%v, want %v", tc.issuedAt, info.Valid, tc.valid)
			}
			if !tc.valid && info.Edition != "community" {
				t.Fatalf("rejected license must fall back to community, got %q", info.Edition)
			}
		})
	}
}
