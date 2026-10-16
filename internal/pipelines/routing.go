package pipelines

import (
	"encoding/json"
	"sync/atomic"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
)

// sinkPath is one sink of a running pipeline: its branch chain, its
// connector and its counters.
type sinkPath struct {
	nodeID string
	name   string // for run logs
	// output is the index of the route output feeding this sink in
	// Route.Handles; unused without a route.
	output int
	chain  *transform.Chain
	sink   SinkConnector
	cfg    ConnectorConfig
	stats  sinkStats
}

type sinkStats struct {
	rows, batches, errors atomic.Int64
	lastWrite             atomic.Value // time.Time
}

// sinkName names a sink in run logs: its label, or its table.
func sinkName(n *database.PipelineNode, cfg ConnectorConfig) string {
	if n.Label != "" {
		return nodeName(n)
	}
	return stringField(cfg.Fields, "database", "default") + "." + stringField(cfg.Fields, "table", "")
}

// sinkBatches runs a source batch through the shared chain, the route and
// the branch chains, and returns the rows of every sink in sink order.
// Without a route the one sink gets the shared chain's output, with the
// batch's own dedup token. With a route each sink's token is
// "<batch token>:<sink node id>", so a replayed batch splits into the same
// tokens. Ack and Checkpoint stay with the source batch.
func (rp *RunningPipeline) sinkBatches(b Batch) []Batch {
	b = applyChain(rp.chain, b)
	if rp.route == nil {
		part := b
		part.Ack = nil
		return []Batch{part}
	}
	var outs [][]int
	if len(b.Records) > 0 {
		recs := make([]map[string]any, len(b.Records))
		for i, rec := range b.Records {
			if rp.chain.Len() > 0 && rec.Data != nil {
				recs[i] = rec.Data // decoded by the chain; the route only reads it
			} else {
				recs[i] = decodeRecord(rec)
			}
		}
		outs = rp.route.Split(recs)
	}
	parts := make([]Batch, len(rp.sinks))
	for i, sp := range rp.sinks {
		var idx []int
		if outs != nil {
			idx = outs[sp.output]
		}
		recs := make([]Record, len(idx))
		for k, j := range idx {
			recs[k] = b.Records[j]
		}
		part := Batch{Records: recs, SourceTS: b.SourceTS, Checkpoint: b.Checkpoint}
		if b.DedupToken != "" {
			part.DedupToken = b.DedupToken + ":" + sp.nodeID
		}
		part = applyChain(sp.chain, part)
		// The sink infers an events table's columns from the first row.
		if len(part.Records) > 0 && part.Records[0].Data == nil {
			var data map[string]interface{}
			if json.Unmarshal(part.Records[0].RawJSON, &data) == nil {
				part.Records[0].Data = data
			}
		}
		parts[i] = part
	}
	return parts
}

// SinkStatus is a running pipeline's totals for one sink.
type SinkStatus struct {
	NodeID      string `json:"node_id"`
	Rows        int64  `json:"rows"`
	Batches     int64  `json:"batches"`
	LastWriteAt string `json:"last_write_at,omitempty"`
	Errors      int64  `json:"errors"`
}

// SinkStatuses returns the per-sink totals of a running pipeline, in sink
// order, or nil when it is not running.
func (r *Runner) SinkStatuses(pipelineID string) []SinkStatus {
	rp := r.running(pipelineID)
	if rp == nil {
		return nil
	}
	out := make([]SinkStatus, 0, len(rp.sinks))
	for _, sp := range rp.sinks {
		st := SinkStatus{NodeID: sp.nodeID, Rows: sp.stats.rows.Load(), Batches: sp.stats.batches.Load(), Errors: sp.stats.errors.Load()}
		if t, ok := sp.stats.lastWrite.Load().(time.Time); ok && !t.IsZero() {
			st.LastWriteAt = t.UTC().Format(time.RFC3339)
		}
		out = append(out, st)
	}
	return out
}
