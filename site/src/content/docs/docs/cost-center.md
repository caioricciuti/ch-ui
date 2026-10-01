---
title: Cost Center
description: "Showback and chargeback for ClickHouse: compute and storage spend per team, user, query pattern, and table, priced from system.query_log and system.parts with your own unit rates"
---

The Cost Center answers "who spends what on this cluster?" with compute spend priced from real CPU time in `system.query_log`, storage priced from on-disk bytes in `system.parts`, attributed to teams via user→team rules and exportable as a CSV showback report.

Available with a Pro license. New in v2.7.0.

Like [Query Insights](/docs/query-insights), everything is a live aggregation pushed down to ClickHouse: no agents, no exporters, nothing to retain. The only local state is your cost model (rates, currency, team rules), stored per connection in CH-UI's SQLite database.

## What it shows

### Headline tiles

For the selected range (24h / 7d / 30d): compute cost, **failed-query cost** (spend wasted on queries that errored), storage cost per month, **allocation coverage** (share of compute spend mapped to a real team), query count with active users, and data scanned.

### Spend trend

Compute spend over time, **stacked by team** (top 5 teams plus an *Other* bucket), so you can see which cost center drove a spike. Buckets adapt to the range: hourly for 24h, 6-hourly for 7d, daily for 30d.

### Sections

Besides the Overview, the page has four sections, listed in the sidebar under Cost Center. Each one is a single full-height table.

| Section | What it answers |
| --- | --- |
| **Teams** | Spend per cost center: compute cost, share of total, failed-query cost, core-hours, scan volume, query and user counts |
| **Users** | The same per ClickHouse user, with the team each user resolved to |
| **Cost drivers** | Query *patterns* ranked by **total spend** (grouped by `normalized_query_hash`), deliberately not by latency, so the cheap query that runs fifty thousand times surfaces here even though Query Insights never shows it. Includes cost per run, and every row opens in a new editor tab |
| **Storage** | Per-table monthly run rate from on-disk bytes, with uncompressed size, **compression ratio**, and row count |

## Requirements

- A **Pro** license: the page and the `/api/costs/*` endpoints return `402` on the free edition.
- `system.query_log` enabled (it is by default in almost every deployment). Without it the page soft-fails with instructions instead of erroring.
- The connection's ClickHouse user needs read access to `system.query_log` and `system.parts`, and to remote nodes for cluster-wide numbers.

## Opening it

Open **Cost Center** from the sidebar or the command palette (it also matches "finops", "chargeback", "showback"). The page lives at `/cost-center`. Pick a range (24h / 7d / 30d, default 7d) and every tile, chart, and section recomputes.

## Configuring rates and currency

Open the settings drawer (gear icon). The cost model has three knobs:

| Setting | Default | Purpose |
| --- | --- | --- |
| Currency | `USD` | Display currency for every cost figure and the CSV export |
| CPU / core-hour | `0.06` | What one CPU core costs you per hour |
| Storage / GB-month | `0.023` | What one GB of disk costs you per month |

The defaults approximate on-demand cloud rates (per vCPU-hour, per GB-month of object storage) so the page shows meaningful numbers out of the box; a banner reminds you they are defaults until you save your own. To derive your real CPU rate: instance price ÷ vCPUs, or amortized hardware cost per core-hour.

The model is saved per connection via `GET`/`PUT /api/costs/config`; submitted values are sanitized (rates clamped to sane bounds, currency upper-cased).

## Mapping users to teams

Team rules map ClickHouse users to cost centers:

- Each team lists user entries: **exact names** (`airflow`) or **prefixes ending with `*`** (`etl_*`).
- **First match wins**, top to bottom.
- Users no rule matches land in the **Unallocated** bucket (the name is reserved, so you can't create a team called that).
- Up to 200 teams per connection.

The **Allocated** tile is the classic FinOps allocation-coverage KPI: the share of compute spend attributed to a real team. It turns amber below 80%. The fix is always adding or widening rules until Unallocated shrinks.

Team attribution happens in CH-UI, not in SQL: the per-user rows come back from ClickHouse and are folded into teams by the rules, so changing rules re-buckets history instantly with no re-scan.

## Showback export

The **Export CSV** button downloads the active section as a showback report. Columns match the table, plus a header comment recording the section, range, currency, and export timestamp. Hand it to finance as-is, or feed it into your chargeback pipeline.

## How costs are computed

These are **estimates**, not your cloud bill. They price real measured consumption with the unit rates you provide. They are good for attribution, trends, and "who should care", not for accounting.

**Compute** is priced per query, from the query log:

```
core_hours   = OSCPUVirtualTimeMicroseconds / 3.6e9
compute_cost = core_hours × cpuPerCoreHour
```

CPU time is the fairest single proxy for compute: it captures actual work done, unlike wall-clock duration (which counts waiting) or read bytes (which ignores CPU-bound queries).

**Storage** is priced per table, from active parts:

```
monthly_cost = bytes_on_disk / 1e9 × storageGBMonth
```

Storage is a snapshot of what is on disk right now, priced as a monthly run rate; the range selector does not apply to it. On clusters, every replica's copy is summed intentionally: each replica occupies real disk, so that *is* the footprint you pay for.

Fine print, all deliberate:

- Only **initial** queries are counted (`is_initial_query = 1`), so distributed sub-queries don't double-bill.
- **Failed queries are included**: a query that burned 20 core-minutes and then threw an exception still cost you 20 core-minutes. Their share is broken out as the failed-query cost tile: pure waste, and usually the cheapest thing on the page to fix.
- CH-UI tags its own Cost Center queries with the log comment `ch-ui:cost-center` and excludes exactly those, so the page never bills itself.
- On cluster deployments, aggregations fan out via `clusterAllReplicas('<cluster>', …)`. The cluster is auto-detected; you can override it with `?cluster=`. If remote nodes can't be reached (for example, credentials aren't valid cluster-wide), the page **degrades to the local node** and shows a Degraded badge rather than failing, except on timeouts, where it won't pile a second heavy scan onto a loaded cluster.

The API mirrors the page: `GET /api/costs/{section}?range=&cluster=` with sections `summary`, `trend`, `users`, `queries`, and `storage`.

## Troubleshooting

| Symptom | Cause & fix |
| --- | --- |
| "system.query_log is not available" | The query log is disabled on your server. Enable `<query_log>` in the ClickHouse config, run a few queries, refresh. Compute sections soft-fail until then; storage still works |
| Allocated below 100% | Some users match no team rule. Open **Users**, look for rows with the italic *Unallocated* team, add rules (prefix wildcards like `etl_*` cover fleets of service users quickly) |
| Degraded badge | Remote nodes couldn't be reached; numbers cover the local node only. Usually the connection's credentials aren't valid cluster-wide |
| Numbers look too low/high | Check your rates: the defaults are generic cloud approximations. Also remember compute prices CPU time only, not memory, network, or licensing |
| Storage doesn't change with the range | Expected. Parts are a snapshot, not a log. The range applies to compute sections only |

## FAQ

**Is this my actual bill?** No. It's measured consumption × your unit rates, built for showback (who drove the spend) and chargeback (splitting it fairly), not invoice reconciliation.

**Why is a fast query my top cost driver?** Cost drivers rank by *total* spend: runs × cost per run. A 50 ms query executed a million times outspends a slow one executed twice. That inversion is exactly what this section exists to surface.

**Does CH-UI's own polling inflate the numbers?** No. Its queries are tagged with `log_comment = 'ch-ui:cost-center'` and excluded. Third-party tools reading `query_log` (Grafana, scripts) still count, because they are real workload.

**Can I price memory or network too?** Not today. CPU core-hours and GB-months cover the two dominant, cleanly measurable cost axes; more dimensions mean more pseudo-precision, not more truth.

The Cost Center pairs naturally with [Query Insights](/docs/query-insights): Insights tells you *which queries are slow*, the Cost Center tells you *which queries are expensive*. They are rarely the same list.
