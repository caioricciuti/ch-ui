package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/mcpserver"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// recordingTransport adds the bearer key and notes which responses arrived
// gzipped (net/http decodes them and sets Uncompressed).
type recordingTransport struct {
	key        string
	mu         sync.Mutex
	compressed int
}

func (rt *recordingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+rt.key)
	resp, err := http.DefaultTransport.RoundTrip(r)
	if err == nil && resp.Uncompressed {
		rt.mu.Lock()
		rt.compressed++
		rt.mu.Unlock()
	}
	return resp, err
}

// A real MCP client gets a large run_select result gzipped and decoded
// intact through CompressStream.
func TestMCPLargeResultArrivesCompressed(t *testing.T) {
	var rows []string
	for i := 0; i < 500; i++ {
		rows = append(rows, fmt.Sprintf(`{"service":"checkout-api","status_code":%d,"requests":"%d"}`, 200+i%5, 1000+i))
	}
	data := "[" + strings.Join(rows, ",") + "]"
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(tunnel.GatewayMessage) *tunnel.AgentMessage {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data),
			Meta: json.RawMessage(`[{"name":"service","type":"String"},{"name":"status_code","type":"UInt16"},{"name":"requests","type":"UInt64"}]`)}
	})
	cfg := &config.Config{AppSecretKey: "test-secret"}
	enc, err := crypto.Encrypt("chpass", cfg.AppSecretKey)
	if err != nil {
		t.Fatal(err)
	}
	key, hash, prefix := mcpserver.GenerateKey()
	if _, err := db.CreateMCPKey(database.CreateMCPKeyParams{Name: "gz", KeyHash: hash, KeyPrefix: prefix, ConnectionID: conn, CHUser: "default", CHPasswordEnc: enc, Scopes: "read", CreatedBy: "admin"}); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(CompressStream(mcpserver.Handler(mcpserver.Deps{DB: db, Gateway: agent.Gateway, Config: cfg})))
	t.Cleanup(srv.Close)

	rt := &recordingTransport{key: key}
	ctx := context.Background()
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil).Connect(ctx,
		&mcp.StreamableClientTransport{Endpoint: srv.URL, HTTPClient: &http.Client{Transport: rt}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })

	rt.mu.Lock()
	rt.compressed = 0
	rt.mu.Unlock()
	res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: "run_select", Arguments: map[string]any{"sql": "SELECT * FROM default.events", "max_rows": 500}})
	if err != nil || res.IsError {
		t.Fatalf("run_select: %v %+v", err, res)
	}
	var out struct {
		RowCount int `json:"row_count"`
	}
	if err := json.Unmarshal([]byte(res.Content[0].(*mcp.TextContent).Text), &out); err != nil || out.RowCount != 500 {
		t.Fatalf("decoded result: %v, row_count %d", err, out.RowCount)
	}
	rt.mu.Lock()
	defer rt.mu.Unlock()
	if rt.compressed == 0 {
		t.Error("the run_select response was not gzipped")
	}
}
