package pipelines

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// liveEnv is a disposable database on a live ClickHouse
// (CHUI_TEST_CLICKHOUSE_URL, passwordless default user) and a runner whose
// sink writes to it through a real gateway.
type liveEnv struct {
	db     *database.DB
	conn   string
	agent  *testutil.Agent
	cfg    *config.Config
	dbName string
	exec   func(sql string) string
}

func newLiveEnv(t *testing.T) *liveEnv {
	t.Helper()
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("CHUI_TEST_CLICKHOUSE_URL not set")
	}
	if os.Getenv("CHUI_TEST_CLICKHOUSE_USER") != "" && os.Getenv("CHUI_TEST_CLICKHOUSE_USER") != "default" || os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD") != "" {
		t.Skip("live pipeline tests need the passwordless default user")
	}
	client := connector.NewCHClient(endpoint, false)
	exec := func(sql string) string {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, "default", "", nil)
		if err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
		var cells []string
		for _, row := range res.Data {
			for _, m := range res.Meta {
				cells = append(cells, fmt.Sprint(row[m.Name]))
			}
		}
		return strings.Join(cells, ",")
	}
	dbName := fmt.Sprintf("chui_pipe_feat_%d", rand.New(rand.NewSource(time.Now().UnixNano())).Int63())
	exec("CREATE DATABASE " + dbName)
	t.Cleanup(func() { exec("DROP DATABASE IF EXISTS " + dbName) })

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	pipelineAccount(t, db, conn)
	return &liveEnv{
		db: db, conn: conn, agent: agent, dbName: dbName, exec: exec,
		cfg: &config.Config{AppSecretKey: "test-secret", DatabasePath: filepath.Join(t.TempDir(), "ch-ui.db")},
	}
}

// One bad row in a batch for a typed table: the good rows land and the bad
// one is in <table>_dlq with its error and position.
func TestLiveDeadLetterIsolation(t *testing.T) {
	shortBackoff(t)
	env := newLiveEnv(t)
	env.exec("CREATE TABLE " + env.dbName + ".events (id UInt32, ts DateTime, name String) ENGINE = MergeTree ORDER BY id")
	id := graphPipeline(t, env.db, env.conn, "source_webhook", map[string]interface{}{}, map[string]interface{}{"database": env.dbName, "table": "events"})

	r := NewRunner(env.db, env.agent.Gateway, env.cfg)
	var acks atomic.Int32
	lines := []string{
		`{"id":1,"ts":"2025-01-01 00:00:00","name":"a"}`,
		`{"id":2,"ts":"2025-01-01 00:00:01","name":"b"}`,
		`{"id":"not a number","ts":"2025-01-01 00:00:02","name":"bad"}`,
		`{"id":4,"ts":"2025-01-01 00:00:03","name":"d"}`,
		`{"id":5,"ts":"2025-01-01 00:00:04","name":"e"}`,
	}
	var recs []Record
	for i, l := range lines {
		recs = append(recs, Record{RawJSON: []byte(l), Position: fmt.Sprintf("line-%d", i+1)})
	}
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, out chan<- Batch) error {
			select {
			case out <- Batch{Records: recs, Ack: func() { acks.Add(1) }, DedupToken: "chui-live-dlq"}:
			case <-ctx.Done():
			}
			<-ctx.Done()
			return nil
		}}, nil
	}
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "batch acked", func() bool { return acks.Load() == 1 })
	r.Stop()

	if got := env.exec("SELECT arrayStringConcat(groupArray(toString(id)), '-') FROM (SELECT id FROM " + env.dbName + ".events ORDER BY id)"); got != "1-2-4-5" {
		t.Fatalf("good rows = %s", got)
	}
	got := env.exec("SELECT count(), any(pipeline_id), any(source_position), any(raw), any(error) LIKE 'Code: 27.%' FROM " + env.dbName + ".events_dlq")
	want := fmt.Sprintf("1,%s,line-3,%s,1", id, lines[2])
	if got != want {
		t.Fatalf("dlq = %s\nwant  %s", got, want)
	}
	if n, _ := env.db.GetPipelineDLQRows(id); n != 1 {
		t.Fatalf("dlq_rows = %d", n)
	}
}

// ClickHouse infers real types from sample records with DESC format.
func TestLiveInferSchema(t *testing.T) {
	env := newLiveEnv(t)
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	s, err := InferSchema(ctx, env.agent.Gateway, env.conn, "default", "", []map[string]interface{}{
		{"event_id": float64(1), "created_at": "2025-01-01 10:00:00", "name": "it's", "tags": []interface{}{"a"}, "score": 1.5},
		{"event_id": float64(2), "created_at": "2025-01-02 11:00:00", "name": nil, "tags": []interface{}{}, "score": float64(2)},
	})
	if err != nil {
		t.Fatal(err)
	}
	types := map[string]string{}
	for _, c := range s.Columns {
		types[c.Name] = c.Type
	}
	if types["event_id"] != "Int64" || !strings.HasPrefix(types["created_at"], "DateTime") || types["name"] != "String" || types["score"] != "Float64" || !strings.HasPrefix(types["tags"], "Array(") {
		t.Fatalf("types: %v", types)
	}
	if s.OrderBy != "(`created_at`, `event_id`)" || s.PartitionBy != "toYYYYMM(`created_at`)" {
		t.Fatalf("keys: %s / %s", s.OrderBy, s.PartitionBy)
	}
	// The inferred types create a valid table.
	cols := make([]string, len(s.Columns))
	for i, c := range s.Columns {
		cols[i] = quoteIdent(c.Name) + " " + c.Type
	}
	env.exec(fmt.Sprintf("CREATE TABLE %s.inferred (%s) ENGINE = MergeTree PARTITION BY %s ORDER BY %s", env.dbName, strings.Join(cols, ", "), s.PartitionBy, s.OrderBy))
}

// A database source writing to ClickHouse resumes from its checkpoint: two
// runs over a growing source table leave every row exactly once in a plain
// MergeTree table (no deduplication to hide repeats).
func TestLiveCheckpointResumeNoDuplicates(t *testing.T) {
	env := newLiveEnv(t)
	path, src := sqliteEvents(t, 3)
	sink := map[string]interface{}{
		"database": env.dbName, "table": "copy", "create_table": true,
		"create_table_columns":  []interface{}{map[string]interface{}{"name": "id", "type": "Int64"}, map[string]interface{}{"name": "name", "type": "String"}},
		"create_table_order_by": "id",
	}
	id := graphPipeline(t, env.db, env.conn, "source_database", sqliteSourceConfig(path), sink)
	count := func() string { return env.exec("SELECT count(), uniqExact(id) FROM " + env.dbName + ".copy") }

	run := func(want string) {
		t.Helper()
		r := NewRunner(env.db, env.agent.Gateway, env.cfg)
		if err := r.StartPipeline(id); err != nil {
			t.Fatal(err)
		}
		deadline := time.Now().Add(15 * time.Second)
		for env.exec("SELECT count() FROM system.tables WHERE database = '"+env.dbName+"' AND name = 'copy'") != "1" || count() != want {
			if time.Now().After(deadline) {
				t.Fatalf("rows = %s, want %s", count(), want)
			}
			time.Sleep(200 * time.Millisecond)
		}
		time.Sleep(1500 * time.Millisecond) // one more poll adds nothing
		if err := r.StopPipeline(id); err != nil {
			t.Fatal(err)
		}
		if got := count(); got != want {
			t.Fatalf("after an extra poll rows = %s, want %s", got, want)
		}
	}
	run("3,3")
	addEvents(t, src, 4, 6)
	run("6,6")

	// The sink check sees the table and the grants.
	sinkNode := NewClickHouseSink(env.agent.Gateway, env.db, "test-secret")
	checks := sinkNode.Check(context.Background(), ConnectorConfig{Fields: map[string]interface{}{"connection_id": env.conn, "database": env.dbName, "table": "copy"}})
	for _, c := range checks {
		if c.Status != "ok" {
			t.Fatalf("check %s: %s %s", c.Name, c.Status, c.Detail)
		}
	}
	missing := sinkNode.Check(context.Background(), ConnectorConfig{Fields: map[string]interface{}{"connection_id": env.conn, "database": env.dbName + "_nope", "table": "x"}})
	if last := missing[len(missing)-1]; last.Name != "Database" || last.Status != "fail" {
		t.Fatalf("missing database check: %+v", missing)
	}
}
