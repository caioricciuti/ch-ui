package models

import "testing"

func TestRenameInSelector(t *testing.T) {
	for _, c := range []struct {
		in, want string
		changed  bool
	}{
		{"orders", "sales", true},
		{"+orders+", "+sales+", true},
		{"raw, +orders , orders_daily+", "raw,+sales,orders_daily+", true},
		{"orders_daily+", "orders_daily+", false},
		{"*", "*", false},
		{"a-b", "a-b", false},
	} {
		got, changed := RenameInSelector(c.in, "orders", "sales")
		if got != c.want || changed != c.changed {
			t.Errorf("RenameInSelector(%q) = %q, %v; want %q, %v", c.in, got, changed, c.want, c.changed)
		}
	}
}
