package middleware

import "net/http"

// SecurityHeaders adds security headers to responses.
func SecurityHeaders(isProduction bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("X-Frame-Options", "DENY")
			w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			w.Header().Set("Permissions-Policy", "geolocation=(), microphone=(), camera=()")

			if isProduction {
				w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
				// CSP allows 'unsafe-eval' for Monaco editor
				w.Header().Set("Content-Security-Policy",
					"default-src 'self'; script-src 'self' 'unsafe-inline' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; connect-src 'self' https://api.openai.com; font-src 'self' data:;")
			}

			next.ServeHTTP(w, r)
		})
	}
}
