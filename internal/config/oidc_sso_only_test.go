package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOIDCSSOOnlyPrecedence(t *testing.T) {
	dir := t.TempDir()
	on := filepath.Join(dir, "on.yaml")
	if err := os.WriteFile(on, []byte("oidc_sso_only: true\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	off := filepath.Join(dir, "off.yaml")
	if err := os.WriteFile(off, []byte("port: 3488\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name string
		path string
		env  string
		want bool
	}{
		{"default off", off, "", false},
		{"yaml on", on, "", true},
		{"env true", off, "true", true},
		{"env false beats yaml", on, "false", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("OIDC_SSO_ONLY", tc.env)
			if got := Load(tc.path).OIDCSSOOnly; got != tc.want {
				t.Fatalf("OIDCSSOOnly = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestAllowPasswordLoginEnv(t *testing.T) {
	off := filepath.Join(t.TempDir(), "off.yaml")
	if err := os.WriteFile(off, []byte("port: 3488\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CHUI_ALLOW_PASSWORD_LOGIN", "")
	if Load(off).AllowPasswordLogin {
		t.Fatal("break-glass must default to off")
	}
	t.Setenv("CHUI_ALLOW_PASSWORD_LOGIN", "true")
	if !Load(off).AllowPasswordLogin {
		t.Fatal("CHUI_ALLOW_PASSWORD_LOGIN=true not read")
	}
}
