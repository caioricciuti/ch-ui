package handlers

import "testing"

func TestTimestampCondition(t *testing.T) {
	got, err := timestampCondition(">=", "2026-08-01T06:55:59.514Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := "Timestamp >= parseDateTime64BestEffort('2026-08-01T06:55:59.514Z')"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}

	for _, bad := range []string{"not-a-date", "2026-08-01", "'); DROP TABLE x; --"} {
		if _, err := timestampCondition("<=", bad); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
}
