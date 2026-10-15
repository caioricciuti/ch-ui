package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

// Lineage is a community feature: any signed-in role reaches the handler
// without a license (it then answers 503 here because no tunnel is online).
func TestLineageRoutesNeedSessionNotPro(t *testing.T) {
	db, err := dbtest.Open(t, "routes.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.CreateSession(database.CreateSessionParams{ConnectionID: conn, ClickhouseUser: "viewer", UserRole: "viewer", Token: "test-session", ExpiresAt: time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	if err != nil {
		t.Fatal(err)
	}
	s := New(&config.Config{DevMode: true, AppSecretKey: "test-only"}, db, nil, nil)
	defer s.gateway.Stop()
	for _, path := range []string{"/api/lineage", "/api/lineage?databases=a,b", "/api/lineage/node?id=a.b"} {
		for _, authenticated := range []bool{false, true} {
			req := httptest.NewRequest("GET", path, nil)
			want := http.StatusUnauthorized
			if authenticated {
				req.AddCookie(&http.Cookie{Name: "chui_session", Value: "test-session"})
				want = http.StatusServiceUnavailable
			}
			response := httptest.NewRecorder()
			s.router.ServeHTTP(response, req)
			if response.Code != want {
				t.Errorf("GET %s authenticated=%v: got %d, want %d: %s", path, authenticated, response.Code, want, response.Body.String())
			}
		}
	}
}
