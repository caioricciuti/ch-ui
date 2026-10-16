package pipelines

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// liveCH returns a query helper for CHUI_TEST_CLICKHOUSE_URL with the
// passwordless default user and a scratch database dropped at the end, or
// skips the test.
func liveCH(t *testing.T, prefix string) (func(string) []map[string]interface{}, string) {
	t.Helper()
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CHUI_TEST_CLICKHOUSE_URL not set")
	}
	if u := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"); u != "" && u != "default" || os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD") != "" {
		t.Skip("the live routing test needs the passwordless default user")
	}
	client := connector.NewCHClient(endpoint, false)
	exec := func(sql string) []map[string]interface{} {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, "default", "", nil)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		return res.Data
	}
	dbName := fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano())
	exec("CREATE DATABASE " + dbName)
	t.Cleanup(func() { exec("DROP DATABASE IF EXISTS " + dbName) })
	return exec, dbName
}

// End to end on a live ClickHouse: an ingest logs pipeline routes ERROR
// records to one exporter table and the rest (the unmatched output) to
// another, each created by its own sink.
func TestLiveIngestLogsRouting(t *testing.T) {
	shortBackoff(t)
	exec, dbName := liveCH(t, "chui_route")

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	pipelineAccount(t, db, conn)

	id := routedPipeline(t, db, conn, "source_ingest", map[string]interface{}{"signal": "logs", "ttl_days": 1}, nil,
		`{"mode":"first","unmatched":"output","routes":[{"id":"errors","name":"Errors","rules":[{"field":"SeverityText","op":"eq","value":"ERROR"}]}]}`,
		[]branchDef{{handle: "errors", table: "otel_errors"}, {handle: "unmatched", table: "otel_rest"}})
	// routedPipeline writes to database "d"; point the sinks at the scratch
	// database and let them create their tables.
	nodes, edges, err := db.GetPipelineGraph(id)
	if err != nil {
		t.Fatal(err)
	}
	for i, n := range nodes {
		if n.NodeType == "sink_clickhouse" {
			nodes[i].ConfigEncrypted = strings.Replace(strings.Replace(n.ConfigEncrypted, `"database":"d"`, `"database":"`+dbName+`"`, 1), `{`, `{"create_table":true,`, 1)
		}
	}
	if err := db.SavePipelineGraph(id, nodes, edges, ""); err != nil {
		t.Fatal(err)
	}

	hub := testHub(t, t.TempDir(), fastSpool())
	cfg := &config.Config{AppSecretKey: "test-secret", DatabasePath: filepath.Join(t.TempDir(), "ch-ui.db")}
	r := NewRunner(db, agent.Gateway, cfg)
	r.SetHub(hub)
	r.proAccess = proActive
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	waitFor(t, "endpoint", func() bool { return hub.Endpoint(id) != nil })

	// What the OTLP receiver does with an OTLP/JSON request.
	now := time.Now().UnixNano()
	var recs []string
	for i, sev := range []string{"INFO", "ERROR", "WARN", "ERROR", "DEBUG"} {
		recs = append(recs, fmt.Sprintf(`{"timeUnixNano":"%d","severityText":"%s","body":{"stringValue":"line %d"}}`, now+int64(i), sev, i))
	}
	body := `{"resourceLogs":[{"resource":{"attributes":[{"key":"service.name","value":{"stringValue":"checkout"}}]},"scopeLogs":[{"scope":{"name":"app"},"logRecords":[` + strings.Join(recs, ",") + `]}]}]}`
	rows, err := otlp.DecodeLogsJSON([]byte(body), otlp.DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	var block []byte
	for i := range rows {
		block = otlp.AppendLogJSON(block, &rows[i])
	}
	if err := hub.Append(id, block); err != nil {
		t.Fatal(err)
	}

	count := func(table string) string {
		if len(exec("SELECT 1 FROM system.tables WHERE database = '"+dbName+"' AND name = '"+table+"'")) == 0 {
			return "none"
		}
		return fmt.Sprint(exec("SELECT count() AS n FROM " + dbName + "." + table)[0]["n"])
	}
	deadline := time.Now().Add(30 * time.Second)
	for count("otel_errors") != "2" || count("otel_rest") != "3" {
		if time.Now().After(deadline) {
			p, _ := db.GetPipelineByID(id)
			t.Fatalf("errors %s, rest %s rows (pipeline status %s, last error %v)", count("otel_errors"), count("otel_rest"), p.Status, p.LastError)
		}
		time.Sleep(200 * time.Millisecond)
	}
	var got []string
	for _, table := range []string{"otel_errors", "otel_rest"} {
		for _, row := range exec("SELECT Body, SeverityText FROM " + dbName + "." + table + " ORDER BY Timestamp") {
			got = append(got, fmt.Sprintf("%s:%v/%v", table, row["Body"], row["SeverityText"]))
		}
	}
	if want := "otel_errors:line 1/ERROR otel_errors:line 3/ERROR otel_rest:line 0/INFO otel_rest:line 2/WARN otel_rest:line 4/DEBUG"; strings.Join(got, " ") != want {
		t.Fatalf("rows\n got %s\nwant %s", strings.Join(got, " "), want)
	}
	waitFor(t, "spool empty", func() bool { return hub.SpoolStats(id).Segments == 0 })
	if s := r.SinkStatuses(id); len(s) != 2 || s[0].Rows != 2 || s[1].Rows != 3 {
		t.Fatalf("sink statuses %+v", s)
	}
}

// On a live ClickHouse a plain MergeTree table created by the sink drops a
// replayed batch (same token) and keeps identical batches sent without a
// token.
func TestLiveCreatedTableDedupsReplay(t *testing.T) {
	exec, dbName := liveCH(t, "chui_route_dedup")
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	pipelineAccount(t, db, conn)
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": dbName, "table": "events", "create_table": true}}
	batch := func(token string) Batch {
		return Batch{Records: []Record{{Data: map[string]interface{}{"v": float64(1)}, RawJSON: []byte(`{"v":1}`)}, {RawJSON: []byte(`{"v":2}`)}}, DedupToken: token}
	}
	for _, tok := range []string{"seg-1:sink-a", "seg-1:sink-a", "", ""} {
		if _, err := sink.WriteBatch(context.Background(), cfg, batch(tok)); err != nil {
			t.Fatal(err)
		}
	}
	if n := fmt.Sprint(exec("SELECT count() AS n FROM " + dbName + ".events")[0]["n"]); n != "6" {
		t.Fatalf("rows = %s, want 6 (2 once by token, then 2 + 2 without a token)", n)
	}
}
