package cronexpr

import (
	"testing"
	"time"
)

func TestComputeNextRun(t *testing.T) {
	from := time.Date(2026, 9, 29, 10, 7, 30, 0, time.UTC)
	cases := map[string]time.Time{
		"*/15 * * * *": time.Date(2026, 9, 29, 10, 15, 0, 0, time.UTC),
		"0 9 * * *":    time.Date(2026, 9, 30, 9, 0, 0, 0, time.UTC),
		"30 8 * * 1":   time.Date(2026, 10, 5, 8, 30, 0, 0, time.UTC), // next Monday
		"0 0 1 1 *":    time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
	}
	for expr, want := range cases {
		got := ComputeNextRun(expr, from)
		if got == nil || !got.Equal(want) {
			t.Errorf("%q: got %v, want %v", expr, got, want)
		}
	}
	for _, bad := range []string{"", "* * * *", "61 * * * *", "nonsense here now at all"} {
		if ValidateCron(bad) {
			t.Errorf("%q: want invalid", bad)
		}
	}
}
