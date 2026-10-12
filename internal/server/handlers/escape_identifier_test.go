package handlers

import "testing"

func TestEscapeIdentifierEscapesBackslash(t *testing.T) {
	for in, want := range map[string]string{
		"plain":  "`plain`",
		"we`ird": "`we``ird`",
		`tail\`:  "`tail\\\\`",
		"a\\`b":  "`a\\\\``b`",
	} {
		if got := escapeIdentifier(in); got != want {
			t.Errorf("escapeIdentifier(%q) = %q, want %q", in, got, want)
		}
	}
}
