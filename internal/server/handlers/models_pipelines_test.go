package handlers

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Pipelines select models by name: a rename must carry the selector along,
// and a delete must show up as a selector error on the pipeline.
func TestModelRenameAndDeleteKeepPipelinesHonest(t *testing.T) {
	db, err := dbtest.Open(t, "models.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	if err != nil {
		t.Fatal(err)
	}
	gw := tunnel.NewGateway(db)
	t.Cleanup(gw.Stop)
	router := (&ModelsHandler{DB: db, Gateway: gw, Runner: models.NewRunner(db, gw, "secret")}).Routes()

	if _, err := db.CreateModel(conn, "raw", "", "default", "view", "SELECT 1 AS id", "", "", ""); err != nil {
		t.Fatal(err)
	}
	orders, err := db.CreateModel(conn, "orders", "", "default", "view", "SELECT * FROM $ref(raw)", "", "", "")
	if err != nil {
		t.Fatal(err)
	}
	pid, err := db.CreateModelPipeline(database.ModelPipeline{ConnectionID: conn, Name: "nightly", Selector: "raw, +orders+"})
	if err != nil {
		t.Fatal(err)
	}

	if rec := pipelineRequest(router, conn, http.MethodPut, "/"+orders, `{"name":"sales"}`); rec.Code != http.StatusOK {
		t.Fatalf("rename: %d %s", rec.Code, rec.Body.String())
	}
	if p, _ := db.GetModelPipelineByID(pid); p == nil || p.Selector != "raw,+sales+" {
		t.Fatalf("the rename must rewrite the selector, got %+v", p)
	}

	type listResp struct {
		Pipelines []struct {
			ID            string   `json:"id"`
			ModelIDs      []string `json:"model_ids"`
			SelectorError string   `json:"selector_error"`
		} `json:"pipelines"`
	}
	list := func() listResp {
		t.Helper()
		rec := pipelineRequest(router, conn, http.MethodGet, "/pipelines", "")
		var l listResp
		if err := json.Unmarshal(rec.Body.Bytes(), &l); err != nil || rec.Code != http.StatusOK || len(l.Pipelines) != 1 {
			t.Fatalf("list: %d %s", rec.Code, rec.Body.String())
		}
		return l
	}
	if l := list(); l.Pipelines[0].SelectorError != "" || len(l.Pipelines[0].ModelIDs) != 2 {
		t.Fatalf("renamed pipeline must still resolve: %+v", l.Pipelines[0])
	}

	if rec := pipelineRequest(router, conn, http.MethodDelete, "/"+orders, ""); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	l := list()
	if got := l.Pipelines[0]; !strings.Contains(got.SelectorError, `unknown model "sales"`) || len(got.ModelIDs) != 0 {
		t.Fatalf("a deleted model must be reported on the pipeline: %+v", got)
	}
	if p, _ := db.GetModelPipelineByID(pid); p.Selector != "raw,+sales+" {
		t.Fatalf("a delete must not rewrite the selector, got %q", p.Selector)
	}
}
