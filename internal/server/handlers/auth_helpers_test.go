package handlers

import (
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
)

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
		{name: "network", raw: "dial tcp 127.0.0.1:8123: connection refused", want: "Connection to ClickHouse failed: could not reach ClickHouse: nothing is listening there. Check that ClickHouse is running and the port is its HTTP port (8123). Inside Docker, localhost is the container itself; use host.docker.internal."},
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
		{name: "no such host", raw: `request failed: Post "http://clickhouse:8123/?default_format=JSON": dial tcp: lookup clickhouse on 127.0.0.11:53: no such host`, want: chAuthUnreachable},
		{name: "native port", raw: `request failed: Post "http://ch:9000/?default_format=JSON": net/http: HTTP/1.x transport connection broken: malformed HTTP response "\x00\x00"`, want: chAuthUnreachable},
		{name: "eof", raw: `request failed: Post "http://ch:9000/?default_format=JSON": EOF`, want: chAuthUnreachable},
		{name: "client timeout", raw: `request failed: Post "http://ch:8123/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)`, want: chAuthUnreachable},
		{name: "tls", raw: `request failed: Post "https://ch:8443/": tls: failed to verify certificate: x509: certificate signed by unknown authority`, want: chAuthUnreachable},
		{name: "host named auth is still unreachable", raw: `request failed: Post "http://auth-db:8123/": dial tcp 10.0.0.5:8123: connect: connection refused`, want: chAuthUnreachable},
		{name: "clickhouse answered with auth error", raw: "ClickHouse error: Code: 516. DB::Exception: default: Authentication failed: password is incorrect, or there is no user with such name. (AUTHENTICATION_FAILED)", want: chAuthRejected},
		{name: "clickhouse answer mentioning network still counts", raw: "ClickHouse error: Code: 210. DB::Exception: network error inside cluster", want: chAuthUnknown},
		{name: "word containing eof is not eof", raw: "geofence lookup failed", want: chAuthUnknown},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classifyClickHouseAuthError(tc.raw); got != tc.want {
				t.Fatalf("classify(%q) = %v, want %v", tc.raw, got, tc.want)
			}
		})
	}
}

func TestDescribeClickHouseConnectionError(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want []string
	}{
		{name: "native port", raw: `request failed: Post "http://ch:9000/?default_format=JSON&user=x": net/http: HTTP/1.x transport connection broken: malformed HTTP response "\x00"`,
			want: []string{"http://ch:9000", "Port 9000 is the native protocol", "8123"}},
		{name: "native port eof", raw: `request failed: Post "http://ch:9000/": EOF`, want: []string{"Port 9000 is the native protocol"}},
		{name: "no such host", raw: `request failed: Post "http://clickhouse:8123/": dial tcp: lookup clickhouse: no such host`,
			want: []string{"http://clickhouse:8123", "does not resolve", "host.docker.internal"}},
		{name: "refused", raw: `request failed: Post "http://localhost:8123/": dial tcp [::1]:8123: connect: connection refused`,
			want: []string{"http://localhost:8123", "nothing is listening"}},
		{name: "timeout", raw: `request failed: Post "http://10.0.0.1:8123/": context deadline exceeded`, want: []string{"timed out"}},
		{name: "tls", raw: `request failed: Post "https://ch:8443/": x509: certificate signed by unknown authority`, want: []string{"TLS handshake failed"}},
		{name: "plain http on https", raw: `request failed: Post "https://ch:8123/": http: server gave HTTP response to HTTPS client`, want: []string{"Use http://"}},
		{name: "tunnel", raw: "tunnel not connected", want: []string{"agent for this connection is not connected"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := describeClickHouseConnectionError(tc.raw)
			if !strings.HasPrefix(got, "Connection to ClickHouse failed") {
				t.Fatalf("message must keep the stable prefix: %q", got)
			}
			if strings.Contains(got, "default_format") || strings.Contains(got, "user=") {
				t.Fatalf("message leaked the query string: %q", got)
			}
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Fatalf("describe(%q) = %q, want it to contain %q", tc.raw, got, w)
				}
			}
		})
	}
}

func TestConnectionKind(t *testing.T) {
	cases := []struct {
		conn database.Connection
		want string
	}{
		{database.Connection{Type: database.ConnectionTypeDirect}, "direct"},
		{database.Connection{IsEmbedded: true, Type: database.ConnectionTypeTunnel}, "direct"},
		{database.Connection{Type: database.ConnectionTypeTunnel}, "agent"},
		{database.Connection{}, "agent"},
	}
	for _, tc := range cases {
		if got := connectionKind(tc.conn); got != tc.want {
			t.Fatalf("connectionKind(%+v) = %q, want %q", tc.conn, got, tc.want)
		}
	}
}
