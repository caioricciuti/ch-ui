// Package clusterhealth provides ClickHouse cluster Operations & Database
// monitoring: live snapshots fanned out across all nodes via clusterAllReplicas,
// plus a lightweight time-series history with configurable retention.
//
// Scope is deliberately Operations + Database (replication, Keeper, merges,
// mutations, backups, long queries, parts pressure, data location) — NOT host
// infrastructure (cpu/mem/disk/net), which needs node-level agents.
package clusterhealth

import (
	"fmt"
	"regexp"
)

// validClusterName matches the characters ClickHouse permits in a cluster name.
// We validate before interpolating into clusterAllReplicas('<name>', ...) since
// the cluster name cannot be passed as a bound parameter.
var validClusterName = regexp.MustCompile(`^[a-zA-Z0-9_.\-]+$`)

// IsValidClusterName reports whether name is safe to embed in a cluster() call.
func IsValidClusterName(name string) bool {
	return name != "" && len(name) <= 128 && validClusterName.MatchString(name)
}

// source returns the table expression to read from. When cluster is non-empty
// (and valid) it fans the read out across every replica of every shard via
// clusterAllReplicas; otherwise it falls back to the locally connected node.
//
// Callers must SELECT hostName() AS node so per-node attribution survives the
// fan-out. clusterAllReplicas executes the query on each node and unions the
// results, so hostName() resolves per node.
func source(cluster, table string) string {
	if IsValidClusterName(cluster) {
		return fmt.Sprintf("clusterAllReplicas('%s', system.%s)", cluster, table)
	}
	return "system." + table
}

// ── Cluster resolution ────────────────────────────────────────────────────────

// ResolveClusterQuery returns the name of the cluster that contains the locally
// connected node (is_local = 1). When several clusters match it prefers the one
// with the most nodes, which is almost always the user's real data cluster
// rather than a single-node helper cluster.
const ResolveClusterQuery = `SELECT cluster, count() AS nodes
FROM system.clusters
WHERE cluster != '' AND cluster NOT LIKE 'all_%'
GROUP BY cluster
HAVING countIf(is_local) > 0
ORDER BY nodes DESC, cluster ASC
LIMIT 1 FORMAT JSON`

// ── Live drill-down queries (not stored; fetched on demand) ──────────────────

// ReplicationQuery returns per-replica health across all nodes. The worst
// offenders (highest delay / deepest queue) sort first.
func ReplicationQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  database,
  table,
  is_readonly,
  is_session_expired,
  future_parts,
  parts_to_check,
  queue_size,
  inserts_in_queue,
  merges_in_queue,
  absolute_delay,
  total_replicas,
  active_replicas
FROM ` + source(cluster, "replicas") + `
ORDER BY absolute_delay DESC, queue_size DESC
LIMIT 500 FORMAT JSON`
}

// ReplicationQueueQuery surfaces stalled or failing replication tasks — the ones
// that have retried, been postponed, or thrown an exception.
func ReplicationQueueQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  database,
  table,
  type,
  create_time,
  num_tries,
  num_postponed,
  postpone_reason,
  last_exception,
  is_currently_executing
FROM ` + source(cluster, "replication_queue") + `
WHERE num_tries > 1 OR num_postponed > 0 OR last_exception != ''
ORDER BY num_tries DESC, create_time ASC
LIMIT 300 FORMAT JSON`
}

// MergesQuery lists in-flight merges and mutations (is_mutation = 1) per node.
func MergesQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  database,
  table,
  round(elapsed, 1) AS elapsed,
  round(progress, 3) AS progress,
  is_mutation,
  num_parts,
  formatReadableSize(memory_usage) AS memory,
  formatReadableSize(total_size_bytes_compressed) AS size,
  result_part_name
FROM ` + source(cluster, "merges") + `
ORDER BY elapsed DESC
LIMIT 300 FORMAT JSON`
}

// MutationsQuery lists unfinished mutations, including the failure reason for
// ones that are stuck.
func MutationsQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  database,
  table,
  mutation_id,
  substring(command, 1, 300) AS command,
  create_time,
  parts_to_do,
  is_done,
  latest_fail_reason,
  latest_fail_time
FROM ` + source(cluster, "mutations") + `
WHERE is_done = 0
ORDER BY create_time ASC
LIMIT 300 FORMAT JSON`
}

// LongQueriesQuery lists currently-running queries that have been executing
// longer than thresholdSeconds, excluding CH-UI's own monitoring queries.
func LongQueriesQuery(cluster string, thresholdSeconds int) string {
	if thresholdSeconds <= 0 {
		thresholdSeconds = 30
	}
	return fmt.Sprintf(`SELECT
  hostName() AS node,
  query_id,
  user,
  round(elapsed, 1) AS elapsed,
  formatReadableSize(memory_usage) AS memory,
  read_rows,
  total_rows_approx,
  substring(query, 1, 300) AS query
FROM %s
WHERE elapsed > %d
  AND query NOT ILIKE '%%system.processes%%'
  AND query NOT ILIKE '%%clusterAllReplicas%%'
ORDER BY elapsed DESC
LIMIT 300 FORMAT JSON`, source(cluster, "processes"), thresholdSeconds)
}

// PartsPressureQuery returns the active-part count per (node, table, partition).
// Compared against parts_to_throw_insert this is the classic "too many parts"
// pressure gauge that precedes insert failures.
func PartsPressureQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  database,
  table,
  partition,
  count() AS parts,
  sum(rows) AS rows,
  formatReadableSize(sum(bytes_on_disk)) AS size
FROM ` + source(cluster, "parts") + `
WHERE active
GROUP BY node, database, table, partition
HAVING parts > 1
ORDER BY parts DESC
LIMIT 300 FORMAT JSON`
}

// PartsLimitsQuery reads the server-default MergeTree thresholds used to flag
// parts pressure. Per-table overrides exist but the server default is the right
// baseline for a cluster-wide gauge.
const PartsLimitsQuery = `SELECT name, value
FROM system.merge_tree_settings
WHERE name IN ('parts_to_throw_insert', 'parts_to_delay_insert') FORMAT JSON`

// DisksQuery reports storage per disk per node, including type so S3 vs local
// data location is visible.
func DisksQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  name,
  path,
  type,
  formatReadableSize(free_space) AS free,
  formatReadableSize(total_space) AS total,
  if(total_space = 0, 0, round((total_space - free_space) / total_space * 100, 1)) AS used_pct
FROM ` + source(cluster, "disks") + `
ORDER BY used_pct DESC FORMAT JSON`
}

// KeeperQuery reports the ZooKeeper/Keeper connection per node. system.zookeeper_connection
// exists on ClickHouse 23.x+; callers must treat an error as "unsupported / no Keeper"
// rather than a hard failure.
func KeeperQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  name,
  host,
  port,
  index,
  connected_time,
  session_uptime_elapsed_seconds,
  is_expired,
  xid
FROM ` + source(cluster, "zookeeper_connection") + `
ORDER BY node FORMAT JSON`
}

// BackupsQuery lists recent backup/restore operations. system.backups may be
// absent on very old ClickHouse; treat an error as "unsupported".
func BackupsQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  id,
  name,
  status,
  error,
  start_time,
  end_time,
  formatReadableSize(total_size) AS total_size,
  num_files
FROM ` + source(cluster, "backups") + `
ORDER BY start_time DESC
LIMIT 100 FORMAT JSON`
}

// ── Aggregate queries (stored as time-series samples by the harvester) ───────

// ReplicationAggQuery rolls replica health up to one row per node.
func ReplicationAggQuery(cluster string) string {
	return `SELECT
  hostName() AS node,
  max(absolute_delay) AS replication_max_delay,
  sum(queue_size) AS replication_queue_total,
  countIf(is_readonly) AS replicas_readonly
FROM ` + source(cluster, "replicas") + `
GROUP BY node FORMAT JSON`
}

// MergesAggQuery counts running merges (excluding mutations) per node.
func MergesAggQuery(cluster string) string {
	return `SELECT hostName() AS node, countIf(NOT is_mutation) AS merges_running
FROM ` + source(cluster, "merges") + `
GROUP BY node FORMAT JSON`
}

// MutationsAggQuery counts unfinished mutations per node.
func MutationsAggQuery(cluster string) string {
	return `SELECT hostName() AS node, countIf(is_done = 0) AS mutations_pending
FROM ` + source(cluster, "mutations") + `
GROUP BY node FORMAT JSON`
}

// PartsAggQuery returns the maximum active-part count for any single partition
// per node — the headline number for parts pressure.
func PartsAggQuery(cluster string) string {
	return `SELECT node, max(parts) AS parts_max_active FROM (
  SELECT hostName() AS node, database, table, partition, count() AS parts
  FROM ` + source(cluster, "parts") + `
  WHERE active
  GROUP BY node, database, table, partition
) GROUP BY node FORMAT JSON`
}

// LongQueriesAggQuery counts running queries over the threshold per node.
func LongQueriesAggQuery(cluster string, thresholdSeconds int) string {
	if thresholdSeconds <= 0 {
		thresholdSeconds = 30
	}
	return fmt.Sprintf(`SELECT hostName() AS node, count() AS long_queries
FROM %s
WHERE elapsed > %d AND query NOT ILIKE '%%system.processes%%' AND query NOT ILIKE '%%clusterAllReplicas%%'
GROUP BY node FORMAT JSON`, source(cluster, "processes"), thresholdSeconds)
}
