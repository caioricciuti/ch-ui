package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/ingest/otlp"
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
)

// tnode is a transform node for chainPipeline.
type tnode struct {
	id, typ, label string
	cfg            map[string]interface{}
}

// chainPipeline saves source -> transforms... -> sink, connected in order.
func chainPipeline(t *testing.T, db *database.DB, conn, srcType string, src map[string]interface{}, chain []tnode) string {
	t.Helper()
	id, err := db.CreatePipeline("p-chain", "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	saveChain(t, db, id, srcType, src, map[string]interface{}{"database": "d", "table": "t"}, chain)
	return id
}

// saveChain saves the graph source -> transforms... -> sink of pipeline id.
func saveChain(t *testing.T, db *database.DB, id, srcType string, src, sink map[string]interface{}, chain []tnode) {
	t.Helper()
	srcJSON, _ := json.Marshal(src)
	sinkJSON, _ := json.Marshal(sink)
	nodes := []database.PipelineNode{{ID: id + "-src", NodeType: srcType, Label: "Source", ConfigEncrypted: string(srcJSON)}}
	prev := id + "-src"
	var edges []database.PipelineEdge
	for i, n := range chain {
		cfg, _ := json.Marshal(n.cfg)
		nid := id + "-" + n.id
		nodes = append(nodes, database.PipelineNode{ID: nid, NodeType: n.typ, Label: n.label, ConfigEncrypted: string(cfg)})
		edges = append(edges, database.PipelineEdge{ID: fmt.Sprintf("%s-e%d", id, i), SourceNodeID: prev, TargetNodeID: nid})
		prev = nid
	}
	nodes = append(nodes, database.PipelineNode{ID: id + "-sink", NodeType: "sink_clickhouse", Label: "Sink", ConfigEncrypted: string(sinkJSON)})
	edges = append(edges, database.PipelineEdge{ID: id + "-elast", SourceNodeID: prev, TargetNodeID: id + "-sink"})
	if err := db.SavePipelineGraph(id, nodes, edges, ""); err != nil {
		t.Fatal(err)
	}
}

func jsonCfg(s string) map[string]interface{} {
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		panic(err)
	}
	return m
}

func TestResolveChain(t *testing.T) {
	n := func(id, typ string) database.PipelineNode {
		return database.PipelineNode{ID: id, NodeType: typ, Label: strings.ToUpper(id)}
	}
	e := func(from, to string) database.PipelineEdge {
		return database.PipelineEdge{ID: from + to, SourceNodeID: from, TargetNodeID: to}
	}
	src, snk := n("s", "source_webhook"), n("k", "sink_clickhouse")
	a, b := n("a", transform.TypeParse), n("b", transform.TypeFilter)
	cases := []struct {
		name  string
		nodes []database.PipelineNode
		edges []database.PipelineEdge
		chain []string
		err   string
	}{
		{name: "source to sink", nodes: []database.PipelineNode{snk, src}, edges: []database.PipelineEdge{e("s", "k")}, chain: []string{}},
		{name: "duplicate edge counts once", nodes: []database.PipelineNode{src, snk}, edges: []database.PipelineEdge{e("s", "k"), e("s", "k")}, chain: []string{}},
		{name: "two transforms in edge order", nodes: []database.PipelineNode{b, snk, a, src}, edges: []database.PipelineEdge{e("b", "k"), e("s", "a"), e("a", "b")}, chain: []string{"a", "b"}},
		{name: "edge to a deleted node is ignored", nodes: []database.PipelineNode{src, snk}, edges: []database.PipelineEdge{e("s", "k"), e("s", "gone")}, chain: []string{}},
		{name: "not connected", nodes: []database.PipelineNode{src, snk}, err: "source node is not connected to sink node"},
		{name: "dangling transform", nodes: []database.PipelineNode{src, a, snk}, edges: []database.PipelineEdge{e("s", "k")}, err: `node "A" is not connected between the source and the sink`},
		{name: "chain stops early", nodes: []database.PipelineNode{src, a, snk}, edges: []database.PipelineEdge{e("s", "a")}, err: `node "A" is not connected to anything downstream`},
		{name: "branch", nodes: []database.PipelineNode{src, a, b, snk}, edges: []database.PipelineEdge{e("s", "a"), e("s", "b"), e("a", "k"), e("b", "k")}, err: `node "S" has more than one outgoing connection`},
		{name: "merge", nodes: []database.PipelineNode{src, a, b, snk}, edges: []database.PipelineEdge{e("s", "a"), e("a", "k"), e("b", "k")}, err: `node "K" has more than one incoming connection`},
		{name: "cycle", nodes: []database.PipelineNode{src, a, b, snk}, edges: []database.PipelineEdge{e("s", "a"), e("a", "b"), e("b", "a")}, err: `more than one incoming`},
		{name: "self loop", nodes: []database.PipelineNode{src, a, snk}, edges: []database.PipelineEdge{e("s", "a"), e("a", "a")}, err: `node "A" has more than one incoming connection`},
		{name: "into source", nodes: []database.PipelineNode{src, a, snk}, edges: []database.PipelineEdge{e("a", "s")}, err: `the source "S" cannot have incoming connections`},
		{name: "out of sink", nodes: []database.PipelineNode{src, a, snk}, edges: []database.PipelineEdge{e("k", "a")}, err: `the sink "K" cannot have outgoing connections`},
		{name: "unknown node type", nodes: []database.PipelineNode{src, n("x", "transform_route"), snk}, err: `node "X" has an unknown type "transform_route"`},
		{name: "two sinks", nodes: []database.PipelineNode{src, snk, n("k2", "sink_clickhouse")}, err: "multiple sink"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, _, chain, err := ResolveChain(c.nodes, c.edges)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			got := []string{}
			for _, n := range chain {
				got = append(got, n.ID)
			}
			if strings.Join(got, ",") != strings.Join(c.chain, ",") {
				t.Fatalf("chain %v, want %v", got, c.chain)
			}
		})
	}
}

func TestApplyChain(t *testing.T) {
	in := Batch{Records: []Record{
		{RawJSON: []byte(`{"id":1,"big":12345678901234567890,"html":"<a&b>"}`), Position: "p1"},
		{RawJSON: []byte(`{"id":2}`), Position: "p2"},
		{Data: map[string]interface{}{"id": float64(3)}, Position: "p3"},
	}, DedupToken: "tok"}

	// No chain: the very same records, nothing decoded.
	if out := applyChain(nil, in); &out.Records[0] != &in.Records[0] {
		t.Fatal("empty chain copied the batch")
	}
	empty, _ := transform.Compile(nil, transform.Options{})
	if out := applyChain(empty, in); &out.Records[0] != &in.Records[0] {
		t.Fatal("empty chain copied the batch")
	}

	c, err := transform.Compile([]transform.NodeSpec{
		{ID: "f", Type: transform.TypeFilter, Config: jsonCfg(`{"mode":"drop","rules":[{"field":"id","op":"eq","value":2}]}`)},
		{ID: "m", Type: transform.TypeMap, Config: jsonCfg(`{"set":[{"field":"env","value":"prod"}]}`)},
	}, transform.Options{})
	if err != nil {
		t.Fatal(err)
	}
	out := applyChain(c, in)
	if out.DedupToken != "tok" || len(out.Records) != 2 {
		t.Fatalf("batch %+v", out)
	}
	want := []string{
		`{"big":12345678901234567890,"env":"prod","html":"<a&b>","id":1}`,
		`{"env":"prod","id":3}`,
	}
	for i, r := range out.Records {
		if string(r.RawJSON) != want[i] {
			t.Errorf("row %d = %s, want %s", i, r.RawJSON, want[i])
		}
		if r.Data["env"] != "prod" {
			t.Errorf("row %d Data not set: %v", i, r.Data)
		}
	}
	if out.Records[0].Position != "p1" || out.Records[1].Position != "p3" {
		t.Fatalf("positions %q %q", out.Records[0].Position, out.Records[1].Position)
	}
	// Dropping everything leaves an empty batch the runner acks.
	all, _ := transform.Compile([]transform.NodeSpec{{ID: "f", Type: transform.TypeFilter, Config: jsonCfg(`{"mode":"keep","rules":[{"field":"nope","op":"exists"}]}`)}}, transform.Options{})
	if out := applyChain(all, in); len(out.Records) != 0 {
		t.Fatalf("%d records left", len(out.Records))
	}
}

// captureSink keeps every attempted batch (token and rows); fail makes
// writes fail.
type captureSink struct {
	mu       sync.Mutex
	fail     bool
	attempts []string // token + "\n" + rows
	rows     []string
}

func (s *captureSink) Type() string                   { return "sink_clickhouse" }
func (s *captureSink) Validate(ConnectorConfig) error { return nil }
func (s *captureSink) WriteBatch(_ context.Context, _ ConnectorConfig, b Batch) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var sb strings.Builder
	sb.WriteString(b.DedupToken)
	for _, r := range b.Records {
		sb.WriteString("\n")
		sb.Write(r.RawJSON)
	}
	s.attempts = append(s.attempts, sb.String())
	if s.fail {
		return 0, errors.New("tunnel not connected")
	}
	for _, r := range b.Records {
		s.rows = append(s.rows, string(r.RawJSON))
	}
	return len(b.Records), nil
}

func (s *captureSink) snapshot() ([]string, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.attempts...), append([]string(nil), s.rows...)
}

// fiveNodes is parse, map, filter, sample and redact on OpenTelemetry logs.
func fiveNodes() []tnode {
	return []tnode{
		{id: "parse", typ: transform.TypeParse, label: "Parse", cfg: jsonCfg(`{"field":"Body","format":"logfmt","target":"LogAttributes"}`)},
		{id: "map", typ: transform.TypeMap, label: "Map", cfg: jsonCfg(`{"severity_from":"LogAttributes.level","set":[{"field":"ResourceAttributes.env","value":"prod"}]}`)},
		{id: "filter", typ: transform.TypeFilter, label: "Filter", cfg: jsonCfg(`{"mode":"drop","rules":[{"field":"SeverityText","op":"eq","value":"DEBUG"}]}`)},
		{id: "sample", typ: transform.TypeSample, label: "Sample", cfg: jsonCfg(`{"keep_percent":50,"key":"TraceId","always_keep":{"match":"any","rules":[{"field":"SeverityText","op":"eq","value":"ERROR"}]}}`)},
		{id: "redact", typ: transform.TypeRedact, label: "Redact", cfg: jsonCfg(`{"kinds":["email","ipv4"]}`)},
	}
}

func proActive() config.ProAccess { return config.ProActive }
func proNone() config.ProAccess   { return config.ProNone }

// The runner runs the chain on every batch: the sink gets the transformed
// rows, drops are counted per node, and the last rows that entered the
// chain are kept for the preview.
func TestRunnerAppliesChain(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := chainPipeline(t, db, conn, "source_webhook", map[string]interface{}{}, []tnode{
		{id: "parse", typ: transform.TypeParse, label: "Parse", cfg: jsonCfg(`{"field":"msg","format":"logfmt","target":""}`)},
		{id: "filter", typ: transform.TypeFilter, label: "Filter", cfg: jsonCfg(`{"mode":"drop","rules":[{"field":"level","op":"eq","value":"debug"}]}`)},
		{id: "map", typ: transform.TypeMap, label: "Map", cfg: jsonCfg(`{"severity_from":"level","drop":["msg"]}`)},
	})
	sink := &captureSink{}
	r := newTestRunner(t, db, cfg, sink)
	release := make(chan struct{})
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, out chan<- Batch) error {
			var recs []Record
			for i := 0; i < 30; i++ {
				level := []string{"debug", "info", "error"}[i%3]
				recs = append(recs, Record{RawJSON: []byte(fmt.Sprintf(`{"i":%d,"msg":"level=%s x=%d"}`, i, level, i))})
			}
			recs = append(recs, Record{RawJSON: []byte(`{"i":30,"msg":"=broken"}`)})
			select {
			case out <- Batch{Records: recs}:
			case <-ctx.Done():
			}
			<-release
			<-ctx.Done()
			return nil
		}}, nil
	}
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	defer close(release)
	waitFor(t, "rows written", func() bool { _, rows := sink.snapshot(); return len(rows) == 21 })
	_, rows := sink.snapshot()
	if rows[0] != `{"i":1,"level":"info","severity":"INFO","x":"1"}` || rows[20] != `{"i":30}` {
		t.Fatalf("rows[0]=%s rows[20]=%s", rows[0], rows[20])
	}
	got := r.TransformCounters(id)
	want := []transform.Counters{
		{NodeID: id + "-parse", In: 31, Out: 31, Errors: 1},
		{NodeID: id + "-filter", In: 31, Out: 21, Dropped: 10},
		{NodeID: id + "-map", In: 21, Out: 21},
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("counters %+v, want %+v", got, want)
	}
	recent := r.RecentRecords(id)
	if len(recent) != sampleRingSize || fmt.Sprint(recent[0]["i"]) != "11" || recent[0]["msg"] != "level=error x=11" {
		t.Fatalf("recent: %d records, first %v", len(recent), recent[0])
	}
	if m := r.GetRunningMetrics(id); m.RowsIngested.Load() != 21 {
		t.Fatalf("rows ingested %d", m.RowsIngested.Load())
	}
	r.Stop()
	if r.TransformCounters(id) != nil || r.RecentRecords(id) != nil {
		t.Fatal("stopped pipeline still reports counters")
	}
}

// A spool segment replayed after a failed run gives the same rows with the
// same token, so ClickHouse can drop the replay.
func TestSpoolReplayIsDeterministic(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := chainPipeline(t, db, conn, "source_ingest", map[string]interface{}{"signal": "logs"}, fiveNodes())
	hub := testHub(t, t.TempDir(), fastSpool())
	sink := &captureSink{fail: true}

	r1 := NewRunner(db, nil, cfg)
	r1.newSink = func() SinkConnector { return sink }
	r1.proAccess = proActive
	r1.SetHub(hub)
	if err := r1.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "endpoint", func() bool { return hub.Endpoint(id) != nil })
	if err := hub.Append(id, otelLogBlock(200)); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "two failed attempts", func() bool { a, _ := sink.snapshot(); return len(a) >= 2 })
	r1.Stop()

	sink.mu.Lock()
	sink.fail = false
	sink.mu.Unlock()
	r2 := NewRunner(db, nil, cfg)
	r2.newSink = func() SinkConnector { return sink }
	r2.proAccess = proActive
	r2.SetHub(hub)
	r2.Start()
	waitFor(t, "replay acked", func() bool { return hub.SpoolStats(id).Segments == 0 })
	r2.Stop()

	attempts, rows := sink.snapshot()
	if len(rows) == 0 || len(rows) >= 200 {
		t.Fatalf("%d rows written; the chain should drop some and keep some", len(rows))
	}
	for i, a := range attempts {
		if a != attempts[0] {
			t.Fatalf("attempt %d differs from the first (token or rows changed on replay)", i)
		}
	}
	if !strings.HasPrefix(attempts[0], "chui-spool:") {
		t.Fatalf("token %q", strings.SplitN(attempts[0], "\n", 2)[0])
	}
	if strings.Contains(attempts[0], "@example.com") || !strings.Contains(attempts[0], `"SeverityNumber":17`) {
		t.Fatal("rows were not transformed")
	}
}

// otelLogBlock is n exporter-layout log rows as the OTLP receiver spools
// them.
func otelLogBlock(n int) []byte {
	var buf []byte
	levels := []string{"debug", "info", "warn", "error"}
	for i := 0; i < n; i++ {
		row := otlp.LogRow{
			Timestamp:          time.Unix(1_790_000_000+int64(i), 0),
			TraceID:            fmt.Sprintf("%032x", i/4),
			SpanID:             fmt.Sprintf("%016x", i),
			ServiceName:        "checkout",
			Body:               fmt.Sprintf(`level=%s msg="order %d placed by user%d@example.com" ip=10.0.%d.%d dur_ms=%d`, levels[i%4], i, i, i%256, i%200, i%1000),
			ResourceAttributes: map[string]string{"service.name": "checkout", "host.name": "web-1"},
			LogAttributes:      map[string]string{"thread": "main"},
			ScopeName:          "app",
		}
		buf = otlp.AppendLogJSON(buf, &row)
	}
	return buf
}

func TestRunnerRefusesProNodesWithoutPro(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := chainPipeline(t, db, conn, "source_webhook", map[string]interface{}{}, []tnode{
		{id: "parse", typ: transform.TypeParse, label: "Parse", cfg: jsonCfg(`{"field":"m","format":"json"}`)},
		{id: "sample", typ: transform.TypeSample, label: "Keep 10%", cfg: jsonCfg(`{"keep_percent":10}`)},
	})
	r := newTestRunner(t, db, cfg, &captureSink{})
	r.proAccess = proNone
	err := r.StartPipeline(id)
	if !errors.Is(err, transform.ErrNeedsPro) || err.Error() != "Keep 10%: Sampling needs CH-UI Pro" {
		t.Fatalf("start without Pro: %v", err)
	}
	if r.isRunning(id) {
		t.Fatal("refused pipeline is running")
	}

	// A grace license still runs it; losing the license stops it.
	grace := true
	var mu sync.Mutex
	r.proAccess = func() config.ProAccess {
		mu.Lock()
		defer mu.Unlock()
		if grace {
			return config.ProGrace
		}
		return config.ProNone
	}
	old := proRecheck
	proRecheck = 0
	t.Cleanup(func() { proRecheck = old })
	batches := make(chan Batch)
	r.newSource = func(string) (SourceConnector, error) {
		return &funcSource{fn: func(ctx context.Context, out chan<- Batch) error {
			for {
				select {
				case b := <-batches:
					select {
					case out <- b:
					case <-ctx.Done():
						return nil
					}
				case <-ctx.Done():
					return nil
				}
			}
		}}, nil
	}
	if err := r.StartPipeline(id); err != nil {
		t.Fatalf("start in grace: %v", err)
	}
	mu.Lock()
	grace = false
	mu.Unlock()
	batches <- Batch{Records: []Record{{RawJSON: []byte(`{"m":"{}"}`)}}}
	waitFor(t, "pipeline stopped with an error", func() bool { return status(t, db, id) == "error" && !r.isRunning(id) })
	p, _ := db.GetPipelineByID(id)
	if p.LastError == nil || !strings.Contains(*p.LastError, "Sampling needs CH-UI Pro") {
		t.Fatalf("last_error %v", p.LastError)
	}
	r.Stop()
}

// Benchmarks: go test ./internal/pipelines -run '^$' -bench ApplyChain

func BenchmarkApplyChainNone(b *testing.B) {
	benchApplyChain(b, nil)
}

func BenchmarkApplyChainFiveNodesOTelLogs(b *testing.B) {
	var specs []transform.NodeSpec
	for _, n := range fiveNodes() {
		specs = append(specs, transform.NodeSpec{ID: n.id, Type: n.typ, Config: n.cfg})
	}
	c, err := transform.Compile(specs, transform.Options{Signal: SignalLogs})
	if err != nil {
		b.Fatal(err)
	}
	benchApplyChain(b, c)
}

func benchApplyChain(b *testing.B, c *transform.Chain) {
	const rows = 10_000
	block := otelLogBlock(rows)
	lines := strings.Split(strings.TrimSuffix(string(block), "\n"), "\n")
	recs := make([]Record, len(lines))
	for j, l := range lines {
		recs[j] = Record{RawJSON: []byte(l)}
	}
	b.SetBytes(int64(len(block)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// applyChain never changes its input records.
		applyChain(c, Batch{Records: recs})
	}
	b.ReportMetric(float64(rows*b.N)/b.Elapsed().Seconds(), "rows/s")
}
