package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func callProTool(t *testing.T, allowed, tool string, args map[string]any) (*mcp.CallToolResult, *fakeCH) {
	t.Helper()
	deps, ak, f := fakeDeps(t, allowed)
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerProTools(srv, deps, ak)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res, f
}

// query_insights_top and costs_summary read system.query_log across every
// database, query text included, so a key limited to some databases must
// not reach them.
func TestProToolsRefuseAllowListedKeys(t *testing.T) {
	for tool, args := range map[string]map[string]any{
		"query_insights_top": {"section": "slow"},
		"costs_summary":      {},
	} {
		res, f := callProTool(t, "analytics", tool, args)
		if !res.IsError || !strings.Contains(resultText(res), "analytics") || !strings.Contains(resultText(res), "not available") {
			t.Errorf("%s: want refusal naming the allow-list, got %s", tool, resultText(res))
		}
		if len(f.sentContaining("query_log")) != 0 {
			t.Errorf("%s: system.query_log must not be queried for an allow-listed key", tool)
		}

		res, f = callProTool(t, "", tool, args)
		if res.IsError {
			t.Errorf("%s: unrestricted key should run, got %s", tool, resultText(res))
		}
		if len(f.sentContaining("query_log")) == 0 {
			t.Errorf("%s: unrestricted key should query system.query_log", tool)
		}
	}
}

// truncated is true only when ClickHouse had more rows than the cap, not
// when the result has exactly max_rows rows.
func TestRunReadQueryTruncatedOnlyWhenMoreRows(t *testing.T) {
	for _, tc := range []struct {
		returned  int
		truncated bool
	}{
		{returned: 3, truncated: false},
		{returned: 4, truncated: true},
	} {
		db, conn := testutil.WorkerDB(t)
		rows := make([]map[string]int, tc.returned)
		for i := range rows {
			rows[i] = map[string]int{"x": i}
		}
		data, _ := json.Marshal(rows)
		var sentMax string
		agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
			if strings.Contains(msg.SQL, "currentDatabase()") {
				return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"db":"default"}]`), Meta: json.RawMessage(`[{"name":"db","type":"String"}]`)}
			}
			sentMax = msg.Settings["max_result_rows"]
			return &tunnel.AgentMessage{Type: "query_result", Data: data, Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
		})
		t.Cleanup(background.Wait)
		deps, ak, _ := fakeDeps(t, "")
		deps.DB, deps.Gateway = db, agent.Gateway
		ak.key.ConnectionID = conn

		res := runReadQuery(context.Background(), deps, ak, readQuery{SQL: "SELECT number AS x FROM numbers(10)", MaxRows: 3, Endpoint: "/test"})
		if res.IsError {
			t.Fatal(resultText(res))
		}
		if sentMax != "4" {
			t.Errorf("max_result_rows = %q, want 4 (cap + 1)", sentMax)
		}
		text := resultText(res)
		if got := strings.Contains(text, `"truncated":true`); got != tc.truncated {
			t.Errorf("%d rows back with cap 3: truncated=%v, want %v (%s)", tc.returned, got, tc.truncated, text)
		}
		if !strings.Contains(text, `"row_count":3`) {
			t.Errorf("%d rows back with cap 3: want row_count 3, got %s", tc.returned, text)
		}
	}
}
