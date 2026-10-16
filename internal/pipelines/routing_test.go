package pipelines

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
)

// tableSink stands in for ClickHouse behind several sinks, keyed by the
// sink's table. It honours dedup tokens like a table with a dedup window:
// a token it has seen is accepted and dropped. fail makes a table's next
// writes fail.
type tableSink struct {
	mu       sync.Mutex
	fail     map[string]int
	seen     map[string]bool
	calls    map[string]int
	tokens   map[string][]string
	rows     map[string][]string
	tokenUse map[string]int
}

func newTableSink() *tableSink {
	return &tableSink{fail: map[string]int{}, seen: map[string]bool{}, calls: map[string]int{}, tokens: map[string][]string{}, rows: map[string][]string{}, tokenUse: map[string]int{}}
}

func (f *tableSink) Type() string                   { return "sink_clickhouse" }
func (f *tableSink) Validate(ConnectorConfig) error { return nil }
func (f *tableSink) WriteBatch(_ context.Context, cfg ConnectorConfig, b Batch) (int, error) {
	table := stringField(cfg.Fields, "table", "")
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls[table]++
	f.tokenUse[b.DedupToken]++
	if f.fail[table] > 0 {
		f.fail[table]--
		return 0, errors.New("clickhouse down")
	}
	f.tokens[table] = append(f.tokens[table], b.DedupToken)
	if b.DedupToken != "" && f.seen[b.DedupToken] {
		return len(b.Records), nil // a replay, dropped
	}
	f.seen[b.DedupToken] = true
	for _, r := range b.Records {
		f.rows[table] = append(f.rows[table], string(r.RawJSON))
	}
	return len(b.Records), nil
}

func (f *tableSink) setFail(table string, n int) {
	f.mu.Lock()
	f.fail[table] = n
	f.mu.Unlock()
}

func (f *tableSink) get(table string) (rows []string, calls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.rows[table]...), f.calls[table]
}

// branchDef is one route output of routedPipeline: its handle, its chain
// and the table of its sink.
type branchDef struct {
	handle string
	chain  []tnode
	table  string
}

// routedPipeline saves source -> shared... -> route -> each branch's
// chain -> its sink. Sink node ids are <pipeline>-sink-<table>.
func routedPipeline(t *testing.T, db *database.DB, conn, srcType string, src map[string]interface{}, shared []tnode, routeCfg string, branches []branchDef) string {
	t.Helper()
	id, err := db.CreatePipeline("p-route", "", conn, "tester")
	if err != nil {
		t.Fatal(err)
	}
	srcJSON, _ := json.Marshal(src)
	nodes := []database.PipelineNode{{ID: id + "-src", NodeType: srcType, Label: "Source", ConfigEncrypted: string(srcJSON)}}
	var edges []database.PipelineEdge
	edge := func(from, to, handle string) {
		e := database.PipelineEdge{ID: fmt.Sprintf("%s-e%d", id, len(edges)), SourceNodeID: from, TargetNodeID: to}
		if handle != "" {
			h := handle
			e.SourceHandle = &h
		}
		edges = append(edges, e)
	}
	add := func(prev string, chain []tnode) string {
		for _, n := range chain {
			cfg, _ := json.Marshal(n.cfg)
			nid := id + "-" + n.id
			nodes = append(nodes, database.PipelineNode{ID: nid, NodeType: n.typ, Label: n.label, ConfigEncrypted: string(cfg)})
			edge(prev, nid, "")
			prev = nid
		}
		return prev
	}
	prev := add(id+"-src", shared)
	routeID := id + "-route"
	nodes = append(nodes, database.PipelineNode{ID: routeID, NodeType: transform.TypeRoute, Label: "Route", ConfigEncrypted: routeCfg})
	edge(prev, routeID, "")
	for _, b := range branches {
		first := routeID
		if len(b.chain) > 0 {
			cfg, _ := json.Marshal(b.chain[0].cfg)
			first = id + "-" + b.chain[0].id
			nodes = append(nodes, database.PipelineNode{ID: first, NodeType: b.chain[0].typ, Label: b.chain[0].label, ConfigEncrypted: string(cfg)})
			edge(routeID, first, b.handle)
		}
		last := add(first, b.chain[min(1, len(b.chain)):])
		sinkJSON, _ := json.Marshal(map[string]interface{}{"database": "d", "table": b.table})
		sinkID := id + "-sink-" + b.table
		nodes = append(nodes, database.PipelineNode{ID: sinkID, NodeType: "sink_clickhouse", Label: b.table, ConfigEncrypted: string(sinkJSON)})
		if last == routeID {
			edge(routeID, sinkID, b.handle)
		} else {
			edge(last, sinkID, "")
		}
	}
	if err := db.SavePipelineGraph(id, nodes, edges, ""); err != nil {
		t.Fatal(err)
	}
	return id
}

const levelRoutes = `{"mode":%q,"unmatched":%q,"routes":[
	{"id":"errors","name":"Errors","rules":[{"field":"level","op":"eq","value":"error"}]},
	{"id":"warn","rules":[{"field":"level","op":"in","value":["warn","error"]}]}
]}`

func TestResolveGraphRoutes(t *testing.T) {
	n := func(id, typ, cfg string) database.PipelineNode {
		return database.PipelineNode{ID: id, NodeType: typ, Label: strings.ToUpper(id), ConfigEncrypted: cfg}
	}
	e := func(from, to string, handle ...string) database.PipelineEdge {
		ed := database.PipelineEdge{ID: from + to, SourceNodeID: from, TargetNodeID: to}
		if len(handle) > 0 {
			ed.SourceHandle = &handle[0]
		}
		return ed
	}
	routeCfg := fmt.Sprintf(levelRoutes, "first", "output")
	src, rt := n("s", "source_webhook", "{}"), n("rt", transform.TypeRoute, routeCfg)
	k1, k2, k3 := n("k1", "sink_clickhouse", "{}"), n("k2", "sink_clickhouse", "{}"), n("k3", "sink_clickhouse", "{}")
	a, b, c := n("a", transform.TypeParse, "{}"), n("b", transform.TypeFilter, "{}"), n("c", transform.TypeMap, "{}")
	nodes := func(ns ...database.PipelineNode) []database.PipelineNode { return ns }
	edges := func(es ...database.PipelineEdge) []database.PipelineEdge { return es }

	// A full tree: shared a, route, errors -> b -> k1, warn -> k2,
	// unmatched -> c -> k3. Branches follow the route's handle order, not
	// the edge order.
	g, err := ResolveGraph(nodes(k3, c, b, k1, src, a, rt, k2),
		edges(e("c", "k3"), e("rt", "c", "unmatched"), e("rt", "k2", "warn"), e("s", "a"), e("a", "rt"), e("rt", "b", "errors"), e("b", "k1")))
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, br := range g.Branches {
		var chain []string
		for _, n := range br.Chain {
			chain = append(chain, n.ID)
		}
		got = append(got, fmt.Sprintf("%s:%v->%s", br.Handle, chain, br.Sink.ID))
	}
	if g.Route.ID != "rt" || len(g.Shared) != 1 || g.Shared[0].ID != "a" || !g.UnmatchedConnected ||
		strings.Join(got, " ") != "errors:[b]->k1 warn:[]->k2 unmatched:[c]->k3" || len(g.Sinks()) != 3 {
		t.Fatalf("graph %v shared %v", got, g.Shared)
	}

	// An unconnected unmatched output is fine; its records are dropped.
	g, err = ResolveGraph(nodes(src, rt, k1, k2), edges(e("s", "rt"), e("rt", "k1", "errors"), e("rt", "k2", "warn")))
	if err != nil || g.UnmatchedConnected || len(g.Branches) != 2 || len(g.Shared) != 0 {
		t.Fatalf("graph %+v err %v", g, err)
	}

	nine := nodes(src, rt)
	var nineEdges []database.PipelineEdge
	for i := 0; i < 9; i++ {
		nine = append(nine, n(fmt.Sprintf("k%d", i+10), "sink_clickhouse", "{}"))
	}
	cases := []struct {
		name  string
		nodes []database.PipelineNode
		edges []database.PipelineEdge
		err   string
	}{
		{"route left unconnected", nodes(src, rt, k1), edges(e("s", "rt"), e("rt", "k1", "errors")), `RT: route "warn" is not connected to a sink`},
		{"named route unconnected", nodes(src, rt, k1), edges(e("s", "rt"), e("rt", "k1", "warn")), `RT: route "Errors" is not connected to a sink`},
		{"route without outputs", nodes(src, n("rt", transform.TypeRoute, `{}`), k1), edges(e("s", "rt")), "RT: not connected to a sink"},
		{"two outputs into one sink", nodes(src, rt, k1), edges(e("s", "rt"), e("rt", "k1", "errors"), e("rt", "k1", "warn")), "K1: more than one incoming connection"},
		{"two branches merge", nodes(src, rt, b, k1), edges(e("s", "rt"), e("rt", "b", "errors"), e("rt", "b", "warn"), e("b", "k1")), "B: more than one incoming connection"},
		{"output used twice", nodes(src, rt, k1, k2), edges(e("s", "rt"), e("rt", "k1", "errors"), e("rt", "k2", "errors")), `RT: output "errors" has more than one connection`},
		{"unknown output", nodes(src, rt, k1), edges(e("s", "rt"), e("rt", "k1", "nope")), `RT: a connection leaves from output "nope", which this node does not have`},
		{"output without handle", nodes(src, rt, k1), edges(e("s", "rt"), e("rt", "k1")), "RT: a connection does not say which route it carries"},
		{"unmatched output on drop", nodes(src, n("rt", transform.TypeRoute, fmt.Sprintf(levelRoutes, "first", "drop")), k1, k2, k3), edges(e("s", "rt"), e("rt", "k1", "errors"), e("rt", "k2", "warn"), e("rt", "k3", "unmatched")), `output "unmatched", which this node does not have`},
		{"branch dead end", nodes(src, rt, b, k1), edges(e("s", "rt"), e("rt", "b", "errors"), e("rt", "k1", "warn")), "B: not connected to anything downstream"},
		{"two routes", nodes(src, rt, n("rt2", transform.TypeRoute, routeCfg), k1, k2), nil, "RT2: a pipeline can have at most one Route node"},
		{"two sinks without a route", nodes(src, k1, k2), edges(e("s", "k1")), "K2: a pipeline with more than one sink needs a Route node"},
		{"too many sinks", nine, nineEdges, "K18: a pipeline can have at most 8 sinks"},
		{"branch from a transform", nodes(src, a, rt, k1, k2), edges(e("s", "a"), e("a", "rt"), e("a", "k1"), e("rt", "k2", "errors")), "A: more than one outgoing connection; only a Route node can branch"},
		{"route before nothing", nodes(src, rt, k1, k2), edges(e("rt", "k1", "errors"), e("rt", "k2", "warn")), "S: not connected to a sink"},
		{"stray node", nodes(src, rt, k1, k2, c), edges(e("s", "rt"), e("rt", "k1", "errors"), e("rt", "k2", "warn")), "C: not connected between the source and a sink"},
		{"cycle back to route", nodes(src, rt, b, k1, k2), edges(e("s", "rt"), e("rt", "b", "errors"), e("b", "rt"), e("rt", "k2", "warn")), "RT: more than one incoming connection"},
		{"bad route config", nodes(src, n("rt", transform.TypeRoute, `{"routes":[{"id":"Bad"}]}`), k1), edges(e("s", "rt")), "id must be"},
		{"sink with outputs", nodes(src, rt, k1, k2), edges(e("s", "rt"), e("rt", "k1", "errors"), e("rt", "k2", "warn"), e("k1", "k2")), "K1: a sink cannot have outgoing connections"},
	}
	// Start errors carry the node the editor marks.
	marks := map[string]string{
		"route left unconnected":    "rt",
		"two outputs into one sink": "k1",
		"output used twice":         "rt",
		"branch dead end":           "b",
		"two routes":                "rt2",
		"two sinks without a route": "k2",
		"too many sinks":            "k18",
		"branch from a transform":   "a",
		"stray node":                "c",
		"bad route config":          "rt",
		"sink with outputs":         "k1",
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := ResolveGraph(c.nodes, c.edges)
			if err == nil || !strings.Contains(err.Error(), c.err) {
				t.Fatalf("err = %v, want %q", err, c.err)
			}
			if want, ok := marks[c.name]; ok {
				var ge *GraphError
				var ne *transform.NodeError
				switch {
				case errors.As(err, &ge) && ge.NodeID == want:
				case errors.As(err, &ne) && ne.NodeID == want:
				default:
					t.Fatalf("err %v does not name node %s", err, want)
				}
			}
		})
	}
}

// startRouted starts a routed pipeline with Pro on a runner whose sinks
// all write to fake.
func startRouted(t *testing.T, id string, r *Runner) {
	t.Helper()
	r.proAccess = proActive
	if err := r.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(r.Stop)
}

// Rows go to every matching sink (mode all), a branch chain runs only on
// its share, the unmatched output drops what matched nothing, a sink with
// no rows in a batch does no insert, and every sink has its own token.
func TestRunnerRoutesToSinks(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := routedPipeline(t, db, conn, "source_webhook", map[string]interface{}{},
		[]tnode{{id: "parse", typ: transform.TypeParse, label: "Parse", cfg: jsonCfg(`{"field":"msg","format":"logfmt","target":""}`)}},
		fmt.Sprintf(levelRoutes, "all", "drop"),
		[]branchDef{
			{handle: "errors", table: "errors", chain: []tnode{{id: "tag", typ: transform.TypeMap, label: "Tag", cfg: jsonCfg(`{"set":[{"field":"page","value":"yes"}]}`)}}},
			{handle: "warn", table: "warn"},
		})
	fake := newTableSink()
	r := newTestRunner(t, db, cfg, fake)
	batches := make(chan Batch, 2)
	acks := make(chan string, 2)
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
	startRouted(t, id, r)
	batch := func(tok string, levels ...string) Batch {
		var recs []Record
		for i, l := range levels {
			recs = append(recs, Record{RawJSON: []byte(fmt.Sprintf(`{"msg":"level=%s n=%d"}`, l, i))})
		}
		return Batch{Records: recs, DedupToken: tok, Ack: func() { acks <- tok }}
	}
	batches <- batch("t1", "error", "info", "warn", "debug")
	if got := <-acks; got != "t1" {
		t.Fatalf("ack %s", got)
	}
	errs, errCalls := fake.get("errors")
	warns, warnCalls := fake.get("warn")
	if len(errs) != 1 || !strings.Contains(errs[0], `"page":"yes"`) || !strings.Contains(errs[0], `"level":"error"`) ||
		len(warns) != 2 || strings.Contains(warns[0], "page") || !strings.Contains(warns[0], `"level":"error"`) || !strings.Contains(warns[1], `"level":"warn"`) {
		t.Fatalf("errors %v warn %v", errs, warns)
	}
	fake.mu.Lock()
	tokens := fmt.Sprint(fake.tokens["errors"], fake.tokens["warn"])
	fake.mu.Unlock()
	if tokens != fmt.Sprintf("[t1:%s-sink-errors] [t1:%s-sink-warn]", id, id) {
		t.Fatalf("tokens %s", tokens)
	}

	// Only a warning: the errors sink gets no rows and does no insert.
	batches <- batch("t2", "warn", "info")
	<-acks
	if _, calls := fake.get("errors"); calls != errCalls {
		t.Fatalf("errors sink was called for an empty share")
	}
	if _, calls := fake.get("warn"); calls != warnCalls+1 {
		t.Fatalf("warn sink calls %d", calls)
	}

	// Metrics are counted right after the ack.
	m := r.GetRunningMetrics(id)
	waitFor(t, "metrics", func() bool { return m.BatchesSent.Load() == 3 })
	if m.RowsIngested.Load() != 4 || m.BatchesSent.Load() != 3 {
		t.Fatalf("summed metrics rows %d batches %d", m.RowsIngested.Load(), m.BatchesSent.Load())
	}
	sinks := r.SinkStatuses(id)
	if len(sinks) != 2 || sinks[0].NodeID != id+"-sink-errors" || sinks[0].Rows != 1 || sinks[0].Batches != 1 || sinks[0].LastWriteAt == "" ||
		sinks[1].Rows != 3 || sinks[1].Batches != 2 || sinks[1].Errors != 0 {
		t.Fatalf("sinks %+v", sinks)
	}
	counters := r.TransformCounters(id)
	byID := map[string]transform.Counters{}
	for _, c := range counters {
		byID[c.NodeID] = c
	}
	route, tag := byID[id+"-route"], byID[id+"-tag"]
	if len(counters) != 3 || counters[0].NodeID != id+"-parse" || counters[1].NodeID != id+"-route" || route.RouteCounters == nil ||
		route.In != 6 || route.UnmatchedDropped != 3 || fmt.Sprint(route.Outputs) != "[{errors 1} {warn 3}]" || route.Out != 4 || tag.In != 1 || tag.Out != 1 {
		t.Fatalf("counters %+v route %+v", counters, route.RouteCounters)
	}
}

// The delivery test: two sinks, the second fails twice. The batch is
// retried until both succeed; the first sink stores each row exactly once
// and the spool segment is acked once.
func TestRoutedRetryWritesEverySinkOnce(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := routedPipeline(t, db, conn, "source_ingest", map[string]interface{}{"signal": "events"}, nil,
		`{"unmatched":"output","routes":[{"id":"errors","rules":[{"field":"level","op":"eq","value":"error"}]}]}`,
		[]branchDef{{handle: "errors", table: "errors"}, {handle: "unmatched", table: "rest"}})
	fake := newTableSink()
	fake.setFail("rest", 2)
	r := NewRunner(db, nil, cfg)
	r.newSink = func() SinkConnector { return fake }
	hub := testHub(t, t.TempDir(), fastSpool())
	r.SetHub(hub)
	startRouted(t, id, r)
	waitFor(t, "endpoint", func() bool { return hub.Endpoint(id) != nil })
	if err := hub.Append(id, []byte(`{"level":"error","i":1}`+"\n"+`{"level":"info","i":2}`+"\n"+`{"level":"info","i":3}`+"\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "spool empty", func() bool { return hub.SpoolStats(id).Segments == 0 })
	waitFor(t, "metrics", func() bool { s := r.SinkStatuses(id); return s[1].Rows == 2 })

	errs, errCalls := fake.get("errors")
	rest, restCalls := fake.get("rest")
	if len(errs) != 1 || errCalls != 1 || len(rest) != 2 || restCalls != 3 {
		t.Fatalf("errors %v (%d calls), rest %v (%d calls)", errs, errCalls, rest, restCalls)
	}
	if v := hub.View(id, time.Now()); v.Written.Rows != 3 || v.Written.Batches != 1 {
		t.Fatalf("segment acked more than once: %+v", v.Written)
	}
	if s := r.SinkStatuses(id); s[0].Errors != 0 || s[1].Errors != 2 || s[1].Rows != 2 || r.GetRunningMetrics(id).ErrorsCount.Load() != 2 {
		t.Fatalf("sink statuses %+v", s)
	}
}

// A routed batch interrupted by a stop is replayed on the next run with the
// same per-sink tokens: the sink that already had its rows drops the replay.
func TestRoutedReplayAfterRestartDedups(t *testing.T) {
	shortBackoff(t)
	db, conn, cfg := testRunnerDB(t)
	id := routedPipeline(t, db, conn, "source_ingest", map[string]interface{}{"signal": "events"}, nil,
		fmt.Sprintf(levelRoutes, "first", "drop"),
		[]branchDef{{handle: "errors", table: "errors"}, {handle: "warn", table: "warn"}})
	fake := newTableSink()
	fake.setFail("warn", 1<<30)
	hub := testHub(t, t.TempDir(), fastSpool())

	r1 := NewRunner(db, nil, cfg)
	r1.newSink = func() SinkConnector { return fake }
	r1.SetHub(hub)
	r1.proAccess = proActive
	if err := r1.StartPipeline(id); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "endpoint", func() bool { return hub.Endpoint(id) != nil })
	if err := hub.Append(id, []byte(`{"level":"error"}`+"\n"+`{"level":"warn"}`+"\n"+`{"level":"info"}`+"\n")); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "warn retried", func() bool { _, calls := fake.get("warn"); return calls >= 2 })
	r1.Stop()
	if rows, _ := fake.get("errors"); len(rows) != 1 {
		t.Fatalf("errors rows before restart: %v", rows)
	}

	fake.setFail("warn", 0)
	r2 := NewRunner(db, nil, cfg)
	r2.newSink = func() SinkConnector { return fake }
	r2.SetHub(hub)
	r2.proAccess = proActive
	r2.Start()
	waitFor(t, "replay acked", func() bool { return hub.SpoolStats(id).Segments == 0 })
	r2.Stop()

	errs, errCalls := fake.get("errors")
	warns, _ := fake.get("warn")
	if len(errs) != 1 || errCalls != 2 || len(warns) != 1 {
		t.Fatalf("errors %v (%d calls), warn %v", errs, errCalls, warns)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	tokens := append([]string(nil), fake.tokens["errors"]...)
	sort.Strings(tokens)
	if tokens[0] != tokens[1] || !strings.HasPrefix(tokens[0], "chui-spool:") || !strings.HasSuffix(tokens[0], ":"+id+"-sink-errors") {
		t.Fatalf("errors tokens %v", tokens)
	}
}

func TestRunnerRouteNeedsPro(t *testing.T) {
	db, conn, cfg := testRunnerDB(t)
	id := routedPipeline(t, db, conn, "source_webhook", map[string]interface{}{}, nil,
		fmt.Sprintf(levelRoutes, "first", "drop"),
		[]branchDef{{handle: "errors", table: "errors"}, {handle: "warn", table: "warn"}})
	r := newTestRunner(t, db, cfg, newTableSink())
	r.proAccess = proNone
	err := r.StartPipeline(id)
	if !errors.Is(err, transform.ErrNeedsPro) || err.Error() != "Route: Routing needs CH-UI Pro" || r.isRunning(id) {
		t.Fatalf("start without Pro: %v", err)
	}
	// A grace license runs it.
	r.proAccess = func() config.ProAccess { return config.ProGrace }
	if err := r.StartPipeline(id); err != nil {
		t.Fatalf("start in grace: %v", err)
	}
	if err := r.StopPipeline(id); err != nil {
		t.Fatal(err)
	}
	// Starting with a broken shape names the route.
	nodes, edges, _ := db.GetPipelineGraph(id)
	var kept []database.PipelineEdge
	for _, e := range edges {
		if e.TargetNodeID != id+"-sink-warn" {
			kept = append(kept, e)
		}
	}
	if err := db.SavePipelineGraph(id, nodes, kept, ""); err != nil {
		t.Fatal(err)
	}
	r.proAccess = proActive
	if err := r.StartPipeline(id); err == nil || !strings.Contains(err.Error(), `Route: route "warn" is not connected to a sink`) {
		t.Fatalf("start with an unconnected route: %v", err)
	}
}
