---
title: Query Insights
description: "Visual analytics over system.query_log: latency percentiles, slow and memory-heavy query patterns, failures, users, and hot tables, with dashboard-wide cross-filtering"
---

Query Insights answers "what is my ClickHouse actually doing?", straight from `system.query_log`, with no agents, exporters, or storage to operate. It is the self-hosted answer to the query analytics you'd otherwise only get in a managed console.

Available with a Pro license.

Everything is computed **live by ClickHouse itself**: the query log *is* the history, so there is no harvester and nothing to retain. Open the page, pick a time range, and the aggregations run on your cluster.

## What it shows

### Headline tiles

For the selected range (1h / 6h / 24h / 7d / 30d): total queries, failures, active users, p50/p95/max latency, data read, and peak memory.

### Trend charts

- **Query volume**: queries and failures per time bucket, so error bursts are visible in context. Quiet periods render as real dips (buckets are zero-filled).
- **Latency**: p50 and p95 duration over time.

Both charts have time axes, a live legend that doubles as a hover tooltip, and adapt their bucket size to the selected range.

### Drill-down sections

| Section | What it answers |
| --- | --- |
| **Slow queries** | Worst query *patterns* by p95, grouped by ClickHouse's `normalized_query_hash`, so a thousand runs of the same query with different literals show as one row with run counts and percentiles |
| **Memory** | Heaviest patterns by peak memory |
| **Frequent** | Most-run patterns |
| **Errors** | Failures grouped by exception code, with the error name, count, and last message |
| **Users** | Per-user load: runs, failures, p95, read bytes, peak memory |
| **Hot tables** | Most-read tables, with per-table I/O fair-shared across multi-table queries |

Every pattern row can be **opened in a new editor tab** with one click.

## Filters & cross-filtering

Filters apply to the **whole dashboard**: tiles, charts, and every section recompute server-side:

- **Search**: free-text match on the query text
- **Kind**: Select / Insert / Create / Alter / Drop / Other
- **Duration**: only queries ≥ 100ms / 1s / 10s
- **Funnel buttons** on Users and Hot tables rows: click a user to see only their workload; click a table to see only the queries touching it

A typical investigation: *p95 spiked* → open **Users** → funnel the heavy user → open **Slow queries** → open the offending pattern in the editor. Three clicks.

## How it works

- Aggregations run via `clusterAllReplicas('<cluster>', system.query_log)` on cluster deployments, with per-node fallback when remote nodes can't be reached (flagged as degraded, never silently wrong).
- CH-UI tags its own introspection queries with a `log_comment` and excludes exactly those. Third-party tools that also read `query_log` (Grafana, scripts) still count toward your real workload numbers.
- Only *initial* queries are counted (`is_initial_query = 1`), so distributed sub-queries don't inflate the numbers.

## Requirements

- A **Pro** license: the page and the `/api/query-insights/*` endpoints return `402` on the free edition.
- `system.query_log` enabled (it is by default in almost every deployment). If it's disabled, the page tells you and links the config to flip.
- The connection's ClickHouse user needs read access to `system.query_log`, and to remote nodes for cluster-wide numbers.

Query Insights pairs naturally with [Cluster Health](/docs/cluster-health): Insights tells you *which queries* hurt, Cluster Health tells you *what they did to the cluster*.
