---
title: Schedules
description: Cron-based scheduled query execution with auto-alerts
---

Schedules let you run saved queries on a cron schedule with timeout control, timezone support, run history, and automatic alerting on failures and slow executions.

Scheduled query jobs are a **Pro** feature (a license with the `enterprise` edition unlocks them too). Model and pipeline schedules are separate and part of the free core; see [Models](/docs/models#scheduling).

Enabled schedules keep running during the 14-day grace period after a license expires. After grace they pause, nothing is deleted, and they resume when a license is activated. See [What happens at expiry](/docs/license#what-happens-at-expiry).

Anyone signed in can list schedules and read their run history. Creating, editing, deleting and manually running a schedule needs the **admin** or **analyst** role; viewers get `403`.

## Overview

Each schedule has:

- **A saved query** (the SQL to execute)
- **A cron expression** (5-field standard format)
- **A timezone** (IANA, default UTC)
- **A timeout** (milliseconds, default 60s)

## Creating a Schedule

Navigate to **Schedules** in the sidebar and click **New Schedule**.

| Field | Description | Default |
|---|---|---|
| Name | Schedule identifier | Required |
| Saved Query | Select from existing saved queries | Required |
| Cron | 5-field cron expression | Required |
| Timezone | IANA timezone (e.g. `America/New_York`) | `UTC` |
| Timeout (ms) | Max execution time | 60000 (60s) |
| Enabled | Whether the scheduler should trigger this | `true` |

### Cron Syntax

Standard 5-field format:

| Field | Range | Special |
|---|---|---|
| Minute | 0-59 | `*`, `,`, `-`, `/` |
| Hour | 0-23 | `*`, `,`, `-`, `/` |
| Day of month | 1-31 | `*`, `,`, `-`, `/` |
| Month | 1-12 | `*`, `,`, `-`, `/` |
| Day of week | 0-6 (Sun=0) | `*`, `,`, `-`, `/` |

Examples:

```
0 9 * * *     # Daily at 9:00 AM
0 */6 * * *   # Every 6 hours
30 2 * * 0    # Sundays at 2:30 AM
0 0 1 * *     # First of month at midnight
```

`next_run_at` is computed automatically and always stored in UTC.

## Execution Model

The schedule runner wakes every **30 seconds** and checks for enabled schedules with `next_run_at <= now`.

### Concurrency

Up to **3 schedules** execute concurrently (system-wide semaphore). Additional due schedules wait for the next tick.

### Credential Resolution

Scheduled runs need ClickHouse credentials. The runner:

1. Uses the connection set on the schedule, or falls back to the saved query's connection
2. Resolves credentials from that connection's `schedule` [background account](/docs/background-accounts): a dedicated ClickHouse account, or in the default session mode a borrowed active session (up to 3 recent sessions are tried)

If the worker is disabled, the saved account cannot be decrypted, or no session is available in session mode, the run is recorded as an error. A configured account never falls back to a human session.

### Execution Flow

1. Fetch the linked saved query
2. Verify the tunnel is online for the target connection
3. Resolve credentials (see above)
4. Execute the query with the configured timeout (minimum enforced: 60s)
5. Record the result (status, elapsed time, rows affected, error)
6. Update `last_run_at`, `last_status`, `last_error`
7. Recompute `next_run_at` if the schedule is still enabled
8. Create audit log entry

### Manual Execution

Trigger a schedule immediately without waiting for the cron:

```bash
POST /api/schedules/{id}/run
```

Returns synchronously with the run result including status, elapsed time, and any error.

A manual run uses your own ClickHouse account, not the background account, so it still works when the `schedule` worker is disabled.

## Auto-Alerts

Schedules automatically create alert events for failures and slow executions. These integrate with the [Governance alerts](/docs/governance) system.

### Failure Alert

| Field | Value |
|---|---|
| Event Type | `schedule.failed` |
| Severity | `error` |
| Fingerprint | `schedule:{id}:error` |

Created immediately when a scheduled run fails.

### Slow Execution Alert

| Field | Value |
|---|---|
| Event Type | `schedule.slow` |
| Severity | `warn` |
| Fingerprint | `schedule:{id}:slow` |
| Threshold | Elapsed time >= 80% of timeout (minimum 5 seconds) |

Created when a run succeeds but takes too long relative to its timeout. Helps identify schedules that may need timeout increases or query optimization.

Both alert types use fingerprint-based deduplication to prevent duplicate alerts per schedule within the configured cooldown window.

## Run History

View execution history for each schedule.

```bash
GET /api/schedules/{id}/runs?limit=50&offset=0
```

Default limit: 50, max: 500.

### Run Fields

| Field | Description |
|---|---|
| `id` | Run identifier |
| `started_at` | When execution began |
| `finished_at` | When execution completed |
| `status` | `running`, `success`, or `error` |
| `rows_affected` | Number of rows affected |
| `elapsed_ms` | Execution time in milliseconds |
| `error` | Error message (if status is `error`) |

## API Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/schedules` | List all schedules |
| `GET` | `/api/schedules/{id}` | Get schedule details |
| `POST` | `/api/schedules` | Create schedule |
| `PUT` | `/api/schedules/{id}` | Update schedule |
| `DELETE` | `/api/schedules/{id}` | Delete schedule |
| `POST` | `/api/schedules/{id}/run` | Manual execution |
| `GET` | `/api/schedules/{id}/runs` | Run history |
