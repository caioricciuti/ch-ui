// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package license

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"
)

// signLicense produces a signed license JSON string using the given private key.
func signLicense(t *testing.T, priv ed25519.PrivateKey, lf LicenseFile) string {
	t.Helper()
	lf.Signature = base64.StdEncoding.EncodeToString(ed25519.Sign(priv, SignablePayload(lf)))
	b, err := json.Marshal(lf)
	if err != nil {
		t.Fatalf("marshal license: %v", err)
	}
	return string(b)
}

func baseLicense() LicenseFile {
	return LicenseFile{
		LicenseID:      "lic_test_123",
		Edition:        "pro",
		Customer:       "Acme Corp",
		Features:       []string{"governance", "cluster_health"},
		MaxConnections: 5,
		IssuedAt:       time.Now().Add(-720 * time.Hour).Format(time.RFC3339),
	}
}

func TestValidateLicense_EmptyAndMalformed(t *testing.T) {
	_, pub := mustKeypair(t)
	for name, in := range map[string]string{
		"empty":      "",
		"not-json":   "{not json",
		"empty-json": "{}",
	} {
		t.Run(name, func(t *testing.T) {
			got := validateLicenseWithKey(in, pub)
			if got.Valid {
				t.Fatalf("%s: expected invalid, got Valid=true", name)
			}
		})
	}
}

func TestValidateLicense_ValidPro(t *testing.T) {
	priv, pub := mustKeypair(t)
	lf := baseLicense()
	lf.ExpiresAt = time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)

	got := validateLicenseWithKey(signLicense(t, priv, lf), pub)
	if !got.Valid {
		t.Fatalf("expected valid license")
	}
	if got.Edition != "pro" {
		t.Fatalf("expected edition pro, got %q", got.Edition)
	}
	if got.InGrace {
		t.Fatalf("active license should not be in grace")
	}
	if got.Customer != "Acme Corp" {
		t.Fatalf("customer mismatch: %q", got.Customer)
	}
}

func TestValidateLicense_TamperedSignatureRejected(t *testing.T) {
	priv, pub := mustKeypair(t)
	lf := baseLicense()
	lf.ExpiresAt = time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)
	signed := signLicense(t, priv, lf)

	// Tamper with the customer after signing.
	var tampered LicenseFile
	if err := json.Unmarshal([]byte(signed), &tampered); err != nil {
		t.Fatal(err)
	}
	tampered.Customer = "Pirate Inc"
	b, _ := json.Marshal(tampered)

	got := validateLicenseWithKey(string(b), pub)
	if got.Valid {
		t.Fatalf("tampered license must be rejected")
	}
}

func TestValidateLicense_WrongKeyRejected(t *testing.T) {
	priv, _ := mustKeypair(t)
	_, otherPub := mustKeypair(t)
	lf := baseLicense()
	lf.ExpiresAt = time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)

	got := validateLicenseWithKey(signLicense(t, priv, lf), otherPub)
	if got.Valid {
		t.Fatalf("license signed by a different key must be rejected")
	}
}

func TestValidateLicense_ExpiredWithinGraceIsReadOnly(t *testing.T) {
	priv, pub := mustKeypair(t)
	lf := baseLicense()
	// Expired 2 days ago — inside the GraceDays window.
	lf.ExpiresAt = time.Now().Add(-48 * time.Hour).Format(time.RFC3339)

	got := validateLicenseWithKey(signLicense(t, priv, lf), pub)
	if got.Valid {
		t.Fatalf("expired license must not be Valid")
	}
	if !got.InGrace {
		t.Fatalf("license expired %d days ago should be in grace (window=%d days)", 2, GraceDays)
	}
	if got.Edition != "pro" {
		t.Fatalf("edition should still be reported as pro during grace, got %q", got.Edition)
	}
	if got.GraceUntil == "" {
		t.Fatalf("GraceUntil should be set during grace")
	}
}

func TestValidateLicense_ExpiredPastGrace(t *testing.T) {
	priv, pub := mustKeypair(t)
	lf := baseLicense()
	// Expired well beyond the grace window.
	lf.ExpiresAt = time.Now().Add(-time.Duration(GraceDays+10) * 24 * time.Hour).Format(time.RFC3339)

	got := validateLicenseWithKey(signLicense(t, priv, lf), pub)
	if got.Valid {
		t.Fatalf("license past grace must not be Valid")
	}
	if got.InGrace {
		t.Fatalf("license past grace must not be InGrace")
	}
}

func mustKeypair(t *testing.T) (ed25519.PrivateKey, ed25519.PublicKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("generate keypair: %v", err)
	}
	return priv, pub
}

func TestLogExpiryOnce(t *testing.T) {
	if !logExpiryOnce("lic-1|2026-01-01T00:00:00Z|grace") {
		t.Fatal("first grace call must log")
	}
	if logExpiryOnce("lic-1|2026-01-01T00:00:00Z|grace") {
		t.Fatal("repeat grace call must not log")
	}
	if !logExpiryOnce("lic-1|2026-01-01T00:00:00Z|expired") {
		t.Fatal("moving from grace to expired must log")
	}
	if !logExpiryOnce("lic-2|2027-01-01T00:00:00Z|grace") {
		t.Fatal("a different license must log")
	}
}
