package handlers

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/schemacompare"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

func TestSchemaCompareLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	user, password := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if user == "" {
		user = "default"
	}
	execute := func(sql string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := client.Execute(ctx, sql, user, password, nil)
		return err
	}
	prefix := fmt.Sprintf("chui_schema_%d", time.Now().UnixNano())
	source, target := prefix+"_source", prefix+"_target"
	for _, name := range []string{source, target} {
		if err := execute("CREATE DATABASE " + name); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := execute("DROP DATABASE " + name); err != nil {
				t.Error(err)
			}
		})
	}
	for _, sql := range []string{
		"CREATE TABLE " + source + ".events (id UInt64, ts DateTime, body String CODEC(ZSTD)) ENGINE=MergeTree ORDER BY id TTL ts + INTERVAL 7 DAY",
		"CREATE TABLE " + target + ".events (id UInt32, ts DateTime, body String) ENGINE=MergeTree ORDER BY ts",
		"CREATE VIEW " + source + ".recent AS SELECT id FROM " + source + ".events",
	} {
		if err := execute(sql); err != nil {
			t.Fatal(err)
		}
	}
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)
	h := &SchemaCompareHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{}}
	credentials := chSession{connID: conn, user: user, password: password}
	a, err := h.snapshot(context.Background(), credentials, source)
	if err != nil {
		t.Fatal(err)
	}
	b, err := h.snapshot(context.Background(), credentials, target)
	if err != nil {
		t.Fatal(err)
	}
	result := schemacompare.Compare(a, b, target)
	if result.SourceTables != 2 || result.TargetTables != 1 {
		t.Fatalf("metadata counts: %+v", result)
	}
	if len(result.Differences) < 4 {
		t.Fatalf("expected column, key, TTL and missing-view differences: %+v", result.Differences)
	}
	for _, line := range strings.Split(result.ReviewSQL, "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "--") {
			t.Fatalf("executable plan line: %s", line)
		}
	}
	for _, msg := range agent.Messages() {
		if msg.Settings["readonly"] != "1" {
			t.Fatal("introspection query lacked readonly setting")
		}
	}
}
