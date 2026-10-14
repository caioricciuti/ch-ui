// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"errors"
	"strings"
	"testing"

	"github.com/caioricciuti/ch-ui/internal/database"
	"github.com/caioricciuti/ch-ui/internal/database/dbtest"
)

// fakeExec answers the aggregate queries for two reachable nodes of a
// three-node cluster, and fails the merges query.
func fakeExec(sql string) ([]map[string]interface{}, error) {
	switch {
	case strings.Contains(sql, "system.one"):
		return []map[string]interface{}{{"node": "ch-1"}, {"node": "ch-2"}}, nil
	case strings.Contains(sql, "FROM system.clusters"):
		return []map[string]interface{}{
			{"host_name": "localhost", "is_local": float64(1), "self": "ch-1"},
			{"host_name": "ch-2.prod.internal", "is_local": float64(0), "self": "ch-1"},
			{"host_name": "ch-3.prod.internal", "is_local": float64(0), "self": "ch-1"},
		}, nil
	case strings.Contains(sql, "system.merges"):
		return nil, errors.New("Code: 497. Not enough privileges")
	case strings.Contains(sql, "system.mutations"):
		return []map[string]interface{}{{"node": "ch-1", "mutations_pending": "2", "mutations_oldest_age_seconds": "1200"}}, nil
	case strings.Contains(sql, "system.disks"):
		return []map[string]interface{}{{"node": "ch-1", "disk_used_pct": 71.5}, {"node": "ch-2", "disk_used_pct": 12.0}}, nil
	default:
		return nil, nil
	}
}

func TestCollectSamplesMarksFailedMetricsUnknown(t *testing.T) {
	col, err := CollectSamples(fakeExec, "prod", 30, "2026-10-12T00:00:00Z")
	if err != nil {
		t.Fatal(err)
	}
	if col.Errors[MetricMerges] == "" {
		t.Fatalf("merges error not recorded: %+v", col.Errors)
	}
	byNode := map[string]Sample{}
	for _, s := range col.Samples {
		byNode[s.Node] = s
	}
	for _, name := range []string{"ch-1", "ch-2"} {
		s, ok := byNode[name]
		if !ok {
			t.Fatalf("node %s missing: %+v", name, col.Samples)
		}
		if !s.IsUnknown(MetricMerges) || s.Down {
			t.Errorf("%s: merges must be unknown and node up, got %+v", name, s)
		}
		if s.IsUnknown(MetricReplication) || s.IsUnknown(MetricMutations) {
			t.Errorf("%s: healthy metrics marked unknown: %v", name, s.Unknown)
		}
	}
	if byNode["ch-1"].MutationsOldestAgeSec != 1200 || byNode["ch-1"].DiskUsedPct == nil || *byNode["ch-1"].DiskUsedPct != 71.5 {
		t.Errorf("ch-1 live fields wrong: %+v", byNode["ch-1"])
	}
	down, ok := byNode["ch-3.prod.internal"]
	if !ok || !down.Down || !down.IsUnknown(MetricReplication) {
		t.Fatalf("ch-3 must be flagged down with unknown metrics, got %+v (ok=%v)", down, ok)
	}
	if len(col.Samples) != 3 {
		t.Errorf("localhost and ch-2 must match reachable nodes, got %d samples", len(col.Samples))
	}
}

func TestFlagMissingNodesAfterFallbackIsNotDown(t *testing.T) {
	samples := FlagMissingNodes(fakeExec, "prod", []Sample{{Node: "ch-1"}}, "t", false)
	if len(samples) != 3 {
		t.Fatalf("want 3 samples, got %+v", samples)
	}
	for _, s := range samples[1:] {
		if s.Down || !s.IsUnknown(MetricReplication) {
			t.Errorf("fallback node must be unknown, not down: %+v", s)
		}
	}
}

func TestStoreKeepsUnknownAndDown(t *testing.T) {
	db, err := dbtest.Open(t, "ch.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	conn, err := db.CreateConnection(database.CreateConnectionParams{Name: "c", TunnelToken: "c"})
	if err != nil {
		t.Fatal(err)
	}
	store := NewStore(db)
	at := "2026-10-12T00:00:00Z"
	in := []Sample{
		{Node: "up", CapturedAt: at, MergesRunning: 3, Unknown: []string{MetricLongQueries}},
		{Node: "gone", CapturedAt: at, Down: true, Unknown: append([]string{}, storedMetrics...)},
	}
	if err := store.InsertSamples(conn, "prod", in); err != nil {
		t.Fatal(err)
	}
	out, err := store.GetHistory(conn, "2026-01-01T00:00:00Z")
	if err != nil || len(out) != 2 {
		t.Fatalf("history: %v %+v", err, out)
	}
	for _, h := range out {
		switch h.Node {
		case "up":
			if h.Down || !h.IsUnknown(MetricLongQueries) || h.IsUnknown(MetricMerges) || h.MergesRunning != 3 || h.LongQueries != 0 {
				t.Errorf("up node decoded wrong: %+v", h.Sample)
			}
		case "gone":
			if !h.Down || h.ReplicationMaxDelay != 0 {
				t.Errorf("down node decoded wrong: %+v", h.Sample)
			}
		}
	}
}

func TestOnCluster(t *testing.T) {
	if got := OnCluster("prod"); got != " ON CLUSTER `prod`" {
		t.Errorf("OnCluster(prod) = %q", got)
	}
	if got := OnCluster("bad`name"); got != "" {
		t.Errorf("invalid cluster must not be interpolated, got %q", got)
	}
}
