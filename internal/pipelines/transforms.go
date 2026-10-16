package pipelines

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"runtime"
	"sync"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
)

// sampleRingSize is how many of the records entering the transform chain a
// running pipeline shows in the editor's preview.
const sampleRingSize = 20

// IsSourceType reports whether nodeType is a source connector.
func IsSourceType(nodeType string) bool { return isSourceType(nodeType) }

func nodeName(n *database.PipelineNode) string {
	if n.Label != "" {
		return fmt.Sprintf("%q", n.Label)
	}
	return fmt.Sprintf("%q", n.NodeType)
}

// TransformOptions returns the chain options for a pipeline's source: an
// ingest source with the logs or traces signal writes the OpenTelemetry
// layout.
func TransformOptions(sourceType string, sourceFields map[string]interface{}) transform.Options {
	if sourceType == "source_ingest" {
		switch s := IngestSignal(sourceFields); s {
		case SignalLogs, SignalTraces:
			return transform.Options{Signal: s}
		}
	}
	return transform.Options{}
}

// transformSpecs turns stored transform nodes into chain specs.
func transformSpecs(nodes []database.PipelineNode) ([]transform.NodeSpec, error) {
	specs := make([]transform.NodeSpec, 0, len(nodes))
	for _, n := range nodes {
		var cfg map[string]any
		if n.ConfigEncrypted != "" {
			if err := json.Unmarshal([]byte(n.ConfigEncrypted), &cfg); err != nil {
				return nil, graphErr(&n, "unreadable config: %v", err)
			}
		}
		specs = append(specs, transform.NodeSpec{ID: n.ID, Type: n.NodeType, Label: n.Label, Config: cfg})
	}
	return specs, nil
}

// applyChainChunk is the batch size above which applyChain splits the work
// across CPUs. Records are independent, so the output is the same.
const applyChainChunk = 1024

// applyChain decodes a batch's rows, runs the chain and encodes the rows the
// chain kept, in their original order. An empty chain returns the batch
// untouched, so pipelines without transforms pay nothing. Numbers are
// decoded as json.Number so large integers keep every digit.
func applyChain(c *transform.Chain, b Batch) Batch {
	if c.Len() == 0 || len(b.Records) == 0 {
		return b
	}
	workers := min(runtime.GOMAXPROCS(0), 8, (len(b.Records)+applyChainChunk-1)/applyChainChunk)
	if workers <= 1 {
		b.Records = runChainOn(c, b.Records)
		return b
	}
	size := (len(b.Records) + workers - 1) / workers
	parts := make([][]Record, workers)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		lo, hi := w*size, min((w+1)*size, len(b.Records))
		wg.Add(1)
		go func() {
			defer wg.Done()
			parts[w] = runChainOn(c, b.Records[lo:hi])
		}()
	}
	wg.Wait()
	n := 0
	for _, p := range parts {
		n += len(p)
	}
	out := make([]Record, 0, n)
	for _, p := range parts {
		out = append(out, p...)
	}
	b.Records = out
	return b
}

// runChainOn transforms one run of records.
func runChainOn(c *transform.Chain, in []Record) []Record {
	recs := make([]map[string]any, len(in))
	for i, rec := range in {
		recs[i] = decodeRecord(rec)
	}
	kept, idx := c.Run(recs)

	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	ends := make([]int, len(kept))
	for i, m := range kept {
		if err := enc.Encode(m); err != nil {
			// Cannot happen for decoded JSON; keep the input row.
			buf.Write(in[idx[i]].RawJSON)
			buf.WriteByte('\n')
		}
		ends[i] = buf.Len() - 1 // without the newline
	}
	data := buf.Bytes()
	out := make([]Record, len(kept))
	start := 0
	for i, m := range kept {
		out[i] = Record{Data: m, RawJSON: data[start:ends[i]:ends[i]], Position: in[idx[i]].Position}
		start = ends[i] + 1
	}
	return out
}

// decodeRecord returns a record's row as a map the chain may change.
func decodeRecord(rec Record) map[string]any {
	if len(rec.RawJSON) > 0 {
		dec := json.NewDecoder(bytes.NewReader(rec.RawJSON))
		dec.UseNumber()
		var m map[string]any
		if dec.Decode(&m) == nil && m != nil {
			return m
		}
	}
	if rec.Data != nil {
		return transform.CopyRecord(rec.Data)
	}
	return map[string]any{}
}

// recordRing keeps the last InferSampleLimit rows that entered the chain:
// the editor's preview shows the last sampleRingSize, schema inference
// reads them all.
type recordRing struct {
	mu   sync.Mutex
	rows [][]byte
}

func (r *recordRing) add(records []Record) {
	if over := len(records) - InferSampleLimit; over > 0 {
		records = records[over:]
	}
	rows := make([][]byte, 0, len(records))
	for _, rec := range records {
		raw := rec.RawJSON
		if len(raw) == 0 {
			raw, _ = json.Marshal(rec.Data)
		} else {
			raw = bytes.Clone(raw) // spool rows alias the segment
		}
		rows = append(rows, raw)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, rows...)
	if over := len(r.rows) - InferSampleLimit; over > 0 {
		r.rows = append([][]byte(nil), r.rows[over:]...)
	}
}

// records returns the last sampleRingSize rows, oldest first.
func (r *recordRing) records() []map[string]any { return r.last(sampleRingSize) }

// all returns every kept row, oldest first.
func (r *recordRing) all() []map[string]any { return r.last(InferSampleLimit) }

func (r *recordRing) last(n int) []map[string]any {
	r.mu.Lock()
	rows := r.rows
	if over := len(rows) - n; over > 0 {
		rows = rows[over:]
	}
	rows = append([][]byte(nil), rows...)
	r.mu.Unlock()
	out := make([]map[string]any, 0, len(rows))
	for _, raw := range rows {
		out = append(out, decodeRecord(Record{RawJSON: raw}))
	}
	return out
}

// TransformCounters returns the per-node counters of a running pipeline:
// the shared chain, the route, then each branch in sink order. It is nil
// when the pipeline is not running.
func (r *Runner) TransformCounters(pipelineID string) []transform.Counters {
	rp := r.running(pipelineID)
	if rp == nil {
		return nil
	}
	out := rp.chain.Counters()
	if rp.route != nil {
		out = append(out, rp.route.Counters())
	}
	for _, sp := range rp.sinks {
		out = append(out, sp.chain.Counters()...)
	}
	return out
}

// RecentRecords returns the last rows that entered a running pipeline's
// chain, oldest first, or nil when it is not running.
func (r *Runner) RecentRecords(pipelineID string) []map[string]any {
	rp := r.running(pipelineID)
	if rp == nil {
		return nil
	}
	return rp.sample.records()
}

func (r *Runner) running(pipelineID string) *RunningPipeline {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pipelines[pipelineID]
}

// PreviewSource returns the chain options of a saved pipeline and a few
// records read from its source without moving its position, for the
// transform preview of a pipeline that is not running. A source that cannot
// be sampled returns no records and no error.
func (r *Runner) PreviewSource(ctx context.Context, pipelineID string, withSample bool) (transform.Options, []map[string]any, error) {
	nodes, _, err := r.db.GetPipelineGraph(pipelineID)
	if err != nil {
		return transform.Options{}, nil, fmt.Errorf("load pipeline graph: %w", err)
	}
	var src *database.PipelineNode
	for i := range nodes {
		if isSourceType(nodes[i].NodeType) {
			src = &nodes[i]
			break
		}
	}
	if src == nil {
		return transform.Options{}, nil, nil
	}
	cfg, err := r.parseNodeConfig(src)
	if err != nil {
		return transform.Options{}, nil, err
	}
	opts := TransformOptions(cfg.NodeType, cfg.Fields)
	if !withSample {
		return opts, nil, nil
	}
	cfg.Fields["pipeline_id"] = pipelineID
	cfg.Hub = r.hub
	source, err := r.newSource(cfg.NodeType)
	if err != nil {
		return opts, nil, nil
	}
	sampler, ok := source.(Sampler)
	if !ok || source.Validate(cfg) != nil {
		return opts, nil, nil
	}
	if IsSQLiteSource(cfg.NodeType, cfg.Fields) {
		if err := CheckSQLitePath(stringField(cfg.Fields, "connection_string", ""), r.dataDir()); err != nil {
			return opts, nil, nil
		}
	}
	sctx, cancel := context.WithTimeout(ctx, sampleTimeout)
	defer cancel()
	res, err := sampler.Sample(sctx, cfg, SampleLimit)
	if err != nil {
		return opts, nil, nil
	}
	return opts, res.Records, nil
}
