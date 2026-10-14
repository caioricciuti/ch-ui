// Package dashboards holds dashboard variable definitions and the built-in
// dashboard templates.
package dashboards

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/queryproc"
)

// Variable types.
const (
	TypeQuery    = "query"
	TypeCustom   = "custom"
	TypeText     = "text"
	TypeConstant = "constant"
	TypeInterval = "interval"
)

// AllValue is the value a client sends to select every option of a variable
// that has include_all set.
const AllValue = "$__all"

const (
	// MaxVariables bounds the variables on one dashboard.
	MaxVariables = 30
	// MaxOptions is the row cap of an options query and of a custom list.
	MaxOptions  = 1000
	maxQueryLen = 20000
)

// StringList decodes from a JSON string or an array of strings and always
// encodes as an array.
type StringList []string

// UnmarshalJSON accepts "a", ["a","b"] or null.
func (s *StringList) UnmarshalJSON(b []byte) error {
	raw := strings.TrimSpace(string(b))
	if raw == "null" || raw == "" {
		*s = nil
		return nil
	}
	if strings.HasPrefix(raw, `"`) {
		var one string
		if err := json.Unmarshal(b, &one); err != nil {
			return err
		}
		*s = StringList{one}
		return nil
	}
	var many []string
	if err := json.Unmarshal(b, &many); err != nil {
		return fmt.Errorf("default must be a string or an array of strings")
	}
	*s = many
	return nil
}

// Variable is one dashboard variable definition.
type Variable struct {
	Name  string `json:"name"`
	Label string `json:"label,omitempty"`
	Type  string `json:"type"`
	// Query (type query): the first column is the value, an optional second
	// column the label.
	Query string `json:"query,omitempty"`
	// Options (types custom and interval): a comma separated list.
	Options    string     `json:"options,omitempty"`
	Multi      bool       `json:"multi,omitempty"`
	IncludeAll bool       `json:"include_all,omitempty"`
	Default    StringList `json:"default,omitempty"`
}

// Option is one selectable value of a variable.
type Option struct {
	Value string `json:"value"`
	Label string `json:"label"`
}

// ParseVariables decodes and validates a JSON array of variables. Empty
// input means no variables.
func ParseVariables(raw []byte) ([]Variable, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return []Variable{}, nil
	}
	var vars []Variable
	if err := json.Unmarshal([]byte(trimmed), &vars); err != nil {
		return nil, fmt.Errorf("variables must be an array of variable objects: %v", err)
	}
	if err := Validate(vars); err != nil {
		return nil, err
	}
	if vars == nil {
		vars = []Variable{}
	}
	return vars, nil
}

// DecodeStored decodes variables saved earlier; bad data yields none.
func DecodeStored(raw string) []Variable {
	var vars []Variable
	if err := json.Unmarshal([]byte(raw), &vars); err != nil || vars == nil {
		return []Variable{}
	}
	return vars
}

// Encode returns the canonical JSON form of vars.
func Encode(vars []Variable) string {
	if vars == nil {
		vars = []Variable{}
	}
	b, _ := json.Marshal(vars)
	return string(b)
}

// Validate checks every definition and normalizes it in place.
func Validate(vars []Variable) error {
	if len(vars) > MaxVariables {
		return fmt.Errorf("at most %d variables per dashboard", MaxVariables)
	}
	seen := map[string]bool{}
	for i := range vars {
		v := &vars[i]
		v.Name = strings.TrimSpace(v.Name)
		v.Label = strings.TrimSpace(v.Label)
		v.Type = strings.ToLower(strings.TrimSpace(v.Type))
		v.Query = strings.TrimSpace(v.Query)
		v.Options = strings.TrimSpace(v.Options)
		where := fmt.Sprintf("variable %d", i+1)
		if v.Name != "" {
			where = fmt.Sprintf("variable %q", v.Name)
		}
		if !queryproc.ValidVarName(v.Name) {
			return fmt.Errorf("%s: name must be letters, digits and _, start with a letter or _, not start with __, at most 64 characters", where)
		}
		if seen[v.Name] {
			return fmt.Errorf("%s: name is used twice", where)
		}
		seen[v.Name] = true
		switch v.Type {
		case TypeQuery:
			if v.Query == "" {
				return fmt.Errorf("%s: query is required", where)
			}
			if len(v.Query) > maxQueryLen {
				return fmt.Errorf("%s: query is too long", where)
			}
			v.Options = ""
		case TypeCustom, TypeInterval:
			if len(v.StaticOptions()) == 0 {
				return fmt.Errorf("%s: options are required (a comma separated list)", where)
			}
			if countOptions(v.Options) > MaxOptions {
				return fmt.Errorf("%s: at most %d options (use a query variable for more)", where, MaxOptions)
			}
			v.Query = ""
			if v.Type == TypeInterval {
				for _, o := range v.StaticOptions() {
					if _, ok := queryproc.IntervalSeconds(o.Value); !ok {
						return fmt.Errorf("%s: %q is not an interval (use 30s, 5m, 1h, 1d)", where, o.Value)
					}
				}
				v.Multi, v.IncludeAll = false, false
			}
		case TypeText, TypeConstant:
			v.Query, v.Options, v.Multi, v.IncludeAll = "", "", false, false
			if v.Type == TypeConstant && len(v.Default) == 0 {
				return fmt.Errorf("%s: a constant needs a default value", where)
			}
		default:
			return fmt.Errorf("%s: type must be one of query, custom, text, constant, interval", where)
		}
		if !v.Multi && len(v.Default) > 1 {
			v.Default = v.Default[:1]
		}
	}
	return nil
}

// countOptions counts the distinct entries of a comma separated list, past
// the MaxOptions that StaticOptions keeps.
func countOptions(list string) int {
	seen := map[string]bool{}
	for _, part := range strings.Split(list, ",") {
		if p := strings.TrimSpace(part); p != "" {
			seen[p] = true
		}
	}
	return len(seen)
}

// StaticOptions returns the options of a custom or interval variable.
func (v Variable) StaticOptions() []Option {
	if v.Type != TypeCustom && v.Type != TypeInterval {
		return nil
	}
	var out []Option
	seen := map[string]bool{}
	for _, part := range strings.Split(v.Options, ",") {
		p := strings.TrimSpace(part)
		if p == "" || seen[p] || len(out) >= MaxOptions {
			continue
		}
		seen[p] = true
		out = append(out, Option{Value: p, Label: p})
	}
	return out
}

// Public returns the definition as a share link viewer may see it: without
// the options query.
func (v Variable) Public() Variable {
	v.Query = ""
	return v
}

// PublicList returns the variables a share link viewer may see. Constants
// are left out: their values stay on the server, which substitutes them when
// a public panel runs. Query variables lose their options query.
func PublicList(vars []Variable) []Variable {
	out := make([]Variable, 0, len(vars))
	for _, v := range vars {
		if v.Type == TypeConstant {
			continue
		}
		out = append(out, v.Public())
	}
	return out
}
