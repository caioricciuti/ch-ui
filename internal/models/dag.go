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
