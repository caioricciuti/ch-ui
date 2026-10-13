package models

import (
	"fmt"
	"strings"
)

// A selector picks models the way dbt's --select does, minus tags and paths:
//
//	name     the model
//	+name    the model and everything upstream of it
//	name+    the model and everything downstream of it
//	+name+   both
//	*        every model
//
// Terms are separated by commas (or spaces); the selection is their union.

// selectorTerm is one parsed term.
type selectorTerm struct {
	name       string
	upstream   bool
	downstream bool
}

// ParseSelector splits a selector into terms and checks each one's syntax.
func ParseSelector(selector string) ([]selectorTerm, error) {
	fields := strings.FieldsFunc(selector, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	if len(fields) == 0 {
		return nil, fmt.Errorf("selector is empty")
	}
	terms := make([]selectorTerm, 0, len(fields))
	for _, f := range fields {
		t := selectorTerm{name: f}
		if strings.HasPrefix(t.name, "+") {
			t.upstream, t.name = true, t.name[1:]
		}
		if strings.HasSuffix(t.name, "+") {
			t.downstream, t.name = true, t.name[:len(t.name)-1]
		}
		if t.name != "*" {
			if err := ValidateModelName(t.name); err != nil {
				return nil, fmt.Errorf("selector term %q: %w", f, err)
			}
		}
		terms = append(terms, t)
	}
	return terms, nil
}

func (t selectorTerm) String() string {
	s := t.name
	if t.upstream {
		s = "+" + s
	}
	if t.downstream {
		s += "+"
	}
	return s
}

// RenameInSelector replaces the model oldName with newName in a selector,
// keeping each term's graph operators. The result is re-serialized with
// commas. A selector that does not parse or does not name oldName is returned
// unchanged with false.
func RenameInSelector(selector, oldName, newName string) (string, bool) {
	terms, err := ParseSelector(selector)
	if err != nil {
		return selector, false
	}
	changed := false
	parts := make([]string, len(terms))
	for i, t := range terms {
		if t.name == oldName {
			t.name = newName
			changed = true
		}
		parts[i] = t.String()
	}
	if !changed {
		return selector, false
	}
	return strings.Join(parts, ","), true
}

// ValidateSelector checks a selector's syntax and, when names is not nil,
// that every model it names exists.
func ValidateSelector(selector string, names map[string]bool) error {
	terms, err := ParseSelector(selector)
	if err != nil {
		return err
	}
	if names == nil {
		return nil
	}
	for _, t := range terms {
		if t.name != "*" && !names[t.name] {
			return fmt.Errorf("selector names unknown model %q", t.name)
		}
	}
	return nil
}

// Select returns the IDs the selector picks, in the DAG's topological order.
func (g *DepGraph) Select(selector string, nameToID map[string]string) ([]string, error) {
	terms, err := ParseSelector(selector)
	if err != nil {
		return nil, err
	}
	picked := make(map[string]bool)
	for _, t := range terms {
		if t.name == "*" {
			for _, id := range g.Order {
				picked[id] = true
			}
			continue
		}
		id, ok := nameToID[t.name]
		if !ok {
			return nil, fmt.Errorf("selector names unknown model %q", t.name)
		}
		picked[id] = true
		if t.upstream {
			for dep := range GetUpstreamDeps(id, g.Deps) {
				picked[dep] = true
			}
		}
		if t.downstream {
			for dep := range GetUpstreamDeps(id, g.RevDeps) {
				picked[dep] = true
			}
		}
	}
	var out []string
	for _, id := range g.Order {
		if picked[id] {
			out = append(out, id)
		}
	}
	return out, nil
}
