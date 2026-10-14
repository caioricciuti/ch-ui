package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func pipelineRequest(router http.Handler, connectionID, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: "analyst", ClickhouseUser: "analyst", ConnectionID: connectionID}))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

func TestPipelinesStayOnTheSessionConnection(t *testing.T) {
	db, err := dbtest.Open(t, "pipelines.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connA, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	connB, err := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"})
	if err != nil {
		t.Fatal(err)
	}
	mine, err := db.CreatePipeline("mine", "", connA, "analyst")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := db.CreatePipeline("theirs", "", connB, "someone")
	if err != nil {
		t.Fatal(err)
	}
	theirRun, err := db.CreatePipelineRun(theirs, "success")
	if err != nil {
		t.Fatal(err)
	}

	router := (&PipelinesHandler{DB: db}).Routes()
	as := func(method, path, body string) int {
		return pipelineRequest(router, connA, method, path, body).Code
	}

	t.Run("list shows only this connection", func(t *testing.T) {
		rec := pipelineRequest(router, connA, http.MethodGet, "/", "")
		var resp struct {
			Pipelines []database.Pipeline `json:"pipelines"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		if len(resp.Pipelines) != 1 || resp.Pipelines[0].ID != mine {
			t.Fatalf("want only %s, got %+v", mine, resp.Pipelines)
		}
	})

	t.Run("another connection's pipeline is not found", func(t *testing.T) {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/" + theirs, ""},
			{http.MethodPut, "/" + theirs, `{"name":"x"}`},
			{http.MethodDelete, "/" + theirs, ""},
			{http.MethodPut, "/" + theirs + "/graph", `{"nodes":[],"edges":[]}`},
			{http.MethodPost, "/" + theirs + "/start", ""},
			{http.MethodPost, "/" + theirs + "/stop", ""},
			{http.MethodGet, "/" + theirs + "/status", ""},
			{http.MethodGet, "/" + theirs + "/runs", ""},
			{http.MethodGet, "/" + theirs + "/runs/" + theirRun + "/logs", ""},
			{http.MethodPost, "/" + theirs + "/test", ""},
			{http.MethodPost, "/" + theirs + "/reset-position", ""},
			{http.MethodGet, "/" + theirs + "/checkpoints", ""},
		} {
			if got := as(c.method, c.path, c.body); got != http.StatusNotFound {
				t.Errorf("%s %s: status %d, want 404", c.method, c.path, got)
			}
		}
		if p, _ := db.GetPipelineByID(theirs); p == nil || p.Name != "theirs" {
			t.Fatal("the other connection's pipeline was changed or deleted")
		}
	})

	t.Run("run logs need a run of the named pipeline", func(t *testing.T) {
		if got := as(http.MethodGet, "/"+mine+"/runs/"+theirRun+"/logs", ""); got != http.StatusNotFound {
			t.Fatalf("status %d, want 404", got)
		}
	})

	t.Run("create only on the session connection", func(t *testing.T) {
		if got := as(http.MethodPost, "/", `{"name":"x","connection_id":"`+connB+`"}`); got != http.StatusForbidden {
			t.Fatalf("create on another connection: status %d, want 403", got)
		}
		rec := pipelineRequest(router, connA, http.MethodPost, "/", `{"name":"y"}`)
		var resp struct {
			Pipeline database.Pipeline `json:"pipeline"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusCreated {
			t.Fatalf("create: status %d, %v: %s", rec.Code, err, rec.Body.String())
		}
		if resp.Pipeline.ConnectionID != connA {
			t.Fatalf("created on %s, want %s", resp.Pipeline.ConnectionID, connA)
		}
	})
}
