// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package handlers

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/config"
	"github.com/caioricciuti/ch-ui/internal/performance"
	"github.com/caioricciuti/ch-ui/internal/testutil"
)

// A user whose ClickHouse profile is readonly=1 refuses every setting the Pro
// read paths send (limits, log_comment). Those paths must still work for them.
func TestReadonlyProfileUserLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	admin, adminPassword := os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD")
	if admin == "" {
		admin = "default"
	}
	execute := func(sql string) error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_, err := client.Execute(ctx, sql, admin, adminPassword, nil)
		return err
	}

	suffix := time.Now().UnixNano()
	profile, user, database := fmt.Sprintf("chui_ro_profile_%d", suffix), fmt.Sprintf("chui_ro_user_%d", suffix), fmt.Sprintf("chui_ro_db_%d", suffix)
	password := "ro-test-only"
	for _, sql := range []string{
		"CREATE SETTINGS PROFILE " + profile + " SETTINGS readonly = 1",
		"CREATE USER " + user + " IDENTIFIED WITH plaintext_password BY '" + password + "' SETTINGS PROFILE '" + profile + "'",
		"CREATE DATABASE " + database,
		"CREATE TABLE " + database + ".events (id UInt64, body String) ENGINE = MergeTree ORDER BY id",
		"GRANT SELECT ON " + database + ".* TO " + user,
	} {
		if err := execute(sql); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		for _, sql := range []string{"DROP USER IF EXISTS " + user, "DROP SETTINGS PROFILE IF EXISTS " + profile, "DROP DATABASE IF EXISTS " + database} {
			if err := execute(sql); err != nil {
				t.Error(err)
			}
		}
	})

	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, nil)

	t.Run("schema compare snapshot", func(t *testing.T) {
		h := &SchemaCompareHandler{DB: db, Gateway: agent.Gateway, Config: &config.Config{}}
		tables, err := h.snapshot(context.Background(), chSession{connID: conn, user: user, password: password}, database)
		if err != nil {
			t.Fatal(err)
		}
		if len(tables) != 1 || len(tables[0].Columns) != 2 {
			t.Fatalf("want one table with two columns, got %+v", tables)
		}
	})

	t.Run("performance settings", func(t *testing.T) {
		result, err := agent.Gateway.ExecuteQueryWithSettings(conn, "SELECT count() AS c FROM "+database+".events", user, password,
			map[string]string{"log_comment": performance.LogComment, "readonly": "1", "max_execution_time": "25", "max_memory_usage": "536870912", "max_threads": "2"}, 30*time.Second)
		if err != nil {
			t.Fatal(err)
		}
		if result == nil || len(result.Data) == 0 {
			t.Fatal("empty result")
		}
	})

	t.Run("writes stay blocked", func(t *testing.T) {
		_, err := agent.Gateway.ExecuteQueryWithSettings(conn, "INSERT INTO "+database+".events VALUES (1, 'x')", user, password,
			map[string]string{"readonly": "1", "max_execution_time": "25"}, 30*time.Second)
		if err == nil {
			t.Fatal("a readonly profile must still refuse writes")
		}
	})
}
