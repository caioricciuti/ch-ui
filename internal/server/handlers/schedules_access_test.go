package handlers

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

func TestScheduleRoutesRequireWriter(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "schedules.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	h := &SchedulesHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/schedules", h.Routes)
	for _, role := range []string{"", "viewer", "analyst", "admin"} {
		for _, route := range []struct {
			method, path string
			allowed      int
		}{
			{"POST", "/schedules/", 400}, {"PUT", "/schedules/missing", 404},
			{"DELETE", "/schedules/missing", 404}, {"POST", "/schedules/missing/run", 404},
		} {
			req := httptest.NewRequest(route.method, route.path, strings.NewReader(`{}`))
			if role != "" {
				req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ConnectionID: "connection-a"}))
			}
			rr := httptest.NewRecorder()
			r.ServeHTTP(rr, req)
			want := http.StatusForbidden
			if role == "admin" || role == "analyst" {
				want = route.allowed
			}
			if rr.Code != want {
				t.Fatalf("%s %s %s: got %d want %d", role, route.method, route.path, rr.Code, want)
			}
		}
	}
	req := httptest.NewRequest("GET", "/schedules/", nil)
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "viewer"}))
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	if rr.Code != 200 {
		t.Fatalf("viewer read: %d", rr.Code)
	}
}

// The writer role is instance-wide, as with shared pipelines and saved queries.
// Account configuration is an explicit delegation to those writers.
func TestScheduleSharedWorkspaceDelegation(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "shared.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other-token"})
	if err != nil {
		t.Fatal(err)
	}
	query, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "shared", Query: "SELECT 1", ConnectionID: conn})
	if err != nil {
		t.Fatal(err)
	}
	h := &SchedulesHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/schedules", h.Routes)
	body := `{"name":"job","cron":"* * * * *","connection_id":"` + conn + `","saved_query_id":"` + query + `"}`
	for _, role := range []string{"viewer", "analyst", "admin"} {
		req := httptest.NewRequest("POST", "/schedules/", strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ConnectionID: "different-connection", ClickhouseUser: role}))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		want := 201
		if role == "viewer" {
			want = 403
		}
		if rr.Code != want {
			t.Fatalf("%s: %d %s", role, rr.Code, rr.Body.String())
		}
	}
	jobs, err := db.GetSchedules()
	if err != nil || len(jobs) != 2 {
		t.Fatalf("jobs: %d %v", len(jobs), err)
	}
}
