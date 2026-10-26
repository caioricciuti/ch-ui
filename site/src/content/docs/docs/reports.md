---
title: Scheduled Reports
description: Send a saved query result or a dashboard summary on a cron to email, Slack, Teams or a webhook, through your existing alert channels.
---

A scheduled report runs a saved query or summarizes a dashboard on a cron and delivers the result to one or more of your [alert channels](/docs/alerts): SMTP email, and webhooks in generic JSON, Slack or Teams format. Reports are separate from [scheduled query jobs](/docs/schedules): a job only runs SQL, a report sends its result to people.

Available on Pro and Enterprise plans. Find reports under **Schedules → Report delivery**; the API lives at `/api/reports` and returns `402` without a license.

Reports belong to the connection they were created on. The list, editing, history, **Preview** and **Send now** only cover the reports of the connection you are signed in to, for admins too; switch connection to see or change the others.

## Before you start

- **A dedicated report account.** Every run, including **Send now** and **Preview**, uses the connection's `report` [background account](/docs/background-accounts). Reports never borrow a signed-in session: without a dedicated account the run fails and nothing is sent. Give the account SELECT on the tables your reported queries and dashboard panels read.
- **At least one alert channel.** An admin creates channels under **Governance → Alert channels**. Reports reuse them; there is nothing new to configure.

## Create a report

Click **New report** in the Report delivery section, **Subscribe** in the header of a dashboard to start with that dashboard, its current time range and its variable values filled in, or **Subscribe** in a saved query's menu to start with that query.

A report always runs on the connection you are signed in to, with that connection's `report` account. The form says which connection that is; there is no connection picker.

| Field | What it does |
|---|---|
| Source | A saved query result, or a dashboard summary |
| Cron and time zone | Five field cron (`0 8 * * 1-5` is weekdays at 08:00), evaluated in the IANA time zone you enter |
| Format | Saved queries only: CSV attachment, inline table, or both |
| Row cap | Rows fetched, 1000 by default, at most 50,000 |
| Inline rows | Rows shown in the message body and in webhooks, 50 by default, at most 200 |
| Attachment size | CSV size cap, 1 MiB by default, at most 5 MiB |
| Timeout | 60 seconds by default, 5 to 300 |
| Subject | Optional, with `{{name}}`, `{{date}}` and `{{rows}}`. Defaults to `[CH-UI] Report: <name>` |
| Deliver to | Up to 5 channels; email channels take up to 20 recipients each |
| Skip when empty | No rows means nothing is sent and the run is recorded as `skipped` |

A saved query with bind parameters (`{name:Type}`) needs a saved default for every parameter. Saving a report on a query that lacks one is refused, since nobody is there to fill it in at 08:00.

**Preview** renders the message with the report account and sends nothing. **Send now** runs the report at once and delivers it to every channel.

## What recipients get

**Saved query, email**: the body states the row count and any truncation, then (inline or both) a table of the first inline rows, as HTML with a plain text alternative. The CSV is attached as `<name>-<yyyymmdd-hhmm>.csv`.

**Dashboard, email**: a text summary of up to 25 query panels in grid order, for a fixed range (the last 24 hours unless you pick another) and the saved variable values, or each variable's default:

| Panel | Summary |
|---|---|
| Stat, gauge | The value, using the panel's calculation (last by default) |
| Table | The first 20 rows |
| Time series, bar | Last, minimum and maximum per series |
| Pie | The 10 largest slices with their share |
| Histogram | Count, minimum, median, mean and maximum of the column |
| Scatter | The number of points and the correlation of X and Y |
| Heatmap | The number of cells and the 5 largest |
| Text | Skipped |

The summary ends with a link to the dashboard when `app_url` is configured. There is no image of the dashboard.

**Webhooks**: generic webhooks get JSON with `event_type: "report.delivered"`, `columns`, `rows` (up to the inline row count), `row_count`, `truncated` and `link`. Slack gets a code block table capped at about 3,500 characters plus a link; Teams gets a card of monospace lines. Nothing is uploaded to Slack or Teams.

Truncation is always stated in the message: when the query has more rows than the cap, when the CSV stops early to stay under the size cap, and when the inline table shows fewer rows than the result.

CSV cells that start with `=`, `+`, `-` or `@` get a leading apostrophe so spreadsheets do not run them as formulas (plain numbers such as `-5` are left alone). Text that is not valid UTF-8 is replaced with the Unicode replacement character.

## How runs work

- Every query runs with `readonly=1`, `max_execution_time`, `max_result_rows` and `max_result_bytes` with `result_overflow_mode=break`, and the `report` [query tag](/docs/query-insights#query-tags) in `log_comment` (`{"chui":"report","person":"<creator>","object":"report:<id>"}`), so report queries are easy to find in `system.query_log`. Governance guardrails are evaluated first, as for SQL alerts; a blocked query fails the run.
- At most 3 report runs execute at once, scheduled and manual together. **Send now** answers `429` when all are busy.
- A run renders one snapshot. Each channel is sent that snapshot; a failed send is retried up to 5 times (after 1, 5, 15 and 30 minutes) from the same snapshot, never by running the query again.
- If the query fails, nothing is sent, the run is recorded as failed and a `report.failed` alert event goes through your alert rules (fingerprint `report:<id>:error`). The next attempt is the next slot.
- After 10 failed runs in a row, the report turns itself off and raises `report.disabled`. Saving it with Enabled on again resets the count.
- While no Pro license is active the runner pauses. Slots missed during the pause are skipped, not sent late.

## Delivery history

Click a report, or its history button, to see its latest 100 runs with the status per channel and the number of tries. **Download** returns the CSV a run sent. Stored snapshots are cleared 7 days after a run finishes; history rows are kept for 90 days by default, set under [Data Retention](/docs/admin#data-retention) as `report_deliveries` (v2.31.0+, `0` keeps them forever).

## Who can do what

| Action | Roles |
|---|---|
| See a report and its history, download CSV | The creator or an admin |
| Create | Admin, analyst |
| Preview, send now, edit or delete | The creator (admin or analyst) or an admin |
| Set recipient domains | Admin |

**Recipient domains**: an admin can limit report recipients to a list of email domains with the settings button on the Report delivery toolbar (setting `report_recipient_domains`). Analysts are then refused any address outside the list; admins are not limited. With no list, any address is accepted.

## API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/reports` | Reports of the session's connection, the channels they can use, the report account mode per connection |
| `POST` | `/api/reports` | Create |
| `PUT` | `/api/reports/{id}` | Replace a report |
| `DELETE` | `/api/reports/{id}` | Delete a report and its history |
| `POST` | `/api/reports/{id}/preview` | Render without sending |
| `POST` | `/api/reports/{id}/send` | Run and send now |
| `GET` | `/api/reports/{id}/deliveries` | Run history |
| `GET` | `/api/reports/deliveries/{id}/download` | The CSV of one run |
| `GET` / `PUT` | `/api/reports/settings` | Recipient domains (PUT is admin only) |

```json
{
  "name": "Daily signups",
  "source_type": "saved_query",
  "saved_query_id": "<saved query id>",
  "cron": "0 8 * * 1-5",
  "timezone": "Europe/Madrid",
  "format": "both",
  "targets": [{ "channel_id": "<channel id>", "recipients": ["growth@example.com"] }]
}
```

`connection_id` is optional. The report is saved on the session's connection; a `connection_id` for any other connection gets `400` (`Reports can only use the connection you are signed in to. Switch to the other connection first.`). A report of another connection answers `404`.

## Audit trail

The [Audit Log](/docs/audit-log) records `report.created`, `report.updated`, `report.deleted`, `report.sent_manual`, `report.delivered` (with the recipient count and rows), `report.auto_disabled` and `report.settings_updated`, plus `report.credential_use` when the runner picks up the dedicated account.
