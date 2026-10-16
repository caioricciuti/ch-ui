package ingest

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/pipelines"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// End to end against a live ClickHouse (CHUI_TEST_CLICKHOUSE_URL,
// passwordless default user): one ingest logs pipeline, a Filebeat-like
// bulk request and a Splunk HEC request with the same key, rows in
// otel_logs that Telemetry's search reads.
func TestLiveIngestESAndHEC(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CHUI_TEST_CLICKHOUSE_URL not set")
	}
	if u := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"); u != "" && u != "default" || os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD") != "" {
		t.Skip("the live ingest test needs the passwordless default user")
	}
	client := connector.NewCHClient(endpoint, false)
	query := func(sql string) []map[string]interface{} {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, "default", "", nil)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return res.Data
	}
	dbName := fmt.Sprintf("chui_es_%d", time.Now().UnixNano())
	query("CREATE DATABASE " + dbName)
	t.Cleanup(func() { query("DROP DATABASE IF EXISTS " + dbName) })

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	enc, err := crypto.Encrypt("", "test-secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetBackgroundCredential(conn, database.BackgroundCredential{Worker: "pipeline", Mode: "service_account", Username: "default", EncryptedPassword: enc}); err != nil {
		t.Fatal(err)
	}
	pid, err := db.CreatePipeline("ingest-es", "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	nodes := []database.PipelineNode{
		{ID: pid + "-src", NodeType: "source_ingest", ConfigEncrypted: `{"signal":"logs","ttl_days":30}`},
		{ID: pid + "-sink", NodeType: "sink_clickhouse", ConfigEncrypted: fmt.Sprintf(`{"database":%q,"table":"otel_logs","create_table":true}`, dbName)},
	}
	edges := []database.PipelineEdge{{ID: pid + "-e", SourceNodeID: pid + "-src", TargetNodeID: pid + "-sink"}}
	if err := db.SavePipelineGraph(pid, nodes, edges, ""); err != nil {
		t.Fatal(err)
	}
	key, hash, prefix, err := GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.CreateIngestKey(pid, "live", hash, prefix, "tester"); err != nil {
		t.Fatal(err)
	}

	sp, err := spool.Open(t.TempDir(), spool.Options{SegmentMaxAge: 50 * time.Millisecond})
	if err != nil {
		t.Fatal(err)
	}
	hub := pipelines.NewHub(sp)
	cfg := &config.Config{AppSecretKey: "test-secret", DatabasePath: filepath.Join(t.TempDir(), "ch-ui.db")}
	runner := pipelines.NewRunner(db, agent.Gateway, cfg)
	runner.SetHub(hub)
	if err := runner.StartPipeline(pid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { runner.Stop(); hub.Close() })
	deadline := time.Now().Add(10 * time.Second)
	for hub.Endpoint(pid) == nil {
		if time.Now().After(deadline) {
			t.Fatal("ingest endpoint not registered")
		}
		time.Sleep(20 * time.Millisecond)
	}

	r := chi.NewRouter()
	r.Route("/ingest", NewReceiver(db, hub).Routes)
	post := func(path, auth, ct string, body []byte) string {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(gz(body)))
		req.Header.Set("Content-Type", ct)
		req.Header.Set("Content-Encoding", "gzip")
		req.Header.Set("Authorization", auth)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
		return rec.Body.String()
	}
	// Current timestamps: rows past the 30 day TTL are dropped on insert.
	now := time.Now().UTC()
	bulk := fmt.Sprintf(`{"create":{"_index":"filebeat-8.15.0"}}
{"@timestamp":%q,"message":"filebeat line","log":{"level":"warn","file":{"path":"/var/log/app.log"}},"host":{"name":"web-1"},"service":{"name":"checkout"}}
{"index":{"_index":"logstash-app"}}
{"@timestamp":%q,"message":"logstash line","host":{"name":"web-2"}}
`, now.Format(time.RFC3339Nano), now.Add(time.Millisecond).Format(time.RFC3339Nano))
	if resp := post("/ingest/es/_bulk", basic("elastic", key), "application/x-ndjson", []byte(bulk)); !strings.Contains(resp, `"errors":false`) {
		t.Fatalf("bulk: %s", resp)
	}
	hecBody := fmt.Sprintf(`{"time":%d.5,"host":"splunk-host","sourcetype":"app","event":"hec line one"}{"time":%d,"host":"splunk-host","event":{"message":"hec line two","level":"error"}}`, now.Unix(), now.Unix())
	post("/ingest/services/collector/event", "Splunk "+key, "application/json", []byte(hecBody))

	deadline = time.Now().Add(30 * time.Second)
	for {
		rows := query("SELECT count() AS n FROM system.tables WHERE database = '" + dbName + "' AND name = 'otel_logs'")
		if len(rows) == 1 && fmt.Sprint(rows[0]["n"]) == "1" {
			if got := query("SELECT count() AS n FROM " + dbName + ".otel_logs"); fmt.Sprint(got[0]["n"]) == "4" {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("rows never reached 4")
		}
		time.Sleep(200 * time.Millisecond)
	}

	got := query("SELECT Body, ServiceName, SeverityText, LogAttributes['es.index'] AS idx, LogAttributes['splunk.sourcetype'] AS st, ResourceAttributes['host.name'] AS host FROM " + dbName + ".otel_logs ORDER BY Body")
	var lines []string
	for _, row := range got {
		lines = append(lines, fmt.Sprintf("%v|%v|%v|%v|%v|%v", row["Body"], row["ServiceName"], row["SeverityText"], row["idx"], row["st"], row["host"]))
	}
	want := []string{
		"filebeat line|checkout|warn|filebeat-8.15.0||web-1",
		"hec line one|splunk-host|||app|splunk-host",
		"hec line two|splunk-host|error|||splunk-host",
		"logstash line|web-2||logstash-app||web-2",
	}
	if strings.Join(lines, "\n") != strings.Join(want, "\n") {
		t.Fatalf("rows:\n%s\nwant:\n%s", strings.Join(lines, "\n"), strings.Join(want, "\n"))
	}

	// Telemetry's own search finds them by service.
	cols := map[string]string{}
	for _, row := range query("DESCRIBE TABLE " + dbName + ".otel_logs") {
		cols[fmt.Sprint(row["name"])] = fmt.Sprint(row["type"])
	}
	lm := telemetry.DefaultLogsMapping()
	src := &telemetry.Source{Kind: telemetry.KindLogs, Database: dbName, Table: "otel_logs", Logs: &lm}
	sql, err := telemetry.SearchSQL(src, cols, telemetry.LogsParams{Q: "service:splunk-host"}, "asc", 100, nil)
	if err != nil {
		t.Fatal(err)
	}
	var bodies []string
	for _, row := range query(sql) {
		bodies = append(bodies, fmt.Sprint(row["body"]))
	}
	sort.Strings(bodies)
	if strings.Join(bodies, "|") != "hec line one|hec line two" {
		t.Fatalf("telemetry search: %v", bodies)
	}
	if v := hub.View(pid, time.Now()); v.Received.Records != 4 || v.Written.Rows != 4 {
		t.Fatalf("stats: %+v", v)
	}
}
