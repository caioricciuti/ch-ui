---
title: Query Insights
description: "Visual analytics over system.query_log: latency percentiles, slow and memory-heavy query patterns, failures, users, and hot tables, with dashboard-wide cross-filtering"
---

Query Insights answers "what is my ClickHouse actually doing?", straight from `system.query_log`, with no agents, exporters, or storage to operate. It is the self-hosted answer to the query analytics you'd otherwise only get in a managed console.

Available with a Pro license.

Everything is computed **live by ClickHouse itself**: the query log *is* the history, so there is no harvester and nothing to retain. Open the page, pick a time range, and the aggregations run on your cluster.

## What it shows

### Headline tiles

For the selected range (1h / 6h / 24h / 7d / 30d): total queries, failures, active users, p50/p95/max latency, data read, and peak memory. A user is the person behind the queries when CH-UI's [query tag](#query-tags) is present (the SSO email, or the owner of a scheduled job), and the ClickHouse account otherwise.

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
| **Users** | Per-user load: runs, failures, p95, read bytes, peak memory. Users are people where the query tag names one and ClickHouse accounts elsewhere; a muted *via* line lists the accounts a person's queries ran as, and the user filter matches that same name |
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

### Query tags

Every query CH-UI runs on someone's behalf carries a small JSON tag in the
`log_comment` setting, so `system.query_log` can be split by person and by
workspace object even when ClickHouse sees a shared account (an SSO service
account, a background account, a public dashboard share):

```json
{"chui":"editor","person":"ana@example.com","object":"saved_query:3f1…"}
```

- `chui` is the feature: `editor`, `saved_query`, `dashboard`,
  `public_dashboard`, `brain`, `mcp`, `schedule`, `sql_alert`, `report`,
  `model`, `pipeline`, and the API area (`admin`, `telemetry`, `governance`…)
  for everything else.
- `person` is the SSO email, or the ClickHouse user for password logins; for
  background jobs it is the job's owner, for MCP the person behind the token.
- `object` points at the workspace object when there is one: `dash:<id>`,
  `saved_query:<id>`, `chat:<id>`, `schedule:<id>`, `alert:<id>`,
  `report:<id>`, `model_run:<id>`, `mcp_key:<id>`.

CH-UI's own introspection (this page, Cost Center, Performance, Cluster
Health, the governance harvester, Incident Timeline) keeps a plain
`ch-ui:<feature>` tag instead and is excluded from the numbers. A user whose
ClickHouse profile is `readonly=1` cannot change settings, so their queries
run untagged. To split a shared account by person in your own queries:

```sql
SELECT JSONExtractString(log_comment, 'person') AS person, count()
FROM system.query_log
WHERE type = 'QueryFinish' AND startsWith(log_comment, '{"chui":')
GROUP BY person
```

## Requirements

- A **Pro** license: the page and the `/api/query-insights/*` endpoints return `402` on the free edition.
- `system.query_log` enabled (it is by default in almost every deployment). If it's disabled, the page tells you and links the config to flip.
- The connection's ClickHouse user needs read access to `system.query_log`, and to remote nodes for cluster-wide numbers.

Query Insights pairs naturally with [Cluster Health](/docs/cluster-health): Insights tells you *which queries* hurt, Cluster Health tells you *what they did to the cluster*.
