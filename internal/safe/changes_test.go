package safe

import "testing"

func TestChangesReportsOnlyChanges(t *testing.T) {
	var c Changes
	steps := []struct {
		key, condition string
		want           bool
	}{
		{"a", "", false},
		{"a", "no account", true},
		{"a", "no account", false},
		{"b", "no account", true},
		{"a", "disabled", true},
		{"a", "", true},
		{"a", "", false},
		{"b", "no account", false},
	}
	for i, s := range steps {
		if got := c.Set(s.key, s.condition); got != s.want {
			t.Fatalf("step %d Set(%q, %q) = %v, want %v", i, s.key, s.condition, got, s.want)
		}
	}
}
