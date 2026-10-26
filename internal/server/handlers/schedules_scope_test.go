// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Jobs run with their connection's credentials. A job on another connection
// is not found, and a manual run sends the caller's password only to the
// connection the caller signed in to.
func TestSchedulesStayOnTheSessionConnection(t *testing.T) {
	db, connA := testutil.WorkerDB(t)
	connB, err := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b-token"})
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	deadline := time.Now().Add(5 * time.Second)
	for !agent.Gateway.IsTunnelOnline(connA) {
		if time.Now().After(deadline) {
			t.Fatal("agent never came online")
		}
		time.Sleep(10 * time.Millisecond)
	}

	queryA, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "a", Query: "SELECT 'a'", ConnectionID: connA})
	if err != nil {
		t.Fatal(err)
	}
	queryB, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "b", Query: "SELECT 'b'", ConnectionID: connB})
	if err != nil {
		t.Fatal(err)
	}
	queryNone, err := db.CreateSavedQuery(database.CreateSavedQueryParams{Name: "none", Query: "SELECT 'none'"})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := db.CreateSchedule("mine", queryA, connA, "0 * * * *", "UTC", "alice", 60000)
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := db.CreateSchedule("theirs", queryA, connB, "0 * * * *", "UTC", "bob", 60000)
	if err != nil {
		t.Fatal(err)
	}
	// No connection of its own: the scheduler runs it on its query's.
	viaQuery, err := db.CreateSchedule("via-query", queryB, "", "0 * * * *", "UTC", "bob", 60000)
	if err != nil {
		t.Fatal(err)
	}
	// No connection anywhere: it falls back to the caller's, as over MCP.
	orphan, err := db.CreateSchedule("orphan", queryNone, "", "0 * * * *", "UTC", "bob", 60000)
	if err != nil {
		t.Fatal(err)
	}

	encrypted, err := crypto.Encrypt("alice-password", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &SchedulesHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}}
	r := chi.NewRouter()
	r.Route("/schedules", h.Routes)
	as := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			UserRole: "analyst", ClickhouseUser: "alice", EncryptedPassword: encrypted, ConnectionID: connA,
		}))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec
	}

	t.Run("list shows only the session connection's jobs", func(t *testing.T) {
		rec := as(http.MethodGet, "/schedules/", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
		}
		var out struct {
			Schedules []database.Schedule `json:"schedules"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		got := map[string]bool{}
		for _, s := range out.Schedules {
			got[s.ID] = true
		}
		if len(got) != 2 || !got[mine] || !got[orphan] {
			t.Fatalf("listed %v, want mine and orphan only", got)
		}
	})

	t.Run("another connection's job is not found", func(t *testing.T) {
		for _, id := range []string{theirs, viaQuery} {
			for _, c := range []struct{ method, path, body string }{
				{http.MethodGet, "/schedules/" + id, ""},
				{http.MethodPut, "/schedules/" + id, `{"name":"renamed"}`},
				{http.MethodDelete, "/schedules/" + id, ""},
				{http.MethodGet, "/schedules/" + id + "/runs", ""},
				{http.MethodPost, "/schedules/" + id + "/run", ""},
			} {
				if rec := as(c.method, c.path, c.body); rec.Code != http.StatusNotFound {
					t.Fatalf("%s %s: %d %s, want 404", c.method, c.path, rec.Code, rec.Body.String())
				}
			}
			if s, _ := db.GetScheduleByID(id); s == nil || s.Name == "renamed" {
				t.Fatalf("job %s changed or deleted from another connection: %+v", id, s)
			}
			if runs, _ := db.GetScheduleRuns(id, 10, 0); len(runs) != 0 {
				t.Fatalf("job %s ran from another connection: %+v", id, runs)
			}
		}
	})

	t.Run("create and update refuse another connection", func(t *testing.T) {
		body := `{"name":"x","cron":"0 * * * *","saved_query_id":"` + queryA + `","connection_id":"` + connB + `"}`
		if rec := as(http.MethodPost, "/schedules/", body); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "Switch to the other connection") {
			t.Fatalf("create on another connection: %d %s, want 400", rec.Code, rec.Body.String())
		}
		if rec := as(http.MethodPut, "/schedules/"+mine, `{"name":"x","connection_id":"`+connB+`"}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("update to another connection: %d %s, want 400", rec.Code, rec.Body.String())
		}
		if s, _ := db.GetScheduleByID(mine); s == nil || s.Name != "mine" || s.ConnectionID == nil || *s.ConnectionID != connA {
			t.Fatalf("refused update changed the job: %+v", s)
		}
		// The saved query's connection does not decide where a new job runs.
		rec := as(http.MethodPost, "/schedules/", `{"name":"from-b","cron":"0 * * * *","saved_query_id":"`+queryB+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
		}
		var created database.Schedule
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatal(err)
		}
		if created.ConnectionID == nil || *created.ConnectionID != connA {
			t.Fatalf("new job on %v, want the session connection %s", created.ConnectionID, connA)
		}
		rec = as(http.MethodPost, "/schedules/", `{"name":"explicit","cron":"0 * * * *","saved_query_id":"`+queryA+`","connection_id":"`+connA+`"}`)
		if rec.Code != http.StatusCreated {
			t.Fatalf("create naming the session connection: %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("manual run uses the session connection", func(t *testing.T) {
		rec := as(http.MethodPost, "/schedules/"+mine+"/run", "")
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"success":true`) {
			t.Fatalf("run: %d %s", rec.Code, rec.Body.String())
		}
		var sent []tunnel.GatewayMessage
		for _, m := range agent.Messages() {
			if strings.Contains(m.SQL, "SELECT 'a'") {
				sent = append(sent, m)
			}
		}
		if len(sent) != 1 || sent[0].User != "alice" || sent[0].Password != "alice-password" {
			t.Fatalf("queries sent: %+v", sent)
		}
		a := findAudit(auditActions(t, db), "schedule.manual_run", "status=success")
		if a == nil || a.ConnectionID == nil || *a.ConnectionID != connA {
			t.Fatalf("manual run audit: %+v", a)
		}
	})
}
