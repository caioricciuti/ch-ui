package tunnel

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/gorilla/websocket"
)

// Pause inside the actual websocket write, after Gorilla has marked its writer
// active. This makes the authentication/request overlap deterministic rather
// than hoping a fast client happens to encounter the handshake race.
type authPauseListener struct {
	net.Listener
	started chan struct{}
	release chan struct{}
	once    sync.Once
}

func (l *authPauseListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	return &authPauseConn{Conn: conn, listener: l}, nil
}

type authPauseConn struct {
	net.Conn
	listener *authPauseListener
}

func (c *authPauseConn) Write(p []byte) (int, error) {
	if bytes.Contains(p, []byte(`"type":"auth_ok"`)) {
		c.listener.once.Do(func() {
			close(c.listener.started)
			<-c.listener.release
		})
	}
	return c.Conn.Write(p)
}

func newTestGateway(t *testing.T) (*Gateway, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "tunnel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	id, err := db.CreateConnection(database.CreateConnectionParams{Name: "test", TunnelToken: "test-only"})
	if err != nil {
		t.Fatal(err)
	}
	g := NewGateway(db)
	t.Cleanup(g.Stop)
	return g, id
}

func TestAuthenticationSerializesFirstQuery(t *testing.T) {
	g, connectionID := newTestGateway(t)
	server := httptest.NewUnstartedServer(http.HandlerFunc(g.HandleWebSocket))
	paused := &authPauseListener{Listener: server.Listener, started: make(chan struct{}), release: make(chan struct{})}
	server.Listener = paused
	server.Start()
	t.Cleanup(server.Close)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(paused.release) }) }
	t.Cleanup(release)

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
	value, ok := g.tunnels.Load(connectionID)
	if !ok {
		t.Fatal("tunnel must be available when authentication is acknowledged")
	}
	tunnel := value.(*ConnectedTunnel)
	if tunnel.mu.TryLock() {
		tunnel.mu.Unlock()
		t.Fatal("authentication write is not serialized with query writes")
	}

	done := make(chan error, 1)
	go func() {
		_, err := g.ExecuteQueryWithSettingsCtx(context.Background(), connectionID, "SELECT 1", "default", "", nil, 5*time.Second)
		done <- err
	}()
	// Wait until a caller has registered a real request while the auth frame
	// remains blocked, then let the socket proceed without delaying either side.
	deadline := time.Now().Add(5 * time.Second)
	for {
		pending := false
		tunnel.Pending.Range(func(_, _ any) bool { pending = true; return false })
		if pending {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("query was not registered during authentication")
		}
		runtime.Gosched()
	}
	release()
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var message GatewayMessage
	if err := ws.ReadJSON(&message); err != nil || message.Type != "auth_ok" {
		t.Fatalf("first frame must acknowledge authentication: %+v, %v", message, err)
	}
	if err := ws.ReadJSON(&message); err != nil || message.Type != "query" {
		t.Fatalf("second frame must contain pending query: %+v, %v", message, err)
	}
	if err := ws.WriteJSON(AgentMessage{Type: "query_result", ID: message.ID, Data: json.RawMessage(`[{"value":1}]`)}); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("first query did not complete")
	}
}

func TestAuthenticatedProtocolViolationsCloseConnection(t *testing.T) {
	for _, message := range []string{`{"type":"auth","token":"test-only"}`, `invalid json`} {
		t.Run(message, func(t *testing.T) {
			g, _ := newTestGateway(t)
			server := httptest.NewServer(http.HandlerFunc(g.HandleWebSocket))
			t.Cleanup(server.Close)
			ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { ws.Close() })
			if err := ws.WriteJSON(AgentMessage{Type: "auth", Token: "test-only"}); err != nil {
				t.Fatal(err)
			}
			ws.SetReadDeadline(time.Now().Add(5 * time.Second))
			var reply GatewayMessage
			if err := ws.ReadJSON(&reply); err != nil || reply.Type != "auth_ok" {
				t.Fatalf("authenticate: %+v %v", reply, err)
			}
			if err := ws.WriteMessage(websocket.TextMessage, []byte(message)); err != nil {
				t.Fatal(err)
			}
			_, _, err = ws.ReadMessage()
			if !websocket.IsCloseError(err, websocket.ClosePolicyViolation) {
				t.Fatalf("expected protocol rejection, got %v", err)
			}
		})
	}
}

func TestTunnelActivityConcurrentStatus(t *testing.T) {
	g := &Gateway{}
	tunnel := &ConnectedTunnel{lastSeen: time.Now()}
	g.tunnels.Store("test", tunnel)
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				g.touchTunnel("test")
				if online, seen := g.GetTunnelStatus("test"); !online || seen.IsZero() {
					t.Error("active tunnel lost its status")
				}
			}
		}()
	}
	wg.Wait()
}
