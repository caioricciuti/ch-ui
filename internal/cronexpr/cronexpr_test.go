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

// Day-of-week 7 and the day-of-month/day-of-week OR rule follow Vixie cron.
func TestComputeNextRunVixieDays(t *testing.T) {
	// Thursday 2026-10-01 10:00 UTC.
	from := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	day := func(d int) time.Time { return time.Date(2026, 10, d, 9, 0, 0, 0, time.UTC) }
	cases := []struct {
		expr string
		want []time.Time
	}{
		// 7 is Sunday, alone, in a range and in a list.
		{"0 9 * * 7", []time.Time{day(4), day(11)}},
		{"0 9 * * 5-7", []time.Time{day(2), day(3), day(4), day(9)}},
		{"0 9 * * 6,7", []time.Time{day(3), day(4), day(10)}},
		{"0 9 * * 0", []time.Time{day(4), day(11)}},
		// Step over "*" is unchanged: Sun, Tue, Thu, Sat.
		{"0 9 * * */2", []time.Time{day(3), day(4), day(6), day(8)}},
		// Both day fields restricted: the 1st OR any Monday.
		{"0 9 1 * 1", []time.Time{day(5), day(12), day(19), day(26), time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 11, 2, 9, 0, 0, 0, time.UTC)}},
		{"0 9 15 * 7", []time.Time{day(4), day(11), day(15), day(18)}},
		// Only one restricted: unchanged.
		{"0 9 1 * *", []time.Time{time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC), time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)}},
		{"0 9 * * 1", []time.Time{day(5), day(12)}},
		// A "*/n" day field counts as unrestricted (it starts with '*'), so
		// the fields AND: odd days that are also Mondays.
		{"0 9 */2 * 1", []time.Time{day(5), day(19), time.Date(2026, 11, 9, 9, 0, 0, 0, time.UTC)}},
		{"0 9 1 * */1", []time.Time{time.Date(2026, 11, 1, 9, 0, 0, 0, time.UTC)}},
	}
	for _, c := range cases {
		at := from
		for i, want := range c.want {
			got := ComputeNextRun(c.expr, at)
			if got == nil || !got.Equal(want) {
				t.Errorf("%q run %d: got %v, want %v", c.expr, i, got, want)
				break
			}
			at = *got
		}
	}
}

func TestValidateCronDayFields(t *testing.T) {
	for _, ok := range []string{"0 0 * * 7", "0 9 * * 5-7", "0 9 1 * 1", "0 9 31 * 0-7"} {
		if !ValidateCron(ok) {
			t.Errorf("%q: want valid", ok)
		}
	}
	// Out-of-range values are dropped, so "0 9 32 * 1" has an empty
	// day-of-month and is invalid rather than "every Monday".
	for _, bad := range []string{"0 9 * * 8", "0 9 32 * *", "0 9 * 13 *", "0 9 32 * 1", "0 9 1 * 8"} {
		if ValidateCron(bad) {
			t.Errorf("%q: want invalid", bad)
		}
	}
}

// Wall-clock reading in a timezone across a DST change is unchanged.
func TestComputeNextRunInDST(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Lisbon")
	if err != nil {
		t.Skip("tzdata unavailable:", err)
	}
	// Lisbon leaves summer time on Sunday 2026-10-25 (UTC+1 -> UTC+0).
	from := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	got := ComputeNextRunIn("0 9 * * 7", from, loc)
	want := time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)
	if got == nil || !got.Equal(want) {
		t.Errorf("after DST: got %v, want %v", got, want)
	}
	got = ComputeNextRunIn("0 9 * * 6", time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC), loc)
	want = time.Date(2026, 10, 24, 8, 0, 0, 0, time.UTC)
	if got == nil || !got.Equal(want) {
		t.Errorf("before DST: got %v, want %v", got, want)
	}
}

func TestComputeNextRunInZone(t *testing.T) {
	from := time.Date(2026, 10, 1, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		tz   string
		want time.Time
	}{
		// 09:00 Madrid in summer time (UTC+2) is 07:00 UTC.
		{"Europe/Madrid", time.Date(2026, 10, 2, 7, 0, 0, 0, time.UTC)},
		{"UTC", time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)},
		{"", time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)},
		{"Not/AZone", time.Date(2026, 10, 2, 9, 0, 0, 0, time.UTC)},
	}
	for _, c := range cases {
		got := ComputeNextRunInZone("0 9 * * *", c.tz, from)
		if got == nil || !got.Equal(c.want) {
			t.Errorf("%q: got %v, want %v", c.tz, got, c.want)
		}
	}
}
