package transform

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// maxPathLen bounds one field path.
const maxPathLen = 256

// otelMapColumns are the Map(String, String) columns of the OpenTelemetry
// logs and traces tables. Values written into them are stringified.
var otelMapColumns = map[string]bool{
	"LogAttributes":      true,
	"ResourceAttributes": true,
	"ScopeAttributes":    true,
	"SpanAttributes":     true,
}

// Path is a parsed field path: "name", "a.b.c" or "Attrs['k']". On an
// OpenTelemetry pipeline a path into a map column has exactly two segments,
// the column and the key, and "LogAttributes.http.method" names the key
// "http.method" because attribute keys usually contain dots.
type Path struct {
	raw    string
	segs   []string
	mapCol bool // segs[0] is an OpenTelemetry map column
}

func (p Path) String() string { return p.raw }

// ParsePath parses and validates a field path.
func ParsePath(s string, opts Options) (Path, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return Path{}, fmt.Errorf("field path is empty")
	}
	if len(s) > maxPathLen {
		return Path{}, fmt.Errorf("field path is longer than %d characters", maxPathLen)
	}
	if opts.otel() {
		if i := strings.IndexByte(s, '.'); i > 0 && otelMapColumns[s[:i]] {
			key := s[i+1:]
			if key == "" {
				return Path{}, fmt.Errorf("field path %q ends with a dot", s)
			}
			return Path{raw: s, segs: []string{s[:i], key}, mapCol: true}, nil
		}
	}
	segs, err := splitPath(s)
	if err != nil {
		return Path{}, err
	}
	p := Path{raw: s, segs: segs}
	if opts.otel() && otelMapColumns[segs[0]] {
		if len(segs) > 2 {
			return Path{}, fmt.Errorf("field path %q: %s has one level of keys", s, segs[0])
		}
		p.mapCol = true
	}
	return p, nil
}

// splitPath splits "a.b['c.d'].e" into segments.
func splitPath(s string) ([]string, error) {
	var segs []string
	i := 0
	for i < len(s) {
		if s[i] == '[' {
			if i+1 >= len(s) || (s[i+1] != '\'' && s[i+1] != '"') {
				return nil, fmt.Errorf("field path %q: expected a quoted key after [", s)
			}
			q := s[i+1]
			end := strings.IndexByte(s[i+2:], q)
			if end < 0 || i+2+end+1 >= len(s) || s[i+2+end+1] != ']' {
				return nil, fmt.Errorf("field path %q: unterminated [ ]", s)
			}
			key := s[i+2 : i+2+end]
			if key == "" {
				return nil, fmt.Errorf("field path %q: empty key", s)
			}
			segs = append(segs, key)
			i = i + 2 + end + 2
			if i < len(s) {
				switch s[i] {
				case '.':
					i++
					if i == len(s) {
						return nil, fmt.Errorf("field path %q ends with a dot", s)
					}
				case '[':
				default:
					return nil, fmt.Errorf("field path %q: expected . or [ after ]", s)
				}
			}
			continue
		}
		j := i
		for j < len(s) && s[j] != '.' && s[j] != '[' {
			if s[j] == ']' || s[j] == '\'' || s[j] == '"' {
				return nil, fmt.Errorf("field path %q: unexpected %q", s, s[j])
			}
			j++
		}
		if j == i {
			return nil, fmt.Errorf("field path %q has an empty segment", s)
		}
		segs = append(segs, s[i:j])
		i = j
		if i < len(s) && s[i] == '.' {
			i++
			if i == len(s) {
				return nil, fmt.Errorf("field path %q ends with a dot", s)
			}
		}
	}
	return segs, nil
}

// get returns the value at p.
func (p Path) get(rec map[string]any) (any, bool) {
	cur := rec
	for i, seg := range p.segs {
		v, ok := cur[seg]
		if !ok {
			return nil, false
		}
		if i == len(p.segs)-1 {
			return v, true
		}
		m, ok := v.(map[string]any)
		if !ok {
			return nil, false
		}
		cur = m
	}
	return nil, false
}

// set writes v at p, creating objects on the way. A value written into an
// OpenTelemetry map column is stringified.
func (p Path) set(rec map[string]any, v any) error {
	if p.mapCol {
		switch {
		case len(p.segs) == 2:
			v = Stringify(v)
		case len(p.segs) == 1:
			// A whole map column: every value becomes a string.
			if m, ok := v.(map[string]any); ok {
				for k, e := range m {
					m[k] = Stringify(e)
				}
			}
		}
	}
	cur := rec
	for i, seg := range p.segs[:len(p.segs)-1] {
		next, ok := cur[seg]
		if !ok || next == nil {
			m := map[string]any{}
			cur[seg] = m
			cur = m
			continue
		}
		m, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("%s is not an object", strings.Join(p.segs[:i+1], "."))
		}
		cur = m
	}
	cur[p.segs[len(p.segs)-1]] = v
	return nil
}

// del removes the value at p.
func (p Path) del(rec map[string]any) {
	cur := rec
	for _, seg := range p.segs[:len(p.segs)-1] {
		m, ok := cur[seg].(map[string]any)
		if !ok {
			return
		}
		cur = m
	}
	delete(cur, p.segs[len(p.segs)-1])
}

// container returns the object at p, creating it when absent. A nil path is
// the record root.
func (p *Path) container(rec map[string]any) (map[string]any, error) {
	if p == nil {
		return rec, nil
	}
	if v, ok := p.get(rec); ok && v != nil {
		m, ok := v.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s is not an object", p.raw)
		}
		return m, nil
	}
	m := map[string]any{}
	cur := rec
	for i, seg := range p.segs {
		if i == len(p.segs)-1 {
			cur[seg] = m
			break
		}
		next, ok := cur[seg].(map[string]any)
		if !ok {
			if v, exists := cur[seg]; exists && v != nil {
				return nil, fmt.Errorf("%s is not an object", strings.Join(p.segs[:i+1], "."))
			}
			next = map[string]any{}
			cur[seg] = next
		}
		cur = next
	}
	return m, nil
}

// Stringify formats a value the way a Map(String, String) column stores it:
// strings as they are, numbers in their shortest form, bools as true/false,
// null as "", objects and arrays as JSON.
func Stringify(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		b, err := json.Marshal(x)
		if err != nil {
			return fmt.Sprint(x)
		}
		return string(b)
	}
}

// toFloat reports v as a number when it is one or a string that parses as
// one.
func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case float32:
		return float64(x), true
	case int:
		return float64(x), true
	case int64:
		return float64(x), true
	case int32:
		return float64(x), true
	case uint64:
		return float64(x), true
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	}
	return 0, false
}

// deepCopy copies the maps and slices of a decoded JSON value.
func deepCopy(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = deepCopy(e)
		}
		return m
	case []any:
		s := make([]any, len(x))
		for i, e := range x {
			s[i] = deepCopy(e)
		}
		return s
	default:
		return v
	}
}

// CopyRecord returns a deep copy of a record.
func CopyRecord(rec map[string]any) map[string]any {
	return deepCopy(rec).(map[string]any)
}
