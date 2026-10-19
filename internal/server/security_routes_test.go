package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

func TestSecurityBrowserAPIRoutes(t *testing.T) {
	db, err := dbtest.Open(t, "csrf-routes.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "admin", UserRole: "admin", Token: "admin-session", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	s := New(&config.Config{AppURL: "https://app.example", AppSecretKey: "test-only"}, db, nil, nil)
	defer s.gateway.Stop()
	for _, tc := range []struct {
		path, origin, contentType string
		want                      int
	}{
		{"/api/license/deactivate", "https://evil.example", "application/json", 403},
		{"/api/license/deactivate", "https://sibling.example", "text/plain", 403},
		{"/api/license/deactivate", "https://app.example", "application/json", 200},
		{"/api/auth/login", "https://evil.example", "text/plain", 403},
		{"/api/auth/login", "https://app.example", "text/plain", 415},
		// Bearer-token webhook routes do not inherit the browser API middleware.
		{"/api/pipelines/webhook/missing", "https://external.example", "application/json", 404},
	} {
		r := httptest.NewRequest(http.MethodPost, "https://app.example"+tc.path, strings.NewReader(`{}`))
		r.Header.Set("Origin", tc.origin)
		r.Header.Set("Content-Type", tc.contentType)
		r.AddCookie(&http.Cookie{Name: "chui_session", Value: "admin-session"})
		w := httptest.NewRecorder()
		s.router.ServeHTTP(w, r)
		if w.Code != tc.want {
			t.Errorf("%s from %s: %d want %d: %s", tc.path, tc.origin, w.Code, tc.want, w.Body.String())
		}
	}
}
