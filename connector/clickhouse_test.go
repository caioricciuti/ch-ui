package connector

import "testing"

func TestBuildMetaQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		// SELECT / WITH / parenthesized statements are wrapped in an outer subquery,
		// which is immune to LIMIT expressions, negative limits, WITH TIES and
		// awkward whitespace/comments.
		{
			name:  "select with positive limit wraps",
			query: "SELECT * FROM t LIMIT 10",
			want:  "SELECT * FROM (\nSELECT * FROM t LIMIT 10\n) LIMIT 0",
		},
		{
			name:  "select with negative limit wraps",
			query: "SELECT * FROM t LIMIT -10",
			want:  "SELECT * FROM (\nSELECT * FROM t LIMIT -10\n) LIMIT 0",
		},
		{
			name:  "select comma form with negative count wraps",
			query: "SELECT * FROM t LIMIT 5, -3",
			want:  "SELECT * FROM (\nSELECT * FROM t LIMIT 5, -3\n) LIMIT 0",
		},
		{
			name:  "select limit with offset wraps",
			query: "SELECT * FROM t LIMIT 10 OFFSET 20",
			want:  "SELECT * FROM (\nSELECT * FROM t LIMIT 10 OFFSET 20\n) LIMIT 0",
		},
		{
			name:  "select no limit wraps",
			query: "SELECT * FROM t",
			want:  "SELECT * FROM (\nSELECT * FROM t\n) LIMIT 0",
		},
		{
			name:  "select with LIMIT expression wraps (was missed by regex)",
			query: "SELECT * FROM t LIMIT 10*2",
			want:  "SELECT * FROM (\nSELECT * FROM t LIMIT 10*2\n) LIMIT 0",
		},
		{
			name:  "select WITH TIES wraps (was missed by regex)",
			query: "SELECT * FROM t ORDER BY x LIMIT 10 WITH TIES",
			want:  "SELECT * FROM (\nSELECT * FROM t ORDER BY x LIMIT 10 WITH TIES\n) LIMIT 0",
		},
		{
			name:  "WITH clause wraps",
			query: "WITH x AS (SELECT 1) SELECT * FROM x",
			want:  "SELECT * FROM (\nWITH x AS (SELECT 1) SELECT * FROM x\n) LIMIT 0",
		},
		{
			name:  "parenthesized query wraps",
			query: "(SELECT * FROM t)",
			want:  "SELECT * FROM (\n(SELECT * FROM t)\n) LIMIT 0",
		},
		{
			name:  "trailing semicolon stripped before wrapping",
			query: "SELECT * FROM t LIMIT -1;",
			want:  "SELECT * FROM (\nSELECT * FROM t LIMIT -1\n) LIMIT 0",
		},
		{
			name:  "trailing line comment within a select is safe to wrap",
			query: "SELECT * FROM t -- comment",
			want:  "SELECT * FROM (\nSELECT * FROM t -- comment\n) LIMIT 0",
		},
		{
			name:  "leading comment still detected as SELECT",
			query: "-- a note\nSELECT * FROM t LIMIT -3",
			want:  "SELECT * FROM (\n-- a note\nSELECT * FROM t LIMIT -3\n) LIMIT 0",
		},

		// Statements that cannot live inside a subquery keep the textual rewrite path.
		{
			name:  "show tables keeps rewrite path",
			query: "SHOW TABLES",
			want:  "SHOW TABLES\nLIMIT 0",
		},
		{
			name:  "show tables with negative limit rewrites via regex",
			query: "SHOW TABLES LIMIT -5",
			want:  "SHOW TABLES LIMIT 0",
		},
		{
			name:  "describe keeps rewrite path",
			query: "DESCRIBE TABLE t",
			want:  "DESCRIBE TABLE t\nLIMIT 0",
		},
		{
			name:  "set statement keeps rewrite path",
			query: "SET max_threads = 4",
			want:  "SET max_threads = 4\nLIMIT 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildMetaQuery(tt.query); got != tt.want {
				t.Errorf("buildMetaQuery(%q)\n got: %q\nwant: %q", tt.query, got, tt.want)
			}
		})
	}
}
