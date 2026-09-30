---
title: Audit Log
description: "The CH-UI audit log: who did what, when, across sign-ins, connections, roles, and license events."
---

The **audit log** records administrative and user actions in CH-UI, so admins
can answer "who changed this, and when?", a baseline requirement for security
reviews and SOC 2. It's a **Pro** feature, available to admins under
**Governance → Audit Log** (and the Admin panel).

:::note
This is the application audit log (administration and user activity). It is
distinct from the [Governance](/docs/governance) query audit, which records
ClickHouse query activity and metadata changes inside a connection.
:::

## What's recorded

- **Sign-ins**: successful and **failed** logins, with password or
  [SSO](/docs/sso)
- **Connection changes**: a connection (tunnel or direct) is added, edited,
  or removed
- **Role changes**: a user's CH-UI role is changed by an admin
- **License events**: a Pro license is applied, renewed, or removed
- **MCP activity**: every query an AI client runs (`mcp.query.execute`),
  every other tool call (`mcp.tool.call`), and every OAuth approval
  (`mcp.oauth.consent`). See [MCP Server](/docs/mcp).
- **Dashboards**: created, updated, deleted, shared, moved
  (`dashboard.moved`), and folder changes (`dashboard.folder.created`,
  `dashboard.folder.updated`, `dashboard.folder.deleted`)
- **Saved queries**: created, updated, duplicated, deleted, run through the API
- **Telemetry**: sources and saved searches created, updated or deleted
  (`telemetry.source.*`, `telemetry.search.*`), monitor changes, and each
  monitor firing or recovering (`telemetry.monitor.fired`,
  `telemetry.monitor.recovered`)
- **Failed sign-in reasons**: a failed password login (`user.login_failed`)
  says why in its details: `invalid credentials` when ClickHouse rejected the
  login (or the error was not recognised), `clickhouse unreachable` when
  ClickHouse did not answer
- **Background accounts** (v2.12.0+): an admin changing a worker's
  [background account](/docs/background-accounts)
  (`connection.background_account.updated`, with the worker, mode and account
  name; the SSO subject is the actor when present)
- **Background account use** (v2.12.0+): each time a worker acquires its
  dedicated account, at most once per worker and connection per hour, and again
  right after the account is rotated: `<worker>.credential_use`. Workers are
  `schedule`, `model`, `pipeline`, `governance`, `cluster_health`,
  `telemetry.monitor`, `performance` and `operations.report`. The row records
  that credentials were handed out, not that the job's SQL succeeded
- **Background credential borrows** (v2.11.1+): in session mode, each time a
  background worker runs with the ClickHouse credentials of an active session,
  with the session id in the details. One row per worker and connection per hour
  at most: `schedule.credential_borrow`, `model.credential_borrow`,
  `pipeline.credential_borrow`, `governance.credential_borrow`,
  `cluster_health.credential_borrow`, `telemetry.monitor.credential_borrow`
- **Incident timeline annotations** (v2.13.0+): a deployment annotation added
  or removed (`incident.annotation.created`, `incident.annotation.deleted`). See
  [Incident Timeline](/docs/incident-timeline)
- **Operations reports** (v2.13.0+): report settings saved
  (`operations.report.settings_updated`), a report generated from the page
  (`operations.report.generated`), and a report emailed from the page
  (`operations.report.email_queued`). See
  [Operations Reports](/docs/operations-reports)

Each entry captures the **actor** (email or ClickHouse user), the **action**,
an optional **target** (e.g. the affected user or connection), structured
**metadata**, the **IP address**, the **user agent**, and a **timestamp**.

For people who sign in with [SSO](/docs/sso) (v2.13.2+), the actor is the
person's email, and the ClickHouse account the action ran as (the shared
service account) is kept in a separate `ch_user` field. `ch_user` is only set
when it differs from the actor. It is returned by `GET
/api/governance/audit-logs` and the JSON export; the CSV export and SIEM
forwarding carry the actor only. Rows written before v2.13.2 keep the service
account as the actor.

## Using it

The page lists events newest-first with:

- **Search** across actor, action, and target
- **Action** filter (sign-ins, connection changes, role changes, license events)
- **Time range** (24h / 7 days / 30 days / all time)
- **Load more** paging

## Properties

- **Tamper-resistant by design:** there is no UI to edit or delete individual
  entries.
- **Actor preserved:** the actor's identity is stored on the event, so the
  trail stays meaningful even if the user is later removed.
- **Local by default:** events live in CH-UI's local SQLite database on your
  own server. Nothing leaves your infrastructure unless you forward it.

## Forwarding to a SIEM

Audit events can be streamed out for retention and correlation:

| Setting | Behavior |
|---|---|
| `audit_webhook_url` | POST each event as JSON to your collector |
| `audit_log_file` | Append events as JSON lines to a file |
| `audit_forward_stdout` | Emit events on stdout for log shippers |

See [Monitoring & SIEM](/docs/monitoring) for details.
