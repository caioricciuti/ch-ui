// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/go-chi/chi/v5"
)

func TestScheduleRoutesRequireWriter(t *testing.T) {
	db, err := dbtest.Open(t, "schedules.db")
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
	db, err := dbtest.Open(t, "shared.db")
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
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ConnectionID: conn, ClickhouseUser: role}))
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

// A job's next run is the cron read in its timezone, and an unknown timezone
// is rejected on create and update.
func TestScheduleTimezone(t *testing.T) {
	db, err := dbtest.Open(t, "tz.db")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "tz-token"})
	if err != nil {
		t.Fatal(err)
	}
	query, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "q", Query: "SELECT 1", ConnectionID: conn})
	if err != nil {
		t.Fatal(err)
	}
	h := &SchedulesHandler{DB: db}
	r := chi.NewRouter()
	r.Route("/schedules", h.Routes)
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "admin", ConnectionID: conn, ClickhouseUser: "admin"}))
		rr := httptest.NewRecorder()
		r.ServeHTTP(rr, req)
		return rr
	}
	nextRunIn := func(id, tz string) string {
		t.Helper()
		s, err := db.GetScheduleByID(id)
		if err != nil || s == nil || s.NextRunAt == nil {
			t.Fatalf("schedule: %v %v", s, err)
		}
		next, err := time.Parse(time.RFC3339, *s.NextRunAt)
		if err != nil {
			t.Fatal(err)
		}
		loc, err := time.LoadLocation(tz)
		if err != nil {
			t.Fatal(err)
		}
		return next.In(loc).Format("15:04")
	}

	base := `"name":"job","cron":"0 9 * * *","saved_query_id":"` + query + `"`
	if rr := do("POST", "/schedules/", `{`+base+`,"timezone":"Not/AZone"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown timezone on create: %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("POST", "/schedules/", `{`+base+`,"timezone":"Europe/Madrid"}`); rr.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rr.Code, rr.Body.String())
	}
	jobs, err := db.GetSchedules()
	if err != nil || len(jobs) != 1 {
		t.Fatalf("jobs: %d %v", len(jobs), err)
	}
	id := jobs[0].ID
	if got := nextRunIn(id, "Europe/Madrid"); got != "09:00" {
		t.Errorf("create: next run %s Madrid time, want 09:00", got)
	}

	if rr := do("PUT", "/schedules/"+id, `{"timezone":"Not/AZone"}`); rr.Code != http.StatusBadRequest {
		t.Fatalf("unknown timezone on update: %d %s", rr.Code, rr.Body.String())
	}
	if rr := do("PUT", "/schedules/"+id, `{"timezone":"America/New_York"}`); rr.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rr.Code, rr.Body.String())
	}
	if got := nextRunIn(id, "America/New_York"); got != "09:00" {
		t.Errorf("update: next run %s New York time, want 09:00", got)
	}
}
