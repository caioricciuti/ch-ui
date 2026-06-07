package clusterhealth

import (
	"strings"
	"testing"
)

func TestIsValidClusterName(t *testing.T) {
	cases := map[string]bool{
		"my_cluster":             true,
		"cluster-1":              true,
		"prod.cluster":           true,
		"abc123":                 true,
		"":                       false,
		"bad name":               false,
		"drop;table":             false,
		"a'b":                    false,
		"x)--":                   false,
		strings.Repeat("a", 129): false,
	}
	for name, want := range cases {
		if got := IsValidClusterName(name); got != want {
			t.Errorf("IsValidClusterName(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestSourceFanOut(t *testing.T) {
	got := source("prod", "replicas")
	if got != "clusterAllReplicas('prod', system.replicas)" {
		t.Errorf("fan-out source = %q", got)
	}
	// Invalid / empty cluster must fall back to the local node, never interpolate.
	if got := source("", "replicas"); got != "system.replicas" {
		t.Errorf("single-node source = %q", got)
	}
	if got := source("bad name", "replicas"); got != "system.replicas" {
		t.Errorf("invalid cluster must fall back, got %q", got)
	}
}

func TestSettingsClamp(t *testing.T) {
	// Zero values become defaults.
	d := Settings{}.Clamp()
	if d.RetentionDays != 7 || d.PollIntervalSeconds != 60 || d.LongQueryThresholdSecs != 30 {
		t.Errorf("zero clamp defaults wrong: %+v", d)
	}
	// Out-of-range values are bounded.
	hi := Settings{RetentionDays: 9999, PollIntervalSeconds: 1, LongQueryThresholdSecs: 99999}.Clamp()
	if hi.RetentionDays != MaxRetentionDays {
		t.Errorf("retention not clamped to max: %d", hi.RetentionDays)
	}
	if hi.PollIntervalSeconds != MinPollIntervalSeconds {
		t.Errorf("poll interval not clamped to min: %d", hi.PollIntervalSeconds)
	}
	if hi.LongQueryThresholdSecs != MaxLongQuerySeconds {
		t.Errorf("threshold not clamped to max: %d", hi.LongQueryThresholdSecs)
	}
}

func TestNumberCoercion(t *testing.T) {
	// ClickHouse JSON quotes 64-bit ints as strings; both shapes must parse.
	if asInt64("12345") != 12345 {
		t.Error("asInt64 string failed")
	}
	if asInt64(float64(42)) != 42 {
		t.Error("asInt64 float failed")
	}
	if asInt64("nope") != 0 {
		t.Error("asInt64 garbage should be 0")
	}
	if asFloat("3.5") != 3.5 {
		t.Error("asFloat string failed")
	}
	if asFloat(float64(2)) != 2 {
		t.Error("asFloat float failed")
	}
	if asString(nil) != "" {
		t.Error("asString nil should be empty")
	}
}

func TestCollectSamplesMerge(t *testing.T) {
	// Fake exec routes each query kind to canned rows so we can assert the
	// per-node merge and parts-pressure computation without ClickHouse.
	exec := func(sql string) ([]map[string]interface{}, error) {
		switch {
		case strings.Contains(sql, "system.replicas") && strings.Contains(sql, "replication_max_delay"):
			return []map[string]interface{}{
				{"node": "n1", "replication_max_delay": float64(5), "replication_queue_total": "10", "replicas_readonly": float64(0)},
				{"node": "n2", "replication_max_delay": float64(120), "replication_queue_total": "3", "replicas_readonly": float64(1)},
			}, nil
		case strings.Contains(sql, "merges_running"):
			return []map[string]interface{}{{"node": "n1", "merges_running": float64(2)}}, nil
		case strings.Contains(sql, "mutations_pending"):
			return []map[string]interface{}{{"node": "n2", "mutations_pending": float64(4)}}, nil
		case strings.Contains(sql, "parts_max_active"):
			return []map[string]interface{}{{"node": "n1", "parts_max_active": "1500"}}, nil
		case strings.Contains(sql, "long_queries"):
			return []map[string]interface{}{{"node": "n2", "long_queries": float64(1)}}, nil
		case strings.Contains(sql, "merge_tree_settings"):
			return []map[string]interface{}{
				{"name": "parts_to_throw_insert", "value": "3000"},
				{"name": "parts_to_delay_insert", "value": "1000"},
			}, nil
		}
		return nil, nil
	}

	samples, limits, err := CollectSamples(exec, "prod", 30, "2026-06-04T00:00:00Z")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if limits.ThrowInsert != 3000 {
		t.Errorf("throw limit = %d", limits.ThrowInsert)
	}
	if len(samples) != 2 {
		t.Fatalf("expected 2 nodes, got %d", len(samples))
	}
	byNode := map[string]Sample{}
	for _, s := range samples {
		byNode[s.Node] = s
	}
	n1 := byNode["n1"]
	if n1.MergesRunning != 2 || n1.PartsMaxActive != 1500 {
		t.Errorf("n1 merge=%d parts=%d", n1.MergesRunning, n1.PartsMaxActive)
	}
	if pct := n1.PartsPressurePct; pct < 49 || pct > 51 {
		t.Errorf("n1 parts pressure pct = %f, want ~50", pct)
	}
	n2 := byNode["n2"]
	if n2.ReplicationMaxDelay != 120 || n2.ReplicasReadonly != 1 || n2.MutationsPending != 4 || n2.LongQueries != 1 {
		t.Errorf("n2 merge wrong: %+v", n2)
	}
	if n2.ReplicationQueueTotal != 3 {
		t.Errorf("n2 queue (from quoted int) = %d, want 3", n2.ReplicationQueueTotal)
	}
}
