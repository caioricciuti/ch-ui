package middleware

import (
	"net/http"

	"github.com/caioricciuti/ch-ui/internal/config"
)

// RequirePro returns a middleware that blocks access unless the installation
// has an active Pro license. Returns 402 Payment Required otherwise.
func RequirePro(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.IsPro() {
				writeJSON(w, http.StatusPaymentRequired, map[string]string{
					"error": "Pro license required",
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
