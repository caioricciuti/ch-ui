package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

// A login that ends before the credential test (here: no connection exists)
// must give its rate-limit slot back, or a burst of such requests from one IP
// would lock that IP out for the reservation lifetime.
func TestLoginEarlyReturnReleasesRateLimitSlot(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "login.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	h := &AuthHandler{DB: db, RateLimiter: middleware.NewRateLimiter(db), Config: &config.Config{AppSecretKey: "test-secret"}}

	for i := 0; i < MaxAttemptsPerIP*3; i++ {
		req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"u","password":"p"}`))
		req.RemoteAddr = "203.0.113.7:4000"
		rec := httptest.NewRecorder()
		h.Login(rec, req)
		if rec.Code == http.StatusTooManyRequests {
			t.Fatalf("attempt %d was rate limited; early returns must release their slot", i+1)
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("attempt %d: status %d, want 400 (no connections): %s", i+1, rec.Code, rec.Body.String())
		}
	}
}
