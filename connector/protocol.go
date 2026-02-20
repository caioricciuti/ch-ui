package connector

// GatewayMessage represents messages received from the CH-UI Cloud tunnel server
type GatewayMessage struct {
	Type     string `json:"type"`               // Message type: auth_ok, auth_error, query, query_stream, ping, cancel_query, test_connection
	QueryID  string `json:"query_id,omitempty"` // Query identifier
	Query    string `json:"query,omitempty"`    // SQL query to execute
	User     string `json:"user,omitempty"`     // ClickHouse username for this query
	Password string `json:"password,omitempty"` // ClickHouse password for this query
	Format   string `json:"format,omitempty"`   // ClickHouse output format (JSONCompact, stream, etc.)
	Error    string `json:"error,omitempty"`    // Error message (for auth_error)
	Message  string `json:"message,omitempty"`  // Additional message info
}

// AgentMessage represents messages sent to the CH-UI Cloud tunnel server
type AgentMessage struct {
	Type      string      `json:"type"`               // Message type: auth, pong, query_result, query_error, test_result, host_info, query_stream_*
	QueryID   string      `json:"query_id,omitempty"` // Query identifier (for query responses)
	Token     string      `json:"token,omitempty"`    // Tunnel token (for auth message)
	Takeover  bool        `json:"takeover,omitempty"` // Request takeover of an existing session for this token
	Data      interface{} `json:"data,omitempty"`     // Query result data
	Meta      interface{} `json:"meta,omitempty"`     // Query result metadata
	Stats     *QueryStats `json:"statistics,omitempty"`
	Error     string      `json:"error,omitempty"`     // Error message
	Version   string      `json:"version,omitempty"`   // ClickHouse version (for test_result)
	Online    bool        `json:"online,omitempty"`    // Connection status (for test_result)
	HostInfo  *HostInfo   `json:"host_info,omitempty"` // Host machine metrics
	Seq       int         `json:"seq,omitempty"`       // Chunk sequence number (for streaming)
	TotalRows int64       `json:"total_rows,omitempty"` // Total row count (for streaming)
}

// QueryStats contains query execution statistics
type QueryStats struct {
	Elapsed   float64 `json:"elapsed"`
	RowsRead  uint64  `json:"rows_read"`
	BytesRead uint64  `json:"bytes_read"`
}

// Message types from gateway
const (
	MsgTypeAuthOK         = "auth_ok"
	MsgTypeAuthError      = "auth_error"
	MsgTypeQuery          = "query"
	MsgTypeQueryStream    = "query_stream"
	MsgTypePing           = "ping"
	MsgTypeCancelQuery    = "cancel_query"
	MsgTypeTestConnection = "test_connection"
)

// Message types to gateway
const (
	MsgTypeAuth              = "auth"
	MsgTypePong              = "pong"
	MsgTypeQueryResult       = "query_result"
	MsgTypeQueryError        = "query_error"
	MsgTypeTestResult        = "test_result"
	MsgTypeHostInfo          = "host_info"
	MsgTypeQueryStreamStart  = "query_stream_start"
	MsgTypeQueryStreamChunk  = "query_stream_chunk"
	MsgTypeQueryStreamEnd    = "query_stream_end"
	MsgTypeQueryStreamError  = "query_stream_error"
)
