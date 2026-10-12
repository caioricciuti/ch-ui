package mcpserver

import (
	"fmt"
	"regexp"
	"strings"
)

// This file decides which databases a read-only statement touches, so a key
// with a database allow-list cannot read outside it through run_select,
// run_saved_query, explain_query or estimate_query.
//
// It is a conservative lexical analysis, not a SQL parser, and it fails
// closed: anything it cannot classify with confidence is rejected. What it
// does NOT see through is ClickHouse objects that read other databases on
// their own: a view, Distributed, Merge or Dictionary-backed table living in
// an allowed database. The ClickHouse user's grants remain the real boundary
// for those.

type tokKind int

const (
	tkWord   tokKind = iota // bare identifier or keyword
	tkIdent                 // `quoted` or "quoted" identifier
	tkString                // 'string literal'
	tkNumber
	tkPunct
)

type token struct {
	kind tokKind
	text string // identifier text unquoted; punctuation as is
}

func (t token) is(word string) bool {
	return t.kind == tkWord && strings.EqualFold(t.text, word)
}

func (t token) punct(p string) bool { return t.kind == tkPunct && t.text == p }

// name reports whether t can name something: a bare or quoted identifier.
func (t token) name() bool { return t.kind == tkWord || t.kind == tkIdent }

// errUnclear is returned for input the analysis refuses to guess about.
type errUnclear struct{ why string }

func (e errUnclear) Error() string { return e.why }

// tokenizeSQL splits sql into tokens, dropping whitespace and comments. In
// strict mode, constructs whose meaning ClickHouse's lexer and this one could
// disagree on (nested block comments, # comments, $ heredocs, non-ASCII
// outside literals) are an error instead of a guess.
func tokenizeSQL(sql string, strict bool) ([]token, error) {
	var out []token
	i, n := 0, len(sql)
	unclear := func(why string) ([]token, error) {
		return nil, errUnclear{"could not analyze the query safely: " + why}
	}
	for i < n {
		c := sql[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			i++
		case c == '-' && i+1 < n && sql[i+1] == '-':
			// Ending at \r too can only make more text count as SQL.
			for i < n && sql[i] != '\n' && sql[i] != '\r' {
				i++
			}
		case c == '/' && i+1 < n && sql[i+1] == '*':
			end := strings.Index(sql[i+2:], "*/")
			if end < 0 {
				if strict {
					return unclear("unterminated /* comment")
				}
				return out, nil
			}
			body := sql[i+2 : i+2+end]
			if strict && strings.Contains(body, "/*") {
				return unclear("nested /* comments")
			}
			i += 2 + end + 2
		case c == '\'' || c == '"' || c == '`':
			j := i + 1
			var sb strings.Builder
			closed := false
			for j < n {
				ch := sql[j]
				if ch == '\\' && j+1 < n {
					sb.WriteByte(sql[j+1])
					j += 2
					continue
				}
				if ch == c {
					if j+1 < n && sql[j+1] == c {
						sb.WriteByte(c)
						j += 2
						continue
					}
					closed = true
					j++
					break
				}
				sb.WriteByte(ch)
				j++
			}
			if !closed {
				if strict {
					return unclear("unterminated quote")
				}
				return out, nil
			}
			kind := tkIdent
			if c == '\'' {
				kind = tkString
			}
			out = append(out, token{kind: kind, text: sb.String()})
			i = j
		case isIdentStart(c):
			j := i + 1
			for j < n && isIdentPart(sql[j]) {
				j++
			}
			out = append(out, token{kind: tkWord, text: sql[i:j]})
			i = j
		case c >= '0' && c <= '9':
			j := i + 1
			for j < n && (isIdentPart(sql[j]) || sql[j] == '.') {
				j++
			}
			out = append(out, token{kind: tkNumber, text: sql[i:j]})
			i = j
		case strict && (c == '#' || c == '$'):
			return unclear(fmt.Sprintf("%q outside a string literal", c))
		case strict && c >= 0x80:
			return unclear("non-ASCII character outside a string literal")
		default:
			out = append(out, token{kind: tkPunct, text: string(c)})
			i++
		}
	}
	return out, nil
}

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

// settingsClause reports whether sql carries its own SETTINGS clause
// (SETTINGS name = ...). With readonly=1 ClickHouse refuses it anyway, but the
// gateway's read-only-profile retry would drop CH-UI's own caps and try
// again, so it is rejected before reaching ClickHouse.
func settingsClause(sql string) bool {
	toks, _ := tokenizeSQL(sql, false)
	for i := 0; i+2 < len(toks); i++ {
		if toks[i].is("SETTINGS") && toks[i+1].name() && toks[i+2].punct("=") {
			return true
		}
	}
	return false
}

// tableRef is one table the statement reads. DB is empty for an unqualified
// name, which ClickHouse resolves against the session's current database.
type tableRef struct {
	DB, Table string
}

// safeTableFuncs generate data without reading any database or external
// source. Every other table function is refused for allow-listed keys.
var safeTableFuncs = map[string]bool{
	"numbers": true, "numbers_mt": true, "zeros": true, "zeros_mt": true,
	"generate_series": true, "generateseries": true, "values": true,
	"generaterandom": true, "null": true,
}

// harmlessSystemTables can be read by any key: they hold no data.
var harmlessSystemTables = map[string]bool{
	"one": true, "numbers": true, "numbers_mt": true, "zeros": true, "zeros_mt": true,
}

// dataByNameFuncs read a table or dictionary named in a string argument,
// which the lexical analysis cannot resolve.
var dataByNameFuncRe = regexp.MustCompile(`(?i)^(dict\w*|joinget\w*|hascolumnintable|getsubcolumn)$`)

// noTableKeywordFuncs take FROM / IN / FOR as argument syntax, not as a
// table position: extract(YEAR FROM ts), position(x IN y), trim(BOTH ' ' FROM s).
var noTableKeywordFuncs = map[string]bool{
	"extract": true, "substring": true, "substr": true, "trim": true, "ltrim": true,
	"rtrim": true, "position": true, "locate": true, "overlay": true,
}

// clause keywords that end a FROM list at the same nesting level.
var clauseWords = map[string]bool{
	"WHERE": true, "PREWHERE": true, "GROUP": true, "HAVING": true, "ORDER": true,
	"LIMIT": true, "SETTINGS": true, "WINDOW": true, "QUALIFY": true, "FORMAT": true,
	"INTO": true, "SELECT": true, "INTERPOLATE": true, "FETCH": true,
}

type scope struct {
	parent  *scope
	clause  string          // last clause keyword seen at this level
	ctes    map[string]bool // WITH name AS (subquery) defined at this level
	special bool            // inside extract(...) and friends
}

func (s *scope) isCTE(name string) bool {
	for p := s; p != nil; p = p.parent {
		if p.ctes[name] {
			return true
		}
	}
	return false
}

func (s *scope) inFromList() bool {
	switch s.clause {
	case "FROM", "JOIN", "ON", "USING":
		return true
	}
	return false
}

// referencedTables lists the tables sql reads, failing closed. params are
// the query's bind values (param_<name>) so {t:Identifier} placeholders in
// table positions can be resolved. A nil error means every table position
// was understood; CTE references are left out because their bodies are
// scanned like any other subquery.
func referencedTables(sql string, params map[string]string) ([]tableRef, error) {
	toks, err := tokenizeSQL(sql, true)
	if err != nil {
		return nil, err
	}
	if len(toks) == 0 {
		return nil, errUnclear{"empty statement"}
	}
	if !(toks[0].is("SELECT") || toks[0].is("WITH") || toks[0].punct("(")) {
		return nil, errUnclear{"keys with a database allow-list can run SELECT / WITH statements only; use list_tables and describe_table to browse"}
	}

	var refs []tableRef
	cur := &scope{}
	at := func(i int) token {
		if i >= 0 && i < len(toks) {
			return toks[i]
		}
		return token{kind: tkPunct}
	}

	// partAt reads one name part at toks[i]: an identifier, or a
	// {name:Identifier} placeholder resolved from params. It returns the
	// text and how many tokens it spans (0 when toks[i] is neither).
	partAt := func(i int) (string, int, error) {
		t := at(i)
		if t.name() {
			return t.text, 1, nil
		}
		if !t.punct("{") {
			return "", 0, nil
		}
		if !(at(i+1).name() && at(i+2).punct(":") && at(i+3).is("Identifier") && at(i+4).punct("}")) {
			return "", 0, errUnclear{"a query parameter in a table position must be {name:Identifier}"}
		}
		v, ok := params["param_"+at(i+1).text]
		if !ok || !simpleIdentPathRe.MatchString(v) {
			return "", 0, errUnclear{fmt.Sprintf("parameter %q does not name a table as db.table or table", at(i+1).text)}
		}
		return v, 5, nil
	}

	// tableAt reads the table expression starting at toks[i] and returns the
	// index of its last token (i-1 when it consumed nothing).
	tableAt := func(i int, inList bool) (int, error) {
		t := at(i)
		if t.punct("(") {
			return i - 1, nil // subquery or tuple: scanned as the walk continues
		}
		if t.name() && at(i+1).punct("(") {
			fn := strings.ToLower(t.text)
			if inList && (fn == "tuple" || fn == "array") {
				return i - 1, nil
			}
			if t.kind == tkWord && safeTableFuncs[fn] {
				return i, nil
			}
			return 0, errUnclear{fmt.Sprintf("table function %s() is not allowed for keys with a database allow-list", t.text)}
		}
		var parts []string
		j := i
		for {
			text, width, err := partAt(j)
			if err != nil {
				return 0, err
			}
			if width == 0 {
				break
			}
			if width == 1 {
				parts = append(parts, text) // `a.b` is one name
			} else {
				parts = append(parts, strings.Split(text, ".")...)
			}
			j += width
			if !at(j).punct(".") {
				break
			}
			j++
		}
		if len(parts) == 0 {
			if inList {
				return i - 1, nil // IN 'literal', IN 1: not a table
			}
			return 0, errUnclear{fmt.Sprintf("could not tell which table %q refers to", t.text)}
		}
		if at(j-1).punct(".") || at(j).punct(".") {
			return 0, errUnclear{fmt.Sprintf("could not resolve %s.", strings.Join(parts, "."))}
		}
		switch len(parts) {
		case 1:
			if !cur.isCTE(parts[0]) {
				refs = append(refs, tableRef{Table: parts[0]})
			}
		case 2:
			refs = append(refs, tableRef{DB: parts[0], Table: parts[1]})
		default:
			return 0, errUnclear{fmt.Sprintf("could not resolve %s", strings.Join(parts, "."))}
		}
		return j - 1, nil
	}

	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch t.kind {
		case tkPunct:
			switch t.text {
			case "(":
				special := at(i-1).kind == tkWord && noTableKeywordFuncs[strings.ToLower(at(i-1).text)]
				cur = &scope{parent: cur, special: special}
			case ")":
				if cur.parent == nil {
					return nil, errUnclear{"unbalanced parentheses"}
				}
				cur = cur.parent
			case ",":
				if !cur.special && cur.inFromList() {
					next, err := tableAt(i+1, false)
					if err != nil {
						return nil, err
					}
					i = next
				}
			}
			continue
		case tkWord, tkIdent:
		default:
			continue
		}

		// Function calls that read a table or dictionary named in a string.
		if at(i+1).punct("(") && dataByNameFuncRe.MatchString(t.text) {
			return nil, errUnclear{fmt.Sprintf("%s() reads data by name and is not allowed for keys with a database allow-list", t.text)}
		}
		if at(i - 1).punct(".") {
			continue // the second half of a.b is never a keyword
		}
		// WITH db.t AS x: an identifier aliased in WITH may name a table.
		if cur.clause == "WITH" && (at(i-1).is("WITH") || at(i-1).punct(",")) {
			parts := []string{t.text}
			j := i + 1
			for ; at(j).punct(".") && at(j+1).name(); j += 2 {
				parts = append(parts, at(j+1).text)
			}
			if at(j).is("AS") && !at(j+1).punct("(") {
				switch {
				case len(parts) == 1 && !cur.isCTE(parts[0]):
					refs = append(refs, tableRef{Table: parts[0]})
				case len(parts) == 2:
					refs = append(refs, tableRef{DB: parts[0], Table: parts[1]})
				case len(parts) > 2:
					return nil, errUnclear{fmt.Sprintf("could not resolve %s", strings.Join(parts, "."))}
				}
			}
		}
		if t.kind != tkWord {
			continue
		}
		up := strings.ToUpper(t.text)
		switch up {
		case "AS":
			next := at(i + 1)
			if next.punct("(") {
				// name AS (subquery) inside WITH defines a CTE.
				if cur.clause == "WITH" && at(i-1).name() {
					if cur.ctes == nil {
						cur.ctes = map[string]bool{}
					}
					cur.ctes[at(i-1).text] = true
				}
				continue
			}
			if next.name() {
				i++ // an alias, whatever word it is
			}
		case "WITH":
			switch strings.ToUpper(at(i + 1).text) {
			case "TOTALS", "FILL", "TIES", "ROLLUP", "CUBE":
			default:
				cur.clause = "WITH"
			}
		case "FROM":
			if cur.special {
				continue
			}
			// a IS [NOT] DISTINCT FROM b is a comparison.
			if at(i-1).is("DISTINCT") && (at(i-2).is("IS") || (at(i-2).is("NOT") && at(i-3).is("IS"))) {
				continue
			}
			cur.clause = "FROM"
			next, err := tableAt(i+1, false)
			if err != nil {
				return nil, err
			}
			i = next
		case "JOIN":
			if at(i - 1).is("ARRAY") {
				cur.clause = "ARRAY JOIN"
				continue
			}
			cur.clause = "JOIN"
			next, err := tableAt(i+1, false)
			if err != nil {
				return nil, err
			}
			i = next
		case "IN":
			if cur.special {
				continue
			}
			next, err := tableAt(i+1, true)
			if err != nil {
				return nil, err
			}
			i = next
		case "ON", "USING":
			if cur.inFromList() {
				cur.clause = up
			}
		case "OFFSET":
			// SAMPLE k OFFSET m stays inside the FROM list.
			if !cur.inFromList() {
				cur.clause = up
			}
		case "UNION", "INTERSECT":
			cur.clause, cur.ctes = "", nil
		case "EXCEPT":
			// set operator, not the * EXCEPT (col) column modifier
			if n := at(i + 1); n.is("SELECT") || n.is("WITH") || n.is("ALL") || n.is("DISTINCT") || (n.punct("(") && (at(i+2).is("SELECT") || at(i+2).is("WITH") || at(i+2).punct("("))) {
				cur.clause, cur.ctes = "", nil
			}
		default:
			// format(...) is a function, FORMAT a clause.
			if clauseWords[up] && !(up == "FORMAT" && at(i+1).punct("(")) {
				cur.clause = up
			}
		}
	}
	if cur.parent != nil {
		return nil, errUnclear{"unbalanced parentheses"}
	}
	return refs, nil
}

var simpleIdentPathRe = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)?$`)

// checkAllowedDatabases rejects sql when the key has a database allow-list
// and the statement reads, or might read, a database outside it. Keys
// without an allow-list are not checked.
func checkAllowedDatabases(lookupCurrentDB func() (string, error), k allowList, sql string, params map[string]string) error {
	set := k.set()
	if set == nil {
		return nil
	}
	refs, err := referencedTables(sql, params)
	if err != nil {
		return fmt.Errorf("query rejected: %v (this key is limited to databases: %s)", err, k.describe())
	}
	var current string
	for _, r := range refs {
		db := r.DB
		if db == "" {
			if current == "" {
				if current, err = lookupCurrentDB(); err != nil || current == "" {
					return fmt.Errorf("query rejected: could not determine the current database for unqualified table %q; qualify it as database.table", r.Table)
				}
			}
			db = current
		}
		if set[db] {
			continue
		}
		if strings.EqualFold(db, "system") && harmlessSystemTables[strings.ToLower(r.Table)] {
			continue
		}
		return fmt.Errorf("query rejected: database %q (table %s) is not in this key's allowlist; allowed: %s", db, r.Table, k.describe())
	}
	return nil
}

// allowList adapts the key's comma-separated AllowedDatabases.
type allowList string

func (a allowList) set() map[string]bool {
	raw := strings.TrimSpace(string(a))
	if raw == "" {
		return nil
	}
	set := make(map[string]bool)
	for _, d := range strings.Split(raw, ",") {
		if d = strings.TrimSpace(d); d != "" {
			set[d] = true
		}
	}
	return set
}

func (a allowList) describe() string {
	var names []string
	for _, d := range strings.Split(string(a), ",") {
		if d = strings.TrimSpace(d); d != "" {
			names = append(names, d)
		}
	}
	return strings.Join(names, ", ")
}
