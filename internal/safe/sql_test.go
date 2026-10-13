package safe

import "testing"

func TestIdentifierEscapesBackslash(t *testing.T) {
	for in, want := range map[string]string{
		"plain":  "`plain`",
		"we`ird": "`we``ird`",
		`tail\`:  "`tail\\\\`",
		"a\\`b":  "`a\\\\``b`",
		"db.tbl": "`db.tbl`",
		"rôle":   "`rôle`",
	} {
		if got := Identifier(in); got != want {
			t.Errorf("Identifier(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLiteralEscapes(t *testing.T) {
	for in, want := range map[string]string{
		"plain": "plain",
		"it's":  "it''s",
		`a\b`:   `a\\b`,
		`\'`:    `\\''`,
	} {
		if got := Literal(in); got != want {
			t.Errorf("Literal(%q) = %q, want %q", in, got, want)
		}
	}
	if got := QuotedLiteral("x'y"); got != "'x''y'" {
		t.Errorf("QuotedLiteral = %q", got)
	}
}
