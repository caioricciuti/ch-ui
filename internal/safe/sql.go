package safe

import "strings"

// Identifier wraps a ClickHouse identifier in backticks. Backslashes are
// escaped first: inside backticks a backslash escapes the next character, so a
// name ending in \ would otherwise swallow the closing backtick. Inner
// backticks are doubled.
func Identifier(name string) string {
	escaped := strings.ReplaceAll(name, `\`, `\\`)
	escaped = strings.ReplaceAll(escaped, "`", "``")
	return "`" + escaped + "`"
}

// Literal escapes backslashes and single quotes for the body of a ClickHouse
// string literal. The caller adds the surrounding quotes.
func Literal(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), "'", "''")
}

// QuotedLiteral returns value as a complete single-quoted string literal.
func QuotedLiteral(value string) string {
	return "'" + Literal(value) + "'"
}
