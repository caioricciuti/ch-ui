package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/license"
)

func TestLicenseFromEnv(t *testing.T) {
	dir := t.TempDir()
	licPath := filepath.Join(dir, "license.json")
	if err := os.WriteFile(licPath, []byte("  {\"from\":\"file\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	t.Run("unset", func(t *testing.T) {
		t.Setenv("CHUI_LICENSE_FILE", "")
		t.Setenv("CHUI_LICENSE", "")
		if lic, _ := LicenseFromEnv(); lic != "" {
			t.Fatalf("expected empty, got %q", lic)
		}
	})

	t.Run("inline env", func(t *testing.T) {
		t.Setenv("CHUI_LICENSE_FILE", "")
		t.Setenv("CHUI_LICENSE", ` {"from":"env"} `)
		lic, src := LicenseFromEnv()
		if lic != `{"from":"env"}` || src != "CHUI_LICENSE" {
			t.Fatalf("got %q from %q", lic, src)
		}
	})

	t.Run("file wins over inline", func(t *testing.T) {
		t.Setenv("CHUI_LICENSE_FILE", licPath)
		t.Setenv("CHUI_LICENSE", `{"from":"env"}`)
		lic, src := LicenseFromEnv()
		if lic != `{"from":"file"}` {
			t.Fatalf("got %q from %q", lic, src)
		}
	})

	t.Run("unreadable file falls back to inline", func(t *testing.T) {
		t.Setenv("CHUI_LICENSE_FILE", filepath.Join(dir, "missing.json"))
		t.Setenv("CHUI_LICENSE", `{"from":"env"}`)
		lic, _ := LicenseFromEnv()
		if lic != `{"from":"env"}` {
			t.Fatalf("got %q", lic)
		}
	})

	t.Run("empty file falls back to inline", func(t *testing.T) {
		empty := filepath.Join(dir, "empty.json")
		if err := os.WriteFile(empty, []byte("  \n"), 0o600); err != nil {
			t.Fatal(err)
		}
		t.Setenv("CHUI_LICENSE_FILE", empty)
		t.Setenv("CHUI_LICENSE", `{"from":"env"}`)
		lic, _ := LicenseFromEnv()
		if lic != `{"from":"env"}` {
			t.Fatalf("got %q", lic)
		}
	})
}

func TestChooseLicense(t *testing.T) {
	infos := map[string]*license.LicenseInfo{
		"env-active":  {Edition: "pro", Valid: true, ExpiresAt: "2099-01-01T00:00:00Z"},
		"env-grace":   {Edition: "pro", InGrace: true, ExpiresAt: "2026-10-10T00:00:00Z"},
		"env-expired": {Edition: "pro", ExpiresAt: "2026-01-01T00:00:00Z"},
		"env-forged":  license.CommunityLicense(),
		"db-old":      {Edition: "pro", Valid: true, ExpiresAt: "2026-12-01T00:00:00Z"},
	}
	validate := func(lic string) *license.LicenseInfo { return infos[lic] }

	cases := []struct {
		name, env, db     string
		wantJSON, wantSrc string
		wantRejected      string
		wantAccess        ProAccess
	}{
		{"env active beats db", "env-active", "db-old", "env-active", "CHUI_LICENSE", "", ProActive},
		{"env in grace beats db", "env-grace", "db-old", "env-grace", "CHUI_LICENSE", "", ProGrace},
		{"env in grace without db", "env-grace", "", "env-grace", "CHUI_LICENSE", "", ProGrace},
		{"env past grace falls back to db", "env-expired", "db-old", "db-old", "database", "expired past the grace window", ProActive},
		{"forged env falls back to db", "env-forged", "db-old", "db-old", "database", "invalid signature or format", ProActive},
		{"forged env without db", "env-forged", "", "", "", "invalid signature or format", ProNone},
		{"db only", "", "db-old", "db-old", "database", "", ProActive},
		{"nothing", "", "", "", "", "", ProNone},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			src := ""
			if tc.env != "" {
				src = "CHUI_LICENSE"
			}
			got := ChooseLicense(tc.env, src, tc.db, validate)
			if got.JSON != tc.wantJSON || got.Source != tc.wantSrc || got.EnvRejected != tc.wantRejected {
				t.Fatalf("got json=%q source=%q rejected=%q", got.JSON, got.Source, got.EnvRejected)
			}
			if access := proAccessFor(got.Info); access != tc.wantAccess {
				t.Fatalf("access = %v, want %v", access, tc.wantAccess)
			}
		})
	}
}
