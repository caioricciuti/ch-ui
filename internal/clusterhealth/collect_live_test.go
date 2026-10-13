// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"

	"github.com/caioricciuti/ch-ui/connector"
)

// Every aggregate query must parse and run on a real server, on the local node
// and fanned out over the server's own cluster.
func TestCollectSamplesLive(t *testing.T) {
	endpoint := os.Getenv("CHUI_TEST_CLICKHOUSE_URL")
	if endpoint == "" {
		t.Skip("set CHUI_TEST_CLICKHOUSE_URL to a disposable test instance")
	}
	client := connector.NewCHClient(endpoint, false)
	user := os.Getenv("CHUI_TEST_CLICKHOUSE_USER")
	if user == "" {
		user = "default"
	}
	exec := ExecFunc(func(sql string) ([]map[string]interface{}, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		res, err := client.Execute(ctx, sql, user, os.Getenv("CHUI_TEST_CLICKHOUSE_PASSWORD"), QuerySettings())
		if err != nil {
			return nil, err
		}
		raw, _ := json.Marshal(res.Data)
		var rows []map[string]interface{}
		if err := json.Unmarshal(raw, &rows); err != nil {
			return nil, err
		}
		return rows, nil
	})
	cluster := ""
	if rows, err := exec(ResolveClusterQuery); err == nil && len(rows) > 0 {
		cluster, _ = rows[0]["cluster"].(string)
	}
	for _, c := range []string{"", cluster} {
		col, err := CollectSamples(exec, c, 30, time.Now().UTC().Format(time.RFC3339))
		if err != nil {
			t.Fatalf("cluster %q: %v", c, err)
		}
		if len(col.Errors) > 0 {
			t.Fatalf("cluster %q: metric errors %v", c, col.Errors)
		}
		if len(col.Samples) != 1 || col.Samples[0].Down || col.Samples[0].DiskUsedPct == nil {
			t.Fatalf("cluster %q: want one healthy local sample, got %+v", c, col.Samples)
		}
	}
}
