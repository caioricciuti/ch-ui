package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSecurityBrowserRequestProtection(t *testing.T) {
	cfg := CORSConfig{AppURL: "https://app.example", AllowedOrigins: []string{"https://trusted.example"}}
	for _, tc := range []struct {
		name, method, origin, site string
		want                       int
	}{
		{"same origin", "POST", "https://app.example", "same-origin", 204},
		{"trusted origin", "POST", "https://trusted.example", "cross-site", 204},
		{"attacker", "POST", "https://evil.example", "cross-site", 403},
		{"sibling origin", "POST", "https://sibling.example", "same-site", 403},
		{"downgrade", "POST", "http://app.example", "same-site", 403},
		{"opaque origin", "POST", "null", "cross-site", 403},
		{"missing cross-site origin", "POST", "", "cross-site", 403},
		{"missing sibling origin", "POST", "", "same-site", 403},
		{"CLI", "POST", "", "", 204},
		{"read", "GET", "https://evil.example", "cross-site", 204},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(tc.method, "https://app.example/api/query", nil)
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("Sec-Fetch-Site", tc.site)
			w := httptest.NewRecorder()
			BrowserRequestProtection(cfg)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })).ServeHTTP(w, r)
			if w.Code != tc.want {
				t.Fatalf("status %d want %d", w.Code, tc.want)
			}
		})
	}
}

// Behind a TLS terminator, APP_URL still forbids the plaintext origin even
// when the proxy omitted X-Forwarded-Proto.
func TestSecurityBrowserPublicScheme(t *testing.T) {
	cfg := CORSConfig{AppURL: "https://app.example"}
	r := httptest.NewRequest(http.MethodPost, "http://app.example/api/query", nil)
	r.Header.Set("Origin", "http://app.example")
	if BrowserRequestAllowed(cfg, r) {
		t.Fatal("accepted downgrade origin behind TLS proxy")
	}
	r.Header.Set("Origin", "https://app.example")
	if !BrowserRequestAllowed(cfg, r) {
		t.Fatal("rejected configured public origin")
	}
}
