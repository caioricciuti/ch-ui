---
title: SQL Workspace
description: Multi-tab SQL editor with streaming results, result filtering & sorting, automatic query history, error navigation, data upload, and saved queries
---

CH-UI provides a ClickHouse-first SQL workspace with a multi-tab editor, schema explorer, autocomplete, query streaming, result filtering and sorting, automatic query history, error navigation, data upload, and saved queries.

## Overview

The rail on the left groups the product into six areas:

| Group | Pages |
|---|---|
| **Query** | The SQL workspace: editor tabs and the schema explorer |
| **Explore** | Saved Queries, Brain |
| **Visualize** | Dashboards, Telemetry |
| **Build** | Models, Pipelines, Schedules |
| **Operate** | Cluster Health, Query Insights, Cost Center, Governance |
| **Settings** | Admin, License |

Picking a group opens its list in the context panel next to the rail. Pages are full screens with their own URL, and pages with several parts (Admin, Governance, Telemetry, the Operate pages) list them as sections in that panel instead of showing tab strips. Tabs are kept for the query workspace, where holding two things open side by side is the point: it has two split groups (left/right), and each tab is a query editor or a table/database browser.

The explorer panel can be resized by dragging and collapsed with **⌘B** / **Ctrl+B**.

## Multi-Tab Editor

The SQL editor uses **CodeMirror 6** with ClickHouse syntax highlighting and schema-aware autocomplete.

### Tab Types

| Type | Behavior |
|---|---|
| Query | Multi-instance, supports dirty tracking, SQL persisted in localStorage |
| Table/Database | Context-specific browsing with data preview |

### Persistence

Tabs are saved to **localStorage** (`ch-ui-tabs`) with auto-save via microtask debounce. Tab state survives browser refreshes and includes:

- Tab ID, type, and name
- SQL content and dirty flag
- Saved query reference (if linked)
- Split group assignment (left/right)

### Split View

Open up to two editor groups side by side. Tabs can be moved between groups. Duplicate detection prevents opening the same saved query twice.

## Query Execution

### Standard Execution

```bash
POST /api/query/run
```

```json
{
  "query": "SELECT * FROM system.tables LIMIT 20",
  "timeout": 30,
  "maxResultRows": 1000
}
```

| Parameter | Description | Default | Max |
|---|---|---|---|
| `query` | SQL to execute | Required | None |
| `timeout` | Seconds before timeout | 30 | 300 (5 min) |
| `maxResultRows` | Server-side row limit sent to ClickHouse | 1000 | None |

### Query Streaming

For large result sets, use SSE-based streaming:

```bash
POST /api/query/stream
```

Returns chunked NDJSON with message types:

| Type | Content |
|---|---|
| `meta` | Column names and types |
| `chunk` | Data rows (with sequence number) |
| `progress` | Live progress: elapsed time, rows and bytes read, percent complete |
| `done` | Statistics (rows and bytes read) and total row count |
| `error` | Error message |

Stream limit: 1,000,000 rows.

### Live progress and cancel

While a query runs, the result panel shows elapsed time, percent complete, rows and bytes read, and read throughput, the same numbers ClickHouse's own `/play` reports. Progress is sampled from `system.processes` every 300 ms. A user without `SELECT` on `system.processes` simply gets no live readout, never an error. The readout stays after the query ends, so a query too short to report progress still shows what it read.

Statements that work while the connection is open report progress too (`INSERT ... SELECT`, `OPTIMIZE TABLE ... FINAL`); mutations run in the background, so their readout covers only the statement itself.

Cancelling a query, or closing its tab, stops it on ClickHouse: every streamed query carries a `query_id`, and an abandoned stream is killed with `KILL QUERY` instead of running to completion unattended.

### EXPLAIN & Profiling

| Endpoint | Purpose |
|---|---|
| `POST /api/query/explain` | Raw EXPLAIN output |
| `POST /api/query/plan` | Parsed query plan as tree (tries EXPLAIN PLAN, falls back to EXPLAIN AST, then generic) |
| `POST /api/query/profile` | Latest `system.query_log` metrics for the exact query (duration, read rows/bytes, memory) |
| `POST /api/query/sample` | First N rows per shard (default 25, max 500), falls back to global LIMIT |

## Working with Results

### Sorting & Filtering

Click any column header to sort, or use the per-column filter popovers (type-aware operators: contains, equals, comparisons, NULL checks). Active filters show as removable chips above the grid.

CH-UI picks the cheapest *correct* strategy automatically:

- **Complete result in memory** (under the row limit and ≤ 50k rows): sorting and filtering happen instantly client-side.
- **Truncated or huge results**: the original query is re-run on the server wrapped as `SELECT * FROM (your query) WHERE … ORDER BY …`, BigQuery-style, so you get the *true* top-N over the full dataset instead of a misleading sort of the loaded page. The footer marks these views as **server-filtered**.

Exports always reflect the filtered/sorted view you're looking at. The feature can be toggled off in the result footer.

### Query History

Every query you run from the editor is recorded automatically: full SQL, success/error/cancelled status, duration, and row count. Open it with the **History** button in the editor toolbar:

- Search and filter (all / success / errors)
- Open any entry in a new tab, copy it, or replace the current editor contents
- Per user, per connection: the last 500 runs are kept; clear it anytime. For [SSO](/docs/sso) people this is per person (by email), not per shared service account, and includes queries their MCP clients ran

Credential-bearing statements (`IDENTIFIED BY`, `s3(...)` keys) are **redacted before storage**, and history lives in CH-UI's local SQLite, so it never leaves your server.

```bash
GET    /api/query-history?search=&status=&limit=50&offset=0
DELETE /api/query-history/{id}
DELETE /api/query-history        # clear all (current user + connection)
```

### Error Navigation

Failed queries show a structured error card instead of a raw dump: the ClickHouse error name and code (`SYNTAX_ERROR · 62`), a cleaned message, an actionable hint for common codes (unknown table/column, memory limit, timeouts…), and the full raw error in a collapsible.

When ClickHouse reports a position, a **"Go to line N, col N"** button jumps the editor cursor straight to the failing token and selects it. It works with both pre- and post-25.2 ClickHouse error formats.

## Schema Explorer

The left sidebar provides a navigable schema tree.

### Explorer Endpoints

| Endpoint | Purpose |
|---|---|
| `GET /api/query/databases` | List all databases |
| `GET /api/query/tables?database=db` | List tables in a database |
| `GET /api/query/columns?database=db&table=tbl` | List columns with types |
| `GET /api/query/data-types` | List all ClickHouse data types |
| `GET /api/query/clusters` | List cluster names |

### Table Browser

Click a table in the explorer to open a table tab showing:

- Column list with types
- Data preview (paginated, default 100 rows, max 1000)
- Host info and table metadata

### Autocomplete

The editor provides schema-aware completions using database, table, and column names from the explorer endpoints.

## Schema Operations

Admin-only operations for managing databases and tables.

### Create Database

```bash
POST /api/query/schema/database
```

```json
{
  "name": "analytics",
  "engine": "Atomic",
  "on_cluster": "default",
  "if_not_exists": true
}
```

### Drop Database

```bash
POST /api/query/schema/database/drop
```

```json
{
  "name": "analytics",
  "if_exists": true,
  "sync": true
}
```

### Create Table

```bash
POST /api/query/schema/table
```

```json
{
  "database": "analytics",
  "name": "events",
  "engine": "MergeTree",
  "columns": [
    { "name": "id", "type": "UInt64" },
    { "name": "timestamp", "type": "DateTime" },
    { "name": "event_type", "type": "String" }
  ],
  "order_by": "(timestamp)",
  "partition_by": "toYYYYMM(timestamp)",
  "if_not_exists": true
}
```

### Drop Table

```bash
POST /api/query/schema/table/drop
```

```json
{
  "database": "analytics",
  "name": "events",
  "if_exists": true
}
```

## Data Upload

Upload CSV, JSON, JSONL, or Parquet files into ClickHouse tables. Max file size: **25 MB**.

### Two-Step Process

**Step 1: Discover schema**

```bash
POST /api/query/upload/discover
Content-Type: multipart/form-data
```

Upload the file. The backend detects the format, parses all rows, infers column types, and returns a preview (20 rows) with the inferred schema.

Type inference order: Bool > Int64 > Float64 > DateTime > Date > String. Nullable wrapper added if any null values found.

**Step 2: Ingest data**

```bash
POST /api/query/upload/ingest
Content-Type: multipart/form-data
```

Confirm the target database, table, and column mapping. Optionally create the table with a custom engine, ORDER BY, PARTITION BY, TTL, etc.

Data is inserted via `JSONEachRow` in **500-row batches** with a 90-second timeout per batch.

### Response

```json
{
  "success": true,
  "database": "analytics",
  "table": "events",
  "rows_inserted": 15000,
  "created_table": true,
  "commands": {
    "create_table": "CREATE TABLE ...",
    "insert": "INSERT INTO ... FORMAT JSONEachRow"
  }
}
```

## Saved Queries

Save, duplicate, update, and delete SQL snippets for reuse.

```bash
# List
GET /api/saved-queries

# Create
POST /api/saved-queries
{ "name": "Daily active users", "query": "SELECT count(DISTINCT user_id) ..." }

# Update (also used to rename: send just the name)
PUT /api/saved-queries/{id}
{ "name": "Updated name", "query": "..." }

# Duplicate (optionally pass a name for the copy)
POST /api/saved-queries/{id}/duplicate
{ "name": "My copy" }   # optional; defaults to "<original> (copy)"

# Delete
DELETE /api/saved-queries/{id}
```

Opening a saved query in the editor links the tab to the saved query ID. Changes are tracked via a dirty flag. Rename a saved query from the **Saved Queries** list (right-click → **Rename**) or by sending `name` to the `PUT` endpoint.

### The Saved Queries page

**Explore → Saved Queries** lists your library like files in a folder: one search box, an **All / Verified** switch, and a table with sortable **Name** and **Updated** columns (click a header to flip the order). Updated shows relative time, with the full date on hover.

- Click or double-click a row to open the query in the editor.
- Hover a row for **Open** and a **⋯** menu; right-click opens the same menu.
- The details sheet shows the SQL, description and dates, with the actions in its footer.

### Verified queries

Mark a saved query as **verified** once someone has checked that the SQL is correct. The flag shows in the list and filters it with the Verified switch. AI clients connected through the [MCP server](/docs/mcp) see it too, and are told to prefer a verified query over writing new SQL for the same question. Verification is a review mark, not a permission: unverified queries still run.

```bash
PUT /api/saved-queries/{id}
{ "verified": true }
```

## Query Parameters

Query parameters in the editor are part of the free core. Only the saved-query Run API below needs a Pro license.

Use ClickHouse's native bind-parameter syntax, `{name:Type}`, directly in your SQL. CH-UI detects parameters automatically and renders an input for each one above the results, so you can test parameterized queries in the editor without hitting an "unbound parameter" error.

```sql
SELECT *
FROM events
WHERE user_id = {user_id:UInt64}
  AND created_at >= {since:DateTime}
LIMIT {limit:UInt32}
```

Values are bound safely by ClickHouse (passed as `param_<name>` URL params), never string-interpolated into the SQL.

### Default values

When you save a parameterized query, the current parameter values are stored as defaults (in the `parameters` field):

```bash
POST /api/saved-queries
{
  "name": "Events for user",
  "query": "SELECT * FROM events WHERE user_id = {user_id:UInt64}",
  "parameters": { "user_id": "1001" }
}
```

### Running a saved query via the API

Execute a saved query, with parameters, and get the result back as JSON. Stored defaults are merged with any `params` you supply (your values win). **Pro only** (an `enterprise` edition license unlocks it too).

```bash
POST /api/saved-queries/{id}/run
{
  "params": { "user_id": "42", "since": "2026-01-01 00:00:00", "limit": "100" },
  "maxResultRows": 1000,
  "timeout": 30
}
```

The response matches the standard query-execution shape (`data`, `meta`, `statistics`, `rows`, `elapsed_ms`). Without an active Pro license this endpoint returns `402 Payment Required`, including during the grace period after expiry.

## Command Palette

Press **⌘K** (macOS) or **Ctrl+K** anywhere in the workspace to open the command palette:

- **Navigate**: fuzzy-search pages, tables, recent tabs, and (Pro) saved queries, dashboards, models, pipelines, Brain chats, and telemetry tabs
- **Run actions**: new query (`⌘⇧N`), new dashboard / model / pipeline / Brain chat, toggle theme, sign out
- **Scope with prefixes** (Pro): `>` actions, `t:` tables, `q:` saved queries, `d:` dashboards, `m:` models, `p:` pipelines, `b:` Brain chats, `tel:` telemetry, `?` help
- **Ask Brain** (Pro): type a question and hit Enter to seed a Brain chat with the prompt

## Display Controls

### Max Result Rows

Controls how many rows the UI requests from the server. Stored in localStorage (`ch-ui-max-result-rows`).

- Default: **1000**
- Minimum: 1
- Sent as `maxResultRows` parameter on query execution

### Number Formatting

Toggle locale-aware number formatting (thousands separators). Stored in localStorage (`ch-ui-format-numbers`), enabled by default.

## Guardrails Integration

All query endpoints run governance guardrails before execution. If a policy with `block` enforcement mode matches, the query is rejected with a 403. See [Governance](/docs/governance) for details.

## Safety Defaults

| Control | Value |
|---|---|
| Query timeout | 30s default, 5 min max |
| Stream row limit | 1,000,000 |
| Upload file size | 25 MB |
| Upload batch size | 500 rows |
| Schema operations | Admin-only |
| Brain-generated SQL | LIMIT 100 default |
