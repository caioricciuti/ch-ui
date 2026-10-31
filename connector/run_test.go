package connector

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector/config"
	"github.com/caioricciuti/ch-ui/connector/ui"
	"github.com/gorilla/websocket"
)

// fakeGateway answers each /connect attempt with reply(attempt), counted
// from 1. "refuse" fails the HTTP handshake, "ok" authenticates and keeps
// the socket open, "drop" authenticates and closes it, anything else is
// sent as an auth_error message.
func fakeGateway(t *testing.T, reply func(attempt int64) string) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var attempts atomic.Int64
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		action := reply(attempts.Add(1))
		if action == "refuse" {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var auth AgentMessage
		if err := conn.ReadJSON(&auth); err != nil {
			return
		}
		switch action {
		case "ok", "drop":
			_ = conn.WriteJSON(GatewayMessage{Type: MsgTypeAuthOK})
			if action == "drop" {
				return
			}
			for {
				if _, _, err := conn.ReadMessage(); err != nil {
					return
				}
			}
		default:
			_ = conn.WriteJSON(GatewayMessage{Type: MsgTypeAuthError, Message: action})
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &attempts
}

// One UI for every test: ui.New writes the global color.NoColor, which
// races with connectors from earlier tests still printing as they stop.
var testUI = ui.New(true, true, false, false)

func testConnector(srv *httptest.Server) *Connector {
	return New(&config.Config{
		TunnelURL:         "ws" + strings.TrimPrefix(srv.URL, "http") + "/connect",
		Token:             "cht_test",
		ClickHouseURL:     "http://127.0.0.1:8123",
		ReconnectDelay:    5 * time.Millisecond,
		MaxReconnectDelay: 20 * time.Millisecond,
		HeartbeatInterval: time.Hour,
		SkipHostInfo:      true,
	}, testUI)
}

func runAsync(c *Connector) <-chan error {
	errCh := make(chan error, 1)
	go func() { errCh <- c.Run() }()
	return errCh
}

func waitRun(t *testing.T, errCh <-chan error) error {
	t.Helper()
	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (c *Connector) isAuthenticated() bool {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.authenticated
}

// The gateway is not listening yet, then cannot check the token, then
// accepts: the first connection keeps retrying instead of giving up.
func TestRunRetriesInitialConnect(t *testing.T) {
	srv, attempts := fakeGateway(t, func(n int64) string {
		switch n {
		case 1:
			return "refuse"
		case 2:
			return "Tunnel auth temporarily unavailable. Please retry."
		default:
			return "ok"
		}
	})
	c := testConnector(srv)
	errCh := runAsync(c)

	waitFor(t, "authentication", c.isAuthenticated)
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3", got)
	}

	c.Shutdown()
	if err := waitRun(t, errCh); err != nil {
		t.Errorf("Run after Shutdown = %v, want nil", err)
	}
}

func TestRunStopsOnPermanentAuthError(t *testing.T) {
	srv, attempts := fakeGateway(t, func(int64) string { return "Invalid tunnel token" })
	c := testConnector(srv)

	err := waitRun(t, runAsync(c))
	if !isPermanentConnectError(err) {
		t.Fatalf("Run = %v, want a permanent auth error", err)
	}
	if got := attempts.Load(); got != 1 {
		t.Errorf("attempts = %d, want 1", got)
	}
	// Shutdown after the connector stopped itself must not close done again.
	c.Shutdown()
	c.Shutdown()
}

func TestShutdownDuringInitialRetry(t *testing.T) {
	srv, attempts := fakeGateway(t, func(int64) string { return "refuse" })
	c := testConnector(srv)
	errCh := runAsync(c)

	waitFor(t, "a retry", func() bool { return attempts.Load() >= 2 })
	c.Shutdown()
	if err := waitRun(t, errCh); err != nil {
		t.Errorf("Run after Shutdown = %v, want nil", err)
	}
	c.Shutdown()
}

// A token revoked while connected stops the connector during reconnect; a
// later Shutdown (as the embedded manager does) must not panic, and the
// message loop must stop instead of polling a nil connection.
func TestPermanentAuthOnReconnectThenShutdown(t *testing.T) {
	srv, attempts := fakeGateway(t, func(n int64) string {
		if n == 1 {
			return "drop"
		}
		return "Tunnel token revoked"
	})
	c := testConnector(srv)

	if err := waitRun(t, runAsync(c)); err != nil {
		t.Fatalf("Run = %v, want nil after a stop on reconnect", err)
	}
	if got := attempts.Load(); got != 2 {
		t.Errorf("attempts = %d, want 2", got)
	}
	if c.ctx.Err() == nil {
		t.Error("context still live after a permanent auth error; loops keep running")
	}
	c.Shutdown()
	c.Shutdown()
}

// The auth message names the ClickHouse server the agent dials, without
// the credentials its URL may carry.
func TestAuthReportsClickHouseEndpointWithoutCredentials(t *testing.T) {
	got := make(chan AgentMessage, 1)
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()
		var auth AgentMessage
		if err := conn.ReadJSON(&auth); err != nil {
			return
		}
		got <- auth
		_ = conn.WriteJSON(GatewayMessage{Type: MsgTypeAuthOK})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	c := testConnector(srv)
	c.cfg.ClickHouseURL = "http://reader:s3cret@CH.internal:8123/?password=x"
	errCh := runAsync(c)

	select {
	case auth := <-got:
		if auth.ClickHouseEndpoint != "http://ch.internal:8123/" {
			t.Errorf("clickhouse_endpoint = %q, want http://ch.internal:8123/", auth.ClickHouseEndpoint)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no auth message")
	}
	c.Shutdown()
	if err := waitRun(t, errCh); err != nil {
		t.Errorf("Run after Shutdown = %v, want nil", err)
	}
}
