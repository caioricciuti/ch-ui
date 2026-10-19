package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// BrowserRequestAllowed verifies origins for unsafe cookie-authenticated
// requests, including same-site sibling origins. Non-browser clients without
// browser headers remain supported. CORS headers alone do not prevent writes.
func BrowserRequestAllowed(cfg CORSConfig, r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		site := r.Header.Get("Sec-Fetch-Site")
		return site == "" || site == "none" || site == "same-origin"
	}
	if OriginAllowed(cfg, origin) {
		return true
	}
	scheme := ""
	if r.TLS != nil {
		scheme = "https"
	} else if FromTrustedProxy(r) && r.Header.Get("X-Forwarded-Proto") != "" {
		scheme = strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")))
	}
	// A TLS-terminating proxy may not have supplied forwarding headers. The
	// configured public scheme still governs requests for that public host.
	if u, err := url.Parse(cfg.AppURL); err == nil && strings.EqualFold(u.Host, r.Host) && (u.Scheme == "https" || u.Scheme == "http") {
		scheme = u.Scheme
	}
	if scheme == "" {
		// Plain HTTP with no trusted hint: a proxy we were not told about may
		// have terminated TLS, so the same host on either scheme is ours.
		return strings.EqualFold(origin, "http://"+r.Host) || strings.EqualFold(origin, "https://"+r.Host)
	}
	return strings.EqualFold(origin, scheme+"://"+r.Host)
}

// BrowserRequestProtection is installed on the browser API router; bearer-key
// webhooks and ingest receivers live outside that router.
func BrowserRequestProtection(cfg CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet, http.MethodHead, http.MethodOptions:
			default:
				if !BrowserRequestAllowed(cfg, r) {
					writeError(w, http.StatusForbidden, "Cross-origin request denied")
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
