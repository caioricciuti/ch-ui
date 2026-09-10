package mcpserver

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/internal/database"
)

func TestKeyLimiter(t *testing.T) {
	l := &keyLimiter{windows: make(map[string]*rateEntry)}
	for i := 0; i < rateLimit; i++ {
		if !l.allow("k1") {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}
	if l.allow("k1") {
		t.Error("request past the limit should be denied")
	}
	if !l.allow("k2") {
		t.Error("another key has its own budget")
	}
	// Expire the window: the key is allowed again.
	l.windows["k1"].start = time.Now().Add(-2 * rateWindow)
	if !l.allow("k1") {
		t.Error("new window should be allowed")
	}
}

func TestRateLimitedRequestGets429(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)

	keys, _ := deps.DB.ListMCPKeys()
	limiter.mu.Lock()
	limiter.windows[keys[0].ID] = &rateEntry{start: time.Now(), count: rateLimit}
	limiter.mu.Unlock()
	t.Cleanup(func() {
		limiter.mu.Lock()
		delete(limiter.windows, keys[0].ID)
		limiter.mu.Unlock()
	})

	rec := mcpRequest(t, h, key, initializeBody)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("want 429, got %d: %s", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Error("429 should carry Retry-After")
	}
}

func TestListSavedQueriesScopedToConnection(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)

	keys, _ := deps.DB.ListMCPKeys()
	mine := keys[0].ConnectionID
	otherConn, err := deps.DB.CreateConnection(database.CreateConnectionParams{
		Name: "other", TunnelToken: "cht_11111111111111111111111111111111", Type: database.ConnectionTypeTunnel,
	})
	if err != nil {
		t.Fatalf("create other connection: %v", err)
	}
	for name, conn := range map[string]string{"mine": mine, "theirs": otherConn, "global": ""} {
		if _, err := deps.DB.CreateSavedQuery(database.CreateSavedQueryParams{Name: name, Query: "SELECT 1", ConnectionID: conn}); err != nil {
			t.Fatalf("create saved query %s: %v", name, err)
		}
	}

	rec := mcpRequest(t, h, key, `{"jsonrpc":"2.0","id":5,"method":"tools/call","params":{"name":"list_saved_queries","arguments":{}}}`)
	body := string(sseData(t, rec.Body.String()))
	// the tool payload is JSON inside a text content block, so quotes are escaped
	if !strings.Contains(body, `\"name\":\"mine\"`) || !strings.Contains(body, `\"name\":\"global\"`) {
		t.Errorf("expected own and global queries, got: %s", body)
	}
	if strings.Contains(body, `\"name\":\"theirs\"`) {
		t.Errorf("saved query of another connection leaked: %s", body)
	}
}

func TestEveryToolCallIsAudited(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)

	mcpRequest(t, h, key, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_dashboards","arguments":{}}}`)

	// audit rows are written asynchronously
	deadline := time.Now().Add(2 * time.Second)
	for {
		logs, err := deps.DB.GetAuditLogs(20)
		if err != nil {
			t.Fatalf("audit logs: %v", err)
		}
		for _, l := range logs {
			if l.Action == "mcp.tool.call" && l.Details != nil && strings.Contains(*l.Details, "tool: list_dashboards") {
				b, _ := json.Marshal(l)
				t.Logf("audit row: %s", b)
				return
			}
		}
		if time.Now().After(deadline) {
			t.Fatal("no mcp.tool.call audit row for list_dashboards")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestExpiredAndRotatedKeys(t *testing.T) {
	deps, key := testDeps(t)
	h := Handler(deps)
	keys, _ := deps.DB.ListMCPKeys()
	conn := keys[0].ConnectionID

	// An expired key is refused with 401.
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	expPlain, expHash, expPrefix := GenerateKey()
	if _, err := deps.DB.CreateMCPKey(database.CreateMCPKeyParams{Name: "expired", KeyHash: expHash, KeyPrefix: expPrefix, ConnectionID: conn, CHUser: "default", CHPasswordEnc: keys[0].CHPasswordEnc, ExpiresAt: &past}); err != nil {
		t.Fatalf("create expired key: %v", err)
	}
	if rec := mcpRequest(t, h, expPlain, initializeBody); rec.Code != http.StatusUnauthorized || !strings.Contains(rec.Body.String(), "expired") {
		t.Errorf("expired key: want 401 expired, got %d %s", rec.Code, rec.Body.String())
	}

	// A future expiry still works.
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	okPlain, okHash, okPrefix := GenerateKey()
	if _, err := deps.DB.CreateMCPKey(database.CreateMCPKeyParams{Name: "fresh", KeyHash: okHash, KeyPrefix: okPrefix, ConnectionID: conn, CHUser: "default", CHPasswordEnc: keys[0].CHPasswordEnc, ExpiresAt: &future}); err != nil {
		t.Fatalf("create fresh key: %v", err)
	}
	if rec := mcpRequest(t, h, okPlain, initializeBody); rec.Code != http.StatusOK {
		t.Errorf("fresh key: want 200, got %d", rec.Code)
	}

	// Rotation: old secret dies, new one carries the binding.
	newPlain, newHash, newPrefix := GenerateKey()
	rotated, err := deps.DB.RotateMCPKey(keys[0].ID, newHash, newPrefix, "admin")
	if err != nil {
		t.Fatalf("rotate: %v", err)
	}
	if rotated.ConnectionID != conn || rotated.Scopes != keys[0].Scopes || rotated.Name != keys[0].Name {
		t.Errorf("rotated key lost its binding: %+v", rotated)
	}
	if rec := mcpRequest(t, h, key, initializeBody); rec.Code != http.StatusUnauthorized {
		t.Errorf("old secret after rotation: want 401, got %d", rec.Code)
	}
	if rec := mcpRequest(t, h, newPlain, initializeBody); rec.Code != http.StatusOK {
		t.Errorf("new secret after rotation: want 200, got %d", rec.Code)
	}
	if _, err := deps.DB.RotateMCPKey(keys[0].ID, "x", "y", "admin"); err == nil {
		t.Error("rotating an already revoked key should fail")
	}
}
