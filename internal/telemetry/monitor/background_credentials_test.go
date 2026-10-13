// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/telemetry"
	"github.com/caioricciuti/ch-ui/internal/testutil"
	"github.com/caioricciuti/ch-ui/internal/tunnel"
)

func TestBackgroundAccountExecution(t *testing.T) {
	db, conn := testutil.WorkerDB(t)
	agent := testutil.NewWorkerAgent(t, db, func(msg tunnel.GatewayMessage) *tunnel.AgentMessage {
		data := `[{"c":0}]`
		if strings.HasPrefix(msg.SQL, "DESCRIBE") {
			data = `[{"name":"Timestamp","type":"DateTime64(9)"},{"name":"Body","type":"String"}]`
		}
		return &tunnel.AgentMessage{Type: "query_result", Data: json.RawMessage(data)}
	})
	// Against a live ClickHouse the fixture above is bypassed, so the logs
	// table has to exist. A unique name per run keeps parallel runs apart.
	table := "otel_logs"
	if endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL"); endpoint != "" {
		table = fmt.Sprintf("chui_bg_monitor_logs_%d", time.Now().UnixNano())
		exec := func(sql string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_, err := connector.NewCHClient(endpoint, false).Execute(ctx, sql, os.Getenv("CHUI_TEST_CLICKHOUSE_USER"), os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), nil)
			return err
		}
		if err := exec("CREATE TABLE default." + table + " (Timestamp DateTime64(9), Body String) ENGINE = MergeTree ORDER BY Timestamp"); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := exec("DROP TABLE IF EXISTS default." + table); err != nil {
				t.Error(err)
			}
		})
	}
	mapping := telemetry.DefaultLogsMapping()
	source, err := db.CreateTelemetrySource(&telemetry.Source{ConnectionID: conn, Name: "logs", Kind: telemetry.KindLogs, Database: "default", Table: table, Logs: &mapping, Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	id, err := db.CreateTelemetryMonitor(&database.TelemetryMonitor{ConnectionID: conn, Name: "monitor", Kind: "logs", SourceID: source, WindowSeconds: 60, IntervalSeconds: 30, Comparator: "gt", Threshold: 10, Severity: "warn", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	r := NewMonitorRunner(db, agent.Gateway, "test-secret", func() bool { return true })
	agent.CheckWorker(t, db, conn, "telemetry.monitor", func() {
		if _, err := db.Conn().Exec("UPDATE telemetry_monitors SET last_run_at=NULL WHERE id=?", id); err != nil {
			t.Fatal(err)
		}
		r.tick()
	})
	m, err := db.GetTelemetryMonitor(id)
	if err != nil || m.LastState != "ok" || m.LastValue == nil || *m.LastValue != 0 {
		t.Fatalf("monitor result: %#v %v", m, err)
	}
}
