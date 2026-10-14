package queryproc

import (
	"strings"
	"testing"
)

func TestSubstituteVars(t *testing.T) {
	vars := map[string][]string{
		"db":      {"system"},
		"tables":  {"parts", "tables"},
		"none":    {},
		"limit":   {"10"},
		"bucket":  {"5m"},
		"evil":    {`x') OR 1=1 --`},
		"slash":   {`a\'b`},
		"dbname2": {"other"},
	}
	types := map[string]string{"bucket": VarTypeInterval}
	tests := []struct {
		name, in, want string
	}{
		{"plain", "SELECT * FROM t WHERE db = $db", "SELECT * FROM t WHERE db = 'system'"},
		{"braced", "WHERE db = ${db}", "WHERE db = 'system'"},
		{"multi in", "WHERE name IN (${tables})", "WHERE name IN ('parts', 'tables')"},
		{"empty multi", "WHERE name IN ($none)", "WHERE name IN (NULL)"},
		{"number", "LIMIT ${limit:number}", "LIMIT 10"},
		{"interval", "INTERVAL $bucket second", "INTERVAL 300 second"},
		{"word bounded", "SELECT $dbname2, $db_x, $db", "SELECT 'other', $db_x, 'system'"},
		{"injection", "WHERE a = $evil", `WHERE a = 'x'') OR 1=1 --'`},
		{"backslash", "WHERE a = $slash", `WHERE a = 'a\\''b'`},
		{"comment untouched", "SELECT 1 -- $db\n, $db", "SELECT 1 -- $db\n, 'system'"},
		{"block comment untouched", "SELECT /* $db */ $db", "SELECT /* $db */ 'system'"},
		{"identifier untouched", "SELECT `$db`, \"$db\"", "SELECT `$db`, \"$db\""},
		{"heredoc untouched", "SELECT $$ $db $$, $db", "SELECT $$ $db $$, 'system'"},
		{"unknown left", "SELECT $other", "SELECT $other"},
		{"macro left", "SELECT $__interval", "SELECT $__interval"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := SubstituteVars(tc.in, vars, types)
			if err != nil {
				t.Fatal(err)
			}
			if got != tc.want {
				t.Fatalf("got  %q\nwant %q", got, tc.want)
			}
		})
	}

	for name, in := range map[string]string{
		"raw":            "SELECT ${db:raw}",
		"not a number":   "LIMIT ${db:number}",
		"inside quotes":  "WHERE a = '$db'",
		"unknown format": "SELECT ${db:csv}",
	} {
		t.Run("error "+name, func(t *testing.T) {
			if _, err := SubstituteVars(in, vars, types); err == nil {
				t.Fatalf("expected an error for %q", in)
			}
		})
	}
}

func TestProcessQueryVariablesSubstitutesAfterMacros(t *testing.T) {
	res := ProcessQueryVariables(ProcessorOptions{
		Query:     "SELECT count() FROM system.query_log WHERE $__timeFilter(event_time) AND user IN (${users})",
		TimeRange: &TimeRange{Type: "relative", From: "1h", To: "now"},
		Vars:      map[string][]string{"users": {"default", "o'brien"}},
	})
	if len(res.Errors) > 0 {
		t.Fatal(res.Errors)
	}
	if !strings.Contains(res.Query, "user IN ('default', 'o''brien')") || strings.Contains(res.Query, "$") {
		t.Fatalf("unexpected query: %s", res.Query)
	}
}

func TestIntervalSeconds(t *testing.T) {
	for in, want := range map[string]int64{"30s": 30, "5m": 300, "1h": 3600, "1d": 86400, "2w": 1209600, "60": 60} {
		if got, ok := IntervalSeconds(in); !ok || got != want {
			t.Fatalf("%s: got %d %v", in, got, ok)
		}
	}
	for _, bad := range []string{"", "0", "1y", "-5m", "5 OR 1"} {
		if _, ok := IntervalSeconds(bad); ok {
			t.Fatalf("%q must not parse", bad)
		}
	}
}

func TestValidVarName(t *testing.T) {
	for _, ok := range []string{"db", "Table_1", "_x"} {
		if !ValidVarName(ok) {
			t.Fatalf("%q should be valid", ok)
		}
	}
	for _, bad := range []string{"", "1x", "__interval", "a-b", "a b"} {
		if ValidVarName(bad) {
			t.Fatalf("%q should be invalid", bad)
		}
	}
}

func TestSubstituteVarsSubquery(t *testing.T) {
	sub := "SELECT `host` FROM (\nSELECT DISTINCT host FROM logs\n)"
	vars := map[string][]string{"host": {sub}}
	types := map[string]string{"host": VarTypeSubquery}
	for in, want := range map[string]string{
		"WHERE host IN ($host)":            "WHERE host IN (" + sub + ")",
		"WHERE host not in ( ${host} )":    "WHERE host not in ( " + sub + " )",
		"WHERE host GLOBAL IN (\n$host\n)": "WHERE host GLOBAL IN (\n" + sub + "\n)",
	} {
		got, err := SubstituteVars(in, vars, types)
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		if got != want {
			t.Fatalf("got  %q\nwant %q", got, want)
		}
	}
	for _, in := range []string{
		"WHERE host = $host",
		"WHERE host IN ($host, 'x')",
		"WHERE host IN ('x', $host)",
		"WHERE has([$host], host)",
		"WHERE host IN (${host:number})",
		"WHERE login($host)",
		"SELECT $host",
	} {
		if _, err := SubstituteVars(in, vars, types); err == nil || !strings.Contains(err.Error(), "IN ($host)") {
			t.Fatalf("%q: expected the IN-only error, got %v", in, err)
		}
	}
}
