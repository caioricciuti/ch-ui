package connector

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caioricciuti/ch-ui/connector/config"
	"github.com/caioricciuti/ch-ui/connector/ui"
	"github.com/gorilla/websocket"
)

// Connector manages the tunnel connection to a CH-UI server
type Connector struct {
	cfg      *config.Config
	ui       *ui.UI
	chClient *CHClient

	conn          *websocket.Conn
	connMu        sync.Mutex
	authenticated bool
	startTime     time.Time

	// Stats
	queriesExecuted atomic.Int64
	lastQueryTime   atomic.Int64

	// Control
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	// Reconnection
	reconnectDelay time.Duration
}

// New creates a new Connector instance
func New(cfg *config.Config, u *ui.UI) *Connector {
	ctx, cancel := context.WithCancel(context.Background())

	return &Connector{
		cfg:            cfg,
		ui:             u,
		chClient:       NewCHClient(cfg.ClickHouseURL, cfg.InsecureSkipVerify),
		reconnectDelay: cfg.ReconnectDelay,
		ctx:            ctx,
		cancel:         cancel,
		done:           make(chan struct{}),
	}
}

// Run starts the connector and blocks until shutdown
func (c *Connector) Run() error {
	c.startTime = time.Now()

	// Initial connection
	if err := c.connect(); err != nil {
		if ce, ok := err.(*ConnectError); ok && ce.Type == "auth" {
			c.ui.Error("Authentication failed — not retrying (token may be invalid or revoked)")
			return err
		}
		return err
	}

	// Start message handler
	go c.messageLoop()

	// Start heartbeat
	go c.heartbeatLoop()

	// Start host info reporting
	go c.hostInfoLoop()

	// Wait for shutdown
	<-c.done
	return nil
}

// Shutdown gracefully stops the connector
func (c *Connector) Shutdown() {
	c.cancel()
	c.connMu.Lock()
	if c.conn != nil {
		c.conn.WriteMessage(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "shutdown"))
		c.conn.Close()
	}
	c.connMu.Unlock()
	close(c.done)
}

// Stats returns current connector statistics
func (c *Connector) Stats() (queriesExecuted int64, uptime time.Duration, lastQuery time.Time) {
	queriesExecuted = c.queriesExecuted.Load()
	uptime = time.Since(c.startTime)
	if ts := c.lastQueryTime.Load(); ts > 0 {
		lastQuery = time.Unix(0, ts)
	}
	return
}

// ConnectError represents a classified connection error
type ConnectError struct {
	Type    string // "network", "auth", "server", "protocol"
	Message string
	Err     error
}

func (e *ConnectError) Error() string {
	return e.Message
}

func (c *Connector) connect() error {
	c.ui.Info("Connecting to %s...", extractHost(c.cfg.TunnelURL))

	dialer := websocket.Dialer{
		HandshakeTimeout: 10 * time.Second,
		TLSClientConfig:  &tls.Config{InsecureSkipVerify: c.cfg.InsecureSkipVerify},
	}

	if c.cfg.InsecureSkipVerify {
		c.ui.Warn("TLS certificate verification is disabled (insecure_skip_verify=true)")
	}

	headers := http.Header{}
	headers.Set("User-Agent", "ch-ui-agent/1.0")

	conn, dialResp, err := dialer.DialContext(c.ctx, c.cfg.TunnelURL, headers)
	if err != nil {
		dialErr := err
		if dialResp != nil {
			body, _ := io.ReadAll(io.LimitReader(dialResp.Body, 2048))
			dialResp.Body.Close()
			if len(body) > 0 {
				dialErr = fmt.Errorf("%w (status=%d body=%q)", err, dialResp.StatusCode, strings.TrimSpace(string(body)))
			} else {
				dialErr = fmt.Errorf("%w (status=%d)", err, dialResp.StatusCode)
			}
		}

		c.ui.ConnectionError(dialErr, c.cfg.TunnelURL)
		return &ConnectError{Type: "network", Message: "Failed to connect to CH-UI server", Err: dialErr}
	}

	c.connMu.Lock()
	c.conn = conn
	c.connMu.Unlock()

	// Send auth message
	authMsg := AgentMessage{
		Type:     MsgTypeAuth,
		Token:    c.cfg.Token,
		Takeover: c.cfg.Takeover,
	}

	if err := c.send(authMsg); err != nil {
		conn.Close()
		c.ui.ConnectionError(err, c.cfg.TunnelURL)
		return &ConnectError{Type: "network", Message: "Failed to send authentication", Err: err}
	}

	c.ui.Debug("Auth message sent, waiting for response...")

	// Wait for auth response
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	_, message, err := conn.ReadMessage()
	if err != nil {
		conn.Close()
		c.ui.ConnectionError(err, c.cfg.TunnelURL)
		return &ConnectError{Type: "network", Message: "Failed to receive auth response", Err: err}
	}
	conn.SetReadDeadline(time.Time{}) // Clear deadline

	var authResp GatewayMessage
	if err := json.Unmarshal(message, &authResp); err != nil {
		conn.Close()
		c.ui.DiagnosticError(ui.ErrorTypeServer, "CH-UI Server",
			"Received invalid response from server",
			[]string{
				"The server may be running an incompatible version",
				"Try updating the agent to the latest version",
				"Contact support if the issue persists",
			})
		return &ConnectError{Type: "protocol", Message: "Invalid server response", Err: err}
	}

	switch authResp.Type {
	case MsgTypeAuthOK:
		c.authenticated = true
		c.reconnectDelay = c.cfg.ReconnectDelay // Reset on successful connection
		c.ui.Success("Authenticated successfully")
		c.ui.Success("Tunnel established")
		c.ui.Status(c.cfg.TunnelURL, c.cfg.ClickHouseURL, time.Since(c.startTime))
		return nil

	case MsgTypeAuthError:
		conn.Close()
		// Server may send error in either "error" or "message" field
		errMsg := authResp.Error
		if errMsg == "" {
			errMsg = authResp.Message
		}
		if errMsg == "" {
			errMsg = "Authentication failed (no details provided)"
		}
		if isPermanentAuthError(errMsg) {
			c.ui.AuthError(errMsg)
			return &ConnectError{Type: "auth", Message: errMsg}
		}

		c.ui.Warn("Server temporarily rejected authentication: %s", errMsg)
		return &ConnectError{Type: "server", Message: errMsg}

	default:
		conn.Close()
		c.ui.DiagnosticError(ui.ErrorTypeServer, "CH-UI Server",
			fmt.Sprintf("Unexpected response type: %s", authResp.Type),
			[]string{
				"The server may be running an incompatible version",
				"Try updating the agent to the latest version",
			})
		return &ConnectError{Type: "protocol", Message: fmt.Sprintf("Unexpected response: %s", authResp.Type)}
	}
}

func isPermanentAuthError(msg string) bool {
	lower := strings.ToLower(strings.TrimSpace(msg))
	if lower == "" {
		return false
	}

	return strings.Contains(lower, "invalid tunnel token") ||
		strings.Contains(lower, "invalid token") ||
		strings.Contains(lower, "revoked")
}

func (c *Connector) messageLoop() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.connMu.Lock()
		conn := c.conn
		c.connMu.Unlock()

		if conn == nil {
			time.Sleep(100 * time.Millisecond)
			continue
		}

		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				c.ui.Disconnected("server closed connection")
			} else {
				c.ui.Disconnected(err.Error())
			}

			c.connMu.Lock()
			c.conn = nil
			c.authenticated = false
			c.connMu.Unlock()

			// Attempt reconnection
			c.reconnect()
			continue
		}

		var msg GatewayMessage
		if err := json.Unmarshal(message, &msg); err != nil {
			c.ui.Debug("Invalid message: %v", err)
			continue
		}

		c.handleMessage(msg)
	}
}

func (c *Connector) handleMessage(msg GatewayMessage) {
	switch msg.Type {
	case MsgTypePing:
		c.send(AgentMessage{Type: MsgTypePong})

	case MsgTypeQuery:
		go c.executeQuery(msg)

	case MsgTypeQueryStream:
		go c.executeStreamQuery(msg)

	case MsgTypeTestConnection:
		go c.testConnection(msg)

	case MsgTypeCancelQuery:
		c.ui.Debug("Cancel query requested for %s (not implemented)", msg.QueryID)

	default:
		c.ui.Debug("Unknown message type: %s", msg.Type)
	}
}

func (c *Connector) executeQuery(msg GatewayMessage) {
	start := time.Now()
	queryID := msg.QueryID
	sql := msg.Query

	format := msg.Format // "" or "JSON" = legacy, "JSONCompact" = tier 1

	// If a compact format is requested, use ExecuteRaw to avoid intermediate parsing
	if format != "" && format != "JSON" {
		raw, err := c.chClient.ExecuteRaw(c.ctx, sql, msg.User, msg.Password, format)
		elapsed := time.Since(start)

		if err != nil {
			c.ui.QueryError(queryID, err)
			c.send(AgentMessage{
				Type:    MsgTypeQueryError,
				QueryID: queryID,
				Error:   err.Error(),
			})
			return
		}

		c.queriesExecuted.Add(1)
		c.lastQueryTime.Store(time.Now().UnixNano())
		c.ui.QueryLog(queryID, elapsed, 0)

		// Send raw bytes directly — no intermediate parse/reserialize
		c.send(AgentMessage{
			Type:    MsgTypeQueryResult,
			QueryID: queryID,
			Data:    raw,
		})
		return
	}

	// Legacy JSON path — parse into structured result
	result, err := c.chClient.Execute(c.ctx, sql, msg.User, msg.Password)
	elapsed := time.Since(start)

	if err != nil {
		c.ui.QueryError(queryID, err)
		c.send(AgentMessage{
			Type:    MsgTypeQueryError,
			QueryID: queryID,
			Error:   err.Error(),
		})
		return
	}

	c.queriesExecuted.Add(1)
	c.lastQueryTime.Store(time.Now().UnixNano())

	rows := len(result.Data)
	c.ui.QueryLog(queryID, elapsed, rows)

	c.send(AgentMessage{
		Type:    MsgTypeQueryResult,
		QueryID: queryID,
		Data:    result.Data,
		Meta:    result.Meta,
		Stats: &QueryStats{
			Elapsed:   result.Statistics.Elapsed,
			RowsRead:  result.Statistics.RowsRead,
			BytesRead: result.Statistics.BytesRead,
		},
	})
}

func (c *Connector) executeStreamQuery(msg GatewayMessage) {
	start := time.Now()
	queryID := msg.QueryID
	sql := msg.Query

	c.ui.Debug("Stream query %s: %s", queryID, truncateStr(sql, 80))

	// Send chunks as they arrive
	onMeta := func(meta json.RawMessage) error {
		return c.send(AgentMessage{
			Type:    MsgTypeQueryStreamStart,
			QueryID: queryID,
			Meta:    meta,
		})
	}

	onChunk := func(seq int, data json.RawMessage) error {
		return c.send(AgentMessage{
			Type:    MsgTypeQueryStreamChunk,
			QueryID: queryID,
			Data:    data,
			Seq:     seq,
		})
	}

	_, totalRows, err := c.chClient.ExecuteStreaming(c.ctx, sql, msg.User, msg.Password, 5000, onMeta, onChunk)
	elapsed := time.Since(start)

	if err != nil {
		c.ui.QueryError(queryID, err)
		c.send(AgentMessage{
			Type:    MsgTypeQueryStreamError,
			QueryID: queryID,
			Error:   err.Error(),
		})
		return
	}

	c.queriesExecuted.Add(1)
	c.lastQueryTime.Store(time.Now().UnixNano())
	c.ui.QueryLog(queryID, elapsed, int(totalRows))

	c.send(AgentMessage{
		Type:      MsgTypeQueryStreamEnd,
		QueryID:   queryID,
		TotalRows: totalRows,
		Stats: &QueryStats{
			Elapsed: elapsed.Seconds(),
		},
	})
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

func (c *Connector) testConnection(msg GatewayMessage) {
	version, err := c.chClient.TestConnection(c.ctx, msg.User, msg.Password)

	if err != nil {
		c.ui.Debug("Connection test failed: %v", err)
		c.send(AgentMessage{
			Type:    MsgTypeTestResult,
			QueryID: msg.QueryID,
			Online:  false,
			Error:   err.Error(),
		})
		return
	}

	c.ui.Debug("Connection test successful, version: %s", version)
	c.send(AgentMessage{
		Type:    MsgTypeTestResult,
		QueryID: msg.QueryID,
		Online:  true,
		Version: version,
	})
}

func (c *Connector) heartbeatLoop() {
	ticker := time.NewTicker(c.cfg.HeartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.connMu.Lock()
			conn := c.conn
			authenticated := c.authenticated
			if conn != nil && authenticated {
				if err := conn.WriteControl(websocket.PingMessage, nil, time.Now().Add(5*time.Second)); err != nil {
					c.ui.Debug("Heartbeat failed: %v", err)
				}
			}
			c.connMu.Unlock()
		}
	}
}

// sendHostInfo collects and sends host machine metrics to the server
func (c *Connector) sendHostInfo() {
	hostInfo := CollectHostInfo(c.startTime)

	if err := c.send(AgentMessage{
		Type:     MsgTypeHostInfo,
		HostInfo: hostInfo,
	}); err != nil {
		c.ui.Debug("Failed to send host info: %v", err)
	} else {
		c.ui.Debug("Host info sent (CPU: %d cores, Mem: %d MB, Disk: %d GB)",
			hostInfo.CPUCores,
			hostInfo.MemoryTotal/(1024*1024),
			hostInfo.DiskTotal/(1024*1024*1024))
	}
}

// hostInfoLoop sends host info periodically (every 60 seconds)
func (c *Connector) hostInfoLoop() {
	// Send initial host info after a short delay to allow auth to complete
	time.Sleep(2 * time.Second)
	c.sendHostInfo()

	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.ctx.Done():
			return
		case <-ticker.C:
			c.connMu.Lock()
			authenticated := c.authenticated
			c.connMu.Unlock()

			if authenticated {
				c.sendHostInfo()
			}
		}
	}
}

func (c *Connector) reconnect() {
	for {
		select {
		case <-c.ctx.Done():
			return
		default:
		}

		c.ui.Reconnecting(c.reconnectDelay)
		time.Sleep(c.reconnectDelay)

		// Exponential backoff
		c.reconnectDelay *= 2
		if c.reconnectDelay > c.cfg.MaxReconnectDelay {
			c.reconnectDelay = c.cfg.MaxReconnectDelay
		}

		if err := c.connect(); err != nil {
			if ce, ok := err.(*ConnectError); ok && ce.Type == "auth" {
				c.ui.Error("Authentication failed — stopping reconnection (token is invalid or revoked)")
				close(c.done)
				return
			}
			c.ui.Error("Reconnection failed: %v", err)
			continue
		}

		return
	}
}

func (c *Connector) send(msg AgentMessage) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return err
	}

	return c.conn.WriteMessage(websocket.TextMessage, data)
}

func extractHost(urlStr string) string {
	parsed, err := url.Parse(urlStr)
	if err == nil && parsed.Host != "" {
		if hostname := parsed.Hostname(); hostname != "" {
			return hostname
		}
		return parsed.Host
	}

	trimmed := strings.TrimPrefix(strings.TrimPrefix(urlStr, "wss://"), "ws://")
	for i, c := range trimmed {
		if c == '/' || c == ':' {
			return trimmed[:i]
		}
	}
	return trimmed
}
