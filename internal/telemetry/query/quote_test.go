package query

import "testing"

func TestQuoteIdentEscapesBackslashThenBacktick(t *testing.T) {
	for in, want := range map[string]string{
		"Body":   "`Body`",
		"a`b":    "`a\\`b`",
		`a\`:     "`a\\\\`",
		"a\\`b":  "`a\\\\\\`b`",
		"x'y\"z": "`x'y\"z`",
	} {
		if got := QuoteIdent(in); got != want {
			t.Errorf("QuoteIdent(%q) = %s, want %s", in, got, want)
		}
	}
	// A search key ending in a backslash must not close the identifier early.
	if got, want := JSONPathAccess("`LogAttributes`", `k\.x`), "toString(`LogAttributes`.`k\\\\`.`x`)"; got != want {
		t.Errorf("JSONPathAccess = %s, want %s", got, want)
	}
}
