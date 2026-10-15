package dashboards

import (
	"fmt"
	"sort"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/queryproc"
)

// OptionsRunner returns the options of a query variable, given the values
// and types of the variables defined before it. all is a SELECT of every
// value of the variable (see AllSubquery), set when the options stopped at
// MaxOptions so a list would leave values out; empty otherwise.
type OptionsRunner func(v Variable, parents map[string][]string, parentTypes map[string]string) (opts []Option, all string, err error)

// Options returns the options of v: the static list, or the result of run
// for a query variable.
func Options(v Variable, parents map[string][]string, parentTypes map[string]string, run OptionsRunner) ([]Option, error) {
	opts, _, err := options(v, parents, parentTypes, run)
	return opts, err
}

func options(v Variable, parents map[string][]string, parentTypes map[string]string, run OptionsRunner) ([]Option, string, error) {
	if v.Type == TypeQuery {
		if run == nil {
			return nil, "", fmt.Errorf("variable %s: options are not available", v.Name)
		}
		return run(v, parents, parentTypes)
	}
	return v.StaticOptions(), "", nil
}

// AllSubquery is the SELECT that "All" of a query variable expands to when
// its options query returns MaxOptions rows or more: the value column of
// the options query, already expanded (macros and parent variables). A
// trailing semicolon is dropped and the query sits on its own lines, so a
// trailing comment cannot swallow the closing parenthesis.
func AllSubquery(expandedQuery, valueColumn string) string {
	q := strings.TrimRight(strings.TrimSpace(expandedQuery), "; \t\r\n")
	return "SELECT " + quoteIdent(valueColumn) + " FROM (\n" + q + "\n)"
}

// quoteIdent returns name as a ClickHouse backquoted identifier.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(strings.ReplaceAll(name, `\`, `\\`), "`", "\\`") + "`"
}

// Resolve works out the value of every variable, in definition order, so a
// query variable sees the values of the variables before it.
//
// requested holds what the client asked for. A variable that is missing
// takes its default, else its first option. AllValue selects every option of
// a variable with include_all. A constant is always its default.
//
// "All" of a query variable whose options stop at MaxOptions does not become
// a list, which would silently filter to the first MaxOptions values: its
// value is the SELECT the runner returned, typed queryproc.VarTypeSubquery,
// and it substitutes only inside IN (...).
//
// strict is for share links: a value must be one of the variable's options
// (re-run here, with the sharer's credentials), and a text variable can only
// be its default. Otherwise any value is accepted; it is quoted when it is
// substituted anyway. In both modes an ISO instant that is the Instant of an
// option becomes that option's value.
//
// It returns the values by name and the type of every interval variable and
// every "All" subquery.
func Resolve(vars []Variable, requested map[string][]string, strict bool, run OptionsRunner) (map[string][]string, map[string]string, error) {
	values := make(map[string][]string, len(vars))
	types := map[string]string{}
	for _, v := range vars {
		if v.Type == TypeInterval {
			types[v.Name] = TypeInterval
		}
		req, asked := requested[v.Name]
		req = cleanValues(req)
		switch v.Type {
		case TypeConstant:
			values[v.Name] = copyValues(v.Default)
			continue
		case TypeText:
			if strict || !asked {
				values[v.Name] = firstOnly(copyValues(v.Default))
			} else {
				values[v.Name] = firstOnly(req)
			}
			continue
		}

		var opts []Option
		var allSQL string
		haveOpts := false
		loadOpts := func() ([]Option, error) {
			if haveOpts {
				return opts, nil
			}
			o, all, err := options(v, values, types, run)
			if err != nil {
				return nil, err
			}
			opts, allSQL, haveOpts = o, all, true
			return opts, nil
		}

		chosen := req
		if len(chosen) == 0 {
			chosen = copyValues(v.Default)
		}
		if len(chosen) == 0 {
			o, err := loadOpts()
			if err != nil {
				return nil, nil, err
			}
			if v.IncludeAll && v.Multi {
				chosen = []string{AllValue}
			} else if len(o) > 0 {
				chosen = []string{o[0].Value}
			}
		}
		if contains(chosen, AllValue) {
			if !v.IncludeAll {
				return nil, nil, fmt.Errorf("variable %s does not allow All", v.Name)
			}
			o, err := loadOpts()
			if err != nil {
				return nil, nil, err
			}
			if allSQL != "" {
				values[v.Name] = []string{allSQL}
				types[v.Name] = queryproc.VarTypeSubquery
				continue
			}
			chosen = make([]string, 0, len(o))
			for _, x := range o {
				chosen = append(chosen, x.Value)
			}
			values[v.Name] = chosen
			continue
		}
		if !v.Multi {
			chosen = firstOnly(chosen)
		}
		// A date and time clicked in a panel arrives as an ISO instant, which
		// is not a valid literal for a DateTime column: it becomes the option
		// with that instant. Without its options, a non-strict value is used
		// as given.
		if v.Type == TypeQuery && anyInstant(chosen) {
			o, err := loadOpts()
			if err != nil && strict {
				return nil, nil, err
			}
			if err == nil {
				chosen = canonicalValues(chosen, o)
			}
		}
		if strict {
			o, err := loadOpts()
			if err != nil {
				return nil, nil, err
			}
			allowed := make(map[string]bool, len(o))
			for _, x := range o {
				allowed[x.Value] = true
			}
			for _, c := range chosen {
				if !allowed[c] {
					return nil, nil, fmt.Errorf("value %q is not an option of variable %s", c, v.Name)
				}
			}
		}
		values[v.Name] = chosen
	}
	return values, types, nil
}

// CanonicalKey is a stable text form of resolved values, for cache keys.
func CanonicalKey(values map[string][]string) string {
	names := make([]string, 0, len(values))
	for n := range values {
		names = append(names, n)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n)
		b.WriteByte('=')
		for i, v := range values[n] {
			if i > 0 {
				b.WriteByte(0x1f)
			}
			b.WriteString(v)
		}
		b.WriteByte(0x1e)
	}
	return b.String()
}

// Needed returns, in definition order, the variables a query depends on:
// the ones it references and, for query variables, the ones their options
// queries reference in turn. Only these need resolving for that query.
func Needed(vars []Variable, query string) []Variable {
	names := make([]string, len(vars))
	for i, v := range vars {
		names[i] = v.Name
	}
	need := map[string]bool{}
	for _, n := range queryproc.ReferencedVars(query, names) {
		need[n] = true
	}
	// Parents come before children, so one pass from the end is enough.
	for i := len(vars) - 1; i >= 0; i-- {
		v := vars[i]
		if need[v.Name] && v.Type == TypeQuery {
			for _, n := range queryproc.ReferencedVars(v.Query, names[:i]) {
				need[n] = true
			}
		}
	}
	out := make([]Variable, 0, len(need))
	for _, v := range vars {
		if need[v.Name] {
			out = append(out, v)
		}
	}
	return out
}

// Before returns the variables defined before name (all of them when name is
// not defined).
func Before(vars []Variable, name string) []Variable {
	for i, v := range vars {
		if v.Name == name {
			return vars[:i]
		}
	}
	return vars
}

// Find returns the variable called name.
func Find(vars []Variable, name string) (Variable, bool) {
	for _, v := range vars {
		if v.Name == name {
			return v, true
		}
	}
	return Variable{}, false
}

func cleanValues(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]bool{}
	for _, v := range in {
		if seen[v] || len(out) >= MaxOptions {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func copyValues(in []string) []string { return append([]string{}, in...) }

func firstOnly(in []string) []string {
	if len(in) > 1 {
		return in[:1]
	}
	return in
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}
