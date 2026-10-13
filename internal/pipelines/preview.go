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
}

// TestPipeline loads the saved pipeline, reads up to SampleLimit records from
// its source within sampleTimeout, and checks its sink. Nothing is written
// and no source position moves. A config that does not validate is an error.
func (r *Runner) TestPipeline(ctx context.Context, pipelineID string) (*TestResult, error) {
	source, sink, sourceCfg, sinkCfg, err := r.prepare(pipelineID)
	if err != nil {
		return nil, err
	}
	res := &TestResult{SourceType: sourceCfg.NodeType, Records: []map[string]interface{}{}, SinkChecks: []SinkCheck{}}

	if s, ok := source.(Sampler); ok {
		sctx, cancel := context.WithTimeout(ctx, sampleTimeout)
		sample, err := s.Sample(sctx, sourceCfg, SampleLimit)
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

	if c, ok := sink.(SinkChecker); ok {
		cctx, cancel := context.WithTimeout(ctx, sampleTimeout)
		res.SinkChecks = c.Check(cctx, sinkCfg)
		cancel()
	}
	return res, nil
}

// InferSchema asks ClickHouse to infer column types for sample records with
// DESC format(JSONEachRow, ...), as a read-only query with the given
// credentials, and suggests ORDER BY and PARTITION BY.
func InferSchema(ctx context.Context, gw *tunnel.Gateway, connectionID, user, password string, records []map[string]interface{}) (*InferredSchema, error) {
	if len(records) == 0 {
		return nil, fmt.Errorf("no sample records to infer a schema from")
	}
	q, err := describeFormatQuery(records)
	if err != nil {
		return nil, err
	}
	res, err := gw.ExecuteQueryWithSettingsCtx(ctx, connectionID, q, user, password, map[string]string{"readonly": "1"}, inferTimeout)
	if err != nil {
		return nil, err
	}
	var rows []map[string]interface{}
	if err := json.Unmarshal(res.Data, &rows); err != nil {
		return nil, fmt.Errorf("read DESC result: %w", err)
	}
	s := SchemaFromDescribe(rows)
	if len(s.Columns) == 0 {
		return nil, fmt.Errorf("ClickHouse inferred no columns from the sample")
	}
	return &s, nil
}
