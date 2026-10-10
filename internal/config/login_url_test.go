package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllowLoginURLPrecedence(t *testing.T) {
	dir := t.TempDir()
	on := filepath.Join(dir, "on.yaml")
	if err := os.WriteFile(on, []byte("allow_login_url: true\n"), 0o600); err != nil {
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
		{"env 1", off, "1", true},
		{"env YES", off, "YES", true},
		{"env other is off", off, "on", false},
		{"env false beats yaml", on, "false", false},
		{"env 0 beats yaml", on, "0", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("ALLOW_LOGIN_URL", tc.env)
			if got := Load(tc.path).AllowLoginURL; got != tc.want {
				t.Fatalf("AllowLoginURL = %v, want %v", got, tc.want)
			}
		})
	}
}
