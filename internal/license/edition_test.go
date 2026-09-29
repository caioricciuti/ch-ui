package license

import (
	"testing"
	"time"
)

func TestIsProEdition(t *testing.T) {
	for edition, want := range map[string]bool{
		"pro": true, "Pro": true, " PRO ": true,
		"enterprise": true, "Enterprise": true, " ENTERPRISE\n": true,
		"community": false, "": false, "pro-trial": false, "enterprises": false,
	} {
		if got := IsProEdition(edition); got != want {
			t.Errorf("IsProEdition(%q) = %v, want %v", edition, got, want)
		}
	}
}

// An enterprise license validates like a pro one; the API reports the
// edition lowercased so the UI can compare it directly.
func TestValidateLicense_ValidEnterprise(t *testing.T) {
	priv, pub := mustKeypair(t)
	lf := baseLicense()
	lf.Edition = "Enterprise"
	lf.ExpiresAt = time.Now().Add(365 * 24 * time.Hour).Format(time.RFC3339)

	got := validateLicenseWithKey(signLicense(t, priv, lf), pub)
	if !got.Valid || got.Edition != "enterprise" || !IsProEdition(got.Edition) {
		t.Fatalf("enterprise license: %+v", got)
	}
}
