package connector

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// A minimal ClickHouse SQL scanner: enough to tell code from strings, quoted
// identifiers, heredocs and comments (--, "# " / "#!", nested /* */), so the
// statement kind is read from code and trailing comments are found without
// being fooled by a "--" inside a string.

// noiseEnd returns the offset just past the token or comment at sql[i], and
// whether it is code (not whitespace or a comment).
func noiseEnd(sql string, i int) (int, bool) {
	c := sql[i]
	var next byte
	if i+1 < len(sql) {
		next = sql[i+1]
	}
	switch {
	case c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v':
		return i + 1, false
	case c == '-' && next == '-', c == '#' && (next == ' ' || next == '!'):
		if nl := strings.IndexByte(sql[i:], '\n'); nl >= 0 {
			return i + nl, false
		}
		return len(sql), false
	case c == '/' && next == '*':
		depth := 0
		for j := i; j < len(sql); {
			switch {
			case strings.HasPrefix(sql[j:], "/*"):
				depth++
				j += 2
			case strings.HasPrefix(sql[j:], "*/"):
				depth--
				j += 2
				if depth == 0 {
					return j, false
				}
			default:
				j++
			}
		}
		return len(sql), false
	case c == '\'' || c == '"' || c == '`':
		for j := i + 1; j < len(sql); j++ {
			switch sql[j] {
			case '\\':
				j++
			case c:
				if j+1 < len(sql) && sql[j+1] == c {
					j++
					continue
				}
				return j + 1, true
			}
		}
		return len(sql), true
	case c == '$':
		if tag := heredocOpenRe.FindString(sql[i:]); tag != "" {
			if end := strings.Index(sql[i+len(tag):], tag); end >= 0 {
				return i + len(tag) + end + len(tag), true
			}
			return len(sql), true
		}
	}
	return i + 1, true
}

var heredocOpenRe = regexp.MustCompile(`^\$([A-Za-z_][A-Za-z0-9_]*)?\$`)

// stripLeadingComments removes leading whitespace, semicolons and comments of
// every kind, so the statement kind is found however the query starts.
func stripLeadingComments(query string) string {
	for i := 0; i < len(query); {
		end, code := noiseEnd(query, i)
		if code && query[i] != ';' {
			return query[i:]
		}
		i = end
	}
	return ""
}

// trimTrailingNoise drops trailing whitespace, semicolons and comments, so
// "SELECT 1; -- note" becomes "SELECT 1" and a clause appended after it is
// neither swallowed by the comment nor turned into a second statement.
func trimTrailingNoise(query string) string {
	last := 0
	for i := 0; i < len(query); {
		end, code := noiseEnd(query, i)
		if code && query[i] != ';' {
			last = end
		}
		i = end
	}
	return query[:last]
}

// metaProbe says how ExecuteStreaming reads a statement's columns before
// running it.
type metaProbe int

const (
	// probeNone: the statement is never run twice. It may change data
	// (REPLACE, BACKUP, CREATE ... AS SELECT, ...) or be unknown.
	probeNone metaProbe = iota
	// probeWrap: a query that can live in SELECT * FROM (...) LIMIT 0.
	probeWrap
	// probeIntrospect: a read-only statement that cannot be wrapped; it gets
	// a textual LIMIT 0, or runs as is when it rejects one.
	probeIntrospect
)

var introspectionKeywords = map[string]bool{"SHOW": true, "DESC": true, "DESCRIBE": true, "EXISTS": true, "EXPLAIN": true}

// metaProbeFor classifies a statement by its first keyword. Only statements
// that certainly read nothing but a result are probed; everything else runs once.
func metaProbeFor(query string) metaProbe {
	q := stripLeadingComments(query)
	if strings.HasPrefix(q, "(") {
		return probeWrap
	}
	end := 0
	for end < len(q) && (q[end] == '_' || q[end] >= 'a' && q[end] <= 'z' || q[end] >= 'A' && q[end] <= 'Z' || q[end] >= '0' && q[end] <= '9') {
		end++
	}
	switch word := strings.ToUpper(q[:end]); {
	case word == "SELECT" || word == "WITH":
		return probeWrap
	case introspectionKeywords[word]:
		return probeIntrospect
	}
	return probeNone
}

// metaWrapPrefix opens the subquery buildMetaQuery wraps a query in. ClickHouse
// error positions in the wrapped probe are shifted by its length.
const metaWrapPrefix = "SELECT * FROM (\n"

// syntaxPositionRe finds the top-level syntax error position and the line/col
// that may follow it.
var syntaxPositionRe = regexp.MustCompile(`Syntax error: failed at position (\d+)((?:\s*\((?:'(?:[^'\\]|\\.)*'|[^()]*)\))?\s*\(line (\d+), col (\d+)\))?`)

// unwrapErrorPosition rewrites the syntax error position of a wrapped meta
// probe so it indexes the user's query again: the position moves back by the
// prefix length and the line by one. Errors relayed from remote shards index
// another query and are left alone.
func unwrapErrorPosition(msg string) string {
	loc := syntaxPositionRe.FindStringSubmatchIndex(msg)
	if loc == nil || strings.Contains(msg[:loc[0]], "Received from") {
		return msg
	}
	pos, _ := strconv.Atoi(msg[loc[2]:loc[3]])
	pos -= len(metaWrapPrefix)
	if pos < 1 {
		pos = 1
	}
	var b strings.Builder
	b.WriteString(msg[:loc[2]])
	b.WriteString(strconv.Itoa(pos))
	if loc[6] >= 0 {
		line, _ := strconv.Atoi(msg[loc[6]:loc[7]])
		if line > 1 {
			line--
		}
		b.WriteString(msg[loc[3]:loc[6]])
		b.WriteString(strconv.Itoa(line))
		b.WriteString(msg[loc[7]:])
	} else {
		b.WriteString(msg[loc[3]:])
	}
	return b.String()
}

// metaProbeError reports a failed meta probe; positions of a wrapped probe are
// mapped back to the user's query.
func metaProbeError(err error, wrapped bool) error {
	if wrapped {
		return fmt.Errorf("metadata query failed: %s", unwrapErrorPosition(err.Error()))
	}
	return fmt.Errorf("metadata query failed: %w", err)
}
