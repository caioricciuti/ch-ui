package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// connectTools serves the free and catalog tools against an agent that
// answers with respond, and returns a connected client.
func connectTools(t *testing.T, respond func(tunnel.GatewayMessage) *tunnel.AgentMessage) *mcp.ClientSession {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", respond)
	t.Cleanup(background.Wait)
	deps := Deps{DB: db, Gateway: agent.Gateway, Config: &config.Config{}}
	ak := &authedKey{key: &database.MCPKey{ID: "k1", Name: "test", ConnectionID: conn, CHUser: "default"}}
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerFreeTools(srv, deps, ak)
	registerCatalogTools(srv, deps, ak)
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
	return cs
}

// The metadata lookups of describe_table and search_catalog must go out
// together. The agent never answers, so a tool that waits for one result
// before sending the next query leaves only one query in flight.
func TestMetadataLookupsRunInParallel(t *testing.T) {
	for _, tc := range []struct {
		tool string
		args map[string]any
		want int64
	}{
		{"describe_table", map[string]any{"database": "default", "table": "events"}, 4},
		{"search_catalog", map[string]any{"query": "ev"}, 2},
		{"describe_table", map[string]any{"tables": []map[string]any{{"database": "default", "table": "a"}, {"database": "default", "table": "b"}}}, 8},
		{"run_select", map[string]any{"queries": []string{"SELECT 1", "SELECT 2", "SELECT 3"}}, 3},
	} {
		t.Run(tc.tool, func(t *testing.T) {
			var seen atomic.Int64
			cs := connectTools(t, func(tunnel.GatewayMessage) *tunnel.AgentMessage {
				seen.Add(1)
				return nil
			})
			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan struct{})
			go func() {
				defer close(done)
				cs.CallTool(ctx, &mcp.CallToolParams{Name: tc.tool, Arguments: tc.args})
			}()
			deadline := time.Now().Add(5 * time.Second)
			for seen.Load() < tc.want && time.Now().Before(deadline) {
				time.Sleep(10 * time.Millisecond)
			}
			cancel()
			<-done
			if got := seen.Load(); got != tc.want {
				t.Errorf("%s: %d queries in flight, want %d", tc.tool, got, tc.want)
			}
		})
	}
}

func TestDescribeTableAssemblesParallelResults(t *testing.T) {
	answer := func(data string) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data), Meta: json.RawMessage(`[]`)}
	}
	respond := func(found bool) func(tunnel.GatewayMessage) *tunnel.AgentMessage {
		return func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
			switch {
			case strings.Contains(msg.SQL, "system.columns"):
				if !found {
					return answer(`[]`)
				}
				return answer(`[{"name":"id","type":"UInt64"}]`)
			case strings.Contains(msg.SQL, "system.tables"):
				return answer(`[{"engine":"MergeTree","sorting_key":"id"}]`)
			case strings.Contains(msg.SQL, "system.parts"):
				return answer(`[{"active_parts":3}]`)
			default:
				return answer(`[{"id":7}]`)
			}
		}
	}
	args := map[string]any{"database": "default", "table": "events"}

	cs := connectTools(t, respond(true))
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "describe_table", Arguments: args})
	if err != nil || res.IsError {
		t.Fatalf("describe_table: %v %s", err, resultText(res))
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(resultText(res)), &out); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"columns", "table_info", "storage", "sample_rows"} {
		if out[k] == nil {
			t.Errorf("missing %s in %s", k, resultText(res))
		}
	}

	cs = connectTools(t, respond(false))
	res, err = cs.CallTool(context.Background(), &mcp.CallToolParams{Name: "describe_table", Arguments: args})
	if err != nil || !res.IsError || !strings.Contains(resultText(res), "not found") {
		t.Errorf("missing table: want not found, got %v %s", err, resultText(res))
	}
}
