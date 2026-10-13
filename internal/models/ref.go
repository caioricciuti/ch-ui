package models

import (
	"fmt"
	"regexp"
	"strings"
)

// sqlSegmentKind classifies a run of SQL text.
type sqlSegmentKind int

const (
	segCode sqlSegmentKind = iota
	segComment
	segQuoted // string literal or quoted identifier
)

type sqlSegment struct {
	kind sqlSegmentKind
	text string
}

// splitSQL cuts SQL into code, comment and quoted runs. It understands
// single quotes, double quotes and backticks (each with doubled-quote and
// backslash escapes), line comments (--) and block comments (/* */). An
// unterminated quote or block comment runs to the end of the input.
func splitSQL(sql string) []sqlSegment {
	var segs []sqlSegment
	start := 0
	flush := func(end int) {
		if end > start {
			segs = append(segs, sqlSegment{kind: segCode, text: sql[start:end]})
		}
	}
	i := 0
	for i < len(sql) {
		c := sql[i]
		switch {
		case c == '-' && i+1 < len(sql) && sql[i+1] == '-':
			flush(i)
			end := strings.IndexByte(sql[i:], '\n')
			if end < 0 {
				end = len(sql)
			} else {
				end += i
			}
			segs = append(segs, sqlSegment{kind: segComment, text: sql[i:end]})
			i, start = end, end
		case c == '/' && i+1 < len(sql) && sql[i+1] == '*':
			flush(i)
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				end = len(sql)
			} else {
				end += i + 4
			}
			segs = append(segs, sqlSegment{kind: segComment, text: sql[i:end]})
			i, start = end, end
		case c == '\'' || c == '"' || c == '`':
			flush(i)
			end := len(sql)
			for j := i + 1; j < len(sql); j++ {
				if sql[j] == '\\' {
					j++
					continue
				}
				if sql[j] == c {
					if j+1 < len(sql) && sql[j+1] == c {
						j++
						continue
					}
					end = j + 1
					break
				}
			}
			segs = append(segs, sqlSegment{kind: segQuoted, text: sql[i:end]})
			i, start = end, end
		default:
			i++
		}
	}
	flush(len(sql))
	return segs
}

// stripSQLComments removes line (--) and block (/* */) comments outside
// string literals and quoted identifiers. A block comment becomes one space so
// the tokens around it stay apart.
func stripSQLComments(sql string) string {
	var b strings.Builder
	for _, s := range splitSQL(sql) {
		switch {
		case s.kind != segComment:
			b.WriteString(s.text)
		case strings.HasPrefix(s.text, "/*"):
			b.WriteByte(' ')
		}
	}
	return b.String()
}

// refPattern matches $ref(model_name) in SQL.
// Model names follow ClickHouse identifier rules: [a-zA-Z_][a-zA-Z0-9_]*
var refPattern = regexp.MustCompile(`\$ref\(\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*\)`)

// modelNamePattern validates model names as valid ClickHouse identifiers.
var modelNamePattern = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// ValidateModelName checks if a name is a valid ClickHouse identifier.
func ValidateModelName(name string) error {
	if name == "" {
		return fmt.Errorf("model name cannot be empty")
	}
	if !modelNamePattern.MatchString(name) {
		return fmt.Errorf("model name %q must be a valid identifier (letters, digits, underscores, starting with letter or underscore)", name)
	}
	return nil
}

// ExtractRefs returns all model names referenced via $ref() in the SQL body.
// $ref() inside comments, string literals and quoted identifiers is ignored.
func ExtractRefs(sqlBody string) []string {
	seen := make(map[string]bool)
	var refs []string
	for _, s := range splitSQL(sqlBody) {
		if s.kind != segCode {
			continue
		}
		for _, m := range refPattern.FindAllStringSubmatch(s.text, -1) {
			name := strings.TrimSpace(m[1])
			if !seen[name] {
				seen[name] = true
				refs = append(refs, name)
			}
		}
	}
	return refs
}

// ResolveRefs replaces $ref(model_name) with `target_database`.`model_name`.
// modelTargets maps model_name -> target_database. Only code is rewritten:
// comments, string literals and quoted identifiers are kept byte for byte, so
// the SQL that runs is the SQL the user wrote.
func ResolveRefs(sqlBody string, modelTargets map[string]string) (string, error) {
	var resolveErr error
	var b strings.Builder
	for _, s := range splitSQL(sqlBody) {
		if s.kind != segCode {
			b.WriteString(s.text)
			continue
		}
		b.WriteString(refPattern.ReplaceAllStringFunc(s.text, func(match string) string {
			sub := refPattern.FindStringSubmatch(match)
			name := strings.TrimSpace(sub[1])
			db, ok := modelTargets[name]
			if !ok {
				if resolveErr == nil {
					resolveErr = fmt.Errorf("unresolved reference: $ref(%s)", name)
				}
				return match
			}
			return quoteIdent(db) + "." + quoteIdent(name)
		}))
	}
	return b.String(), resolveErr
}

// thisPattern matches $this, the model's own target table.
var thisPattern = regexp.MustCompile(`\$this\b`)

// UsesThis reports whether the SQL reads $this outside comments and quotes.
func UsesThis(sqlBody string) bool {
	for _, s := range splitSQL(sqlBody) {
		if s.kind == segCode && thisPattern.MatchString(s.text) {
			return true
		}
	}
	return false
}

// ResolveModelSQL resolves $ref(name) like ResolveRefs and $this to the
// model's own target, `target_database`.`name` (for a materialized view, its
// target table). Comments, string literals and quoted identifiers are kept as
// written.
func ResolveModelSQL(sqlBody string, modelTargets map[string]string, targetDB, name string) (string, error) {
	resolved, err := ResolveRefs(sqlBody, modelTargets)
	if err != nil {
		return resolved, err
	}
	this := quoteIdent(targetDB) + "." + quoteIdent(name)
	var b strings.Builder
	for _, s := range splitSQL(resolved) {
		if s.kind != segCode {
			b.WriteString(s.text)
			continue
		}
		b.WriteString(thisPattern.ReplaceAllLiteralString(s.text, this))
	}
	return b.String(), nil
}
