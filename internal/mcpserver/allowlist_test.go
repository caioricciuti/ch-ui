package mcpserver

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// fakeCH is a protocol-level agent that records every SQL it receives and
// answers currentDatabase() with "default" and anything else with one row.
type fakeCH struct {
	mu   sync.Mutex
	sent []tunnel.GatewayMessage
}

func (f *fakeCH) respond(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
	f.mu.Lock()
	f.sent = append(f.sent, msg)
	f.mu.Unlock()
	if strings.Contains(msg.SQL, "currentDatabase()") {
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"db":"default"}]`), Meta: json.RawMessage(`[{"name":"db","type":"String"}]`)}
	}
	return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(`[{"x":1}]`), Meta: json.RawMessage(`[{"name":"x","type":"UInt8"}]`)}
}

// sentContaining returns the messages whose SQL contains s.
func (f *fakeCH) sentContaining(s string) []tunnel.GatewayMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tunnel.GatewayMessage
	for _, m := range f.sent {
		if strings.Contains(m.SQL, s) {
			out = append(out, m)
		}
	}
	return out
}

func fakeDeps(t *testing.T, allowed string) (Deps, *authedKey, *fakeCH) {
	t.Helper()
	db, conn := testutil.WorkerDB(t)
	f := &fakeCH{}
	agent := testutil.NewAgent(t, db, "worker-token", f.respond)
	t.Cleanup(background.Wait) // runs before the database closes
	ak := &authedKey{key: &database.MCPKey{ID: "k1", Name: "test", ConnectionID: conn, CHUser: "default", AllowedDatabases: allowed}}
	return Deps{DB: db, Gateway: agent.Gateway, Config: &config.Config{}}, ak, f
}

func resultText(r *mcp.CallToolResult) string {
	var sb strings.Builder
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String()
}

func TestRunReadQueryEnforcesAllowedDatabases(t *testing.T) {
	deps, ak, f := fakeDeps(t, "analytics")
	ctx := context.Background()

	rejected := []struct{ sql, want string }{
		{"SELECT * FROM hr.salaries", `"hr"`},
		{"SELECT * FROM `hr`.`salaries`", `"hr"`},
		{"select * from analytics.events e join hr.salaries s on e.id = s.id", `"hr"`},
		{"SELECT * FROM analytics.events, hr.salaries", `"hr"`},
		{"SELECT * FROM analytics.events WHERE id IN (SELECT id FROM hr.salaries)", `"hr"`},
		{"SELECT * FROM analytics.events WHERE id IN hr.salaries", `"hr"`},
		{"SELECT position('a', 'b', 1 IN hr.salaries)", `"hr"`},
		{"SELECT extract(YEAR FROM ts), trim(BOTH ' ' FROM s), position(s IN 'x') FROM analytics.events WHERE substring(s, 1 IN hr.salaries)", `"hr"`},
		{"WITH x AS (SELECT * FROM hr.salaries) SELECT * FROM x", `"hr"`},
		{"SELECT (SELECT max(salary) FROM hr.salaries) AS m", `"hr"`},
		{"SELECT * FROM analytics.events UNION ALL SELECT * FROM hr.salaries", `"hr"`},
		{"SELECT * FROM salaries", `"default"`},
		{"SELECT * FROM system.query_log", `"system"`},
		{"SELECT * FROM url('http://example.com/x.csv', CSV)", "url()"},
		{"SELECT * FROM remote('127.0.0.1', hr.salaries)", "remote()"},
		{"SELECT * FROM cluster('default', hr.salaries)", "cluster()"},
		{"SELECT * FROM file('x.csv')", "file()"},
		{"SELECT * FROM s3('https://b/x.parquet')", "s3()"},
		{"SELECT dictGet('hr.dict', 'salary', toUInt64(1))", "dictGet"},
		{"SELECT joinGet('hr.j', 'v', 1)", "joinGet"},
		{"SHOW TABLES FROM hr", "SELECT"},
		{"DESCRIBE TABLE hr.salaries", "SELECT"},
		{"SELECT 1 /* nested /* comment */ */ FROM analytics.events", "could not"},
	}
	for _, tc := range rejected {
		res := runReadQuery(ctx, deps, ak, readQuery{SQL: tc.sql, Endpoint: "/test"})
		if !res.IsError {
			t.Errorf("%q: want rejection, got %s", tc.sql, resultText(res))
			continue
		}
		if !strings.Contains(resultText(res), tc.want) {
			t.Errorf("%q: error should mention %s, got %s", tc.sql, tc.want, resultText(res))
		}
	}
	if got := f.sentContaining("salaries"); len(got) != 0 {
		t.Fatalf("rejected SQL must never reach ClickHouse, sent: %v", got[0].SQL)
	}

	allowed := []string{
		"SELECT count() FROM analytics.events",
		"SELECT 1",
		"SELECT * FROM numbers(10)",
		"WITH x AS (SELECT id FROM analytics.events) SELECT * FROM x",
		"SELECT e.id FROM analytics.events AS e LEFT JOIN analytics.users u ON e.uid = u.id WHERE e.id IN (SELECT id FROM analytics.users) ORDER BY e.id, u.id LIMIT 10",
		"SELECT extract(YEAR FROM ts) FROM analytics.events",
		"SELECT 'FROM hr.salaries' AS s FROM analytics.events -- FROM hr.x",
	}
	for _, sql := range allowed {
		if res := runReadQuery(ctx, deps, ak, readQuery{SQL: sql, Endpoint: "/test"}); res.IsError {
			t.Errorf("%q: want success, got %s", sql, resultText(res))
		}
	}
}

func TestRunReadQueryUnqualifiedInAllowedCurrentDatabase(t *testing.T) {
	deps, ak, _ := fakeDeps(t, "analytics, default")
	if res := runReadQuery(context.Background(), deps, ak, readQuery{SQL: "SELECT * FROM events", Endpoint: "/test"}); res.IsError {
		t.Fatalf("unqualified table in an allowed current database should run: %s", resultText(res))
	}
}

func TestRunReadQueryWithoutAllowListUnchanged(t *testing.T) {
	deps, ak, f := fakeDeps(t, "")
	ctx := context.Background()
	for _, sql := range []string{"SELECT * FROM hr.salaries", "SELECT * FROM url('http://example.com/x.csv', CSV)", "SHOW TABLES FROM hr"} {
		if res := runReadQuery(ctx, deps, ak, readQuery{SQL: sql, Endpoint: "/test"}); res.IsError {
			t.Errorf("%q: key without allow-list should run it, got %s", sql, resultText(res))
		}
	}
	if len(f.sentContaining("hr.salaries")) != 1 {
		t.Error("query should have reached ClickHouse once")
	}
	if len(f.sentContaining("currentDatabase()")) != 0 {
		t.Error("no allow-list means no database lookup")
	}
}

func TestRunReadQueryRunsReadonlyOneWithCaps(t *testing.T) {
	deps, ak, f := fakeDeps(t, "")
	res := runReadQuery(context.Background(), deps, ak, readQuery{SQL: "SELECT 42 AS answer", MaxRows: 5, Endpoint: "/test"})
	if res.IsError {
		t.Fatal(resultText(res))
	}
	sent := f.sentContaining("SELECT 42")
	if len(sent) != 1 {
		t.Fatalf("want one query, got %d", len(sent))
	}
	s := sent[0].Settings
	if s["readonly"] != "1" {
		t.Errorf("readonly = %q, want 1 so the query cannot change its own limits", s["readonly"])
	}
	if s["max_result_rows"] != "6" || s["max_execution_time"] == "" || s["result_overflow_mode"] != "break" {
		t.Errorf("limits must travel as query settings, got %v", s)
	}
}

func TestRunReadQueryRejectsSettingsClause(t *testing.T) {
	deps, ak, f := fakeDeps(t, "")
	for _, sql := range []string{
		"SELECT * FROM numbers(1e9) SETTINGS max_result_rows = 0, max_execution_time = 0",
		"SELECT 1 settings readonly=0",
	} {
		res := runReadQuery(context.Background(), deps, ak, readQuery{SQL: sql, Endpoint: "/test"})
		if !res.IsError || !strings.Contains(resultText(res), "SETTINGS") {
			t.Errorf("%q: want SETTINGS rejection, got %s", sql, resultText(res))
		}
	}
	if len(f.sentContaining("numbers(1e9)")) != 0 {
		t.Error("a query carrying its own SETTINGS must not reach ClickHouse")
	}
	// A column or string named settings is fine.
	if res := runReadQuery(context.Background(), deps, ak, readQuery{SQL: "SELECT 'SETTINGS a=1' AS settings", Endpoint: "/test"}); res.IsError {
		t.Errorf("settings as a name must not be rejected: %s", resultText(res))
	}
}

func TestExplainAndEstimateEnforceAllowedDatabases(t *testing.T) {
	deps, ak, f := fakeDeps(t, "analytics")
	srv := mcp.NewServer(&mcp.Implementation{Name: "t", Version: "0"}, nil)
	registerFreeTools(srv, deps, ak)
	registerCatalogTools(srv, deps, ak)
	ctx := context.Background()
	ct, st := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	client := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "0"}, nil)
	cs, err := client.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer cs.Close()
	for _, tool := range []string{"explain_query", "estimate_query"} {
		res, err := cs.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: map[string]any{"sql": "SELECT * FROM hr.salaries"}})
		if err != nil {
			t.Fatal(err)
		}
		if !res.IsError || !strings.Contains(resultText(res), `"hr"`) {
			t.Errorf("%s: want allow-list rejection, got %s", tool, resultText(res))
		}
	}
	if len(f.sentContaining("salaries")) != 0 {
		t.Error("rejected SQL must not reach ClickHouse")
	}
}
