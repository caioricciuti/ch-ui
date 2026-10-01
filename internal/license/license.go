// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package license

import (
	"crypto/ed25519"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// LicenseFile is the on-disk JSON format for a signed license.
type LicenseFile struct {
	LicenseID      string   `json:"license_id"`
	Edition        string   `json:"edition"`
	Customer       string   `json:"customer"`
	Features       []string `json:"features"`
	MaxConnections int      `json:"max_connections"`
	IssuedAt       string   `json:"issued_at"`
	ExpiresAt      string   `json:"expires_at"`
	Signature      string   `json:"signature"`
}

// GraceDays is the read-only grace window after a Pro license expires. During
// this window the installation is no longer "active" (writes to Pro features are
// blocked) but read-only access to Pro features — crucially, monitoring — keeps
// working so an expiry never causes a hard lockout in the middle of an incident.
const GraceDays = 14

// LicenseInfo is the public-facing license status returned by the API.
type LicenseInfo struct {
	Edition   string `json:"edition"`
	Valid     bool   `json:"valid"`
	Customer  string `json:"customer,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	LicenseID string `json:"license_id,omitempty"`
	// InGrace is true when the license has expired but is still within the
	// GraceDays read-only window. Valid is false in this state.
	InGrace bool `json:"in_grace,omitempty"`
	// GraceUntil is the RFC3339 timestamp when the grace window ends (only set
	// when InGrace is true).
	GraceUntil string `json:"grace_until,omitempty"`
}

// IsProEdition reports whether a license edition unlocks Pro features. The
// license is all-or-nothing: "enterprise" unlocks exactly what "pro" does.
// The comparison ignores case and surrounding whitespace.
func IsProEdition(edition string) bool {
	switch strings.ToLower(strings.TrimSpace(edition)) {
	case "pro", "enterprise":
		return true
	}
	return false
}

// CommunityLicense returns the default community license info.
func CommunityLicense() *LicenseInfo {
	return &LicenseInfo{
		Edition: "community",
		Valid:   false,
	}
}

// ValidateLicense parses and verifies a signed license JSON string against the
// embedded public key. Returns a LicenseInfo with Valid=true on success, or
// CommunityLicense() on any failure.
func ValidateLicense(licenseJSON string) *LicenseInfo {
	if licenseJSON == "" {
		return CommunityLicense()
	}
	pub, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		slog.Error("Failed to parse embedded public key", "error", err)
		return CommunityLicense()
	}
	return validateLicenseWithKey(licenseJSON, pub)
}

// lastExpiryLog remembers which expiry state was last logged. Background
// workers validate the license on every tick, so the warning is written once
// per license and state (grace, then expired), not on every call.
var lastExpiryLog struct {
	sync.Mutex
	key string
}

func logExpiryOnce(key string) bool {
	lastExpiryLog.Lock()
	defer lastExpiryLog.Unlock()
	if lastExpiryLog.key == key {
		return false
	}
	lastExpiryLog.key = key
	return true
}

// validateLicenseWithKey is the core validation routine, parameterized on the
// verifying key so it can be exercised in tests with a generated keypair.
func validateLicenseWithKey(licenseJSON string, pub ed25519.PublicKey) *LicenseInfo {
	if licenseJSON == "" {
		return CommunityLicense()
	}

	var lf LicenseFile
	if err := json.Unmarshal([]byte(licenseJSON), &lf); err != nil {
		slog.Warn("License parse error", "error", err)
		return CommunityLicense()
	}

	// Rebuild the signable payload (all fields except signature)
	payload := SignablePayload(lf)

	// Decode and verify the signature
	sig, err := base64.StdEncoding.DecodeString(lf.Signature)
	if err != nil {
		slog.Warn("License signature decode error", "error", err)
		return CommunityLicense()
	}

	if !ed25519.Verify(pub, payload, sig) {
		slog.Warn("License signature verification failed")
		return CommunityLicense()
	}

	// Check expiry
	expires, err := time.Parse(time.RFC3339, lf.ExpiresAt)
	if err != nil {
		slog.Warn("License expiry parse error", "error", err)
		return CommunityLicense()
	}

	if expires.Before(time.Now()) {
		graceUntil := expires.AddDate(0, 0, GraceDays)
		inGrace := time.Now().Before(graceUntil)
		state := "expired"
		if inGrace {
			state = "grace"
		}
		if logExpiryOnce(lf.LicenseID + "|" + lf.ExpiresAt + "|" + state) {
			if inGrace {
				slog.Warn("License expired — in read-only grace period", "expires_at", lf.ExpiresAt, "grace_until", graceUntil.Format(time.RFC3339))
			} else {
				slog.Warn("License expired", "expires_at", lf.ExpiresAt)
			}
		}
		return &LicenseInfo{
			Edition:    strings.ToLower(strings.TrimSpace(lf.Edition)),
			Valid:      false,
			Customer:   lf.Customer,
			ExpiresAt:  lf.ExpiresAt,
			LicenseID:  lf.LicenseID,
			InGrace:    inGrace,
			GraceUntil: graceUntil.Format(time.RFC3339),
		}
	}

	edition := strings.ToLower(strings.TrimSpace(lf.Edition))

	slog.Debug("Pro license validated", "customer", lf.Customer, "expires", lf.ExpiresAt)
	return &LicenseInfo{
		Edition:   edition,
		Valid:     true,
		Customer:  lf.Customer,
		ExpiresAt: lf.ExpiresAt,
		LicenseID: lf.LicenseID,
	}
}

// SignablePayload returns the canonical JSON bytes for signature verification.
// All fields except "signature", sorted by key, compact encoding.
func SignablePayload(lf LicenseFile) []byte {
	m := map[string]interface{}{
		"license_id":      lf.LicenseID,
		"edition":         lf.Edition,
		"customer":        lf.Customer,
		"features":        lf.Features,
		"max_connections": lf.MaxConnections,
		"issued_at":       lf.IssuedAt,
		"expires_at":      lf.ExpiresAt,
	}

	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	buf := []byte("{")
	for i, k := range keys {
		if i > 0 {
			buf = append(buf, ',')
		}
		kb, _ := json.Marshal(k)
		vb, _ := json.Marshal(m[k])
		buf = append(buf, kb...)
		buf = append(buf, ':')
		buf = append(buf, vb...)
	}
	buf = append(buf, '}')
	return buf
}

func parsePublicKey(pemData []byte) (ed25519.PublicKey, error) {
	block, _ := pem.Decode(pemData)
	if block == nil {
		return nil, fmt.Errorf("no PEM block found")
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, err
	}
	pub, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("not an Ed25519 public key")
	}
	return pub, nil
}
