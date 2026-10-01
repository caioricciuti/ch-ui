---
title: Background Accounts
description: Choose which ClickHouse account each unattended job uses, per connection
---

Background accounts (added in v2.12.0) decide which ClickHouse account CH-UI uses when work runs with nobody in the loop: scheduled queries, model runs, pipeline sinks, governance sync, Cluster Health sampling, telemetry monitors, the performance regression monitor and weekly operations reports.

Before v2.12.0 every one of these borrowed the credentials of whichever person happened to be signed in to the connection. That meant jobs stopped when everyone signed out, and a job ran with the grants of a random human. Background accounts let an administrator give each worker its own ClickHouse user, keep the old behavior on purpose, or turn background execution off.

Configuring accounts is part of the community edition and needs the admin role. It does not unlock any licensed feature: a Pro worker such as governance or Cluster Health still needs a Pro license to run.

## Modes

Each connection has one setting per worker. The modes are:

| Mode | In the UI | What happens |
|---|---|---|
| `service_account` | Dedicated ClickHouse account | The worker signs in with a username and password you saved. Runs keep going after everyone signs out. |
| `session` | An active user session | The worker borrows the credentials of a signed-in session on the connection (the newest of up to 3 active sessions whose password decrypts). Someone has to stay signed in. |
| `disabled` | Disabled | The worker gets no credentials, so background runs stop. It never borrows a session. |

`session` is the default for every worker, so an upgrade keeps the old behavior until an administrator changes it. Choosing `session` or `disabled` deletes any saved account for that worker.

## Workers

| Worker | UI label | Grants to give the account |
|---|---|---|
| `schedule` | Scheduled queries | Only the operations the scheduled SQL needs |
| `model` | Models | SELECT on model sources; CREATE, INSERT, ALTER and DROP as needed on model targets |
| `pipeline` | Pipeline sink | INSERT on pipeline targets; CREATE TABLE only if the sink creates its destination |
| `governance` | Governance | Access to the system tables governance reads and the metadata you want collected |
| `cluster_health` | Cluster Health | SELECT on the system tables Cluster Health uses |
| `telemetry.monitor` | Telemetry monitors | SELECT on the configured logs and traces tables |
| `performance` | Performance regression monitor | SELECT on `system.query_log` |
| `operations.report` | Weekly operations reports | SELECT on `system.query_log`, `system.parts` and `system.clusters` |

The performance regression monitor and weekly operations reports refuse `session` mode. They only run unattended with a dedicated account, and the UI does not offer the session option for them. See [Performance](/docs/performance) and [Operations Reports](/docs/operations-reports).

## Configure an account

1. Create a ClickHouse user for the worker with only the grants in the table above.
2. Go to **Admin → Connections** and click **Background accounts** on the connection.
3. Pick the worker, choose **Dedicated ClickHouse account** under **Run using**, and enter the username and password.
4. Click **Verify and save**.

Saving verifies the account by running `SELECT 1` through the connection with a 10 second timeout. The connection has to be online, otherwise the save is refused with `503 Connection must be online to verify this account`. If verification fails, the previous setting stays in place.

`SELECT 1` only proves the account can sign in. It does not prove the worker's grants, so run a representative job after saving.

The password is encrypted with `APP_SECRET_KEY` before it is stored and is never returned by the API, not even in encrypted form. Enter it again every time you save. An empty password field saves an empty password on purpose.

A new or rotated account is picked up on the next run, with no restart. Work that already holds credentials finishes with them: a model run keeps its credentials for the whole run, and pipelines fetch credentials per batch.

### API

Both routes need the admin role.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/connections/{id}/background-credentials` | List every worker's mode and username |
| `PUT` | `/api/connections/{id}/background-credentials/{worker}` | Set one worker's mode |

```json
{ "mode": "service_account", "username": "chui_schedules", "password": "..." }
```

For `session` and `disabled`, send only `{ "mode": "session" }` or `{ "mode": "disabled" }`.

## No silent fallback

- A `disabled` worker does not run in the background, and it does not fall back to a session.
- If a saved password cannot be decrypted (for example after `APP_SECRET_KEY` changed), the worker fails closed. It does not borrow someone else's session.
- In `session` mode with nobody signed in, the worker has no credentials. Scheduled runs are recorded as errors; governance, Cluster Health and telemetry monitors skip that tick and keep their last result.

## Manual runs

Clicking a run button is not background work, so most manual runs use your own session:

- **Schedules**: **Run now** uses the caller's ClickHouse account and still works when the `schedule` worker is disabled.
- **Telemetry monitors**: **Run now** uses your own credentials, also when disabled.
- **Governance**: a manual sync uses your session.
- **Weekly operations reports**: generating a report on demand uses your session.
- **Models**: manual runs use the `model` worker's setting. If the `model` worker is disabled, manual model runs are blocked too.

## Shared jobs and delegation

:::caution
CH-UI is a shared workspace. The admin and analyst roles apply to the whole instance, not to one connection. Anyone with those roles who can create or edit a schedule, model, pipeline or telemetry monitor can make it run with the configured background account's grants, including jobs on other connections. Picking a connection is not a tenant boundary. Do not give a background account broader grants than every admin and analyst should be able to use.
:::

Viewers cannot create, edit, delete or run schedules, models, pipelines or telemetry monitors.

## Audit trail

These events show up in the [Audit Log](/docs/audit-log):

| Action | When |
|---|---|
| `connection.background_account.updated` | An admin saved a setting. Records the admin (SSO subject when present), the worker, the mode and the account name. |
| `<worker>.credential_use` | A worker picked up its dedicated account. At most once per worker and connection per hour, and again right after a rotation. |
| `<worker>.credential_borrow` | A worker in `session` mode borrowed a session. Records the session id, at most once per worker and connection per hour. |

These record that credentials were acquired, not that the SQL succeeded. Check the job's own results for that.

## Revoking access

Choosing **Disabled** stops future runs but does not cancel work that already has credentials. For immediate revocation, also revoke the ClickHouse user's grants or password and kill its running queries with your usual ClickHouse procedures.

## Troubleshooting

If a worker stops, check in order: the connection is online, the worker's mode, that `APP_SECRET_KEY` has not changed, and the account's ClickHouse grants. The server's debug logs record each credential use and every lookup failure.

## Restore and rollback

Test restores on an isolated host, not on production: stop the server, restore a consistent backup made with `ch-ui backup`, start it with the original `APP_SECRET_KEY`, then check that both `session` mode jobs and dedicated-account jobs run. Saved account passwords only decrypt with the key they were saved with. Database migrations run on every start and are safe to repeat, so restarting a restored database is fine. See [Deployment](/docs/deployment#backup-and-restore) for the backup itself.

v2.11.1 and older do not read these settings. An old binary goes back to borrowing sessions for every worker, even ones you set to Disabled. To roll back, stop the server, then restore the pre-upgrade database and configuration together with the old binary. Do not rely on a v2.12.0 Disabled setting to protect an older binary.
