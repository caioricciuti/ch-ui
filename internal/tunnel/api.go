package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

// IsTunnelOnline checks if a tunnel connection is currently active.
func (g *Gateway) IsTunnelOnline(connectionID string) bool {
	_, ok := g.tunnels.Load(connectionID)
	return ok
}

// GetTunnelStatus returns the online status and last seen time for a connection.
func (g *Gateway) GetTunnelStatus(connectionID string) (online bool, lastSeen time.Time) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return false, time.Time{}
	}
	t := val.(*ConnectedTunnel)
	return true, t.LastSeen
}

// GetConnectedCount returns the number of currently connected tunnels.
func (g *Gateway) GetConnectedCount() int {
	count := 0
	g.tunnels.Range(func(_, _ any) bool {
		count++
		return true
	})
	return count
}

// ExecuteQuery sends a SQL query to the agent via the tunnel and waits for a result.
func (g *Gateway) ExecuteQuery(connectionID, sql, user, password string, timeout time.Duration) (*QueryResult, error) {
	return g.ExecuteQueryWithSettings(connectionID, sql, user, password, nil, timeout)
}

// ExecuteQueryWithSettings is like ExecuteQuery but forwards ClickHouse query
// settings — including bind parameters (param_<name>) — to the agent, which
// passes them to ClickHouse as URL params.
func (g *Gateway) ExecuteQueryWithSettings(connectionID, sql, user, password string, settings map[string]string, timeout time.Duration) (*QueryResult, error) {
	return g.ExecuteQueryWithSettingsCtx(context.Background(), connectionID, sql, user, password, settings, timeout)
}

// ExecuteQueryWithSettingsCtx is ExecuteQueryWithSettings bound to a context:
// when ctx is cancelled before the agent answers, the query is cancelled on
// the agent side too and ctx.Err() is returned.
func (g *Gateway) ExecuteQueryWithSettingsCtx(ctx context.Context, connectionID, sql, user, password string, settings map[string]string, timeout time.Duration) (*QueryResult, error) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return nil, errors.New("tunnel not connected")
	}
	t := val.(*ConnectedTunnel)

	requestID := uuid.NewString()
	pending := &PendingRequest{
		ResultCh: make(chan json.RawMessage, 1),
		ErrorCh:  make(chan error, 1),
	}
	t.Pending.Store(requestID, pending)
	defer t.Pending.Delete(requestID)

	msg := GatewayMessage{
		Type:     "query",
		ID:       requestID,
		QueryID:  requestID,
		SQL:      sql,
		Query:    sql,
		User:     user,
		Password: password,
		Settings: settings,
	}

	data, _ := json.Marshal(msg)
	t.mu.Lock()
	err := t.WS.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case payload := <-pending.ResultCh:
		var result QueryResult
		if err := json.Unmarshal(payload, &result); err != nil {
			return nil, err
		}
		return &result, nil
	case err := <-pending.ErrorCh:
		return nil, err
	case <-ctx.Done():
		t.sendCancel(requestID)
		return nil, ctx.Err()
	case <-time.After(timeout):
		t.sendCancel(requestID)
		return nil, errors.New("query timeout")
	}
}

// sendCancel asks the agent to kill a query it is still running.
func (t *ConnectedTunnel) sendCancel(requestID string) {
	cancel := GatewayMessage{
		Type:    "cancel_query",
		ID:      requestID,
		QueryID: requestID,
	}
	cancelData, _ := json.Marshal(cancel)
	t.mu.Lock()
	t.WS.WriteMessage(websocket.TextMessage, cancelData)
	t.mu.Unlock()
}

// ExecuteQueryWithFormat sends a SQL query with a specific output format and returns the raw result.
// The format parameter (e.g. "JSONCompact") is passed to the agent, which appends FORMAT <format> to the query.
// Returns the raw ClickHouse response as-is (no intermediate parse/reserialize).
func (g *Gateway) ExecuteQueryWithFormat(connectionID, sql, user, password, format string, timeout time.Duration) (json.RawMessage, error) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return nil, errors.New("tunnel not connected")
	}
	t := val.(*ConnectedTunnel)

	requestID := uuid.NewString()
	pending := &PendingRequest{
		ResultCh: make(chan json.RawMessage, 1),
		ErrorCh:  make(chan error, 1),
	}
	t.Pending.Store(requestID, pending)
	defer t.Pending.Delete(requestID)

	msg := GatewayMessage{
		Type:     "query",
		ID:       requestID,
		QueryID:  requestID,
		SQL:      sql,
		Query:    sql,
		User:     user,
		Password: password,
		Format:   format,
	}

	data, _ := json.Marshal(msg)
	t.mu.Lock()
	err := t.WS.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case payload := <-pending.ResultCh:
		// The payload is a marshaled QueryResult{Data, Meta, Stats}.
		// For format-aware queries the agent puts the raw CH response in Data.
		var result QueryResult
		if err := json.Unmarshal(payload, &result); err != nil {
			return payload, nil // fallback: return as-is
		}
		if len(result.Data) > 0 {
			return result.Data, nil
		}
		return payload, nil
	case err := <-pending.ErrorCh:
		return nil, err
	case <-time.After(timeout):
		cancel := GatewayMessage{
			Type:    "cancel_query",
			ID:      requestID,
			QueryID: requestID,
		}
		cancelData, _ := json.Marshal(cancel)
		t.mu.Lock()
		t.WS.WriteMessage(websocket.TextMessage, cancelData)
		t.mu.Unlock()
		return nil, errors.New("query timeout")
	}
}

// ExecuteStreamQuery sends a streaming query to the agent and returns channels for progressive consumption.
// The caller must range over stream.ChunkCh, then select on stream.DoneCh/ErrorCh.
// Call CleanupStream when done to release resources.
func (g *Gateway) ExecuteStreamQuery(connectionID, sql, user, password string, settings map[string]string) (requestID string, stream *PendingStreamRequest, err error) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return "", nil, errors.New("tunnel not connected")
	}
	t := val.(*ConnectedTunnel)

	requestID = uuid.NewString()
	stream = &PendingStreamRequest{
		MetaCh:     make(chan json.RawMessage, 1),
		ChunkCh:    make(chan json.RawMessage, 8),
		ProgressCh: make(chan json.RawMessage, 1),
		DoneCh:     make(chan json.RawMessage, 1),
		ErrorCh:    make(chan error, 1),
	}
	t.Pending.Store(requestID, stream)

	msg := GatewayMessage{
		Type:     "query_stream",
		ID:       requestID,
		QueryID:  requestID,
		SQL:      sql,
		Query:    sql,
		User:     user,
		Password: password,
		Settings: settings,
	}

	data, _ := json.Marshal(msg)
	t.mu.Lock()
	wsErr := t.WS.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()
	if wsErr != nil {
		t.Pending.Delete(requestID)
		return "", nil, wsErr
	}

	return requestID, stream, nil
}

// CancelStreamQuery tells the agent to abort a streaming query it is still
// running, so a query the user cancelled stops consuming ClickHouse resources.
func (g *Gateway) CancelStreamQuery(connectionID, requestID string) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	data, _ := json.Marshal(GatewayMessage{
		Type:    "cancel_query",
		ID:      requestID,
		QueryID: requestID,
	})

	t.mu.Lock()
	err := t.WS.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()
	if err != nil {
		slog.Debug("Failed to send query cancellation", "request_id", requestID, "error", err)
	}
}

// CleanupStream removes a pending stream request from the tunnel's pending map.
// Call this when the HTTP handler finishes (completion, error, or client disconnect).
func (g *Gateway) CleanupStream(connectionID, requestID string) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)
	t.Pending.Delete(requestID)
}

// TestConnection tests a ClickHouse connection through the tunnel.
func (g *Gateway) TestConnection(connectionID, user, password string, timeout time.Duration) (*TestResult, error) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return nil, errors.New("tunnel not connected")
	}
	t := val.(*ConnectedTunnel)

	requestID := uuid.NewString()
	pending := &PendingRequest{
		ResultCh: make(chan json.RawMessage, 1),
		ErrorCh:  make(chan error, 1),
	}
	t.Pending.Store(requestID, pending)
	defer t.Pending.Delete(requestID)

	msg := GatewayMessage{
		Type:     "test_connection",
		ID:       requestID,
		QueryID:  requestID,
		User:     user,
		Password: password,
	}

	data, _ := json.Marshal(msg)
	t.mu.Lock()
	err := t.WS.WriteMessage(websocket.TextMessage, data)
	t.mu.Unlock()
	if err != nil {
		return nil, err
	}

	select {
	case payload := <-pending.ResultCh:
		var result TestResult
		if err := json.Unmarshal(payload, &result); err != nil {
			return nil, err
		}
		return &result, nil
	case err := <-pending.ErrorCh:
		return nil, err
	case <-time.After(timeout):
		return nil, errors.New("connection test timeout")
	}
}
