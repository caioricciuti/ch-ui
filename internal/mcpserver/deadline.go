package mcpserver

import (
	"net/http"
	"time"
)

// clearWriteDeadline lifts the server-wide WriteTimeout for one response.
//
// A subscriptions/listen POST (protocol 2026-07-28, which Claude Code speaks)
// is a server-sent event stream that stays open until the client cancels it.
// The server's five-minute WriteTimeout would otherwise close it mid-stream:
// the client then reconnects every few minutes and the proxy logs each cut
// as an aborted request. Tool calls stay bounded by their ClickHouse
// timeouts, and a dead peer is still reaped by TCP keepalive.
//
// Best effort: a ResponseWriter wrapper without Unwrap reports
// ErrNotSupported, and then the server-wide timeout still applies.
func clearWriteDeadline(w http.ResponseWriter) {
	_ = http.NewResponseController(w).SetWriteDeadline(time.Time{})
}
