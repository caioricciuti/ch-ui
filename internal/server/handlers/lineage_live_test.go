package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/go-chi/chi/v5"
)

func TestLineageLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	user, password := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if user == "" {
		user = "default"
	}
	name := fmt.Sprintf("chui_lineage_%d", time.Now().UnixNano())
	execute := func(sql string, settings map[string]string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := client.Execute(ctx, sql, user, password, settings)
		return err
	}
	other := name + "_reader"
	for _, db := range []string{name, other} {
		if err := execute("CREATE DATABASE "+db, nil); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := execute("DROP DATABASE IF EXISTS "+db+" SYNC", nil); err != nil {
				t.Error(err)
			}
		})
	}
	// The view is created with an unqualified FROM in its own database.
	inDB := map[string]string{"database": name}
	for _, sql := range []string{
		"CREATE TABLE " + name + ".src (id UInt64, v String) ENGINE = MergeTree ORDER BY id",
		"CREATE TABLE " + name + ".tgt (id UInt64, v String) ENGINE = MergeTree ORDER BY id",
		"CREATE MATERIALIZED VIEW " + name + ".mv TO " + name + ".tgt AS SELECT id, v FROM " + name + ".src",
		"CREATE MATERIALIZED VIEW " + name + ".mv_inner ENGINE = MergeTree ORDER BY id AS SELECT id FROM " + name + ".src",
		"CREATE VIEW " + name + ".vw AS SELECT id, 'FROM fake' AS s FROM tgt",
		"CREATE DICTIONARY " + name + ".dict (id UInt64, v String) PRIMARY KEY id SOURCE(CLICKHOUSE(TABLE 'src' DB '" + name + "')) LIFETIME(0) LAYOUT(FLAT())",
		"CREATE TABLE " + name + ".buf AS " + name + ".tgt ENGINE = Buffer(" + name + ", tgt, 1, 10, 100, 10000, 1000000, 10000000, 100000000)",
		"INSERT INTO " + name + ".src VALUES (1, 'a'), (2, 'b')",
		// A reader in another database, linked only through its own JOIN.
		"CREATE VIEW " + other + ".rv AS SELECT * FROM " + name + ".src AS s JOIN " + name + ".tgt AS t USING id",
	} {
		if err := execute(sql, inDB); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	encrypted, err := crypto.Encrypt(password, "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	h := &LineageHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{AppSecretKey: "test-secret"}}
	router := chi.NewRouter()
	router.Route("/api/lineage", h.Routes)
	get := func(path string) []byte {
		req := httptest.NewRequest("GET", path, nil)
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{ConnectionID: conn, ClickhouseUser: user, EncryptedPassword: encrypted}))
		rr := httptest.NewRecorder()
		router.ServeHTTP(rr, req)
		if rr.Code != 200 {
			t.Fatalf("%s: %d %s", path, rr.Code, rr.Body.String())
		}
		return rr.Body.Bytes()
	}

	var g lineageResponse
	if err := json.Unmarshal(get("/api/lineage?databases="+name), &g); err != nil {
		t.Fatal(err)
	}
	for _, want := range [][4]string{
		{"src", "mv", "triggers", "system"},
		{"mv", "tgt", "writes", "parsed"},
		{"src", "mv_inner", "triggers", "system"},
		{"tgt", "vw", "reads", "parsed"},
		{"src", "dict", "sources", "parsed"},
		{"buf", "tgt", "routes", "system"},
	} {
		if !g.hasEdge(name+"."+want[0], name+"."+want[1], want[2], want[3]) {
			t.Errorf("missing edge %v; edges %v", want, g.Edges)
		}
	}
	for _, from := range []string{"src", "tgt"} {
		if !g.hasEdge(name+"."+from, other+".rv", "reads", "parsed") {
			t.Errorf("cross-database reader of %s missing; edges %v", from, g.Edges)
		}
	}
	if len(g.Edges) != 8 {
		t.Errorf("edges: %v", g.Edges)
	}
	kinds := map[string]string{"src": "table", "tgt": "table", "mv": "mv", "mv_inner": "mv", "vw": "view", "dict": "dictionary", "buf": "buffer"}
	if n := g.node(other + ".rv"); n == nil || n["kind"] != "view" {
		t.Errorf("reader node: %v", n)
	}
	if len(g.Nodes) != len(kinds)+1 {
		t.Errorf("nodes: %v", g.Nodes)
	}
	for short, kind := range kinds {
		if n := g.node(name + "." + short); n == nil || n["kind"] != kind {
			t.Errorf("%s: %v", short, n)
		}
	}
	if n := g.node(name + ".mv_inner"); n != nil && n["rows"] != float64(2) {
		t.Errorf("inner rows not folded into mv_inner: %v", n)
	}
	if g.Meta.Host == "" || g.Meta.Cached || g.Meta.NodeCount != len(g.Nodes) {
		t.Errorf("meta: %+v", g.Meta)
	}

	var node map[string]any
	if err := json.Unmarshal(get("/api/lineage/node?id="+name+".mv_inner"), &node); err != nil {
		t.Fatal(err)
	}
	if node["kind"] != "mv" || !strings.HasPrefix(node["ddl"].(string), "CREATE MATERIALIZED VIEW") || node["rows"] != float64(2) {
		t.Errorf("node: %v", node)
	}

	// Without a filter the graph holds the same objects among the rest.
	if err := json.Unmarshal(get("/api/lineage?refresh=1"), &g); err != nil {
		t.Fatal(err)
	}
	if !g.hasEdge(name+".tgt", name+".vw", "reads", "parsed") {
		t.Error("unfiltered graph lacks the view edge")
	}
}
