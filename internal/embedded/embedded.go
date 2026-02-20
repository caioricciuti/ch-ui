package embedded

import (
	"fmt"
	"log/slog"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	connconfig "github.com/caioricciuti/ch-ui/connector/config"
	"github.com/caioricciuti/ch-ui/connector/ui"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/license"
)

// EmbeddedAgent manages an in-process tunnel connector that connects
// to the local CH-UI server for the embedded ClickHouse connection.
type EmbeddedAgent struct {
	conn *connector.Connector
	db   *database.DB
}

// Start creates the embedded connection record if needed and launches
// the in-process connector. It should be called after the HTTP server is
// listening so the WebSocket endpoint is available.
func Start(db *database.DB, port int, clickhouseURL string) (*EmbeddedAgent, error) {
	if clickhouseURL == "" {
		slog.Info("No CLICKHOUSE_URL configured, skipping embedded agent")
		return nil, nil
	}

	// Ensure an embedded connection record exists
	dbConn, err := db.GetEmbeddedConnection()
	if err != nil {
		return nil, fmt.Errorf("check embedded connection: %w", err)
	}

	if dbConn == nil {
		token := license.GenerateTunnelToken()
		id, err := db.CreateConnection("Local ClickHouse", token, true)
		if err != nil {
			return nil, fmt.Errorf("create embedded connection: %w", err)
		}
		slog.Info("Created embedded connection", "id", id)

		dbConn, err = db.GetConnectionByID(id)
		if err != nil || dbConn == nil {
			return nil, fmt.Errorf("fetch created embedded connection: %w", err)
		}
	}

	tunnelURL := fmt.Sprintf("ws://127.0.0.1:%d/connect", port)

	cfg := &connconfig.Config{
		TunnelURL:         tunnelURL,
		Token:             dbConn.TunnelToken,
		ClickHouseURL:     clickhouseURL,
		Takeover:          true, // Always takeover on startup
		HeartbeatInterval: 30 * time.Second,
		ReconnectDelay:    2 * time.Second,
		MaxReconnectDelay: 30 * time.Second,
	}

	// Use quiet mode for the embedded agent (suppresses terminal output)
	u := ui.New(true, true, false, false)

	conn := connector.New(cfg, u)

	ea := &EmbeddedAgent{
		conn: conn,
		db:   db,
	}

	go func() {
		// Small delay to let the HTTP server start accepting connections
		time.Sleep(500 * time.Millisecond)
		slog.Info("Starting embedded agent", "clickhouse_url", clickhouseURL, "tunnel_url", tunnelURL)
		if err := conn.Run(); err != nil {
			slog.Error("Embedded agent exited with error", "error", err)
		}
	}()

	return ea, nil
}

// Stop gracefully shuts down the embedded agent.
func (ea *EmbeddedAgent) Stop() {
	if ea != nil && ea.conn != nil {
		slog.Info("Stopping embedded connector")
		ea.conn.Shutdown()
	}
}
