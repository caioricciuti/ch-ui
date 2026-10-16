// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package queryinsights provides PRO analytics over ClickHouse's
// system.query_log: latency percentiles, volume trends, slow/heavy/frequent
// query patterns, failures, users and hot tables.
//
// Unlike clusterhealth there is no harvester or local storage — query_log IS
// the history, so every section is a live aggregation pushed down to
// ClickHouse, fanned out across the cluster when one is detected.
package queryinsights

import (
	"fmt"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
)

// Filters narrow every section's aggregation. All values are sanitized here —
// strings are escaped before interpolation and the query kind is allowlisted.
type Filters struct {
	User          string // exact ClickHouse user
	QueryKind     string // allowlisted system.query_log query_kind
	Search        string // case-insensitive substring of the query text
	Table         string // only queries touching this table (has(tables, …))
	MinDurationMS int    // only queries at least this slow
	// ExcludeLogComments hides queries tagged with any of these log_comment
	// values, on top of this package's own tag (Brain hides its own reads).
	ExcludeLogComments []string
	// ExcludeLogCommentPrefixes hides queries whose log_comment starts with
	// any of these, for the JSON tags (tunnel.LogCommentPrefix).
	ExcludeLogCommentPrefixes []string
}

var allowedQueryKinds = map[string]bool{
	"Select": true, "Insert": true, "Create": true, "Alter": true,
	"Drop": true, "Show": true, "System": true, "Other": true,
}

// escapeString quotes a value for use inside a single-quoted ClickHouse
// string literal.
func escapeString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// escapeLikePattern additionally neutralizes LIKE wildcards so user input
// matches literally inside an ILIKE '%…%' pattern.
func escapeLikePattern(s string) string {
	s = escapeString(s)
	s = strings.ReplaceAll(s, `%`, `\%`)
	s = strings.ReplaceAll(s, `_`, `\_`)
	return s
}

// clauses renders the active filters as additional AND conditions.
func (f Filters) clauses() string {
	var b strings.Builder
	if f.User != "" {
		fmt.Fprintf(&b, "\n  AND user = '%s'", escapeString(f.User))
	}
	if allowedQueryKinds[f.QueryKind] {
		fmt.Fprintf(&b, "\n  AND query_kind = '%s'", f.QueryKind)
	}
	if f.Search != "" {
		fmt.Fprintf(&b, "\n  AND query ILIKE '%%%s%%'", escapeLikePattern(f.Search))
	}
	if f.Table != "" {
		fmt.Fprintf(&b, "\n  AND has(tables, '%s')", escapeString(f.Table))
	}
	if f.MinDurationMS > 0 {
		fmt.Fprintf(&b, "\n  AND query_duration_ms >= %d", f.MinDurationMS)
	}
	if len(f.ExcludeLogComments) > 0 {
		quoted := make([]string, 0, len(f.ExcludeLogComments))
		for _, c := range f.ExcludeLogComments {
			quoted = append(quoted, "'"+escapeString(c)+"'")
		}
		fmt.Fprintf(&b, "\n  AND log_comment NOT IN (%s)", strings.Join(quoted, ", "))
	}
	for _, p := range f.ExcludeLogCommentPrefixes {
		fmt.Fprintf(&b, "\n  AND NOT startsWith(log_comment, '%s')", escapeString(p))
	}
	return b.String()
}

// Range describes a validated time window and its chart bucket size.
type Range struct {
	Name          string
	Interval      string // ClickHouse INTERVAL expression body, e.g. "6 HOUR"
	BucketSeconds int    // time-series bucket for volume/latency charts
}

// ranges is the allowlist of supported windows; the name is interpolated into
// SQL so anything outside this table is rejected.
var ranges = map[string]Range{
	"1h":  {Name: "1h", Interval: "1 HOUR", BucketSeconds: 60},
	"6h":  {Name: "6h", Interval: "6 HOUR", BucketSeconds: 300},
	"24h": {Name: "24h", Interval: "24 HOUR", BucketSeconds: 900},
	"7d":  {Name: "7d", Interval: "7 DAY", BucketSeconds: 3600},
	"30d": {Name: "30d", Interval: "30 DAY", BucketSeconds: 21600},
}

// DefaultRange is used when the request omits or mangles ?range=.
var DefaultRange = ranges["24h"]

// RangeSpec resolves a range name against the allowlist.
func RangeSpec(name string) (Range, bool) {
	r, ok := ranges[name]
	return r, ok
}

// source mirrors clusterhealth's fan-out: clusterAllReplicas across a valid
// cluster, local system table otherwise.
func source(cluster string) string {
	if clusterhealth.IsValidClusterName(cluster) {
		return fmt.Sprintf("clusterAllReplicas('%s', system.query_log)", cluster)
	}
	return "system.query_log"
}

// LogComment tags every query this package issues, so the analytics can
// exclude exactly CH-UI's own introspection — and nothing else. (A textual
// 'query NOT ILIKE %query_log%' filter would also hide Grafana dashboards,
// DBA scripts and any other legitimate query_log consumer.)
const LogComment = "ch-ui:query-insights"

// baseWhere returns the filters shared by every section: the time window,
// initial (non-internal) queries only, terminal event types, exclusion of
// our own introspection queries (tagged via the log_comment setting), and
// any user-selected filters.
func baseWhere(rng Range, f Filters) string {
	return fmt.Sprintf(`event_time >= now() - INTERVAL %s
  AND event_date >= toDate(now() - INTERVAL %s)
  AND is_initial_query = 1
  AND type IN ('QueryFinish', 'ExceptionBeforeStart', 'ExceptionWhileProcessing')
  AND log_comment != '%s'%s`, rng.Interval, rng.Interval, LogComment, f.clauses())
}

// SummaryQuery powers the headline tiles: volume, failures, latency
// percentiles, read volume and peak memory over the window.
func SummaryQuery(cluster string, rng Range, f Filters) string {
	return fmt.Sprintf(`SELECT
  countIf(type = 'QueryFinish') AS total_queries,
  countIf(type != 'QueryFinish') AS failed_queries,
  uniqIf(user, type = 'QueryFinish') AS active_users,
  round(quantileIf(0.5)(query_duration_ms, type = 'QueryFinish'), 1) AS p50_ms,
  round(quantileIf(0.95)(query_duration_ms, type = 'QueryFinish'), 1) AS p95_ms,
  maxIf(query_duration_ms, type = 'QueryFinish') AS max_ms,
  sumIf(read_bytes, type = 'QueryFinish') AS read_bytes,
  maxIf(memory_usage, type = 'QueryFinish') AS peak_memory
FROM %s
WHERE %s
FORMAT JSON`, source(cluster), baseWhere(rng, f))
}

// VolumeQuery buckets query counts and failures over time for the trend chart.
func VolumeQuery(cluster string, rng Range, f Filters) string {
	return fmt.Sprintf(`SELECT
  toUnixTimestamp(toStartOfInterval(event_time, INTERVAL %d SECOND)) AS t,
  countIf(type = 'QueryFinish') AS queries,
  countIf(type != 'QueryFinish') AS failures
FROM %s
WHERE %s
GROUP BY t
ORDER BY t ASC WITH FILL STEP %d
LIMIT 2000 FORMAT JSON`, rng.BucketSeconds, source(cluster), baseWhere(rng, f), rng.BucketSeconds)
}

// LatencyQuery buckets duration percentiles over time for the trend chart.
func LatencyQuery(cluster string, rng Range, f Filters) string {
	return fmt.Sprintf(`SELECT
  toUnixTimestamp(toStartOfInterval(event_time, INTERVAL %d SECOND)) AS t,
  round(quantile(0.5)(query_duration_ms), 1) AS p50_ms,
  round(quantile(0.95)(query_duration_ms), 1) AS p95_ms,
  max(query_duration_ms) AS max_ms
FROM %s
WHERE %s
  AND type = 'QueryFinish'
GROUP BY t
ORDER BY t ASC
LIMIT 2000 FORMAT JSON`, rng.BucketSeconds, source(cluster), baseWhere(rng, f))
}

// SampleQueryCap bounds the sample_query text returned per pattern. The UI
// can open the sample in an editor tab, so it must be long enough to be
// runnable for real queries; the frontend flags samples that hit this cap.
const SampleQueryCap = 2000

// patternGroup is the shared shape for the pattern-grouped sections (slow,
// memory, frequent): one row per normalized query pattern.
func patternGroup(cluster string, rng Range, f Filters, orderBy string) string {
	return fmt.Sprintf(`SELECT
  toString(normalized_query_hash) AS hash,
  substring(any(query), 1, `+fmt.Sprint(SampleQueryCap)+`) AS sample_query,
  count() AS runs,
  round(quantile(0.5)(query_duration_ms), 1) AS p50_ms,
  round(quantile(0.95)(query_duration_ms), 1) AS p95_ms,
  max(query_duration_ms) AS max_ms,
  max(memory_usage) AS max_memory,
  sum(read_rows) AS read_rows,
  sum(read_bytes) AS read_bytes,
  max(event_time) AS last_seen
FROM %s
WHERE %s
  AND type = 'QueryFinish'
GROUP BY normalized_query_hash
ORDER BY %s
LIMIT 100 FORMAT JSON`, source(cluster), baseWhere(rng, f), orderBy)
}

// SlowQueriesQuery: worst patterns by p95 latency.
func SlowQueriesQuery(cluster string, rng Range, f Filters) string {
	return patternGroup(cluster, rng, f, "p95_ms DESC, max_ms DESC")
}

// MemoryQuery: heaviest patterns by peak memory.
func MemoryQuery(cluster string, rng Range, f Filters) string {
	return patternGroup(cluster, rng, f, "max_memory DESC")
}

// HeavyReadersQuery: patterns that read the most bytes.
func HeavyReadersQuery(cluster string, rng Range, f Filters) string {
	return patternGroup(cluster, rng, f, "read_bytes DESC")
}

// FrequentQuery: most-run patterns.
func FrequentQuery(cluster string, rng Range, f Filters) string {
	return patternGroup(cluster, rng, f, "runs DESC")
}

// ErrorsQuery groups failures by exception code.
func ErrorsQuery(cluster string, rng Range, f Filters) string {
	return fmt.Sprintf(`SELECT
  exception_code,
  errorCodeToName(exception_code) AS error_name,
  count() AS occurrences,
  substring(any(exception), 1, 300) AS sample_error,
  substring(any(query), 1, `+fmt.Sprint(SampleQueryCap)+`) AS sample_query,
  max(event_time) AS last_seen
FROM %s
WHERE %s
  AND exception_code != 0
GROUP BY exception_code
ORDER BY occurrences DESC
LIMIT 100 FORMAT JSON`, source(cluster), baseWhere(rng, f))
}

// UsersQuery aggregates load per ClickHouse user.
func UsersQuery(cluster string, rng Range, f Filters) string {
	return fmt.Sprintf(`SELECT
  user,
  countIf(type = 'QueryFinish') AS runs,
  countIf(type != 'QueryFinish') AS failures,
  round(quantileIf(0.95)(query_duration_ms, type = 'QueryFinish'), 1) AS p95_ms,
  sumIf(read_bytes, type = 'QueryFinish') AS read_bytes,
  maxIf(memory_usage, type = 'QueryFinish') AS max_memory
FROM %s
WHERE %s
GROUP BY user
ORDER BY runs DESC
LIMIT 100 FORMAT JSON`, source(cluster), baseWhere(rng, f))
}

// TablesQuery surfaces the most-read tables. Requires the query_log `tables`
// column (ClickHouse >= ~21.8); the handler soft-fails this section elsewhere.
//
// The filtering happens inside arrayFilter, NOT as a WHERE reference to the
// ARRAY JOIN alias — referencing the alias in WHERE over a distributed source
// (clusterAllReplicas) is broken on several analyzer-era ClickHouse versions
// (issues #62043, #70851). Per-query read totals are divided across the
// query's tables so a 3-table JOIN doesn't triple-count its I/O.
func TablesQuery(cluster string, rng Range, f Filters) string {
	return fmt.Sprintf(`SELECT
  t AS table,
  count() AS reads,
  toUInt64(round(sum(read_rows / greatest(length(tables), 1)))) AS read_rows,
  toUInt64(round(sum(read_bytes / greatest(length(tables), 1)))) AS read_bytes,
  max(event_time) AS last_seen
FROM %s
ARRAY JOIN arrayFilter(x -> NOT like(x, 'system.%%') AND NOT like(x, '_temporary_and_external_tables.%%'), tables) AS t
WHERE %s
  AND type = 'QueryFinish'
  AND query_kind = 'Select'
GROUP BY t
ORDER BY reads DESC
LIMIT 100 FORMAT JSON`, source(cluster), baseWhere(rng, f))
}
