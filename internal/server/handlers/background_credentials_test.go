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
	"github.com/go-chi/chi/v5"
)

func TestBackgroundAccountAdminRoutes(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "accounts.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-token"})
	if err != nil {
		t.Fatal(err)
	}
	h := &ConnectionsHandler{DB: db, Config: &config.Config{AppSecretKey: "secret"}}
	r := chi.NewRouter()
	r.Route("/connections/{id}/background-credentials", h.BackgroundCredentialRoutes)
	call := func(role, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, "/connections/"+conn+"/background-credentials"+path, strings.NewReader(body))
		if role != "" {
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: "shared-sso", AuthSubject: "operator@example.test"}))
		}
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	for _, role := range []string{"", "viewer", "analyst"} {
		for _, method := range []string{http.MethodGet, http.MethodPut} {
			path := "/"
			if method == http.MethodPut {
				path = "/schedule"
			}
			if rr := call(role, method, path, `{"mode":"disabled"}`); rr.Code != http.StatusForbidden {
				t.Fatalf("%s %s: %d", role, method, rr.Code)
			}
		}
	}
	if rr := call("admin", "GET", "/", ""); rr.Code != http.StatusOK || strings.Contains(rr.Body.String(), "password") {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}
	for _, body := range []string{`{"mode":"invalid"}`, `{"mode":"service_account","username":"worker"}`, `{"mode":"disabled","unexpected":true}`} {
		if rr := call("admin", "PUT", "/schedule", body); rr.Code != http.StatusBadRequest {
			t.Fatalf("invalid input: %d", rr.Code)
		}
	}
	if rr := call("admin", "PUT", "/unknown", `{"mode":"disabled"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown worker: %d", rr.Code)
	}
	if rr := call("admin", "PUT", "/schedule", `{"mode":"service_account","username":"worker","password":"test"}`); rr.Code != http.StatusServiceUnavailable {
		t.Fatalf("offline verification: %d", rr.Code)
	}
	for _, mode := range []string{"disabled", "session"} {
		if rr := call("admin", "PUT", "/schedule", `{"mode":"`+mode+`"}`); rr.Code != http.StatusOK {
			t.Fatalf("save %s: %d %s", mode, rr.Code, rr.Body.String())
		}
	}
	logs, err := db.GetAuditLogs(10)
	if err != nil || len(logs) != 2 {
		t.Fatalf("audit: %d %v", len(logs), err)
	}
	if logs[0].Username == nil || *logs[0].Username != "operator@example.test" {
		t.Fatal("audit must identify the human administrator")
	}
}
