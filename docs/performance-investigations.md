# Performance investigations

Performance is a Pro workspace for detecting query regressions and keeping evidence of an optimization. Open **Performance**, select a 1-hour, 6-hour, 24-hour, or 7-day window, and review changes against the immediately preceding window of the same length.

The scan groups `system.query_log` by normalized query hash **and current database**. It compares successful initial queries and records failed query counts separately. A pattern is flagged when both windows contain at least 10 successful executions and one of these conditions holds:

| Metric | Relative increase | Minimum absolute increase |
| --- | --- | --- |
| p95 latency | 50% | 100 ms |
| Mean memory per query | 50% | 16 MiB |
| Mean bytes read per query | 50% | 16 MiB |

These thresholds identify changes worth investigating; they do not establish a cause or statistical significance. Per-query CPU and mean latency are also retained for comparisons. Additional aggregate duration uses the increase in mean latency multiplied by current execution count; it is not an estimate of CPU time or financial cost.

## Capture and verify a change

1. Select **Investigate** on a regression, or enable **All observed patterns** to choose another query. The current window must contain at least 10 successful executions.
2. Give the investigation a title, assign an owner, add context, and capture the baseline **before** making a change. The server rereads the metrics using your current ClickHouse account.
3. Make your change and save a note with its deployment time. Mark the investigation **Monitoring a change** if useful.
4. After a full window has elapsed since deploying the change, select **Capture comparison**. Choose a replacement query pattern if the SQL rewrite changed its normalized hash. The new window must not overlap the baseline. The server enforces baseline non-overlap; use the displayed window timestamps to confirm all comparison traffic is after your deployment.
5. Review latency, memory, read volume, CPU, execution counts, and failures. Mark the investigation resolved when your evidence supports that decision.

Saved measurements and notes are shared with users of the same connection. Viewers can read them; analysts and administrators can create investigations and add updates. The actor is the human SSO identity when available. Baselines and comparison events are immutable; updates are recorded in history, and stale edits are rejected. The UI shows the most recent 100 events.

The normalized sample query excludes literal values. It is explanatory text, not executable SQL. Comparing a replacement pattern is an explicit choice: confirm that both patterns represent equivalent work and explain the rewrite in a note.

## Automatic scans

Interactive scans run on opening the page, on refresh, and every five minutes while the page is visible. They always use the signed-in user's ClickHouse credentials.

Administrators can also enable **Hourly background detection**:

1. Configure the **Performance** background account for this connection under **Connections → Background accounts**. It must be a dedicated service account with access to `system.query_log`.
2. Enable hourly scans on the Performance page.

The worker checks every minute for due work and compares the latest 24 hours to the preceding 24 hours. It requires an active Pro license at execution time and never borrows a human session. Rotating or disabling the configured account takes effect on the next run. A failed scan preserves the last successful report and exposes its age and error. Only administrators can read these service-account snapshots; live interactive analysis remains governed by the caller's ClickHouse permissions. There is no immediate email or webhook alert from this worker.

## Coverage and limits

- The UI and hourly worker scan the connected node. API clients can explicitly request cluster coverage; failures do not silently fall back to narrower coverage. A saved local comparison rejects a different physical node, which matters for load-balanced endpoints.
- Only retained, logged initial queries are visible. Query-log sampling, disabled logging, insufficient permissions, retention gaps, different workloads, and cache state can affect the result. Missing samples are shown as insufficient data, never as proven improvement.
- The newest minute is excluded to allow log flushing. Deployments with longer flush intervals can still have incomplete recent observations.
- A scan returns the 1,000 most frequent current patterns, with an explicit truncation notice. Each result includes both windows, so a result limit cannot split the baseline/current pair.
- Queries are read-only, limited to 25 seconds of server execution, two threads, and 512 MiB of query memory. A permission or resource-limit failure is an error, not an empty successful scan.
- CPU time comes from logged user/system CPU profile events. It can be zero if profile events were not collected.

The data source and normalization follow ClickHouse's [query-log reference](https://clickhouse.com/docs/operations/system-tables/query_log) and [query normalization functions](https://clickhouse.com/docs/sql-reference/functions/other-functions#normalizeQuery).
