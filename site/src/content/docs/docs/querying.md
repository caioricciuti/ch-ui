---
title: SQL Workspace
description: Multi-tab SQL editor with streaming results, result filtering & sorting, automatic query history, error navigation, data upload, and saved queries
---

CH-UI provides a ClickHouse-first SQL workspace with a multi-tab editor, schema explorer, autocomplete, query streaming, result filtering and sorting, automatic query history, error navigation, data upload, and saved queries.

New to CH-UI? [Run your first query](/docs/guides/first-query) walks through sign-in, running and saving in a few steps.

## Overview

The rail on the left groups the product into six areas:

| Group | Pages |
|---|---|
| **Query** | The SQL workspace: editor tabs and the schema explorer |
| **Explore** | Saved Queries, Lineage, Brain |
| **Visualize** | Dashboards, Telemetry |
| **Build** | Models, Pipelines, Schedules, Schema Compare |
| **Operate** | Health: Fleet, Cluster Health. Performance: Performance, Query Insights, Cost Center. Control: Governance, Incident Timeline, Reports |
| **Settings** | Admin, Instance (license, instance details, legal) |

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

Open up to two editor groups side by side. Tabs can be moved between groups. Duplicate detection prevents opening the same saved query twice: opening it again brings the existing tab forward and, if the tab has no unsaved edits, reloads the stored SQL and parameter defaults into it.

### Running statements

| Shortcut | Runs |
|---|---|
| **⌘↵** / **Ctrl+Enter** | The selection; with no selection, the statement under the cursor when the editor holds several (the active statement is shaded); otherwise the whole editor |
| **⇧⌘↵** / **Shift+Ctrl+Enter** | Every statement in the editor (or in the selection), one after another, stopping at the first error. Each statement gets its own result tab |

Statements are split on `;` outside strings, quoted identifiers, heredocs and comments. The Run button, Explain, the query plan, sampling and the cost estimate all use the same text as **⌘↵**, with the current parameter values.

### Formatting

**Format** (**Shift+Alt+F**) asks ClickHouse to format the SQL with `formatQuery()` (ClickHouse 23.10 and later), so the result always means the same as the input. ClickHouse's formatter drops comments; CH-UI tells you when that happens, and **⌘Z** brings them back. On older servers, or when ClickHouse cannot parse the text, a built-in formatter puts clauses on their own lines and upper-cases a few unambiguous keywords, without touching strings, comments or identifiers.

### Cost estimate

After you stop typing for a few seconds, the toolbar shows an `EXPLAIN ESTIMATE` of the rows, parts and marks the query would read. It runs only when the editor holds a single `SELECT` (or `WITH`) whose parameters all have values, and it goes through the same governance guardrails as a run.

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
| `done` | Statistics (rows and bytes read, rows written for writes), total row count, and the `query_id` |
| `error` | Error message |

Stream limit: 1,000,000 rows.

### Live progress and cancel

While a query runs, the result panel shows elapsed time, percent complete, rows and bytes read, and read throughput, the same numbers ClickHouse's own `/play` reports. Progress is sampled from `system.processes` every 300 ms. A user without `SELECT` on `system.processes` simply gets no live readout, never an error. The readout stays after the query ends, so a query too short to report progress still shows what it read.

Statements that work while the connection is open report progress too (`INSERT ... SELECT`, `OPTIMIZE TABLE ... FINAL`); mutations run in the background, so their readout covers only the statement itself.

Cancelling a query, or closing its tab, stops it on ClickHouse: every streamed query carries a `query_id`, and an abandoned stream is killed with `KILL QUERY` instead of running to completion unattended.

### EXPLAIN & Profiling

| Endpoint | Purpose |
|---|---|
| `POST /api/query/explain` | Raw EXPLAIN output (a statement that already starts with `EXPLAIN` runs as written) |
| `POST /api/query/plan` | Parsed query plan as tree (tries EXPLAIN PLAN, falls back to EXPLAIN AST, then generic) |
| `POST /api/query/profile` | `system.query_log` metrics of a streamed run, looked up by the `query_id` from its `done` message (duration, read rows/bytes, memory). `query_log` is flushed asynchronously, so the lookup retries once |
| `POST /api/query/sample` | First N rows per shard (default 25, max 500), falls back to global LIMIT |
| `POST /api/query/estimate` | `EXPLAIN ESTIMATE` for a single `SELECT` |

Explain, plan, sample and estimate accept the same `params` object as a run.

## Working with Results

### Sorting & Filtering

Click any column header to sort, or use the per-column filter popovers (type-aware operators: contains, equals, comparisons, NULL checks). Active filters show as removable chips above the grid.

CH-UI picks the cheapest *correct* strategy automatically:

- **Complete result in memory** (under the row limit and ≤ 50k rows): sorting and filtering happen instantly client-side.
- **Truncated or huge results**: the original query is re-run on the server wrapped as `SELECT * FROM (your query) WHERE … ORDER BY …`, BigQuery-style, so you get the *true* top-N over the full dataset instead of a misleading sort of the loaded page. The footer marks these views as **server-filtered**.

Exports always reflect the filtered/sorted view you're looking at. The feature can be toggled off in the result footer.

### Result tabs

Every result sits in a tab in the strip above the result grid. A tab shows the statement number and its first keywords (`2 · SELECT count from`), a status icon (running, done, failed), the row count and the elapsed time. Click a tab, or use the arrow keys in the strip, to switch.

- **⇧⌘↵** clears the unpinned result tabs, then adds one tab per statement as it runs.
- **⌘↵** replaces only the active result tab. When that tab is pinned, the new result gets a tab of its own instead.
- Sorting and filtering apply to the active tab and are remembered per tab.
- A query tab keeps at most 10 result tabs; past that, the oldest unpinned one is closed. Each result keeps no more rows than the **Rows** limit.

### Pinned results

Hover a result tab and click the pin to keep it: the next run never replaces a pinned result. A query tab holds up to 5 pinned results; unpin one to pin another. Pinned results stay when you switch query tabs, and come back after a page reload with their columns and up to the first 1,000 rows. When a pinned result had more rows than that, a note says so; re-run the statement to reload it in full. Pinned results are kept in this browser only (localStorage), not on the server.

### Rows, cells and exports

Rows appear in the grid as they stream in. A statement that returns no columns (DDL, `INSERT`) shows **Statement executed** with the rows written, when ClickHouse reports them, and the elapsed time.

Click a cell to focus it, then press **Enter** (or double-click the cell) to open the cell inspector, which pretty-prints JSON, arrays and maps and copies the value. **⌘C** / **Ctrl+C** copies the focused cell; arrow keys move between cells.

The **Export** menu has two parts. **Current rows** copies or downloads the rows loaded in the grid. They come as CSV, TSV, JSON, JSONCompact, JSONLines, Markdown, SQL `INSERT` and XML. CSV and TSV prefix text that starts with `=`, `+`, `-` or `@` with `'` so spreadsheets do not run it as a formula; numbers such as `-5` are left as they are. The SQL `INSERT` export quotes values by column type (a `String` column holding `01234` stays a string) and writes arrays, maps and tuples as ClickHouse literals.

### Chart a result

The **Chart** view in the result footer, after Insights, draws the result with the same charts dashboards use. It picks a type from the columns: one row with a single numeric column is a stat, a date or DateTime column with numeric columns is a time series, and a text column with numeric columns is a bar chart. A result with no numeric column has nothing to chart. The column on the right changes the type (table, stat, gauge, time series, bar, pie, histogram, scatter or heatmap; see [Panel Types](/docs/dashboards#panel-types)), the X column, the Y columns and a **Series** column, which splits the first Y column into one line or bar per value (up to 10). Settings are kept per query tab until the columns change.

The chart follows the sorting and filters of the Data view and draws at most the first 10,000 rows.

**Add to dashboard** saves the statement that produced the result, with the chart settings, as a panel on an existing dashboard or on a new one (pick a name and a folder). Choose the panel name and a half or full width; the panel goes below the existing panels. The panel runs the statement as written, without the editor's filters. Statements with `{name:Type}` bind parameters cannot be added, since dashboards have no bind parameters. Viewers do not see the button.

### Full result export

**Full result (server)** in the Export menu runs the statement of the active result again on the server and downloads everything it returns, not just the loaded rows. The formats are ClickHouse's own output formats:

| Menu | ClickHouse format | File |
|---|---|---|
| CSV | `CSVWithNames` | `.csv` |
| TSV | `TSVWithNames` | `.tsv` |
| JSONLines | `JSONEachRow` | `.jsonl` |
| Parquet | `Parquet` | `.parquet` |

There is no Excel (XLSX) option: ClickHouse has no XLSX output format. Open the CSV or Parquet file in your spreadsheet tool instead.

The export goes through the same checks as a run: governance guardrails, your session's ClickHouse user, the query parameter values, the audit log (`query.export`) and Query History. It always runs with `readonly=1`, so it cannot change data. The footer shows the bytes received while the export runs, with a button to cancel it.

The export runs without the grid's row limit but with the server's own caps: 10,000,000 rows and 1 GiB by default, set with `export_max_rows` and `export_max_bytes` (see [Configuration](/docs/configuration)). A statement over a cap fails with an error that names it. When ClickHouse fails after the download has started, the server cuts the download off, so you never get a truncated file that looks complete; the reason is in Query History.

The file is collected in the browser before it is saved, so very large exports need the memory to hold them. The statement must not end with its own `FORMAT` clause. Results of Explain and Sample cannot be exported this way.

Over a tunnel, the export needs a CH-UI agent that supports it. An older agent gets the error "update the agent (ch-ui-agent) to export"; the embedded local connection always supports it.

```bash
curl -X POST http://localhost:3488/api/query/export \
  -H 'Content-Type: application/json' \
  -b cookies.txt \
  -d '{"sql": "SELECT * FROM events WHERE day = {day:Date}", "format": "Parquet", "params": {"day": "2026-10-01"}}' \
  -o events.parquet
```

### Explorer shortcuts

Right-click a table in the explorer for **Preview 100 Rows** (opens a query tab), **Show DDL** (the `SHOW CREATE TABLE` output in a side panel, with copy), **Copy Qualified Name** and **Copy Column List**. Refreshing the schema also refreshes the editor's autocomplete.

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

Click a table in the explorer to open a table tab. Its header names the database as a breadcrumb, and it has four tabs:

- **Overview**: rows, size, engine and last modified, the storage keys (partition, sorting, primary, sampling), and the `CREATE TABLE` statement with a copy button
- **Schema**: column list with types
- **Data Sample**: data preview (paginated, default 100 rows, max 1000)
- **Lineage**: what feeds and reads the table

A database tab has **Overview** (tables, rows, total size, engine) and **Tables**.

### Autocomplete

The editor provides schema-aware completions using database, table, and column names from the explorer endpoints.

### Open a table from the editor

Hold **⌘** (macOS) or **Ctrl** and click a database or table name in the editor to open its tab. Names you can open have a dotted underline, and hovering one shows what it opens. Holding the key turns the underline solid. In `otel.otel_logs`, clicking `otel` opens the database and clicking `otel_logs` opens the table; backtick and double-quote quoting both work. A table name without a database (`FROM otel_logs`) is a link only when exactly one database has a table with that name.

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

Save SQL for reuse, organize it in folders, decide who can see it, and get back any earlier version.

### Who can see a saved query

Every saved query has an owner (the person who saved it) and a visibility:

| Visibility | Who can see and run it |
|---|---|
| **Everyone** (`shared`, the default) | Every user of the instance |
| **Only me** (`private`) | The owner |
| **Admins** (`admins`) | Admins and the owner. Only admins can choose it |

- Only the owner or an admin can edit, move, tag, delete, restore or change the visibility of a query. Viewers can read and run what they can see.
- The **verified** mark can be changed by the owner or an admin.
- Queries saved before owners and visibility existed are shared and have no owner. Any admin or analyst can still edit them, as before. Making one private makes you its owner.
- A query you cannot see answers `404`, in the UI, the API, Brain and the MCP server alike.
- Admins can pick **All owners** in the toolbar to see everyone's private queries, for example when someone leaves the team.
- A schedule keeps running a query that became private. The schedules list then shows only its name.
- **Duplicate** makes a private copy owned by you, in the same folder.

### The Saved Queries page

**Explore → Saved Queries** has four sections in the sidebar: **All**, **Mine** (queries you own), **Shared** (visible to everyone) and **Verified**.

- The folder tree on the left nests like a file browser. Selecting a folder lists it and everything below it. Folders are shared by everyone; any admin or analyst can create, rename and delete them.
- Deleting a folder deletes its subfolders too. The queries inside move to the root; none are deleted.
- The toolbar searches name, description, SQL and tags, and filters by tag.
- The table shows **Name**, **Folder**, **Owner**, **Visibility** and **Updated**. Click a row to open the query in the editor. Drag a row onto a folder to move it.
- Right-click a row (or use its menu) for **Open**, **Edit details** (name, description, folder, tags, visibility), **Move**, **Duplicate**, **History**, **Copy SQL** and **Delete**. Actions you are not allowed to take are disabled.

The **Save query** form in the editor also asks for a folder and the visibility.

### Version history

Saving a change to the name, description, SQL, parameters or connection records a new version. Moving, tagging and visibility changes do not. A save that changes nothing records nothing, and each query keeps its latest 50 versions. Queries saved before history existed get their first version, the state before the change, the first time they are edited.

**History** in the row menu lists the versions with who saved them and when, and previews the SQL of the selected one. **Restore** writes that version back and records it as a new version noted "Restored version N", so nothing is lost.

### API

```bash
# List what you can see; scope=all|mine|shared|verified, folder=<id>|root, owners=all (admins)
GET /api/saved-queries?scope=mine

# Create (folder_id, tags and visibility are optional; visibility defaults to shared)
POST /api/saved-queries
{ "name": "Daily active users", "query": "SELECT count(DISTINCT user_id) ...",
  "folder_id": "<folder id>", "tags": ["product"], "visibility": "private" }

# Update any subset of the fields (also used to rename, move and re-tag)
PUT /api/saved-queries/{id}
{ "name": "Updated name", "folder_id": "", "visibility": "shared" }

# Duplicate as your own private copy (optionally pass a name)
POST /api/saved-queries/{id}/duplicate
{ "name": "My copy" }

# Delete
DELETE /api/saved-queries/{id}

# Folders
GET    /api/saved-queries/folders
POST   /api/saved-queries/folders          { "name": "Finance", "parent_id": "" }
PUT    /api/saved-queries/folders/{id}     { "name": "Billing" }
DELETE /api/saved-queries/folders/{id}

# History
GET  /api/saved-queries/{id}/versions
GET  /api/saved-queries/{id}/versions/{n}
POST /api/saved-queries/{id}/versions/{n}/restore
```

Each listed query carries `folder_id`, `tags`, `visibility`, `owner` and `can_edit` (whether you may change it). A sibling folder with the same name answers `409`. Moves, visibility changes and restores are written to the audit log as `saved_query.moved`, `saved_query.visibility_changed` and `saved_query.version_restored`.

Opening a saved query in the editor links the tab to the saved query ID. Changes are tracked via a dirty flag.

### Verified queries

Mark a saved query as **verified** once someone has checked that the SQL is correct. The list shows it, and the **Verified** section filters on it. AI clients connected through the [MCP server](/docs/mcp) see it too, and are told to prefer a verified query over writing new SQL for the same question. Verification is a review mark, not a permission: unverified queries still run.

MCP keys only see shared queries and the queries the key saved itself. Brain sees what the person chatting sees.

```bash
PUT /api/saved-queries/{id}
{ "verified": true }
```

## Query Parameters

Query parameters in the editor are part of the free core. Only the saved-query Run API below needs a Pro license.

Use ClickHouse's native bind-parameter syntax, `{name:Type}`, directly in your SQL. CH-UI detects parameters automatically (ignoring text inside strings and comments) and counts them on the **Parameters** button in the editor toolbar. The button opens a popover with an input for each one, and **Run** opens it on the first parameter that still has no value, so you never hit an "unbound parameter" error. Values are kept per tab, across tab switches and reloads.

```sql
SELECT *
FROM events
WHERE user_id = {user_id:UInt64}
  AND created_at >= {since:DateTime}
LIMIT {limit:UInt32}
```

Values are bound safely by ClickHouse (passed as `param_<name>` URL params), never string-interpolated into the SQL.

### Default values

When you save a parameterized query, the current parameter values are stored as defaults (in the `parameters` field). Opening the saved query fills the inputs with them. Saving again with a field left empty keeps the stored default for it instead of erasing it:

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

- **Navigate**: fuzzy-search pages, every page section (listed as "Page · Section", for example "Governance · Alert rules", with the same admin and Pro gating as the sidebar), tables, recent tabs, and (Pro) saved queries, dashboards, models, pipelines, Brain chats, and telemetry tabs
- **Run actions**: new query (**⌥N** / **Alt+N**, or **⌘⇧N**), new dashboard, model or pipeline (each opens its create sheet), **Send data to Telemetry**, new Brain chat, and for admins **New MCP service key** and **New ClickHouse user**; also theme, switch connection, keyboard shortcuts and sign out
- **Scope with prefixes** (Pro): `>` actions, `t:` tables, `q:` saved queries, `d:` dashboards, `m:` models, `p:` pipelines, `b:` Brain chats, `tel:` telemetry, `?` help
- **Ask Brain** (Pro): type a question and hit Enter to seed a Brain chat with the prompt

Press **?** anywhere outside a text field for the full list of keyboard shortcuts.

### Keyboard in sheets and dialogs

- **Esc** closes only the topmost layer: an open dropdown, menu or the command palette inside a sheet closes first, then the sheet. In a code editor inside a sheet, the first Esc leaves the editor and the second closes the sheet, so unsaved SQL is not lost by accident.
- **Enter** in a one-line field runs the sheet's main footer button. It does not fire in multi-line fields, code editors, or on a danger button.
- **Tab** moves between fields and stays inside the open sheet; when it closes, focus goes back to what opened it.
- Workspace shortcuts such as **Alt+W** and the Telemetry list keys do not act on the page behind an open sheet. **⌘K** still works.

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
| Full export | `readonly=1`; 10,000,000 rows and 1 GiB by default (`export_max_rows`, `export_max_bytes`) |
| Upload file size | 25 MB |
| Upload batch size | 500 rows |
| Schema operations | Admin-only |
| Brain-generated SQL | LIMIT 100 default |
