package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Connection represents a connection record (agent or embedded).
type Connection struct {
	ID           string  `json:"id"`
	Name         string  `json:"name"`
	TunnelToken  string  `json:"tunnel_token"`
	IsEmbedded   bool    `json:"is_embedded"`
	Status       string  `json:"status"`
	LastSeenAt   *string `json:"last_seen_at"`
	HostInfoJSON *string `json:"host_info"`
	CreatedAt    string  `json:"created_at"`
}

// HostInfo represents the host machine metrics reported by the tunnel agent.
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

// GetConnections retrieves all connections ordered by creation date.
func (db *DB) GetConnections() ([]Connection, error) {
	rows, err := db.conn.Query(
		"SELECT id, name, tunnel_token, is_embedded, status, last_seen_at, host_info, created_at FROM connections ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("get connections: %w", err)
	}
	defer rows.Close()

	var conns []Connection
	for rows.Next() {
		var c Connection
		var lastSeenAt, hostInfo sql.NullString
		var isEmbedded int
		if err := rows.Scan(&c.ID, &c.Name, &c.TunnelToken, &isEmbedded, &c.Status, &lastSeenAt, &hostInfo, &c.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		c.IsEmbedded = isEmbedded == 1
		c.LastSeenAt = nullStringToPtr(lastSeenAt)
		c.HostInfoJSON = nullStringToPtr(hostInfo)
		conns = append(conns, c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection rows: %w", err)
	}
	return conns, nil
}

// GetConnectionByToken retrieves a connection by its tunnel token.
func (db *DB) GetConnectionByToken(token string) (*Connection, error) {
	row := db.conn.QueryRow(
		"SELECT id, name, tunnel_token, is_embedded, status, last_seen_at, host_info, created_at FROM connections WHERE tunnel_token = ?",
		token,
	)

	var c Connection
	var lastSeenAt, hostInfo sql.NullString
	var isEmbedded int

	err := row.Scan(
		&c.ID, &c.Name, &c.TunnelToken, &isEmbedded, &c.Status,
		&lastSeenAt, &hostInfo, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connection by token: %w", err)
	}

	c.IsEmbedded = isEmbedded == 1
	c.LastSeenAt = nullStringToPtr(lastSeenAt)
	c.HostInfoJSON = nullStringToPtr(hostInfo)
	return &c, nil
}

// GetConnectionByTokenCtx retrieves a connection by its tunnel token using a context.
// This is used by tunnel auth to avoid hanging while SQLite is busy.
func (db *DB) GetConnectionByTokenCtx(ctx context.Context, token string) (*Connection, error) {
	row := db.conn.QueryRowContext(ctx,
		"SELECT id, name, tunnel_token, is_embedded, status, last_seen_at, host_info, created_at FROM connections WHERE tunnel_token = ?",
		token,
	)

	var c Connection
	var lastSeenAt, hostInfo sql.NullString
	var isEmbedded int

	err := row.Scan(
		&c.ID, &c.Name, &c.TunnelToken, &isEmbedded, &c.Status,
		&lastSeenAt, &hostInfo, &c.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connection by token: %w", err)
	}

	c.IsEmbedded = isEmbedded == 1
	c.LastSeenAt = nullStringToPtr(lastSeenAt)
	c.HostInfoJSON = nullStringToPtr(hostInfo)
	return &c, nil
}

// GetConnectionByID retrieves a connection by its ID.
func (db *DB) GetConnectionByID(id string) (*Connection, error) {
	row := db.conn.QueryRow(
		"SELECT id, name, tunnel_token, is_embedded, status, last_seen_at, host_info, created_at FROM connections WHERE id = ?", id,
	)

	var c Connection
	var lastSeenAt, hostInfo sql.NullString
	var isEmbedded int

	err := row.Scan(&c.ID, &c.Name, &c.TunnelToken, &isEmbedded, &c.Status, &lastSeenAt, &hostInfo, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connection by id: %w", err)
	}

	c.IsEmbedded = isEmbedded == 1
	c.LastSeenAt = nullStringToPtr(lastSeenAt)
	c.HostInfoJSON = nullStringToPtr(hostInfo)
	return &c, nil
}

// GetConnectionCount returns the total number of connections.
func (db *DB) GetConnectionCount() (int, error) {
	var count int
	err := db.conn.QueryRow("SELECT COUNT(*) FROM connections").Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("get connection count: %w", err)
	}
	return count, nil
}

// CreateConnection creates a new connection and returns its ID.
func (db *DB) CreateConnection(name, token string, isEmbedded bool) (string, error) {
	id := uuid.NewString()
	embedded := 0
	if isEmbedded {
		embedded = 1
	}
	_, err := db.conn.Exec(
		"INSERT INTO connections (id, name, tunnel_token, is_embedded) VALUES (?, ?, ?, ?)",
		id, name, token, embedded,
	)
	if err != nil {
		return "", fmt.Errorf("create connection: %w", err)
	}
	return id, nil
}

// UpdateConnectionStatus updates the status and last_seen_at of a connection.
func (db *DB) UpdateConnectionStatus(id, status string) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := db.conn.Exec(
		"UPDATE connections SET status = ?, last_seen_at = ? WHERE id = ?",
		status, now, id,
	)
	if err != nil {
		return fmt.Errorf("update connection status: %w", err)
	}
	return nil
}

// DeleteConnection deletes a connection by its ID.
func (db *DB) DeleteConnection(id string) error {
	_, err := db.conn.Exec("DELETE FROM connections WHERE id = ?", id)
	if err != nil {
		return fmt.Errorf("delete connection: %w", err)
	}
	return nil
}

// UpdateConnectionToken updates the tunnel token for a connection.
func (db *DB) UpdateConnectionToken(id, newToken string) error {
	_, err := db.conn.Exec("UPDATE connections SET tunnel_token = ? WHERE id = ?", newToken, id)
	if err != nil {
		return fmt.Errorf("update connection token: %w", err)
	}
	return nil
}

// UpdateConnectionHostInfo stores the host info JSON for a connection.
func (db *DB) UpdateConnectionHostInfo(connId string, info HostInfo) error {
	data, err := json.Marshal(info)
	if err != nil {
		return fmt.Errorf("marshal host info: %w", err)
	}
	_, err = db.conn.Exec(
		"UPDATE connections SET host_info = ? WHERE id = ?",
		string(data), connId,
	)
	if err != nil {
		return fmt.Errorf("update connection host info: %w", err)
	}
	return nil
}

// GetConnectionHostInfo retrieves the parsed host info for a connection.
func (db *DB) GetConnectionHostInfo(connId string) (*HostInfo, error) {
	var hostInfoStr sql.NullString
	err := db.conn.QueryRow(
		"SELECT host_info FROM connections WHERE id = ?", connId,
	).Scan(&hostInfoStr)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connection host info: %w", err)
	}

	if !hostInfoStr.Valid || hostInfoStr.String == "" {
		return nil, nil
	}

	var info HostInfo
	if err := json.Unmarshal([]byte(hostInfoStr.String), &info); err != nil {
		return nil, nil
	}
	return &info, nil
}

// GetEmbeddedConnection retrieves the embedded connection (if any).
func (db *DB) GetEmbeddedConnection() (*Connection, error) {
	row := db.conn.QueryRow(
		"SELECT id, name, tunnel_token, is_embedded, status, last_seen_at, host_info, created_at FROM connections WHERE is_embedded = 1 LIMIT 1",
	)

	var c Connection
	var lastSeenAt, hostInfo sql.NullString
	var isEmbedded int

	err := row.Scan(&c.ID, &c.Name, &c.TunnelToken, &isEmbedded, &c.Status, &lastSeenAt, &hostInfo, &c.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get embedded connection: %w", err)
	}

	c.IsEmbedded = isEmbedded == 1
	c.LastSeenAt = nullStringToPtr(lastSeenAt)
	c.HostInfoJSON = nullStringToPtr(hostInfo)
	return &c, nil
}
