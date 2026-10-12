// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

package clusterhealth

import (
	"net"
	"strconv"
	"strings"
)

// ExecFunc runs a ClickHouse SQL statement and returns the result rows as a
// slice of column→value maps (ClickHouse JSON format). Both the HTTP handler
// (via the tunnel gateway) and the background harvester supply one of these so
// sample collection logic is shared.
type ExecFunc func(sql string) ([]map[string]interface{}, error)

// PartsLimits holds the server-default MergeTree thresholds used to score parts
// pressure as a percentage.
type PartsLimits struct {
	ThrowInsert int64 `json:"parts_to_throw_insert"`
	DelayInsert int64 `json:"parts_to_delay_insert"`
}

// Collection is one poll: a Sample per node, the parts limits, and the error
// of every metric group that failed (keyed by Metric*). A failed group is
// also listed in each sample's Unknown so it is never mistaken for zero.
type Collection struct {
	Samples []Sample
	Limits  PartsLimits
	Errors  map[string]string
}

// CollectSamples runs the per-node aggregate queries and merges them into one
// Sample per node. A failing metric group does not abort the snapshot: it is
// recorded in Errors and marked unknown on every node. The returned error is
// the fan-out failure (node presence or replication) that should make the
// caller retry on the local node only. On a cluster, nodes listed in
// system.clusters that returned nothing are added as Down.
// capturedAt is stamped by the caller so every node in a poll shares a timestamp.
func CollectSamples(exec ExecFunc, cluster string, longQueryThreshold int, capturedAt string) (Collection, error) {
	byNode := map[string]*Sample{}
	order := []string{}
	node := func(name string) *Sample {
		if s, ok := byNode[name]; ok {
			return s
		}
		s := &Sample{Node: name, CapturedAt: capturedAt}
		byNode[name] = s
		order = append(order, name)
		return s
	}
	errs := map[string]string{}

	// Presence seeds one sample per reachable node, so a node with no
	// replicated tables, merges or parts still shows up (and is not later
	// mistaken for a missing node).
	var fanOutErr error
	if rows, err := exec(NodesQuery(cluster)); err != nil {
		fanOutErr = err
		errs["nodes"] = err.Error()
	} else {
		for _, r := range rows {
			node(asString(r["node"]))
		}
	}

	if rows, err := exec(ReplicationAggQuery(cluster)); err != nil {
		if fanOutErr == nil {
			fanOutErr = err
		}
		errs[MetricReplication] = err.Error()
	} else {
		for _, r := range rows {
			s := node(asString(r["node"]))
			s.ReplicationMaxDelay = asFloat(r["replication_max_delay"])
			s.ReplicationQueueTotal = asInt64(r["replication_queue_total"])
			s.ReplicasReadonly = asInt64(r["replicas_readonly"])
		}
	}

	if rows, err := exec(MergesAggQuery(cluster)); err != nil {
		errs[MetricMerges] = err.Error()
	} else {
		for _, r := range rows {
			node(asString(r["node"])).MergesRunning = asInt64(r["merges_running"])
		}
	}
	if rows, err := exec(MutationsAggQuery(cluster)); err != nil {
		errs[MetricMutations] = err.Error()
	} else {
		for _, r := range rows {
			s := node(asString(r["node"]))
			s.MutationsPending = asInt64(r["mutations_pending"])
			s.MutationsOldestAgeSec = asInt64(r["mutations_oldest_age_seconds"])
		}
	}
	if rows, err := exec(PartsAggQuery(cluster)); err != nil {
		errs[MetricParts] = err.Error()
	} else {
		for _, r := range rows {
			node(asString(r["node"])).PartsMaxActive = asInt64(r["parts_max_active"])
		}
	}
	if rows, err := exec(LongQueriesAggQuery(cluster, longQueryThreshold)); err != nil {
		errs[MetricLongQueries] = err.Error()
	} else {
		for _, r := range rows {
			node(asString(r["node"])).LongQueries = asInt64(r["long_queries"])
		}
	}
	if rows, err := exec(DiskAggQuery(cluster)); err != nil {
		errs[MetricDisk] = err.Error()
	} else {
		for _, r := range rows {
			v := asFloat(r["disk_used_pct"])
			node(asString(r["node"])).DiskUsedPct = &v
		}
	}

	limits := PartsLimits{ThrowInsert: 3000, DelayInsert: 1000}
	if rows, err := exec(PartsLimitsQuery); err == nil {
		for _, r := range rows {
			switch asString(r["name"]) {
			case "parts_to_throw_insert":
				if v := asInt64(r["value"]); v > 0 {
					limits.ThrowInsert = v
				}
			case "parts_to_delay_insert":
				if v := asInt64(r["value"]); v > 0 {
					limits.DelayInsert = v
				}
			}
		}
	}

	samples := make([]Sample, 0, len(byNode))
	for _, name := range order {
		s := byNode[name]
		for _, m := range append(append([]string{}, storedMetrics...), MetricDisk) {
			if _, failed := errs[m]; failed {
				s.markUnknown(m)
			}
		}
		if s.DiskUsedPct == nil {
			s.markUnknown(MetricDisk)
		}
		if limits.ThrowInsert > 0 && !s.IsUnknown(MetricParts) {
			s.PartsPressurePct = float64(s.PartsMaxActive) / float64(limits.ThrowInsert) * 100
		}
		samples = append(samples, *s)
	}

	out := Collection{Samples: samples, Limits: limits, Errors: errs}
	if cluster != "" && fanOutErr == nil {
		out.Samples = FlagMissingNodes(exec, cluster, out.Samples, capturedAt, true)
	}
	return out, fanOutErr
}

// FlagMissingNodes compares the nodes that answered against the cluster's
// definition in system.clusters and appends a sample for every node that did
// not answer, with all metrics unknown. down=true marks them Down (the
// fan-out ran and they were skipped); down=false is used after a fallback to
// the local node, where their state is simply unknown. On error the samples
// are returned unchanged.
func FlagMissingNodes(exec ExecFunc, cluster string, samples []Sample, capturedAt string, down bool) []Sample {
	if !IsValidClusterName(cluster) {
		return samples
	}
	rows, err := exec(ClusterNodesQuery(cluster))
	if err != nil {
		return samples
	}
	seen := map[string]bool{}
	for _, s := range samples {
		seen[strings.ToLower(s.Node)] = true
		seen[shortHost(s.Node)] = true
	}
	added := map[string]bool{}
	for _, r := range rows {
		name := asString(r["host_name"])
		if asInt64(r["is_local"]) == 1 {
			if self := asString(r["self"]); self != "" {
				name = self
			}
		}
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] || seen[shortHost(name)] || added[key] {
			continue
		}
		added[key] = true
		missing := Sample{Node: name, CapturedAt: capturedAt, Down: down}
		for _, m := range storedMetrics {
			missing.markUnknown(m)
		}
		missing.markUnknown(MetricDisk)
		samples = append(samples, missing)
	}
	return samples
}

// shortHost lowercases a host name and drops its domain, so "ch-1" matches
// "ch-1.prod.internal" whichever side is qualified.
func shortHost(name string) string {
	name = strings.ToLower(name)
	if i := strings.IndexByte(name, '.'); i > 0 && net.ParseIP(name) == nil {
		return name[:i]
	}
	return name
}

// ── JSON value coercion ──────────────────────────────────────────────────────
//
// ClickHouse JSON format returns 64-bit integers as quoted strings (the default
// output_format_json_quote_64bit_integers) while smaller ints arrive as numbers,
// so every numeric reader must tolerate both shapes.

func asString(v interface{}) string {
	switch t := v.(type) {
	case string:
		return t
	case nil:
		return ""
	default:
		return ""
	}
}

func asInt64(v interface{}) int64 {
	switch t := v.(type) {
	case float64:
		return int64(t)
	case int64:
		return t
	case int:
		return int64(t)
	case string:
		if n, err := strconv.ParseInt(t, 10, 64); err == nil {
			return n
		}
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return int64(f)
		}
	}
	return 0
}

func asFloat(v interface{}) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int64:
		return float64(t)
	case int:
		return float64(t)
	case string:
		if f, err := strconv.ParseFloat(t, 64); err == nil {
			return f
		}
	}
	return 0
}
