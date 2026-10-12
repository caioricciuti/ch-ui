package models

import (
	"reflect"
	"testing"
)

func TestStripSQLCommentsKeepsLiterals(t *testing.T) {
	cases := []struct{ in, want string }{
		{"SELECT 1 -- note\nFROM t", "SELECT 1 \nFROM t"},
		{"SELECT /* a */ 1", "SELECT   1"},
		{"SELECT * FROM t WHERE s LIKE '%--%'", "SELECT * FROM t WHERE s LIKE '%--%'"},
		{"SELECT '/* not a comment */' AS x", "SELECT '/* not a comment */' AS x"},
		{"SELECT 'it''s -- fine' -- gone", "SELECT 'it''s -- fine' "},
		{`SELECT 'a\'b -- still' -- gone`, `SELECT 'a\'b -- still' `},
		{"SELECT `col--x`, \"y/*z*/\" FROM t", "SELECT `col--x`, \"y/*z*/\" FROM t"},
		{"SELECT 'unterminated -- x", "SELECT 'unterminated -- x"},
		{"SELECT 1 /* unterminated", "SELECT 1  "},
	}
	for _, c := range cases {
		if got := stripSQLComments(c.in); got != c.want {
			t.Errorf("stripSQLComments(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestExtractRefsIgnoresCommentsAndLiterals(t *testing.T) {
	sql := "SELECT * FROM $ref(a) -- $ref(b)\n/* $ref(c) */ JOIN $ref( d ) USING id WHERE x = '$ref(e)' AND `$ref(f)` = 1 UNION ALL SELECT * FROM $ref(a)"
	if got, want := ExtractRefs(sql), []string{"a", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("ExtractRefs = %v, want %v", got, want)
	}
}

func TestResolveRefsKeepsTheUsersSQL(t *testing.T) {
	sql := "SELECT * FROM $ref(src) WHERE s LIKE '%--%' -- trailing $ref(nope)\nAND t = '$ref(src)'"
	got, err := ResolveRefs(sql, map[string]string{"src": "analytics"})
	if err != nil {
		t.Fatal(err)
	}
	want := "SELECT * FROM `analytics`.`src` WHERE s LIKE '%--%' -- trailing $ref(nope)\nAND t = '$ref(src)'"
	if got != want {
		t.Fatalf("ResolveRefs =\n%q\nwant\n%q", got, want)
	}
	if _, err := ResolveRefs("SELECT * FROM $ref(missing)", nil); err == nil {
		t.Fatal("an unknown $ref must fail")
	}
}
