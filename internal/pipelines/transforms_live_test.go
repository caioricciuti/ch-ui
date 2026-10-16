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
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// End to end on a live ClickHouse (CHUI_TEST_CLICKHOUSE_URL, passwordless
// default user): an ingest logs pipeline with parse(logfmt) ->
// map(severity_from) -> filter(drop DEBUG) writes OTLP log records into the
// exporter table, transformed, without the debug lines.
func TestLiveIngestLogsWithTransforms(t *testing.T) {
	shortBackoff(t)
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CHUI_TEST_CLICKHOUSE_URL not set")
	}
	if u := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"); u != "" && u != "default" || os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD") != "" {
		t.Skip("the live transform test needs the passwordless default user")
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
	dbName := fmt.Sprintf("chui_proc_%d", time.Now().UnixNano())
	exec("CREATE DATABASE " + dbName)
	t.Cleanup(func() { exec("DROP DATABASE IF EXISTS " + dbName) })

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	pipelineAccount(t, db, conn)

	id, err := db.CreatePipeline("logs with transforms", "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	saveChain(t, db, id, "source_ingest", map[string]interface{}{"signal": "logs", "ttl_days": 1},
		map[string]interface{}{"database": dbName, "table": "otel_logs", "create_table": true},
		[]tnode{
			{id: "parse", typ: transform.TypeParse, label: "Parse", cfg: jsonCfg(`{"field":"Body","format":"logfmt"}`)},
			{id: "map", typ: transform.TypeMap, label: "Severity", cfg: jsonCfg(`{"severity_from":"LogAttributes.level","set":[{"field":"ResourceAttributes['deployment.environment']","value":"prod"}]}`)},
			{id: "filter", typ: transform.TypeFilter, label: "No debug", cfg: jsonCfg(`{"mode":"drop","rules":[{"field":"SeverityText","op":"eq","value":"DEBUG"}]}`)},
		})

	hub := testHub(t, t.TempDir(), fastSpool())
	cfg := &config.Config{AppSecretKey: "test-secret", DatabasePath: filepath.Join(t.TempDir(), "ch-ui.db")}
	r := NewRunner(db, agent.Gateway, cfg)
	r.SetHub(hub)
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
	waitFor(t, "endpoint", func() bool { return hub.Endpoint(id) != nil })

	// What the OTLP receiver does with an OTLP/JSON request.
	now := time.Now().UnixNano()
	var recs []string
	for i, line := range []string{
		`level=info msg=\"order placed\" order_id=17`,
		`level=debug msg=\"cache miss\"`,
		`level=error msg=\"payment failed\" code=502`,
		`level=warning msg=slow dur_ms=1200`,
		`not logfmt at all`,
	} {
		recs = append(recs, fmt.Sprintf(`{"timeUnixNano":"%d","body":{"stringValue":"%s"},"attributes":[{"key":"thread","value":{"stringValue":"main"}}]}`, now+int64(i), line))
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

	deadline := time.Now().Add(30 * time.Second)
	for {
		if len(exec("SELECT 1 FROM system.tables WHERE database = '"+dbName+"' AND name = 'otel_logs'")) == 1 &&
			fmt.Sprint(exec("SELECT count() AS n FROM " + dbName + ".otel_logs")[0]["n"]) == "4" {
			break
		}
		if time.Now().After(deadline) {
			p, _ := db.GetPipelineByID(id)
			t.Fatalf("rows never reached 4 (pipeline status %s, last error %v)", p.Status, p.LastError)
		}
		time.Sleep(200 * time.Millisecond)
	}

	got := exec("SELECT Body, SeverityText, SeverityNumber, LogAttributes['msg'] AS msg, LogAttributes['thread'] AS thread, ResourceAttributes['deployment.environment'] AS env, ServiceName FROM " + dbName + ".otel_logs ORDER BY Timestamp")
	want := []string{
		"INFO 9 order placed main prod checkout",
		"ERROR 17 payment failed main prod checkout",
		"WARN 13 slow main prod checkout",
		" 0  main prod checkout", // not logfmt: kept unchanged, counted as an error
	}
	for i, row := range got {
		line := fmt.Sprintf("%v %v %v %v %v %v", row["SeverityText"], row["SeverityNumber"], row["msg"], row["thread"], row["env"], row["ServiceName"])
		if line != want[i] {
			t.Errorf("row %d (%v): %q, want %q", i, row["Body"], line, want[i])
		}
	}
	counters := r.TransformCounters(id)
	if len(counters) != 3 || counters[0].Errors != 1 || counters[2].Dropped != 1 || counters[2].Out != 4 {
		t.Fatalf("counters %+v", counters)
	}
}
