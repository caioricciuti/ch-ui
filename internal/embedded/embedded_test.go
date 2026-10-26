package embedded

import (
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

// A config clickhouse_url on a different server resets the embedded
// connection; one on the same server only updates the URL.
func TestEnsureEmbeddedConnectionResetsOnServerChange(t *testing.T) {
	db, err := dbtest.Open(t, "embedded.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	m := NewManager(db, 0, false)
	m.SameServer = func(a, b *database.Connection) bool {
		host := func(c *database.Connection) string {
			return strings.Split(strings.TrimPrefix(c.ClickHouseURL, "http://"), ":")[0]
		}
		return host(a) == host(b)
	}
	if err := m.ensureEmbeddedConnection("http://old:8123", "Local"); err != nil {
		t.Fatal(err)
	}
	conn, err := db.GetEmbeddedConnection()
	if err != nil || conn == nil {
		t.Fatalf("embedded connection = %+v, %v", conn, err)
	}
	newSession := func(token string) {
		t.Helper()
		if _, err := db.CreateSession(database.CreateSessionParams{
			ConnectionID: conn.ID, ClickhouseUser: "alice", EncryptedPassword: "enc", Token: token,
			ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), UserRole: "analyst",
		}); err != nil {
			t.Fatal(err)
		}
	}
	newSession("same")

	if err := m.ensureEmbeddedConnection("http://old:9000", "Local"); err != nil {
		t.Fatal(err)
	}
	if s, _ := db.GetSession("same"); s == nil {
		t.Fatal("a URL change on the same server signed the session out")
	}

	if err := m.ensureEmbeddedConnection("http://new:8123", "Local"); err != nil {
		t.Fatal(err)
	}
	if s, _ := db.GetSession("same"); s != nil {
		t.Fatal("the session on the old server survived")
	}
	if c, _ := db.GetEmbeddedConnection(); c == nil || c.ClickHouseURL != "http://new:8123" {
		t.Fatalf("embedded connection after change = %+v", c)
	}
	logs, err := db.GetAuditLogs(10)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range logs {
		if l.Action == "connection.reset" && l.Details != nil && strings.Contains(*l.Details, "1 session(s) signed out") {
			found = true
		}
	}
	if !found {
		t.Fatal("no connection.reset audit entry")
	}

	// Without a comparator every URL change counts as a new server.
	m.SameServer = nil
	newSession("nil-hook")
	if err := m.ensureEmbeddedConnection("http://new:8124", "Local"); err != nil {
		t.Fatal(err)
	}
	if s, _ := db.GetSession("nil-hook"); s != nil {
		t.Fatal("nil SameServer must reset")
	}
}

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
