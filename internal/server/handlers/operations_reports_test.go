package handlers

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestOperationsReportsRequireAdminAndScopeSavedReports(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	for _, id := range []string{"a", "b"} {
		if _, err = db.Conn().Exec(`INSERT INTO connections(id,name,tunnel_token) VALUES(?,?,?)`, id, id, id); err != nil {
			t.Fatal(err)
		}
	}
	_, err = db.SaveOperationsReport(database.OperationsReport{ConnectionID: "b", Body: "private-b", Payload: json.RawMessage(`{}`)}, "")
	if err != nil {
		t.Fatal(err)
	}
	h := (&OperationsReportsHandler{DB: db, Config: &config.Config{}}).Routes()
	for _, role := range []string{"", "viewer", "analyst", "admin"} {
		req := httptest.NewRequest("GET", "/", nil)
		if role != "" {
			req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: "a", UserRole: role}))
		}
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		want := 403
		if role == "admin" {
			want = 200
		}
		if res.Code != want {
			t.Fatalf("role %q got %d, want %d", role, res.Code, want)
		}
		if strings.Contains(res.Body.String(), "private-b") {
			t.Fatal("cross-connection report exposed")
		}
	}
}

func TestOperationsReportScheduleNeedsExplicitBackgroundAccount(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "reports.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err = db.Conn().Exec(`INSERT INTO connections(id,name,tunnel_token) VALUES('a','a','a')`); err != nil {
		t.Fatal(err)
	}
	h := (&OperationsReportsHandler{DB: db, Config: &config.Config{}}).Routes()
	req := httptest.NewRequest("PUT", "/settings", strings.NewReader(`{"enabled":true,"weekday":1,"hour":9,"recipients":[]}`))
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: "a", UserRole: "admin"}))
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != 400 || !strings.Contains(res.Body.String(), "dedicated") {
		t.Fatalf("unsafe schedule accepted: %d %s", res.Code, res.Body.String())
	}
}
