package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/ingest/spool"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// testHub opens a spool in dir and closes it at the end of the test.
func testHub(t *testing.T, dir string, opts spool.Options) *Hub {
	t.Helper()
	sp, err := spool.Open(dir, opts)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { sp.Close() })
	return NewHub(sp)
}

// dedupCH stands in for ClickHouse with deduplication: a batch whose token
// it has seen is accepted and dropped. hang makes a write insert and then
// block until the pipeline is cancelled, as if the server crashed between
// the insert and the ack.
type dedupCH struct {
	mu       sync.Mutex
	seen     map[string]bool
	attempts map[string]int
	rows     []string
	hang     bool
}

func newDedupCH() *dedupCH {
	return &dedupCH{seen: map[string]bool{}, attempts: map[string]int{}}
}

func (f *dedupCH) Type() string                   { return "sink_clickhouse" }
func (f *dedupCH) Validate(ConnectorConfig) error { return nil }
func (f *dedupCH) WriteBatch(ctx context.Context, _ ConnectorConfig, b Batch) (int, error) {
	f.mu.Lock()
	f.attempts[b.DedupToken]++
	if b.DedupToken == "" || !f.seen[b.DedupToken] {
		f.seen[b.DedupToken] = true
		for _, r := range b.Records {
			f.rows = append(f.rows, string(r.RawJSON))
		}
	}
	hang := f.hang
	f.mu.Unlock()
	if hang {
		<-ctx.Done()
		return 0, ctx.Err()
	}
	return len(b.Records), nil
}

func (f *dedupCH) snapshot() ([]string, map[string]int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	att := map[string]int{}
	for k, v := range f.attempts {
		att[k] = v
	}
	return append([]string(nil), f.rows...), att
}

func fastSpool() spool.Options {
	return spool.Options{SegmentMaxAge: 20 * time.Millisecond, SyncMode: spool.SyncNone}
}

func TestIngestSourceValidate(t *testing.T) {
	src := &IngestSource{}
	for _, sig := range []string{"logs", "traces", "events"} {
		if err := src.Validate(ConnectorConfig{Fields: map[string]interface{}{"signal": sig, "ttl_days": float64(30)}}); err != nil {
			t.Errorf("%s rejected: %v", sig, err)
		}
	}
	for _, bad := range []map[string]interface{}{
		{},
		{"signal": "metrics"},
		{"signal": "logs", "rate_limit_rps": float64(-1)},
		{"signal": "logs", "daily_quota_mb": float64(-5)},
	} {
		if err := src.Validate(ConnectorConfig{Fields: bad}); err == nil {
			t.Errorf("accepted %v", bad)
		}
	}
	if err := (&IngestSource{}).Start(context.Background(), ConnectorConfig{Fields: map[string]interface{}{"signal": "logs"}}, nil); err != ErrNoSpool {
		t.Fatalf("start without spool: %v", err)
	}
}

// A running ingest pipeline registers its endpoint with its limits, drains
// what is spooled (one batch per segment, the segment's dedup token), and
// unregisters when stopped. Data spooled while stopped stays.
func TestIngestPipelineDrainsSpool(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := graphPipeline(t, db, conn, "source_ingest",
		map[string]interface{}{"signal": "events", "rate_limit_rps": float64(5), "daily_quota_mb": float64(2)},
		map[string]interface{}{"database": "d", "table": "t"})
	ch := newDedupCH()
	r := NewRunner(db, nil, cfg)
	r.newSink = func() SinkConnector { return ch }
	hub := testHub(t, t.TempDir(), fastSpool())
	r.SetHub(hub)
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "endpoint registered", func() bool { return hub.Endpoint(id) != nil })
	ep := hub.Endpoint(id)
	if ep.Signal != "events" || ep.RateLimitRPS != 5 || ep.DailyQuotaBytes != 2<<20 {
		t.Fatalf("endpoint: %+v", ep)
	}
	for i := 0; i < 3; i++ {
		if err := hub.Append(id, []byte(fmt.Sprintf("{\"i\":%d}\n{\"i\":%d}\n", 2*i, 2*i+1))); err != nil {
			t.Fatal(err)
		}
	}
	waitFor(t, "rows written", func() bool { rows, _ := ch.snapshot(); return len(rows) == 6 })
	waitFor(t, "spool empty", func() bool { return hub.SpoolStats(id).Segments == 0 })
	_, attempts := ch.snapshot()
	for tok := range attempts {
		if !strings.HasPrefix(tok, "chui-spool:") {
			t.Fatalf("batch token %q is not the segment token", tok)
		}
	}
	if v := hub.View(id, time.Now()); v.Written.Rows != 6 || v.Written.LastWriteAt == nil {
		t.Fatalf("written stats: %+v", v.Written)
	}

	if err := r.StopPipeline(id); err != nil {
		t.Fatal(err)
	}
	if hub.Endpoint(id) != nil {
		t.Fatal("endpoint still registered after stop")
	}
	if err := hub.Append(id, []byte("{\"i\":99}\n")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond)
	if st := hub.SpoolStats(id); st.Records != 1 {
		t.Fatalf("stopped pipeline: spool holds %d records, want 1", st.Records)
	}
	if err := r.DropSpool(id); err != nil {
		t.Fatal(err)
	}
	if st := hub.SpoolStats(id); st.Records != 0 {
		t.Fatalf("after drop: %d records", st.Records)
	}
}

// The crash test: the insert lands, then the process dies before the ack.
// A new spool and runner over the same directory replay the segment with the
// same dedup token, so every row is in "ClickHouse" exactly once and the
// spool ends empty.
func TestIngestDurableAcrossCrashExactlyOnce(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := graphPipeline(t, db, conn, "source_ingest", map[string]interface{}{"signal": "logs"}, map[string]interface{}{"database": "d", "table": "t"})
	dir := filepath.Join(t.TempDir(), "ingest-spool")
	ch := newDedupCH()
	ch.hang = true

	sp1, err := spool.Open(dir, fastSpool())
	if err != nil {
		t.Fatal(err)
	}
	hub1 := NewHub(sp1)
	r1 := NewRunner(db, nil, cfg)
	r1.newSink = func() SinkConnector { return ch }
	r1.SetHub(hub1)
	if err := r1.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "endpoint", func() bool { return hub1.Endpoint(id) != nil })
	const n = 50
	var block strings.Builder
	for i := 0; i < n; i++ {
		fmt.Fprintf(&block, "{\"Body\":\"line %d\"}\n", i)
	}
	if err := hub1.Append(id, []byte(block.String())); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "insert reached ClickHouse", func() bool { rows, _ := ch.snapshot(); return len(rows) == n })

	// Crash: stop without the ack, then drop the spool handle.
	r1.Stop()
	sp1.Close()
	_, attempts := ch.snapshot()
	if len(attempts) != 1 {
		t.Fatalf("tokens before restart: %v", attempts)
	}

	ch.mu.Lock()
	ch.hang = false
	ch.mu.Unlock()
	hub2 := testHub(t, dir, fastSpool())
	if st := hub2.SpoolStats(id); st.Records != 1 {
		t.Fatalf("after restart the spool holds %d records, want the unacked one", st.Records)
	}
	r2 := NewRunner(db, nil, cfg)
	r2.newSink = func() SinkConnector { return ch }
	r2.SetHub(hub2)
	r2.Start() // resumes the pipeline left "running"
	waitFor(t, "replay acked", func() bool { return hub2.SpoolStats(id).Segments == 0 })
	r2.Stop()

	rows, attempts := ch.snapshot()
	if len(rows) != n {
		t.Fatalf("rows = %d, want %d exactly once", len(rows), n)
	}
	if len(attempts) != 1 {
		t.Fatalf("the replay used a different token: %v", attempts)
	}
	for tok, a := range attempts {
		if a != 2 {
			t.Fatalf("token %s written %d times, want 2 (insert, replay)", tok, a)
		}
	}
}

// A webhook pipeline goes through the spool: 200 means spooled, and the
// rows reach the sink with the segment token.
func TestWebhookThroughSpool(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := webhookPipeline(t, db, conn, "wh")
	ch := newDedupCH()
	r := NewRunner(db, nil, cfg)
	r.newSink = func() SinkConnector { return ch }
	hub := testHub(t, t.TempDir(), fastSpool())
	r.SetHub(hub)
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()
	waitFor(t, "webhook registered", func() bool { _, ok := webhookRegistry.Load(id); return ok })
	code, body := postWebhook(id, `[{"a":1},{"a":2}]`)
	if code != 200 || body != `{"accepted":2}` {
		t.Fatalf("post: %d %s", code, body)
	}
	waitFor(t, "rows written", func() bool { rows, _ := ch.snapshot(); return len(rows) == 2 })
	rows, attempts := ch.snapshot()
	if rows[0] != `{"a":1}` || rows[1] != `{"a":2}` {
		t.Fatalf("rows: %v", rows)
	}
	for tok := range attempts {
		if !strings.HasPrefix(tok, "chui-spool:") {
			t.Fatalf("token %q", tok)
		}
	}
}

// For a logs or traces ingest pipeline the sink creates the exporter's
// tables (with the dedup window) and inserts skip unknown fields.
func TestSinkCreatesOTelTables(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	var mu sync.Mutex
	var settings []map[string]string
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		if strings.HasPrefix(msg.SQL, "INSERT") {
			mu.Lock()
			settings = append(settings, msg.Settings)
			mu.Unlock()
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[]`)}
	})
	pipelineAccount(t, db, conn)
	for _, signal := range []string{"logs", "traces"} {
		sink := NewClickHouseSink(agent.Gateway, db, "test-secret")
		cfg := ConnectorConfig{Fields: map[string]interface{}{
			"connection_id": conn, "database": "obs", "table": "otel_" + signal, "create_table": true,
			sinkOTelSignal: signal, sinkOTelTTLDays: 7,
		}}
		if _, err := sink.WriteBatch(context.Background(), cfg, Batch{Records: []Record{{RawJSON: []byte(`{"Body":"x"}`)}}, DedupToken: "tok"}); err != nil {
			t.Fatal(err)
		}
	}
	var creates []string
	for _, m := range agent.Messages() {
		if strings.HasPrefix(m.SQL, "CREATE") {
			creates = append(creates, m.SQL)
		}
	}
	if len(creates) != 4 {
		t.Fatalf("CREATE statements = %d, want 4 (logs, spans, lookup, view)", len(creates))
	}
	for i, c := range creates[:3] {
		if !strings.Contains(c, "non_replicated_deduplication_window = 1000") || !strings.Contains(c, "toIntervalDay(7)") {
			t.Fatalf("create %d lacks the dedup window or TTL:\n%s", i, c)
		}
	}
	if !strings.Contains(creates[0], "`obs`.`otel_logs`") || !strings.Contains(creates[3], "MATERIALIZED VIEW") {
		t.Fatalf("unexpected statements:\n%s", strings.Join(creates, "\n---\n"))
	}
	mu.Lock()
	defer mu.Unlock()
	for _, s := range settings {
		if s["input_format_skip_unknown_fields"] != "1" || s["insert_deduplication_token"] != "tok" {
			t.Fatalf("insert settings: %v", s)
		}
	}
}

func postWebhook(id, body string) (int, string) {
	rec := httptest.NewRecorder()
	HandleWebhook(rec, httptest.NewRequest(http.MethodPost, "/api/pipelines/webhook/"+id, strings.NewReader(body)))
	return rec.Code, rec.Body.String()
}
