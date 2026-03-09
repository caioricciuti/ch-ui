package models

import "fmt"

// DepGraph is the resolved DAG of model dependencies.
type DepGraph struct {
	// Order is the topological execution order (model IDs).
	Order []string
	// Deps maps model_id -> [dependency model_ids] (upstream).
	Deps map[string][]string
	// RevDeps maps model_id -> [dependent model_ids] (downstream).
	RevDeps map[string][]string
}

// BuildDAG constructs the dependency graph and returns topological order.
// modelIDs: all model IDs.
// refsByID: model_id -> [referenced model names from $ref()].
// nameToID: model_name -> model_id.
func BuildDAG(modelIDs []string, refsByID map[string][]string, nameToID map[string]string) (*DepGraph, error) {
	g := &DepGraph{
		Deps:    make(map[string][]string),
		RevDeps: make(map[string][]string),
	}

	// Build in-degree map
	inDegree := make(map[string]int, len(modelIDs))
	for _, id := range modelIDs {
		inDegree[id] = 0
	}

	for id, refNames := range refsByID {
		for _, refName := range refNames {
			depID, ok := nameToID[refName]
			if !ok {
				return nil, fmt.Errorf("model references unknown model %q via $ref()", refName)
			}
			if depID == id {
				return nil, fmt.Errorf("model cannot reference itself via $ref(%s)", refName)
			}
			g.Deps[id] = append(g.Deps[id], depID)
			g.RevDeps[depID] = append(g.RevDeps[depID], id)
			inDegree[id]++
		}
	}

	// Kahn's algorithm for topological sort
	var queue []string
	for _, id := range modelIDs {
		if inDegree[id] == 0 {
			queue = append(queue, id)
		}
	}

	var order []string
	for len(queue) > 0 {
		curr := queue[0]
		queue = queue[1:]
		order = append(order, curr)

		for _, downstream := range g.RevDeps[curr] {
			inDegree[downstream]--
			if inDegree[downstream] == 0 {
				queue = append(queue, downstream)
			}
		}
	}

	if len(order) != len(modelIDs) {
		return nil, fmt.Errorf("cycle detected in model dependencies")
	}

	g.Order = order
	return g, nil
}

// ConnectedComponents returns groups of model IDs (independent pipelines).
// Each group preserves topological order from g.Order.
func (g *DepGraph) ConnectedComponents() [][]string {
	all := make(map[string]bool, len(g.Order))
	for _, id := range g.Order {
		all[id] = true
	}

	visited := make(map[string]bool, len(g.Order))
	var components [][]string

	for _, id := range g.Order {
		if visited[id] {
			continue
		}
		// BFS on undirected edges
		component := make(map[string]bool)
		queue := []string{id}
		for len(queue) > 0 {
			cur := queue[0]
			queue = queue[1:]
			if visited[cur] {
				continue
			}
			visited[cur] = true
			component[cur] = true
			for _, dep := range g.Deps[cur] {
				if !visited[dep] && all[dep] {
					queue = append(queue, dep)
				}
			}
			for _, rev := range g.RevDeps[cur] {
				if !visited[rev] && all[rev] {
					queue = append(queue, rev)
				}
			}
		}
		// Filter g.Order to preserve topological order
		var ordered []string
		for _, oid := range g.Order {
			if component[oid] {
				ordered = append(ordered, oid)
			}
		}
		components = append(components, ordered)
	}

	return components
}

// ComponentContaining returns the component that includes modelID,
// preserving topological order from g.Order.
func (g *DepGraph) ComponentContaining(modelID string) []string {
	for _, comp := range g.ConnectedComponents() {
		for _, id := range comp {
			if id == modelID {
				return comp
			}
		}
	}
	return nil
}

// GetUpstreamDeps returns the transitive upstream dependencies for a model ID.
func GetUpstreamDeps(modelID string, deps map[string][]string) map[string]bool {
	visited := make(map[string]bool)
	var walk func(id string)
	walk = func(id string) {
		for _, depID := range deps[id] {
			if !visited[depID] {
				visited[depID] = true
				walk(depID)
			}
		}
	}
	walk(modelID)
	return visited
}
