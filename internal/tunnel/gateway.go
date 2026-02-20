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
type PendingStreamRequest struct {
	MetaCh  chan json.RawMessage // receives query_stream_start meta
	ChunkCh chan json.RawMessage // receives query_stream_chunk data (buffered)
	DoneCh  chan json.RawMessage // receives query_stream_end statistics
	ErrorCh chan error
}

// ConnectedTunnel represents an active tunnel agent connection.
type ConnectedTunnel struct {
	ConnectionID   string
	ConnectionName string
	WS             *websocket.Conn
	LastSeen       time.Time
	Pending        sync.Map // map[requestID]*PendingRequest
	mu             sync.Mutex
}

// Gateway manages WebSocket connections from tunnel agents.
type Gateway struct {
	db      *database.DB
	tunnels sync.Map // map[connectionID]*ConnectedTunnel
	stopCh  chan struct{}
}

// NewGateway creates a new tunnel gateway.
func NewGateway(db *database.DB) *Gateway {
	g := &Gateway{
		db:     db,
		stopCh: make(chan struct{}),
	}
	go g.heartbeatLoop()
	slog.Info("Tunnel gateway initialized")
	return g
}

// Stop stops the gateway heartbeat.
func (g *Gateway) Stop() {
	close(g.stopCh)
}

// HandleWebSocket handles the WebSocket upgrade and read loop for a tunnel agent.
func (g *Gateway) HandleWebSocket(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("WebSocket upgrade failed", "error", err)
		return
	}

	slog.Debug("New tunnel WebSocket connection")

	// Read loop
	go g.readLoop(conn)
}

func (g *Gateway) readLoop(conn *websocket.Conn) {
	var connID string // set after auth

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
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Invalid message format"))
			return
		}

		// Any valid message means the tunnel is alive.
		if connID != "" {
			g.touchTunnel(connID)
		}

		switch msg.Type {
		case "auth":
			authConnID := g.handleAuth(conn, msg.Token, msg.Takeover)
			if authConnID == "" {
				return // auth failed, connection closed
			}
			connID = authConnID

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

		case "query_stream_chunk":
			g.handleStreamChunk(connID, &msg)

		case "query_stream_end":
			g.handleStreamEnd(connID, &msg)

		case "query_stream_error":
			g.handleStreamError(connID, &msg)

		default:
			slog.Warn("Unknown tunnel message type", "type", msg.Type)
		}
	}
}

func (g *Gateway) handleAuth(conn *websocket.Conn, token string, takeover bool) string {
	remoteAddr := ""
	if conn != nil && conn.RemoteAddr() != nil {
		remoteAddr = conn.RemoteAddr().String()
	}

	authCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	tc, err := g.db.GetConnectionByTokenCtx(authCtx, token)
	if err != nil {
		slog.Warn("Tunnel auth failed: token lookup error", "remote_addr", remoteAddr, "error", err)
		g.sendJSON(conn, GatewayMessage{Type: "auth_error", Message: "Tunnel auth temporarily unavailable. Please retry."})
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseTryAgainLater, "Auth backend busy"))
		return ""
	}
	if tc == nil {
		slog.Debug("Tunnel auth failed: invalid token", "remote_addr", remoteAddr)
		g.sendJSON(conn, GatewayMessage{Type: "auth_error", Message: "Invalid tunnel token"})
		conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Invalid token"))
		return ""
	}

	// If a healthy connection already exists for this token, reject duplicates.
	// This avoids two agent processes evicting each other in a reconnect loop.
	if existing, ok := g.tunnels.Load(tc.ID); ok {
		t := existing.(*ConnectedTunnel)
		isHealthy := time.Since(t.LastSeen) < 45*time.Second
		if isHealthy && !takeover {
			slog.Warn("Tunnel auth rejected: connection already active", "name", tc.Name)
			g.sendJSON(conn, GatewayMessage{Type: "auth_error", Message: "Tunnel token already connected (use --takeover to replace it)"})
			conn.WriteMessage(websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.ClosePolicyViolation, "Token already connected"))
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
		LastSeen:       time.Now(),
	}
	g.tunnels.Store(tc.ID, tunnel)

	g.db.UpdateConnectionStatus(tc.ID, "connected")

	g.sendJSON(conn, GatewayMessage{
		Type:           "auth_ok",
		ConnectionID:   tc.ID,
		ConnectionName: tc.Name,
	})

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
		t.LastSeen = time.Now()
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

	pending.ChunkCh <- msg.Data // backpressure: blocks if consumer is slow
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
	}
	payload, _ := json.Marshal(done)

	close(pending.ChunkCh)
	select {
	case pending.DoneCh <- payload:
	default:
	}
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

	close(pending.ChunkCh)
	select {
	case pending.ErrorCh <- errors.New(msg.Error):
	default:
	}
}

func (g *Gateway) handleDisconnect(connID string, ws *websocket.Conn) {
	val, ok := g.tunnels.Load(connID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)
	if ws != nil && t.WS != ws {
		// A newer connection already replaced this one.
		return
	}
	if !g.tunnels.CompareAndDelete(connID, t) {
		return
	}

	// Reject all pending requests
	t.Pending.Range(func(key, value any) bool {
		switch p := value.(type) {
		case *PendingRequest:
			select {
			case p.ErrorCh <- errors.New("tunnel disconnected"):
			default:
			}
		case *PendingStreamRequest:
			close(p.ChunkCh)
			select {
			case p.ErrorCh <- errors.New("tunnel disconnected"):
			default:
			}
		}
		t.Pending.Delete(key)
		return true
	})

	g.db.UpdateConnectionStatus(connID, "disconnected")

	slog.Info("Tunnel disconnected", "name", t.ConnectionName, "connection_id", connID)

	g.db.CreateAuditLog(database.AuditLogParams{
		Action:       "tunnel.disconnected",
		ConnectionID: strPtr(connID),
	})
}

func strPtr(s string) *string { return &s }

func (g *Gateway) sendJSON(conn *websocket.Conn, msg GatewayMessage) {
	data, _ := json.Marshal(msg)
	conn.WriteMessage(websocket.TextMessage, data)
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

		if now.Sub(t.LastSeen) > staleThreshold {
			slog.Warn("Tunnel connection stale, removing", "name", t.ConnectionName, "lastSeen", t.LastSeen)
			t.mu.Lock()
			t.WS.Close()
			t.mu.Unlock()
			g.handleDisconnect(connID, t.WS)
			return true
		}

		ping := GatewayMessage{Type: "ping"}
		data, _ := json.Marshal(ping)
		t.mu.Lock()
		err := t.WS.WriteMessage(websocket.TextMessage, data)
		t.mu.Unlock()
		if err != nil {
			slog.Warn("Ping failed", "name", t.ConnectionName, "error", err)
			g.handleDisconnect(connID, t.WS)
		}
		return true
	})
}
