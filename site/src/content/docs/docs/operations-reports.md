---
title: Operations Reports
description: Weekly operations reports with regressions, failures, CPU-heavy workloads, storage and resolved investigations, saved in CH-UI and optionally emailed.
---

An operations report is a saved weekly summary for one connection: query regressions, failed executions, the most CPU-heavy query patterns, active-part storage, and how many [performance investigations](/docs/performance) were marked resolved. Reports are stored in CH-UI, so they stay readable after `system.query_log` has rotated those rows away. You can generate one by hand or have CH-UI generate and email one every week.

Available on Pro and Enterprise plans.

Open it from **Operate → Reports**. It is only available to administrators.

## What a report contains

Each report compares the last seven days with the seven days before, using the same rules as [Performance](/docs/performance#how-a-regression-is-detected):

- Number of regressed query patterns, patterns compared, and patterns with insufficient observations
- Up to 20 regressions, with the metrics that crossed a threshold
- Failed query executions across the observed patterns
- The 10 patterns with the most measured CPU time
- Active-part storage from `system.parts`, and the change since the last report
- Investigations marked resolved during the week
- The coverage of the query log scan, and any warnings

Things the figures do not mean:

- Resource numbers describe logged initial queries. They are not a bill.
- Storage is the sum of `bytes_on_disk` for active parts. It includes replicas, excludes inactive and detached parts, and can double-count shared storage.
- Storage growth is only shown when the previous report was made by the same ClickHouse account, with the same coverage and the same set of nodes. The date of that earlier report is shown. Grant changes on that account can still change what it sees.
- A resolved investigation count is the number of recorded decisions, not proof of an improvement.

If `system.parts` cannot be read, the report says storage is unavailable and still includes the query findings.

### Coverage

Before scanning, CH-UI tries to find the cluster the connection belongs to. If it finds one, the report reads all replicas through `clusterAllReplicas`. If cluster discovery or the cluster-wide read fails, the whole report falls back to the connected node and says so in a warning. Check the coverage line before comparing reports from different deployments.

Queries run with `readonly=1`, a 25 second execution limit, 512 MiB of memory and 2 threads.

## Generating a report by hand

Click **Generate report**. It runs with your current ClickHouse account and saves the report. It does not send email. To send it, select the report and click **Email report**.

The list on the left shows the 52 most recent reports with their email status. Select one to read it, or click **Download** to save it as a plain text file.

## Scheduling weekly reports

Schedules start disabled. Installing or upgrading CH-UI creates the report tables but does not enable any scan or report delivery; nothing runs until an administrator saves an enabled schedule.

1. Go to **Admin → Connections**, click **Background accounts** on the connection, and configure **Weekly operations reports** with a dedicated service account. Grant it `SELECT` on `system.query_log` and `system.parts`, and on `system.clusters` if you want cluster coverage. Borrowing a signed-in user's session is not allowed for this worker. See [Background accounts](/docs/background-accounts).
2. In **Reports**, under **Weekly schedule**, tick **Enabled**, choose a **Day** and an **Hour (UTC)**. The default is Monday at 09:00 UTC. All schedules use UTC.
3. Optionally choose an **Email channel** and **Email recipients** (see below).
4. Click **Save settings**. The page shows when the next report is due.

Saving fails if the schedule is enabled and the connection has no dedicated Weekly operations reports account.

If a scheduled run fails, the error appears under the schedule and CH-UI tries again every 15 minutes until one succeeds. Scheduled generation and delivery only run while the Pro license is active.

## Email delivery

Email is optional. Reports can go out through an existing [alert channel](/docs/alerts) of type SMTP, Resend or Brevo, configured under **Governance → Alerts**. Choose the channel and enter up to 20 comma-separated recipient addresses. Channel and recipients go together: set both, or leave both empty for in-app reports only.

- A scheduled report with a channel set is queued for email automatically.
- A manually generated report is only emailed when you click **Email report**. You cannot queue a report that is already queued or sending.
- The email subject is `[CH-UI] Weekly operations report` and the body is the same plain text you see in the app. It contains database names, query hashes and aggregate metrics, not query text. Send it only to people allowed to see that.

Delivery is retried up to 5 times, waiting 5 minutes longer after each failed attempt (5, 10, 15, 20 minutes). After the fifth failure the status becomes `failed`. Each retry uses the saved report and the recipient list captured when it was queued. As with any SMTP delivery, if CH-UI stops right after a send succeeded, the same email may be sent again.

Saving the schedule settings cancels scheduled deliveries that are still queued or waiting to retry; a delivery already in progress may finish. Manually queued sends are not affected.

The email status of a report is one of `not requested`, `queued`, `sending`, `retry`, `sent`, `failed` or `canceled`.

## Limits

- Weekday 0 to 6 (Sunday is 0), hour 0 to 23, UTC.
- At most 20 recipients, plain email addresses only.
- The list shows the 52 most recent reports.
- A report can only describe what the query log still holds. The regression comparison needs 14 days of query log.

## Permissions

Everything on this page, including reading reports, is admin only. Analysts and viewers see a message that an administrator can configure reports.

These actions are recorded in the [audit log](/docs/audit-log):

| Action | When |
|---|---|
| `operations.report.settings_updated` | Schedule, channel or recipients saved |
| `operations.report.generated` | A report generated with **Generate report** |
| `operations.report.email_queued` | **Email report** clicked |

Scheduled generation and delivery attempts are not written to the audit log.

Reports are kept in the CH-UI SQLite database until the connection is deleted. They are not covered by the data retention settings.

## API

All endpoints are admin only and need a Pro license.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/operations-reports/` | The 52 most recent reports for the current connection |
| `GET` | `/api/operations-reports/settings` | Schedule settings |
| `PUT` | `/api/operations-reports/settings` | Save schedule settings |
| `GET` | `/api/operations-reports/channels` | Active email channels |
| `POST` | `/api/operations-reports/generate` | Generate a report with your credentials |
| `POST` | `/api/operations-reports/{id}/send` | Queue a saved report for email |

```json
PUT /api/operations-reports/settings
{ "enabled": true, "weekday": 1, "hour": 9, "channel_id": "ch_123", "recipients": ["data-team@example.com"] }
```

Send every field: a missing `weekday` or `hour` is saved as `0`. `send` returns `409` if the report is already queued and `400` if no channel and recipients are saved.
