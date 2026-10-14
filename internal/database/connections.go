package database

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// Connection types.
const (
	// ConnectionTypeDirect runs an in-process connector against clickhouse_url.
	ConnectionTypeDirect = "direct"
	// ConnectionTypeTunnel waits for a remote ch-ui agent to dial in.
	ConnectionTypeTunnel = "tunnel"
)

// Connection represents a connection record (direct or tunnel).
type Connection struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Never serialized: only the admin token endpoints return it, explicitly.
	TunnelToken   string  `json:"-"`
	IsEmbedded    bool    `json:"is_embedded"`
	Type          string  `json:"type"`
	ClickHouseURL string  `json:"clickhouse_url,omitempty"`
	Status        string  `json:"status"`
	LastSeenAt    *string `json:"last_seen_at"`
	HostInfoJSON  *string `json:"host_info"`
	CreatedAt     string  `json:"created_at"`
}

const connectionColumns = "id, name, tunnel_token, is_embedded, type, clickhouse_url, status, last_seen_at, host_info, created_at"

// scanConnection scans a connection row from any row scanner (sql.Row / sql.Rows).
func scanConnection(scan func(dest ...any) error) (*Connection, error) {
	var c Connection
	var lastSeenAt, hostInfo, chURL sql.NullString
	var isEmbedded int
	if err := scan(&c.ID, &c.Name, &c.TunnelToken, &isEmbedded, &c.Type, &chURL, &c.Status, &lastSeenAt, &hostInfo, &c.CreatedAt); err != nil {
		return nil, err
	}
	c.IsEmbedded = isEmbedded == 1
	c.ClickHouseURL = chURL.String
	c.LastSeenAt = nullStringToPtr(lastSeenAt)
	c.HostInfoJSON = nullStringToPtr(hostInfo)
	return &c, nil
}

// GetConnectionSSOAccount returns the per-connection ClickHouse service account
// (username and encrypted password) used by OIDC SSO sessions. Empty strings
// mean no service account is configured for the connection.
func (db *DB) GetConnectionSSOAccount(connID string) (user string, encryptedPassword string, err error) {
	var u, p sql.NullString
	err = db.conn.QueryRow(
		"SELECT sso_ch_user, sso_ch_password_encrypted FROM connections WHERE id = ?", connID,
	).Scan(&u, &p)
	if err == sql.ErrNoRows {
		return "", "", nil
	}
	if err != nil {
		return "", "", fmt.Errorf("get connection sso account: %w", err)
	}
	return u.String, p.String, nil
}

// SetConnectionSSOAccount stores the ClickHouse service account credentials used
// by OIDC SSO sessions for a connection. The password must already be encrypted.
func (db *DB) SetConnectionSSOAccount(connID, user, encryptedPassword string) error {
	res, err := db.conn.Exec(
		"UPDATE connections SET sso_ch_user = ?, sso_ch_password_encrypted = ? WHERE id = ?",
		user, encryptedPassword, connID,
	)
	if err != nil {
		return fmt.Errorf("set connection sso account: %w", err)
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("connection not found: %s", connID)
	}
	return nil
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
		"SELECT " + connectionColumns + " FROM connections ORDER BY created_at ASC",
	)
	if err != nil {
		return nil, fmt.Errorf("get connections: %w", err)
	}
	defer rows.Close()

	var conns []Connection
	for rows.Next() {
		c, err := scanConnection(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		conns = append(conns, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection rows: %w", err)
	}
	return conns, nil
}

// GetDirectConnections retrieves all direct (in-process connector) connections.
func (db *DB) GetDirectConnections() ([]Connection, error) {
	rows, err := db.conn.Query(
		"SELECT "+connectionColumns+" FROM connections WHERE type = ? ORDER BY created_at ASC",
		ConnectionTypeDirect,
	)
	if err != nil {
		return nil, fmt.Errorf("get direct connections: %w", err)
	}
	defer rows.Close()

	var conns []Connection
	for rows.Next() {
		c, err := scanConnection(rows.Scan)
		if err != nil {
			return nil, fmt.Errorf("scan connection: %w", err)
		}
		conns = append(conns, *c)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate connection rows: %w", err)
	}
	return conns, nil
}

// GetConnectionByToken retrieves a connection by its tunnel token.
func (db *DB) GetConnectionByToken(token string) (*Connection, error) {
	return db.getConnectionRow("SELECT "+connectionColumns+" FROM connections WHERE tunnel_token = ?", token)
}

// GetConnectionByTokenCtx retrieves a connection by its tunnel token using a context.
// This is used by tunnel auth to avoid hanging while SQLite is busy.
func (db *DB) GetConnectionByTokenCtx(ctx context.Context, token string) (*Connection, error) {
	row := db.conn.QueryRowContext(ctx,
		"SELECT "+connectionColumns+" FROM connections WHERE tunnel_token = ?", token,
	)
	c, err := scanConnection(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connection by token: %w", err)
	}
	return c, nil
}

// GetConnectionByID retrieves a connection by its ID.
func (db *DB) GetConnectionByID(id string) (*Connection, error) {
	return db.getConnectionRow("SELECT "+connectionColumns+" FROM connections WHERE id = ?", id)
}

// GetEmbeddedConnection retrieves the embedded connection (if any).
func (db *DB) GetEmbeddedConnection() (*Connection, error) {
	return db.getConnectionRow("SELECT " + connectionColumns + " FROM connections WHERE is_embedded = 1 LIMIT 1")
}

func (db *DB) getConnectionRow(query string, args ...any) (*Connection, error) {
	row := db.conn.QueryRow(query, args...)
	c, err := scanConnection(row.Scan)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get connection: %w", err)
	}
	return c, nil
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

// CreateConnectionParams describes a new connection record.
type CreateConnectionParams struct {
	Name          string
	TunnelToken   string
	IsEmbedded    bool
	Type          string // ConnectionTypeDirect or ConnectionTypeTunnel
	ClickHouseURL string // required for direct connections
}

// CreateConnection creates a new connection and returns its ID.
func (db *DB) CreateConnection(p CreateConnectionParams) (string, error) {
	id := uuid.NewString()
	embedded := 0
	if p.IsEmbedded {
		embedded = 1
	}
	connType := p.Type
	if connType == "" {
		connType = ConnectionTypeTunnel
	}
	_, err := db.conn.Exec(
		"INSERT INTO connections (id, name, tunnel_token, is_embedded, type, clickhouse_url) VALUES (?, ?, ?, ?, ?, ?)",
		id, p.Name, p.TunnelToken, embedded, connType, p.ClickHouseURL,
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

// UpdateConnectionName updates the display name for a connection.
func (db *DB) UpdateConnectionName(id, newName string) error {
	_, err := db.conn.Exec("UPDATE connections SET name = ? WHERE id = ?", newName, id)
	if err != nil {
		return fmt.Errorf("update connection name: %w", err)
	}
	return nil
}

// UpdateConnectionClickHouseURL updates the target URL of a direct connection.
func (db *DB) UpdateConnectionClickHouseURL(id, url string) error {
	_, err := db.conn.Exec("UPDATE connections SET clickhouse_url = ? WHERE id = ?", url, id)
	if err != nil {
		return fmt.Errorf("update connection clickhouse url: %w", err)
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
