package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestHealthOmitsVersion pins the contract of the unauthenticated health
// endpoint: enough for a liveness probe, nothing that helps someone scanning
// the internet pick an exploit. The version stays available to signed-in
// callers through the session endpoints.
func TestHealthOmitsVersion(t *testing.T) {
	rr := httptest.NewRecorder()
	(&HealthHandler{}).Health(rr, httptest.NewRequest(http.MethodGet, "/health", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rr.Code, http.StatusOK)
	}

	var body map[string]string
	if err := json.NewDecoder(rr.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if v, ok := body["version"]; ok {
		t.Errorf("unauthenticated /health disclosed the version: %q", v)
	}
	if _, ok := body["commit"]; ok {
		t.Errorf("unauthenticated /health disclosed the commit")
	}

	if body["status"] != "ok" {
		t.Errorf("status = %q, want ok", body["status"])
	}
	if body["service"] != "ch-ui" {
		t.Errorf("service = %q, want ch-ui", body["service"])
	}
	if body["timestamp"] == "" {
		t.Error("timestamp is empty; probes and log correlation rely on it")
	}
}
