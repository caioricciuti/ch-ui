package lineage

import "strings"

// refKind says what a parsed reference points at.
type refKind uint8

const (
	refTable    refKind = iota // a table, view or MV (db may be empty: unqualified)
	refExternal                // a table function reading outside ClickHouse
)

// ref is one object a definition reads from.
type ref struct {
	kind     refKind
	db, name string // refTable
	external string // refExternal: descriptor, see externalFunction
}

// parser collects the objects a SELECT reads: FROM and JOIN tables (comma
// joins included), subqueries at any depth, table functions, dictGet* and
// dictionary() targets. CTE names are not tables and aliases are skipped.
type parser struct {
	toks  []token
	match []int // index of the matching paren, -1 when unbalanced or not a paren
	encl  []int // innermost open paren around each token, -1 at top level
	ctes  map[string]bool
	refs  []ref
}

// selectRefs returns what sql reads, in first-seen order without duplicates.
// It never fails; what it cannot follow it skips.
func selectRefs(sql string) []ref {
	return refsFromTokens(tokenize(sql))
}

func refsFromTokens(toks []token) []ref {
	p := parser{toks: toks}
	p.index()
	p.walk(0, len(toks))
	return dedupeRefs(p.refs)
}

func dedupeRefs(refs []ref) []ref {
	if len(refs) < 2 {
		return refs
	}
	out := refs[:0]
	for i, r := range refs {
		dup := false
		for _, prev := range refs[:i] {
			if prev == r {
				dup = true
				break
			}
		}
		if !dup {
			out = append(out, r)
		}
	}
	return out
}

func (p *parser) index() {
	n := len(p.toks)
	p.match = make([]int, n)
	p.encl = make([]int, n)
	stack := make([]int, 0, 8)
	for i, t := range p.toks {
		p.match[i] = -1
		if len(stack) > 0 {
			p.encl[i] = stack[len(stack)-1]
		} else {
			p.encl[i] = -1
		}
		switch {
		case t.punct('('):
			stack = append(stack, i)
		case t.punct(')'):
			if len(stack) > 0 {
				open := stack[len(stack)-1]
				stack = stack[:len(stack)-1]
				p.match[open], p.match[i] = i, open
				p.encl[i] = p.encl[open]
			}
		case t.keyword("AS") && i > 0 && p.toks[i-1].name() && i+2 < n && p.toks[i+1].punct('(') &&
			(p.toks[i+2].keyword("SELECT") || p.toks[i+2].keyword("WITH") || p.toks[i+2].punct('(')):
			// name AS (SELECT ...) is a CTE.
			if p.ctes == nil {
				p.ctes = map[string]bool{}
			}
			p.ctes[p.toks[i-1].text] = true
		}
	}
	// An unclosed paren makes the rest of the statement its body.
	for _, open := range stack {
		p.match[open] = n
	}
}

// closeOf returns the index of the paren closing the one at open, or hi.
func (p *parser) closeOf(open, hi int) int {
	if c := p.match[open]; c > open && c <= hi {
		return c
	}
	return hi
}

// Functions whose arguments use FROM as syntax, not for a table.
var fromSyntaxFuncs = map[string]bool{"extract": true, "trim": true, "substring": true, "substr": true, "overlay": true}

func (p *parser) walk(lo, hi int) {
	for i := lo; i < hi; {
		t := p.toks[i]
		if t.kind != tIdent {
			i++
			continue
		}
		switch {
		case t.keyword("FROM") && p.fromIsTable(i):
			i = p.tableList(i+1, hi)
		case t.keyword("JOIN") && !(i > 0 && p.toks[i-1].keyword("ARRAY")):
			i = p.tableList(i+1, hi)
		case i+2 < hi && p.toks[i+1].punct('(') && p.toks[i+2].kind == tString && isDictFunc(t.text):
			p.addDictionary(p.toks[i+2].text)
			i += 3
		default:
			i++
		}
	}
}

func (p *parser) fromIsTable(i int) bool {
	if e := p.encl[i]; e > 0 && p.toks[e-1].kind == tIdent && fromSyntaxFuncs[strings.ToLower(p.toks[e-1].text)] {
		return false
	}
	// a IS [NOT] DISTINCT FROM b
	return !(i > 0 && p.toks[i-1].keyword("DISTINCT"))
}

func isDictFunc(name string) bool {
	if len(name) < 7 || !strings.EqualFold(name[:4], "dict") {
		return false
	}
	rest := strings.ToLower(name[4:])
	return strings.HasPrefix(rest, "get") || strings.HasPrefix(rest, "has") || strings.HasPrefix(rest, "isin")
}

// Keywords that end a table expression; anything else after a table is an alias.
var tableClauseEnd = map[string]bool{
	"WHERE": true, "PREWHERE": true, "GROUP": true, "ORDER": true, "LIMIT": true, "HAVING": true,
	"UNION": true, "EXCEPT": true, "INTERSECT": true, "JOIN": true, "INNER": true, "LEFT": true,
	"RIGHT": true, "FULL": true, "CROSS": true, "OUTER": true, "ANY": true, "ALL": true, "ASOF": true,
	"SEMI": true, "ANTI": true, "GLOBAL": true, "LOCAL": true, "ARRAY": true, "ON": true, "USING": true,
	"SAMPLE": true, "SETTINGS": true, "FORMAT": true, "WINDOW": true, "QUALIFY": true, "INTO": true,
	"WITH": true, "SELECT": true, "OFFSET": true, "FETCH": true, "PASTE": true, "COMMENT": true,
	"INTERPOLATE": true, "FINAL": true, "AS": true,
}

// tableList reads one or more comma separated table expressions.
func (p *parser) tableList(i, hi int) int {
	for i < hi {
		next := p.tableExpr(i, hi)
		if next == i {
			return i
		}
		i = next
		// FINAL, AS alias, or a bare alias.
		for i < hi {
			t := p.toks[i]
			if t.keyword("FINAL") {
				i++
				continue
			}
			if t.keyword("AS") {
				i += 2
				break
			}
			if t.kind == tQuoted || t.kind == tIdent && !tableClauseEnd[strings.ToUpper(t.text)] {
				i++
			}
			break
		}
		if i < hi && p.toks[i].keyword("FINAL") {
			i++
		}
		if i >= hi || !p.toks[i].punct(',') {
			return i
		}
		i++
	}
	return i
}

// tableExpr reads one table, subquery or table function; it returns i when
// there is nothing it understands at i.
func (p *parser) tableExpr(i, hi int) int {
	t := p.toks[i]
	if t.punct('(') {
		end := p.closeOf(i, hi)
		p.walk(i+1, end)
		return min(end+1, hi)
	}
	if t.kind == tParam {
		return i + 1
	}
	if !t.name() {
		return i
	}
	db, name, j := "", t.text, i+1
	if j+1 < hi && p.toks[j].punct('.') && p.toks[j+1].name() {
		db, name, j = t.text, p.toks[j+1].text, j+2
	}
	if j < hi && p.toks[j].punct('(') {
		end := p.closeOf(j, hi)
		if db == "" {
			p.tableFunction(strings.ToLower(name), j+1, end)
		}
		return min(end+1, hi)
	}
	p.addTable(db, name)
	return j
}

func (p *parser) addTable(db, name string) {
	if db == "" && p.ctes[name] {
		return
	}
	if isSystemDatabase(db) {
		return
	}
	p.refs = append(p.refs, ref{kind: refTable, db: db, name: name})
}

// addDictionary records a dictGet('db.dict', ...) or dictionary('dict') target.
func (p *parser) addDictionary(full string) {
	db, name := splitQualified(full)
	if name != "" {
		p.addTable(db, name)
	}
}

// args splits the tokens in [lo, hi) at top level commas.
func (p *parser) args(lo, hi int) [][2]int {
	if lo >= hi {
		return nil
	}
	var out [][2]int
	start := lo
	for i := lo; i < hi; i++ {
		t := p.toks[i]
		if t.punct('(') {
			i = p.closeOf(i, hi)
			continue
		}
		if t.punct(',') {
			out = append(out, [2]int{start, i})
			start = i + 1
		}
	}
	return append(out, [2]int{start, hi})
}

// argValue is a literal or bare identifier argument; complex ones are "".
type argValue struct {
	text    string
	literal bool // a string literal, not an identifier
}

func (p *parser) argValues(spans [][2]int) []argValue {
	out := make([]argValue, len(spans))
	for k, s := range spans {
		if s[1]-s[0] == 1 {
			t := p.toks[s[0]]
			if t.kind == tString || t.kind == tNumber {
				out[k] = argValue{t.text, true}
			} else if t.name() {
				out[k] = argValue{t.text, false}
			}
		}
	}
	return out
}

// spanRef reads db.t, t, or 'db.t' from one argument.
func (p *parser) spanRef(s [2]int) (db, name string, ok bool) {
	switch s[1] - s[0] {
	case 1:
		t := p.toks[s[0]]
		if t.kind == tString {
			db, name = splitQualified(t.text)
			return db, name, name != ""
		}
		return "", t.text, t.name()
	case 3:
		a, dot, b := p.toks[s[0]], p.toks[s[0]+1], p.toks[s[0]+2]
		return a.text, b.text, a.name() && dot.punct('.') && b.name()
	}
	return "", "", false
}

// Table functions that generate rows or read local server state: no node.
var generatorFuncs = map[string]bool{
	"numbers": true, "numbers_mt": true, "zeros": true, "zeros_mt": true, "generaterandom": true,
	"generate_series": true, "generateseries": true, "values": true, "null": true, "input": true,
	"format": true, "primes": true, "merge": true, "loop": true, "fuzzjson": true, "fuzzquery": true,
	"mergetreeindex": true, "mergetreeprojection": true, "timeseriesdata": true, "timeseriesmetrics": true,
	"timeseriestags": true,
}

func (p *parser) tableFunction(fn string, lo, hi int) {
	spans := p.args(lo, hi)
	switch {
	case generatorFuncs[fn]:
	case fn == "view" || fn == "viewifpermitted" || fn == "viewexplain":
		p.walk(lo, hi)
	case fn == "dictionary":
		if vals := p.argValues(spans); len(vals) > 0 && vals[0].text != "" {
			p.addDictionary(vals[0].text)
		}
	case fn == "cluster" || fn == "clusterallreplicas":
		// The same table on every node of a cluster: the local table.
		if db, name, ok := p.clusterTarget(spans); ok {
			p.addTable(db, name)
		} else if len(spans) > 1 {
			p.walk(spans[1][0], hi)
		}
	case fn == "remote" || fn == "remotesecure":
		vals := p.argValues(spans)
		if db, name, ok := p.clusterTarget(spans); ok && len(vals) > 0 && vals[0].literal {
			p.refs = append(p.refs, ref{kind: refExternal, external: "remote://" + sanitizeLocation(vals[0].text) + "/" + qualified(db, name)})
		}
	default:
		if ext := externalFunction(fn, p.argValues(spans)); ext != "" {
			p.refs = append(p.refs, ref{kind: refExternal, external: ext})
		}
	}
}

// clusterTarget reads the table of cluster('c', db.t), cluster('c', 'db', 't')
// or cluster('c', db, t); remote() takes the same shape.
func (p *parser) clusterTarget(spans [][2]int) (db, name string, ok bool) {
	switch {
	case len(spans) == 2:
		return p.spanRef(spans[1])
	case len(spans) >= 3:
		vals := p.argValues(spans[1:3])
		if vals[0].text != "" && vals[1].text != "" {
			// cluster('c', db.t, sharding_key) is two-part too.
			if d, n, ok := p.spanRef(spans[1]); ok && d != "" {
				return d, n, true
			}
			return vals[0].text, vals[1].text, true
		}
		return p.spanRef(spans[1])
	}
	return "", "", false
}

func splitQualified(s string) (db, name string) {
	if i := strings.IndexByte(s, '.'); i >= 0 {
		return s[:i], s[i+1:]
	}
	return "", s
}

func qualified(db, name string) string {
	if db == "" {
		return name
	}
	return db + "." + name
}

func isSystemDatabase(db string) bool {
	return db == "system" || strings.EqualFold(db, "information_schema")
}

// definition is what a view, MV or refreshable MV declares besides its
// SELECT: the TO target, DEPENDS ON list, and whether it refreshes.
type definition struct {
	to          *ref
	dependsOn   []ref
	refreshable bool
	body        []token // tokens after the top level AS, for servers without as_select
}

// parseDefinition reads a CREATE [MATERIALIZED] VIEW statement's header up to
// the top level AS that starts its SELECT. withBody keeps the SELECT tokens.
func parseDefinition(ddl string, withBody bool) definition {
	var d definition
	l := lexer{s: ddl}
	var prev token
	depth := 0
	engineSeen := false
	var pending []token // name tokens read after TO or DEPENDS ON
	mode := ""          // "to" or "depends" while reading names
	expect := false     // a name must follow (after TO, ON or a comma)
	flush := func() {
		if len(pending) == 0 {
			return
		}
		r := ref{kind: refTable, name: pending[0].text}
		if len(pending) == 3 {
			r.db, r.name = pending[0].text, pending[2].text
		}
		if mode == "to" && d.to == nil {
			d.to = &r
		} else if mode == "depends" {
			d.dependsOn = append(d.dependsOn, r)
		}
		pending = pending[:0]
	}
	for {
		t, ok := l.next()
		if !ok {
			flush()
			return d
		}
		if mode != "" {
			switch {
			case expect && t.name():
				pending, expect = append(pending, t), false
				continue
			case len(pending) == 1 && t.punct('.'):
				pending = append(pending, t)
				continue
			case len(pending) == 2 && t.name():
				pending = append(pending, t)
				flush()
				continue
			}
			flush()
			if mode == "depends" && t.punct(',') {
				expect = true
				continue
			}
			mode, expect = "", false
		}
		switch {
		case t.punct('('):
			depth++
		case t.punct(')'):
			depth--
		case depth != 0:
		case t.keyword("REFRESH"):
			d.refreshable = true
		case t.keyword("ON") && prev.keyword("DEPENDS"):
			mode, expect = "depends", true
		case t.keyword("ENGINE"):
			engineSeen = true
		case t.keyword("TO") && !engineSeen && d.to == nil:
			mode, expect = "to", true
		case t.keyword("AS"):
			// AS SELECT / AS WITH / AS ( starts the body.
			save := l
			n, ok := l.next()
			if ok && (n.keyword("SELECT") || n.keyword("WITH") || n.punct('(')) {
				if withBody {
					d.body = append([]token{n}, tokenize(l.s[l.i:])...)
				}
				return d
			}
			l = save
		}
		prev = t
	}
}

// dictionarySource is the SOURCE(...) clause of a CREATE DICTIONARY.
type dictionarySource struct {
	kind string            // lower case: clickhouse, mysql, http, ...
	keys map[string]string // upper case key to simple value
}

func parseDictionarySource(ddl string) (dictionarySource, bool) {
	toks := tokenize(ddl)
	depth := 0
	for i := 0; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.punct('('):
			depth++
		case t.punct(')'):
			depth--
		case depth == 0 && t.keyword("SOURCE") && i+3 < len(toks) && toks[i+1].punct('(') && toks[i+2].kind == tIdent && toks[i+3].punct('('):
			src := dictionarySource{kind: strings.ToLower(toks[i+2].text), keys: map[string]string{}}
			inner := 0
			for j := i + 4; j < len(toks); j++ {
				tj := toks[j]
				if tj.punct('(') {
					inner++
					continue
				}
				if tj.punct(')') {
					if inner == 0 {
						break
					}
					inner--
					continue
				}
				if inner == 0 && tj.kind == tIdent && j+1 < len(toks) {
					v := toks[j+1]
					if v.kind == tString || v.kind == tNumber || v.name() {
						src.keys[strings.ToUpper(tj.text)] = v.text
						j++
					}
				}
			}
			return src, true
		}
	}
	return dictionarySource{}, false
}

// engineSpec is a table engine with its arguments and SETTINGS, read from
// system.tables.engine_full.
type engineSpec struct {
	args     []argValue
	argSpans [][2]int
	settings map[string]string
	p        *parser
}

func parseEngineFull(engineFull string) engineSpec {
	toks := tokenize(engineFull)
	p := &parser{toks: toks}
	p.index()
	spec := engineSpec{p: p}
	if len(toks) > 1 && toks[1].punct('(') {
		end := p.closeOf(1, len(toks))
		spec.argSpans = p.args(2, end)
		spec.args = p.argValues(spec.argSpans)
	}
	for i := 0; i < len(toks); i++ {
		if !(toks[i].keyword("SETTINGS") && p.encl[i] == -1) {
			continue
		}
		for j := i + 1; j+2 < len(toks); j++ {
			if toks[j].kind == tIdent && toks[j+1].punct('=') {
				v := toks[j+2]
				if v.kind == tString || v.kind == tNumber || v.name() {
					if spec.settings == nil {
						spec.settings = map[string]string{}
					}
					spec.settings[strings.ToLower(toks[j].text)] = v.text
				}
				j += 2
			}
		}
		break
	}
	return spec
}

// arg returns the i-th argument's text, resolving currentDatabase() to own.
func (e engineSpec) arg(i int, own string) string {
	if i >= len(e.args) {
		return ""
	}
	if e.args[i].text != "" {
		return e.args[i].text
	}
	s := e.argSpans[i]
	if s[1]-s[0] == 3 && strings.EqualFold(e.p.toks[s[0]].text, "currentDatabase") {
		return own
	}
	return ""
}

// argCall returns fn and its single literal argument when argument i is
// fn('literal'), as in Merge(REGEXP('^db'), ...).
func (e engineSpec) argCall(i int) (fn, value string) {
	if i >= len(e.argSpans) {
		return "", ""
	}
	s := e.argSpans[i]
	t := e.p.toks
	if s[1]-s[0] == 4 && t[s[0]].kind == tIdent && t[s[0]+1].punct('(') && t[s[0]+2].kind == tString {
		return strings.ToLower(t[s[0]].text), t[s[0]+2].text
	}
	return "", ""
}
