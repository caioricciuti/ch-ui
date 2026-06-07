package clusterhealth

import (
	"strconv"
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

// CollectSamples runs the per-node aggregate queries and merges them into one
// Sample per node. It is resilient: a failing category (e.g. a node without
// replicated tables) contributes zero rather than aborting the whole snapshot.
// capturedAt is stamped by the caller so every node in a poll shares a timestamp.
func CollectSamples(exec ExecFunc, cluster string, longQueryThreshold int, capturedAt string) ([]Sample, PartsLimits, error) {
	byNode := map[string]*Sample{}
	node := func(name string) *Sample {
		if s, ok := byNode[name]; ok {
			return s
		}
		s := &Sample{Node: name, CapturedAt: capturedAt}
		byNode[name] = s
		return s
	}

	// Replication is the primary signal; if it fails outright we still return
	// whatever else we can gather, but surface the error so the caller can log it.
	var firstErr error
	if rows, err := exec(ReplicationAggQuery(cluster)); err != nil {
		firstErr = err
	} else {
		for _, r := range rows {
			s := node(asString(r["node"]))
			s.ReplicationMaxDelay = asFloat(r["replication_max_delay"])
			s.ReplicationQueueTotal = asInt64(r["replication_queue_total"])
			s.ReplicasReadonly = asInt64(r["replicas_readonly"])
		}
	}

	if rows, err := exec(MergesAggQuery(cluster)); err == nil {
		for _, r := range rows {
			node(asString(r["node"])).MergesRunning = asInt64(r["merges_running"])
		}
	}
	if rows, err := exec(MutationsAggQuery(cluster)); err == nil {
		for _, r := range rows {
			node(asString(r["node"])).MutationsPending = asInt64(r["mutations_pending"])
		}
	}
	if rows, err := exec(PartsAggQuery(cluster)); err == nil {
		for _, r := range rows {
			node(asString(r["node"])).PartsMaxActive = asInt64(r["parts_max_active"])
		}
	}
	if rows, err := exec(LongQueriesAggQuery(cluster, longQueryThreshold)); err == nil {
		for _, r := range rows {
			node(asString(r["node"])).LongQueries = asInt64(r["long_queries"])
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
	for _, s := range byNode {
		if limits.ThrowInsert > 0 {
			s.PartsPressurePct = float64(s.PartsMaxActive) / float64(limits.ThrowInsert) * 100
		}
		samples = append(samples, *s)
	}
	return samples, limits, firstErr
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
