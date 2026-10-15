package mcpserver

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestReferencedTables(t *testing.T) {
	cases := []struct {
		sql    string
		params map[string]string
		want   []tableRef
	}{
		{"SELECT 1", nil, nil},
		{"SELECT * FROM a.t", nil, []tableRef{{"a", "t"}}},
		{"SELECT * FROM `my db`.\"my t\"", nil, []tableRef{{"my db", "my t"}}},
		{"SELECT * FROM `a.t`", nil, []tableRef{{"", "a.t"}}}, // one identifier with a dot: table in the current database
		{"SELECT * FROM a . t", nil, []tableRef{{"a", "t"}}},
		{"SELECT * FROM t", nil, []tableRef{{"", "t"}}},
		{"-- lead\n/* c */ SELECT * FROM a.t", nil, []tableRef{{"a", "t"}}},
		{"SELECT * FROM a.order, hr.s", nil, []tableRef{{"a", "order"}, {"hr", "s"}}},
		{"SELECT * FROM a.t AS order, hr.s", nil, []tableRef{{"a", "t"}, {"hr", "s"}}},
		{"SELECT * FROM a.t SAMPLE 0.1 OFFSET 0.5, hr.s", nil, []tableRef{{"a", "t"}, {"hr", "s"}}},
		{"SELECT * FROM a.t x JOIN a.u y ON x.id = y.id, hr.s", nil, []tableRef{{"a", "t"}, {"a", "u"}, {"hr", "s"}}},
		{"SELECT * FROM a.t JOIN a.u USING id, hr.s", nil, []tableRef{{"a", "t"}, {"a", "u"}, {"hr", "s"}}},
		{"SELECT * FROM (SELECT 1) AS x, hr.s", nil, []tableRef{{"hr", "s"}}},
		{"SELECT a, b FROM a.t WHERE c IN (1, 2) ORDER BY a, b LIMIT 10, 20", nil, []tableRef{{"a", "t"}}},
		{"SELECT * FROM a.t WHERE x GLOBAL NOT IN hr.s", nil, []tableRef{{"a", "t"}, {"hr", "s"}}},
		{"SELECT * FROM a.t WHERE x IN tuple(1, 2)", nil, []tableRef{{"a", "t"}}},
		{"SELECT arrayFilter(x -> x IN hr.s, arr) FROM a.t", nil, []tableRef{{"hr", "s"}, {"a", "t"}}},
		{"WITH x AS (SELECT * FROM hr.s) SELECT * FROM x", nil, []tableRef{{"hr", "s"}}},
		{"WITH x AS (SELECT 1), y AS (SELECT 2) SELECT * FROM x, y", nil, nil},
		// A CTE defined inside a subquery does not shadow a table outside it.
		{"SELECT * FROM (WITH s AS (SELECT 1) SELECT * FROM s), s", nil, []tableRef{{"", "s"}}},
		// WITH does not carry across UNION: the second x is a table.
		{"WITH x AS (SELECT 1) SELECT * FROM x UNION ALL SELECT * FROM x", nil, []tableRef{{"", "x"}}},
		{"WITH x AS (SELECT 1) SELECT * EXCEPT (a) FROM x", nil, nil},
		{"WITH hr.s AS x SELECT * FROM x", nil, []tableRef{{"hr", "s"}, {"", "x"}}},
		{"WITH 1 AS one SELECT one FROM a.t", nil, []tableRef{{"a", "t"}}},
		{"SELECT extract(YEAR FROM ts), position(s IN 'x'), trim(BOTH ' ' FROM s) FROM a.t", nil, []tableRef{{"a", "t"}}},
		// Only position's own IN separator and the FROM of extract / trim /
		// substring syntax are argument syntax; any other IN reads a table.
		{"SELECT position('a', 'b', 1 IN hr.s)", nil, []tableRef{{"hr", "s"}}},
		{"SELECT position(1 IN hr.s IN hr.u)", nil, []tableRef{{"hr", "u"}}},
		{"SELECT position(s IN 'x'), locate(1 IN hr.s)", nil, []tableRef{{"hr", "s"}}},
		{"SELECT extract(x, 1 IN hr.s), substring(s FROM 1 FOR 2), trim(LEADING 'a' FROM 1 IN hr.u)", nil, []tableRef{{"hr", "s"}, {"hr", "u"}}},
		{"SELECT extract(YEAR FROM (SELECT max(d) FROM hr.s))", nil, []tableRef{{"hr", "s"}}},
		{"SELECT a IS NOT DISTINCT FROM b FROM a.t", nil, []tableRef{{"a", "t"}}},
		{"SELECT * FROM a.t ARRAY JOIN arr AS x, arr2 AS y", nil, []tableRef{{"a", "t"}}},
		{"SELECT format('{}', x) FROM a.t, hr.s", nil, []tableRef{{"a", "t"}, {"hr", "s"}}},
		{"SELECT * FROM numbers(10) WHERE number IN (SELECT id FROM a.t)", nil, []tableRef{{"a", "t"}}},
		{"(SELECT 1 FROM a.t) UNION ALL (SELECT 2 FROM hr.s)", nil, []tableRef{{"a", "t"}, {"hr", "s"}}},
		{"SELECT 'FROM hr.s', \"x\" FROM a.t -- FROM hr.s", nil, []tableRef{{"a", "t"}}},
		{"SELECT 'it''s \\' FROM hr.s' FROM a.t", nil, []tableRef{{"a", "t"}}},
		{"SELECT * FROM {t:Identifier}", map[string]string{"param_t": "hr.s"}, []tableRef{{"hr", "s"}}},
		{"SELECT * FROM {d:Identifier}.{t:Identifier}", map[string]string{"param_d": "hr", "param_t": "s"}, []tableRef{{"hr", "s"}}},
		{"SELECT * FROM a.t WHERE d = {d:Date}", nil, []tableRef{{"a", "t"}}},
	}
	for _, tc := range cases {
		got, err := referencedTables(tc.sql, tc.params)
		if err != nil {
			t.Errorf("%q: unexpected error %v", tc.sql, err)
			continue
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("%q:\n got  %v\n want %v", tc.sql, got, tc.want)
		}
	}
}

// Everything the analysis cannot classify with confidence is an error.
func TestReferencedTablesFailsClosed(t *testing.T) {
	cases := []struct {
		sql    string
		params map[string]string
		want   string
	}{
		{"SHOW TABLES FROM hr", nil, "SELECT / WITH"},
		{"EXPLAIN SELECT * FROM hr.s", nil, "SELECT / WITH"},
		{"SELECT * FROM url('http://x/y.csv', CSV)", nil, "url()"},
		{"SELECT * FROM remote('h', hr.s)", nil, "remote()"},
		{"SELECT * FROM clusterAllReplicas('c', hr.s)", nil, "clusterAllReplicas()"},
		{"SELECT * FROM merge('hr', '.*')", nil, "merge()"},
		{"SELECT * FROM view(SELECT * FROM hr.s)", nil, "view()"},
		{"SELECT * FROM a.t, `url`('http://x')", nil, "url()"},
		{"SELECT dictGetString('hr.d', 'a', 1)", nil, "dictGetString()"},
		{"SELECT `dictGet`('hr.d', 'a', 1)", nil, "dictGet()"},
		{"SELECT joinGetOrNull('hr.j', 'v', 1)", nil, "joinGetOrNull()"},
		{"SELECT hasColumnInTable('hr', 's', 'salary')", nil, "hasColumnInTable()"},
		// ClickHouse nests block comments; a lexer that does not would see
		// the UNION as a string literal.
		{"SELECT 1 FROM a.t WHERE 1 = /* /* */ ' */ 1 UNION ALL SELECT * FROM hr.s -- '", nil, "nested"},
		{"SELECT 1 FROM a.t WHERE x = $$'$$ UNION ALL SELECT * FROM hr.s --'", nil, `'$'`},
		{"SELECT 1 # comment\nFROM a.t", nil, `'#'`},
		{"SELECT 1 FROM a.t WHERE x = \u2018'\u2019 UNION ALL SELECT * FROM hr.s -- '", nil, "non-ASCII"},
		{"SELECT 'unterminated FROM a.t", nil, "unterminated"},
		{"SELECT * FROM a.b.c", nil, "could not resolve"},
		{"SELECT * FROM a.", nil, "could not resolve"},
		{"SELECT * FROM 'x'", nil, "could not tell"},
		{"SELECT * FROM {t:String}", nil, "{name:Identifier}"},
		{"SELECT * FROM {t:Identifier}", nil, `parameter "t"`},
		{"SELECT * FROM {t:Identifier}", map[string]string{"param_t": "a.b.c"}, `parameter "t"`},
		{"SELECT (1", nil, "unbalanced"},
		{"SELECT 1)", nil, "unbalanced"},
	}
	for _, tc := range cases {
		_, err := referencedTables(tc.sql, tc.params)
		var u errUnclear
		if !errors.As(err, &u) {
			t.Errorf("%q: want errUnclear, got %v", tc.sql, err)
			continue
		}
		if !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%q: error %q should mention %s", tc.sql, err, tc.want)
		}
	}
}

func TestCheckAllowedDatabases(t *testing.T) {
	lookups := 0
	current := func() (string, error) { lookups++; return "default", nil }

	if err := checkAllowedDatabases(current, "", "SELECT * FROM hr.s", nil); err != nil {
		t.Errorf("no allow-list: %v", err)
	}
	if err := checkAllowedDatabases(current, " analytics , ", "SELECT * FROM analytics.t JOIN system.one ON 1", nil); err != nil {
		t.Errorf("allowed: %v", err)
	}
	if err := checkAllowedDatabases(current, "analytics", "SELECT * FROM hr.s", nil); err == nil || !strings.Contains(err.Error(), `database "hr"`) || !strings.Contains(err.Error(), "allowed: analytics") {
		t.Errorf("hr must be named in the rejection, got %v", err)
	}
	if err := checkAllowedDatabases(current, "analytics", "SELECT * FROM system.tables", nil); err == nil {
		t.Error("system tables other than one/numbers/zeros are outside the allow-list")
	}
	if lookups != 0 {
		t.Errorf("qualified names must not look up the current database, did %d times", lookups)
	}
	if err := checkAllowedDatabases(current, "analytics", "SELECT * FROM t1, t2", nil); err == nil || !strings.Contains(err.Error(), `database "default"`) {
		t.Errorf("unqualified table resolves to the current database, got %v", err)
	}
	if lookups != 1 {
		t.Errorf("current database looked up %d times, want once per query", lookups)
	}
	if err := checkAllowedDatabases(current, "analytics,default", "SELECT * FROM t1", nil); err != nil {
		t.Errorf("current database allowed: %v", err)
	}
	failing := func() (string, error) { return "", errors.New("offline") }
	if err := checkAllowedDatabases(failing, "analytics", "SELECT * FROM t1", nil); err == nil || !strings.Contains(err.Error(), "qualify it") {
		t.Errorf("unknown current database must reject, got %v", err)
	}
}

func TestSettingsClause(t *testing.T) {
	for sql, want := range map[string]bool{
		"SELECT 1 SETTINGS max_result_rows = 0":            true,
		"select 1 settings readonly=0, max_threads=1":      true,
		"SELECT * FROM t SETTINGS\n\tmax_execution_time=0": true,
		"SELECT 1 AS settings":                             false,
		"SELECT 'SETTINGS a = 1'":                          false,
		"SELECT 1 -- SETTINGS a = 1":                       false,
		"SELECT settings FROM t WHERE settings = 1":        false,
	} {
		if got := settingsClause(sql); got != want {
			t.Errorf("settingsClause(%q) = %v, want %v", sql, got, want)
		}
	}
}

func TestQuoteIdentEscapesBackslash(t *testing.T) {
	cases := map[string]string{
		"plain":  "`plain`",
		"a`b":    "`a\\`b`",
		`a\`:     "`a\\\\`",
		"a\\`b":  "`a\\\\\\`b`",
		"x'y\"z": "`x'y\"z`",
	}
	for in, want := range cases {
		if got := quoteIdent(in); got != want {
			t.Errorf("quoteIdent(%q) = %s, want %s", in, got, want)
		}
		// Round trip through the tokenizer, which unescapes like ClickHouse.
		toks, err := tokenizeSQL(quoteIdent(in), true)
		if err != nil || len(toks) != 1 || toks[0].text != in {
			t.Errorf("quoteIdent(%q) does not read back as one identifier: %v %v", in, toks, err)
		}
	}
}
