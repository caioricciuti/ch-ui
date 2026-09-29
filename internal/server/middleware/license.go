// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package middleware

import (
	"net/http"

	"github.com/caioricciuti/ch-ui/internal/config"
)

// RequirePro returns a middleware that gates access to Pro features.
//
//   - Active Pro license: full access.
//   - Expired but within the read-only grace window: safe (GET/HEAD) requests
//     are allowed so monitoring keeps working; mutating requests are blocked.
//     This prevents a license expiry from hard-locking an installation in the
//     middle of an incident.
//   - Otherwise: 402 Payment Required.
func RequirePro(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch cfg.ProAccess() {
			case config.ProActive:
				next.ServeHTTP(w, r)
			case config.ProGrace:
				w.Header().Set("X-CH-UI-License-Status", "grace")
				if r.Method == http.MethodGet || r.Method == http.MethodHead {
					next.ServeHTTP(w, r)
					return
				}
				writeJSON(w, http.StatusPaymentRequired, map[string]interface{}{
					"success": false,
					"error":   "Pro license expired — read-only grace period. Renew to restore write access to Pro features.",
					"status":  "grace",
				})
			default:
				writeError(w, http.StatusPaymentRequired, "Pro license required")
			}
		})
	}
}

// RequireProRead gates Pro routes that are reads even though they use POST
// (search and compare endpoints that carry a query body). They behave like
// GETs under RequirePro: any method is allowed with an active license or in
// the read-only grace window, and 402 otherwise. Use it only on routes that
// change nothing.
func RequireProRead(cfg *config.Config) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch cfg.ProAccess() {
			case config.ProActive:
				next.ServeHTTP(w, r)
			case config.ProGrace:
				w.Header().Set("X-CH-UI-License-Status", "grace")
				next.ServeHTTP(w, r)
			default:
				writeError(w, http.StatusPaymentRequired, "Pro license required")
			}
		})
	}
}
