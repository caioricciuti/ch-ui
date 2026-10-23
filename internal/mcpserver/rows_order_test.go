package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// run_select must hand rows back as ClickHouse sent them: columns in
// SELECT order (not sorted) and numbers with every digit (not float64).
func TestRunSelectKeepsColumnOrderAndPrecision(t *testing.T) {
	const row = `{"status_code":200,"requests":"18446744073709551615","amount":123456789012345678.1234567891}`
	cs := connectTools(t, func(tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[` + row + `]`),
			Meta: json.RawMessage(`[{"name":"status_code","type":"UInt16"},{"name":"requests","type":"UInt64"},{"name":"amount","type":"Decimal(38, 10)"}]`)}
	})
	call := func(format string) string {
		res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "run_select",
			Arguments: map[string]any{"sql": "SELECT status_code, requests, amount FROM default.sales", "format": format}})
		if err != nil || res.IsError {
			t.Fatalf("%s: %v %s", format, err, resultText(res))
		}
		return resultText(res)
	}

	if got := call("json"); !strings.Contains(got, `"rows":[`+row+`]`) {
		t.Errorf("json rows changed on the way through:\n got: %s\nwant rows: [%s]", got, row)
	}
	if got := call("csv"); !strings.HasPrefix(got, "status_code,requests,amount\n200,18446744073709551615,123456789012345678.1234567891\n") {
		t.Errorf("csv lost order or precision: %q", got)
	}
}
