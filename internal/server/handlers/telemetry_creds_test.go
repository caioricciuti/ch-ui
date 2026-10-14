package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// A stored password the server can no longer decrypt (the app secret changed)
// must not answer 401: the client treats 401 as an expired session, the login
// page sends a signed-in user straight back, and the page loops.
func TestTelemetryCredsStatus(t *testing.T) {
	enc, err := crypto.Encrypt("pw", "old-secret-old-secret-old-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &TelemetryHandler{Config: &config.Config{AppSecretKey: "new-secret-new-secret-new-secret"}}

	cases := []struct {
		name    string
		session *middleware.SessionInfo
		want    int
	}{
		{"no session", nil, http.StatusUnauthorized},
		{"undecryptable password", &middleware.SessionInfo{EncryptedPassword: enc}, http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := httptest.NewRequest(http.MethodGet, "/api/telemetry/service-map", nil)
			if tc.session != nil {
				r = r.WithContext(middleware.SetSession(r.Context(), tc.session))
			}
			w := httptest.NewRecorder()
			h.ServiceMap(w, r)
			if w.Code != tc.want {
				t.Fatalf("status = %d, want %d (body %s)", w.Code, tc.want, w.Body.String())
			}
		})
	}
}
