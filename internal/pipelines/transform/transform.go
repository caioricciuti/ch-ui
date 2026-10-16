// Package transform runs the processor nodes of a pipeline (parse, map,
// filter, sample, redact) on decoded records between the source and the
// sink.
//
// Processors are deterministic: the same input always gives the same output.
// A spool segment that is replayed reuses its insert_deduplication_token, so
// ClickHouse only drops the replay if the rows are identical. Nothing here
// reads the clock or a random source.
package transform

import (
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
)

// Node types.
const (
	TypeParse  = "transform_parse"
	TypeMap    = "transform_map"
	TypeFilter = "transform_filter"
	TypeSample = "transform_sample"
	TypeRedact = "transform_redact"
)

// Limits on one chain and one node config.
const (
	MaxNodes        = 20
	maxPatternLen   = 1000
	maxRules        = 50
	maxListItems    = 100
	maxCustomKinds  = 20
	maxInValues     = 1000
	maxReplacement  = 200
	maxSetValueSize = 4096
)

// Signals of an ingest source that use the OpenTelemetry table layout.
const (
	SignalLogs   = "logs"
	SignalTraces = "traces"
)

// Options describe the records a chain sees.
type Options struct {
	// Signal is "logs" or "traces" for an ingest pipeline writing the
	// OpenTelemetry tables, else empty.
	Signal string
}

func (o Options) otel() bool { return o.Signal == SignalLogs || o.Signal == SignalTraces }
func (o Options) logs() bool { return o.Signal == SignalLogs }

// IsTransform reports whether nodeType is a processor node.
func IsTransform(nodeType string) bool {
	switch nodeType {
	case TypeParse, TypeMap, TypeFilter, TypeSample, TypeRedact:
		return true
	}
	return false
}

// ProFeature names the Pro feature a node type needs, or "" when it is free.
func ProFeature(nodeType string) string {
	switch nodeType {
	case TypeSample:
		return "Sampling"
	case TypeRedact:
		return "Redaction"
	}
	return ""
}

// NodeSpec is one processor node: its id, type, label and config.
type NodeSpec struct {
	ID     string         `json:"id"`
	Type   string         `json:"node_type"`
	Label  string         `json:"label,omitempty"`
	Config map[string]any `json:"config"`
}

func (s NodeSpec) name() string {
	switch {
	case s.Label != "":
		return s.Label
	case s.ID != "":
		return s.ID
	}
	return s.Type
}

// ErrNeedsPro is wrapped by the NodeError of a Pro node run without Pro.
var ErrNeedsPro = errors.New("needs CH-UI Pro")

// NodeError is a config or licensing problem with one node. Its message
// names the node.
type NodeError struct {
	NodeID string
	Label  string
	Msg    string
	// Pro is set when the node needs a Pro license.
	Pro bool
}

func (e *NodeError) Error() string { return e.Label + ": " + e.Msg }

// Unwrap lets errors.Is find ErrNeedsPro.
func (e *NodeError) Unwrap() error {
	if e.Pro {
		return ErrNeedsPro
	}
	return nil
}

func nodeErr(s NodeSpec, format string, args ...any) *NodeError {
	return &NodeError{NodeID: s.ID, Label: s.name(), Msg: fmt.Sprintf(format, args...)}
}

// RequirePro returns a NodeError for the first node that needs Pro when
// allowed is false, else nil.
func RequirePro(specs []NodeSpec, allowed bool) error {
	if allowed {
		return nil
	}
	for _, s := range specs {
		if f := ProFeature(s.Type); f != "" {
			e := nodeErr(s, "%s needs CH-UI Pro", f)
			e.Pro = true
			return e
		}
	}
	return nil
}

// NeedsPro reports whether any node needs Pro.
func NeedsPro(specs []NodeSpec) bool {
	for _, s := range specs {
		if ProFeature(s.Type) != "" {
			return true
		}
	}
	return false
}

// processor runs one node on a record, changing it in place. keep false
// drops the record. err reports a failure (counted as an error); reason
// explains a drop to the preview.
type processor interface {
	process(rec map[string]any) (keep bool, reason string, err error)
}

// Counters are a node's totals since the pipeline started.
type Counters struct {
	NodeID  string `json:"node_id"`
	In      int64  `json:"in"`
	Out     int64  `json:"out"`
	Dropped int64  `json:"dropped"`
	Errors  int64  `json:"errors"`
}

type node struct {
	spec NodeSpec
	proc processor

	in, out, dropped, errs atomic.Int64
}

// Chain is a compiled, ordered list of processors. Processors hold no
// state after Compile, so Run, Preview and Counters are safe for concurrent
// use.
type Chain struct {
	nodes []*node
}

// Compile validates the node configs and compiles them in order. Regular
// expressions are compiled here, once.
func Compile(specs []NodeSpec, opts Options) (*Chain, error) {
	if len(specs) > MaxNodes {
		return nil, fmt.Errorf("a pipeline can have at most %d transform nodes", MaxNodes)
	}
	c := &Chain{}
	for _, s := range specs {
		p, err := compileNode(s, opts)
		if err != nil {
			return nil, err
		}
		c.nodes = append(c.nodes, &node{spec: s, proc: p})
	}
	return c, nil
}

// Validate checks one node's config.
func Validate(spec NodeSpec, opts Options) error {
	_, err := compileNode(spec, opts)
	return err
}

func compileNode(s NodeSpec, opts Options) (processor, error) {
	var (
		p   processor
		err error
	)
	switch s.Type {
	case TypeParse:
		p, err = newParse(s.Config, opts)
	case TypeMap:
		p, err = newMap(s.Config, opts)
	case TypeFilter:
		p, err = newFilter(s.Config, opts)
	case TypeSample:
		p, err = newSample(s.Config, opts)
	case TypeRedact:
		p, err = newRedact(s.Config, opts)
	default:
		return nil, nodeErr(s, "unknown transform type %q", s.Type)
	}
	if err != nil {
		return nil, nodeErr(s, "%s", err.Error())
	}
	return p, nil
}

// Len is the number of nodes.
func (c *Chain) Len() int {
	if c == nil {
		return 0
	}
	return len(c.nodes)
}

// Run passes records through every node in order and returns the records
// that were kept, reusing recs' backing array, and for each kept record its
// index in recs. Records are changed in place.
func (c *Chain) Run(recs []map[string]any) ([]map[string]any, []int) {
	idx := make([]int, len(recs))
	for i := range idx {
		idx[i] = i
	}
	if c == nil {
		return recs, idx
	}
	for _, n := range c.nodes {
		in := len(recs)
		kept := 0
		var errs int64
		for i, rec := range recs {
			keep, _, err := n.proc.process(rec)
			if err != nil {
				errs++
			}
			if keep {
				recs[kept], idx[kept] = rec, idx[i]
				kept++
			}
		}
		// Clear the tail so dropped records can be collected.
		clear(recs[kept:in])
		recs, idx = recs[:kept], idx[:kept]
		n.in.Add(int64(in))
		n.out.Add(int64(kept))
		n.dropped.Add(int64(in - kept))
		n.errs.Add(errs)
	}
	return recs, idx
}

// Counters returns every node's totals, in chain order.
func (c *Chain) Counters() []Counters {
	if c == nil {
		return []Counters{}
	}
	out := make([]Counters, 0, len(c.nodes))
	for _, n := range c.nodes {
		out = append(out, Counters{
			NodeID:  n.spec.ID,
			In:      n.in.Load(),
			Out:     n.out.Load(),
			Dropped: n.dropped.Load(),
			Errors:  n.errs.Load(),
		})
	}
	return out
}

// Step is what one node did to its input in a preview.
type Step struct {
	NodeID  string           `json:"node_id"`
	Output  []map[string]any `json:"output"`
	Dropped int              `json:"dropped"`
	Errors  []StepIssue      `json:"errors"`
	// Kept is, for every output record, its index in the step's input.
	Kept []int `json:"kept"`
	// Drops lists the dropped input indexes with the reason, so the editor
	// can strike them out.
	Drops []StepDrop `json:"drops"`
}

// StepDrop is a record a step dropped, by its index in the step's input.
type StepDrop struct {
	Index  int    `json:"index"`
	Reason string `json:"reason"`
}

// StepIssue points at a record of the step's input.
type StepIssue struct {
	Index   int    `json:"index"`
	Message string `json:"message"`
}

// Preview runs the chain on copies of recs and reports every step. It does
// not touch the counters. recs are not changed.
func (c *Chain) Preview(recs []map[string]any) []Step {
	steps := []Step{}
	cur := recs
	for _, n := range c.nodes {
		st := Step{NodeID: n.spec.ID, Output: []map[string]any{}, Errors: []StepIssue{}, Kept: []int{}, Drops: []StepDrop{}}
		for i, rec := range cur {
			rec = CopyRecord(rec)
			keep, reason, err := n.proc.process(rec)
			if err != nil {
				st.Errors = append(st.Errors, StepIssue{Index: i, Message: err.Error()})
				if reason == "" {
					reason = err.Error()
				}
			}
			if keep {
				st.Output = append(st.Output, rec)
				st.Kept = append(st.Kept, i)
				continue
			}
			st.Dropped++
			st.Drops = append(st.Drops, StepDrop{Index: i, Reason: reason})
		}
		steps = append(steps, st)
		cur = st.Output
	}
	return steps
}

// decodeConfig converts a node config map into a typed struct.
func decodeConfig(cfg map[string]any, v any) error {
	if cfg == nil {
		cfg = map[string]any{}
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("invalid config: %v", err)
	}
	if err := json.Unmarshal(b, v); err != nil {
		return fmt.Errorf("invalid config: %v", err)
	}
	return nil
}

// number is a config number that may arrive as a JSON number or a numeric
// string from a form field.
type number struct {
	set bool
	v   float64
}

func (n *number) UnmarshalJSON(b []byte) error {
	if string(b) == "null" {
		return nil
	}
	var f float64
	if err := json.Unmarshal(b, &f); err == nil {
		n.set, n.v = true, f
		return nil
	}
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("expected a number")
	}
	if s == "" {
		return nil
	}
	f, ok := toFloat(s)
	if !ok {
		return fmt.Errorf("expected a number, got %q", s)
	}
	n.set, n.v = true, f
	return nil
}
