package pipelines

import (
	"encoding/json"
	"fmt"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/pipelines/transform"
)

// MaxSinks is the most sinks one pipeline may write to.
const MaxSinks = 8

// Graph is a validated pipeline graph: the source, the chain every record
// runs through, and either one sink or a route node whose outputs each lead
// through their own chain to one sink.
type Graph struct {
	Source *database.PipelineNode
	Shared []database.PipelineNode
	// Route is nil for a source -> transforms -> sink pipeline.
	Route    *database.PipelineNode
	Branches []Branch
	// UnmatchedConnected is set when the route's unmatched output has a
	// branch.
	UnmatchedConnected bool
}

// Branch is the path from one route output (or, without a route, from the
// shared chain) to a sink.
type Branch struct {
	// Handle is the route output; empty without a route.
	Handle string
	Chain  []database.PipelineNode
	Sink   *database.PipelineNode
}

// Sinks returns the sink nodes in branch order.
func (g *Graph) Sinks() []*database.PipelineNode {
	out := make([]*database.PipelineNode, 0, len(g.Branches))
	for _, b := range g.Branches {
		out = append(out, b.Sink)
	}
	return out
}

// ResolveGraph checks a pipeline graph and returns it in order. A graph is
// source -> transforms -> sink, or source -> transforms -> route, with each
// route output connected to transforms -> sink. Errors name the node by its
// label.
func ResolveGraph(nodes []database.PipelineNode, edges []database.PipelineEdge) (*Graph, error) {
	var source, route *database.PipelineNode
	var sinkNodes []*database.PipelineNode
	byID := make(map[string]*database.PipelineNode, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		switch {
		case isSourceType(n.NodeType):
			if source != nil {
				return nil, graphErr(n, "a pipeline can have only one source")
			}
			source = n
		case n.NodeType == "sink_clickhouse":
			sinkNodes = append(sinkNodes, n)
		case transform.IsRoute(n.NodeType):
			if route != nil {
				return nil, graphErr(n, "a pipeline can have at most one Route node")
			}
			route = n
		case !transform.IsTransform(n.NodeType):
			return nil, graphErr(n, "unknown node type %q", n.NodeType)
		}
		byID[n.ID] = n
	}
	sinks := len(sinkNodes)
	switch {
	case source == nil:
		return nil, graphErr(nil, "Add a source first: drag Kafka, Webhook, Database, S3 or Ingest from the palette onto the canvas")
	case sinks == 0:
		return nil, graphErr(nil, "Add a ClickHouse sink: drag it from the palette and connect the source to it")
	case sinks > MaxSinks:
		return nil, graphErr(sinkNodes[MaxSinks], "a pipeline can have at most %d sinks", MaxSinks)
	case sinks > 1 && route == nil:
		return nil, graphErr(sinkNodes[1], "a pipeline with more than one sink needs a Route node")
	}

	var handles []string
	if route != nil {
		h, err := transform.RouteHandles(routeSpec(route))
		if err != nil {
			return nil, err
		}
		handles = h
	}
	isHandle := func(h string) bool {
		for _, x := range handles {
			if x == h {
				return true
			}
		}
		return false
	}

	// One outgoing edge per node except the sink and the route (one per
	// route output), one incoming per node except the source. Duplicate
	// edges count once; edge handles only matter on the route.
	next := map[string]string{}
	byHandle := map[string]string{}
	inDeg := map[string]int{}
	seen := map[[3]string]bool{}
	for _, e := range edges {
		from, to := byID[e.SourceNodeID], byID[e.TargetNodeID]
		if from == nil || to == nil {
			continue // an edge to a deleted node
		}
		handle := ""
		if from == route && e.SourceHandle != nil {
			handle = *e.SourceHandle
		}
		key := [3]string{from.ID, to.ID, handle}
		if seen[key] {
			continue
		}
		seen[key] = true
		if from.NodeType == "sink_clickhouse" {
			return nil, graphErr(from, "a sink cannot have outgoing connections")
		}
		if to == source {
			return nil, graphErr(source, "a source cannot have incoming connections")
		}
		if from == route {
			switch {
			case handle == "":
				return nil, graphErr(route, "a connection does not say which route it carries")
			case !isHandle(handle):
				return nil, graphErr(route, "a connection leaves from output %q, which this node does not have", handle)
			}
			if _, dup := byHandle[handle]; dup {
				return nil, graphErr(route, "output %q has more than one connection", handle)
			}
			byHandle[handle] = to.ID
		} else {
			if _, dup := next[from.ID]; dup {
				return nil, graphErr(from, "more than one outgoing connection; only a Route node can branch")
			}
			next[from.ID] = to.ID
		}
		inDeg[to.ID]++
		if inDeg[to.ID] > 1 {
			return nil, graphErr(to, "more than one incoming connection")
		}
	}

	g := &Graph{Source: source, Route: route}
	visited := map[string]bool{source.ID: true}
	// walk follows the chain from a node to a sink, returning the transform
	// nodes on the way. inBranch refuses a route.
	walk := func(from string, inBranch bool) ([]database.PipelineNode, *database.PipelineNode, error) {
		var chain []database.PipelineNode
		cur := from
		for {
			to, ok := next[cur]
			if !ok {
				if cur == source.ID {
					return nil, nil, graphErr(source, "not connected to a sink")
				}
				return nil, nil, graphErr(byID[cur], "not connected to anything downstream")
			}
			n := byID[to]
			if visited[to] {
				return nil, nil, graphErr(n, "part of a cycle")
			}
			visited[to] = true
			switch {
			case n.NodeType == "sink_clickhouse":
				return chain, n, nil
			case n == route && inBranch:
				return nil, nil, graphErr(n, "a Route node cannot be inside a branch")
			case n == route:
				return chain, nil, nil
			}
			chain = append(chain, *n)
			cur = to
		}
	}

	shared, sink, err := walk(source.ID, false)
	if err != nil {
		return nil, err
	}
	g.Shared = shared
	switch {
	case sink != nil:
		g.Branches = []Branch{{Sink: sink}}
	default:
		labels := routeLabels(route)
		for _, h := range handles {
			start, ok := byHandle[h]
			if !ok {
				if h == transform.UnmatchedHandle {
					continue // unmatched records are dropped
				}
				return nil, graphErr(route, "route %q is not connected to a sink", labels[h])
			}
			var chain []database.PipelineNode
			n := byID[start]
			if visited[start] {
				return nil, graphErr(n, "part of a cycle")
			}
			visited[start] = true
			switch {
			case n.NodeType == "sink_clickhouse":
				sink = n
			case n == route:
				return nil, graphErr(n, "part of a cycle")
			default:
				rest, s, err := walk(start, true)
				if err != nil {
					return nil, err
				}
				chain, sink = append([]database.PipelineNode{*n}, rest...), s
			}
			g.Branches = append(g.Branches, Branch{Handle: h, Chain: chain, Sink: sink})
			if h == transform.UnmatchedHandle {
				g.UnmatchedConnected = true
			}
		}
		if len(g.Branches) == 0 {
			return nil, graphErr(route, "not connected to a sink")
		}
	}

	for i := range nodes {
		if !visited[nodes[i].ID] {
			return nil, graphErr(&nodes[i], "not connected between the source and a sink")
		}
	}
	total := len(g.Shared)
	for _, b := range g.Branches {
		total += len(b.Chain)
	}
	if total > transform.MaxNodes {
		return nil, graphErr(nil, "a pipeline can have at most %d transform nodes", transform.MaxNodes)
	}
	return g, nil
}

// GraphError is a problem with a pipeline's graph or node configs found at
// start: a user error, not a server failure. NodeID names the node the
// editor should mark (empty when no node is at fault) and the message
// starts with its label, like a transform's NodeError.
type GraphError struct {
	NodeID string
	Label  string
	Msg    string
}

func (e *GraphError) Error() string {
	if e.Label == "" {
		return e.Msg
	}
	return e.Label + ": " + e.Msg
}

// graphErr returns a GraphError for node n, which may be nil.
func graphErr(n *database.PipelineNode, format string, args ...any) error {
	e := &GraphError{Msg: fmt.Sprintf(format, args...)}
	if n != nil {
		e.NodeID, e.Label = n.ID, n.Label
		if e.Label == "" {
			e.Label = n.NodeType
		}
	}
	return e
}

// routeSpec is the spec of a stored route node.
func routeSpec(n *database.PipelineNode) transform.NodeSpec {
	var cfg map[string]any
	_ = json.Unmarshal([]byte(n.ConfigEncrypted), &cfg)
	return transform.NodeSpec{ID: n.ID, Type: n.NodeType, Label: n.Label, Config: cfg}
}

// routeLabels maps a route node's route ids to their names, or to the id
// when unnamed.
func routeLabels(n *database.PipelineNode) map[string]string {
	var cfg struct {
		Routes []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"routes"`
	}
	_ = json.Unmarshal([]byte(n.ConfigEncrypted), &cfg)
	out := map[string]string{}
	for _, r := range cfg.Routes {
		out[r.ID] = r.ID
		if r.Name != "" {
			out[r.ID] = r.Name
		}
	}
	return out
}
