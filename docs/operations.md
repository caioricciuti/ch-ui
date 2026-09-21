# Production operations workflows

These features require CH-UI Pro. Open **Operate** for Performance, Fleet and
Reports; open **Build → Schema Compare** to compare environments. Installing
an upgrade creates the supporting SQLite tables without enabling background
scans or report delivery.

## Find and measure a performance regression

**Performance** compares normalized query patterns in adjacent, equal time
windows. It excludes the latest minute to allow query-log flushing. A pattern
needs at least ten successful executions in both windows before a regression
can be reported. The initial thresholds require at least 50% growth plus an
absolute increase of 100 ms for latency or 16 MiB for memory/read bytes.

Results distinguish insufficient observations from no detected regression.
Changes in inputs, traffic and node coverage can affect the comparison; a
regression is a finding to investigate, not a diagnosis. Analysis covers logged
initial queries and is bounded to 1,000 patterns. Reaching the limit is shown
explicitly. Missing or inaccessible logs produce an error.

Save an investigation to retain the baseline, assign an owner and record a
proposed change. After applying a change through your normal workflow, capture
an observed comparison. If a rewrite changes the normalized hash, specify its
replacement hash. The history retains notes and measurements. Marking an
investigation resolved records an explicit resolution; CH-UI does not infer
that your intervention caused a performance change.

Administrators can enable hourly scans after configuring the **Performance
regression monitor** account in **Admin → Connections → Background accounts**.
Scans use a dedicated service account, cover the connected node and compare
the last 24 hours against the preceding 24 hours. They never borrow a signed-in
user's session. Grant `SELECT` on `system.query_log`; scans use `readonly=1`
and bounded execution resources. The latest successful report and scan error
are retained. An inactive Pro license prevents background scans.

## Review the fleet

**Fleet** is available to administrators. It combines connection availability,
the latest complete retained Cluster Health poll, open incidents and the latest
successful background performance scan. Configure Cluster Health collection and
Performance scanning separately for each connection.

Missing, disabled and stale sources remain visible. A missing performance scan
is not counted as zero regressions; scans older than two hours are unavailable
for the fleet count. Opening another environment requires authentication to that
connection. Fleet does not reuse another person's ClickHouse credentials.

## Compare environment schemas

**Schema Compare** reads metadata from two selected connection/database pairs.
Your current connection can use your current session; another connection
requires credentials for that comparison. Those credentials are not persisted.
Both readers use `readonly=1` with query, row and response-size limits.

Differences include tables, columns, types, defaults, codecs, ordering, engines,
keys, TTLs and views. The downloadable SQL plan is entirely commented and must
be reviewed and adapted before execution. CH-UI does not execute the plan.
Engine arguments are deliberately omitted because they may contain credentials.
Table settings, grants, dictionaries and data are outside this comparison.

The result covers only metadata visible to the supplied accounts. Use accounts
with equivalent visibility. Permission/query failures fail the comparison rather
than presenting two empty schemas as identical. Review engine, key, view and
destructive changes carefully; some need a table rebuild or manual migration.

## Keep a weekly operations report

**Reports** is available to administrators. **Generate report** uses the current
ClickHouse account and saves an in-app snapshot. It compares the last seven
days with the preceding seven, reporting regressions, observed query failures,
CPU-heavy patterns, active-part storage and investigations marked resolved.

Resource figures describe logged initial queries, not billing costs. Physical
storage here means active-part bytes, includes replicas, excludes inactive and
detached parts, and can double-count shared storage. Storage growth compares the
current measurement with the last saved report from the same ClickHouse account
and coverage; the reference timestamp is shown. Changes to that account's grants
may still affect visibility. Measurements from different physical nodes are not
compared for storage growth.
Missing storage permissions are labeled without discarding query findings.

To schedule reports, configure a dedicated **Weekly operations reports** account
under **Admin → Connections → Background accounts**, then enable a weekday and
hour in Reports. All schedules use UTC. Grant `SELECT` on `system.query_log`,
`system.parts` and optionally `system.clusters`. Cluster discovery can broaden
coverage through `clusterAllReplicas` when remote-read permissions and node
availability permit it. If cluster reads fail, a labeled fallback covers the
connected node. Review the report's coverage before comparing deployments.
An inactive Pro license prevents generation and queued delivery.

Email is optional. Configure an SMTP, Resend or Brevo channel in **Governance →
Alerts**, then save that channel and recipients in Reports. Scheduled reports
queue an email automatically; manual generation only saves the report, and
**Email report** explicitly queues it. Emails include workload identifiers and
operational aggregates, without query text. Use recipients authorized to see
that operational information.

Delivery has a persisted lease and retries up to five times with increasing
delays. A retry uses the saved report and recipient snapshot. As with ordinary
SMTP delivery, an interrupted successful send can be delivered again. Disabling
the schedule cancels pending scheduled deliveries; a delivery already in progress
may finish. Manual sends remain separate. The page shows delivery status/errors
and supports downloading a plain-text copy.

## Reconstruct an incident

**Incident Timeline** gives administrators one chronological view of a selected
window of up to seven days: query completions and failures, observed latency,
completed merges and mutations, retained replication/parts pressure, governance
incidents and comments, and deployment annotations. Filter by source to narrow
the view. Coincident events are observations; the timeline does not assert a
root cause.

Live system logs use the current user's ClickHouse credentials and cover the
connected node. Query summaries use one-, five- or thirty-minute buckets based
on the selected range. Merge/mutation events require `system.part_log`. Retained
health requires Cluster Health collection; only samples above the documented
pressure thresholds appear. Every source reports availability, retention/coverage
limits and truncation. Each source is bounded to 500 events. An unavailable
source does not hide the other sources.

Add a timestamped deployment annotation to put an application release or schema
change beside operational evidence. The annotation records the signed-in human
identity and connection. Writers can add annotations through the API and remove
their own; administrators can remove any annotation on their current connection.
The combined timeline remains admin-only because retained observations may have
been collected with a privileged background account.

## Telemetry histogram accuracy

Log histogram tooltips use counts for the matching severity, independently of
the stacked drawing order. Log and trace charts retain the exact fetched time
bounds, include empty time buckets and zoom to complete bucket intervals. A
failed refresh clears the old histogram instead of displaying stale counts
against a new requested interval.
