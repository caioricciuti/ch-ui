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
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// TestDashboardDateTimeVariablesLive clicks a DateTime value in a panel (an
// ISO instant) and filters by it through a DateTime variable, signed in and
// on a share link, for DateTime, DateTime with a zone and DateTime64.
func TestDashboardDateTimeVariablesLive(t *testing.T) {
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
	dbName := fmt.Sprintf("chui_dt_%d", time.Now().UnixNano())
	exec("CREATE DATABASE " + dbName)
	t.Cleanup(func() { exec("DROP DATABASE IF EXISTS " + dbName) })
	table := dbName + ".events"
	exec("CREATE TABLE " + table + " (ts DateTime, md DateTime('Europe/Madrid'), ms DateTime64(3, 'Europe/Madrid')) ENGINE = MergeTree ORDER BY ts")
	exec("INSERT INTO " + table + " VALUES ('2026-01-01 09:00:00', '2026-01-01 10:00:00', '2026-01-01 10:00:00.120'), ('2026-07-01 09:00:00', '2026-07-01 11:00:00', '2026-01-01 10:00:00.123')")

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
	rows := func(t *testing.T, rec *httptest.ResponseRecorder) []map[string]json.RawMessage {
		t.Helper()
		var res struct {
			Data []map[string]json.RawMessage `json:"data"`
		}
		if rec.Code != http.StatusOK || json.Unmarshal(rec.Body.Bytes(), &res) != nil {
			t.Fatalf("%d %s", rec.Code, rec.Body.String())
		}
		return res.Data
	}

	rec := do(http.MethodPost, "/", `{"name":"datetime vars"}`)
	var created struct {
		Dashboard struct {
			ID string `json:"id"`
		} `json:"dashboard"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &created)
	id := created.Dashboard.ID
	vars := []dashboards.Variable{
		{Name: "ts", Type: dashboards.TypeQuery, Query: "SELECT DISTINCT ts FROM " + table + " ORDER BY ts"},
		{Name: "md", Type: dashboards.TypeQuery, Query: "SELECT DISTINCT md FROM " + table + " ORDER BY md"},
		{Name: "ms", Type: dashboards.TypeQuery, Query: "SELECT DISTINCT ms FROM " + table + " ORDER BY ms", Multi: true},
	}
	raw, _ := json.Marshal(map[string]interface{}{"variables": vars})
	if rec := do(http.MethodPut, "/"+id+"/variables", string(raw)); rec.Code != http.StatusOK {
		t.Fatalf("save variables: %d %s", rec.Code, rec.Body.String())
	}

	// Options stay the text form ClickHouse reads back as a literal, in the
	// column's zone, and carry their instant.
	rec = do(http.MethodPost, "/"+id+"/variables/md/options", `{}`)
	var opts struct {
		Options []dashboards.Option `json:"options"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &opts)
	if rec.Code != http.StatusOK || len(opts.Options) != 2 || opts.Options[0].Value != "2026-01-01 10:00:00" || opts.Options[0].Instant != "2026-01-01T09:00:00Z" {
		t.Fatalf("md options: %d %s", rec.Code, rec.Body.String())
	}

	// What a panel click hands over: the raw iso values of the first row.
	panelRows := rows(t, do(http.MethodPost, "/query", `{"query":"SELECT ts, md, ms FROM `+table+` ORDER BY ms DESC LIMIT 1"}`))
	clicked := map[string][]string{}
	for _, col := range []string{"ts", "md", "ms"} {
		v := jsonText(panelRows[0][col])
		if !strings.Contains(v, "T") || !strings.HasSuffix(v, "Z") {
			t.Fatalf("panel %s value %q is not an iso instant", col, v)
		}
		clicked[col] = []string{v}
	}
	if clicked["ms"][0] != "2026-01-01T09:00:00.123Z" {
		t.Fatalf("ms clicked %q", clicked["ms"][0])
	}

	query := "SELECT count() AS c FROM " + table + " WHERE ts = ${ts} AND md = ${md} AND ms IN (${ms})"
	countOf := func(t *testing.T, rec *httptest.ResponseRecorder) string {
		t.Helper()
		data := rows(t, rec)
		if len(data) != 1 {
			t.Fatalf("rows: %v", data)
		}
		return jsonText(data[0]["c"])
	}
	t.Run("signed in", func(t *testing.T) {
		body, _ := json.Marshal(map[string]interface{}{"query": query, "variables": clicked, "dashboard_id": id})
		if got := countOf(t, do(http.MethodPost, "/query", string(body))); got != "1" {
			t.Fatalf("clicked instants: got %s, want 1", got)
		}
		// The stored text form keeps working.
		stored := map[string][]string{"ts": {"2026-07-01 09:00:00"}, "md": {"2026-07-01 11:00:00"}, "ms": {"2026-01-01 10:00:00.123"}}
		body, _ = json.Marshal(map[string]interface{}{"query": query, "variables": stored, "dashboard_id": id})
		if got := countOf(t, do(http.MethodPost, "/query", string(body))); got != "1" {
			t.Fatalf("stored values: got %s, want 1", got)
		}
	})

	t.Run("share link", func(t *testing.T) {
		share, err := db.CreateDashboardShare(database.CreateDashboardShareParams{DashboardID: id, AccessLevel: "viewer", Visibility: "public", ConnectionID: conn, ClickhouseUser: user, EncryptedPassword: enc})
		if err != nil {
			t.Fatal(err)
		}
		panelBody, _ := json.Marshal(map[string]interface{}{"name": "n", "panel_type": "stat", "query": query})
		rec := do(http.MethodPost, "/"+id+"/panels", string(panelBody))
		var p struct {
			Panel struct {
				ID string `json:"id"`
			} `json:"panel"`
		}
		_ = json.Unmarshal(rec.Body.Bytes(), &p)
		public := h.PublicRoutes()
		post := func(vars map[string][]string) *httptest.ResponseRecorder {
			body, _ := json.Marshal(map[string]interface{}{"panel_id": p.Panel.ID, "variables": vars})
			rec := httptest.NewRecorder()
			public.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/"+share.Token+"/query", strings.NewReader(string(body))))
			return rec
		}
		if got := countOf(t, post(clicked)); got != "1" {
			t.Fatalf("clicked instants: got %s, want 1", got)
		}
		// A browser's toISOString form of the same instant.
		if got := countOf(t, post(map[string][]string{"ts": {"2026-01-01T09:00:00.000Z"}, "md": {"2026-01-01T09:00:00.000Z"}, "ms": {"2026-01-01T09:00:00.120Z"}})); got != "1" {
			t.Fatalf("toISOString instants: got %s, want 1", got)
		}
		for _, bad := range []map[string][]string{
			{"md": {"2026-01-01T09:00:01Z"}},
			{"ms": {"2026-01-01T09:00:00.121Z"}},
			{"md": {"2026-01-01 09:00:00"}},
			{"md": {"2026-01-01T09:00:00Z') OR 1=1 --"}},
		} {
			if rec := post(bad); rec.Code != http.StatusBadRequest {
				t.Fatalf("%v must be refused: %d %s", bad, rec.Code, rec.Body.String())
			}
		}
	})
}
