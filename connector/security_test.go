package connector

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"testing"
)

type securityTransport func(*http.Request) (*http.Response, error)

func (f securityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSecurityRejectsCredentialRedirects(t *testing.T) {
	for _, status := range []int{301, 302, 303, 307, 308} {
		t.Run(fmt.Sprint(status), func(t *testing.T) {
			for name, run := range map[string]func(*CHClient) error{
				"query": func(c *CHClient) error {
					_, err := c.Execute(context.Background(), "SELECT 1", "alice", "secret", nil)
					return err
				},
				"raw": func(c *CHClient) error {
					_, err := c.ExecuteRaw(context.Background(), "SELECT 1", "alice", "secret", "JSON", nil)
					return err
				},
				"login": func(c *CHClient) error {
					_, err := c.TestConnection(context.Background(), "alice", "secret")
					return err
				},
				"stream": func(c *CHClient) error {
					_, _, err := c.ExecuteStreaming(context.Background(), "", "SELECT 1 FORMAT JSONCompactEachRow", "alice", "secret", 1, nil, func(json.RawMessage) error { return nil }, func(int, json.RawMessage) error { return nil }, nil)
					return err
				},
				"export": func(c *CHClient) error {
					_, err := c.ExecuteExport(context.Background(), "", "SELECT 1", "alice", "secret", "CSV", nil, func([]byte) error { return nil })
					return err
				},
			} {
				t.Run(name, func(t *testing.T) {
					c := NewCHClient("https://clickhouse.example", false)
					calls := 0
					c.httpClient.Transport = securityTransport(func(r *http.Request) (*http.Response, error) {
						calls++
						if r.URL.Host != "clickhouse.example" {
							t.Fatal("followed credential-bearing redirect")
						}
						if r.Header.Get("X-ClickHouse-Key") != "secret" {
							t.Fatal("missing initial authentication")
						}
						return &http.Response{StatusCode: status, Header: http.Header{"Location": []string{"http://attacker.example/collect"}}, Body: io.NopCloser(strings.NewReader("redirect")), Request: r}, nil
					})
					if err := run(c); err == nil {
						t.Fatal("redirect must fail query")
					}
					if calls != 1 {
						t.Fatalf("requests: %d", calls)
					}
				})
			}
		})
	}
}

func TestSecurityDialPinsValidatedAddresses(t *testing.T) {
	for _, tc := range []struct {
		name    string
		ips     []string
		blocked bool
	}{
		{"private", []string{"10.1.2.3", "fd12::1"}, false},
		{"loopback", []string{"127.0.0.1", "::1"}, false},
		{"public", []string{"203.0.113.5"}, false},
		{"metadata alias", []string{"169.254.169.254"}, true},
		{"mapped metadata", []string{"::ffff:169.254.169.254"}, true},
		{"ipv6 metadata", []string{"fd00:ec2::254"}, true},
		{"mixed answers", []string{"10.1.2.3", "169.254.169.254"}, true},
		{"unspecified", []string{"0.0.0.0"}, true},
		{"multicast", []string{"ff02::1"}, true},
		{"empty", nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			lookups, calls := 0, 0
			lookup := func(context.Context, string, string) ([]netip.Addr, error) {
				lookups++
				var ips []netip.Addr
				for _, ip := range tc.ips {
					ips = append(ips, netip.MustParseAddr(ip))
				}
				return ips, nil
			}
			dial := func(_ context.Context, _ string, addr string) (net.Conn, error) {
				calls++
				host, port, err := net.SplitHostPort(addr)
				if err != nil || port != "8123" {
					t.Fatalf("bad address: %s", addr)
				}
				if _, err := netip.ParseAddr(host); err != nil {
					t.Fatal("dial must receive validated IP, not DNS name")
				}
				return nil, nil
			}
			_, err := dialClickHouse(context.Background(), "tcp", "alias.example:8123", lookup, dial)
			if (err != nil) != tc.blocked {
				t.Fatalf("blocked=%v error=%v", tc.blocked, err)
			}
			if lookups != 1 || (tc.blocked && calls != 0) || (!tc.blocked && calls != 1) {
				t.Fatalf("lookups=%d dials=%d", lookups, calls)
			}
		})
	}
}

// Ordinary queries, sign-in, streams and exports must still work with the
// redirect policy enabled. A mocked transport keeps this runnable without ports.
func TestSecurityNormalClickHouseUsage(t *testing.T) {
	jsonBody := `{"meta":[{"name":"version","type":"String"}],"data":[{"version":"25.8"}],"rows":1}`
	for name, tc := range map[string]struct {
		body string
		run  func(*CHClient) error
	}{
		"query": {jsonBody, func(c *CHClient) error {
			r, e := c.Execute(context.Background(), "SELECT version() AS version", "alice", "secret", nil)
			if e == nil && len(r.Data) != 1 {
				t.Error("query data lost")
			}
			return e
		}},
		"raw": {jsonBody, func(c *CHClient) error {
			_, e := c.ExecuteRaw(context.Background(), "SELECT 1", "alice", "secret", "JSON", nil)
			return e
		}},
		"login": {jsonBody, func(c *CHClient) error {
			version, e := c.TestConnection(context.Background(), "alice", "secret")
			if e == nil && version != "25.8" {
				t.Errorf("version %q", version)
			}
			return e
		}},
		"stream": {"[1]\n", func(c *CHClient) error {
			chunks := 0
			_, rows, e := c.ExecuteStreaming(context.Background(), "", "SELECT 1 FORMAT JSONCompactEachRow", "alice", "secret", 1, nil, func(json.RawMessage) error { return nil }, func(int, json.RawMessage) error { chunks++; return nil }, nil)
			if e == nil && (rows != 1 || chunks != 1) {
				t.Errorf("stream rows=%d chunks=%d", rows, chunks)
			}
			return e
		}},
		"export": {"1\n", func(c *CHClient) error {
			var out strings.Builder
			n, e := c.ExecuteExport(context.Background(), "", "SELECT 1", "alice", "secret", "CSV", nil, func(b []byte) error { _, err := out.Write(b); return err })
			if e == nil && (n != 2 || out.String() != "1\n") {
				t.Errorf("export n=%d data=%q", n, out.String())
			}
			return e
		}},
	} {
		t.Run(name, func(t *testing.T) {
			c := NewCHClient("https://clickhouse.example", false)
			c.httpClient.Transport = securityTransport(func(r *http.Request) (*http.Response, error) {
				if r.Header.Get("X-ClickHouse-User") != "alice" || r.Header.Get("X-ClickHouse-Key") != "secret" {
					t.Error("authentication changed")
				}
				return &http.Response{StatusCode: 200, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(tc.body)), Request: r}, nil
			})
			if err := tc.run(c); err != nil {
				t.Fatal(err)
			}
		})
	}
}
