package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/safe"
)

// Limits for the agent WebSocket. The endpoint is reachable without
// credentials, so a client that has not authenticated yet gets a small frame
// budget and a short deadline to send its auth message. Once authenticated the
// limit is raised: query_result frames carry a whole non-streamed ClickHouse
// result in one message (the agent buffers the full HTTP body), so the cap only
// guards against runaway memory use, not against legitimate large results.
// The auth deadline is copied into each Gateway so tests can shorten it.
const (
	defaultAuthTimeout = 10 * time.Second
	preAuthReadLimit   = 64 << 10  // 64 KiB: an auth frame is a token and a flag
	authedReadLimit    = 256 << 20 // 256 MiB per frame
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		// Non-browser clients (like ch-ui-agent) typically do not send Origin.
		if origin == "" {
			return true
		}

		originURL, err := url.Parse(origin)
		if err != nil {
			return false
		}

		originHost, originPort := splitHostPort(originURL.Host)
		requestHost, requestPort := splitHostPort(r.Host)
		if originHost == "" || requestHost == "" {
			return false
		}

		samePort := originPort == "" || requestPort == "" || originPort == requestPort
		if samePort && strings.EqualFold(originHost, requestHost) {
			return true
		}

		// Local development friendliness: consider localhost and loopback IPs equivalent.
		if samePort && isLoopbackHost(originHost) && isLoopbackHost(requestHost) {
			return true
		}

		return false
	},
	EnableCompression: true,
}

func splitHostPort(hostport string) (string, string) {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return "", ""
	}

	host, port, err := net.SplitHostPort(hostport)
	if err == nil {
		return strings.Trim(host, "[]"), strings.TrimSpace(port)
	}

	// Host without explicit port.
	return strings.Trim(hostport, "[]"), ""
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// PendingRequest represents a request waiting for a response from the agent.
type PendingRequest struct {
	ResultCh chan json.RawMessage // receives the full response payload
	ErrorCh  chan error
}

// PendingStreamRequest represents a streaming query waiting for chunked responses.
//
// ChunkCh is never closed, so a late sender can never panic. The end of the
// stream is signalled by Done(): whichever path removes the request from the
// pending map (stream end, stream error, disconnect, or CleanupStream) first
// puts the outcome on DoneCh or ErrorCh and then closes done, exactly once.
// Chunks already buffered in ChunkCh when Done() fires belong to the stream
// and must be drained before reading DoneCh/ErrorCh.
type PendingStreamRequest struct {
	MetaCh     chan json.RawMessage // receives query_stream_start meta
	ChunkCh    chan json.RawMessage // receives query_stream_chunk data (buffered, never closed)
	ProgressCh chan json.RawMessage // receives query_stream_progress snapshots (latest wins)
	DoneCh     chan json.RawMessage // receives query_stream_end statistics
	ErrorCh    chan error

	done     chan struct{}
	doneOnce sync.Once
}

func newPendingStream() *PendingStreamRequest {
	return &PendingStreamRequest{
		MetaCh:     make(chan json.RawMessage, 1),
		ChunkCh:    make(chan json.RawMessage, 8),
		ProgressCh: make(chan json.RawMessage, 1),
		DoneCh:     make(chan json.RawMessage, 1),
		ErrorCh:    make(chan error, 1),
		done:       make(chan struct{}),
	}
}

// Done is closed when the stream has finished for any reason. After it is
// closed, no more chunks are delivered.
func (p *PendingStreamRequest) Done() <-chan struct{} {
	return p.done
}

// finish closes done exactly once. Call it only after the request has been
// removed from the pending map and any outcome was put on DoneCh/ErrorCh.
func (p *PendingStreamRequest) finish() {
	p.doneOnce.Do(func() { close(p.done) })
}

// ConnectedTunnel represents an active tunnel agent connection.
type ConnectedTunnel struct {
	ConnectionID   string
	ConnectionName string
	WS             *websocket.Conn
	Pending        sync.Map   // map[requestID]*PendingRequest
	mu             sync.Mutex // serializes every application-frame write
	seenMu         sync.RWMutex
	lastSeen       time.Time
	// capabilities the agent advertised when it authenticated.
	capabilities map[string]bool
}

// Supports reports whether the agent advertised capability at auth time.
func (t *ConnectedTunnel) Supports(capability string) bool {
	return t.capabilities[capability]
}

func (t *ConnectedTunnel) seenAt() time.Time {
	t.seenMu.RLock()
	defer t.seenMu.RUnlock()
	return t.lastSeen
}

func (t *ConnectedTunnel) touch() {
	t.seenMu.Lock()
	t.lastSeen = time.Now()
	t.seenMu.Unlock()
}

func (t *ConnectedTunnel) sendJSON(msg GatewayMessage) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	return sendJSON(t.WS, msg)
}

// Gateway manages WebSocket connections from tunnel agents.
type Gateway struct {
	db      *database.DB
	tunnels sync.Map // map[connectionID]*ConnectedTunnel
	stopCh  chan struct{}
	// readLoops counts agent read loops still running, including their
	// disconnect bookkeeping; see WaitReadLoops.
	readLoops sync.WaitGroup

	authTimeout time.Duration // how long a new socket may take to authenticate
}

// NewGateway creates a new tunnel gateway.
func NewGateway(db *database.DB) *Gateway {
	g := &Gateway{
		db:          db,
		stopCh:      make(chan struct{}),
		authTimeout: defaultAuthTimeout,
	}
	go g.heartbeatLoop()
	slog.Info("Tunnel gateway initialized")
	return g
}

// Stop stops the gateway heartbeat.
func (g *Gateway) Stop() {
	close(g.stopCh)
}

// WaitReadLoops waits up to timeout for every agent read loop to return,
// which includes the database writes of its disconnect. It reports whether
// they all did. Tests call it after closing their agents, so nothing writes
// to a database that is about to be closed and removed.
func (g *Gateway) WaitReadLoops(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		g.readLoops.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// HandleWebSocket handles the WebSocket upgrade and read loop for a tunnel agent.
func (g *Gateway) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("WebSocket upgrade failed", "error", err)
		return
	}

	slog.Debug("New tunnel WebSocket connection")

	// Read loop. A panic in a message handler must drop this one agent
	// connection, not the whole server.
	g.readLoops.Add(1)
	safe.Go("tunnel read loop", func() {
		defer g.readLoops.Done()
		g.readLoop(conn)
	})
}

func (g *Gateway) readLoop(conn *websocket.Conn) {
	var connID string // set after auth

	// Until the agent authenticates it is an anonymous client: bound the frame
	// size and the time it may take to send the auth message.
	conn.SetReadLimit(preAuthReadLimit)
	authTimeout := g.authTimeout
	if authTimeout <= 0 {
		authTimeout = defaultAuthTimeout
	}
	_ = conn.SetReadDeadline(time.Now().Add(authTimeout))

	conn.SetPingHandler(func(appData string) error {
		if connID != "" {
			g.touchTunnel(connID)
		}
		// Keep Gorilla's default behavior: reply with a Pong control frame.
		return conn.WriteControl(websocket.PongMessage, []byte(appData), time.Now().Add(5*time.Second))
	})
	conn.SetPongHandler(func(_ string) error {
		if connID != "" {
			g.touchTunnel(connID)
		}
		return nil
	})

	defer func() {
		if connID != "" {
			g.handleDisconnect(connID, conn)
		}
		conn.Close()
	}()

	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
				slog.Debug("Tunnel WebSocket read error", "error", err)
			}
			return
		}

		var msg AgentMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			slog.Warn("Failed to parse tunnel message", "error", err)
			conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Invalid message format"), time.Now().Add(5*time.Second))
			return
		}

		// Any valid message means the tunnel is alive.
		if connID != "" {
			g.touchTunnel(connID)
		}

		switch msg.Type {
		case "auth":
			if connID != "" {
				// Authentication writes below are exclusive to an unpublished
				// socket. Never let an established agent re-enter that path.
				conn.WriteControl(websocket.CloseMessage,
					websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Already authenticated"), time.Now().Add(5*time.Second))
				return
			}
			authConnID := g.handleAuth(conn, msg.Token, msg.Takeover, msg.Capabilities)
			if authConnID == "" {
				return // auth failed, connection closed
			}
			connID = authConnID
			conn.SetReadLimit(authedReadLimit)
			_ = conn.SetReadDeadline(time.Time{})

		case "pong":
			g.handlePong(connID)

		case "query_result":
			g.handleQueryResult(connID, &msg)

		case "query_error":
			g.handleQueryError(connID, &msg)

		case "test_result":
			g.handleTestResult(connID, &msg)

		case "host_info":
			g.handleHostInfo(connID, &msg)

		case "query_stream_start":
			g.handleStreamStart(connID, &msg)

		// Export results travel like a stream; the chunks carry base64 bytes
		// instead of JSON rows, and only the export consumer reads them.
		case "query_stream_chunk", "query_export_chunk":
			g.handleStreamChunk(connID, &msg)

		case "query_stream_progress":
			g.handleStreamProgress(connID, &msg)

		case "query_stream_end", "query_export_end":
			g.handleStreamEnd(connID, &msg)

		case "query_stream_error", "query_export_error":
			g.handleStreamError(connID, &msg)

		default:
			slog.Warn("Unknown tunnel message type", "type", msg.Type)
		}
	}
}

func (g *Gateway) handleAuth(conn *websocket.Conn, token string, takeover bool, capabilities []string) string {
	remoteAddr := ""
	if conn != nil && conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}

	authCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tc, err := g.db.GetConnectionByTokenCtx(authCtx, token)
	if err != nil {
		slog.Warn("Tunnel auth failed: token lookup error", "remote_addr", remoteAddr, "error", err)
		sendJSON(conn, GatewayMessage{Type: "auth_error", Message: "Tunnel auth temporarily unavailable. Please retry."})
		conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "Auth backend busy"), time.Now().Add(5*time.Second))
		return ""
	}
	if tc == nil {
		slog.Debug("Tunnel auth failed: invalid token", "remote_addr", remoteAddr)
		sendJSON(conn, GatewayMessage{Type: "auth_error", Message: "Invalid tunnel token"})
		conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Invalid token"), time.Now().Add(5*time.Second))
		return ""
	}

	// If a healthy connection already exists for this token, reject duplicates.
	// This avoids two agent processes evicting each other in a reconnect loop.
	if existing, ok := g.tunnels.Load(tc.ID); ok {
		t := existing.(*ConnectedTunnel)
		isHealthy := time.Since(t.seenAt()) < 45*time.Second
		if isHealthy && !takeover {
			slog.Warn("Tunnel auth rejected: connection already active", "name", tc.Name)
			sendJSON(conn, GatewayMessage{Type: "auth_error", Message: "Tunnel token already connected (use --takeover to replace it)"})
			conn.WriteControl(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Token already connected"), time.Now().Add(5*time.Second))
			return ""
		}
		if isHealthy && takeover {
			slog.Warn("Replacing active tunnel connection via takeover", "name", tc.Name)
		}

		// Replace stale session.
		t.mu.Lock()
		_ = t.WS.Close()
		t.mu.Unlock()
		g.handleDisconnect(tc.ID, t.WS)
		slog.Warn("Replaced stale tunnel connection", "name", tc.Name)
	}

	tunnel := &ConnectedTunnel{
		ConnectionID:   tc.ID,
		ConnectionName: tc.Name,
		WS:             conn,
		lastSeen:       time.Now(),
		capabilities:   make(map[string]bool, len(capabilities)),
	}
	for _, c := range capabilities {
		tunnel.capabilities[c] = true
	}
	// Publish under the write lock so an API request can find this tunnel as
	// soon as the agent receives auth_ok, but cannot write ahead of or race
	// with that acknowledgement.
	tunnel.mu.Lock()
	g.tunnels.Store(tc.ID, tunnel)
	err = sendJSON(conn, GatewayMessage{
		Type:           "auth_ok",
		ConnectionID:   tc.ID,
		ConnectionName: tc.Name,
	})
	tunnel.mu.Unlock()
	if err != nil {
		g.handleDisconnect(tc.ID, conn)
		return ""
	}

	// The token may have been regenerated, or the connection deleted, between
	// the lookup above and publishing. DisconnectConnection runs after that
	// change, so either it finds this tunnel or this check sees the change.
	current, err := g.db.GetConnectionByTokenCtx(authCtx, token)
	if err != nil || current == nil || current.ID != tc.ID {
		slog.Warn("Tunnel auth revoked during handshake", "name", tc.Name, "connection_id", tc.ID, "error", err)
		g.closeTunnel(tunnel, errTokenRevoked)
		return ""
	}

	g.db.UpdateConnectionStatus(tc.ID, "connected")

	slog.Info("Tunnel agent authenticated", "name", tc.Name, "connection_id", tc.ID)

	g.db.CreateAuditLog(database.AuditLogParams{
		Action:       "tunnel.connected",
		ConnectionID: strPtr(tc.ID),
	})

	return tc.ID
}

func (g *Gateway) handlePong(connID string) {
	if connID == "" {
		return
	}
	g.touchTunnel(connID)
	g.db.UpdateConnectionStatus(connID, "connected")
}

func (g *Gateway) touchTunnel(connID string) {
	if connID == "" {
		return
	}
	if val, ok := g.tunnels.Load(connID); ok {
		t := val.(*ConnectedTunnel)
		t.touch()
	}
}

func (g *Gateway) handleQueryResult(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.LoadAndDelete(id)
	if !ok {
		slog.Warn("Query result for unknown request", "id", id)
		return
	}
	pending := pendingVal.(*PendingRequest)

	// Build the result payload
	result := QueryResult{
		Data:  msg.Data,
		Meta:  msg.Meta,
		Stats: msg.GetStats(),
	}
	payload, _ := json.Marshal(result)

	select {
	case pending.ResultCh <- payload:
	default:
	}
}

func (g *Gateway) handleQueryError(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.LoadAndDelete(id)
	if !ok {
		return
	}
	pending := pendingVal.(*PendingRequest)

	select {
	case pending.ErrorCh <- errors.New(msg.Error):
	default:
	}
}

func (g *Gateway) handleTestResult(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.LoadAndDelete(id)
	if !ok {
		return
	}
	pending := pendingVal.(*PendingRequest)

	if msg.IsTestSuccess() {
		result := TestResult{Success: true, Version: msg.Version}
		payload, _ := json.Marshal(result)
		select {
		case pending.ResultCh <- payload:
		default:
		}
	} else {
		errMsg := msg.Error
		if errMsg == "" {
			errMsg = "Connection test failed"
		}
		select {
		case pending.ErrorCh <- errors.New(errMsg):
		default:
		}
	}
}

func (g *Gateway) handleHostInfo(connID string, msg *AgentMessage) {
	if connID == "" || len(msg.HostInfo) == 0 {
		return
	}

	var info database.HostInfo
	if err := json.Unmarshal(msg.HostInfo, &info); err != nil {
		slog.Warn("Failed to parse host info", "error", err)
		return
	}

	g.db.UpdateConnectionHostInfo(connID, info)
	slog.Debug("Host info received", "connection", connID, "hostname", info.Hostname)
}

func (g *Gateway) handleStreamStart(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.Load(id)
	if !ok {
		slog.Warn("Stream start for unknown request", "id", id)
		return
	}
	pending, ok := pendingVal.(*PendingStreamRequest)
	if !ok {
		return
	}

	select {
	case pending.MetaCh <- msg.Meta:
	default:
	}
}

func (g *Gateway) handleStreamChunk(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.Load(id)
	if !ok {
		return
	}
	pending, ok := pendingVal.(*PendingStreamRequest)
	if !ok {
		return
	}

	// Backpressure: wait while the consumer is slow, but give up as soon as
	// the stream is finished (consumer gone, cleanup, or disconnect) so the
	// read loop of this connection never blocks forever.
	select {
	case pending.ChunkCh <- msg.Data:
	case <-pending.done:
	}
}

// handleStreamProgress forwards a progress snapshot to the waiting HTTP handler.
// Snapshots are disposable: if the consumer has not picked up the previous one,
// it is dropped so a slow reader can never stall the agent's socket.
func (g *Gateway) handleStreamProgress(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" || len(msg.Progress) == 0 {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.Load(id)
	if !ok {
		return
	}
	pending, ok := pendingVal.(*PendingStreamRequest)
	if !ok || pending.ProgressCh == nil {
		return
	}

	select {
	case pending.ProgressCh <- msg.Progress:
	default:
		// Replace the stale snapshot with this newer one when possible.
		select {
		case <-pending.ProgressCh:
		default:
		}
		select {
		case pending.ProgressCh <- msg.Progress:
		default:
		}
	}
}

func (g *Gateway) handleStreamEnd(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.LoadAndDelete(id)
	if !ok {
		return
	}
	pending, ok := pendingVal.(*PendingStreamRequest)
	if !ok {
		return
	}

	done := StreamDone{
		Statistics: msg.GetStats(),
		TotalRows:  msg.TotalRows,
		Bytes:      msg.Bytes,
	}
	payload, _ := json.Marshal(done)

	select {
	case pending.DoneCh <- payload:
	default:
	}
	pending.finish()
}

func (g *Gateway) handleStreamError(connID string, msg *AgentMessage) {
	id := msg.GetMessageID()
	if connID == "" || id == "" {
		return
	}

	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	pendingVal, ok := t.Pending.LoadAndDelete(id)
	if !ok {
		return
	}
	pending, ok := pendingVal.(*PendingStreamRequest)
	if !ok {
		return
	}

	select {
	case pending.ErrorCh <- errors.New(msg.Error):
	default:
	}
	pending.finish()
}

// errTokenRevoked fails requests in flight on a tunnel whose token was
// regenerated or whose connection was deleted.
var errTokenRevoked = errors.New("tunnel agent disconnected: its token was revoked")

// DisconnectConnection closes the live agent tunnel of a connection, if any,
// and fails its pending requests. Call it after regenerating the token or
// deleting the connection: tokens are checked only at connect time, so an
// agent already attached would otherwise keep serving. Reports whether an
// agent was connected.
func (g *Gateway) DisconnectConnection(connID string) bool {
	val, ok := g.tunnels.Load(connID)
	if !ok {
		return false
	}
	return g.closeTunnel(val.(*ConnectedTunnel), errTokenRevoked)
}

// closeTunnel unpublishes t, failing its pending requests with reason, then
// tells the agent why and closes its socket. Unpublishing first keeps the read
// loop's own disconnect from racing ahead with a generic error. Reports
// whether t was still the published tunnel for its connection.
func (g *Gateway) closeTunnel(t *ConnectedTunnel, reason error) bool {
	removed := g.disconnect(t.ConnectionID, t.WS, reason)
	t.mu.Lock()
	_ = t.WS.WriteControl(websocket.CloseMessage,
		websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Token revoked"), time.Now().Add(time.Second))
	_ = t.WS.Close()
	t.mu.Unlock()
	return removed
}

func (g *Gateway) handleDisconnect(connID string, ws *websocket.Conn) {
	g.disconnect(connID, ws, errors.New("tunnel disconnected"))
}

// disconnect unpublishes the tunnel for connID when ws is still its socket (or
// ws is nil) and fails every pending request with reason.
func (g *Gateway) disconnect(connID string, ws *websocket.Conn, reason error) bool {
	val, ok := g.tunnels.Load(connID)
	if !ok {
		return false
	}
	t := val.(*ConnectedTunnel)
	if ws != nil && t.WS != ws {
		// A newer connection already replaced this one.
		return false
	}
	if !g.tunnels.CompareAndDelete(connID, t) {
		return false
	}

	// Reject all pending requests. LoadAndDelete makes this path the only one
	// that finalizes a request, even if the read loop finishes it concurrently.
	t.Pending.Range(func(key, _ any) bool {
		value, ok := t.Pending.LoadAndDelete(key)
		if !ok {
			return true
		}
		switch p := value.(type) {
		case *PendingRequest:
			select {
			case p.ErrorCh <- reason:
			default:
			}
		case *PendingStreamRequest:
			select {
			case p.ErrorCh <- reason:
			default:
			}
			p.finish()
		}
		return true
	})

	g.db.UpdateConnectionStatus(connID, "disconnected")

	slog.Info("Tunnel disconnected", "name", t.ConnectionName, "connection_id", connID)

	g.db.CreateAuditLog(database.AuditLogParams{
		Action:       "tunnel.disconnected",
		ConnectionID: strPtr(connID),
	})
	return true
}

func strPtr(s string) *string { return &s }

// sendJSON requires exclusive write ownership: either the connection is not
// authenticated/published yet, or its ConnectedTunnel.mu is held.
func sendJSON(conn *websocket.Conn, msg GatewayMessage) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, data)
}

// heartbeatLoop pings all connected agents every 30 seconds.
func (g *Gateway) heartbeatLoop() {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.pingAll()
		}
	}
}

func (g *Gateway) pingAll() {
	now := time.Now()
	staleThreshold := 3 * time.Minute

	g.tunnels.Range(func(key, value any) bool {
		connID := key.(string)
		t := value.(*ConnectedTunnel)

		lastSeen := t.seenAt()
		if now.Sub(lastSeen) > staleThreshold {
			slog.Warn("Tunnel connection stale, removing", "name", t.ConnectionName, "lastSeen", lastSeen)
			t.mu.Lock()
			t.WS.Close()
			t.mu.Unlock()
			g.handleDisconnect(connID, t.WS)
			return true
		}

		err := t.sendJSON(GatewayMessage{Type: "ping"})
		if err != nil {
			slog.Warn("Ping failed", "name", t.ConnectionName, "error", err)
			g.handleDisconnect(connID, t.WS)
		}
		return true
	})
}
