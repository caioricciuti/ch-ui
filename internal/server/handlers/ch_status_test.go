package handlers

import (
	"errors"
	"net/http"
	"testing"
)

func TestClickhouseStatus(t *testing.T) {
	cases := []struct {
		name string
		err  string
		want int
	}{
		{"access denied", "ClickHouse error: Code: 497. DB::Exception: sso:a@b.c: Not enough privileges. To execute this query, it's necessary to have the grant DROP DATABASE ON chui.*. (ACCESS_DENIED) (version 26.7.29.12 (official build))", http.StatusForbidden},
		{"readonly", "ClickHouse error: Code: 164. DB::Exception: Cannot execute query in readonly mode. (READONLY)", http.StatusForbidden},
		{"unknown table", "ClickHouse error: Code: 60. DB::Exception: Unknown table expression identifier 'FORMAT'. (UNKNOWN_TABLE)", http.StatusBadRequest},
		{"syntax", "ClickHouse error: Code: 62. DB::Exception: Syntax error: failed at position 36", http.StatusBadRequest},
		{"timeout", "ClickHouse error: Code: 159. DB::Exception: Timeout exceeded: elapsed 45.1 seconds. (TIMEOUT_EXCEEDED)", http.StatusGatewayTimeout},
		{"too many queries", "ClickHouse error: Code: 202. DB::Exception: Too many simultaneous queries. (TOO_MANY_SIMULTANEOUS_QUERIES)", http.StatusServiceUnavailable},
		{"other code", "ClickHouse error: Code: 241. DB::Exception: Memory limit exceeded. (MEMORY_LIMIT_EXCEEDED)", http.StatusBadGateway},
		{"no code", "tunnel: connector disconnected", http.StatusBadGateway},
		{"code not an exception", "Post \"http://127.0.0.1:8123\": dial tcp: connection refused", http.StatusBadGateway},
	}
	for _, c := range cases {
		if got := clickhouseStatus(errors.New(c.err)); got != c.want {
			t.Errorf("%s: got %d, want %d", c.name, got, c.want)
		}
	}
	if got := clickhouseStatus(nil); got != http.StatusOK {
		t.Errorf("nil error: got %d, want 200", got)
	}
}
