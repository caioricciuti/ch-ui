package queryproc

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Dashboard variables are referenced as $name or ${name}. Every value is
// quoted here, on the server, as a ClickHouse string literal; a multi-value
// variable becomes a comma list of literals for use inside IN (...).
// ${name:number} emits bare numbers and fails unless every value parses as a
// number. An interval variable is emitted as a whole number of seconds.
// ${name:raw} is deliberately not supported: no value is ever pasted into SQL
// unquoted.

// VarTypeInterval marks a variable whose values are durations ("5m", "1h")
// and that substitutes as seconds.
const VarTypeInterval = "interval"

// varNamePattern is a valid variable name. Names starting with "__" are
// reserved for the built-in macros ($__timeFilter, $__interval, ...).
var varNamePattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

// ValidVarName reports whether name can be used as a dashboard variable.
func ValidVarName(name string) bool {
	return len(name) <= 64 && varNamePattern.MatchString(name) && !strings.HasPrefix(name, "__")
}

// QuoteString returns s as a ClickHouse string literal. Backslashes are
// escaped first, then single quotes are doubled.
func QuoteString(s string) string {
	return "'" + strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), "'", "''") + "'"
}

var (
	patNumber   = regexp.MustCompile(`^-?(\d+(\.\d+)?|\.\d+)([eE][-+]?\d+)?$`)
	patDuration = regexp.MustCompile(`^(\d+)\s*(s|m|h|d|w)?$`)
)

// IntervalSeconds parses an interval variable value: "30s", "5m", "1h", "1d",
// "1w" or a bare number of seconds.
func IntervalSeconds(v string) (int64, bool) {
	m := patDuration.FindStringSubmatch(strings.TrimSpace(v))
	if m == nil {
		return 0, false
	}
	n, err := strconv.ParseInt(m[1], 10, 64)
	if err != nil || n <= 0 {
		return 0, false
	}
	mult := map[string]int64{"": 1, "s": 1, "m": 60, "h": 3600, "d": 86400, "w": 604800}[m[2]]
	if n > (1<<62)/mult {
		return 0, false
	}
	return n * mult, true
}

// formatVar renders the values of one variable as SQL.
func formatVar(name, format, varType string, values []string) (string, error) {
	switch format {
	case "", "number":
	case "raw":
		return "", fmt.Errorf("${%s:raw} is not supported: variable values are always quoted", name)
	default:
		return "", fmt.Errorf("${%s:%s}: unknown format (use ${%s} or ${%s:number})", name, format, name, name)
	}
	if len(values) == 0 {
		return "NULL", nil
	}
	parts := make([]string, 0, len(values))
	for _, v := range values {
		switch {
		case varType == VarTypeInterval:
			secs, ok := IntervalSeconds(v)
			if !ok {
				return "", fmt.Errorf("variable %s: %q is not an interval (use 30s, 5m, 1h, 1d)", name, v)
			}
			parts = append(parts, strconv.FormatInt(secs, 10))
		case format == "number":
			t := strings.TrimSpace(v)
			if !patNumber.MatchString(t) {
				return "", fmt.Errorf("variable %s: %q is not a number", name, v)
			}
			parts = append(parts, t)
		default:
			parts = append(parts, QuoteString(v))
		}
	}
	return strings.Join(parts, ", "), nil
}

// SubstituteVars replaces $name and ${name} references to the given
// variables. References inside string literals, quoted identifiers and
// comments are not touched; a reference inside a string literal is reported
// as an error, because the value is quoted already and the surrounding quotes
// would turn it into SQL. Names not in vars are left as they are.
func SubstituteVars(query string, vars map[string][]string, types map[string]string) (string, error) {
	if len(vars) == 0 || !strings.Contains(query, "$") {
		return query, nil
	}
	var b strings.Builder
	n := len(query)
	for i := 0; i < n; {
		c := query[i]
		switch {
		case c == '\'' || c == '"' || c == '`':
			end := skipQuoted(query, i)
			if c == '\'' {
				if name := firstVarRef(query[i:end], vars); name != "" {
					return "", fmt.Errorf("variable $%s is inside quotes: remove the quotes, values are quoted for you", name)
				}
			}
			b.WriteString(query[i:end])
			i = end
		case c == '-' && i+1 < n && query[i+1] == '-', c == '#':
			end := strings.IndexByte(query[i:], '\n')
			if end < 0 {
				end = n
			} else {
				end += i
			}
			b.WriteString(query[i:end])
			i = end
		case c == '/' && i+1 < n && query[i+1] == '*':
			end := strings.Index(query[i+2:], "*/")
			if end < 0 {
				end = n
			} else {
				end += i + 4
			}
			b.WriteString(query[i:end])
			i = end
		case c == '$' && heredocEnd(query, i) > 0:
			end := heredocEnd(query, i)
			b.WriteString(query[i:end])
			i = end
		case c == '$':
			name, format, end, ok := parseVarRef(query, i)
			values, known := vars[name]
			if !ok || !known {
				b.WriteByte(c)
				i++
				continue
			}
			out, err := formatVar(name, format, types[name], values)
			if err != nil {
				return "", err
			}
			b.WriteString(out)
			i = end
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String(), nil
}

// skipQuoted returns the index just past the quoted token starting at i. A
// backslash escapes the next character and a doubled quote is an escaped
// quote. An unterminated token runs to the end of the query.
func skipQuoted(q string, i int) int {
	quote := q[i]
	for j := i + 1; j < len(q); j++ {
		switch q[j] {
		case '\\':
			j++
		case quote:
			if j+1 < len(q) && q[j+1] == quote {
				j++
				continue
			}
			return j + 1
		}
	}
	return len(q)
}

var patHeredocOpen = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

// heredocEnd returns the index just past a ClickHouse heredoc string
// ($$...$$ or $tag$...$tag$) starting at i, or 0 when there is none.
func heredocEnd(q string, i int) int {
	m := patHeredocOpen.FindString(q[i:])
	if m == "" {
		return 0
	}
	close := strings.Index(q[i+len(m):], m)
	if close < 0 {
		return 0
	}
	return i + len(m) + close + len(m)
}

// parseVarRef reads a $name or ${name} or ${name:format} reference at i.
func parseVarRef(q string, i int) (name, format string, end int, ok bool) {
	j := i + 1
	if j < len(q) && q[j] == '{' {
		close := strings.IndexByte(q[j:], '}')
		if close < 0 {
			return "", "", 0, false
		}
		inner := q[j+1 : j+close]
		name, format, _ = strings.Cut(inner, ":")
		if !varNamePattern.MatchString(name) {
			return "", "", 0, false
		}
		return name, strings.ToLower(strings.TrimSpace(format)), j + close + 1, true
	}
	k := j
	for k < len(q) && (q[k] == '_' || isAlnum(q[k])) {
		k++
	}
	name = q[j:k]
	if !varNamePattern.MatchString(name) {
		return "", "", 0, false
	}
	return name, "", k, true
}

func isAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// firstVarRef returns the first known variable referenced in s, or "".
func firstVarRef(s string, vars map[string][]string) string {
	for i := 0; i < len(s); i++ {
		if s[i] != '$' {
			continue
		}
		if name, _, _, ok := parseVarRef(s, i); ok {
			if _, known := vars[name]; known {
				return name
			}
		}
	}
	return ""
}

// ReferencedVars returns the names in names that query references.
func ReferencedVars(query string, names []string) []string {
	known := make(map[string][]string, len(names))
	for _, n := range names {
		known[n] = nil
	}
	seen := map[string]bool{}
	var out []string
	for i := 0; i < len(query); i++ {
		if query[i] != '$' {
			continue
		}
		if name, _, _, ok := parseVarRef(query, i); ok {
			if _, k := known[name]; k && !seen[name] {
				seen[name] = true
				out = append(out, name)
			}
		}
	}
	return out
}
