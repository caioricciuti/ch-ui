package connector

import "testing"

func TestBuildMetaQuery(t *testing.T) {
	tests := []struct {
		name  string
		query string
		want  string
	}{
		{
			name:  "positive limit",
			query: "SELECT * FROM t LIMIT 10",
			want:  "SELECT * FROM t LIMIT 0",
		},
		{
			name:  "negative limit",
			query: "SELECT * FROM t LIMIT -10",
			want:  "SELECT * FROM t LIMIT 0",
		},
		{
			name:  "comma form with negative count",
			query: "SELECT * FROM t LIMIT 5, -3",
			want:  "SELECT * FROM t LIMIT 0",
		},
		{
			name:  "limit with offset",
			query: "SELECT * FROM t LIMIT 10 OFFSET 20",
			want:  "SELECT * FROM t LIMIT 0",
		},
		{
			name:  "negative limit with offset",
			query: "SELECT * FROM t LIMIT -10 OFFSET 5",
			want:  "SELECT * FROM t LIMIT 0",
		},
		{
			name:  "no limit appends one",
			query: "SELECT * FROM t",
			want:  "SELECT * FROM t\nLIMIT 0",
		},
		{
			name:  "trailing semicolon stripped",
			query: "SELECT * FROM t LIMIT -1;",
			want:  "SELECT * FROM t LIMIT 0",
		},
		{
			name:  "trailing line comment gets newline",
			query: "SELECT * FROM t -- comment",
			want:  "SELECT * FROM t -- comment\nLIMIT 0",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildMetaQuery(tt.query); got != tt.want {
				t.Errorf("buildMetaQuery(%q) = %q, want %q", tt.query, got, tt.want)
			}
		})
	}
}
