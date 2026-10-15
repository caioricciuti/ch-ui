package lineage

import "strings"

type tokKind uint8

const (
	tIdent  tokKind = iota // bare identifier or keyword
	tQuoted                // `quoted` or "quoted" identifier, unescaped
	tString                // 'string literal', unescaped
	tNumber
	tParam // {name:Type} query parameter
	tPunct // any other single byte
)

type token struct {
	kind tokKind
	text string
}

func (t token) is(kind tokKind, text string) bool { return t.kind == kind && t.text == text }

// keyword reports whether t is the bare keyword kw (upper case).
func (t token) keyword(kw string) bool { return t.kind == tIdent && strings.EqualFold(t.text, kw) }

func (t token) name() bool { return t.kind == tIdent || t.kind == tQuoted }

func (t token) punct(c byte) bool { return t.kind == tPunct && len(t.text) == 1 && t.text[0] == c }

// lexer splits ClickHouse SQL into tokens. It never fails: anything it does
// not understand becomes a one byte punctuation token, so a definition the
// parser cannot follow yields fewer references, never an error.
type lexer struct {
	s string
	i int
}

func (l *lexer) next() (token, bool) {
	s := l.s
	for l.i < len(s) {
		c := s[l.i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
			l.i++
		case c == '-' && l.i+1 < len(s) && s[l.i+1] == '-', c == '#':
			for l.i < len(s) && s[l.i] != '\n' {
				l.i++
			}
		case c == '/' && l.i+1 < len(s) && s[l.i+1] == '*':
			l.skipBlockComment()
		case c == '\'':
			return token{tString, l.quoted('\'')}, true
		case c == '`' || c == '"':
			return token{tQuoted, l.quoted(c)}, true
		case c >= '0' && c <= '9':
			start := l.i
			for l.i < len(s) && (isWordByte(s[l.i]) || s[l.i] == '.') {
				l.i++
			}
			return token{tNumber, s[start:l.i]}, true
		case isWordByte(c) || c == '$':
			if c == '$' {
				if tag, ok := l.heredocTag(); ok {
					l.i += len(tag)
					end := strings.Index(s[l.i:], tag)
					if end < 0 {
						end = len(s) - l.i
					}
					text := s[l.i : l.i+end]
					l.i = min(len(s), l.i+end+len(tag))
					return token{tString, text}, true
				}
			}
			start := l.i
			for l.i < len(s) && (isWordByte(s[l.i]) || s[l.i] == '$') {
				l.i++
			}
			return token{tIdent, s[start:l.i]}, true
		case c == '{':
			start := l.i
			end := strings.IndexByte(s[l.i:], '}')
			if end < 0 {
				l.i++
				return token{tPunct, "{"}, true
			}
			l.i += end + 1
			return token{tParam, s[start:l.i]}, true
		default:
			l.i++
			return token{tPunct, s[l.i-1 : l.i]}, true
		}
	}
	return token{}, false
}

func isWordByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c >= 0x80
}

// skipBlockComment skips a /* */ comment; ClickHouse allows them to nest.
func (l *lexer) skipBlockComment() {
	depth := 0
	for l.i < len(l.s) {
		switch {
		case strings.HasPrefix(l.s[l.i:], "/*"):
			depth++
			l.i += 2
		case strings.HasPrefix(l.s[l.i:], "*/"):
			depth--
			l.i += 2
			if depth == 0 {
				return
			}
		default:
			l.i++
		}
	}
}

// quoted reads a literal opened by q, handling backslash escapes and a
// doubled quote, and returns its unescaped text. A literal without escapes is
// returned as a substring, without allocating.
func (l *lexer) quoted(q byte) string {
	l.i++
	start := l.i
	var b *strings.Builder
	for l.i < len(l.s) {
		c := l.s[l.i]
		doubled := c == q && l.i+1 < len(l.s) && l.s[l.i+1] == q
		if c == q && !doubled {
			l.i++
			if b == nil {
				return l.s[start : l.i-1]
			}
			return b.String()
		}
		if c != '\\' && !doubled {
			if b != nil {
				b.WriteByte(c)
			}
			l.i++
			continue
		}
		if b == nil {
			b = &strings.Builder{}
			b.WriteString(l.s[start:l.i])
		}
		if doubled {
			b.WriteByte(q)
		} else if l.i+1 < len(l.s) {
			b.WriteByte(unescape(l.s[l.i+1]))
		}
		l.i += 2
	}
	if b != nil {
		return b.String()
	}
	return l.s[start:]
}

func unescape(c byte) byte {
	switch c {
	case 'n':
		return '\n'
	case 't':
		return '\t'
	case 'r':
		return '\r'
	case '0':
		return 0
	}
	return c
}

// heredocTag returns the $tag$ that opens a heredoc string at l.i.
func (l *lexer) heredocTag() (string, bool) {
	s := l.s[l.i+1:]
	for j := 0; j < len(s); j++ {
		if s[j] == '$' {
			return l.s[l.i : l.i+j+2], true
		}
		if !isWordByte(s[j]) {
			return "", false
		}
	}
	return "", false
}

func tokenize(sql string) []token {
	l := lexer{s: sql}
	toks := make([]token, 0, len(sql)/5+4)
	for {
		t, ok := l.next()
		if !ok {
			return toks
		}
		toks = append(toks, t)
	}
}
