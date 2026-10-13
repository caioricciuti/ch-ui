package middleware

import (
	"net/http"
	"net/url"
	"strings"
)

// CORSConfig holds CORS configuration.
type CORSConfig struct {
	AllowedOrigins []string
	AppURL         string
	DevMode        bool
}

// OriginAllowed reports whether origin may make credentialed requests: any
// localhost origin in dev mode, an explicitly allowed origin, or the origin of
// APP_URL. CORS uses it for its headers; state-changing endpoints that must
// refuse cross-site callers use it too.
func OriginAllowed(cfg CORSConfig, origin string) bool {
	if origin == "" {
		return false
	}
	// In dev mode, allow any localhost origin
	if cfg.DevMode {
		if strings.HasPrefix(origin, "http://localhost:") ||
			strings.HasPrefix(origin, "http://127.0.0.1:") {
			return true
		}
	}
	// Check explicit allowed origins
	for _, o := range cfg.AllowedOrigins {
		if o == origin {
			return true
		}
	}
	// Check against APP_URL origin
	if cfg.AppURL != "" {
		if u, err := url.Parse(cfg.AppURL); err == nil && u.Host != "" {
			appOrigin := u.Scheme + "://" + u.Host
			if strings.TrimSuffix(origin, "/") == appOrigin {
				return true
			}
		}
	}
	return false
}

// CORS returns a middleware that handles CORS headers.
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if OriginAllowed(cfg, origin) {
				w.Header().Set("Access-Control-Allow-Origin", origin)
				w.Header().Set("Access-Control-Allow-Credentials", "true")
				w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
				w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie, X-CHUI-Connection")
				w.Header().Set("Vary", "Origin")
			}

			// Handle preflight
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
