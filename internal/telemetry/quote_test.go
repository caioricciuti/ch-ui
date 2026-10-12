package telemetry

import "testing"

func TestQuoteEscapes(t *testing.T) {
	for in, want := range map[string]string{
		"Events.Name": "`Events.Name`",
		"a`b":         "`a\\`b`",
		`a\`:          "`a\\\\`",
	} {
		if got := Quote(in); got != want {
			t.Errorf("Quote(%q) = %s, want %s", in, got, want)
		}
	}
}
