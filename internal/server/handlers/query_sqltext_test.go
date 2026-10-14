package handlers

import (
	"reflect"
	"strings"
	"testing"
)

func TestTokenizeSQLRoundTrips(t *testing.T) {
	inputs := []string{
		"SELECT 'a''b', 'c\\'d', `e``f`, \"g\" -- h\n/* i /* j */ k */ $$l$$ $t$m$t$ # n\n#!o",
		"SELECT 'unterminated",
		"SELECT 1 /* open",
		"",
	}
	for _, in := range inputs {
		var b strings.Builder
		for _, tok := range tokenizeSQL(in) {
			b.WriteString(tok.text)
		}
		if b.String() != in {
			t.Errorf("tokens do not round-trip:\n got %q\nwant %q", b.String(), in)
		}
	}
}

func TestSplitSQLStatements(t *testing.T) {
	got := splitSQLStatements("SELECT ';' AS a; -- x;y\nSELECT `b;c` /* ; */;\n;  -- only a comment\n")
	want := []string{"SELECT ';' AS a", "-- x;y\nSELECT `b;c` /* ; */"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("splitSQLStatements = %q, want %q", got, want)
	}
}

func TestStartsWithExplain(t *testing.T) {
	cases := map[string]bool{
		"EXPLAIN SELECT 1":                  true,
		"  explain pipeline SELECT 1":       true,
		"/* note */ -- x\nEXPLAIN SELECT 1": true,
		"SELECT 'EXPLAIN'":                  false,
		"-- EXPLAIN\nSELECT 1":              false,
	}
	for in, want := range cases {
		if got := startsWithExplain(in); got != want {
			t.Errorf("startsWithExplain(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestFormatSQLFallbackKeepsMeaning(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "clauses on their own lines, keywords upper-cased",
			in:   "select a, b from t where x = 1 order by a limit 10",
			want: "SELECT a, b\nFROM t\nWHERE x = 1\nORDER BY a\nLIMIT 10",
		},
		{
			name: "string literals are untouched",
			in:   "select 'select from where' as s from t",
			want: "SELECT 'select from where' AS s\nFROM t",
		},
		{
			name: "comments are untouched and never swallow code",
			in:   "select a -- from here, where\nfrom t /* order by */ where b",
			want: "SELECT a -- from here, where\nFROM t /* order by */\nWHERE b",
		},
		{
			name: "identifiers that are keywords elsewhere keep their case",
			in:   "select end, table, set, t.from, x as from from t",
			want: "SELECT end, table, set, t.from, x AS from\nFROM t",
		},
		{
			name: "quoted identifiers are untouched",
			in:   "select `from`, \"where\" from t",
			want: "SELECT `from`, \"where\"\nFROM t",
		},
		{
			name: "function calls keep their case and inner FROM stays inline",
			in:   "select extract(day from d), in(1, x), not(y) from t",
			want: "SELECT extract(day FROM d), in(1, x), not(y)\nFROM t",
		},
		{
			name: "subqueries are indented",
			in:   "select * from (select a from t where a > 1) as s left join u on s.a = u.a",
			want: "SELECT *\nFROM (SELECT a\n    FROM t\n    WHERE a > 1) AS s\nLEFT JOIN u ON s.a = u.a",
		},
		{
			name: "group by only when followed by BY",
			in:   "select group from t group by group",
			want: "SELECT group\nFROM t\nGROUP BY group",
		},
		{
			name: "heredoc is untouched",
			in:   "select $$ from where $$ from t",
			want: "SELECT $$ from where $$\nFROM t",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := formatSQL(tt.in); got != tt.want {
				t.Errorf("formatSQL(%q)\n got: %q\nwant: %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestFormatSQLFallbackOnlyChangesWhitespaceAndKeywordCase(t *testing.T) {
	in := "with x as (select 1 as `a b`) select * from x -- c\n where 'it''s' != $t$;$t$ settings max_threads = 1; select 2"
	out := formatSQL(in)
	norm := func(s string) string {
		var b strings.Builder
		for _, tok := range tokenizeSQL(s) {
			switch tok.kind {
			case tokSpace:
				b.WriteString(" ")
			case tokWord:
				b.WriteString(strings.ToUpper(tok.text))
			default:
				b.WriteString(tok.text)
			}
		}
		return strings.Join(strings.Fields(b.String()), " ")
	}
	if norm(in) != norm(out) {
		t.Fatalf("formatter changed more than whitespace/case:\n in: %q\nout: %q", in, out)
	}
	// The line comment must still end before WHERE.
	if !strings.Contains(out, "-- c\n") {
		t.Fatalf("line comment lost its newline: %q", out)
	}
}

func TestStripTrailingComments(t *testing.T) {
	if got := stripTrailingComments("SELECT 1; -- done\n"); got != "SELECT 1;" {
		t.Fatalf("got %q", got)
	}
}

func TestTrimTrailingSQLNoise(t *testing.T) {
	for in, want := range map[string]string{
		"SELECT 1":                "SELECT 1",
		"SELECT 1; -- note":       "SELECT 1",
		"SELECT 1 # note\n":       "SELECT 1",
		"SELECT '--' /* c */ ;\n": "SELECT '--'",
		"SELECT 1 -- a\n+ 1 -- b": "SELECT 1 -- a\n+ 1",
	} {
		if got := trimTrailingSQLNoise(in); got != want {
			t.Errorf("trimTrailingSQLNoise(%q) = %q, want %q", in, got, want)
		}
	}
}
