package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/caioricciuti/ch-ui/internal/version"
)

type HealthHandler struct{}

func (h *HealthHandler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"status":    "ok",
		"service":   "ch-ui",
		"version":   version.Version,
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	})
}
