package mcpserver

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// Against a real ClickHouse: limits cannot be lifted from inside the SQL and
// the database allow-list holds on run_select.
func TestMCPReadonlyAndAllowListLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	user, password := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if user == "" {
		user = "default"
	}
	client := connector.NewCHClient(endpoint, false)
	execute := func(sql string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := client.Execute(ctx, sql, user, password, nil)
		return err
	}

	suffix := time.Now().UnixNano()
	allowedDB, otherDB := fmt.Sprintf("chui_mcp_allowed_%d", suffix), fmt.Sprintf("chui_mcp_other_%d", suffix)
	for _, sql := range []string{
		"CREATE DATABASE " + allowedDB,
		"CREATE DATABASE " + otherDB,
		"CREATE TABLE " + allowedDB + ".events (id UInt64) ENGINE = MergeTree ORDER BY id",
		"CREATE TABLE " + otherDB + ".secrets (id UInt64) ENGINE = MergeTree ORDER BY id",
		"INSERT INTO " + allowedDB + ".events SELECT number FROM numbers(100)",
		"INSERT INTO " + otherDB + ".secrets VALUES (42)",
	} {
		if err := execute(sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, db := range []string{allowedDB, otherDB} {
			if err := execute("DROP DATABASE IF EXISTS " + db); err != nil {
				t.Error(err)
			}
		}
	})

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	t.Cleanup(background.Wait)
	deps := Deps{DB: db, Gateway: agent.Gateway, Config: &config.Config{}}
	key := func(allowed string) *authedKey {
		return &authedKey{key: &database.MCPKey{ID: "live", Name: "live", ConnectionID: conn, CHUser: user, AllowedDatabases: allowed}, chPassword: password}
	}
	ctx := context.Background()

	t.Run("readonly=1 refuses settings inside the query", func(t *testing.T) {
		_, err := runCHFull(ctx, deps, key(""), "SELECT 1 SETTINGS readonly = 0", nil, queryTimeout)
		if err == nil || !strings.Contains(err.Error(), "readonly") {
			t.Fatalf("want ClickHouse's readonly error, got %v", err)
		}
		res := runReadQuery(ctx, deps, key(""), readQuery{SQL: "SELECT number FROM numbers(100000) SETTINGS max_result_rows = 0, max_execution_time = 0", Endpoint: "/test"})
		if !res.IsError {
			t.Fatalf("a SETTINGS clause must not run: %s", resultText(res))
		}
	})

	t.Run("row cap applies", func(t *testing.T) {
		res := runReadQuery(ctx, deps, key(""), readQuery{SQL: "SELECT id FROM " + allowedDB + ".events", MaxRows: 5, Endpoint: "/test"})
		if res.IsError || !strings.Contains(resultText(res), `"row_count":5`) || !strings.Contains(resultText(res), `"truncated":true`) {
			t.Fatalf("want 5 rows and truncated, got %s", resultText(res))
		}
	})

	t.Run("allow-list", func(t *testing.T) {
		k := key(allowedDB)
		if res := runReadQuery(ctx, deps, k, readQuery{SQL: "SELECT count() AS c FROM " + allowedDB + ".events", Endpoint: "/test"}); res.IsError {
			t.Fatalf("allowed database: %s", resultText(res))
		}
		for _, sql := range []string{
			"SELECT * FROM " + otherDB + ".secrets",
			"SELECT * FROM " + allowedDB + ".events WHERE id IN (SELECT id FROM " + otherDB + ".secrets)",
			"SELECT * FROM remote('localhost', " + otherDB + ".secrets)",
			"SELECT * FROM tables", // unqualified: the current database is not allowed
		} {
			res := runReadQuery(ctx, deps, k, readQuery{SQL: sql, Endpoint: "/test"})
			if !res.IsError || strings.Contains(resultText(res), `"rows"`) {
				t.Errorf("%q must be rejected, got %s", sql, resultText(res))
			}
		}
		if res := runReadQuery(ctx, deps, key(""), readQuery{SQL: "SELECT * FROM " + otherDB + ".secrets", Endpoint: "/test"}); res.IsError {
			t.Errorf("a key without an allow-list keeps full access: %s", resultText(res))
		}
	})
}
