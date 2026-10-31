package tunnel

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/caioricciuti/ch-ui/internal/database"
)

// signIn authenticates one agent socket that reports endpoint, returns the
// gateway's reply and closes the socket once the gateway has let go of it.
func signIn(t *testing.T, g *Gateway, url, endpoint string) GatewayMessage {
	t.Helper()
	ws, _, err := websocket.DefaultDialer.Dial(url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := ws.WriteJSON(AgentMessage{Type: "auth", Token: "test-only", ClickHouseEndpoint: endpoint}); err != nil {
		t.Fatal(err)
	}
	_ = ws.SetReadDeadline(time.Now().Add(5 * time.Second))
	var reply GatewayMessage
	if err := ws.ReadJSON(&reply); err != nil {
		t.Fatalf("auth reply: %v", err)
	}
	ws.Close()
	if !g.WaitReadLoops(5 * time.Second) {
		t.Fatal("gateway read loop did not stop")
	}
	return reply
}

func TestAgentEndpointChangeClearsOldServerState(t *testing.T) {
	g, connID := newTestGateway(t)
	server := httptest.NewServer(http.HandlerFunc(g.HandleWebSocket))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")
	var forgotten []string
	g.OnServerChange = func(id string) { forgotten = append(forgotten, id) }

	stored := func() string {
		t.Helper()
		c, err := g.db.GetConnectionByID(connID)
		if err != nil || c == nil {
			t.Fatalf("connection: %v", err)
		}
		if c.AgentEndpoint == nil {
			return ""
		}
		return *c.AgentEndpoint
	}
	sessions := func() int {
		t.Helper()
		var n int
		if err := g.db.Conn().QueryRow("SELECT count(*) FROM sessions WHERE connection_id = ?", connID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	addSession := func(token string) {
		t.Helper()
		if _, err := g.db.CreateSession(database.CreateSessionParams{
			ConnectionID: connID, ClickhouseUser: "alice", EncryptedPassword: "enc", Token: token,
			ExpiresAt: time.Now().UTC().Add(time.Hour).Format(time.RFC3339), UserRole: "analyst",
		}); err != nil {
			t.Fatal(err)
		}
	}
	resets := func() int {
		t.Helper()
		var n int
		if err := g.db.Conn().QueryRow("SELECT count(*) FROM audit_logs WHERE action = 'connection.reset' AND connection_id = ?", connID).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}

	// An agent older than the field reports nothing: let through, nothing stored.
	addSession("s1")
	if r := signIn(t, g, url, ""); r.Type != "auth_ok" {
		t.Fatalf("old agent: %+v", r)
	}
	if stored() != "" || sessions() != 1 {
		t.Fatalf("old agent: endpoint %q, %d sessions", stored(), sessions())
	}

	// First report is stored, credentials never in it.
	if r := signIn(t, g, url, "http://user:secret@CH-A.internal/"); r.Type != "auth_ok" {
		t.Fatalf("first report: %+v", r)
	}
	if got := stored(); got != "http://ch-a.internal:8123/" || sessions() != 1 {
		t.Fatalf("first report: endpoint %q, %d sessions", got, sessions())
	}

	// The same server spelled differently changes nothing.
	if r := signIn(t, g, url, "http://ch-a.internal:8123"); r.Type != "auth_ok" {
		t.Fatalf("same server: %+v", r)
	}
	if sessions() != 1 || resets() != 0 || len(forgotten) != 0 {
		t.Fatalf("same server: %d sessions, %d resets, forgotten %v", sessions(), resets(), forgotten)
	}

	// A different server clears the old one's state before auth_ok.
	if r := signIn(t, g, url, "https://ch-b.internal"); r.Type != "auth_ok" {
		t.Fatalf("new server: %+v", r)
	}
	if got := stored(); got != "https://ch-b.internal:8443/" {
		t.Fatalf("new server: endpoint %q", got)
	}
	if sessions() != 0 || resets() != 1 || len(forgotten) != 1 || forgotten[0] != connID {
		t.Fatalf("new server: %d sessions, %d resets, forgotten %v", sessions(), resets(), forgotten)
	}
	if c, _ := g.db.GetConnectionByID(connID); c == nil || c.ServerResetAt == nil {
		t.Fatal("new server: server_reset_at not set")
	}

	// Going back to an agent that reports nothing keeps the stored endpoint.
	addSession("s2")
	if r := signIn(t, g, url, ""); r.Type != "auth_ok" {
		t.Fatalf("downgraded agent: %+v", r)
	}
	if stored() != "https://ch-b.internal:8443/" || sessions() != 1 || resets() != 1 {
		t.Fatalf("downgraded agent: endpoint %q, %d sessions, %d resets", stored(), sessions(), resets())
	}
}

func TestDirectConnectionSkipsAgentEndpointCheck(t *testing.T) {
	g, connID := newTestGateway(t)
	if _, err := g.db.Conn().Exec("UPDATE connections SET type = ?, clickhouse_url = ? WHERE id = ?",
		database.ConnectionTypeDirect, "http://ch-a:8123", connID); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(http.HandlerFunc(g.HandleWebSocket))
	t.Cleanup(server.Close)
	url := "ws" + strings.TrimPrefix(server.URL, "http")

	if r := signIn(t, g, url, "http://ch-b:8123"); r.Type != "auth_ok" {
		t.Fatalf("direct: %+v", r)
	}
	c, err := g.db.GetConnectionByID(connID)
	if err != nil || c == nil {
		t.Fatalf("connection: %v", err)
	}
	if c.AgentEndpoint != nil {
		t.Fatalf("direct connection stored agent endpoint %q", *c.AgentEndpoint)
	}
}
