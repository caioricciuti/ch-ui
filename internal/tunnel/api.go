package tunnel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
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
	return true, t.seenAt()
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
//
// A user whose ClickHouse profile is already readonly=1 may not change any
// setting, so ClickHouse refuses the query naming that setting. In that case
// the setting is dropped and the query retried: readonly=1 itself and bind
// parameters are always accepted, and the profile already blocks writes.
func (g *Gateway) ExecuteQueryWithSettingsCtx(ctx context.Context, connectionID, sql, user, password string, settings map[string]string, timeout time.Duration) (*QueryResult, error) {
	result, err := g.executeQuery(ctx, connectionID, sql, user, password, settings, timeout)
	for attempt := 0; attempt < MaxRefusedSettings && err != nil; attempt++ {
		name := DroppableRefusedSetting(err, sql, settings)
		if name == "" {
			break
		}
		settings = withoutSetting(settings, name)
		slog.Debug("ClickHouse profile is read only, retrying without setting", "setting", name, "user", user)
		result, err = g.executeQuery(ctx, connectionID, sql, user, password, settings, timeout)
	}
	return result, err
}

// DroppableRefusedSetting returns the setting a readonly=1 profile refused in
// err when it is one of ours and may be dropped for a retry, else "".
// readonly itself and bind parameters are always accepted, and a setting the
// statement names itself is the query's own change, not ours.
func DroppableRefusedSetting(err error, sql string, settings map[string]string) string {
	name := RefusedSetting(err)
	if _, ok := settings[name]; !ok || name == "readonly" || strings.HasPrefix(name, "param_") || sqlMentionsSetting(sql, name) {
		return ""
	}
	return name
}

// sqlMentionsSetting reports whether the statement itself names setting, for
// example in a SETTINGS clause. ClickHouse then refuses the query's own change,
// not ours, and dropping our value would let the query lift the cap it set.
func sqlMentionsSetting(sql, setting string) bool {
	lower := strings.ToLower(sql)
	name := strings.ToLower(setting)
	for i := 0; ; {
		j := strings.Index(lower[i:], name)
		if j < 0 {
			return false
		}
		start, end := i+j, i+j+len(name)
		if !isIdentByte(lower, start-1) && !isIdentByte(lower, end) {
			return true
		}
		i = end
	}
}

func isIdentByte(s string, i int) bool {
	if i < 0 || i >= len(s) {
		return false
	}
	c := s[i]
	return c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

// MaxRefusedSettings bounds the retries for a read-only profile.
const MaxRefusedSettings = 8

var refusedSettingPattern = regexp.MustCompile(`Cannot modify '([A-Za-z0-9_]+)' setting in readonly mode`)

// RefusedSetting returns the setting ClickHouse refused because the user's
// profile is read only, or "" for any other error.
func RefusedSetting(err error) string {
	if err == nil {
		return ""
	}
	m := refusedSettingPattern.FindStringSubmatch(err.Error())
	if m == nil {
		return ""
	}
	return m[1]
}

func withoutSetting(settings map[string]string, name string) map[string]string {
	out := make(map[string]string, len(settings))
	for k, v := range settings {
		if k != name {
			out[k] = v
		}
	}
	return out
}

func (g *Gateway) executeQuery(ctx context.Context, connectionID, sql, user, password string, settings map[string]string, timeout time.Duration) (*QueryResult, error) {
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

	err := t.sendJSON(msg)
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
	_ = t.sendJSON(cancel)
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

	err := t.sendJSON(msg)
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
		t.sendCancel(requestID)
		return nil, errors.New("query timeout")
	}
}

// ExecuteStreamQuery sends a streaming query to the agent and returns channels for progressive consumption.
// The caller reads stream.ChunkCh until stream.Done() is closed, drains any
// chunks still buffered, then reads stream.DoneCh or stream.ErrorCh.
// Call CleanupStream when done to release resources; it also unblocks the
// tunnel if the caller stopped reading early.
func (g *Gateway) ExecuteStreamQuery(connectionID, sql, user, password string, settings map[string]string) (requestID string, stream *PendingStreamRequest, err error) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return "", nil, errors.New("tunnel not connected")
	}
	t := val.(*ConnectedTunnel)

	requestID = uuid.NewString()
	stream = newPendingStream()
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

	wsErr := t.sendJSON(msg)
	if wsErr != nil {
		if _, ok := t.Pending.LoadAndDelete(requestID); ok {
			stream.finish()
		}
		return "", nil, wsErr
	}

	return requestID, stream, nil
}

// ErrExportUnsupported is returned for an agent that predates server-side export.
var ErrExportUnsupported = errors.New("this connection's agent does not support full result export; update the agent (ch-ui-agent) to export")

// ExecuteExportQuery asks the agent to run sql with FORMAT format and stream
// the raw output back. It is consumed like ExecuteStreamQuery: every chunk on
// stream.ChunkCh is a JSON string holding base64 bytes (decode with
// DecodeExportChunk), DoneCh carries a StreamDone with the byte count. Call
// CleanupStream when finished and CancelStreamQuery to stop it early.
func (g *Gateway) ExecuteExportQuery(connectionID, sql, user, password, format string, settings map[string]string) (requestID string, stream *PendingStreamRequest, err error) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return "", nil, errors.New("tunnel not connected")
	}
	t := val.(*ConnectedTunnel)
	if !t.Supports(CapabilityExport) {
		return "", nil, ErrExportUnsupported
	}

	requestID = uuid.NewString()
	stream = newPendingStream()
	t.Pending.Store(requestID, stream)

	wsErr := t.sendJSON(GatewayMessage{
		Type:     "query_export",
		ID:       requestID,
		QueryID:  requestID,
		Query:    sql,
		User:     user,
		Password: password,
		Format:   format,
		Settings: settings,
	})
	if wsErr != nil {
		if _, ok := t.Pending.LoadAndDelete(requestID); ok {
			stream.finish()
		}
		return "", nil, wsErr
	}
	return requestID, stream, nil
}

// DecodeExportChunk turns an export chunk (a JSON base64 string) into bytes.
func DecodeExportChunk(chunk json.RawMessage) ([]byte, error) {
	var b []byte
	if err := json.Unmarshal(chunk, &b); err != nil {
		return nil, fmt.Errorf("invalid export chunk: %w", err)
	}
	return b, nil
}

// CancelStreamQuery tells the agent to abort a streaming query it is still
// running, so a query the user cancelled stops consuming ClickHouse resources.
func (g *Gateway) CancelStreamQuery(connectionID, requestID string) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)

	err := t.sendJSON(GatewayMessage{
		Type:    "cancel_query",
		ID:      requestID,
		QueryID: requestID,
	})

	if err != nil {
		slog.Debug("Failed to send query cancellation", "request_id", requestID, "error", err)
	}
}

// CleanupStream removes a pending stream request from the tunnel's pending map
// and releases a read loop that may be waiting to hand it a chunk.
// Call this when the HTTP handler finishes (completion, error, or client disconnect).
func (g *Gateway) CleanupStream(connectionID, requestID string) {
	val, ok := g.tunnels.Load(connectionID)
	if !ok {
		return
	}
	t := val.(*ConnectedTunnel)
	if v, ok := t.Pending.LoadAndDelete(requestID); ok {
		if p, ok := v.(*PendingStreamRequest); ok {
			p.finish()
		}
	}
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

	err := t.sendJSON(msg)
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
