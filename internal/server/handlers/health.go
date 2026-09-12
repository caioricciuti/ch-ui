package handlers

import (
	"encoding/json"
	"net/http"
	"time"
)

type HealthHandler struct{}

// Health answers liveness probes. It is deliberately unauthenticated, which
// is why it carries no build information: a version string on a public
// endpoint tells anyone scanning for hosts exactly which advisories to try.
// Signed-in callers get the version as appVersion from the session
// endpoints, and operators have `ch-ui version` on the host.
func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"service":   "ch-ui",
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
