package pipelines

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// Against a live ClickHouse (CHUI_TEST_CLICKHOUSE_URL): identifiers with
// backticks are escaped, and a retried batch with the same dedup token is
// inserted once on a table with non_replicated_deduplication_window.
func TestSinkLiveDedupAndEscaping(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CHUI_TEST_CLICKHOUSE_URL not set")
	}
	user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}
	pass := os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	client := connector.NewCHClient(endpoint, false)
	exec := func(sql string) [][]string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, user, pass, nil)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		var out [][]string
		for _, row := range res.Data {
			var cells []string
			for _, m := range res.Meta {
				cells = append(cells, fmt.Sprint(row[m.Name]))
			}
			out = append(out, cells)
		}
		return out
	}

	dbName := fmt.Sprintf("chui_pipe_test_%d", time.Now().UnixNano())
	exec("CREATE DATABASE " + dbName)
	t.Cleanup(func() { exec("DROP DATABASE IF EXISTS " + dbName) })

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	if user == "default" && pass == "" {
		pipelineAccount(t, db, conn)
	} else {
		t.Skip("live dedup test needs the passwordless default user")
	}
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")

	// Auto-create with hostile identifiers.
	weird := "we`ird\\tbl"
	cfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": dbName, "table": weird, "create_table": true}}
	rec := Record{Data: map[string]interface{}{"a`b": "x", "n": float64(1)}}
	if _, err := sink.WriteBatch(context.Background(), cfg, Batch{Records: []Record{rec}}); err != nil {
		t.Fatalf("write to escaped table: %v", err)
	}
	rows := exec("SELECT count() FROM system.tables WHERE database = '" + dbName + "' AND name = 'we`ird\\\\tbl'")
	if len(rows) != 1 || rows[0][0] != "1" {
		t.Fatalf("escaped table not created: %v", rows)
	}

	// Dedup: same token twice inserts once.
	exec("CREATE TABLE " + dbName + ".dedup (id UInt32) ENGINE = MergeTree ORDER BY id SETTINGS non_replicated_deduplication_window = 100")
	dcfg := ConnectorConfig{Fields: map[string]interface{}{"connection_id": conn, "database": dbName, "table": "dedup"}}
	batch := Batch{Records: []Record{{RawJSON: []byte(`{"id":1}`)}, {RawJSON: []byte(`{"id":2}`)}}, DedupToken: "chui-test-token-1"}
	for i := 0; i < 2; i++ {
		if _, err := sink.WriteBatch(context.Background(), dcfg, batch); err != nil {
			t.Fatal(err)
		}
	}
	batch.DedupToken = "chui-test-token-2"
	if _, err := sink.WriteBatch(context.Background(), dcfg, batch); err != nil {
		t.Fatal(err)
	}
	rows = exec("SELECT count() FROM " + dbName + ".dedup")
	if rows[0][0] != "4" {
		t.Fatalf("rows = %s, want 4 (one duplicate dropped)", strings.Join(rows[0], ","))
	}
}
