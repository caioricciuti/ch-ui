package embedded

import (
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	connconfig "github.com/caioricciuti/ch-ui/connector/config"
	"github.com/caioricciuti/ch-ui/connector/ui"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tokens"
)

// Manager runs one in-process connector per direct connection. Each connector
// dials the local tunnel gateway exactly like a remote agent would, so the
// rest of the server treats direct and tunnel connections identically.
type Manager struct {
	db   *database.DB
	port int

	mu         sync.Mutex
	connectors map[string]*connector.Connector
}

// NewManager creates a connector manager. Call Start after the HTTP server
// port is known; connectors retry until the gateway is accepting connections.
func NewManager(db *database.DB, port int) *Manager {
	return &Manager{
		db:         db,
		port:       port,
		connectors: make(map[string]*connector.Connector),
	}
}

// Start ensures the config-defined embedded connection exists (when
// clickhouseURL is set), then launches a connector for every direct
// connection. It should be called after the HTTP server starts listening;
// connectors reconnect on their own until it does.
func (m *Manager) Start(clickhouseURL, connectionName string) error {
	if clickhouseURL != "" {
		if err := m.ensureEmbeddedConnection(clickhouseURL, connectionName); err != nil {
			return err
		}
	} else {
		slog.Info("No CLICKHOUSE_URL configured, skipping embedded connection")
	}

	conns, err := m.db.GetDirectConnections()
	if err != nil {
		return fmt.Errorf("load direct connections: %w", err)
	}
	for _, c := range conns {
		if strings.TrimSpace(c.ClickHouseURL) == "" {
			slog.Warn("Direct connection has no ClickHouse URL, skipping", "id", c.ID, "name", c.Name)
			continue
		}
		m.StartConnection(c)
	}
	return nil
}

// ensureEmbeddedConnection creates or updates the connection record managed by
// server config (clickhouse_url / connection_name).
func (m *Manager) ensureEmbeddedConnection(clickhouseURL, connectionName string) error {
	connectionName = strings.TrimSpace(connectionName)
	if connectionName == "" {
		connectionName = "Local ClickHouse"
	}

	dbConn, err := m.db.GetEmbeddedConnection()
	if err != nil {
		return fmt.Errorf("check embedded connection: %w", err)
	}

	if dbConn == nil {
		token := tokens.GenerateTunnelToken()
		id, err := m.db.CreateConnection(database.CreateConnectionParams{
			Name:          connectionName,
			TunnelToken:   token,
			IsEmbedded:    true,
			Type:          database.ConnectionTypeDirect,
			ClickHouseURL: clickhouseURL,
		})
		if err != nil {
			return fmt.Errorf("create embedded connection: %w", err)
		}
		slog.Info("Created embedded connection", "id", id)
		return nil
	}

	// Server config is the source of truth for the embedded connection.
	if strings.TrimSpace(dbConn.Name) != connectionName {
		if err := m.db.UpdateConnectionName(dbConn.ID, connectionName); err != nil {
			return fmt.Errorf("update embedded connection name: %w", err)
		}
	}
	if strings.TrimSpace(dbConn.ClickHouseURL) != clickhouseURL {
		if err := m.db.UpdateConnectionClickHouseURL(dbConn.ID, clickhouseURL); err != nil {
			return fmt.Errorf("update embedded connection url: %w", err)
		}
	}
	return nil
}

// StartConnection launches (or restarts) the in-process connector for a
// direct connection. Safe to call for an already-running connection.
func (m *Manager) StartConnection(c database.Connection) {
	m.mu.Lock()
	if existing, ok := m.connectors[c.ID]; ok {
		existing.Shutdown()
		delete(m.connectors, c.ID)
	}

	cfg := &connconfig.Config{
		TunnelURL:         fmt.Sprintf("ws://127.0.0.1:%d/connect", m.port),
		Token:             c.TunnelToken,
		ClickHouseURL:     c.ClickHouseURL,
		Takeover:          true,
		HeartbeatInterval: 30 * time.Second,
		ReconnectDelay:    2 * time.Second,
		MaxReconnectDelay: 30 * time.Second,
	}
	conn := connector.New(cfg, ui.New(true, true, false, false)) // quiet mode
	m.connectors[c.ID] = conn
	m.mu.Unlock()

	go func() {
		// Give the HTTP server a moment on first boot; reconnect logic covers
		// the rest.
		time.Sleep(500 * time.Millisecond)
		slog.Info("Starting direct connector", "connection", c.Name, "id", c.ID, "clickhouse_url", c.ClickHouseURL)
		if err := conn.Run(); err != nil {
			slog.Error("Direct connector exited with error", "connection", c.Name, "id", c.ID, "error", err)
		}
	}()
}

// StopConnection shuts down the connector for a connection, if running.
func (m *Manager) StopConnection(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if conn, ok := m.connectors[id]; ok {
		conn.Shutdown()
		delete(m.connectors, id)
	}
}

// StopAll gracefully shuts down every managed connector.
func (m *Manager) StopAll() {
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, conn := range m.connectors {
		conn.Shutdown()
		delete(m.connectors, id)
	}
}
