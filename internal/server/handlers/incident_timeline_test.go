// SPDX-License-Identifier: BUSL-1.1
package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/incidenttimeline"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
	"github.com/go-chi/chi/v5"
)

type timelineGatewayFixture struct {
	t          *testing.T
	connection string
	called     int
	response   json.RawMessage
	nilResult  bool
}

func (f *timelineGatewayFixture) IsTunnelOnline(connection string) bool {
	return connection == f.connection
}
func (f *timelineGatewayFixture) ExecuteQueryWithSettingsCtx(_ context.Context, connection, query, user, password string, settings map[string]string, _ time.Duration) (*tunnel.QueryResult, error) {
	f.called++
	if connection != f.connection || user != "caller_ch" || password != "caller_password" || settings["readonly"] != "1" || settings["max_execution_time"] != "10" || settings["max_memory_usage"] != "536870912" || settings["max_threads"] != "2" {
		f.t.Fatal("timeline must use caller credentials and readonly queries")
	}
	if strings.Contains(query, "system.part_log") {
		return nil, errors.New("UNKNOWN_TABLE")
	}
	if f.nilResult {
		return nil, nil
	}
	if f.response != nil {
		return &tunnel.QueryResult{Data: f.response}, nil
	}
	return &tunnel.QueryResult{Data: json.RawMessage(`[{"t":"2026-09-21T10:05:00Z","queries":38,"failures":1,"p95_ms":37,"max_ms":42}]`)}, nil
}

func TestIncidentTimelineMalformedResponseIsUnavailable(t *testing.T) {
	for _, raw := range []string{"nil", "null", `{"data":[]}`, `not-json`} {
		t.Run(raw, func(t *testing.T) {
			_, h, session, r := timelineFixture(t)
			h.Gateway = &timelineGatewayFixture{t: t, connection: session.ConnectionID, response: json.RawMessage(raw), nilResult: raw == "nil"}
			res := timelineRequest(r, session, "GET", "/timeline/"+timelineWindow, "")
			if res.Code != 200 {
				t.Fatal(res.Body.String())
			}
			var out struct {
				Coverage []incidenttimeline.Coverage `json:"coverage"`
			}
			if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
				t.Fatal(err)
			}
			for _, source := range out.Coverage {
				if source.Source == "queries" && source.Available {
					t.Fatalf("invalid %q response reported healthy empty", raw)
				}
			}
		})
	}
}

func timelineFixture(t *testing.T) (*database.DB, *IncidentTimelineHandler, *middleware.SessionInfo, chi.Router) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "timeline.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	for _, statement := range database.IncidentTimelineSchema {
		if _, err := db.Conn().Exec(statement); err != nil {
			t.Fatal(err)
		}
	}
	connection, err := db.CreateConnection(database.CreateConnectionParams{Name: "timeline", TunnelToken: "timeline"})
	if err != nil {
		t.Fatal(err)
	}
	password, err := crypto.Encrypt("caller_password", "test-key")
	if err != nil {
		t.Fatal(err)
	}
	h := &IncidentTimelineHandler{DB: db, Config: &config.Config{AppSecretKey: "test-key"}}
	session := &middleware.SessionInfo{ConnectionID: connection, UserRole: "admin", ClickhouseUser: "caller_ch", AuthSubject: "human@example.com", EncryptedPassword: password}
	r := chi.NewRouter()
	r.Route("/timeline", h.Routes)
	return db, h, session, r
}

func timelineRequest(r chi.Router, session *middleware.SessionInfo, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if session != nil {
		req = req.WithContext(middleware.SetSession(req.Context(), session))
	}
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)
	return rr
}

const timelineWindow = "?from=2026-09-21T10:00:00Z&to=2026-09-21T11:00:00Z"

func TestIncidentTimelineAuthorizationAndScope(t *testing.T) {
	db, _, session, r := timelineFixture(t)
	for _, role := range []string{"", "viewer", "analyst", "admin"} {
		var actor *middleware.SessionInfo
		if role != "" {
			copy := *session
			copy.UserRole = role
			actor = &copy
		}
		want := 403
		if role == "" {
			want = 401
		}
		if role == "admin" {
			want = 200
		}
		res := timelineRequest(r, actor, "GET", "/timeline/"+timelineWindow, "")
		if res.Code != want {
			t.Fatalf("role %q got %d want %d: %s", role, res.Code, want, res.Body)
		}
	}
	if res := timelineRequest(r, session, "GET", "/timeline/?from=2026-09-01T00:00:00Z&to=2026-09-21T00:00:00Z", ""); res.Code != 400 {
		t.Fatalf("unbounded window: %d", res.Code)
	}
	other, err := db.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "other"})
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Conn().Exec(`INSERT INTO gov_incidents (id,connection_id,source_type,title,severity,status,first_seen_at,last_seen_at) VALUES ('other-incident',?,'manual','private','warn','open','2026-09-21T10:00:00Z','2026-09-21T10:00:00Z')`, other)
	if err != nil {
		t.Fatal(err)
	}
	if res := timelineRequest(r, session, "GET", "/timeline/"+timelineWindow+"&incident_id=other-incident", ""); res.Code != 404 {
		t.Fatalf("other connection incident leaked: %d %s", res.Code, res.Body)
	}
}

func TestIncidentTimelineCoverageCredentialsAndOrder(t *testing.T) {
	db, h, session, r := timelineFixture(t)
	fixture := &timelineGatewayFixture{t: t, connection: session.ConnectionID}
	h.Gateway = fixture
	_, err := db.CreateIncidentAnnotation(context.Background(), database.IncidentAnnotation{ConnectionID: session.ConnectionID, OccurredAt: "2026-09-21T10:06:00Z", Title: "later deployment", CreatedBy: "author"})
	if err != nil {
		t.Fatal(err)
	}
	res := timelineRequest(r, session, "GET", "/timeline/"+timelineWindow, "")
	if res.Code != 200 {
		t.Fatal(res.Body.String())
	}
	var out struct {
		Events   []incidenttimeline.Event    `json:"events"`
		Coverage []incidenttimeline.Coverage `json:"coverage"`
	}
	if err := json.Unmarshal(res.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if fixture.called != 2 || len(out.Events) != 2 || out.Events[0].Source != "queries" || out.Events[1].Source != "deployments" {
		t.Fatalf("unexpected observations: %+v calls:%d", out.Events, fixture.called)
	}
	for _, c := range out.Coverage {
		if c.Source == "parts" && c.Available {
			t.Fatal("missing part_log must not look available")
		}
		if c.Source == "queries" && !c.Available {
			t.Fatal("query_log unnecessarily lost")
		}
	}
}

func TestIncidentAnnotationWriterIdentityAndDeleteIsolation(t *testing.T) {
	db, _, session, r := timelineFixture(t)
	body := `{"occurred_at":"2026-09-21T10:00:00+02:00","title":"Deployed API","details":"release 7","connection_id":"forged","created_by":"forged"}`
	viewer := *session
	viewer.UserRole = "viewer"
	if res := timelineRequest(r, &viewer, "POST", "/timeline/annotations", body); res.Code != 403 {
		t.Fatalf("viewer write: %d", res.Code)
	}
	session.UserRole = "analyst"
	res := timelineRequest(r, session, "POST", "/timeline/annotations", body)
	if res.Code != 201 {
		t.Fatal(res.Body.String())
	}
	var annotation database.IncidentAnnotation
	if err := json.Unmarshal(res.Body.Bytes(), &annotation); err != nil {
		t.Fatal(err)
	}
	if annotation.ConnectionID != session.ConnectionID || annotation.CreatedBy != "human@example.com" || annotation.OccurredAt != "2026-09-21T08:00:00Z" {
		t.Fatalf("forged identity/scope accepted: %+v", annotation)
	}
	other := *session
	other.AuthSubject = "another@example.com"
	if res := timelineRequest(r, &other, "DELETE", "/timeline/annotations/"+annotation.ID, ""); res.Code != 404 {
		t.Fatalf("another writer delete: %d", res.Code)
	}
	other.UserRole = "admin"
	other.ConnectionID = "another-connection"
	if res := timelineRequest(r, &other, "DELETE", "/timeline/annotations/"+annotation.ID, ""); res.Code != 404 {
		t.Fatalf("cross-connection delete: %d", res.Code)
	}
	if res := timelineRequest(r, session, "DELETE", "/timeline/annotations/"+annotation.ID, ""); res.Code != http.StatusOK {
		t.Fatalf("owner delete: %d", res.Code)
	}
	var actor string
	if err := db.Conn().QueryRow(`SELECT username FROM audit_logs WHERE action = 'incident.annotation.created'`).Scan(&actor); err != nil || actor != "human@example.com" {
		t.Fatalf("audit actor: %q %v", actor, err)
	}
}
