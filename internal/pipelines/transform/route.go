package transform

import (
	"fmt"
	"regexp"
	"sync/atomic"
)

// TypeRoute splits records between the branches of a pipeline. It is not a
// chain node: a pipeline has at most one, between the shared chain and the
// branches, and each of its outputs leads to one sink.
const TypeRoute = "transform_route"

// UnmatchedHandle is the output of records that match no route.
const UnmatchedHandle = "unmatched"

// Limits of one route node.
const (
	MaxRoutes       = 16
	maxRouteNameLen = 100
)

var routeIDPattern = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// IsRoute reports whether nodeType is the route node.
func IsRoute(nodeType string) bool { return nodeType == TypeRoute }

type routeEntryConfig struct {
	ID    string       `json:"id"`
	Name  string       `json:"name"`
	Match string       `json:"match"`
	Rules []ruleConfig `json:"rules"`
}

type routeConfig struct {
	Mode      string             `json:"mode"`
	Routes    []routeEntryConfig `json:"routes"`
	Unmatched string             `json:"unmatched"`
}

type routeOut struct {
	id    string
	rules ruleSet
	out   atomic.Int64
}

// Route is a compiled route node. Routing is deterministic: a record's
// outputs depend only on the record. Split, Preview and Counters are safe
// for concurrent use.
type Route struct {
	spec   NodeSpec
	all    bool
	routes []*routeOut
	// unmatchedOut sends records that match no route to the unmatched
	// output; otherwise they are dropped and counted.
	unmatchedOut bool

	in, unmatchedSent, unmatchedDropped atomic.Int64
}

// RouteHandles returns the output handles a route config declares: the
// route ids in order, then UnmatchedHandle when unmatched records have an
// output. It checks the ids only; CompileRoute checks the rest.
func RouteHandles(spec NodeSpec) ([]string, error) {
	cfg, err := decodeRouteConfig(spec)
	if err != nil {
		return nil, err
	}
	handles := make([]string, 0, len(cfg.Routes)+1)
	for _, rc := range cfg.Routes {
		handles = append(handles, rc.ID)
	}
	if cfg.Unmatched == "output" {
		handles = append(handles, UnmatchedHandle)
	}
	return handles, nil
}

func decodeRouteConfig(spec NodeSpec) (routeConfig, error) {
	var cfg routeConfig
	if err := decodeConfig(spec.Config, &cfg); err != nil {
		return cfg, nodeErr(spec, "%s", err.Error())
	}
	switch cfg.Mode {
	case "", "first", "all":
	default:
		return cfg, nodeErr(spec, "mode must be first or all")
	}
	switch cfg.Unmatched {
	case "", "drop", "output":
	default:
		return cfg, nodeErr(spec, "unmatched must be drop or output")
	}
	if len(cfg.Routes) > MaxRoutes {
		return cfg, nodeErr(spec, "at most %d routes", MaxRoutes)
	}
	seen := map[string]bool{}
	for i, rc := range cfg.Routes {
		switch {
		case !routeIDPattern.MatchString(rc.ID):
			return cfg, nodeErr(spec, "%s: id must be 1 to 32 characters of a-z, 0-9, _ and -", routeName(i, rc))
		case rc.ID == UnmatchedHandle:
			return cfg, nodeErr(spec, "%s: id %q is reserved for unmatched records", routeName(i, rc), rc.ID)
		case seen[rc.ID]:
			return cfg, nodeErr(spec, "%s: id %q is used by another route", routeName(i, rc), rc.ID)
		case len(rc.Name) > maxRouteNameLen:
			return cfg, nodeErr(spec, "%s: name is longer than %d characters", routeName(i, rc), maxRouteNameLen)
		}
		seen[rc.ID] = true
	}
	return cfg, nil
}

func routeName(i int, rc routeEntryConfig) string {
	if rc.Name != "" {
		return fmt.Sprintf("route %d (%q)", i+1, rc.Name)
	}
	return fmt.Sprintf("route %d", i+1)
}

// CompileRoute validates and compiles a route node.
func CompileRoute(spec NodeSpec, opts Options) (*Route, error) {
	cfg, err := decodeRouteConfig(spec)
	if err != nil {
		return nil, err
	}
	r := &Route{spec: spec, all: cfg.Mode == "all", unmatchedOut: cfg.Unmatched == "output"}
	for i, rc := range cfg.Routes {
		rs, err := compileRuleSet(ruleSetConfig{Match: rc.Match, Rules: rc.Rules}, opts)
		if err != nil {
			return nil, nodeErr(spec, "%s: %s", routeName(i, rc), err.Error())
		}
		r.routes = append(r.routes, &routeOut{id: rc.ID, rules: rs})
	}
	return r, nil
}

// DropUnmatched makes records that match no route be dropped even when the
// config gives them an output, for a pipeline that left that output
// unconnected. Call it before the route runs.
func (r *Route) DropUnmatched() { r.unmatchedOut = false }

// ID is the node id.
func (r *Route) ID() string { return r.spec.ID }

// Handles returns the outputs Split fills, in order: the route ids, then
// UnmatchedHandle when unmatched records have an output.
func (r *Route) Handles() []string {
	out := make([]string, 0, len(r.routes)+1)
	for _, rt := range r.routes {
		out = append(out, rt.id)
	}
	if r.unmatchedOut {
		out = append(out, UnmatchedHandle)
	}
	return out
}

func (rt *routeOut) matches(rec map[string]any) bool {
	return len(rt.rules.rules) == 0 || rt.rules.match(rec)
}

// assign returns, for every record, the indexes of the routes it goes to;
// an empty list means it matched none.
func (r *Route) assign(recs []map[string]any) [][]int {
	out := make([][]int, len(recs))
	for i, rec := range recs {
		for j, rt := range r.routes {
			if rt.matches(rec) {
				out[i] = append(out[i], j)
				if !r.all {
					break
				}
			}
		}
	}
	return out
}

// Split routes records for a running pipeline and counts them. It returns,
// for every handle in Handles order, the indexes of the records that go
// there, in input order. Records are not changed.
func (r *Route) Split(recs []map[string]any) [][]int {
	outs := make([][]int, len(r.Handles()))
	var unmatched int64
	for i, routes := range r.assign(recs) {
		if len(routes) == 0 {
			unmatched++
			if r.unmatchedOut {
				outs[len(r.routes)] = append(outs[len(r.routes)], i)
			}
			continue
		}
		for _, j := range routes {
			outs[j] = append(outs[j], i)
		}
	}
	r.in.Add(int64(len(recs)))
	for j, rt := range r.routes {
		rt.out.Add(int64(len(outs[j])))
	}
	if r.unmatchedOut {
		r.unmatchedSent.Add(unmatched)
	} else {
		r.unmatchedDropped.Add(unmatched)
	}
	return outs
}

// RouteCounters are the extra totals of a route node.
type RouteCounters struct {
	Outputs          []OutputCounters `json:"outputs"`
	UnmatchedDropped int64            `json:"unmatched_dropped"`
}

// OutputCounters is how many records one route output sent on.
type OutputCounters struct {
	Handle string `json:"handle"`
	Out    int64  `json:"out"`
}

// Counters returns the route's totals. Out is the sum of the outputs (a
// record sent to two routes counts twice) and Dropped the unmatched records
// that were dropped.
func (r *Route) Counters() Counters {
	rc := &RouteCounters{Outputs: []OutputCounters{}}
	c := Counters{NodeID: r.spec.ID, In: r.in.Load(), RouteCounters: rc}
	for _, rt := range r.routes {
		n := rt.out.Load()
		rc.Outputs = append(rc.Outputs, OutputCounters{Handle: rt.id, Out: n})
		c.Out += n
	}
	if r.unmatchedOut {
		n := r.unmatchedSent.Load()
		rc.Outputs = append(rc.Outputs, OutputCounters{Handle: UnmatchedHandle, Out: n})
		c.Out += n
	}
	rc.UnmatchedDropped = r.unmatchedDropped.Load()
	c.Dropped = rc.UnmatchedDropped
	return c
}

// RouteResult is what a route step of a preview did: the input indexes
// each output got and those that matched no route.
type RouteResult struct {
	Routes    []RouteStep `json:"routes"`
	Unmatched []int       `json:"unmatched"`
}

// RouteStep is the route output of a preview step.
type RouteStep struct {
	Handle string `json:"handle"`
	Kept   []int  `json:"kept"`
}

// Preview routes copies of recs without counting. Without a branch the
// step's output is every record that goes to at least one output; with a
// branch (a handle) only that output's records, which the nodes after the
// route then see.
func (r *Route) Preview(recs []map[string]any, branch string) Step {
	st := Step{NodeID: r.spec.ID, Output: []map[string]any{}, Errors: []StepIssue{}, Kept: []int{}, Drops: []StepDrop{}}
	st.RouteResult = &RouteResult{Routes: make([]RouteStep, 0, len(r.routes)+1), Unmatched: []int{}}
	for _, rt := range r.routes {
		st.Routes = append(st.Routes, RouteStep{Handle: rt.id, Kept: []int{}})
	}
	for i, routes := range r.assign(recs) {
		if len(routes) == 0 {
			st.Unmatched = append(st.Unmatched, i)
		}
		for _, j := range routes {
			st.Routes[j].Kept = append(st.Routes[j].Kept, i)
		}
		pass, reason := len(routes) > 0 || r.unmatchedOut, "route: matched no route"
		if branch != "" {
			pass = false
			if branch == UnmatchedHandle {
				pass = len(routes) == 0
			}
			for _, j := range routes {
				pass = pass || r.routes[j].id == branch
			}
			if !pass && len(routes) > 0 {
				reason = "route: goes to another output"
			}
		}
		if pass {
			st.Output = append(st.Output, CopyRecord(recs[i]))
			st.Kept = append(st.Kept, i)
			continue
		}
		st.Dropped++
		st.Drops = append(st.Drops, StepDrop{Index: i, Reason: reason})
	}
	if r.unmatchedOut {
		st.Routes = append(st.Routes, RouteStep{Handle: UnmatchedHandle, Kept: append([]int{}, st.Unmatched...)})
	}
	return st
}

// PreviewPlan is a compiled preview of nodes in order that may hold one
// route node: the nodes before it, the route, and the nodes of one branch
// after it.
type PreviewPlan struct {
	before *Chain
	route  *Route
	after  *Chain
	branch string
}

// CompilePreview compiles nodes for a preview. branch names the route
// output whose records the nodes after the route see; it is needed when
// nodes follow the route.
func CompilePreview(specs []NodeSpec, branch string, opts Options) (*PreviewPlan, error) {
	at := -1
	for i, s := range specs {
		if IsRoute(s.Type) {
			if at >= 0 {
				return nil, fmt.Errorf("a pipeline can have at most one Route node")
			}
			at = i
		}
	}
	p := &PreviewPlan{branch: branch}
	if at < 0 {
		if branch != "" {
			return nil, fmt.Errorf("branch needs a Route node in nodes")
		}
		c, err := Compile(specs, opts)
		if err != nil {
			return nil, err
		}
		p.before = c
		return p, nil
	}
	before, err := Compile(specs[:at], opts)
	if err != nil {
		return nil, err
	}
	route, err := CompileRoute(specs[at], opts)
	if err != nil {
		return nil, err
	}
	if branch == "" && at < len(specs)-1 {
		return nil, fmt.Errorf("Set branch to preview nodes after a Route node")
	}
	if branch != "" {
		ok := false
		for _, h := range route.Handles() {
			ok = ok || h == branch
		}
		if !ok {
			return nil, fmt.Errorf("branch %q is not an output of the Route node", branch)
		}
	}
	after, err := Compile(specs[at+1:], opts)
	if err != nil {
		return nil, err
	}
	p.before, p.route, p.after = before, route, after
	return p, nil
}

// Preview runs the plan on copies of recs and reports every step.
func (p *PreviewPlan) Preview(recs []map[string]any) []Step {
	steps := p.before.Preview(recs)
	if p.route == nil {
		return steps
	}
	cur := recs
	if len(steps) > 0 {
		cur = steps[len(steps)-1].Output
	}
	st := p.route.Preview(cur, p.branch)
	return append(append(steps, st), p.after.Preview(st.Output)...)
}
