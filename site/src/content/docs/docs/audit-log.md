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
  or removed. A connection pointed at a different ClickHouse server also
  writes `connection.reset`, with the counts of sessions, role overrides, SSO
  records, background accounts, MCP keys and share links cleared (actor
  `system` when the server config changed the embedded connection). From
  v2.33.0 an agent that signs in pointing at a different server writes it
  too, naming the new and the old server. See
  [Repointing a direct connection](/docs/connections/#repointing-a-direct-connection)
  and [Repointing an agent](/docs/connections/#repointing-an-agent-v2330)
- **Connections created from the login page**: with
  [sign-in with a ClickHouse URL](/docs/configuration/#sign-in-with-a-clickhouse-url)
  on, each new connection the login page creates
  (`connection.created_from_login`, with the ClickHouse user who typed it, the
  connection name and URL, and the IP). Reusing an existing connection writes
  no row
- **Role changes**: a user's CH-UI role is changed by an admin
- **License events**: a Pro license is applied, renewed, or removed
- **MCP activity**: every query an AI client runs (`mcp.query.execute`),
  every other tool call (`mcp.tool.call`), every confirmation prompt the
  user declined (`mcp.write.declined`), and every OAuth approval
  (`mcp.oauth.consent`). Drafts and lifecycle actions an AI client makes
  are logged with the same actions as in the UI (`model.run`,
  `pipeline.started`, ...). See [MCP Server](/docs/mcp).
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
  ClickHouse did not answer. An SSO sign-in or connection switch refused
  because the person's ClickHouse user could not be provisioned writes the
  same action with the provisioning error (users, roles and connections,
  never a password)
- **Per-person SSO users**: on a connection with
  [per-person ClickHouse users](/docs/sso#per-person-clickhouse-users), the
  user created at a person's first sign-in (`sso.user_provisioned`) and
  re-synced at every later one (`sso.user_synced`), both with the roles before
  and after; the sweep or an admin locking (`sso.user_locked`) or dropping
  (`sso.user_dropped`) a user, with the person's email and last sign-in, the
  actor being `system` for the sweep; and **Remove access** in Admin → Users
  (`sso.access_removed`, with the counts of users dropped, failures,
  sessions ended and MCP keys and OAuth tokens revoked). The `ch_user` is the per-person user and the connection is
  recorded on each row
- **SSO identity settings**: an admin saving a connection's identity mode and
  group to role mapping (`connection.sso_identity.updated`, with the mode,
  prefix and number of mappings) or running **Verify** on the provisioner
  (`connection.sso_identity.verified`, with the account and how many checks
  failed)
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
- **Ingest keys**: a key created or revoked for an [ingest](/docs/ingest)
  pipeline (`ingest_key.created`, `ingest_key.revoked`), with the pipeline,
  the key's name and its first 8 characters, never the key itself
- **Incident timeline annotations** (v2.13.0+): a deployment annotation added
  or removed (`incident.annotation.created`, `incident.annotation.deleted`). See
  [Incident Timeline](/docs/incident-timeline)
- **Operations reports** (v2.13.0+): report settings saved
  (`operations.report.settings_updated`), a report generated from the page
  (`operations.report.generated`), and a report emailed from the page
  (`operations.report.email_queued`). See
  [Operations Reports](/docs/operations-reports)
- **ClickHouse access control changes**: each apply from
  [Governance → Access control](/docs/rbac) (`rbac.apply`, with the number of
  changes, the cluster and the plan hash), and one row per statement that ran
  (`rbac.statement.applied` or `rbac.statement.failed`, with the SQL truncated
  to 2 KB and the error on failure; a new user's password hash and salt
  appear as `'***'`). The actor is the person; `ch_user` is the
  ClickHouse account the statements ran as
- **Access as code and history**: an access file exported (`rbac.exported`),
  an import previewed (`rbac.import.planned`), and a revert built from
  [Access history](/docs/rbac#access-history)
  (`rbac.history.revert_prepared`), with counts only, never file content
- **Brain budgets**: a budget rule created, changed or deleted
  (`brain.budget.created`, `brain.budget.updated`, `brain.budget.deleted`).
  See [Brain → Budgets](/docs/brain#budgets)
- **First-run setup**: a wrong setup code on the login page
  (`setup.code_rejected`), the code discarded after 10 wrong attempts
  (`setup.code_burned`), a connection saved through setup
  (`setup.connection_saved`), and setup closing for good (`setup.closed`,
  with the reason). The first three use the actor `setup`; `setup.closed` uses
  the admin who signed in, or `setup` when it closed at startup. See
  [Can't login?](/docs/cant-login/#first-run-setup-from-the-login-page)

Each entry captures the **actor** (email or ClickHouse user), the **action**,
an optional **target** (e.g. the affected user or connection), structured
**metadata**, the **IP address**, the **user agent**, and a **timestamp**.

For people who sign in with [SSO](/docs/sso) (v2.13.2+), the actor is the
person's email, and the ClickHouse account the action ran as (the shared
service account, or their own per-person user) is kept in a separate `ch_user`
field. `ch_user` is only set
when it differs from the actor. It shows as the **ClickHouse user** column on
the page and is carried by `GET /api/governance/audit-logs`, the JSON and CSV
exports, and SIEM forwarding. Rows written before v2.13.2 keep the service
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
