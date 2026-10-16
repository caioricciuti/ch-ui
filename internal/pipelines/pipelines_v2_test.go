package pipelines

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// graphPipeline creates a pipeline with the given source wired to a sink.
func graphPipeline(t *testing.T, db *database.DB, conn, srcType string, src, sink map[string]interface{}) string {
	t.Helper()
	id, err := db.CreatePipeline("p-"+srcType, "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	srcJSON, _ := json.Marshal(src)
	sinkJSON, _ := json.Marshal(sink)
	nodes := []database.PipelineNode{
		{ID: id + "-src", NodeType: srcType, ConfigEncrypted: string(srcJSON)},
		{ID: id + "-sink", NodeType: "sink_clickhouse", ConfigEncrypted: string(sinkJSON)},
	}
	edges := []database.PipelineEdge{{ID: id + "-e", SourceNodeID: id + "-src", TargetNodeID: id + "-sink"}}
	if err := db.SavePipelineGraph(id, nodes, edges, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

// rowSink records the "id" of every written record. A record with "bad"
// set makes the whole insert fail with a ClickHouse parse error; failNext
// makes the next n writes fail with a connection error.
type rowSink struct {
	mu       sync.Mutex
	ids      []string
	letters  []DeadLetter
	writes   int
	failNext int
}

func (s *rowSink) Type() string                   { return "sink_clickhouse" }
func (s *rowSink) Validate(ConnectorConfig) error { return nil }
func (s *rowSink) WriteBatch(_ context.Context, _ ConnectorConfig, b Batch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.writes++
	if s.failNext > 0 {
		s.failNext--
		return 0, errors.New("execute insert: tunnel not connected")
	}
	for _, r := range b.Records {
		if bad, _ := r.Data["bad"].(bool); bad {
			return 0, errors.New("execute insert: ClickHouse error: Code: 27. DB::Exception: Cannot parse input: expected '\"' (at row 1) (CANNOT_PARSE_INPUT_ASSERTION_FAILED)")
		}
	}
	for _, r := range b.Records {
		s.ids = append(s.ids, fmt.Sprint(r.Data["id"]))
	}
	return len(b.Records), nil
}
func (s *rowSink) WriteDeadLetters(_ context.Context, _ ConnectorConfig, _ string, l []DeadLetter, _ string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.letters = append(s.letters, l...)
	return nil
}
func (s *rowSink) DeadLetterTable(ConnectorConfig) (string, string) { return "default", "t_dlq" }
func (s *rowSink) snapshot() ([]string, []DeadLetter) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ids...), append([]DeadLetter(nil), s.letters...)
}

func TestIsDataError(t *testing.T) {
	data := []string{
		"execute insert: ClickHouse error: Code: 27. DB::Exception: Cannot parse input",
		"Code: 41. DB::Exception: Cannot parse datetime",
		"Code: 72. DB::Exception: Unsigned type must not contain '-' symbol",
		"Code: 26. DB::Exception: Cannot parse JSON string",
		"Code: 53. DB::Exception: Type mismatch",
	}
	for _, m := range data {
		if !IsDataError(errors.New(m)) {
			t.Errorf("not a data error: %s", m)
		}
	}
	other := []error{
		errors.New("tunnel not connected"),
		errors.New("query timeout"),
		errors.New("Code: 241. DB::Exception: Memory limit exceeded"),
		errors.New("Code: 497. DB::Exception: Not enough privileges"),
		errors.New("Code: 62. DB::Exception: Syntax error"),
		context.DeadlineExceeded,
		nil,
	}
	for _, e := range other {
		if IsDataError(e) {
			t.Errorf("classified as data error: %v", e)
		}
	}
}

func recordsWithBad(n int, bad ...int) []Record {
	isBad := map[int]bool{}
	for _, b := range bad {
		isBad[b] = true
	}
	recs := make([]Record, n)
	for i := range recs {
		d := map[string]interface{}{"id": i}
		if isBad[i] {
			d["bad"] = true
		}
		raw, _ := json.Marshal(d)
		recs[i] = Record{Data: d, RawJSON: raw, Position: fmt.Sprintf("pos-%d", i)}
	}
	return recs
}

// A batch refused for its data is split: good rows land, bad rows go to the
// dead-letter table with their position, the batch is acked and the
// pipeline's counter grows.
func TestRunnerIsolatesBadRowsIntoDeadLetters(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := webhookPipeline(t, db, conn, "a")
	sink := &rowSink{}
	r := newTestRunner(t, db, cfg, sink)
	var acks atomic.Int32
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, out chan<- Batch) error {
			select {
			case out <- Batch{Records: recordsWithBad(10, 3, 7), Ack: func() { acks.Add(1) }, DedupToken: "tok"}:
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
	metrics := r.GetRunningMetrics(id)
	r.Stop()

	ids, letters := sink.snapshot()
	sort.Strings(ids)
	if strings.Join(ids, ",") != "0,1,2,4,5,6,8,9" {
		t.Fatalf("good rows = %v", ids)
	}
	if len(letters) != 2 || letters[0].Position != "pos-3" || letters[1].Position != "pos-7" {
		t.Fatalf("dead letters = %+v", letters)
	}
	if !strings.HasPrefix(letters[0].Error, "Code: 27.") || !strings.Contains(letters[0].Raw, `"bad":true`) {
		t.Fatalf("dead letter content: %+v", letters[0])
	}
	if metrics.DLQRows.Load() != 2 || metrics.RowsIngested.Load() != 8 {
		t.Fatalf("metrics: dlq %d rows %d", metrics.DLQRows.Load(), metrics.RowsIngested.Load())
	}
	if n, _ := db.GetPipelineDLQRows(id); n != 2 {
		t.Fatalf("dlq_rows = %d, want 2", n)
	}
}

// A transient error during isolation keeps the progress: no good row is
// written twice and the batch is acked once.
func TestIsolationResumesAfterTransientError(t *testing.T) {
	sink := &rowSink{}
	batch := Batch{Records: recordsWithBad(8, 5)}
	_, err := sink.WriteBatch(context.Background(), ConnectorConfig{}, batch)
	iso := newIsolation(len(batch.Records), err)
	sink.failNext = 1
	sink.mu.Lock()
	sink.writes = 0
	sink.mu.Unlock()
	// First sub-write fails transiently.
	if err := iso.run(context.Background(), sink, sink, ConnectorConfig{}, "p", batch); err == nil || IsDataError(err) {
		t.Fatalf("want transient error, got %v", err)
	}
	if err := iso.run(context.Background(), sink, sink, ConnectorConfig{}, "p", batch); err != nil {
		t.Fatal(err)
	}
	ids, letters := sink.snapshot()
	if len(ids) != 7 || len(letters) != 1 {
		t.Fatalf("ids %v letters %d", ids, len(letters))
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Fatalf("row %s written twice", id)
		}
		seen[id] = true
	}
}

// Every row bad: isolation stops at its write budget and dead-letters the
// rest whole instead of issuing one insert per row.
func TestIsolationIsBounded(t *testing.T) {
	n := 5000
	bad := make([]int, n)
	for i := range bad {
		bad[i] = i
	}
	sink := &rowSink{}
	batch := Batch{Records: recordsWithBad(n, bad...)}
	_, err := sink.WriteBatch(context.Background(), ConnectorConfig{}, batch)
	iso := newIsolation(n, err)
	if err := iso.run(context.Background(), sink, sink, ConnectorConfig{}, "p", batch); err != nil {
		t.Fatal(err)
	}
	_, letters := sink.snapshot()
	if len(letters) != n {
		t.Fatalf("letters = %d, want %d", len(letters), n)
	}
	// Ranges already queued when the budget runs out are still tried once
	// (they may be good); depth-first splitting keeps that queue short.
	if iso.writes > maxBisectWrites+maxBisectDepth+1 {
		t.Fatalf("writes = %d, over the budget %d", iso.writes, maxBisectWrites)
	}
}

// With dead_letter off, a data error is retried like any other error.
func TestDeadLetterCanBeTurnedOff(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := graphPipeline(t, db, conn, "source_webhook", map[string]interface{}{}, map[string]interface{}{"database": "default", "table": "t", "dead_letter": false})
	sink := &rowSink{}
	r := newTestRunner(t, db, cfg, sink)
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, out chan<- Batch) error {
			select {
			case out <- Batch{Records: recordsWithBad(4, 1)}:
			case <-ctx.Done():
			}
			<-ctx.Done()
			return nil
		}}, nil
	}
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "retries", func() bool { sink.mu.Lock(); defer sink.mu.Unlock(); return sink.writes >= 3 })
	r.Stop()
	ids, letters := sink.snapshot()
	if len(ids) != 0 || len(letters) != 0 {
		t.Fatalf("ids %v letters %v", ids, letters)
	}
}

// sqliteEvents creates a SQLite file with rows 1..n outside the data dir.
func sqliteEvents(t *testing.T, n int) (string, *sql.DB) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "events.db")
	src, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { src.Close() })
	if _, err := src.Exec("CREATE TABLE events (id INTEGER PRIMARY KEY, name TEXT)"); err != nil {
		t.Fatal(err)
	}
	addEvents(t, src, 1, n)
	return path, src
}

func addEvents(t *testing.T, src *sql.DB, from, to int) {
	t.Helper()
	for i := from; i <= to; i++ {
		if _, err := src.Exec("INSERT INTO events (id, name) VALUES (?, ?)", i, fmt.Sprintf("e%d", i)); err != nil {
			t.Fatal(err)
		}
	}
}

func sqliteSourceConfig(path string) map[string]interface{} {
	return map[string]interface{}{
		"db_type":           "sqlite",
		"connection_string": path,
		"query":             "SELECT id, name FROM events WHERE id > COALESCE(?, 0) ORDER BY id",
		"watermark_column":  "id",
		"poll_interval":     float64(1),
		"batch_size":        float64(2),
	}
}

// The database source resumes from the saved watermark after a restart, so
// no row is sent twice; a reset reads everything again.
func TestDatabaseSourceResumesFromCheckpoint(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	path, src := sqliteEvents(t, 3)
	id := graphPipeline(t, db, conn, "source_database", sqliteSourceConfig(path), map[string]interface{}{"database": "default", "table": "t"})

	run := func(want int) []string {
		t.Helper()
		sink := &rowSink{}
		r := newTestRunner(t, db, cfg, sink)
		if err := r.StartPipeline(id); err != nil {
			t.Fatal(err)
		}
		waitFor(t, fmt.Sprintf("%d rows", want), func() bool { ids, _ := sink.snapshot(); return len(ids) >= want })
		time.Sleep(1500 * time.Millisecond) // another poll: nothing more
		if err := r.StopPipeline(id); err != nil {
			t.Fatal(err)
		}
		ids, _ := sink.snapshot()
		return ids
	}

	if got := run(3); strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("first run: %v", got)
	}
	cps, _ := db.PipelineCheckpointMap(id)
	if wm, _ := decodeWatermark(cps[watermarkCheckpointKey]); wm != int64(3) {
		t.Fatalf("saved watermark = %v (%q)", wm, cps[watermarkCheckpointKey])
	}

	addEvents(t, src, 4, 5)
	if got := run(2); strings.Join(got, ",") != "4,5" {
		t.Fatalf("resumed run: %v, want 4,5", got)
	}

	if n, err := db.DeletePipelineCheckpoints(id); err != nil || n != 1 {
		t.Fatalf("reset: %d %v", n, err)
	}
	if got := run(5); strings.Join(got, ",") != "1,2,3,4,5" {
		t.Fatalf("after reset: %v", got)
	}
}

func TestWatermarkEncoding(t *testing.T) {
	ts := time.Date(2025, 1, 2, 3, 4, 5, 6, time.UTC)
	for _, v := range []interface{}{int64(42), float64(1.5), "abc", ts, true, uint64(7)} {
		enc, err := encodeWatermark(v)
		if err != nil {
			t.Fatal(err)
		}
		back, err := decodeWatermark(enc)
		if err != nil {
			t.Fatal(err)
		}
		if tt, ok := v.(time.Time); ok {
			if !back.(time.Time).Equal(tt) {
				t.Fatalf("time round trip: %v", back)
			}
			continue
		}
		if back != v {
			t.Fatalf("round trip %v (%T) -> %v (%T)", v, v, back, back)
		}
	}
}

// fakeS3 serves objects from memory.
type fakeS3 struct {
	mu   sync.Mutex
	objs map[string]fakeObj
	gets []string
}

type fakeObj struct{ etag, body string }

func (f *fakeS3) put(key, etag, body string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.objs[key] = fakeObj{etag, body}
}

func (f *fakeS3) ListObjects(_ context.Context, _ string, opts minio.ListObjectsOptions) <-chan minio.ObjectInfo {
	f.mu.Lock()
	keys := make([]string, 0, len(f.objs))
	for k := range f.objs {
		if strings.HasPrefix(k, opts.Prefix) {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	infos := make([]minio.ObjectInfo, len(keys))
	for i, k := range keys {
		infos[i] = minio.ObjectInfo{Key: k, ETag: f.objs[k].etag}
	}
	f.mu.Unlock()
	ch := make(chan minio.ObjectInfo, len(infos))
	for _, i := range infos {
		ch <- i
	}
	close(ch)
	return ch
}

func (f *fakeS3) GetObject(_ context.Context, _, key string) (io.ReadCloser, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets = append(f.gets, key)
	o, ok := f.objs[key]
	if !ok {
		return nil, errors.New("no such key")
	}
	return io.NopCloser(strings.NewReader(o.body)), nil
}

// The S3 source saves processed files (with ETag) after their rows are
// written, so a restart reads only new or rewritten files.
func TestS3SourceResumesFromCheckpoint(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	fake := &fakeS3{objs: map[string]fakeObj{}}
	fake.put("in/a.json", "e1", "{\"id\":1}\n{\"id\":2}\n")
	fake.put("in/b.json", "e1", "{\"id\":3}\n")
	fake.put("in/empty.json", "e1", "")
	src := map[string]interface{}{"bucket": "b", "prefix": "in/", "access_key": "a", "secret_key": "s", "format": "ndjson", "poll_interval": float64(1)}
	id := graphPipeline(t, db, conn, "source_s3", src, map[string]interface{}{"database": "default", "table": "t"})

	run := func(want int) []string {
		t.Helper()
		sink := &rowSink{}
		r := newTestRunner(t, db, cfg, sink)
		r.newSource = func(string) (SourceConnector, error) {
			return &S3Source{newClient: func(ConnectorConfig) (s3API, error) { return fake, nil }}, nil
		}
		if err := r.StartPipeline(id); err != nil {
			t.Fatal(err)
		}
		waitFor(t, fmt.Sprintf("%d rows", want), func() bool { ids, _ := sink.snapshot(); return len(ids) >= want })
		time.Sleep(1500 * time.Millisecond)
		if err := r.StopPipeline(id); err != nil {
			t.Fatal(err)
		}
		ids, _ := sink.snapshot()
		return ids
	}

	if got := run(3); strings.Join(got, ",") != "1,2,3" {
		t.Fatalf("first run: %v", got)
	}
	fake.put("in/c.json", "e1", "{\"id\":4}\n")
	fake.put("in/b.json", "e2", "{\"id\":30}\n") // rewritten: new ETag
	fake.mu.Lock()
	fake.gets = nil
	fake.mu.Unlock()
	got := run(2)
	sort.Strings(got)
	if strings.Join(got, ",") != "30,4" {
		t.Fatalf("resumed run: %v, want 30 and 4", got)
	}
	fake.mu.Lock()
	gets := strings.Join(fake.gets, ",")
	fake.mu.Unlock()
	if strings.Contains(gets, "a.json") || strings.Contains(gets, "empty.json") {
		t.Fatalf("processed files read again: %s", gets)
	}
}

func TestS3ProcessedSetIsBounded(t *testing.T) {
	p := loadS3Processed("")
	for i := 0; i < s3MaxTrackedFiles+10; i++ {
		p = p.with(fmt.Sprintf("k%05d", i), "e")
	}
	if len(p.Files) != s3MaxTrackedFiles {
		t.Fatalf("tracked %d files", len(p.Files))
	}
	if p.Floor != "k00009" {
		t.Fatalf("floor = %q", p.Floor)
	}
	back := loadS3Processed(p.encode())
	if !back.done("k00003", "other") || !back.done("k00500", "e") || back.done("k00500", "changed") || back.done("k99999", "e") {
		t.Fatal("done() after reload is wrong")
	}
}

func TestValidateColumnType(t *testing.T) {
	for _, ok := range []string{"String", "Int64", "DateTime64(9)", "Nullable(String)", "Array(Nullable(Int64))", "Map(String, UInt64)", "Enum8('a' = 1, 'b)' = 2)", "Tuple(a String, b Int8)", "Decimal(18, 4)"} {
		if err := validateColumnType(ok); err != nil {
			t.Errorf("rejected %q: %v", ok, err)
		}
	}
	for _, bad := range []string{"", "String DEFAULT 1", "String, x Int8", "String) DEFAULT (1", "Int8; DROP TABLE x", "String -- c", "(String)", "Int8 CODEC(ZSTD)", "Array(String"} {
		if err := validateColumnType(bad); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestSuggestKeys(t *testing.T) {
	order, part := suggestKeys([]SchemaColumn{
		{Name: "user_id", Type: "Int64"},
		{Name: "payload", Type: "String"},
		{Name: "created_at", Type: "DateTime64(9)"},
		{Name: "tags", Type: "Array(String)"},
	})
	if order != "(`created_at`, `user_id`)" || part != "toYYYYMM(`created_at`)" {
		t.Fatalf("keys: %q %q", order, part)
	}
	// Nullable columns cannot be keys.
	order, part = suggestKeys([]SchemaColumn{{Name: "id", Type: "Nullable(Int64)"}, {Name: "ts", Type: "Nullable(DateTime64(3))"}, {Name: "x", Type: "String"}})
	if order != "tuple()" || part != "" {
		t.Fatalf("nullable keys: %q %q", order, part)
	}
}

// Auto-create uses the saved column list instead of inferring from the
// first record.
func TestSinkCreatesTableFromSavedSchema(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var ddl atomic.Value
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.HasPrefix(msg.SQL, "CREATE TABLE") {
			ddl.Store(msg.SQL)
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`), Meta: json.RawMessage(`[]`)}
	})
	pipelineAccount(t, db, conn)
	sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
	cfg := ConnectorConfig{Fields: map[string]interface{}{
		"connection_id": conn, "database": "d", "table": "t", "create_table": true,
		"create_table_columns":      []interface{}{map[string]interface{}{"name": "ts", "type": "DateTime64(3)"}, map[string]interface{}{"name": "n", "type": "Int64"}},
		"create_table_order_by":     "ts",
		"create_table_partition_by": "toYYYYMM(ts)",
	}}
	if err := sink.Validate(cfg); err != nil {
		t.Fatal(err)
	}
	if _, err := sink.WriteBatch(context.Background(), cfg, Batch{Records: []Record{{Data: map[string]interface{}{"zzz": "x"}, RawJSON: []byte(`{"zzz":"x"}`)}}}); err != nil {
		t.Fatal(err)
	}
	got, _ := ddl.Load().(string)
	if !strings.Contains(got, "`ts` DateTime64(3),\n  `n` Int64") || strings.Contains(got, "zzz") || !strings.Contains(got, "PARTITION BY toYYYYMM(ts)") {
		t.Fatalf("DDL: %s", got)
	}

	cfg.Fields["create_table_columns"] = []interface{}{map[string]interface{}{"name": "x", "type": "String DEFAULT 'a'"}}
	if err := sink.Validate(cfg); err == nil {
		t.Fatal("accepted a column type with DEFAULT")
	}
	cfg.Fields["create_table_columns"] = nil
	cfg.Fields["max_error_ratio"] = float64(2)
	if err := sink.Validate(cfg); err == nil {
		t.Fatal("accepted max_error_ratio 2")
	}
}

// Test reads a sample from the source without moving its position.
func TestRunnerTestPipelineSamplesWithoutCheckpoint(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	path, _ := sqliteEvents(t, 30)
	id := graphPipeline(t, db, conn, "source_database", sqliteSourceConfig(path), map[string]interface{}{"database": "default", "table": "t"})
	r := newTestRunner(t, db, cfg, &rowSink{})
	res, err := r.TestPipeline(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if res.SourceError != "" || len(res.Records) != SampleLimit {
		t.Fatalf("sample: %d records, error %q", len(res.Records), res.SourceError)
	}
	// Schema inference gets the whole sample, up to InferSampleLimit.
	if len(res.SinkOrder) != 1 || len(res.SinkRecords[res.SinkOrder[0]]) != 30 {
		t.Fatalf("sink sample: %v, %d rows", res.SinkOrder, len(res.SinkRecords[res.SinkOrder[0]]))
	}
	if cps, _ := db.PipelineCheckpointMap(id); len(cps) != 0 {
		t.Fatalf("test saved a checkpoint: %v", cps)
	}

	// After a saved watermark the sample starts there.
	pos, _ := encodeWatermark(int64(25))
	if err := db.SavePipelineCheckpoint(id, watermarkCheckpointKey, pos); err != nil {
		t.Fatal(err)
	}
	res, err = r.TestPipeline(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Records) != 5 || fmt.Sprint(res.Records[0]["id"]) != "26" {
		t.Fatalf("sample after watermark: %v", res.Records)
	}
}

func TestWebhookSampleReturnsRecentRecords(t *testing.T) {
	w := &WebhookSource{}
	res, err := w.Sample(context.Background(), ConnectorConfig{Fields: map[string]interface{}{"pipeline_id": "nope"}}, 20)
	if err != nil || len(res.Records) != 0 || !strings.Contains(res.Note, "/api/pipelines/webhook/nope") {
		t.Fatalf("stopped webhook sample: %+v %v", res, err)
	}
	recv := &webhookReceiver{}
	webhookRegistry.Store("wh-sample", recv)
	defer webhookRegistry.Delete("wh-sample")
	for i := 0; i < 30; i++ {
		recv.remember([]Record{{Data: map[string]interface{}{"i": i}}})
	}
	res, _ = w.Sample(context.Background(), ConnectorConfig{Fields: map[string]interface{}{"pipeline_id": "wh-sample"}}, 20)
	if len(res.Records) != webhookRecentMax || res.Records[0]["i"] != 10 {
		t.Fatalf("recent: %d, first %v", len(res.Records), res.Records[0])
	}
}
