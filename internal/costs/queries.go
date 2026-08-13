// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.

// Package costs provides PRO cost attribution (showback/chargeback) over
// ClickHouse's system.query_log and system.parts: compute spend per user and
// team, top cost-driving query patterns, and storage cost per table.
//
// Like queryinsights, every section is a live aggregation pushed down to
// ClickHouse. Consumption is converted to currency with user-supplied unit
// rates: CPU core-hours (from OSCPUVirtualTimeMicroseconds) and GB-months of
// storage. Team attribution happens in the handler, not in SQL — user→team
// rules live in the local config.
package costs

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/caioricciuti/ch-ui/internal/clusterhealth"
)

// TeamRule assigns ClickHouse users to a cost center. Entries are exact user
// names, or prefixes when they end with '*' (e.g. "etl_*").
type TeamRule struct {
	Name  string   `json:"name"`
	Users []string `json:"users"`
}

// Config is the per-connection cost model, stored as JSON in SQLite.
type Config struct {
	Currency       string     `json:"currency"`
	CPUPerCoreHour float64    `json:"cpuPerCoreHour"`
	StorageGBMonth float64    `json:"storageGBMonth"`
	Teams          []TeamRule `json:"teams"`
}

// DefaultConfig approximates on-demand cloud rates (per vCPU-hour, per
// GB-month of object storage) so the page shows meaningful numbers before the
// user plugs in their real ones.
func DefaultConfig() Config {
	return Config{Currency: "USD", CPUPerCoreHour: 0.06, StorageGBMonth: 0.023}
}

// Unallocated is the bucket for users no team rule matches. Its share of
// spend is the page's allocation-coverage KPI.
const Unallocated = "Unallocated"

// MapTeam resolves a user against the team rules; first match wins.
func MapTeam(user string, teams []TeamRule) string {
	for _, t := range teams {
		for _, p := range t.Users {
			if p == "" {
				continue
			}
			if strings.HasSuffix(p, "*") {
				if strings.HasPrefix(user, strings.TrimSuffix(p, "*")) {
					return t.Name
				}
			} else if user == p {
				return t.Name
			}
		}
	}
	return Unallocated
}

// Sanitize clamps a stored or submitted config to safe values.
func (c Config) Sanitize() Config {
	c.Currency = strings.ToUpper(strings.TrimSpace(c.Currency))
	if len(c.Currency) > 8 || c.Currency == "" {
		c.Currency = "USD"
	}
	c.CPUPerCoreHour = clampRate(c.CPUPerCoreHour)
	c.StorageGBMonth = clampRate(c.StorageGBMonth)
	if len(c.Teams) > 200 {
		c.Teams = c.Teams[:200]
	}
	teams := c.Teams[:0]
	for _, t := range c.Teams {
		t.Name = strings.TrimSpace(t.Name)
		if t.Name == "" || t.Name == Unallocated {
			continue
		}
		users := t.Users[:0]
		for _, u := range t.Users {
			if u = strings.TrimSpace(u); u != "" {
				users = append(users, u)
			}
		}
		t.Users = users
		teams = append(teams, t)
	}
	c.Teams = teams
	return c
}

func clampRate(f float64) float64 {
	if math.IsNaN(f) || math.IsInf(f, 0) || f < 0 || f > 1e6 {
		return 0
	}
	return f
}

// rate renders a rate as a plain (non-exponent) SQL numeric literal.
func rate(f float64) string {
	return strconv.FormatFloat(clampRate(f), 'f', -1, 64)
}

// Range describes a validated time window and its chart bucket size. Cost
// trends bucket much coarser than latency charts — spend is read per hour or
// day, not per minute.
type Range struct {
	Name          string
	Interval      string
	BucketSeconds int
}

var ranges = map[string]Range{
	"24h": {Name: "24h", Interval: "24 HOUR", BucketSeconds: 3600},
	"7d":  {Name: "7d", Interval: "7 DAY", BucketSeconds: 21600},
	"30d": {Name: "30d", Interval: "30 DAY", BucketSeconds: 86400},
}

// DefaultRange is used when the request omits or mangles ?range=.
var DefaultRange = ranges["7d"]

// RangeSpec resolves a range name against the allowlist.
func RangeSpec(name string) (Range, bool) {
	r, ok := ranges[name]
	return r, ok
}

// LogComment tags every query this package issues so the cost numbers exclude
// exactly CH-UI's own introspection (same rationale as queryinsights).
const LogComment = "ch-ui:cost-center"

// cpuHours converts the per-query CPU counter to core-hours. A missing map
// key reads as 0 on ClickHouse, and versions without the ProfileEvents map
// column soft-fail the section in the handler.
const cpuHours = "ProfileEvents['OSCPUVirtualTimeMicroseconds'] / 3.6e9"

func source(cluster, table string) string {
	if clusterhealth.IsValidClusterName(cluster) {
		return fmt.Sprintf("clusterAllReplicas('%s', %s)", cluster, table)
	}
	return table
}

func baseWhere(rng Range) string {
	return fmt.Sprintf(`event_time >= now() - INTERVAL %s
  AND event_date >= toDate(now() - INTERVAL %s)
  AND is_initial_query = 1
  AND type IN ('QueryFinish', 'ExceptionBeforeStart', 'ExceptionWhileProcessing')
  AND log_comment != '%s'`, rng.Interval, rng.Interval, LogComment)
}

// SummaryQuery powers the headline tiles: compute spend, waste (spend on
// failed queries), core-hours, scan volume.
func SummaryQuery(cluster string, rng Range, cfg Config) string {
	return fmt.Sprintf(`SELECT
  countIf(type = 'QueryFinish') AS total_queries,
  countIf(type != 'QueryFinish') AS failed_queries,
  uniq(user) AS active_users,
  round(sum(%[1]s), 4) AS cpu_core_hours,
  round(sum(%[1]s) * %[2]s, 4) AS compute_cost,
  round(sumIf(%[1]s, type != 'QueryFinish') * %[2]s, 4) AS failed_cost,
  sum(read_bytes) AS read_bytes
FROM %[3]s
WHERE %[4]s
FORMAT JSON`, cpuHours, rate(cfg.CPUPerCoreHour), source(cluster, "system.query_log"), baseWhere(rng))
}

// TrendQuery buckets compute spend per user over time; the handler folds
// users into teams so the chart can stack by cost center.
func TrendQuery(cluster string, rng Range, cfg Config) string {
	return fmt.Sprintf(`SELECT
  toUnixTimestamp(toStartOfInterval(event_time, INTERVAL %d SECOND)) AS t,
  user,
  round(sum(%s) * %s, 6) AS cost
FROM %s
WHERE %s
GROUP BY t, user
ORDER BY t ASC
LIMIT 20000 FORMAT JSON`, rng.BucketSeconds, cpuHours, rate(cfg.CPUPerCoreHour), source(cluster, "system.query_log"), baseWhere(rng))
}

// UsersQuery aggregates spend per ClickHouse user; the handler attaches the
// team so the UI can roll the same rows up into a per-team table.
func UsersQuery(cluster string, rng Range, cfg Config) string {
	return fmt.Sprintf(`SELECT
  user,
  countIf(type = 'QueryFinish') AS queries,
  countIf(type != 'QueryFinish') AS failures,
  round(sum(%[1]s), 4) AS cpu_core_hours,
  round(sum(%[1]s) * %[2]s, 4) AS compute_cost,
  round(sumIf(%[1]s, type != 'QueryFinish') * %[2]s, 4) AS failed_cost,
  sum(read_bytes) AS read_bytes
FROM %[3]s
WHERE %[4]s
GROUP BY user
ORDER BY compute_cost DESC
LIMIT 200 FORMAT JSON`, cpuHours, rate(cfg.CPUPerCoreHour), source(cluster, "system.query_log"), baseWhere(rng))
}

// SampleQueryCap mirrors queryinsights: long enough for the sample to be
// runnable when opened in an editor tab.
const SampleQueryCap = 2000

// QueriesQuery ranks normalized query patterns by total spend — deliberately
// not by latency, so the cheap query that runs fifty thousand times surfaces
// here even though Query Insights never shows it.
func QueriesQuery(cluster string, rng Range, cfg Config) string {
	return fmt.Sprintf(`SELECT
  toString(normalized_query_hash) AS hash,
  substring(any(query), 1, %[1]d) AS sample_query,
  count() AS runs,
  uniq(user) AS users,
  round(sum(%[2]s), 4) AS cpu_core_hours,
  round(sum(%[2]s) * %[3]s, 4) AS compute_cost,
  round(sum(%[2]s) * %[3]s / count(), 6) AS cost_per_run,
  sum(read_bytes) AS read_bytes,
  max(event_time) AS last_seen
FROM %[4]s
WHERE %[5]s
  AND type = 'QueryFinish'
GROUP BY normalized_query_hash
ORDER BY compute_cost DESC
LIMIT 100 FORMAT JSON`, SampleQueryCap, cpuHours, rate(cfg.CPUPerCoreHour), source(cluster, "system.query_log"), baseWhere(rng))
}

// StorageQuery prices the on-disk footprint per table as a monthly run rate.
// Fanning out over clusterAllReplicas intentionally sums every replica's
// copy — each replica occupies real disk, so that IS the footprint you pay
// for. Range does not apply: parts are a snapshot, not a log.
func StorageQuery(cluster string, cfg Config) string {
	// monthly_cost must reference the alias, not sum(bytes_on_disk) again:
	// the alias shadows the column, so a second sum() would nest aggregates
	// and fail with ILLEGAL_AGGREGATION.
	return fmt.Sprintf(`SELECT
  database,
  table,
  sum(bytes_on_disk) AS bytes_on_disk,
  sum(data_uncompressed_bytes) AS uncompressed_bytes,
  sum(rows) AS total_rows,
  round(bytes_on_disk / 1e9 * %s, 4) AS monthly_cost
FROM %s
WHERE active
GROUP BY database, table
ORDER BY monthly_cost DESC
LIMIT 500 FORMAT JSON`, rate(cfg.StorageGBMonth), source(cluster, "system.parts"))
}
