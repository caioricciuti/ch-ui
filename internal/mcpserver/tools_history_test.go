package mcpserver

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func callHistory(t *testing.T, deps Deps, ak *authedKey, args map[string]any) []string {
	t.Helper()
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerHistoryTools(srv, deps, ak)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "query_history", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if res.IsError {
		t.Fatalf("query_history: %s", resultText(res))
	}
	var out struct {
		Queries []struct {
			Query string `json:"query"`
		} `json:"queries"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatal(err)
	}
	got := make([]string, 0, len(out.Queries))
	for _, q := range out.Queries {
		got = append(got, q.Query)
	}
	return got
}

func sameSet(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	seen := map[string]bool{}
	for _, g := range got {
		seen[g] = true
	}
	for _, w := range want {
		if !seen[w] {
			return false
		}
	}
	return true
}

// query_history returns only what this MCP client ran: an API key its own
// rows, an OAuth token its person's MCP rows across refreshed tokens.
// Editor history and other keys or people never show, even when they share
// the ClickHouse user.
func TestQueryHistoryOnlyOwnRows(t *testing.T) {
	db := openTestDB(t)
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "cht_hist1", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	other, err := db.CreateConnection(database.CreateConnectionParams{Name: "o", TunnelToken: "cht_hist2", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{DB: db}
	add := func(conn, actor, keyID, source, status, sql string) {
		t.Helper()
		if err := db.CreateQueryHistoryEntry(database.CreateQueryHistoryParams{
			ConnectionID: conn, User: "default", Actor: actor, QueryText: sql, Status: status, Source: source, MCPKeyID: keyID,
		}); err != nil {
			t.Fatal(err)
		}
	}
	add(conn, "default", "ka", "mcp", "success", "a1")
	add(conn, "default", "ka", "mcp", "error", "a-err")
	add(other, "default", "ka", "mcp", "success", "a other connection")
	add(conn, "default", "kb", "mcp", "success", "b1")
	add(conn, "default", "", "editor", "success", "editor")
	add(conn, "default", "", "mcp", "success", "legacy")
	add(conn, "alice@example.com", "o1", "mcp", "success", "alice before refresh")
	add(conn, "alice@example.com", "o2", "mcp", "success", "alice after refresh")
	add(conn, "alice@example.com", "", "editor", "success", "alice editor")
	add(conn, "bob@example.com", "o3", "mcp", "success", "bob")

	keyA := &authedKey{key: &database.MCPKey{ID: "ka", ConnectionID: conn, CHUser: "default", Kind: "api"}}
	keyB := &authedKey{key: &database.MCPKey{ID: "kb", ConnectionID: conn, CHUser: "default", Kind: "api"}}
	alice := &authedKey{key: &database.MCPKey{ID: "o2", ConnectionID: conn, CHUser: "default", Kind: "oauth", Subject: "alice@example.com"}}

	for _, tc := range []struct {
		name string
		ak   *authedKey
		args map[string]any
		want []string
	}{
		{"key A", keyA, nil, []string{"a1", "a-err"}},
		{"key B", keyB, nil, []string{"b1"}},
		{"key A errors", keyA, map[string]any{"status": "error"}, []string{"a-err"}},
		{"key A ok", keyA, map[string]any{"status": "ok"}, []string{"a1"}},
		{"oauth across tokens", alice, nil, []string{"alice before refresh", "alice after refresh"}},
	} {
		if got := callHistory(t, deps, tc.ak, tc.args); !sameSet(got, tc.want) {
			t.Errorf("%s: got %v, want %v", tc.name, got, tc.want)
		}
	}

	// Range and limit.
	if _, err := db.Conn().Exec(`UPDATE query_history SET created_at = '2000-01-01 00:00:00' WHERE query_text = 'a-err'`); err != nil {
		t.Fatal(err)
	}
	if got := callHistory(t, deps, keyA, nil); !sameSet(got, []string{"a1"}) {
		t.Errorf("default range should drop the old row, got %v", got)
	}
	for i := 0; i < 3; i++ {
		add(conn, "default", "ka", "mcp", "success", "more")
	}
	if got := callHistory(t, deps, keyA, map[string]any{"limit": 2}); len(got) != 2 {
		t.Errorf("limit 2 returned %d rows", len(got))
	}
}

// recordQuery stamps the key on the row, which is what query_history reads.
func TestRecordQueryStoresKeyID(t *testing.T) {
	db := openTestDB(t)
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "cht_hist3", Type: database.ConnectionTypeTunnel})
	if err != nil {
		t.Fatal(err)
	}
	deps := Deps{DB: db}
	ak := &authedKey{key: &database.MCPKey{ID: "k1", Name: "k", ConnectionID: conn, CHUser: "default", Kind: "api"}}
	recordQuery(deps, ak, "SELECT 42", "success", "", time.Millisecond, 1)
	background.Wait()
	if got := callHistory(t, deps, ak, nil); !sameSet(got, []string{"SELECT 42"}) {
		t.Errorf("got %v", got)
	}
	other := &authedKey{key: &database.MCPKey{ID: "k2", ConnectionID: conn, CHUser: "default", Kind: "api"}}
	if got := callHistory(t, deps, other, nil); len(got) != 0 {
		t.Errorf("another key with the same ClickHouse user sees %v", got)
	}
}

func TestListMCPQueryHistoryNeedsOneOwner(t *testing.T) {
	db := openTestDB(t)
	for _, f := range []database.MCPHistoryFilter{
		{ConnectionID: "c"},
		{ConnectionID: "c", KeyID: "k", Actor: "a"},
		{KeyID: "k"},
	} {
		if _, err := db.ListMCPQueryHistory(f); err == nil {
			t.Errorf("filter %+v should be refused", f)
		}
	}
}
