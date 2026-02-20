package tunnel

import "encoding/json"

// AgentMessage represents messages from the tunnel agent to the gateway.
type AgentMessage struct {
	Type       string          `json:"type"`
	Token      string          `json:"token,omitempty"`      // auth
	Takeover   bool            `json:"takeover,omitempty"`   // auth (replace active session)
	ID         string          `json:"id,omitempty"`         // legacy JS agent
	QueryID    string          `json:"query_id,omitempty"`   // Go agent
	Data       json.RawMessage `json:"data,omitempty"`       // query_result, query_stream_chunk
	Meta       json.RawMessage `json:"meta,omitempty"`       // query_result, query_stream_start
	Stats      json.RawMessage `json:"stats,omitempty"`      // query_result (legacy)
	Statistics json.RawMessage `json:"statistics,omitempty"` // query_result (Go agent), query_stream_end
	Error      string          `json:"error,omitempty"`      // query_error, query_stream_error
	Success    *bool           `json:"success,omitempty"`    // test_result (legacy)
	Online     *bool           `json:"online,omitempty"`     // test_result (Go agent)
	Version    string          `json:"version,omitempty"`    // test_result
	HostInfo   json.RawMessage `json:"host_info,omitempty"`  // host_info
	Seq        int             `json:"seq,omitempty"`        // query_stream_chunk sequence number
	TotalRows  int64           `json:"total_rows,omitempty"` // query_stream_end total row count
}

// GetMessageID returns the message ID from either legacy or Go agent format.
func (m *AgentMessage) GetMessageID() string {
	if m.QueryID != "" {
		return m.QueryID
	}
	return m.ID
}

// GetStats returns stats from either legacy or Go agent format.
func (m *AgentMessage) GetStats() json.RawMessage {
	if len(m.Stats) > 0 {
		return m.Stats
	}
	return m.Statistics
}

// IsTestSuccess normalizes the test result between legacy and Go agent.
func (m *AgentMessage) IsTestSuccess() bool {
	if m.Success != nil {
		return *m.Success
	}
	if m.Online != nil {
		return *m.Online
	}
	return false
}

// GatewayMessage represents messages from the gateway to the tunnel agent.
type GatewayMessage struct {
	Type           string `json:"type"`
	ConnectionID   string `json:"connectionId,omitempty"`   // auth_ok
	ConnectionName string `json:"connectionName,omitempty"` // auth_ok
	Message        string `json:"message,omitempty"`        // auth_error
	ID             string `json:"id,omitempty"`             // legacy JS agent
	QueryID        string `json:"query_id,omitempty"`       // Go agent
	SQL            string `json:"sql,omitempty"`            // query (legacy)
	Query          string `json:"query,omitempty"`          // query (Go agent)
	User           string `json:"user,omitempty"`           // query, test
	Password       string `json:"password,omitempty"`       // query, test
	Format         string `json:"format,omitempty"`         // query
}

// QueryResult represents a ClickHouse query result returned from the agent.
type QueryResult struct {
	Data  json.RawMessage `json:"data"`
	Meta  json.RawMessage `json:"meta"`
	Stats json.RawMessage `json:"stats"`
}

// TestResult represents a connection test result returned from the agent.
type TestResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
	Version string `json:"version,omitempty"`
}

// StreamDone represents the final payload of a streaming query.
type StreamDone struct {
	Statistics json.RawMessage `json:"statistics"`
	TotalRows  int64          `json:"total_rows"`
}

// HostInfo represents machine info from the agent.
type HostInfo struct {
	Hostname    string  `json:"hostname"`
	OS          string  `json:"os"`
	Arch        string  `json:"arch"`
	CPUCores    int     `json:"cpu_cores"`
	MemoryTotal int64   `json:"memory_total"`
	MemoryFree  int64   `json:"memory_free"`
	DiskTotal   int64   `json:"disk_total"`
	DiskFree    int64   `json:"disk_free"`
	GoVersion   string  `json:"go_version"`
	AgentUptime float64 `json:"agent_uptime"`
	CollectedAt string  `json:"collected_at"`
}
