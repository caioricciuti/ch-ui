package chendpoint

import "testing"

func TestNormalize(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"http://CH.internal", "http://ch.internal:8123/"},
		{"HTTPS://ch/", "https://ch:8443/"},
		{"http://proxy:8123/ch-a/", "http://proxy:8123/ch-a"},
		{" http://user:secret@ch:8123/?password=x#frag ", "http://ch:8123/"},
		{"http://[::1]:8123", "http://[::1]:8123/"},
	} {
		got, ok := Normalize(tc.in)
		if !ok || got != tc.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", tc.in, got, ok, tc.want)
		}
	}
	for _, bad := range []string{"", "ch:8123", "tcp://ch:9000", "http://", "://x"} {
		if got, ok := Normalize(bad); ok {
			t.Errorf("Normalize(%q) = %q, want not ok", bad, got)
		}
	}
}
