package tunnel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func authAgent(t *testing.T, url, token string) (*websocket.Conn, GatewayMessage) {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(AgentMessage{Type: "auth", Token: token}); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var reply GatewayMessage
	if err := ws.ReadJSON(&reply); err != nil {
		t.Fatalf("auth reply: %v", err)
	}
	return ws, reply
}

func TestDisconnectConnectionFailsPendingWork(t *testing.T) {
	g, connID := newTestGateway(t)
	server := httptest.NewServer(http.HandlerFunc(g.HandleWebSocket))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	if g.DisconnectConnection(connID) {
		t.Fatal("no agent was connected")
	}

	ws, reply := authAgent(t, url, "test-only")
	if reply.Type != "auth_ok" {
		t.Fatalf("auth: %+v", reply)
	}

	queryErr := make(chan error, 1)
	go func() {
		_, err := g.ExecuteQuery(connID, "SELECT 1", "default", "", 5*time.Second)
		queryErr <- err
	}()
	_, stream, err := g.ExecuteStreamQuery(connID, "SELECT 1", "default", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	// Both requests are registered once the agent has read them.
	for i := 0; i < 2; i++ {
		var msg GatewayMessage
		if err := ws.ReadJSON(&msg); err != nil {
			t.Fatalf("read request %d: %v", i, err)
		}
	}

	if !g.DisconnectConnection(connID) {
		t.Fatal("DisconnectConnection must report the connected agent")
	}
	if g.IsTunnelOnline(connID) {
		t.Fatal("tunnel still published")
	}
	select {
	case err := <-queryErr:
		if err == nil || !strings.Contains(err.Error(), "token was revoked") {
			t.Fatalf("pending query error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending query was not failed")
	}
	select {
	case <-stream.Done():
		if err := <-stream.ErrorCh; err == nil || !strings.Contains(err.Error(), "token was revoked") {
			t.Fatalf("pending stream error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("pending stream was not finished")
	}
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
				t.Fatalf("agent close: %v", err)
			}
			break
		}
	}
	if g.DisconnectConnection(connID) {
		t.Fatal("second disconnect found an agent")
	}
}

// A token rotated while an agent is mid-handshake must not leave that agent
// attached: the gateway rechecks the token after publishing the tunnel.
func TestAuthRechecksTokenAfterPublishing(t *testing.T) {
	g, connID := newTestGateway(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(g.HandleWebSocket))
	paused := &authPauseListener{Listener: server.Listener, started: make(chan struct{}), release: make(chan struct{})}
	server.Listener = paused
	server.Start()
	t.Cleanup(server.Close)

	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	if err := ws.WriteJSON(AgentMessage{Type: "auth", Token: "test-only"}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-paused.started:
	case <-time.After(5 * time.Second):
		t.Fatal("authentication did not reach its websocket write")
	}
	// The token lookup already passed; rotate before the handshake finishes.
	if err := g.db.UpdateConnectionToken(connID, "rotated"); err != nil {
		t.Fatal(err)
	}
	close(paused.release)

	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	for {
		if _, _, err := ws.ReadMessage(); err != nil {
			if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
				t.Fatalf("agent close: %v", err)
			}
			break
		}
	}
	if g.IsTunnelOnline(connID) {
		t.Fatal("agent with a revoked token stayed attached")
	}
}
