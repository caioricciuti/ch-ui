package mcpserver

import (
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/database"
)

type (
	mcpResultAlias   = mcp.CallToolResult
	textContentAlias = mcp.TextContent
)

func callCatalogTool(t *testing.T, deps Deps, key, name, args string) string {
	t.Helper()
	h := Handler(deps)
	rec := mcpRequest(t, h, key, `{"jsonrpc":"2.0","id":9,"method":"tools/call","params":{"name":"`+name+`","arguments":`+args+`}}`)
	return string(sseData(t, rec.Body.String()))
}

func TestCatalogToolsListed(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)
	rec := mcpRequest(t, h, key, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	for _, tool := range []string{"search_catalog", "estimate_query", "run_saved_query"} {
		if !strings.Contains(rec.Body.String(), `"name":"`+tool+`"`) {
			t.Errorf("tools/list missing %s", tool)
		}
	}
}

func TestFindSavedQuery(t *testing.T) {
	deps, _ := testDeps(t)
	keys, _ := deps.DB.ListMCPKeys()
	ak := &authedKey{key: keys[0]}
	mine := keys[0].ConnectionID
	other, _ := deps.DB.CreateConnection(database.CreateConnectionParams{Name: "other", TunnelToken: "cht_22222222222222222222222222222222", Type: database.ConnectionTypeTunnel})

	id1, _ := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: "Daily Active", Query: "SELECT 1", ConnectionID: mine, Verified: true})
	deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: "daily active", Query: "SELECT 2", ConnectionID: mine})
	theirs, _ := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: "Secret", Query: "SELECT 3", ConnectionID: other})
	global, _ := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: "Global", Query: "SELECT 4"})

	if sq, e := findSavedQuery(deps, ak, id1, ""); e != nil || sq == nil || !sq.Verified {
		t.Errorf("by id: %v %v", sq, e)
	}
	if _, e := findSavedQuery(deps, ak, theirs, ""); e == nil {
		t.Error("another connection's query must not resolve")
	}
	if sq, e := findSavedQuery(deps, ak, global, ""); e != nil || sq == nil {
		t.Errorf("global query should resolve: %v", e)
	}
	// two case-insensitive matches: must be an error naming both ids
	if _, e := findSavedQuery(deps, ak, "", "daily active"); e == nil || !strings.Contains(errText(e), id1) {
		t.Errorf("ambiguous name should error listing ids, got %v", e)
	}
	if _, e := findSavedQuery(deps, ak, "", "nope"); e == nil {
		t.Error("unknown name should error")
	}
	if _, e := findSavedQuery(deps, ak, "", ""); e == nil {
		t.Error("empty lookup should error")
	}
}

func TestCatalogToolsValidation(t *testing.T) {
	deps, key := testDeps(t)
	if body := callCatalogTool(t, deps, key, "search_catalog", `{"query":""}`); !strings.Contains(body, "query is required") {
		t.Errorf("empty search: %s", body)
	}
	if body := callCatalogTool(t, deps, key, "estimate_query", `{"sql":"DROP TABLE t"}`); !strings.Contains(body, "SELECT / WITH") {
		t.Errorf("estimate should reject non-select: %s", body)
	}
	if body := callCatalogTool(t, deps, key, "run_saved_query", `{}`); !strings.Contains(body, "id or name") {
		t.Errorf("run_saved_query without id/name: %s", body)
	}
	keys, _ := deps.DB.ListMCPKeys()
	id, _ := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: "p", Query: "SELECT {d:String}", ConnectionID: keys[0].ConnectionID})
	if body := callCatalogTool(t, deps, key, "run_saved_query", `{"id":"`+id+`","params":{"bad name":"x"}}`); !strings.Contains(body, "invalid parameter name") {
		t.Errorf("bad param name: %s", body)
	}
	// Valid lookup reaches the gateway, which is offline in tests.
	if body := callCatalogTool(t, deps, key, "run_saved_query", `{"id":"`+id+`","params":{"d":"x"}}`); !strings.Contains(body, "offline") {
		t.Errorf("expected offline tunnel error, got: %s", body)
	}
	// describe_table and search_catalog also reach the gateway.
	if body := callCatalogTool(t, deps, key, "search_catalog", `{"query":"ev"}`); !strings.Contains(body, "offline") {
		t.Errorf("expected offline tunnel error, got: %s", body)
	}
}

func TestQuoteIdent(t *testing.T) {
	if quoteIdent("a`b") != "`a\\`b`" {
		t.Errorf("got %s", quoteIdent("a`b"))
	}
}

func errText(r *mcpResultAlias) string {
	if r == nil || len(r.Content) == 0 {
		return ""
	}
	if tc, ok := r.Content[0].(*textContentAlias); ok {
		return tc.Text
	}
	return ""
}
