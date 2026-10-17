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
- **Access control** (admins): edit ClickHouse roles, grants, row policies, quotas and settings profiles through a reviewed change set (see [Access Control Editor](/docs/rbac))
- **Effective access** (admins): what a user can do on an object and through which roles (see [Effective access](/docs/rbac#effective-access))
- **Access history** (admins): snapshots of ClickHouse access control over time, changes made outside CH-UI flagged as drift, compare and revert (see [Access history](/docs/rbac#access-history))
- **Incidents**: the incident queue from policy violations, with comments. Incident details are Markdown, with a Write / Preview toggle
- **Policies**: guardrail definitions with warn/block enforcement. The editor picks databases, tables, columns and roles from your schema and shows a plain-words summary of what the policy does
- **Alerts**, **Alert rules** and **Alert channels**: fired events, routing rules and delivery channels (see [Alerts](/docs/alerts))
- **Audit Log**: the privileged action audit trail
- **Settings**: the background sync switch and the query harvest mode (see [Settings](#settings) below)

## Sync Model

Governance caches ClickHouse metadata and access state in CH-UI's **local SQLite database** on your own server, via three background sync workers, so the data never leaves your infrastructure. The collector is **opt-in** and runs with the connection's governance [background account](/docs/background-accounts): a dedicated ClickHouse account, a borrowed active session (the default), or disabled.

> **Retention & limits.** Query history is capped at the newest **100,000** queries per connection and a **14-day** rolling window (whichever is smaller); metadata, users, roles, and grants reflect the latest sync. Query text can contain literal values from your SQL; CH-UI does not read your table data/rows.

| Worker | Source | Strategy | Tick |
|---|---|---|---|
| `metadata` | `system.databases`, `system.tables`, `system.columns`, `system.parts` | Full resync with soft deletes | 5 min |
| `query_log` | `system.query_log` | Watermark-based incremental (`event_time_microseconds` plus `query_id`) | 5 min |
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

Creating a user (`POST /api/governance/access/users`, admin only) runs a plain `CREATE USER`, never `IF NOT EXISTS`. If the name is taken the request fails with `409` and no roles are granted, so an existing account cannot pick up roles by accident.

To change roles, grants, row policies, quotas or settings profiles, use **Governance → Access control**: see [Access Control Editor](/docs/rbac). Applying a change there triggers an access sync when background sync is on. With background sync on, each sync also stores a snapshot for [Access history](/docs/rbac#access-history).

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
- **Column-level**: protects a specific column (word-boundary match against query text; `SELECT *` and `t.*` count as touching every column of the table unless the column is in an `EXCEPT` list or governance metadata shows the table has no such column)

### Evaluation

1. Check if the query touches a protected object (tables extracted from SQL + text heuristic). Unqualified table names resolve against the session's current database, or `default` when it is not known
2. Verify the user's roles from the access matrix
3. If the user lacks the required role, create a **violation**
4. If enforcement mode is `block`, reject the query with a 403

### Guardrails

The guardrail service runs pre-execution checks on all query endpoints. If access state is stale (>10 minutes), an alert event is emitted but the query is allowed.

Guardrails are advisory. They match the query text, not a parsed plan, and they fail open when access data is stale or missing. The Policies tab says so. Use ClickHouse grants and row policies when a rule must hold.

## Incidents

Incidents, violations, policies and the Query Audit are admin only: they name other users and hold SQL harvested with privileged credentials. Non-admins see an "Admins only" notice; the Dashboard still shows them the violation and query counts.

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

The **Query Audit** section lists the queries the `query_log` worker has harvested into CH-UI's database. It does not query `system.query_log` live. Filter by user, pick how many rows to load, and open **View** on a row for the full query and its metrics. The user column shows the person behind the query (the `actor`: the person named by CH-UI's [query tag](/docs/query-insights#query-tags), or the email behind a [per-person SSO user](/docs/sso#per-person-clickhouse-users)) with the ClickHouse account it ran as underneath; the filter matches either name. Policy violations stay keyed on the ClickHouse user. When the list is empty, check the harvest mode under [Settings](#settings): in the default `auto` mode nothing is harvested until a policy exists.

### What gets harvested

Harvesting is incremental. The watermark is the `event_time_microseconds` and `query_id` of the last stored row, so rows that share a second are never skipped when a batch ends between them, and a failed harvest keeps the old watermark and retries the same window. It keeps initial queries (`is_initial_query = 1`) that finished or failed (`QueryFinish`, `ExceptionBeforeStart`, `ExceptionWhileProcessing`), so denied attempts are audited too. The only queries it leaves out are the governance syncer's own, which it tags with `log_comment = 'ch-ui:governance'` and recognises by that comment together with the harvest user, so setting the comment by hand does not hide a query. An optional minimum duration (see [Settings](#settings)) skips fast successful queries; it is 0 by default. Each entry holds:

- Query ID, query text, event time
- `ch_user`, the ClickHouse account, and `actor`, the person behind the row: the `person` in CH-UI's query tag when there is one, else the email of a per-person SSO user, else the ClickHouse account again
- Normalized hash (SHA-256 of the query with literals replaced, for fingerprinting similar queries)
- Query kind (Select/Insert/Create/Alter/Drop/Other)
- Metrics: duration, read rows/bytes, result rows, written rows/bytes, memory usage
- Tables used (the `tables` column of `system.query_log`, when the server has it)
- Exception code and message

Batch limit: 5000 queries per sync cycle.

### API

```bash
# Harvested entries, newest first. limit 1-5000 (default 100), offset, user (matches ch_user or actor), table
GET /api/governance/query-log?user=alice&limit=100

# One entry by ClickHouse query_id
GET /api/governance/query-log/{query_id}
```

`GET /api/governance/query-log` returns `{ "entries": [...], "total": N }`. Both endpoints are admin only, as are `GET /api/governance/violations` and the incident reads. The table detail returns its recent queries to admins only.

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
{ "mode": "always", "min_duration_ms": 0 }
```

Both return `{ "mode": "auto", "min_duration_ms": 0, "policy_count": 0, "harvesting": false }`, where `policy_count` is for your current connection and `harvesting` says whether the worker is harvesting it right now. `min_duration_ms` (0 to 3600000, optional on PUT) skips successful queries faster than that; failed and denied queries are always kept. Anyone with Governance access can read the settings; changing them is admin only and is audited as `governance.query_harvest_mode`.

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
- `rbac.apply`, `rbac.statement.applied`, `rbac.statement.failed`: an access control change set applied, and each statement's outcome (see [Access Control Editor](/docs/rbac#audit))
- `rbac.exported`, `rbac.import.planned`, `rbac.history.revert_prepared`: an access file exported, an import previewed, a revert built from access history
- `governance.policy.created` / `governance.policy.updated` / `governance.policy.deleted`: policy change
- `governance.incident.from_violation`: incident auto-created
- `alerts.channel.created` / `alerts.channel.updated` / `alerts.channel.deleted`: channel change
- `alerts.rule.created` / `alerts.rule.updated` / `alerts.rule.deleted`: rule change

### Filtering

Filter by time range, action type, username, or free-text search. Default limit 100, max 1000.
