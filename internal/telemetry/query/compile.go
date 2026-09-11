// Package query compiles the HyperDX-style search language used by the
// Telemetry pages into a ClickHouse WHERE clause.
//
// Grammar (whitespace separates terms, adjacent terms are ANDed):
//
//	expr    := or
//	or      := and ( "OR" and )*
//	and     := not ( ["AND"] not )*
//	not     := ( "NOT" | "-" ) not | primary
//	primary := "(" expr ")" | field ":" value | value
//	value   := '"' phrase '"' | [ cmp ] word | "*"
//	cmp     := ">=" | "<=" | ">" | "<" | "!="
//
// Free text matches whole words in the implicit text column, case
// insensitive. `field:value` matches a role (service, level, trace_id, …)
// when the schema defines one, else an attribute map key. `field:*`
// tests for existence. `*` inside a value is a wildcard. Comparisons work
// on numeric roles (duration in milliseconds) and on attribute values
// (parsed as Float64).
//
// Every identifier in the output comes from the Schema, which the server
// builds from a verified source mapping. Literals are escaped. The query
// text itself never contributes an identifier.
package query

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// NumericField is a role that compares as a number. Scale converts the
// user's unit into the column's unit (duration: ms -> ns is 1e6).
type NumericField struct {
	Expr  string
	Scale float64
}

// Schema tells the compiler how the search language maps onto one table.
type Schema struct {
	// TextExpr is the implicit full-text column for bare terms (Body).
	TextExpr string
	// Roles maps a searchable field name to a SQL expression. Values
	// compare as strings. Field names are matched case-insensitively.
	Roles map[string]string
	// CaseInsensitive lists roles compared with lower() on both sides
	// (level, status, kind, span_name).
	CaseInsensitive map[string]bool
	// Numeric maps a field name to a numeric expression and unit scale.
	Numeric map[string]NumericField
	// Aliases maps alternative field names onto Roles / Numeric keys
	// (severity -> level, service_name -> service, trace -> trace_id).
	Aliases map[string]string
	// AttributeMaps lists Map(String, String) columns searched, in order,
	// for any field that is not a role: LogAttributes, ResourceAttributes…
	AttributeMaps []string
	// AttributeJSON marks entries of AttributeMaps (same quoted spelling)
	// stored as a JSON column instead of a Map: the exporter's `json: true`
	// mode. Access is by path (col.`a`.`b`), existence via JSONAllPaths.
	AttributeJSON map[string]bool
	// Columns is the verified column list (name -> type). A field that
	// names a real column is used directly, so power users can write
	// ScopeName:foo. Optional.
	Columns map[string]string
}

// Compile turns a search string into a WHERE fragment (without the
// keyword). An empty query compiles to "1". Errors are user facing.
func Compile(q string, s Schema) (string, error) {
	toks, err := tokenize(q)
	if err != nil {
		return "", err
	}
	if len(toks) == 0 {
		return "1", nil
	}
	p := &parser{toks: toks, schema: s}
	sql, err := p.parseOr()
	if err != nil {
		return "", err
	}
	if p.pos < len(p.toks) {
		return "", fmt.Errorf("unexpected %q", p.toks[p.pos].text)
	}
	return sql, nil
}

// ── Tokenizer ────────────────────────────────────────────────────────

type tokKind int

const (
	tWord   tokKind = iota // bare word, may include field:value
	tQuoted                // "phrase" (text is the unquoted content)
	tLParen
	tRParen
	tAnd
	tOr
	tNot
	tField // word followed by ':' — text is the field name
)

type token struct {
	kind tokKind
	text string
}

func tokenize(q string) ([]token, error) {
	var out []token
	rs := []rune(q)
	i := 0
	for i < len(rs) {
		r := rs[i]
		switch {
		case unicode.IsSpace(r):
			i++
		case r == '(':
			out = append(out, token{tLParen, "("})
			i++
		case r == ')':
			out = append(out, token{tRParen, ")"})
			i++
		case r == '"':
			j := i + 1
			var b strings.Builder
			closed := false
			for j < len(rs) {
				if rs[j] == '\\' && j+1 < len(rs) {
					b.WriteRune(rs[j+1])
					j += 2
					continue
				}
				if rs[j] == '"' {
					closed = true
					break
				}
				b.WriteRune(rs[j])
				j++
			}
			if !closed {
				return nil, fmt.Errorf("unterminated quote")
			}
			out = append(out, token{tQuoted, b.String()})
			i = j + 1
		case r == '-' && i+1 < len(rs) && !unicode.IsSpace(rs[i+1]) && rs[i+1] != ')':
			// A dash that starts a token negates it. Dashes inside a word
			// (api-gateway) never reach here: the word loop consumes them.
			out = append(out, token{tNot, "-"})
			i++
		default:
			j := i
			for j < len(rs) && !unicode.IsSpace(rs[j]) && rs[j] != '(' && rs[j] != ')' && rs[j] != '"' {
				if rs[j] == ':' {
					break
				}
				j++
			}
			word := string(rs[i:j])
			if j < len(rs) && rs[j] == ':' {
				if word == "" {
					return nil, fmt.Errorf("missing field name before ':'")
				}
				out = append(out, token{tField, word})
				i = j + 1
				continue
			}
			switch strings.ToUpper(word) {
			case "AND":
				out = append(out, token{tAnd, word})
			case "OR":
				out = append(out, token{tOr, word})
			case "NOT":
				out = append(out, token{tNot, word})
			default:
				out = append(out, token{tWord, word})
			}
			i = j
		}
	}
	return out, nil
}

// ── Parser ───────────────────────────────────────────────────────────

type parser struct {
	toks   []token
	pos    int
	schema Schema
}

func (p *parser) peek() *token {
	if p.pos < len(p.toks) {
		return &p.toks[p.pos]
	}
	return nil
}

func (p *parser) parseOr() (string, error) {
	left, err := p.parseAnd()
	if err != nil {
		return "", err
	}
	parts := []string{left}
	for t := p.peek(); t != nil && t.kind == tOr; t = p.peek() {
		p.pos++
		right, err := p.parseAnd()
		if err != nil {
			return "", err
		}
		parts = append(parts, right)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

func (p *parser) parseAnd() (string, error) {
	left, err := p.parseNot()
	if err != nil {
		return "", err
	}
	parts := []string{left}
	for {
		t := p.peek()
		if t == nil || t.kind == tOr || t.kind == tRParen {
			break
		}
		if t.kind == tAnd {
			p.pos++
			if p.peek() == nil {
				return "", fmt.Errorf("dangling AND")
			}
		}
		right, err := p.parseNot()
		if err != nil {
			return "", err
		}
		parts = append(parts, right)
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return "(" + strings.Join(parts, " AND ") + ")", nil
}

func (p *parser) parseNot() (string, error) {
	t := p.peek()
	if t != nil && t.kind == tNot {
		p.pos++
		if p.peek() == nil {
			return "", fmt.Errorf("dangling NOT")
		}
		inner, err := p.parseNot()
		if err != nil {
			return "", err
		}
		return "NOT (" + inner + ")", nil
	}
	return p.parsePrimary()
}

func (p *parser) parsePrimary() (string, error) {
	t := p.peek()
	if t == nil {
		return "", fmt.Errorf("unexpected end of query")
	}
	switch t.kind {
	case tLParen:
		p.pos++
		inner, err := p.parseOr()
		if err != nil {
			return "", err
		}
		if c := p.peek(); c == nil || c.kind != tRParen {
			return "", fmt.Errorf("missing ')'")
		}
		p.pos++
		// Compound expressions already carry their own parentheses.
		return inner, nil
	case tRParen:
		return "", fmt.Errorf("unexpected ')'")
	case tAnd, tOr:
		return "", fmt.Errorf("unexpected %s", strings.ToUpper(t.text))
	case tField:
		p.pos++
		v := p.peek()
		if v == nil || (v.kind != tWord && v.kind != tQuoted) {
			return "", fmt.Errorf("field %q needs a value", t.text)
		}
		p.pos++
		return p.fieldTerm(t.text, v.text, v.kind == tQuoted)
	case tQuoted:
		p.pos++
		return p.phraseTerm(t.text), nil
	default:
		p.pos++
		return p.textTerm(t.text), nil
	}
}

// ── Term compilers ───────────────────────────────────────────────────

func (p *parser) textTerm(word string) string {
	col := p.schema.TextExpr
	if col == "" {
		col = "Body"
	}
	if strings.Contains(word, "*") {
		return fmt.Sprintf("%s ILIKE '%s'", col, likePattern(word, true))
	}
	if isToken(word) {
		return fmt.Sprintf("hasTokenCaseInsensitive(%s, '%s')", col, escape(word))
	}
	return fmt.Sprintf("positionCaseInsensitive(%s, '%s') > 0", col, escape(word))
}

func (p *parser) phraseTerm(phrase string) string {
	col := p.schema.TextExpr
	if col == "" {
		col = "Body"
	}
	if strings.TrimSpace(phrase) == "" {
		return "1"
	}
	return fmt.Sprintf("positionCaseInsensitive(%s, '%s') > 0", col, escape(phrase))
}

func (p *parser) fieldTerm(field, raw string, quoted bool) (string, error) {
	name := strings.ToLower(field)
	if a, ok := p.schema.Aliases[name]; ok {
		name = a
	}

	// Comparison prefix on the value.
	op := "="
	val := raw
	if !quoted {
		for _, c := range []string{">=", "<=", "!=", ">", "<"} {
			if strings.HasPrefix(val, c) {
				op = c
				val = val[len(c):]
				break
			}
		}
	}
	if val == "" {
		return "", fmt.Errorf("field %q needs a value", field)
	}

	// Numeric role.
	if nf, ok := p.schema.Numeric[name]; ok {
		if val == "*" {
			return fmt.Sprintf("%s IS NOT NULL", nf.Expr), nil
		}
		n, err := strconv.ParseFloat(val, 64)
		if err != nil {
			return "", fmt.Errorf("%s expects a number, got %q", field, val)
		}
		scale := nf.Scale
		if scale == 0 {
			scale = 1
		}
		return fmt.Sprintf("%s %s %s", nf.Expr, op, formatNumber(n*scale)), nil
	}

	// String role.
	if expr, ok := p.schema.Roles[name]; ok {
		return p.stringCompare(expr, op, val, p.schema.CaseInsensitive[name]), nil
	}

	// A verified real column, written exactly.
	if typ, ok := p.schema.Columns[field]; ok {
		if isNumericType(typ) {
			if val == "*" {
				return fmt.Sprintf("%s IS NOT NULL", quoteIdent(field)), nil
			}
			n, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return "", fmt.Errorf("%s expects a number, got %q", field, val)
			}
			return fmt.Sprintf("%s %s %s", quoteIdent(field), op, formatNumber(n)), nil
		}
		return p.stringCompare(quoteIdent(field), op, val, false), nil
	}

	// Attribute key in one of the map columns.
	if len(p.schema.AttributeMaps) == 0 {
		return "", fmt.Errorf("unknown field %q", field)
	}
	key := escape(field)
	parts := make([]string, 0, len(p.schema.AttributeMaps))
	for _, m := range p.schema.AttributeMaps {
		isJSON := p.schema.AttributeJSON[m]
		access := fmt.Sprintf("%s['%s']", m, key)
		if isJSON {
			access = JSONPathAccess(m, field)
		}
		switch {
		case val == "*" && isJSON:
			parts = append(parts, fmt.Sprintf("has(JSONAllPaths(%s), '%s')", m, key))
		case val == "*":
			parts = append(parts, fmt.Sprintf("mapContains(%s, '%s')", m, key))
		case op != "=" && op != "!=":
			n, err := strconv.ParseFloat(val, 64)
			if err != nil {
				return "", fmt.Errorf("%s expects a number for %s, got %q", field, op, val)
			}
			parts = append(parts, fmt.Sprintf("toFloat64OrNull(%s) %s %s", access, op, formatNumber(n)))
		default:
			parts = append(parts, p.stringCompare(access, op, val, false))
		}
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return "(" + strings.Join(parts, " OR ") + ")", nil
}

func (p *parser) stringCompare(expr, op, val string, ci bool) string {
	if val == "*" {
		return fmt.Sprintf("%s != ''", expr)
	}
	neg := op == "!="
	var cond string
	switch {
	case strings.Contains(val, "*"):
		cond = fmt.Sprintf("%s ILIKE '%s'", expr, likePattern(val, false))
	case ci:
		cond = fmt.Sprintf("lower(%s) = '%s'", expr, escape(strings.ToLower(val)))
	default:
		cond = fmt.Sprintf("%s = '%s'", expr, escape(val))
	}
	if neg {
		return "NOT " + cond
	}
	return cond
}

// ── Helpers ──────────────────────────────────────────────────────────

// escape makes a string safe inside a single-quoted ClickHouse literal.
func escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	s = strings.ReplaceAll(s, "\x00", "")
	return s
}

// likePattern turns a wildcard value into a LIKE pattern: * -> %, real
// % and _ escaped. contains wraps it in % on both sides.
func likePattern(s string, contains bool) string {
	var b strings.Builder
	for _, r := range escape(s) {
		switch r {
		case '*':
			b.WriteRune('%')
		case '%':
			b.WriteString(`\%`)
		case '_':
			b.WriteString(`\_`)
		default:
			b.WriteRune(r)
		}
	}
	out := b.String()
	if contains {
		out = "%" + out + "%"
	}
	return out
}

// isToken reports whether hasToken can match the word (letters, digits,
// underscore only; ClickHouse splits tokens on anything else).
func isToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !(unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_') {
			return false
		}
	}
	return true
}

func isNumericType(t string) bool {
	t = strings.TrimPrefix(t, "LowCardinality(")
	t = strings.TrimPrefix(t, "Nullable(")
	for _, prefix := range []string{"UInt", "Int", "Float", "Decimal"} {
		if strings.HasPrefix(t, prefix) {
			return true
		}
	}
	return false
}

func formatNumber(f float64) string {
	if f == float64(int64(f)) {
		return strconv.FormatInt(int64(f), 10)
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// quoteIdent backtick-quotes a verified column name.
func quoteIdent(name string) string {
	return "`" + strings.ReplaceAll(name, "`", "") + "`"
}

// JSONPathAccess renders a string read of a dotted key inside a JSON
// column: col.`a`.`b` wrapped in toString. Dots in OTel keys become
// nesting on insert, so the path mirrors the key. `col` is already quoted.
func JSONPathAccess(col, key string) string {
	segs := strings.Split(key, ".")
	parts := make([]string, 0, len(segs))
	for _, seg := range segs {
		if seg == "" {
			continue
		}
		parts = append(parts, quoteIdent(seg))
	}
	if len(parts) == 0 {
		return "toString(" + col + ")"
	}
	return "toString(" + col + "." + strings.Join(parts, ".") + ")"
}
