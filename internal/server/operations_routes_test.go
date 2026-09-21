package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestOperationsRoutesRequireSessionAndPro(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "routes.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "admin", UserRole: "admin", Token: "test-session", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	s := New(&config.Config{DevMode: true, AppSecretKey: "test-only"}, db, nil, nil)
	defer s.gateway.Stop()
	for _, endpoint := range []struct{ method, path string }{
		{"GET", "/api/performance/regressions"}, {"GET", "/api/performance/investigations"}, {"PUT", "/api/performance/monitor"},
		{"GET", "/api/fleet/"}, {"POST", "/api/schema-compare/compare"},
		{"GET", "/api/operations-reports/"}, {"POST", "/api/operations-reports/generate"},
		{"GET", "/api/incident-timeline/"}, {"POST", "/api/incident-timeline/annotations"},
	} {
		for _, authenticated := range []bool{false, true} {
			req := httptest.NewRequest(endpoint.method, endpoint.path, nil)
			want := http.StatusUnauthorized
			if authenticated {
				req.AddCookie(&http.Cookie{Name: "chui_session", Value: "test-session"})
				want = http.StatusPaymentRequired
			}
			response := httptest.NewRecorder()
			s.router.ServeHTTP(response, req)
			if response.Code != want {
				t.Errorf("%s %s authenticated=%v: got %d, want %d: %s", endpoint.method, endpoint.path, authenticated, response.Code, want, response.Body.String())
			}
		}
	}
}
