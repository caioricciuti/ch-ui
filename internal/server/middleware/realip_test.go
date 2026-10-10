package middleware

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
)

func defaultTrusted(t *testing.T) []netip.Prefix {
	t.Helper()
	p, err := (&config.Config{}).TrustedProxyPrefixes()
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveClient(t *testing.T) {
	trusted := defaultTrusted(t)
	cases := []struct {
		name     string
		peer     string
		xff      []string
		realIP   string
		trusted  []netip.Prefix
		wantIP   string
		viaProxy bool
	}{
		{"public peer, no headers", "203.0.113.9:5000", nil, "", trusted, "203.0.113.9", false},
		{"public peer spoofing XFF is ignored", "203.0.113.9:5000", []string{"1.1.1.1"}, "", trusted, "203.0.113.9", false},
		{"public peer spoofing X-Real-IP is ignored", "203.0.113.9:5000", nil, "1.1.1.1", trusted, "203.0.113.9", false},
		{"proxy on loopback, one hop", "127.0.0.1:5000", []string{"198.51.100.7"}, "", trusted, "198.51.100.7", true},
		{"client-prepended fake hop is skipped", "127.0.0.1:5000", []string{"6.6.6.6, 198.51.100.7, 10.0.0.5"}, "", trusted, "198.51.100.7", true},
		{"two XFF headers are one chain", "10.0.0.1:5000", []string{"6.6.6.6", "198.51.100.7, 10.0.0.5"}, "", trusted, "198.51.100.7", true},
		{"all hops trusted: leftmost is the client", "127.0.0.1:5000", []string{"10.0.0.9, 10.0.0.5"}, "", trusted, "10.0.0.9", true},
		{"ipv6 hop with brackets and port", "127.0.0.1:5000", []string{"[2001:db8::1]:4444"}, "", trusted, "2001:db8::1", true},
		{"ipv4-mapped hop is unmapped", "127.0.0.1:5000", []string{"::ffff:198.51.100.7"}, "", trusted, "198.51.100.7", true},
		{"malformed hop falls back to the trusted hop right of it", "127.0.0.1:5000", []string{"junk, 10.0.0.5"}, "", trusted, "10.0.0.5", true},
		{"malformed only hop falls back to the peer", "127.0.0.1:5000", []string{"junk"}, "", trusted, "127.0.0.1", true},
		{"X-Real-IP when there is no XFF", "127.0.0.1:5000", nil, "198.51.100.8", trusted, "198.51.100.8", true},
		{"proxy without headers", "[::1]:5000", nil, "", trusted, "::1", true},
		{"ipv4-mapped loopback peer is trusted", "[::ffff:127.0.0.1]:5000", []string{"198.51.100.7"}, "", trusted, "198.51.100.7", true},
		{"trust nobody", "127.0.0.1:5000", []string{"198.51.100.7"}, "", []netip.Prefix{}, "127.0.0.1", false},
		{"explicit single proxy", "192.0.2.10:5000", []string{"198.51.100.7"}, "", []netip.Prefix{netip.MustParsePrefix("192.0.2.10/32")}, "198.51.100.7", true},
		{"peer without port", "203.0.113.9", []string{"1.1.1.1"}, "", trusted, "203.0.113.9", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.RemoteAddr = tc.peer
			for _, v := range tc.xff {
				r.Header.Add("X-Forwarded-For", v)
			}
			if tc.realIP != "" {
				r.Header.Set("X-Real-IP", tc.realIP)
			}
			got := resolveClient(r, tc.trusted)
			if got.ip != tc.wantIP || got.viaTrustedProxy != tc.viaProxy {
				t.Fatalf("got %q via=%v, want %q via=%v", got.ip, got.viaTrustedProxy, tc.wantIP, tc.viaProxy)
			}
		})
	}
}

func TestRealIPMiddlewareAndFallback(t *testing.T) {
	trusted := defaultTrusted(t)
	var sawIP string
	var sawProxy bool
	h := RealIP(trusted)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawIP = ClientIP(r)
		sawProxy = FromTrustedProxy(r)
	}))

	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if sawIP != "198.51.100.7" || !sawProxy {
		t.Fatalf("through middleware: ip=%q proxy=%v", sawIP, sawProxy)
	}

	// Without the middleware the helpers fall back to the TCP peer and never
	// believe a header.
	r = httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("X-Forwarded-For", "198.51.100.7")
	if got := ClientIP(r); got != "127.0.0.1" {
		t.Fatalf("ClientIP without middleware = %q", got)
	}
	if FromTrustedProxy(r) {
		t.Fatal("FromTrustedProxy without middleware must be false")
	}
}
