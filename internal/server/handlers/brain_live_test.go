package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/brain/tools"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

// Against a real ClickHouse: Ask AI sees views and column comments, ranks by
// column names, and Brain's run_query handles DESCRIBE and refuses FORMAT.
func TestBrainSchemaAndQueriesLive(t *testing.T) {
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
	dbName := fmt.Sprintf("chui_brain_live_%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = execute("DROP DATABASE IF EXISTS " + dbName) })
	for _, sql := range []string{
		"CREATE DATABASE " + dbName,
		"CREATE TABLE " + dbName + ".t_a (zzq_revenue Float64 COMMENT 'gross revenue in EUR', country String) ENGINE = MergeTree ORDER BY tuple() COMMENT 'sales facts'",
		"CREATE VIEW " + dbName + ".v_b AS SELECT country FROM " + dbName + ".t_a",
	} {
		if err := execute(sql); err != nil {
			t.Fatalf("%s: %v", sql, err)
		}
	}

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewAgent(t, db, "worker-token", func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, msg.SQL, user, password, msg.Settings)
		if err != nil {
			return &tunnel.AgentMessage{Type: "query_error", Error: err.Error()}
		}
		data, _ := json.Marshal(res.Data)
		meta, _ := json.Marshal(res.Meta)
		return &tunnel.AgentMessage{Type: "query_result", Data: data, Meta: meta}
	})
	h := &BrainHandler{DB: db, Gateway: agent.Gateway}

	schema, used := h.buildSchemaContext(context.Background(), conn, user, password, "zzq_revenue per country")
	if len(used) == 0 || used[0] != dbName+".t_a" {
		t.Errorf("column match should rank %s.t_a first, got %v", dbName, used)
	}
	if !strings.Contains(schema, "gross revenue in EUR") || !strings.Contains(schema, "sales facts") {
		t.Errorf("column and table comments missing:\n%s", schema)
	}
	if !strings.Contains(schema, dbName+".v_b (View)") {
		t.Errorf("views should be included:\n%s", schema)
	}

	tctx := tools.Context{Ctx: context.Background(), ConnectionID: conn, CHUser: user, CHPassword: password, Gateway: agent.Gateway}
	reg := tools.New()
	tools.RegisterRead(reg)
	if _, err := reg.Execute(tctx, "run_query", json.RawMessage(`{"sql":"DESCRIBE TABLE `+dbName+`.t_a"}`)); err != nil {
		t.Errorf("DESCRIBE should run without an injected LIMIT: %v", err)
	}
	if _, err := reg.Execute(tctx, "run_query", json.RawMessage(`{"sql":"SELECT 1 FORMAT CSV"}`)); err == nil || !strings.Contains(err.Error(), "FORMAT") {
		t.Errorf("FORMAT should be refused, got %v", err)
	}
	out, err := reg.Execute(tctx, "run_query", json.RawMessage(`{"sql":"SELECT number FROM numbers(5000)","limit":1000}`))
	if err != nil {
		t.Fatal(err)
	}
	view := tools.ModelView("run_query", out)
	if !strings.Contains(view, "truncated: true") || len(view) > tools.ModelMaxBytes+512 {
		t.Errorf("model view of 1000 rows should be truncated to 100: %d bytes", len(view))
	}
}
