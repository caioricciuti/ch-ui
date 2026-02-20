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

// CORS returns a middleware that handles CORS headers.
func CORS(cfg CORSConfig) func(http.Handler) http.Handler {
	var appOrigin string
	if cfg.AppURL != "" {
		if u, err := url.Parse(cfg.AppURL); err == nil {
			appOrigin = u.Scheme + "://" + u.Host
		}
	}

	allowedSet := make(map[string]bool, len(cfg.AllowedOrigins))
	for _, o := range cfg.AllowedOrigins {
		allowedSet[o] = true
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			origin := r.Header.Get("Origin")

			if origin != "" {
				allowed := false

				// In dev mode, allow any localhost origin
				if cfg.DevMode {
					if strings.HasPrefix(origin, "http://localhost:") ||
						strings.HasPrefix(origin, "http://127.0.0.1:") {
						allowed = true
					}
				}

				// Check explicit allowed origins
				if !allowed && allowedSet[origin] {
					allowed = true
				}

				// Check against APP_URL origin
				if !allowed && appOrigin != "" {
					cleaned := strings.TrimSuffix(origin, "/")
					if cleaned == appOrigin || cleaned == strings.TrimSuffix(appOrigin, "/") {
						allowed = true
					}
				}

				if allowed {
					w.Header().Set("Access-Control-Allow-Origin", origin)
					w.Header().Set("Access-Control-Allow-Credentials", "true")
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, Cookie")
					w.Header().Set("Vary", "Origin")
				}
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
