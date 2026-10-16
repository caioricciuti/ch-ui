package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/server/middleware"
)

func TestIngestKeysAndStats(t *testing.T) {
	db, err := dbtest.Open(t, "ingest.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	connA, _ := db.CreateConnection(database.CreateConnectionParams{Name: "a", TunnelToken: "a"})
	connB, _ := db.CreateConnection(database.CreateConnectionParams{Name: "b", TunnelToken: "b"})
	mine, _ := db.CreatePipeline("logs in", "", connA, "analyst")
	plain, _ := db.CreatePipeline("plain", "", connA, "analyst")
	theirs, _ := db.CreatePipeline("theirs", "", connB, "someone")
	if err := db.SavePipelineGraph(mine, []database.PipelineNode{{ID: mine + "-src", NodeType: "source_ingest", ConfigEncrypted: `{"signal":"logs"}`}}, nil, ""); err != nil {
		t.Fatal(err)
	}

	sp, err := spool.Open(t.TempDir(), spool.Options{SyncMode: spool.SyncNone})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	hub := pipelines.NewHub(sp)
	runner := pipelines.NewRunner(db, nil, &config.Config{})
	runner.SetHub(hub)
	router := (&PipelinesHandler{DB: db, Runner: runner}).Routes()
	as := func(role, method, path, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		req = req.WithContext(middleware.SetSession(req.Context(), &middleware.SessionInfo{UserRole: role, ClickhouseUser: "alice", ConnectionID: connA}))
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		return rec
	}

	rec := as("analyst", http.MethodPost, "/"+mine+"/ingest-keys", `{"name":"collector"}`)
	var created struct {
		Key    map[string]interface{} `json:"key"`
		Secret string                 `json:"secret"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil || rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body)
	}
	if !strings.HasPrefix(created.Secret, "chi_") || len(created.Secret) != 44 || created.Key["prefix"] != created.Secret[4:12] || created.Key["name"] != "collector" || created.Key["created_by"] != "alice" {
		t.Fatalf("created: %+v", created)
	}
	for _, f := range []string{"id", "name", "prefix", "created_by", "created_at", "last_used_at", "revoked_at"} {
		if _, ok := created.Key[f]; !ok {
			t.Fatalf("key lacks %s: %v", f, created.Key)
		}
	}
	if strings.Contains(rec.Body.String(), "hash") {
		t.Fatalf("response leaks the hash: %s", rec.Body)
	}
	keyID := created.Key["id"].(string)

	list := as("analyst", http.MethodGet, "/"+mine+"/ingest-keys", "")
	if list.Code != 200 || strings.Contains(list.Body.String(), created.Secret) || !strings.Contains(list.Body.String(), keyID) {
		t.Fatalf("list: %d %s", list.Code, list.Body)
	}
	for _, c := range []struct{ method, path, body string }{
		{http.MethodGet, "/" + mine + "/ingest-keys", ""},
		{http.MethodPost, "/" + mine + "/ingest-keys", `{"name":"x"}`},
		{http.MethodDelete, "/" + mine + "/ingest-keys/" + keyID, ""},
	} {
		if got := as("viewer", c.method, c.path, c.body).Code; got != http.StatusForbidden {
			t.Errorf("viewer %s %s: %d", c.method, c.path, got)
		}
	}
	if got := as("analyst", http.MethodGet, "/"+theirs+"/ingest-keys", "").Code; got != http.StatusNotFound {
		t.Errorf("other connection: %d", got)
	}
	if got := as("analyst", http.MethodPost, "/"+mine+"/ingest-keys", `{"name":"  "}`).Code; got != http.StatusBadRequest {
		t.Errorf("blank name: %d", got)
	}
	if got := as("analyst", http.MethodDelete, "/"+plain+"/ingest-keys/"+keyID, "").Code; got != http.StatusNotFound {
		t.Errorf("revoke through another pipeline: %d", got)
	}

	if got := as("analyst", http.MethodDelete, "/"+mine+"/ingest-keys/"+keyID, "").Code; got != http.StatusNoContent {
		t.Fatalf("revoke: %d", got)
	}
	if k, _ := db.GetIngestKey(mine, keyID); k == nil || k.RevokedAt == nil {
		t.Fatal("key not revoked")
	}
	for _, action := range []string{"ingest_key.created", "ingest_key.revoked"} {
		logs, err := db.GetAuditLogsFiltered(10, "", action, "", "")
		if err != nil || len(logs) != 1 {
			t.Fatalf("%s audit: %v %v", action, logs, err)
		}
		if b, _ := json.Marshal(logs[0]); strings.Contains(string(b), created.Secret) || !strings.Contains(string(b), "collector") {
			t.Fatalf("%s audit entry: %s", action, b)
		}
	}

	// The list carries each pipeline's source type and the ingest signal.
	listed := as("viewer", http.MethodGet, "/", "")
	var lr struct {
		Pipelines []map[string]interface{} `json:"pipelines"`
	}
	if err := json.Unmarshal(listed.Body.Bytes(), &lr); err != nil || len(lr.Pipelines) != 2 {
		t.Fatalf("list: %d %s", listed.Code, listed.Body)
	}
	for _, p := range lr.Pipelines {
		switch p["id"] {
		case mine:
			if p["source_type"] != "source_ingest" || p["signal"] != "logs" || p["name"] != "logs in" {
				t.Fatalf("ingest pipeline item: %v", p)
			}
		case plain:
			if _, ok := p["source_type"]; ok {
				t.Fatalf("pipeline without nodes has a source_type: %v", p)
			}
			if _, ok := p["signal"]; ok {
				t.Fatalf("non-ingest pipeline has a signal: %v", p)
			}
		}
	}

	// Stats: the signal comes from the source node; other sources have none.
	stats := as("viewer", http.MethodGet, "/"+mine+"/ingest-stats", "")
	var view map[string]interface{}
	if err := json.Unmarshal(stats.Body.Bytes(), &view); err != nil || stats.Code != 200 || view["signal"] != "logs" {
		t.Fatalf("stats: %d %s", stats.Code, stats.Body)
	}
	for _, f := range []string{"received", "rejected", "written", "spool", "rate", "last_error"} {
		if _, ok := view[f]; !ok {
			t.Fatalf("stats lack %s: %s", f, stats.Body)
		}
	}
	if got := as("viewer", http.MethodGet, "/"+plain+"/ingest-stats", "").Code; got != http.StatusNotFound {
		t.Errorf("stats of a non-push pipeline: %d", got)
	}

	// Deleting the pipeline drops its spooled data.
	if err := hub.Append(mine, []byte("{}\n")); err != nil {
		t.Fatal(err)
	}
	if got := as("analyst", http.MethodDelete, "/"+mine, "").Code; got != http.StatusOK {
		t.Fatalf("delete: %d", got)
	}
	if st := hub.SpoolStats(mine); st.Records != 0 {
		t.Fatalf("spool kept %d records of a deleted pipeline", st.Records)
	}
}

func TestIngestBaseURL(t *testing.T) {
	req := func(host string) *http.Request {
		r := httptest.NewRequest(http.MethodGet, "/api/ingest/info", nil)
		r.Host = host
		return r
	}
	cases := []struct {
		appURL, ingestAddr, host, want string
	}{
		{"https://ch.example.com", "", "ch.example.com", "https://ch.example.com/ingest"},
		{"https://ch.example.com/", "", "other", "https://ch.example.com/ingest"},
		{"https://ch.example.com", ":4318", "ch.example.com", "https://ch.example.com:4318/ingest"},
		{"http://localhost:3488", "", "10.0.0.5:3488", "http://10.0.0.5:3488/ingest"},
		{"http://localhost:3488", "", "localhost:3488", "http://localhost:3488/ingest"},
		{"http://localhost:3488", "0.0.0.0:4318", "box:3488", "http://box:4318/ingest"},
	}
	for _, c := range cases {
		got := IngestBaseURL(&config.Config{AppURL: c.appURL, IngestAddr: c.ingestAddr}, req(c.host))
		if got != c.want {
			t.Errorf("%s + %q via %s: %s, want %s", c.appURL, c.ingestAddr, c.host, got, c.want)
		}
	}
}
