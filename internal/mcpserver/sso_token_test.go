package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// An OAuth token granted by an SSO person with a per-person ClickHouse user
// holds no password: every request runs with the user's current one, and
// the token dies with the user.
func TestOAuthTokenOfPerPersonUserFollowsLiveCredentials(t *testing.T) {
	db := openTestDB(t)
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "worker", TunnelToken: "worker-token", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	agent := testutil.NewAgent(t, db, "worker-token", func(tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
	})
	cfg := &config.Config{AppSecretKey: "test-secret-key-for-mcp-tests"}
	ident, err := db.UpsertSSOIdentity("https://idp.example", "sub-ana", "ana@x.com", "Ana", []string{"analysts"})
	if err != nil {
		t.Fatal(err)
	}
	saveUser := func(pw string) {
		t.Helper()
		enc, err := crypto.Encrypt(pw, cfg.AppSecretKey)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SaveSSOClickHouseUser(database.SSOClickHouseUser{
			IdentityID: ident.ID, ConnectionID: conn, CHUser: "sso:ana@x.com", EncryptedPassword: enc, Roles: []string{"analyst"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	saveUser("pw1")

	plaintext, hash, prefix := GenerateKey()
	if _, err := db.CreateMCPKey(database.CreateMCPKeyParams{
		Name: "Claude (ana@x.com)", KeyHash: hash, KeyPrefix: prefix, ConnectionID: conn,
		CHUser: "sso:ana@x.com", SSOIdentityID: ident.ID, Scopes: "read", CreatedBy: "oauth:ana@x.com",
		Kind: "oauth", Subject: "ana@x.com", ClientID: "client-1",
	}); err != nil {
		t.Fatal(err)
	}
	h := Handler(Deps{DB: db, Gateway: agent.Gateway, Config: cfg})
	const call = `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"run_select","arguments":{"sql":"SELECT 1"}}}`
	lastQuery := func() tunnel.GatewayMessage {
		t.Helper()
		msgs := agent.Messages()
		for i := len(msgs) - 1; i >= 0; i-- {
			if msgs[i].Type == "query" && strings.Contains(msgs[i].SQL, "SELECT 1") {
				return msgs[i]
			}
		}
		t.Fatal("the query never reached the agent")
		return tunnel.GatewayMessage{}
	}

	rec := mcpRequest(t, h, plaintext, call)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "isError") {
		t.Fatalf("with pw1: %d %s", rec.Code, rec.Body.String())
	}
	if m := lastQuery(); m.User != "sso:ana@x.com" || m.Password != "pw1" {
		t.Fatalf("ran as %s/%s, want sso:ana@x.com/pw1", m.User, m.Password)
	}

	// The person signs in again and the password rotates: the token follows.
	saveUser("pw2")
	rec = mcpRequest(t, h, plaintext, call)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "isError") {
		t.Fatalf("with pw2: %d %s", rec.Code, rec.Body.String())
	}
	if m := lastQuery(); m.Password != "pw2" {
		t.Fatalf("ran with password %q, want the rotated pw2", m.Password)
	}

	// Locked by the sweep or an admin: the token is refused before any SQL.
	if err := db.SetSSOClickHouseUserLocked(ident.ID, conn, true); err != nil {
		t.Fatal(err)
	}
	before := len(agent.Messages())
	rec = mcpRequest(t, h, plaintext, call)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "no longer has access") {
		t.Fatalf("locked user: %d %s", rec.Code, rec.Body.String())
	}

	// Dropped: still refused.
	if err := db.DeleteSSOClickHouseUser(ident.ID, conn); err != nil {
		t.Fatal(err)
	}
	rec = mcpRequest(t, h, plaintext, call)
	if rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "no longer has access") {
		t.Fatalf("dropped user: %d %s", rec.Code, rec.Body.String())
	}
	if n := len(agent.Messages()) - before; n != 0 {
		t.Fatalf("%d statements reached ClickHouse for a retired user", n)
	}
}
