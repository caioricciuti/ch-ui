package transform

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

type ruleConfig struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}

type ruleSetConfig struct {
	Match string       `json:"match"`
	Rules []ruleConfig `json:"rules"`
}

type operand struct {
	s     string
	f     float64
	isNum bool
}

func newOperand(v any) operand {
	o := operand{s: Stringify(v)}
	o.f, o.isNum = toFloat(v)
	return o
}

type rule struct {
	field Path
	op    string
	val   operand
	re    *regexp.Regexp
	set   []operand
}

// ruleSet matches a record against rules, all or any of them.
type ruleSet struct {
	any   bool
	rules []rule
}

func compileRuleSet(cfg ruleSetConfig, opts Options) (ruleSet, error) {
	rs := ruleSet{}
	switch cfg.Match {
	case "", "all":
	case "any":
		rs.any = true
	default:
		return rs, fmt.Errorf("match must be all or any")
	}
	if len(cfg.Rules) > maxRules {
		return rs, fmt.Errorf("at most %d rules", maxRules)
	}
	for i, rc := range cfg.Rules {
		r, err := compileRule(rc, opts)
		if err != nil {
			return rs, fmt.Errorf("rule %d: %w", i+1, err)
		}
		rs.rules = append(rs.rules, r)
	}
	return rs, nil
}

func compileRule(rc ruleConfig, opts Options) (rule, error) {
	p, err := ParsePath(rc.Field, opts)
	if err != nil {
		return rule{}, err
	}
	r := rule{field: p, op: rc.Op}
	switch rc.Op {
	case "exists", "missing":
	case "eq", "ne", "contains", "not_contains", "gt", "gte", "lt", "lte":
		r.val = newOperand(rc.Value)
	case "matches":
		pat := Stringify(rc.Value)
		if pat == "" {
			return rule{}, fmt.Errorf("matches needs a pattern")
		}
		if len(pat) > maxPatternLen {
			return rule{}, fmt.Errorf("pattern is longer than %d characters", maxPatternLen)
		}
		re, err := regexp.Compile(pat)
		if err != nil {
			return rule{}, fmt.Errorf("pattern: %v", err)
		}
		r.re = re
	case "in":
		vals, err := inValues(rc.Value)
		if err != nil {
			return rule{}, err
		}
		for _, v := range vals {
			r.set = append(r.set, newOperand(v))
		}
	case "":
		return rule{}, fmt.Errorf("op is required")
	default:
		return rule{}, fmt.Errorf("unknown op %q", rc.Op)
	}
	return r, nil
}

// inValues reads the value of an "in" rule: a JSON array, or a string that
// holds one.
func inValues(v any) ([]any, error) {
	var vals []any
	switch x := v.(type) {
	case []any:
		vals = x
	case string:
		dec := json.NewDecoder(strings.NewReader(x))
		dec.UseNumber()
		if err := dec.Decode(&vals); err != nil {
			return nil, fmt.Errorf("in needs a JSON array, like [\"a\", \"b\"]")
		}
	default:
		return nil, fmt.Errorf("in needs a JSON array, like [\"a\", \"b\"]")
	}
	if len(vals) > maxInValues {
		return nil, fmt.Errorf("in has more than %d values", maxInValues)
	}
	return vals, nil
}

func (rs ruleSet) match(rec map[string]any) bool {
	if len(rs.rules) == 0 {
		return !rs.any
	}
	for _, r := range rs.rules {
		m := r.match(rec)
		if rs.any && m {
			return true
		}
		if !rs.any && !m {
			return false
		}
	}
	return !rs.any
}

func (r rule) match(rec map[string]any) bool {
	v, ok := r.field.get(rec)
	present := ok && v != nil
	switch r.op {
	case "exists":
		return present
	case "missing":
		return !present
	case "ne":
		return !present || !equal(newOperand(v), r.val)
	case "not_contains":
		return !present || !strings.Contains(Stringify(v), r.val.s)
	}
	if !present {
		return false
	}
	switch r.op {
	case "eq":
		return equal(newOperand(v), r.val)
	case "contains":
		return strings.Contains(Stringify(v), r.val.s)
	case "matches":
		return r.re.MatchString(Stringify(v))
	case "in":
		o := newOperand(v)
		for _, s := range r.set {
			if equal(o, s) {
				return true
			}
		}
		return false
	case "gt", "gte", "lt", "lte":
		c := compare(newOperand(v), r.val)
		switch r.op {
		case "gt":
			return c > 0
		case "gte":
			return c >= 0
		case "lt":
			return c < 0
		default:
			return c <= 0
		}
	}
	return false
}

// equal compares as numbers when both sides are numbers, else as text.
func equal(a, b operand) bool {
	if a.isNum && b.isNum {
		return a.f == b.f
	}
	return a.s == b.s
}

func compare(a, b operand) int {
	if a.isNum && b.isNum {
		switch {
		case a.f < b.f:
			return -1
		case a.f > b.f:
			return 1
		}
		return 0
	}
	return strings.Compare(a.s, b.s)
}

type filterConfig struct {
	Mode  string       `json:"mode"`
	Match string       `json:"match"`
	Rules []ruleConfig `json:"rules"`
}

type filterNode struct {
	drop  bool
	rules ruleSet
}

func newFilter(raw map[string]any, opts Options) (processor, error) {
	var cfg filterConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return nil, err
	}
	n := &filterNode{}
	switch cfg.Mode {
	case "", "keep":
	case "drop":
		n.drop = true
	default:
		return nil, fmt.Errorf("mode must be keep or drop")
	}
	rs, err := compileRuleSet(ruleSetConfig{Match: cfg.Match, Rules: cfg.Rules}, opts)
	if err != nil {
		return nil, err
	}
	n.rules = rs
	return n, nil
}

func (n *filterNode) process(rec map[string]any) (bool, string, error) {
	if len(n.rules.rules) == 0 {
		return true, "", nil // nothing configured yet
	}
	m := n.rules.match(rec)
	switch {
	case n.drop && m:
		return false, "filter: matched the drop rules", nil
	case !n.drop && !m:
		return false, "filter: did not match the keep rules", nil
	}
	return true, "", nil
}
