package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/license"
)

// GET /api/license needs no session: the customer and license id go only to
// a signed-in admin, the status to everyone.
func TestGetLicenseHidesCustomerFromNonAdmins(t *testing.T) {
	db, err := dbtest.Open(t, "license.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "t"})
	if err != nil {
		t.Fatal(err)
	}
	expires := time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
	for token, role := range map[string]string{"admin-sess": "admin", "analyst-sess": "analyst"} {
		if _, err := db.CreateSession(database.CreateSessionParams{
			ConnectionID: conn, ClickhouseUser: role, EncryptedPassword: "x", Token: token,
			ExpiresAt: expires, UserRole: role,
		}); err != nil {
			t.Fatal(err)
		}
	}
	h := &LicenseHandler{DB: db, Config: &config.Config{LicenseJSON: "signed"}, validate: func(string) *license.LicenseInfo {
		return &license.LicenseInfo{Edition: "pro", Valid: true, Customer: "Acme Corp", ExpiresAt: expires, LicenseID: "lic-123"}
	}}
	get := func(cookie string) (string, license.LicenseInfo) {
		req := httptest.NewRequest(http.MethodGet, "/api/license", nil)
		if cookie != "" {
			req.AddCookie(&http.Cookie{Name: SessionCookie, Value: cookie})
		}
		rec := httptest.NewRecorder()
		h.GetLicense(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("cookie %q: %d", cookie, rec.Code)
		}
		var info license.LicenseInfo
		_ = json.Unmarshal(rec.Body.Bytes(), &info)
		return rec.Body.String(), info
	}

	for _, cookie := range []string{"", "analyst-sess", "no-such-session"} {
		raw, info := get(cookie)
		if strings.Contains(raw, "Acme") || strings.Contains(raw, "lic-123") || strings.Contains(raw, "customer") || strings.Contains(raw, "license_id") {
			t.Fatalf("cookie %q got details: %s", cookie, raw)
		}
		if info.Edition != "pro" || !info.Valid || info.ExpiresAt != expires {
			t.Fatalf("cookie %q lost the status: %s", cookie, raw)
		}
	}
	if _, info := get("admin-sess"); info.Customer != "Acme Corp" || info.LicenseID != "lic-123" {
		t.Fatalf("admin did not get details: %+v", info)
	}
}
