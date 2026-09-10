package mcpserver

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/crypto"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestIsReadOnlyStatement(t *testing.T) {
	allowed := []string{
		"SELECT 1",
		"  select * from t",
		"WITH x AS (SELECT 1) SELECT * FROM x",
		"(SELECT 1)",
		"SHOW TABLES",
		"DESCRIBE TABLE t",
		"DESC t",
		"EXPLAIN SELECT 1",
		"EXISTS t",
		"-- comment\nSELECT 1",
		"/* block */ SELECT 1",
	}
	for _, q := range allowed {
		if !isReadOnlyStatement(q) {
			t.Errorf("expected allowed: %q", q)
		}
	}
	denied := []string{
		"INSERT INTO t VALUES (1)",
		"DROP TABLE t",
		"ALTER TABLE t DELETE WHERE 1",
		"TRUNCATE TABLE t",
		"CREATE TABLE t (x Int64) ENGINE = Memory",
		"KILL QUERY WHERE 1",
		"SET max_threads = 1",
		"OPTIMIZE TABLE t",
		"SELECT * FROM t INTO OUTFILE '/tmp/x'",
		"-- comment\nINSERT INTO t VALUES (1)",
		"GRANT SELECT ON *.* TO u",
	}
	for _, q := range denied {
		if isReadOnlyStatement(q) {
			t.Errorf("expected denied: %q", q)
		}
	}
}

func TestTrailingFormatRe(t *testing.T) {
	if !trailingFormatRe.MatchString("SELECT 1 FORMAT CSV") {
		t.Error("expected FORMAT CSV to match")
	}
	if !trailingFormatRe.MatchString("SELECT 1 format JSONEachRow;") {
		t.Error("expected lowercase format to match")
	}
	if trailingFormatRe.MatchString("SELECT formatDateTime(now(), '%Y')") {
		t.Error("formatDateTime should not match")
	}
}

func TestSanitizeForHistory(t *testing.T) {
	q := "SELECT * FROM s3('https://bucket/x.csv', 'AKIA123', 'supersecret', 'CSV')"
	got := sanitizeForHistory(q)
	if strings.Contains(got, "supersecret") || strings.Contains(got, "AKIA123") {
		t.Errorf("credentials survived sanitization: %s", got)
	}
	plain := "SELECT 'hello' FROM t"
	if sanitizeForHistory(plain) != plain {
		t.Errorf("plain query should be untouched, got %s", sanitizeForHistory(plain))
	}
}

func TestGenerateAndHashKey(t *testing.T) {
	plaintext, hash, prefix := GenerateKey()
	if !keyFormatRe.MatchString(plaintext) {
		t.Fatalf("bad key format: %s", plaintext)
	}
	if HashKey(plaintext) != hash {
		t.Fatal("hash mismatch")
	}
	if !strings.HasPrefix(plaintext, strings.TrimSuffix(prefix, "...")) {
		t.Fatalf("prefix %q not a prefix of key", prefix)
	}
}

func TestAllowedDBs(t *testing.T) {
	k := &database.MCPKey{AllowedDatabases: " analytics, logs "}
	if !dbAllowed(k, "analytics") || !dbAllowed(k, "logs") {
		t.Error("allowlisted databases should pass")
	}
	if dbAllowed(k, "secrets") {
		t.Error("non-listed database should be denied")
	}
	open := &database.MCPKey{}
	if !dbAllowed(open, "anything") {
		t.Error("empty allowlist means unrestricted")
	}
}

// testDeps spins up a real SQLite store with one connection and one MCP key.
func testDeps(t *testing.T) (Deps, string) {
	t.Helper()
	db, err := database.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	connID, err := db.CreateConnection(database.CreateConnectionParams{
		Name:        "test-conn",
		TunnelToken: "cht_00000000000000000000000000000000",
		Type:        database.ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("create connection: %v", err)
	}

	cfg := &config.Config{AppSecretKey: "test-secret-key-for-mcp-tests"}
	enc, err := crypto.Encrypt("chpass", cfg.AppSecretKey)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	plaintext, hash, prefix := GenerateKey()
	if _, err := db.CreateMCPKey(database.CreateMCPKeyParams{Name: "test key", KeyHash: hash, KeyPrefix: prefix, ConnectionID: connID, CHUser: "default", CHPasswordEnc: enc, Scopes: "read", CreatedBy: "admin"}); err != nil {
		t.Fatalf("create mcp key: %v", err)
	}

	return Deps{DB: db, Gateway: tunnel.NewGateway(db), Config: cfg}, plaintext
}

func mcpRequest(t *testing.T, h http.Handler, auth, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

const initializeBody = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0.0.0"}}}`

func TestAuthRejections(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)

	if rec := mcpRequest(t, h, "", initializeBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("no key: want 401, got %d", rec.Code)
	}
	if rec := mcpRequest(t, h, "not-a-key", initializeBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("malformed key: want 401, got %d", rec.Code)
	}
	wrong, _, _ := GenerateKey()
	if rec := mcpRequest(t, h, wrong, initializeBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("unknown key: want 401, got %d", rec.Code)
	}

	// Revoke the good key: it must stop working.
	keys, err := deps.DB.ListMCPKeys()
	if err != nil || len(keys) != 1 {
		t.Fatalf("list keys: %v (%d)", err, len(keys))
	}
	if err := deps.DB.RevokeMCPKey(keys[0].ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if rec := mcpRequest(t, h, key, initializeBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("revoked key: want 401, got %d", rec.Code)
	}
}

func TestInitializeAndToolList(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)

	rec := mcpRequest(t, h, key, initializeBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("initialize: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "ch-ui") {
		t.Errorf("initialize response should carry server info, got: %s", rec.Body.String())
	}

	rec = mcpRequest(t, h, key, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/list: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, tool := range []string{"list_databases", "list_tables", "describe_table", "run_select", "explain_query"} {
		if !strings.Contains(body, tool) {
			t.Errorf("tools/list missing %q", tool)
		}
	}
	// Non-Pro config: Pro tools must not be visible.
	for _, tool := range []string{"query_insights_top", "costs_summary"} {
		if strings.Contains(body, tool) {
			t.Errorf("tools/list should not expose Pro tool %q without a license", tool)
		}
	}
}

func TestRunSelectRejectsWrites(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)

	call := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"run_select","arguments":{"sql":"DROP TABLE users"}}}`
	rec := mcpRequest(t, h, key, call)
	if rec.Code != http.StatusOK {
		t.Fatalf("tools/call transport: want 200, got %d: %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "isError") || !strings.Contains(body, "read-only") {
		t.Errorf("write statement should be rejected as a tool error, got: %s", body)
	}
}

func TestRunSelectOfflineTunnel(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)

	call := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"run_select","arguments":{"sql":"SELECT 1"}}}`
	rec := mcpRequest(t, h, key, call)
	body := rec.Body.String()
	if !strings.Contains(body, "offline") {
		t.Errorf("expected offline-tunnel tool error, got: %s", body)
	}
}

// sanity: the JSON envelope of tool results decodes.
func TestErrResultShape(t *testing.T) {
	res := errResult("boom %d", 42)
	if !res.IsError {
		t.Fatal("expected IsError")
	}
	b, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), "boom 42") {
		t.Errorf("unexpected: %s", b)
	}
}

// sseData returns the JSON payload of the first "data:" line in a streamable
// HTTP response, which the SDK frames as server-sent events.
func sseData(t *testing.T, body string) []byte {
	t.Helper()
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "data:") {
			return []byte(strings.TrimSpace(strings.TrimPrefix(line, "data:")))
		}
	}
	t.Fatalf("no data line in response: %s", body)
	return nil
}

// Every tool must carry a title and spec annotations: clients use
// readOnlyHint/destructiveHint to decide whether to ask for confirmation, and
// connector directories reject tools without them.
func TestToolAnnotations(t *testing.T) {
	// Write-scope key so the additive tools are listed too.
	deps, _, key := writeDeps(t)
	h := Handler(deps)

	rec := mcpRequest(t, h, key, initializeBody)
	if !strings.Contains(rec.Body.String(), "schema-first") {
		t.Errorf("initialize should carry server instructions, got: %s", rec.Body.String())
	}

	rec = mcpRequest(t, h, key, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)
	var resp struct {
		Result struct {
			Tools []struct {
				Name        string `json:"name"`
				Title       string `json:"title"`
				Annotations *struct {
					Title           string `json:"title"`
					ReadOnlyHint    bool   `json:"readOnlyHint"`
					DestructiveHint *bool  `json:"destructiveHint"`
				} `json:"annotations"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(sseData(t, rec.Body.String()), &resp); err != nil {
		t.Fatalf("decode tools/list: %v: %s", err, rec.Body.String())
	}
	if len(resp.Result.Tools) < 13 {
		t.Fatalf("expected free + list + write tools, got %d", len(resp.Result.Tools))
	}
	writeTools := map[string]bool{"save_query": true, "create_dashboard": true, "create_model": true, "create_pipeline": true}
	for _, tool := range resp.Result.Tools {
		if tool.Title == "" || tool.Annotations == nil || tool.Annotations.Title == "" {
			t.Errorf("%s: missing title or annotations", tool.Name)
			continue
		}
		if writeTools[tool.Name] {
			if tool.Annotations.ReadOnlyHint {
				t.Errorf("%s: write tool must not be readOnlyHint", tool.Name)
			}
			if tool.Annotations.DestructiveHint == nil || *tool.Annotations.DestructiveHint {
				t.Errorf("%s: additive tool must set destructiveHint=false", tool.Name)
			}
		} else if !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s: read tool must be readOnlyHint", tool.Name)
		}
	}
}
