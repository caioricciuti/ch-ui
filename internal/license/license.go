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

// LicenseInfo is the public-facing license status returned by the API.
type LicenseInfo struct {
	Edition   string `json:"edition"`
	Valid     bool   `json:"valid"`
	Customer  string `json:"customer,omitempty"`
	ExpiresAt string `json:"expires_at,omitempty"`
	LicenseID string `json:"license_id,omitempty"`
}

// CommunityLicense returns the default community license info.
func CommunityLicense() *LicenseInfo {
	return &LicenseInfo{
		Edition: "community",
		Valid:   false,
	}
}

// ValidateLicense parses and verifies a signed license JSON string.
// Returns a LicenseInfo with Valid=true on success, or CommunityLicense() on any failure.
func ValidateLicense(licenseJSON string) *LicenseInfo {
	if licenseJSON == "" {
		return CommunityLicense()
	}

	var lf LicenseFile
	if err := json.Unmarshal([]byte(licenseJSON), &lf); err != nil {
		slog.Warn("License parse error", "error", err)
		return CommunityLicense()
	}

	// Decode the embedded public key
	pub, err := parsePublicKey(publicKeyPEM)
	if err != nil {
		slog.Error("Failed to parse embedded public key", "error", err)
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
		slog.Warn("License expired", "expires_at", lf.ExpiresAt)
		return &LicenseInfo{
			Edition:   strings.ToLower(strings.TrimSpace(lf.Edition)),
			Valid:     false,
			Customer:  lf.Customer,
			ExpiresAt: lf.ExpiresAt,
			LicenseID: lf.LicenseID,
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
