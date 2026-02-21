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
