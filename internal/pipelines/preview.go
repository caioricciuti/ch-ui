package pipelines

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Bounds of a pipeline test.
const (
	SampleLimit   = 20
	sampleTimeout = 10 * time.Second
	inferTimeout  = 15 * time.Second
)

// SampleResult is what a source returns for a test: sample records and a
// sentence saying where they came from.
type SampleResult struct {
	Records []map[string]interface{} `json:"records"`
	Note    string                   `json:"note"`
}

// Sampler is a source that can read a few records without moving its
// position (no offset commit, no checkpoint).
type Sampler interface {
	Sample(ctx context.Context, cfg ConnectorConfig, limit int) (SampleResult, error)
}

// SinkChecker is a sink that can verify its target before a run.
type SinkChecker interface {
	Check(ctx context.Context, cfg ConnectorConfig) []SinkCheck
}

// TestResult is the outcome of testing a saved pipeline.
type TestResult struct {
	SourceType  string                   `json:"source_type"`
	Records     []map[string]interface{} `json:"records"`
	Note        string                   `json:"note"`
	SourceError string                   `json:"source_error,omitempty"`
	SinkChecks  []SinkCheck              `json:"sink_checks"`
	// SinkRecords holds, per sink node id, up to InferSampleLimit sample
	// rows after the shared chain, the route and the sink's branch chain:
	// what that sink would write, for schema inference.
	SinkRecords map[string][]map[string]interface{} `json:"-"`
	// SinkOrder lists the sink node ids in sink order.
	SinkOrder []string `json:"-"`
}

// TestPipeline loads the saved pipeline, reads up to InferSampleLimit
// records from its source within sampleTimeout, returns the first
// SampleLimit, and checks its sinks. The rows each sink would write, for
// schema inference, come from the sample or, when larger, the last records
// a running pipeline took in. Nothing is written and no source position moves. A config that
// does not validate is an error.
func (r *Runner) TestPipeline(ctx context.Context, pipelineID string) (*TestResult, error) {
	p, err := r.prepare(pipelineID)
	if err != nil {
		return nil, err
	}
	source, sourceCfg := p.source, p.sourceCfg
	res := &TestResult{SourceType: sourceCfg.NodeType, Records: []map[string]interface{}{}, SinkChecks: []SinkCheck{}}

	if s, ok := source.(Sampler); ok {
		sctx, cancel := context.WithTimeout(ctx, sampleTimeout)
		sample, err := s.Sample(sctx, sourceCfg, InferSampleLimit)
		cancel()
		if err != nil {
			res.SourceError = err.Error()
		} else {
			res.Note = sample.Note
			if sample.Records != nil {
				res.Records = sample.Records
			}
		}
	} else {
		res.Note = "This source cannot be previewed."
	}
	// Inference reads the larger of the sample and what a running pipeline
	// last took in (a webhook keeps only its last 20 records).
	all := res.Records
	if rp := r.running(pipelineID); rp != nil {
		if ring := rp.sample.all(); len(ring) > len(all) {
			all = ring
		}
	}
	if len(res.Records) > SampleLimit {
		res.Records = res.Records[:SampleLimit]
	}
	res.SinkRecords, res.SinkOrder = sinkSamples(p, all)

	// With several sinks every check carries its sink's node id and name.
	for _, sp := range p.sinks {
		c, ok := sp.sink.(SinkChecker)
		if !ok {
			continue
		}
		cctx, cancel := context.WithTimeout(ctx, sampleTimeout)
		checks := c.Check(cctx, sp.cfg)
		cancel()
		if len(p.sinks) > 1 {
			for i := range checks {
				checks[i].NodeID = sp.nodeID
				checks[i].Name = sp.name + ": " + checks[i].Name
			}
		}
		res.SinkChecks = append(res.SinkChecks, checks...)
	}
	return res, nil
}

// sinkSamples runs sample records through the shared chain, the route and
// each branch chain, and returns every sink's rows by node id, with the
// sink ids in order.
func sinkSamples(p *preparedPipeline, records []map[string]interface{}) (map[string][]map[string]interface{}, []string) {
	if len(records) > InferSampleLimit {
		records = records[:InferSampleLimit]
	}
	out := make(map[string][]map[string]interface{}, len(p.sinks))
	order := make([]string, 0, len(p.sinks))
	recs := make([]Record, len(records))
	for i, m := range records {
		recs[i] = Record{Data: m}
	}
	rp := &RunningPipeline{chain: p.chain, route: p.route, sinks: p.sinks}
	parts := rp.sinkBatches(Batch{Records: recs})
	for i, sp := range p.sinks {
		rows := make([]map[string]interface{}, 0, len(parts[i].Records))
		for _, rec := range parts[i].Records {
			rows = append(rows, decodeRecord(rec))
		}
		out[sp.nodeID] = rows
		order = append(order, sp.nodeID)
	}
	return out, order
}

// ServerJSONType reports, with a read-only SELECT version(), whether the
// server has the JSON type (ClickHouse 25.3 and later).
func ServerJSONType(ctx context.Context, gw *tunnel.Gateway, connectionID, user, password string) (bool, error) {
	res, err := gw.ExecuteQueryWithSettingsCtx(ctx, connectionID, "SELECT version() AS v", user, password, map[string]string{"readonly": "1"}, inferTimeout)
	if err != nil {
		return false, err
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(res.Data, &rows); err != nil || len(rows) == 0 {
		return false, fmt.Errorf("read version(): unexpected result")
	}
	v, _ := rows[0]["v"].(string)
	return jsonTypeSupported(v), nil
}

// InferSchema infers column types for sample records (see InferColumns),
// asking the server whether it has the JSON type, and suggests ORDER BY
// and PARTITION BY.
func InferSchema(ctx context.Context, gw *tunnel.Gateway, connectionID, user, password string, records []map[string]interface{}) (*InferredSchema, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("no sample records to infer a schema from")
	}
	jsonType, err := ServerJSONType(ctx, gw, connectionID, user, password)
	if err != nil {
		return nil, err
	}
	s := InferSchemaFrom(records, jsonType)
	if len(s.Columns) == 0 {
		return nil, fmt.Errorf("the sample records have no fields to infer columns from")
	}
	return &s, nil
}
