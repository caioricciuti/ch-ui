---
title: Performance
description: Find query regressions in system.query_log, save an investigation with a baseline, and prove whether a change helped. Includes the Fleet overview.
---

Performance answers two questions: which query patterns got slower or heavier recently, and did the change you made actually fix one. It compares normalized query patterns from `system.query_log` across two equal, adjacent time windows, and lets you save an investigation that keeps the before and after measurements side by side.

Available on Pro and Enterprise plans.

Open it from **Operate → Performance**. The page has two tabs: **Regressions** and **Investigations**.

## How a regression is detected

The scan groups `system.query_log` by normalized query hash and current database. It only looks at initial queries (not the sub-queries a distributed query fans out to), and it ignores queries CH-UI itself runs. Successful queries are measured; failed ones are counted separately.

The current window ends one minute before now, so rows ClickHouse has not flushed yet do not skew the result. Deployments with a longer `query_log` flush interval can still have incomplete recent observations in the current window. The baseline is the window of the same length right before it.

A pattern is flagged as a regression when both windows have at least 10 successful executions and at least one of these holds:

| Metric | Relative increase | And an absolute increase of at least |
|---|---|---|
| p95 latency | 50% | 100 ms |
| Mean memory per query | 50% | 16 MiB |
| Mean bytes read per query | 50% | 16 MiB |

Both conditions are required, so a query going from 2 ms to 4 ms is not flagged. Mean latency and mean CPU time per query are also recorded for comparisons but never trigger a regression on their own.

Each pattern gets one of three states:

- **Regression**: enough samples in both windows and at least one threshold crossed.
- **Stable**: enough samples, nothing crossed.
- **Insufficient data**: fewer than 10 successful runs in one of the windows. This is shown as its own state, never as "no regression" and never as an improvement.

Regressions are sorted by **additional aggregate duration**: the increase in mean latency multiplied by the current execution count. It is a rough way to rank impact, not an estimate of CPU time or cost.

A flagged pattern is something to look at, not a diagnosis. Changes in input data, traffic mix, cold caches or which node served the queries can all move these numbers.

## Using the Regressions tab

1. Pick a window under **Compare each window**: 1 hour, 6 hours, 24 hours or 7 days. The default is 24 hours.
2. Read the summary: number of regressions, patterns compared, and patterns with insufficient data. The panel below it shows the exact current and baseline windows and what the scan covered.
3. Each card shows the normalized query text (literal values are stripped), the p95 latency, mean memory and mean bytes read for both windows, and the success and failure counts.
4. Turn on **All observed patterns** to see stable and insufficient patterns too, not only regressions. Use the search box to filter by text or database.

The page rescans when you open it, when you click **Refresh**, and every five minutes while the tab is visible. Interactive scans always run with your own ClickHouse credentials.

If `system.query_log` is missing or your account cannot read it, the page says **Query log is not available** instead of showing an empty result.

## Saving an investigation

An investigation keeps a baseline measurement, an owner, notes, and every later comparison, so you can show what changed after a fix.

1. On a pattern, click **Investigate**. The button is disabled if the current window has fewer than 10 successful runs.
2. Give it a **Title**, an **Owner** (a name or email), and optional initial notes, then click **Capture baseline**. The server rereads the metrics for that exact pattern with your credentials. If it finds fewer than 10 successful executions, the baseline is refused.
3. Ship your change through your normal process. Come back and add a note describing what changed and when it was deployed. Set the status to **Monitoring a change** if that helps the team.
4. When a full window has passed since the baseline, click **Capture comparison**. Until then the page shows when the comparison becomes available. If you rewrote the SQL and its normalized hash changed, pick the new pattern under **Compare against**; the choices come from the latest scan.
5. Review the before and after figures, then set the status to **Resolved** when the evidence supports it.

Things to know about comparisons:

- The comparison window must not overlap the baseline window. The server enforces this; confirm yourself that all traffic in the new window is after your deployment.
- If either side has fewer than 10 successful runs, the comparison is still saved but marked as insufficient, and no change is claimed.
- Without cluster coverage, the comparison is rejected if the connected node is different from the one the baseline was measured on. This matters behind a load balancer.
- Picking a replacement pattern is an explicit choice. Make sure both patterns do the same work and explain the rewrite in a note.
- Marking an investigation resolved records your decision. CH-UI does not decide that your change caused an improvement.

### History and sharing

Investigations belong to the connection, and everyone using that connection sees them. The investigation page shows the captured baseline, the latest comparison, and the history of the last 100 events. Baselines and comparisons are never edited after they are captured; title, owner, status and notes changes are added as update events. If someone else saved a change after you opened the page, your save is rejected so you can reload.

The actor recorded on each event is the SSO identity when you signed in with SSO, otherwise the ClickHouse user.

## Hourly background detection

Administrators can turn on hourly scans for a connection, so regressions are recorded even when nobody has the page open.

1. Go to **Admin → Connections**, click **Background accounts** on the connection, and configure **Performance regression monitor** with a dedicated service account. It needs `SELECT` on `system.query_log`. Borrowing a signed-in user's session is not allowed for this worker. See [Background accounts](/docs/background-accounts).
2. On the Performance page, in the **Hourly background detection** panel, click **Enable hourly scans**. Enabling fails until the service account is configured.

The worker checks every minute for due work and scans a connection when its last scan is at least an hour old. Each hourly scan compares the last 24 hours with the 24 hours before, on the connected node. The panel shows the last attempt, the last successful scan with its regression count, and the error from the last failed scan. A failed scan keeps the previous successful report. Scans only run while the Pro license is active.

There is no email, webhook or alert when a background scan finds a regression. Use the panel, the [Fleet overview](#fleet-overview) or the weekly [Operations reports](/docs/operations-reports) to see the results.

## Coverage and limits

- The UI and the hourly worker scan the connected node only. API clients can pass a `cluster` name to read all replicas through `clusterAllReplicas`; if that fails, the request fails instead of falling back to one node.
- Results depend entirely on what `system.query_log` still holds. Query log retention, sampling, disabled logging and missing grants all limit what can be compared. A 7-day comparison needs 14 days of query log.
- A scan evaluates the 1,000 most frequent patterns in the current window. When that limit is hit, the page says so; other patterns have not been evaluated.
- Scan queries run with `readonly=1`, a 25 second execution limit, 512 MiB of memory and 2 threads. A permission or resource error is shown as an error, not as an empty result.
- CPU time comes from the `UserTimeMicroseconds` and `SystemTimeMicroseconds` profile events. It can be zero if profile events were not collected.
- The sample query shown is normalized text for reading, not SQL you can run.
- Title is limited to 200 characters, owner to 320, and a note to 8,000.

## Permissions

| Action | Viewer | Analyst | Admin |
|---|---|---|---|
| View regressions and investigations | Yes | Yes | Yes |
| Create investigations, add updates, capture comparisons | No | Yes | Yes |
| View and change hourly background detection | No | No | Yes |

Interactive scans are also limited by what your ClickHouse account can read. Investigation changes are not written to the [audit log](/docs/audit-log).

Resolved investigations are pruned 180 days after they were resolved (v2.14.3+; change it under [Data Retention](/docs/admin#data-retention), `resolved_investigations`). Open and monitoring investigations, and the background scan result, are kept until the connection is deleted.

## Fleet overview

**Operate → Fleet** is an administrator view of every connection on one page. For each connection it shows:

- Whether the connector is online
- Replication delay, replication queue, readonly replicas, parts pressure, pending mutations and long queries from the latest retained [Cluster Health](/docs/cluster-health) poll
- Open governance incidents (anything not resolved)
- **Query regressions** from the latest background performance scan

Each connection gets a status:

| Status | Meaning |
|---|---|
| Offline | The connector is not connected. Figures shown are historical. |
| Collection disabled | Cluster Health background collection is off for this connection. |
| No samples | Collection is on but no health samples exist yet. |
| Stale samples | The latest sample is older than three poll intervals (at least 3 minutes). |
| Critical | Readonly replicas, replication delay of 60 s or more, or parts pressure of 80% or more. |
| Needs attention | Replication delay of 10 s or more, a queue of 10 or more, pending mutations, long queries, open incidents, or regressions. |
| Healthy | None of the above. |

The regression count only appears when hourly background detection is enabled, the last scan succeeded, and that scan is less than 2 hours old. Otherwise the field is left out rather than shown as zero.

Fleet reads only data CH-UI already retains; it never queries another connection with someone else's credentials. The page refreshes every 30 seconds. **Inspect health**, **Incidents** and **Performance** open a sign-in sheet for that connection, because switching to it needs your own ClickHouse credentials for it. Configure Cluster Health collection and performance scanning separately for each connection.

## API

All endpoints need a session and a Pro license.

| Method | Path | Role | Purpose |
|---|---|---|---|
| `GET` | `/api/performance/regressions?range=24h` | Any | Compare windows. `range` is `1h`, `6h`, `24h` (default) or `7d`. Optional `cluster`. |
| `GET` | `/api/performance/investigations` | Any | List investigations for the current connection |
| `GET` | `/api/performance/investigations/{id}` | Any | Investigation with its last 100 events |
| `POST` | `/api/performance/investigations` | Analyst, admin | Capture a baseline and create an investigation |
| `PUT` | `/api/performance/investigations/{id}` | Analyst, admin | Update title, owner, status, add a note |
| `POST` | `/api/performance/investigations/{id}/compare` | Analyst, admin | Capture a comparison |
| `GET` | `/api/performance/monitor` | Admin | Hourly scan state and latest report |
| `PUT` | `/api/performance/monitor` | Admin | Enable or disable hourly scans |
| `GET` | `/api/fleet` | Admin | Fleet overview |

Create an investigation:

```json
POST /api/performance/investigations
{
  "title": "Slow orders dashboard",
  "owner": "ana@example.com",
  "note": "p95 doubled after Tuesday's release",
  "hash": "12345678901234567890",
  "database": "shop",
  "range": "24h"
}
```

`hash` is the decimal normalized query hash from the regressions response. A baseline with fewer than 10 successful runs returns `409`.

Update an investigation. `status` is `open`, `monitoring` or `resolved`, and `revision` must be the value you last read; a stale revision returns `409`:

```json
PUT /api/performance/investigations/{id}
{ "title": "Slow orders dashboard", "owner": "ana@example.com", "status": "monitoring", "note": "Added a skip index, deployed 14:05 UTC", "revision": 2 }
```

Capture a comparison. The body is optional; send a `hash` and `database` to compare against a rewritten pattern. It returns `409` while the new window would still overlap the baseline, with the time it becomes available:

```json
POST /api/performance/investigations/{id}/compare
{ "hash": "98765432109876543210", "database": "shop" }
```

Enable hourly scans with `PUT /api/performance/monitor` and `{"enabled": true}`. It returns `409` if the connection has no dedicated Performance background account.
