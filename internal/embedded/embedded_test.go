package embedded

import (
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

func TestTunnelURLFollowsTLS(t *testing.T) {
	if got := NewManager(nil, 3488, false).tunnelURL(); got != "ws://127.0.0.1:3488/connect" {
		t.Errorf("plain: %q", got)
	}
	if got := NewManager(nil, 8443, true).tunnelURL(); got != "wss://127.0.0.1:8443/connect" {
		t.Errorf("tls: %q", got)
	}
}

// Restarting and stopping a connection shuts its connector down more than
// once (StartConnection over a running one, then StopAll); none of it may
// panic. Host info stored by older versions is dropped on start.
func TestStartConnectionRestartAndStop(t *testing.T) {
	db, err := dbtest.Open(t, "embedded.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })

	id, err := db.CreateConnection(database.CreateConnectionParams{
		Name:          "direct",
		TunnelToken:   "cht_test",
		Type:          database.ConnectionTypeDirect,
		ClickHouseURL: "http://127.0.0.1:8123",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateConnectionHostInfo(id, database.HostInfo{Hostname: "chui-host"}); err != nil {
		t.Fatal(err)
	}
	conn, err := db.GetConnectionByID(id)
	if err != nil || conn == nil {
		t.Fatalf("get connection: %v", err)
	}

	// Port 1 refuses, so the connector sits in its retry loop.
	m := NewManager(db, 1, true)
	m.StartConnection(*conn)
	m.StartConnection(*conn)
	m.StopConnection(id)
	m.StopConnection(id)
	m.StartConnection(*conn)
	m.StopAll()
	if len(m.connectors) != 0 {
		t.Errorf("connectors left after StopAll: %d", len(m.connectors))
	}

	info, err := db.GetConnectionHostInfo(id)
	if err != nil {
		t.Fatal(err)
	}
	if info != nil {
		t.Errorf("host info kept for a direct connection: %+v", info)
	}
}
