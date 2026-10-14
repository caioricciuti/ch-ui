package handlers

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/models"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestModelsStayOnTheSessionConnection(t *testing.T) {
	db, err := dbtest.Open(t, "models.db")
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

	theirTest, err := db.CreateModelTest(database.ModelTest{ModelID: theirs, Kind: "not_null", ColumnName: "id", Severity: "error"})
	if err != nil {
		t.Fatal(err)
	}
	theirPipeline, err := db.CreateModelPipeline(database.ModelPipeline{ConnectionID: connB, Name: "theirs", Selector: "theirs"})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("another connection's model is not found", func(t *testing.T) {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodGet, "/" + theirs, ""},
			{http.MethodPut, "/" + theirs, `{"sql_body":"SELECT 2"}`},
			{http.MethodDelete, "/" + theirs, ""},
			{http.MethodPost, "/" + theirs + "/run", ""},
			{http.MethodPost, "/" + theirs + "/run", `{"full_refresh":true}`},
			{http.MethodPost, "/" + theirs + "/compile", ""},
			{http.MethodGet, "/" + theirs + "/tests", ""},
			{http.MethodPost, "/" + theirs + "/tests", `{"kind":"not_null","column_name":"id"}`},
			{http.MethodPut, "/" + theirs + "/tests/" + theirTest, `{"kind":"unique","column_name":"id"}`},
			{http.MethodDelete, "/" + theirs + "/tests/" + theirTest, ""},
			{http.MethodPost, "/preview", `{"sql_body":"SELECT 1","model_id":"` + theirs + `"}`},
			{http.MethodGet, "/runs/" + theirRun, ""},
			{http.MethodPut, "/pipelines/" + theirPipeline, `{"name":"x","selector":"mine"}`},
			{http.MethodDelete, "/pipelines/" + theirPipeline, ""},
			{http.MethodPost, "/pipelines/" + theirPipeline + "/run", ""},
			{http.MethodPost, "/no-such-model/run", ""},
		} {
			if got := as(c.method, c.path, c.body); got != http.StatusNotFound {
				t.Errorf("%s %s: status %d, want 404", c.method, c.path, got)
			}
		}
		if m, _ := db.GetModelByID(theirs); m == nil || m.SQLBody != "SELECT 1" {
			t.Fatal("the other connection's model was changed or deleted")
		}
		if tt, _ := db.GetModelTestByID(theirTest); tt == nil || tt.Kind != "not_null" {
			t.Fatal("the other connection's test was changed or deleted")
		}
		if p, _ := db.GetModelPipelineByID(theirPipeline); p == nil || p.Name != "theirs" {
			t.Fatal("the other connection's pipeline was changed or deleted")
		}
	})

	t.Run("viewers cannot write", func(t *testing.T) {
		for _, c := range []struct{ method, path, body string }{
			{http.MethodPost, "/" + mine + "/tests", `{"kind":"not_null","column_name":"id"}`},
			{http.MethodPost, "/pipelines", `{"name":"p","selector":"mine"}`},
			{http.MethodPost, "/run", `{"selector":"mine"}`},
			{http.MethodPost, "/" + mine + "/run", `{"full_refresh":true}`},
		} {
			if got := pipelineRequestAs(router, "viewer", connA, c.method, c.path, c.body).Code; got != http.StatusForbidden {
				t.Errorf("viewer %s %s: status %d, want 403", c.method, c.path, got)
			}
		}
	})

	t.Run("materialization and options are validated", func(t *testing.T) {
		for _, body := range []string{
			`{"name":"bad1","sql_body":"SELECT 1","materialization":"ephemeral"}`,
			`{"name":"bad2","sql_body":"SELECT 1","materialization":"table","partition_by":"toYYYYMM(ts); DROP TABLE x"}`,
			`{"name":"bad3","sql_body":"SELECT 1","materialization":"table","partition_by":"ts -- )"}`,
			`{"name":"bad4","sql_body":"SELECT 1","materialization":"incremental","incremental_strategy":"delete_insert"}`,
			`{"name":"bad5","sql_body":"SELECT 1","materialization":"incremental","incremental_strategy":"merge"}`,
			`{"name":"bad6","sql_body":"SELECT 1","materialization":"table","table_engine":"Memory","partition_by":"x"}`,
		} {
			if got := as(http.MethodPost, "/", body); got != http.StatusBadRequest {
				t.Errorf("%s: status %d, want 400", body, got)
			}
		}
		if got := as(http.MethodPut, "/"+mine, `{"materialization":"snapshot"}`); got != http.StatusBadRequest {
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
		rec = pipelineRequest(router, connA, http.MethodPost, "/", `{"name":"inc","sql_body":"SELECT 1 AS id","materialization":"incremental","incremental_strategy":"delete_insert","unique_key":"id , ts","watermark_column":"ts","partition_by":"toYYYYMM(ts)","table_engine":"ReplacingMergeTree","engine_args":"ts"}`)
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusCreated {
			t.Fatalf("create incremental: %d %s", rec.Code, rec.Body.String())
		}
		if m := resp.Model; m.UniqueKey != "id, ts" || m.PartitionBy != "toYYYYMM(ts)" || m.EngineArgs != "ts" || m.WatermarkColumn != "ts" {
			t.Fatalf("options not stored: %+v", m)
		}
		// Options left out of an update keep their values.
		rec = pipelineRequest(router, connA, http.MethodPut, "/"+resp.Model.ID, `{"sql_body":"SELECT 2 AS id"}`)
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK || resp.Model.PartitionBy != "toYYYYMM(ts)" {
			t.Fatalf("update kept options? %d %s", rec.Code, rec.Body.String())
		}
	})

	t.Run("data tests", func(t *testing.T) {
		if got := as(http.MethodPost, "/"+mine+"/tests", `{"kind":"accepted_values","column_name":"status"}`); got != http.StatusBadRequest {
			t.Fatalf("accepted_values without values: %d", got)
		}
		if got := as(http.MethodPost, "/"+mine+"/tests", `{"kind":"expression","args":{"expression":"1; DROP TABLE x"}}`); got != http.StatusBadRequest {
			t.Fatalf("expression with ';': %d", got)
		}
		rec := pipelineRequest(router, connA, http.MethodPost, "/"+mine+"/tests", `{"kind":"accepted_values","column_name":"status","args":{"values":["a","b"]},"severity":"warn"}`)
		var resp struct {
			Test database.ModelTest `json:"test"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusCreated {
			t.Fatalf("create test: %d %s", rec.Code, rec.Body.String())
		}
		if got := as(http.MethodPut, "/"+downstream+"/tests/"+resp.Test.ID, `{"kind":"unique","column_name":"id"}`); got != http.StatusNotFound {
			t.Fatalf("a test is only reachable through its model: %d", got)
		}
		if got := as(http.MethodDelete, "/"+mine+"/tests/"+resp.Test.ID, ""); got != http.StatusOK {
			t.Fatalf("delete test: %d", got)
		}
	})

	t.Run("compile", func(t *testing.T) {
		rec := pipelineRequest(router, connA, http.MethodPost, "/"+downstream+"/compile", `{"sql_body":"SELECT * FROM $ref(mine) WHERE 1 -- note"}`)
		var resp struct {
			ResolvedSQL string            `json:"resolved_sql"`
			Statements  []models.PlanStep `json:"statements"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil || rec.Code != http.StatusOK {
			t.Fatalf("compile: %d %s", rec.Code, rec.Body.String())
		}
		if resp.ResolvedSQL != "SELECT * FROM `default`.`mine` WHERE 1 -- note" || len(resp.Statements) == 0 {
			t.Fatalf("unexpected compile output %+v", resp)
		}
	})

	t.Run("named pipelines", func(t *testing.T) {
		if got := as(http.MethodPost, "/pipelines", `{"name":"p","selector":"+no_such_model"}`); got != http.StatusBadRequest {
			t.Fatalf("unknown model in selector: %d", got)
		}
		if got := as(http.MethodPost, "/pipelines", `{"name":"p","selector":"mine","cron":"nope"}`); got != http.StatusBadRequest {
			t.Fatalf("bad cron: %d", got)
		}
		if got := as(http.MethodPost, "/pipelines", `{"name":"p","selector":"mine","cron":"0 * * * *","timezone":"Mars/Base"}`); got != http.StatusBadRequest {
			t.Fatalf("bad timezone: %d", got)
		}
		rec := pipelineRequest(router, connA, http.MethodPost, "/pipelines", `{"name":"nightly","selector":"mine+","cron":"0 2 * * *","timezone":"Europe/Lisbon"}`)
		var created struct {
			Pipeline database.ModelPipeline `json:"pipeline"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
			t.Fatalf("create pipeline: %d %s", rec.Code, rec.Body.String())
		}
		if !created.Pipeline.Enabled || created.Pipeline.NextRunAt == nil {
			t.Fatalf("a pipeline with a cron starts enabled and scheduled: %+v", created.Pipeline)
		}
		if got := as(http.MethodPost, "/pipelines", `{"name":"nightly","selector":"mine"}`); got != http.StatusConflict {
			t.Fatalf("duplicate name: %d", got)
		}
		if got := as(http.MethodPut, "/pipelines/"+created.Pipeline.ID, `{"name":"nightly","selector":"mine+","cron":"0 2 * * *","enabled":false}`); got != http.StatusOK {
			t.Fatalf("pause: %d", got)
		}
		if p, _ := db.GetModelPipelineByID(created.Pipeline.ID); p == nil || p.Enabled || p.Timezone != "UTC" {
			t.Fatalf("pause not stored: %+v", p)
		}
		rec = pipelineRequest(router, connA, http.MethodGet, "/pipelines", "")
		var list struct {
			Pipelines []struct {
				ID       string   `json:"id"`
				ModelIDs []string `json:"model_ids"`
			} `json:"pipelines"`
			Ungrouped []string `json:"ungrouped_model_ids"`
		}
		if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil || len(list.Pipelines) != 1 {
			t.Fatalf("list: %s", rec.Body.String())
		}
		if ids := list.Pipelines[0].ModelIDs; len(ids) != 2 || ids[0] != mine || ids[1] != downstream {
			t.Fatalf("mine+ must pick mine then downstream, got %v", ids)
		}
		if got := as(http.MethodGet, "/schedule-preview?cron=0+9+*+*+*&timezone=Europe/Lisbon", ""); got != http.StatusOK {
			t.Fatalf("schedule preview: %d", got)
		}
		if got := as(http.MethodDelete, "/pipelines/"+created.Pipeline.ID, ""); got != http.StatusOK {
			t.Fatalf("delete pipeline: %d", got)
		}
	})
}
