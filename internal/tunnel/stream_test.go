package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

// dialAgent opens a websocket to a test gateway, as an agent would.
func dialAgent(t *testing.T, g *Gateway) *websocket.Conn {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(g.HandleWebSocket))
	t.Cleanup(server.Close)
	ws, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ws.Close() })
	return ws
}

// connectAgent dials and authenticates a fake agent.
func connectAgent(t *testing.T, g *Gateway) *websocket.Conn {
	t.Helper()
	ws := dialAgent(t, g)
	if err := ws.WriteJSON(AgentMessage{Type: "auth", Token: "test-only"}); err != nil {
		t.Fatal(err)
	}
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var ack GatewayMessage
	if err := ws.ReadJSON(&ack); err != nil || ack.Type != "auth_ok" {
		t.Fatalf("auth: %+v, %v", ack, err)
	}
	ws.SetReadDeadline(time.Time{})
	return ws
}

// startStream dispatches a streaming query and returns it with the request
// the agent received.
func startStream(t *testing.T, g *Gateway, connectionID string, ws *websocket.Conn) (string, *PendingStreamRequest) {
	t.Helper()
	requestID, stream, err := g.ExecuteStreamQuery(connectionID, "SELECT number FROM numbers(100)", "default", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var msg GatewayMessage
	if err := ws.ReadJSON(&msg); err != nil || msg.Type != "query_stream" || msg.QueryID != requestID {
		t.Fatalf("agent did not receive the stream: %+v, %v", msg, err)
	}
	ws.SetReadDeadline(time.Time{})
	return requestID, stream
}

// fillStream sends more chunks than the stream buffers, so the gateway's read
// loop ends up waiting to deliver the last one.
func fillStream(t *testing.T, ws *websocket.Conn, requestID string, stream *PendingStreamRequest) {
	t.Helper()
	if err := ws.WriteJSON(AgentMessage{Type: "query_stream_start", QueryID: requestID, Meta: json.RawMessage(`[]`)}); err != nil {
		t.Fatal(err)
	}
	for i := 0; i <= cap(stream.ChunkCh); i++ {
		if err := ws.WriteJSON(AgentMessage{Type: "query_stream_chunk", QueryID: requestID, Seq: i, Data: json.RawMessage(`[[1]]`)}); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(5 * time.Second)
	for len(stream.ChunkCh) < cap(stream.ChunkCh) {
		if time.Now().After(deadline) {
			t.Fatal("chunk buffer never filled")
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Give the read loop time to read the extra chunk and wait on the full buffer.
	time.Sleep(100 * time.Millisecond)
}

// answerQueries replies to every plain query with a one-row result.
func answerQueries(ws *websocket.Conn) {
	for {
		var msg GatewayMessage
		if err := ws.ReadJSON(&msg); err != nil {
			return
		}
		if msg.Type != "query" {
			continue
		}
		if err := ws.WriteJSON(AgentMessage{Type: "query_result", ID: msg.ID, Data: json.RawMessage(`[{"value":1}]`)}); err != nil {
			return
		}
	}
}

func TestStreamEndDeliversBufferedChunksThenDone(t *testing.T) {
	g, connectionID := newTestGateway(t)
	ws := connectAgent(t, g)
	requestID, stream := startStream(t, g, connectionID, ws)
	defer g.CleanupStream(connectionID, requestID)

	ws.WriteJSON(AgentMessage{Type: "query_stream_start", QueryID: requestID, Meta: json.RawMessage(`[]`)})
	for i := 0; i < 3; i++ {
		ws.WriteJSON(AgentMessage{Type: "query_stream_chunk", QueryID: requestID, Seq: i, Data: json.RawMessage(`[[1]]`)})
	}
	ws.WriteJSON(AgentMessage{Type: "query_stream_end", QueryID: requestID, TotalRows: 3})

	select {
	case <-stream.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not finish")
	}
	if got := len(stream.ChunkCh); got != 3 {
		t.Fatalf("want 3 buffered chunks after the end, got %d", got)
	}
	select {
	case payload := <-stream.DoneCh:
		var done StreamDone
		if err := json.Unmarshal(payload, &done); err != nil || done.TotalRows != 3 {
			t.Fatalf("done payload: %s, %v", payload, err)
		}
	default:
		t.Fatal("DoneCh must hold the result when Done() is closed")
	}
}

func TestAbandonedStreamDoesNotBlockTunnel(t *testing.T) {
	g, connectionID := newTestGateway(t)
	ws := connectAgent(t, g)
	requestID, stream := startStream(t, g, connectionID, ws)
	fillStream(t, ws, requestID, stream)

	// The browser aborted: the handler stops reading and cleans up.
	g.CleanupStream(connectionID, requestID)

	go answerQueries(ws)
	result, err := g.ExecuteQueryWithSettingsCtx(context.Background(), connectionID, "SELECT 1", "default", "", nil, 3*time.Second)
	if err != nil || result == nil {
		t.Fatalf("a later query on the same tunnel must complete: %v", err)
	}
}

func TestDisconnectWithBlockedStreamSenderDoesNotPanic(t *testing.T) {
	g, connectionID := newTestGateway(t)
	ws := connectAgent(t, g)
	requestID, stream := startStream(t, g, connectionID, ws)
	fillStream(t, ws, requestID, stream)

	// The heartbeat drops the tunnel while the read loop waits on the stream.
	g.handleDisconnect(connectionID, nil)

	select {
	case <-stream.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("disconnect did not finish the stream")
	}
	select {
	case err := <-stream.ErrorCh:
		if err == nil || err.Error() != "tunnel disconnected" {
			t.Fatalf("unexpected stream error: %v", err)
		}
	default:
		t.Fatal("ErrorCh must hold the disconnect error when Done() is closed")
	}

	// The read loop survived: it still answers pings on this socket.
	var ponged atomic.Bool
	ws.SetPongHandler(func(string) error { ponged.Store(true); return nil })
	if err := ws.WriteControl(websocket.PingMessage, nil, time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	ws.SetReadDeadline(time.Now().Add(time.Second))
	_, _, err := ws.ReadMessage()
	var netErr net.Error
	if !errors.As(err, &netErr) || !netErr.Timeout() {
		t.Fatalf("read loop closed the socket: %v", err)
	}
	if !ponged.Load() {
		t.Fatal("read loop no longer answers pings")
	}
}

func TestUnauthenticatedClientIsDisconnectedAfterDeadline(t *testing.T) {
	g, _ := newTestGateway(t)
	g.authTimeout = 200 * time.Millisecond
	ws := dialAgent(t, g)
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err := ws.ReadMessage()
	var netErr net.Error
	if err == nil || (errors.As(err, &netErr) && netErr.Timeout()) {
		t.Fatalf("gateway must drop a client that never authenticates, got %v", err)
	}
}

func TestAuthDeadlineIsClearedAfterAuth(t *testing.T) {
	g, connectionID := newTestGateway(t)
	g.authTimeout = 200 * time.Millisecond
	ws := connectAgent(t, g)
	time.Sleep(400 * time.Millisecond)

	go answerQueries(ws)
	if _, err := g.ExecuteQueryWithSettingsCtx(context.Background(), connectionID, "SELECT 1", "default", "", nil, 3*time.Second); err != nil {
		t.Fatalf("authenticated tunnel must outlive the auth deadline: %v", err)
	}
}

func TestOversizedFrameBeforeAuthClosesConnection(t *testing.T) {
	g, _ := newTestGateway(t)
	ws := dialAgent(t, g)
	big := `{"type":"auth","token":"` + strings.Repeat("x", preAuthReadLimit*2) + `"}`
	if err := ws.WriteMessage(websocket.TextMessage, []byte(big)); err != nil {
		// The gateway rejects the frame from its header and closes the socket
		// while the payload is still being written.
		return
	}
	ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, _, err := ws.ReadMessage()
	if websocket.IsCloseError(err, websocket.CloseMessageTooBig) {
		return
	}
	// Without a limit the frame is read whole and answered as a bad token
	// (auth_error, then a policy-violation close).
	var netErr net.Error
	if err == nil || websocket.IsCloseError(err, websocket.ClosePolicyViolation) || (errors.As(err, &netErr) && netErr.Timeout()) {
		t.Fatalf("oversized frame must be refused before it is processed, got %v", err)
	}
}
