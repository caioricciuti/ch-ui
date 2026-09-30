---
title: Governance
description: Metadata harvesting, access matrix, query audit, policies, incidents, alerts, and audit log
---

Governance provides ClickHouse visibility: metadata catalog, access control analysis, query audit, policy enforcement, incident management, alerting, and audit logging.

Governance is a Pro feature.

## Overview

Governance is split into sections, listed in the sidebar under Governance (each has its own URL, `/governance?section=...`):

- **Dashboard**: attention tiles, inventory and sync status
- **Tables**: metadata, comments, notes, sensitivity tags
- **Query Audit**: the queries harvested from `system.query_log`, filterable by user, with a detail sheet per query
- **Access**: one table of risky grants, plus users and the grants matrix, with details in a side sheet
- **Incidents**: the incident queue from policy violations, with comments. Incident details are Markdown, with a Write / Preview toggle
- **Policies**: guardrail definitions with warn/block enforcement. The editor picks databases, tables, columns and roles from your schema and shows a plain-words summary of what the policy does
- **Alerts**: channels, rules and events (see [Alerts](/docs/alerts))
- **Audit Log**: the privileged action audit trail
- **Settings**: the background sync switch and the query harvest mode (see [Settings](#settings) below)

## Sync Model

Governance caches ClickHouse metadata and access state in CH-UI's **local SQLite database** on your own server, via three background sync workers, so the data never leaves your infrastructure. The collector is **opt-in** and runs with the connection's governance [background account](/docs/background-accounts): a dedicated ClickHouse account, a borrowed active session (the default), or disabled.

> **Retention & limits.** Query history is capped at the newest **100,000** queries per connection and a **14-day** rolling window (whichever is smaller); metadata, users, roles, and grants reflect the latest sync. Query text can contain literal values from your SQL; CH-UI does not read your table data/rows.

| Worker | Source | Strategy | Tick |
|---|---|---|---|
| `metadata` | `system.databases`, `system.tables`, `system.columns`, `system.parts` | Full resync with soft deletes | 5 min |
| `query_log` | `system.query_log` | Watermark-based incremental (timestamp) | 5 min |
| `access` | `system.users`, `system.roles`, `system.role_grants`, `system.grants` | Full resync | 5 min |

Workers wake every 5 minutes and trigger a sync if data is older than 10 minutes (staleness threshold). You can also trigger syncs manually, full or single type.

### Credentials

Background sync uses the `governance` worker's setting under **Admin → Connections → Background accounts**. With a dedicated account, sync keeps running after everyone signs out. In the default session mode it borrows an active session on the connection (up to 3 recent sessions are tried), and each borrow is audited as `governance.credential_borrow`, at most once per connection per hour. When no credentials are available, or the worker is disabled, the tick is skipped. A manual sync always uses your own session. See [Background Accounts](/docs/background-accounts).

### Sync Status

Each worker tracks: `status` (idle/running/error), `last_synced_at`, `watermark` (query_log only), `row_count`, and `last_error`.

## Metadata Harvesting

Metadata sync runs in three phases:

### Phase 1: Databases

Queries `system.databases` (excludes `system` and `INFORMATION_SCHEMA`). Tracks name, engine, first seen, and last updated. Detects `database_added` and `database_removed` changes.

### Phase 2: Tables

Queries `system.tables` joined with `system.parts` for statistics. Tracks table name, engine, UUID, total rows, total bytes, and partition count. Detects `table_added` and `table_removed` changes.

### Phase 3: Columns

Queries `system.columns`. Tracks column name, type, position, default expression, and comment. Detects `column_added`, `column_removed`, and `column_type_changed` events.

All removals use **soft deletes** (`is_deleted` flag) to preserve history.

### Schema Changes

Seven change types are tracked with old/new values and a `detected_at` timestamp:

- `database_added`, `database_removed`
- `table_added`, `table_removed`
- `column_added`, `column_removed`, `column_type_changed`

View recent changes via **Governance > Tables** or `GET /api/governance/schema-changes`.

## Sensitivity Tags

Tag tables or columns with classification labels for catalog and compliance purposes.

| Tag | Purpose |
|---|---|
| `PII` | Personally identifiable information |
| `FINANCIAL` | Financial data |
| `INTERNAL` | Internal/confidential |
| `PUBLIC` | Public data |
| `CRITICAL` | Critical/sensitive |

Tags are stored in CH-UI's database and displayed alongside metadata in the governance UI. Each tag records who applied it and when.

## Comments & Notes

Turn CH-UI into a lightweight data catalog by annotating tables and columns.

### Comments

- **Table comments**: synced back to ClickHouse via `ALTER TABLE ... MODIFY COMMENT`
- **Column comments**: stored in CH-UI's database, displayed alongside column metadata
- Max 4000 characters per comment

### Notes

Free-form notes on tables or columns. Multiple notes per object, each with author and timestamp. Admin-only creation and deletion.

## Access Control

### Users & Roles

Harvested from `system.users` and `system.roles`. Tracks auth type (`no_password`, `plaintext_password`, `sha256_password`, `double_sha1_password`), host restrictions, and default roles.

### Grants

Harvested from `system.grants`. Tracks `access_type` (SELECT, INSERT, ALTER, CREATE, DROP, etc.), target database/table/column, partial revokes, and grant options.

### Access Matrix

After each access sync, an **access matrix** is rebuilt combining direct user grants and role-based grants into a unified view:

| Field | Description |
|---|---|
| User | ClickHouse username |
| Role | Granted role (if role-based) |
| Database | Target database |
| Table | Target table |
| Privilege | Access type (SELECT, INSERT, etc.) |
| Direct | Whether the grant is direct or via role |
| Last Query | Last time user queried this object |

### Over-Permission Detection

Identifies privileges that haven't been used within a configurable threshold.

- Default: **30 days** of inactivity
- Configurable via `?days=N` query parameter (1-3650)
- Helps identify privilege cleanup candidates

## Policy Engine

Define guardrails that check queries against access rules before or after execution.

### Policy Fields

| Field | Description |
|---|---|
| Name | Policy identifier |
| Description | What the policy enforces |
| Object Type | `database`, `table`, or `column` |
| Object Target | Database, table, and/or column name |
| Required Role | ClickHouse role needed to access the object |
| Severity | `info`, `warn`, `error`, `critical` |
| Enforcement Mode | `warn` or `block` |
| Enabled | Toggle |

### Enforcement Modes

| Mode | Behavior |
|---|---|
| `warn` | Log violation, allow query execution (post-execution detection) |
| `block` | Reject query before execution (pre-execution guardrail) |

### Granularity

- **Database-level**: protects all tables in the database
- **Table-level**: protects a specific table
- **Column-level**: protects a specific column (word-boundary regex match against query text)

### Evaluation

1. Check if the query touches a protected object (tables extracted from SQL + text heuristic)
2. Verify the user's roles from the access matrix
3. If the user lacks the required role, create a **violation**
4. If enforcement mode is `block`, reject the query with a 403

### Guardrails

The guardrail service runs pre-execution checks on all query endpoints. If access state is stale (>10 minutes), an alert event is emitted but the query is allowed.

## Incidents

Incidents track policy violations through a resolution workflow.

### Auto-Creation

When a policy violation occurs, an incident is automatically created (or deduplicated into an existing one).

**Deduplication key**: `violation:{policy_name}:{user}:{severity}` (lowercased)

If an open/triaged/in-progress incident with the same key exists:
- `occurrence_count` is incremented
- `last_seen_at` is updated
- No duplicate incident is created

### Source Types

| Source | Description |
|---|---|
| `violation` | Auto-created from policy violation |
| `over_permission` | Created from over-permission detection |
| `manual` | Manually created by admin |

### Status Workflow

```
open → triaged → in_progress → resolved (or dismissed)
```

`resolved_at` is auto-set when status changes to `resolved` or `dismissed`.

### Comments

Admins can add threaded comments to incidents. Each comment updates the incident's `updated_at` timestamp.

## Query Audit

The **Query Audit** section lists the queries the `query_log` worker has harvested into CH-UI's database. It does not query `system.query_log` live. Filter by ClickHouse user, pick how many rows to load, and open **View** on a row for the full query and its metrics. When the list is empty, check the harvest mode under [Settings](#settings): in the default `auto` mode nothing is harvested until a policy exists.

### What gets harvested

Harvesting is incremental, using the last `event_time` as a watermark. It keeps finished, initial queries (`type = 'QueryFinish'`, `is_initial_query = 1`) that took 10 ms or more, and skips CH-UI's own polls of `system.query_log`, `system.tables`, `system.columns` and `system.grants`. Each entry holds:

- Query ID, query text, user, event time
- Normalized hash (SHA-256 of the query with literals replaced, for fingerprinting similar queries)
- Query kind (Select/Insert/Create/Alter/Drop/Other)
- Metrics: duration, read rows/bytes, result rows, written rows/bytes, memory usage
- Tables used (the `tables` column of `system.query_log`, when the server has it)
- Exception code and message

Batch limit: 5000 queries per sync cycle.

### API

```bash
# Harvested entries, newest first. limit 1-5000 (default 100), offset, user, table
GET /api/governance/query-log?user=alice&limit=100

# One entry by ClickHouse query_id
GET /api/governance/query-log/{query_id}
```

`GET /api/governance/query-log` returns `{ "entries": [...], "total": N }`.

## Settings

**Governance > Settings** holds two controls.

### Background sync

The switch that turns the collector on. It is off by default, and when it is off the workers do not run and **Sync now** returns `409` with `governance_sync_disabled`. Turning it on needs an active Pro license (or one in the read-only grace period); turning it off is always allowed. The toggle is admin only and is recorded in the audit log as `governance.sync_toggle`.

```bash
GET /api/admin/governance/settings
PUT /api/admin/governance/settings
{ "sync_enabled": true }
```

The response carries `sync_enabled`, `updated_at`, `updated_by`, `banner_dismissed` and `syncer_running`. Enabling without a license returns `402`.

### Query harvest mode

Controls which queries the `query_log` worker keeps. It is one setting for the whole instance.

| Mode | Behavior |
|---|---|
| `auto` (default) | Harvest a connection's queries only while at least one policy exists for it |
| `always` | Harvest whether or not a policy exists |
| `off` | Harvest nothing. Query Audit stays empty and policies cannot detect violations |

```bash
GET /api/governance/query-harvest
PUT /api/governance/query-harvest
{ "mode": "always" }
```

Both return `{ "mode": "auto", "policy_count": 0, "harvesting": false }`, where `policy_count` is for your current connection and `harvesting` says whether the worker is harvesting it right now. Anyone with Governance access can read the mode; changing it is admin only and is audited as `governance.query_harvest_mode`.

## Alerts

Governance events (policy violations, schedule failures, slow schedules) can be delivered by email through alert rules and channels. Channels support SMTP, Resend, and Brevo; configs are encrypted at rest and each channel has a **Test** button.

Rules match events to channels by event type and minimum severity, with per-fingerprint cooldowns, retry limits, and templated subject/body. Delivery runs on an 8-second dispatch loop with exponential-backoff retries.

See the dedicated [Alerts](/docs/alerts) page for the full setup guide, all channel options (including the SMTP TLS modes), rule fields, template variables, and troubleshooting.

## Audit Log

Tracks privileged actions across governance and alerts.

### Captured Actions

- `governance.sync`: full sync triggered
- `governance.sync_toggle`: background sync turned on or off
- `governance.query_harvest_mode`: query harvest mode changed
- `governance.table.comment.updated`: table comment change
- `governance.column.comment.updated`: column comment change
- `governance.table.note.created` / `governance.column.note.created`: note added
- `governance.object.note.deleted`: note deleted
- `governance.tag.created` / `governance.tag.deleted`: tag change
- `governance.access.user.created` / `governance.access.user.deleted`: user change
- `governance.policy.created` / `governance.policy.updated` / `governance.policy.deleted`: policy change
- `governance.incident.from_violation`: incident auto-created
- `alerts.channel.created` / `alerts.channel.updated` / `alerts.channel.deleted`: channel change
- `alerts.rule.created` / `alerts.rule.updated` / `alerts.rule.deleted`: rule change

### Filtering

Filter by time range, action type, username, or free-text search. Default limit 100, max 1000.
