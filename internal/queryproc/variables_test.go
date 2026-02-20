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
