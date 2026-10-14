package handlers

import (
	"regexp"
	"strings"
)

// A small ClickHouse SQL lexer: enough to tell code from strings, quoted
// identifiers, heredocs and comments, so text rewrites (formatting, statement
// splitting, keyword detection) never touch what is inside a literal or a
// comment. It mirrors ui/src/lib/editor/statements.ts.

type sqlTokenKind int

const (
	tokWord       sqlTokenKind = iota // identifier or keyword
	tokSpace                          // whitespace
	tokString                         // '...' or $tag$...$tag$
	tokIdentifier                     // `...` or "..."
	tokComment                        // -- ..., # ..., /* ... */
	tokOther                          // numbers, operators, punctuation
)

type sqlToken struct {
	kind sqlTokenKind
	text string
}

var heredocOpenRe = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

func quotedEnd(sql string, start int, quote byte) int {
	i := start + 1
	for i < len(sql) {
		switch sql[i] {
		case '\\':
			i += 2
			continue
		case quote:
			if i+1 < len(sql) && sql[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		}
		i++
	}
	return len(sql)
}

func blockCommentEnd(sql string, start int) int {
	depth := 0
	i := start
	for i < len(sql) {
		switch {
		case strings.HasPrefix(sql[i:], "/*"):
			depth++
			i += 2
		case strings.HasPrefix(sql[i:], "*/"):
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(sql)
}

func isWordByte(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c >= 0x80
}

func isSpaceByte(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v'
}

func lineEnd(sql string, start int) int {
	if nl := strings.IndexByte(sql[start:], '\n'); nl >= 0 {
		return start + nl
	}
	return len(sql)
}

// tokenizeSQL splits sql into tokens whose texts concatenate back to sql.
func tokenizeSQL(sql string) []sqlToken {
	var out []sqlToken
	i := 0
	for i < len(sql) {
		c := sql[i]
		var next byte
		if i+1 < len(sql) {
			next = sql[i+1]
		}
		end := i + 1
		kind := tokOther
		switch {
		case isSpaceByte(c):
			for end < len(sql) && isSpaceByte(sql[end]) {
				end++
			}
			kind = tokSpace
		case c == '\'':
			end, kind = quotedEnd(sql, i, '\''), tokString
		case c == '"' || c == '`':
			end, kind = quotedEnd(sql, i, c), tokIdentifier
		case c == '-' && next == '-', c == '#' && (next == ' ' || next == '!'):
			end, kind = lineEnd(sql, i), tokComment
		case c == '/' && next == '*':
			end, kind = blockCommentEnd(sql, i), tokComment
		case c == '$' && heredocOpenRe.MatchString(sql[i:]):
			tag := heredocOpenRe.FindString(sql[i:])
			if close := strings.Index(sql[i+len(tag):], tag); close >= 0 {
				end = i + len(tag) + close + len(tag)
			} else {
				end = len(sql)
			}
			kind = tokString
		case isWordByte(c):
			for end < len(sql) && isWordByte(sql[end]) {
				end++
			}
			kind = tokWord
			if c >= '0' && c <= '9' {
				kind = tokOther
			}
		}
		out = append(out, sqlToken{kind: kind, text: sql[i:end]})
		i = end
	}
	return out
}

// sqlHasComments reports whether the SQL contains any comment.
func sqlHasComments(sql string) bool {
	for _, t := range tokenizeSQL(sql) {
		if t.kind == tokComment {
			return true
		}
	}
	return false
}

// splitSQLStatements splits on ';' outside strings, identifiers and comments.
// Statements that hold only whitespace or comments are dropped.
func splitSQLStatements(sql string) []string {
	var out []string
	var cur strings.Builder
	hasCode := false
	flush := func() {
		if hasCode {
			out = append(out, strings.TrimSpace(cur.String()))
		}
		cur.Reset()
		hasCode = false
	}
	for _, t := range tokenizeSQL(sql) {
		if t.kind == tokOther && t.text == ";" {
			flush()
			continue
		}
		if t.kind != tokSpace && t.kind != tokComment {
			hasCode = true
		}
		cur.WriteString(t.text)
	}
	flush()
	return out
}

// stripLeadingSQLComments drops leading whitespace and comments.
func stripLeadingSQLComments(sql string) string {
	offset := 0
	for _, t := range tokenizeSQL(sql) {
		if t.kind != tokSpace && t.kind != tokComment {
			break
		}
		offset += len(t.text)
	}
	return sql[offset:]
}

// trimTrailingSQLNoise drops trailing whitespace, semicolons and comments.
func trimTrailingSQLNoise(sql string) string {
	end, offset := 0, 0
	for _, t := range tokenizeSQL(sql) {
		offset += len(t.text)
		if t.kind != tokSpace && t.kind != tokComment && t.text != ";" {
			end = offset
		}
	}
	return sql[:end]
}

// firstSQLKeyword returns the upper-cased first word of the statement, after comments.
func firstSQLKeyword(sql string) string {
	for _, t := range tokenizeSQL(sql) {
		switch t.kind {
		case tokSpace, tokComment:
			continue
		case tokWord:
			return strings.ToUpper(t.text)
		default:
			return ""
		}
	}
	return ""
}

// startsWithExplain reports whether the statement is already an EXPLAIN.
func startsWithExplain(sql string) bool {
	return firstSQLKeyword(sql) == "EXPLAIN"
}

// --- Fallback formatter ---

// formatUpperWords are upper-cased by the fallback formatter. Words that are
// common column names or functions (end, table, set, key, value, format, date,
// desc, all, ...) are deliberately left out: ClickHouse identifiers are case
// sensitive, so upper-casing one would change the query.
var formatUpperWords = map[string]bool{
	"SELECT": true, "FROM": true, "WHERE": true, "PREWHERE": true, "HAVING": true,
	"LIMIT": true, "OFFSET": true, "UNION": true, "JOIN": true, "INNER": true,
	"LEFT": true, "RIGHT": true, "FULL": true, "CROSS": true, "OUTER": true,
	"ON": true, "USING": true, "AS": true, "AND": true, "OR": true, "NOT": true,
	"IN": true, "IS": true, "NULL": true, "BETWEEN": true, "LIKE": true, "ILIKE": true,
	"DISTINCT": true, "CASE": true, "WHEN": true, "THEN": true, "ELSE": true,
	"WITH": true, "INSERT": true, "INTO": true,
	"VALUES": true, "EXISTS": true,
}

// clauseStarts get a line of their own when they open a clause.
var clauseStarts = map[string]bool{
	"FROM": true, "WHERE": true, "PREWHERE": true, "GROUP": true, "ORDER": true,
	"HAVING": true, "LIMIT": true, "UNION": true, "SETTINGS": true,
	"JOIN": true, "INNER": true, "LEFT": true, "RIGHT": true, "FULL": true,
	"CROSS": true, "ARRAY": true, "GLOBAL": true, "ANY": true, "ASOF": true,
}

// joinModifiers may precede JOIN; a newline goes before the first of them.
var joinModifiers = map[string]bool{
	"INNER": true, "LEFT": true, "RIGHT": true, "FULL": true, "CROSS": true,
	"OUTER": true, "ANY": true, "ALL": true, "ASOF": true, "SEMI": true,
	"ANTI": true, "GLOBAL": true, "ARRAY": true, "PASTE": true,
}

func nextCodeToken(tokens []sqlToken, i int) (sqlToken, int) {
	for j := i + 1; j < len(tokens); j++ {
		if tokens[j].kind != tokSpace && tokens[j].kind != tokComment {
			return tokens[j], j
		}
	}
	return sqlToken{}, -1
}

func prevCodeToken(tokens []sqlToken, i int) sqlToken {
	for j := i - 1; j >= 0; j-- {
		if tokens[j].kind != tokSpace && tokens[j].kind != tokComment {
			return tokens[j]
		}
	}
	return sqlToken{}
}

// isClauseStart reports whether the word at i opens a clause that deserves a
// new line: GROUP/ORDER only before BY, join modifiers only before a JOIN.
func isClauseStart(tokens []sqlToken, i int) bool {
	word := strings.ToUpper(tokens[i].text)
	if !clauseStarts[word] {
		return false
	}
	next, j := nextCodeToken(tokens, i)
	nextWord := strings.ToUpper(next.text)
	switch {
	case word == "GROUP" || word == "ORDER":
		return next.kind == tokWord && nextWord == "BY"
	case word == "JOIN":
		// Only when no modifier precedes it (the modifier got the newline).
		prev := prevCodeToken(tokens, i)
		return !(prev.kind == tokWord && joinModifiers[strings.ToUpper(prev.text)])
	case joinModifiers[word]:
		if prev := prevCodeToken(tokens, i); prev.kind == tokWord && joinModifiers[strings.ToUpper(prev.text)] {
			return false
		}
		for j >= 0 && next.kind == tokWord {
			n := strings.ToUpper(next.text)
			if n == "JOIN" {
				return true
			}
			if !joinModifiers[n] {
				return false
			}
			next, j = nextCodeToken(tokens, j)
		}
		return false
	case word == "SETTINGS":
		// SETTINGS name = value
		return next.kind == tokWord
	}
	return true
}

// formatSQL is the fallback formatter used when ClickHouse cannot format the
// query itself. It only changes whitespace between code tokens and the case
// of a short list of unambiguous keywords; strings, identifiers and comments
// are copied as they are, and no newline is ever removed (so a -- comment
// can never swallow code).
func formatSQL(sql string) string {
	tokens := tokenizeSQL(strings.TrimSpace(sql))
	var b strings.Builder
	// Paren stack: true when the paren holds a subquery (its first word is SELECT/WITH).
	var stack []bool
	inSubquery := func() bool { return len(stack) == 0 || stack[len(stack)-1] }
	depth := func() int {
		n := 0
		for _, sub := range stack {
			if sub {
				n++
			}
		}
		return n
	}
	pendingSpace := ""
	for i, t := range tokens {
		switch t.kind {
		case tokSpace:
			pendingSpace = t.text
			continue
		case tokOther:
			switch t.text {
			case "(":
				next, _ := nextCodeToken(tokens, i)
				w := strings.ToUpper(next.text)
				stack = append(stack, next.kind == tokWord && (w == "SELECT" || w == "WITH"))
			case ")":
				if len(stack) > 0 {
					stack = stack[:len(stack)-1]
				}
			}
		case tokWord:
			prev := prevCodeToken(tokens, i)
			next, _ := nextCodeToken(tokens, i)
			// Qualified names (db.table, t.end) and aliases are identifiers.
			identifierPos := prev.text == "." || next.text == "." || strings.EqualFold(prev.text, "AS")
			upper := strings.ToUpper(t.text)
			// A word glued to "(" is a function call (in(), not(), like()), leave it.
			isCall := i+1 < len(tokens) && tokens[i+1].text == "("
			// GROUP / ORDER / BY are keywords only as a pair.
			isByPair := (upper == "GROUP" || upper == "ORDER") && strings.EqualFold(next.text, "BY") && next.kind == tokWord ||
				upper == "BY" && prev.kind == tokWord && (strings.EqualFold(prev.text, "GROUP") || strings.EqualFold(prev.text, "ORDER"))
			if !identifierPos && (formatUpperWords[upper] || isByPair) && !isCall {
				t.text = upper
			}
			if !identifierPos && b.Len() > 0 && inSubquery() && isClauseStart(tokens, i) {
				b.WriteString("\n" + strings.Repeat("    ", depth()))
				b.WriteString(t.text)
				pendingSpace = ""
				continue
			}
		}
		b.WriteString(pendingSpace)
		pendingSpace = ""
		b.WriteString(t.text)
	}
	return strings.TrimSpace(b.String())
}
