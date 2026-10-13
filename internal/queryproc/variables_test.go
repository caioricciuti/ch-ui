package queryproc

import (
	"strings"
	"testing"
	"time"
)

func TestParseRelativeTime_NowMinusMinutes(t *testing.T) {
	base := time.Date(2026, 2, 12, 12, 0, 0, 0, time.UTC)

	cases := []string{"now-5m", "now-5min", "5m", "5minutes"}
	for _, tc := range cases {
		got := parseRelativeTime(tc, base)
		want := base.Add(-5 * time.Minute)
		if got.Unix() != want.Unix() {
			t.Fatalf("%s: expected %v, got %v", tc, want, got)
		}
	}
}

func TestGetTimeBounds_RelativeRangeWithCustomTo(t *testing.T) {
	from, to, ok := getTimeBounds(&TimeRange{
		Type: "relative",
		From: "now-15m",
		To:   "now-5m",
	})
	if !ok {
		t.Fatalf("expected valid range")
	}
	if !from.Before(to) {
		t.Fatalf("expected from < to, got from=%v to=%v", from, to)
	}
}

func TestProcessQueryVariables_TimestampMacroWithRelativeExpression(t *testing.T) {
	out := ProcessQueryVariables(ProcessorOptions{
		Query: "SELECT count() FROM x WHERE $__timestamp(event_time)",
		TimeRange: &TimeRange{
			Type: "relative",
			From: "now-1h",
			To:   "now",
		},
	})

	if len(out.Errors) > 0 {
		t.Fatalf("unexpected errors: %+v", out.Errors)
	}
	if strings.Contains(out.Query, "$__timestamp") {
		t.Fatalf("timestamp macro was not replaced: %s", out.Query)
	}
}

func absRange() *TimeRange {
	return &TimeRange{Type: "absolute", From: "2026-01-01T00:00:00Z", To: "2026-01-02T00:00:00Z"}
}

func process(t *testing.T, opts ProcessorOptions) string {
	t.Helper()
	out := ProcessQueryVariables(opts)
	if len(out.Errors) > 0 {
		t.Fatalf("unexpected errors: %+v", out.Errors)
	}
	return out.Query
}

func TestTimeFilter_UnknownTypeMatchesSecondsOrMillis(t *testing.T) {
	got := process(t, ProcessorOptions{Query: "WHERE $__timeFilter(event_time)", TimeRange: absRange()})
	want := "WHERE ((event_time >= 1767225600 AND event_time <= 1767312000) OR (event_time >= 1767225600000 AND event_time <= 1767312000000))"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestTimeFilter_ExplicitUnits(t *testing.T) {
	cases := map[string]string{
		"$__timeFilter(ts, s)":        "(ts >= 1767225600 AND ts <= 1767312000)",
		"$__timeFilter(ts, ms)":       "(ts >= 1767225600000 AND ts <= 1767312000000)",
		"$__timeFilter( ts , MS )":    "(ts >= 1767225600000 AND ts <= 1767312000000)",
		"$__timeFilter(ts, datetime)": "(ts >= fromUnixTimestamp64Milli(1767225600000) AND ts <= fromUnixTimestamp64Milli(1767312000000))",
		"$__timeFilter(ts, date)":     "(ts >= fromUnixTimestamp64Milli(1767225600000) AND ts <= fromUnixTimestamp64Milli(1767312000000))",
	}
	for in, want := range cases {
		if got := process(t, ProcessorOptions{Query: in, TimeRange: absRange()}); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}

func TestTimeFilter_UnitFromNameSuffixAndCaller(t *testing.T) {
	if got := process(t, ProcessorOptions{Query: "$__timeFilter(t.created_s)", TimeRange: absRange()}); got != "(t.created_s >= 1767225600 AND t.created_s <= 1767312000)" {
		t.Fatalf("suffix _s: %s", got)
	}
	if got := process(t, ProcessorOptions{Query: "$__timeFilter(ts)", TimeRange: absRange(), TimeFieldUnit: "s"}); got != "(ts >= 1767225600 AND ts <= 1767312000)" {
		t.Fatalf("caller unit: %s", got)
	}
}

func TestTimeFilter_UnknownUnitIsAnError(t *testing.T) {
	out := ProcessQueryVariables(ProcessorOptions{Query: "$__timeFilter(ts, weeks)", TimeRange: absRange()})
	if len(out.Errors) == 0 {
		t.Fatalf("expected an error, got %s", out.Query)
	}
}

func TestTimeFilter_EachColumnGetsItsOwnPredicate(t *testing.T) {
	got := process(t, ProcessorOptions{
		Query:     "WHERE $__timeFilter(a, s) AND $__timeFilter(b, ms)",
		TimeRange: absRange(),
	})
	want := "WHERE (a >= 1767225600 AND a <= 1767312000) AND (b >= 1767225600000 AND b <= 1767312000000)"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}

func TestTimeFilter_BareMacroUsesTimeField(t *testing.T) {
	got := process(t, ProcessorOptions{Query: "WHERE $__timeFilter", TimeRange: absRange(), TimeField: "ts_ms"})
	if got != "WHERE (ts_ms >= 1767225600000 AND ts_ms <= 1767312000000)" {
		t.Fatalf("got %s", got)
	}
}

func TestInterval_MsVariantIsNotSwallowed(t *testing.T) {
	// One day over 1000 points is an 86 s bucket.
	got := process(t, ProcessorOptions{Query: "SELECT $__interval, $__interval_ms", TimeRange: absRange()})
	if got != "SELECT 86, 86000" {
		t.Fatalf("got %s", got)
	}
}

func TestTimeFromTo_DefaultMilliseconds(t *testing.T) {
	got := process(t, ProcessorOptions{Query: "$__timeFrom $__timeTo", TimeRange: absRange()})
	if got != "1767225600000 1767312000000" {
		t.Fatalf("got %s", got)
	}
	got = process(t, ProcessorOptions{Query: "$__timeFrom", TimeRange: absRange(), TimeFieldUnit: "s"})
	if got != "1767225600" {
		t.Fatalf("seconds: got %s", got)
	}
}

func TestParseRelativeTime_MonthsAndMinutesKeepCase(t *testing.T) {
	base := time.Date(2026, 3, 31, 12, 0, 0, 0, time.UTC)
	if got := parseRelativeTime("1M", base); !got.Equal(base.AddDate(0, -1, 0)) {
		t.Fatalf("1M: got %v", got)
	}
	if got := parseRelativeTime("now-1M", base); !got.Equal(base.AddDate(0, -1, 0)) {
		t.Fatalf("now-1M: got %v", got)
	}
	if got := parseRelativeTime("1m", base); !got.Equal(base.Add(-time.Minute)) {
		t.Fatalf("1m: got %v", got)
	}
	if got := parseRelativeTime("2MONTHS", base); !got.Equal(base.AddDate(0, -2, 0)) {
		t.Fatalf("2MONTHS: got %v", got)
	}
	if got := parseRelativeTime("NOW", base); !got.Equal(base) {
		t.Fatalf("NOW: got %v", got)
	}
	if got := parseRelativeTime("1y", base); !got.Equal(base.AddDate(-1, 0, 0)) {
		t.Fatalf("1y: got %v", got)
	}
}

func TestInferTimeUnit(t *testing.T) {
	cases := map[string]string{
		"created_ms": "ms", "ts_s": "s", "t.ts_ns": "ns", "`ev_us`": "us",
		"event_time": "", "timestamp": "", "status": "",
	}
	for in, want := range cases {
		if got := InferTimeUnit(in); got != want {
			t.Errorf("%s: got %q, want %q", in, got, want)
		}
	}
}

func TestTimeFilter_NestedExpression(t *testing.T) {
	got := process(t, ProcessorOptions{Query: "WHERE $__timeFilter(toUnixTimestamp(event_time), s) AND $__timeFilter(toDateTime(a, 'UTC'))", TimeRange: absRange()})
	want := "WHERE (toUnixTimestamp(event_time) >= 1767225600 AND toUnixTimestamp(event_time) <= 1767312000) AND " +
		"((toDateTime(a, 'UTC') >= 1767225600 AND toDateTime(a, 'UTC') <= 1767312000) OR (toDateTime(a, 'UTC') >= 1767225600000 AND toDateTime(a, 'UTC') <= 1767312000000))"
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
}
