package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func batchAgent(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	data := `[{"x":1}]`
	if strings.Contains(msg.SQL, "system.columns") && msg.Settings["param_tbl"] == "missing" {
		data = `[]`
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
}

func callBatch(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) *mcp.CallToolResult {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestDescribeTableBatch(t *testing.T) {
	cs := connectTools(t, batchAgent)
	res := callBatch(t, cs, "describe_table", map[string]any{"tables": []map[string]any{
		{"database": "default", "table": "events"},
		{"database": "default", "table": "missing"},
		{"database": "", "table": "x"},
	}})
	if res.IsError {
		t.Fatalf("batch failed: %s", resultText(res))
	}
	var out struct {
		Tables []map[string]any `json:"tables"`
	}
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil || len(out.Tables) != 3 {
		t.Fatalf("decode: %v %s", err, resultText(res))
	}
	if out.Tables[0]["table"] != "events" || out.Tables[0]["columns"] == nil {
		t.Errorf("first table: %v", out.Tables[0])
	}
	if !strings.Contains(out.Tables[1]["error"].(string), "not found") || out.Tables[1]["table"] != "missing" {
		t.Errorf("missing table should report in place: %v", out.Tables[1])
	}
	if !strings.Contains(out.Tables[2]["error"].(string), "required") {
		t.Errorf("incomplete ref should report in place: %v", out.Tables[2])
	}

	for name, args := range map[string]map[string]any{
		"both":      {"database": "default", "table": "a", "tables": []map[string]any{{"database": "default", "table": "b"}}},
		"too many":  {"tables": make([]map[string]any, maxBatchTables+1)},
		"no target": {},
	} {
		if res := callBatch(t, cs, "describe_table", args); !res.IsError {
			t.Errorf("%s: want an error, got %s", name, resultText(res))
		}
	}
}

func TestRunSelectBatch(t *testing.T) {
	cs := connectTools(t, batchAgent)
	res := callBatch(t, cs, "run_select", map[string]any{"queries": []string{"SELECT 1", "DROP TABLE t", "SELECT 3"}})
	if res.IsError {
		t.Fatalf("a batch with one bad query must not fail: %s", resultText(res))
	}
	text := resultText(res)
	for _, want := range []string{`"query_index":0`, `"query_index":2`, "query 1 failed: only read-only"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in %s", want, text)
		}
	}
	if len(res.Content) != 3 {
		t.Errorf("want one block per query, got %d", len(res.Content))
	}

	if res := callBatch(t, cs, "run_select", map[string]any{"queries": []string{"DROP TABLE a", "DELETE FROM b"}}); !res.IsError {
		t.Error("a batch where every query fails is an error")
	}
	for name, args := range map[string]map[string]any{
		"both":     {"sql": "SELECT 1", "queries": []string{"SELECT 2"}},
		"too many": {"queries": make([]string, maxBatchQueries+1)},
	} {
		if res := callBatch(t, cs, "run_select", args); !res.IsError {
			t.Errorf("%s: want an error, got %s", name, resultText(res))
		}
	}
}
