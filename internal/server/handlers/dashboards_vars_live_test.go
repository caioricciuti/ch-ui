package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/dashboards"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// TestDashboardVariablesLive runs variable substitution, cascading options,
// injection attempts and every template panel against a real ClickHouse.
func TestDashboardVariablesLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	user, password := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if user == "" {
		user = "default"
	}
	client := connector.NewCHClient(endpoint, false)
	exec := func(sql string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, err := client.Execute(ctx, sql, user, password, nil); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}
	dbName := fmt.Sprintf("chui_dashvars_%d", time.Now().UnixNano())
	exec("CREATE DATABASE " + dbName)
	t.Cleanup(func() { exec("DROP DATABASE IF EXISTS " + dbName) })
	exec("CREATE TABLE " + dbName + ".items (grp String, name String, n UInt32) ENGINE = MergeTree ORDER BY name")
	exec(`INSERT INTO ` + dbName + `.items VALUES ('g1','a',1), ('g1','b',2), ('g2','c',3), ('g2','o''brien\\x',4)`)

	const secret = "test-secret"
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	h := &DashboardsHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: secret}}
	enc, err := crypto.Encrypt(password, secret)
	if err != nil {
		t.Fatal(err)
	}
	router := h.Routes()
	do := func(method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{
			ConnectionID: conn, ClickhouseUser: user, EncryptedPassword: enc, UserRole: "admin",
		}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}
	count := func(t *testing.T, query string, vars map[string][]string, dashID string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]interface{}{"query": query, "variables": vars, "dashboard_id": dashID})
		rec := do(http.MethodPost, "/query", string(body))
		if rec.Code != http.StatusOK {
			t.Fatalf("query %s: %d %s", query, rec.Code, rec.Body.String())
		}
		var res struct {
			Data []map[string]json.RawMessage `json:"data"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &res)
		if len(res.Data) != 1 {
			t.Fatalf("query %s: %s", query, rec.Body.String())
		}
		return jsonText(res.Data[0]["c"])
	}
	table := dbName + ".items"

	t.Run("multi value IN and numbers", func(t *testing.T) {
		if got := count(t, "SELECT count() AS c FROM "+table+" WHERE name IN (${names})", map[string][]string{"names": {"a", "c"}}, ""); got != "2" {
			t.Fatalf("got %s, want 2", got)
		}
		if got := count(t, "SELECT count() AS c FROM "+table+" WHERE n >= ${min:number}", map[string][]string{"min": {"3"}}, ""); got != "2" {
			t.Fatalf("got %s, want 2", got)
		}
		// A quoted literal compares fine with a numeric column too.
		if got := count(t, "SELECT count() AS c FROM "+table+" WHERE n = $n", map[string][]string{"n": {"4"}}, ""); got != "1" {
			t.Fatalf("got %s, want 1", got)
		}
	})

	t.Run("injection stays a literal", func(t *testing.T) {
		for _, evil := range []string{`x') OR 1=1 --`, `' OR '1'='1`, `\' OR 1=1 --`, `a\`, "a' UNION ALL SELECT 1 --"} {
			if got := count(t, "SELECT count() AS c FROM "+table+" WHERE name IN ($v)", map[string][]string{"v": {evil}}, ""); got != "0" {
				t.Fatalf("%q matched %s rows: not treated as a literal", evil, got)
			}
		}
		if got := count(t, "SELECT count() AS c FROM "+table+" WHERE name = $v", map[string][]string{"v": {`o'brien\x`}}, ""); got != "1" {
			t.Fatalf("a value with a quote and a backslash must match itself exactly, got %s", got)
		}
		rec := do(http.MethodPost, "/query", `{"query":"SELECT ${v:number} AS c","variables":{"v":["1 OR 1=1"]}}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf(":number must refuse a non-number, got %d", rec.Code)
		}
		rec = do(http.MethodPost, "/query", `{"query":"SELECT ${v:raw} AS c","variables":{"v":["1"]}}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf(":raw must be refused, got %d", rec.Code)
		}
	})

	t.Run("cascading options and dashboard values", func(t *testing.T) {
		rec := do(http.MethodPost, "/", `{"name":"live vars"}`)
		var created struct {
			Dashboard struct {
				ID string `json:"id"`
			} `json:"dashboard"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &created)
		id := created.Dashboard.ID
		vars := []dashboards.Variable{
			{Name: "grp", Type: dashboards.TypeQuery, Query: "SELECT DISTINCT grp FROM " + table + " ORDER BY grp", Multi: true, IncludeAll: true},
			{Name: "item", Type: dashboards.TypeQuery, Query: "SELECT name, upper(name) FROM " + table + " WHERE grp IN (${grp}) ORDER BY name", Multi: true, IncludeAll: true, Default: dashboards.StringList{dashboards.AllValue}},
			{Name: "fixed", Type: dashboards.TypeConstant, Default: dashboards.StringList{"g2"}},
		}
		raw, _ := json.Marshal(map[string]interface{}{"variables": vars})
		if rec := do(http.MethodPut, "/"+id+"/variables", string(raw)); rec.Code != http.StatusOK {
			t.Fatalf("save variables: %d %s", rec.Code, rec.Body.String())
		}
		rec = do(http.MethodPost, "/"+id+"/variables/item/options", `{"variables":{"grp":["g2"]}}`)
		var opts struct {
			Options []dashboards.Option `json:"options"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &opts)
		if rec.Code != http.StatusOK || len(opts.Options) != 2 || opts.Options[0].Value != "c" || opts.Options[0].Label != "C" {
			t.Fatalf("cascading options: %d %s", rec.Code, rec.Body.String())
		}
		// All for grp, default (All) for item: every row.
		if got := count(t, "SELECT count() AS c FROM "+table+" WHERE name IN (${item})", map[string][]string{"grp": {dashboards.AllValue}}, id); got != "4" {
			t.Fatalf("all: got %s, want 4", got)
		}
		if got := count(t, "SELECT count() AS c FROM "+table+" WHERE name IN (${item})", map[string][]string{"grp": {"g1"}}, id); got != "2" {
			t.Fatalf("cascaded all: got %s, want 2", got)
		}

		// Share link: only options are accepted, re-run with the sharer's credentials.
		share, err := db.CreateDashboardShare(id, "viewer", "public", "", conn, user, enc, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		panelBody, _ := json.Marshal(map[string]interface{}{"name": "n", "panel_type": "stat", "query": "SELECT count() AS c FROM " + table + " WHERE name IN (${item})"})
		rec = do(http.MethodPost, "/"+id+"/panels", string(panelBody))
		var p struct {
			Panel struct {
				ID string `json:"id"`
			} `json:"panel"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		public := h.PublicRoutes()
		post := func(body string) *httptest.ResponseRecorder {
			rec := httptest.NewRecorder()
			public.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+share.Token+"/query", strings.NewReader(body)))
			return rec
		}
		if rec := post(`{"panel_id":"` + p.Panel.ID + `","variables":{"grp":["g1"],"item":["b"]}}`); rec.Code != http.StatusOK || !(strings.Contains(rec.Body.String(), `"c":"1"`) || strings.Contains(rec.Body.String(), `"c":1}`)) {
			t.Fatalf("public valid values: %d %s", rec.Code, rec.Body.String())
		}
		if rec := post(`{"panel_id":"` + p.Panel.ID + `","variables":{"grp":["g1"],"item":["c"]}}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("public value from another group must be refused: %d %s", rec.Code, rec.Body.String())
		}
		if rec := post(`{"panel_id":"` + p.Panel.ID + `","variables":{"item":["x') OR 1=1 --"]}}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("public injected value must be refused: %d %s", rec.Code, rec.Body.String())
		}

		// A constant is not listed on the share link, but public panels still
		// get its stored value, whatever the client sends for it.
		get := httptest.NewRecorder()
		public.ServeHTTP(get, httptest.NewRequest(http.MethodGet, "/"+share.Token, nil))
		if get.Code != http.StatusOK || strings.Contains(get.Body.String(), `"fixed"`) {
			t.Fatalf("public dashboard must not list the constant: %d %s", get.Code, get.Body.String())
		}
		constBody, _ := json.Marshal(map[string]interface{}{"name": "fixed", "panel_type": "stat", "query": "SELECT count() AS c FROM " + table + " WHERE grp = ${fixed}"})
		rec = do(http.MethodPost, "/"+id+"/panels", string(constBody))
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		for _, body := range []string{
			`{"panel_id":"` + p.Panel.ID + `"}`,
			`{"panel_id":"` + p.Panel.ID + `","variables":{"fixed":["g1"]}}`,
		} {
			if rec := post(body); rec.Code != http.StatusOK || !(strings.Contains(rec.Body.String(), `"c":"2"`) || strings.Contains(rec.Body.String(), `"c":2}`)) {
				t.Fatalf("public constant panel %s: %d %s", body, rec.Code, rec.Body.String())
			}
		}
	})

	t.Run("All past the options cap covers every value", func(t *testing.T) {
		many := dbName + ".many"
		exec("CREATE TABLE " + many + " (name String) ENGINE = MergeTree ORDER BY name")
		exec("INSERT INTO " + many + " SELECT toString(number) FROM numbers(1500)")
		rec := do(http.MethodPost, "/", `{"name":"many"}`)
		var created struct {
			Dashboard struct {
				ID string `json:"id"`
			} `json:"dashboard"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &created)
		id := created.Dashboard.ID
		vars := []dashboards.Variable{{Name: "name", Type: dashboards.TypeQuery, Query: "SELECT name FROM " + many + " ORDER BY name;", Multi: true, IncludeAll: true}}
		raw, _ := json.Marshal(map[string]interface{}{"variables": vars})
		if rec := do(http.MethodPut, "/"+id+"/variables", string(raw)); rec.Code != http.StatusOK {
			t.Fatalf("save variables: %d %s", rec.Code, rec.Body.String())
		}
		query := "SELECT count() AS c FROM " + many + " WHERE name IN (${name})"
		all := map[string][]string{"name": {dashboards.AllValue}}
		if got := count(t, query, all, id); got != "1500" {
			t.Fatalf("All: got %s, want 1500", got)
		}
		body, _ := json.Marshal(map[string]interface{}{"query": "SELECT count() AS c FROM " + many + " WHERE name = ${name}", "variables": all, "dashboard_id": id})
		if rec := do(http.MethodPost, "/query", string(body)); rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "IN ($name)") {
			t.Fatalf("All outside IN must be refused: %d %s", rec.Code, rec.Body.String())
		}

		share, err := db.CreateDashboardShare(id, "viewer", "public", "", conn, user, enc, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		panelBody, _ := json.Marshal(map[string]interface{}{"name": "n", "panel_type": "stat", "query": query})
		rec = do(http.MethodPost, "/"+id+"/panels", string(panelBody))
		var p struct {
			Panel struct {
				ID string `json:"id"`
			} `json:"panel"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		pub := httptest.NewRecorder()
		h.PublicRoutes().ServeHTTP(pub, httptest.NewRequest(http.MethodPost, "/"+share.Token+"/query", strings.NewReader(`{"panel_id":"`+p.Panel.ID+`","variables":{"name":["$__all"]}}`)))
		if pub.Code != http.StatusOK || !(strings.Contains(pub.Body.String(), `"c":"1500"`) || strings.Contains(pub.Body.String(), `"c":1500}`)) {
			t.Fatalf("public All: %d %s", pub.Code, pub.Body.String())
		}
	})

	t.Run("docs examples execute", func(t *testing.T) {
		rangeBody := map[string]string{"type": "relative", "from": "1h", "to": "now"}
		run := func(query string, vars map[string][]string, dashID string) {
			t.Helper()
			body, _ := json.Marshal(map[string]interface{}{"query": query, "variables": vars, "dashboard_id": dashID, "time_range": rangeBody})
			if rec := do(http.MethodPost, "/query", string(body)); rec.Code != http.StatusOK {
				t.Fatalf("%s: %d %s", query, rec.Code, rec.Body.String())
			}
		}
		rec := do(http.MethodPost, "/", `{"name":"docs examples"}`)
		var created struct {
			Dashboard struct {
				ID string `json:"id"`
			} `json:"dashboard"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &created)
		id := created.Dashboard.ID
		defs := `{"variables":[
			{"name":"database","label":"Database","type":"query","query":"SELECT name FROM system.databases ORDER BY name","multi":true,"include_all":true,"default":["$__all"]},
			{"name":"table","type":"query","query":"SELECT name FROM system.tables WHERE database IN (${database}) ORDER BY name","multi":true,"include_all":true,"default":["$__all"]},
			{"name":"bucket","type":"interval","options":"1m,5m,15m,1h","default":["5m"]},
			{"name":"search","type":"text","default":"SELECT"},
			{"name":"top","type":"custom","options":"10,50,100","default":["10"]},
			{"name":"labelled","type":"query","query":"SELECT name, concat(name, ' (', engine, ')') FROM system.databases ORDER BY name"}]}`
		if rec := do(http.MethodPut, "/"+id+"/variables", defs); rec.Code != http.StatusOK {
			t.Fatalf("save: %d %s", rec.Code, rec.Body.String())
		}
		rec = do(http.MethodPost, "/"+id+"/variables/table/options", `{"variables":{"database":["system"]}}`)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"value":"parts"`) {
			t.Fatalf("table options: %d %s", rec.Code, rec.Body.String())
		}
		rec = do(http.MethodPost, "/"+id+"/variables/labelled/options", `{}`)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"label":"system (`) {
			t.Fatalf("labelled options: %d %s", rec.Code, rec.Body.String())
		}
		run("SELECT table, count() AS parts, sum(rows) AS rows\nFROM system.parts\nWHERE active AND database IN (${database}) AND table IN (${table})\nGROUP BY table\nORDER BY parts DESC", map[string][]string{"database": {"system"}}, id)
		run("SELECT\n  toStartOfInterval(event_time, INTERVAL $bucket second) AS ts,\n  count() AS queries\nFROM system.query_log\nWHERE $__timeFilter(event_time, datetime)\nGROUP BY ts\nORDER BY ts", map[string][]string{"bucket": {"1h"}}, id)
		run("SELECT event_time, user, left(query, 200) AS query\nFROM system.query_log\nWHERE type = 'QueryFinish'\n  AND $__timeFilter(event_time, datetime)\n  AND positionCaseInsensitive(query, ${search}) > 0\nORDER BY event_time DESC\nLIMIT ${top:number}", map[string][]string{"search": {"system"}, "top": {"50"}}, id)
	})

	t.Run("template panels execute", func(t *testing.T) {
		list, err := dashboards.Templates()
		if err != nil {
			t.Fatal(err)
		}
		for _, tpl := range list {
			rec := do(http.MethodPost, "/from-template", `{"template_id":"`+tpl.ID+`"}`)
			if rec.Code != http.StatusCreated {
				t.Fatalf("%s: %d %s", tpl.ID, rec.Code, rec.Body.String())
			}
			d := decodeDetail(t, rec)
			for _, p := range d.Panels {
				body, _ := json.Marshal(map[string]interface{}{
					"query": p.Query, "dashboard_id": d.Dashboard.ID,
					"time_range": map[string]string{"type": "relative", "from": "1h", "to": "now"},
				})
				rec := do(http.MethodPost, "/query", string(body))
				if rec.Code != http.StatusOK {
					t.Errorf("%s / %s: %d %s", tpl.ID, p.Name, rec.Code, rec.Body.String())
				}
			}
		}
	})
}
