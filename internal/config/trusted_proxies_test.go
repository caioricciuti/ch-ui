package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrustedProxiesPrecedence(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	plain := write("plain.yaml", "port: 3488\n")
	empty := write("empty.yaml", "trusted_proxies: []\n")
	listed := write("listed.yaml", "trusted_proxies:\n  - 10.1.0.0/16\n  - 192.0.2.10\n")

	cases := []struct {
		name    string
		path    string
		env     string
		wantNil bool
		want    []string
	}{
		{"unset means default", plain, "", true, nil},
		{"yaml empty list trusts none", empty, "", false, []string{}},
		{"yaml list", listed, "", false, []string{"10.1.0.0/16", "192.0.2.10"}},
		{"env list beats yaml", listed, "172.20.0.0/16, 203.0.113.1", false, []string{"172.20.0.0/16", "203.0.113.1"}},
		{"env none beats yaml", listed, "none", false, []string{}},
		{"env NONE any case", plain, "NONE", false, []string{}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TRUSTED_PROXIES", tc.env)
			got := Load(tc.path).TrustedProxies
			if tc.wantNil {
				if got != nil {
					t.Fatalf("TrustedProxies = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatal("TrustedProxies is nil, want a list")
			}
			if len(got) != len(tc.want) {
				t.Fatalf("TrustedProxies = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("TrustedProxies = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

func TestTrustedProxyPrefixes(t *testing.T) {
	cases := []struct {
		name    string
		list    []string
		wantLen int
		wantErr bool
	}{
		{"nil is the default set", nil, len(DefaultTrustedProxies), false},
		{"empty trusts none", []string{}, 0, false},
		{"none trusts none", []string{"none"}, 0, false},
		{"cidr and single addresses", []string{"10.0.0.0/8", "192.0.2.10", "2001:db8::1", " "}, 3, false},
		{"bad entry", []string{"10.0.0.0/8", "proxy.internal"}, 0, true},
		{"none mixed with others", []string{"none", "10.0.0.0/8"}, 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := (&Config{TrustedProxies: tc.list}).TrustedProxyPrefixes()
			if tc.wantErr {
				if err == nil {
					t.Fatalf("want error, got %v", got)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != tc.wantLen {
				t.Fatalf("got %v, want %d prefixes", got, tc.wantLen)
			}
		})
	}
	got, _ := (&Config{TrustedProxies: []string{"192.0.2.10", "2001:db8::1"}}).TrustedProxyPrefixes()
	if got[0].Bits() != 32 || got[1].Bits() != 128 {
		t.Fatalf("single addresses must become host prefixes, got %v", got)
	}
}
