package handlers

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestModelsStayOnTheSessionConnection(t *testing.T) {
	db, err := database.Open(filepath.Join(t.TempDir(), "models.db"))
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
	mine, err := db.CreateModel(connA, "mine", "", "default", "view", "SELECT 1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	downstream, err := db.CreateModel(connA, "downstream", "", "default", "view", "SELECT * FROM $ref(mine)", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	theirs, err := db.CreateModel(connB, "theirs", "", "default", "view", "SELECT 1", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	theirRun, err := db.CreateModelRun(connB, 1, "someone")
	if err != nil {
		t.Fatal(err)
	}

	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	router := (&ModelsHandler{DB: db, Gateway: gw, Runner: models.NewRunner(db, gw, "secret")}).Routes()
	as := func(method, path, body string) int {
		return pipelineRequest(router, connA, method, path, body).Code
	}

	t.Run("another connection's model is not found", func(t *testing.T) {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/" + theirs, ""},
			{http.MethodPut, "/" + theirs, `{"sql_body":"SELECT 2"}`},
			{http.MethodDelete, "/" + theirs, ""},
			{http.MethodPost, "/" + theirs + "/run", ""},
			{http.MethodGet, "/runs/" + theirRun, ""},
			{http.MethodPost, "/pipelines/" + theirs + "/run", ""},
			{http.MethodPut, "/schedule/" + theirs, `{"cron":"0 * * * *"}`},
			{http.MethodPost, "/no-such-model/run", ""},
		} {
			if got := as(c.method, c.path, c.body); got != http.StatusNotFound {
				t.Errorf("%s %s: status %d, want 404", c.method, c.path, got)
			}
		}
		if m, _ := db.GetModelByID(theirs); m == nil || m.SQLBody != "SELECT 1" {
			t.Fatal("the other connection's model was changed or deleted")
		}
		if s, _ := db.GetModelScheduleByAnchor(connA, theirs); s != nil {
			t.Fatal("a schedule was created on another connection's model")
		}
	})

	t.Run("materialization is validated", func(t *testing.T) {
		if got := as(http.MethodPost, "/", `{"name":"inc","sql_body":"SELECT 1","materialization":"incremental"}`); got != http.StatusBadRequest {
			t.Fatalf("create: status %d, want 400", got)
		}
		if got := as(http.MethodPut, "/"+mine, `{"materialization":"materialized_view"}`); got != http.StatusBadRequest {
			t.Fatalf("update: status %d, want 400", got)
		}
		rec := pipelineRequest(router, connA, http.MethodPost, "/", `{"name":"tbl","sql_body":"SELECT 1","materialization":"table","table_engine":"Memory"}`)
		var resp struct {
			Model database.Model `json:"model"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusCreated {
			t.Fatalf("create table: %d %s", rec.Code, rec.Body.String())
		}
		if resp.Model.OrderBy != "" {
			t.Fatalf("Memory takes no ORDER BY, stored %q", resp.Model.OrderBy)
		}
	})

	t.Run("schedules pause, resume and report missing deletes", func(t *testing.T) {
		if got := as(http.MethodDelete, "/schedule/"+mine, ""); got != http.StatusNotFound {
			t.Fatalf("delete without a schedule: status %d, want 404", got)
		}
		enabled := func() bool {
			s, err := db.GetModelScheduleByAnchor(connA, mine)
			if err != nil || s == nil {
				t.Fatalf("schedule: %v %v", s, err)
			}
			return s.Enabled
		}
		if got := as(http.MethodPut, "/schedule/"+mine, `{"cron":"0 * * * *","enabled":false}`); got != http.StatusOK {
			t.Fatalf("pause: status %d", got)
		}
		if enabled() {
			t.Fatal("enabled=false must pause the schedule")
		}
		if got := as(http.MethodPut, "/schedule/"+mine, `{"cron":"0 2 * * *"}`); got != http.StatusOK || enabled() {
			t.Fatalf("a cron edit without enabled must keep the schedule paused (status %d)", got)
		}
		if got := as(http.MethodPut, "/schedule/"+mine, `{"cron":"0 2 * * *","enabled":true}`); got != http.StatusOK || !enabled() {
			t.Fatalf("resume failed (status %d)", got)
		}
		if got := as(http.MethodDelete, "/schedule/"+mine, ""); got != http.StatusOK {
			t.Fatalf("delete: status %d", got)
		}
	})

	t.Run("pipelines report the stored schedule's anchor", func(t *testing.T) {
		// A schedule stored on a model that is not first in the component
		// (the anchor moved after models were added).
		if _, err := db.UpsertModelSchedule(connA, downstream, "0 * * * *", "", ""); err != nil {
			t.Fatal(err)
		}
		rec := pipelineRequest(router, connA, http.MethodGet, "/pipelines", "")
		var resp struct {
			Pipelines []struct {
				AnchorModelID string                  `json:"anchor_model_id"`
				ModelIDs      []string                `json:"model_ids"`
				Schedule      *database.ModelSchedule `json:"schedule"`
			} `json:"pipelines"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, p := range resp.Pipelines {
			if p.Schedule != nil {
				found = true
				if p.AnchorModelID != downstream || p.ModelIDs[0] != mine {
					t.Fatalf("anchor %s, want the stored %s (components %v)", p.AnchorModelID, downstream, p.ModelIDs)
				}
			}
		}
		if !found {
			t.Fatal("scheduled pipeline missing")
		}
		if got := as(http.MethodDelete, "/schedule/"+downstream, ""); got != http.StatusOK {
			t.Fatalf("delete through the reported anchor: status %d", got)
		}
	})
}
