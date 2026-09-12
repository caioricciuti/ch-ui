package handlers

import "testing"

func TestUserRateLimitKeyScopedByConnection(t *testing.T) {
	k1 := userRateLimitKey("Default", "conn-a")
	k2 := userRateLimitKey("default", "conn-b")

	if k1 == k2 {
		t.Fatalf("user rate limit key must include connection scope")
	}

	if k1 != "user:default:conn-a" {
		t.Fatalf("unexpected normalized key: %s", k1)
	}
}

func TestSanitizeClickHouseAuthMessage(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "credentials", raw: "Code: 516. DB::Exception: Authentication failed", want: "Invalid credentials"},
		{name: "network", raw: "dial tcp 127.0.0.1:8123: connection refused", want: "Connection to ClickHouse failed"},
		{name: "empty", raw: "", want: "Invalid credentials"},
		{name: "fallback", raw: "unexpected upstream response", want: "Authentication failed"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := sanitizeClickHouseAuthMessage(tc.raw)
			if got != tc.want {
				t.Fatalf("unexpected sanitized message: got %q want %q", got, tc.want)
			}
		})
	}
}

// TestClassifyClickHouseAuthError pins down which failures count against the
// login rate limiter. The agent reports a rejected password and an
// unreachable ClickHouse the same way, so only the clearly unreachable ones
// may skip the counter: everything else, including messages we do not
// recognise, has to count or the lockout can be dodged.
func TestClassifyClickHouseAuthError(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want chAuthErrorKind
	}{
		{name: "clickhouse rejected", raw: "Code: 516. DB::Exception: default: Authentication failed", want: chAuthRejected},
		{name: "access denied", raw: "Access denied for user", want: chAuthRejected},
		{name: "empty", raw: "", want: chAuthRejected},
		{name: "refused", raw: "dial tcp 127.0.0.1:8123: connection refused", want: chAuthUnreachable},
		{name: "tunnel offline", raw: "tunnel not connected", want: chAuthUnreachable},
		{name: "tunnel dropped", raw: "tunnel disconnected", want: chAuthUnreachable},
		{name: "timeout", raw: "connection test timeout", want: chAuthUnreachable},
		{name: "generic agent failure", raw: "Connection test failed", want: chAuthUnreachable},
		{name: "unrecognised counts", raw: "unexpected upstream response", want: chAuthUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyClickHouseAuthError(tc.raw); got != tc.want {
				t.Fatalf("classify(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}
