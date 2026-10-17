---
title: Dashboards
description: Build and share ClickHouse dashboards with SQL-powered panels
---

Dashboards let you build visual panels backed by SQL queries against your ClickHouse data. Each panel supports time-range interpolation, multiple chart types, and a grid layout.

Available on all plans (limits scale by plan).

## Overview

Each dashboard has:

- **A name and description**
- **One or more panels**: each with its own SQL query, chart type, and layout position
- **A shared time range**: applied to all panels simultaneously
- **Variables** (optional): drop-downs and inputs above the panels that filter every query
- **A version history**: every save is kept, and any version can be restored

A default "System Overview" dashboard is auto-created on first access. To start from something ready made, use **New from template** in the dashboard browser.

## Folders, tags and stars

Open **Visualize → Dashboards** to get the dashboard browser: a folder tree next to a table of dashboards. Each dashboard opens as its own page, with a breadcrumb that shows its folder path.

- **Folders** nest as deep as you like. Names are unique within the same parent. Rename a folder or move it under another one; moving a folder inside its own subtree is refused.
- **Move** a dashboard by dragging it onto a folder in the tree, or from its menu.
- **Deleting a folder deletes no dashboards.** Its dashboards and subfolders move up one level, in a single transaction.
- **Tags** are free-form labels on a dashboard, set when you create it or later.
- **Stars** are per person: starring a dashboard does not change what anyone else sees. Anyone who can view dashboards can star them.
- The **command palette** (⌘K) shows each dashboard with its folder path.

Creating, moving and tagging dashboards and changing folders needs the analyst or admin role. Folder changes and moves are recorded in the [audit log](/docs/audit-log) (`dashboard.folder.created`, `dashboard.folder.updated`, `dashboard.folder.deleted`, `dashboard.moved`).

```bash
# Folders
GET    /api/dashboards/folders
POST   /api/dashboards/folders
{ "name": "Payments", "parent_id": "" }          # empty parent_id: top level
PUT    /api/dashboards/folders/{folderId}
{ "name": "Billing", "parent_id": "<folder-id>" } # rename, move, or both
DELETE /api/dashboards/folders/{folderId}         # contents move up one level

# Move a dashboard (empty folder_id: top level)
PUT /api/dashboards/{id}/move
{ "folder_id": "<folder-id>" }

# Replace tags
PUT /api/dashboards/{id}/tags
{ "tags": ["prod", "finance"] }

# Star / unstar for the current user
POST   /api/dashboards/{id}/star
DELETE /api/dashboards/{id}/star
```

## Dashboard CRUD

```bash
# List all dashboards (each carries folder_id, tags, and whether you starred it)
GET /api/dashboards

# Create (folder_id and tags are optional)
POST /api/dashboards
{ "name": "Production Metrics", "description": "Key production KPIs", "folder_id": "<folder-id>", "tags": ["prod"] }

# Get dashboard + panels
GET /api/dashboards/{id}

# Update
PUT /api/dashboards/{id}
{ "name": "Updated Name" }

# Delete (cascades to all panels)
DELETE /api/dashboards/{id}
```

## Panel Types

| Type | Use Case |
|---|---|
| `table` | Tabular data (default). A panel shows the first 100 rows and says "Showing 100 of N rows"; fullscreen shows every row. |
| `stat` | Single number: last, first, mean, sum, min, max, count or range of a column, with units and color thresholds |
| `gauge` | Single number on an arc between a min and a max |
| `timeseries` | Line chart over time. Can be drawn as a filled area, and its series stacked |
| `bar` | Bar chart, grouped or stacked, over time, numbers or categories |
| `pie` | Pie or donut of a label column and a value column |
| `histogram` | Distribution of one numeric column, bucketed in the browser. The query returns raw values, not counts. 30 equal-width buckets by default, 1 to 200. Rows without a number are left out, and a note says how many |
| `scatter` | One point per row: an X and a Y column, an optional size column (the point area follows the value) and an optional group column that colors the points (up to 10 groups, the rest as "Other"). At most 10,000 points are drawn |
| `heatmap` | A grid of cells: one column per X value, one row per Y value. Each cell sums the value column, or counts rows when there is no numeric column. Up to 200 values per axis; beyond that, neighbouring values share a cell |
| `text` | Markdown text, no query |

Any other `panel_type` is refused with `400`. The same chart types (all but `text`) are in the SQL editor's [Chart view](/docs/querying#chart-a-result), and Brain's `add_dashboard_panel` tool can create every one of them.

## Panel Editor

Each panel is configured with:

| Field | Description | Default |
|---|---|---|
| Name | Panel title | Required |
| Panel Type | One of the types above | `table` |
| Query | SQL query (supports the variables below). Not needed for `text`. | Required |
| Config | Display options as JSON (below) | `{}` |

### Chart Config

```json
{
  "chartType": "timeseries",
  "xColumn": "ts",
  "yColumns": ["queries", "errors"],
  "colors": ["#F97316", "#10B981"],
  "legendPosition": "bottom"
}
```

`legendPosition` is `bottom` (default), `right` or `none`. `seriesColumn` names a column whose values split the first Y column into one series each (rows in long form, such as `day, host, hits`); the 10 largest series are drawn and the legend says how many more were left out. Click a legend entry to hide or show that series. NULL values are drawn as gaps, not as zero. A series split by `seriesColumn` that has no row at some time is joined across it, so series that do not share timestamps still draw.

For `timeseries`, `fill` is `line` (default, a line with a light tint) or `area`, and `stacked: true` puts each series on top of the ones before it. A stacked series counts a missing value as 0, so the bands stay closed.

| Type | Keys | When unset |
|---|---|---|
| `histogram` | `histogramColumn`, `histogramBuckets` | The first numeric column, 30 buckets |
| `scatter` | `scatterX`, `scatterY`, `scatterSize`, `scatterSeries` | X and Y are the first two numeric columns; fixed point size; one group |
| `heatmap` | `heatmapX`, `heatmapY`, `heatmapValue` | X is the first date or text column, Y the next text column, the value the first other numeric column (else a row count) |

Other keys: `barMode` (`grouped` or `stacked`), `gaugeMin` and `gaugeMax`, `pieLabelColumn`, `pieValueColumn` and `pieDonut`, `content` (Markdown for `text`), and for `stat` and `gauge`: `statField`, `statCalculation`, `statUnit`, `statPrefix`, `statSuffix`, `statDecimals`, `statColorMode`, `statThresholds`, and `links` (see [Drilldown links](#drilldown-links)).

### Time zones

Panel queries run with `date_time_output_format=iso`, so DateTime values arrive as UTC instants and charts show them in the viewer's browser time zone. `Date` values have no time of day and are placed at local midnight, so each point keeps its calendar day.

## Layout

Panels are arranged on a **12-column grid**. Each panel stores its position:

| Field | Description | Default | Allowed |
|---|---|---|---|
| `layout_x` | Column offset | 0 | 0 or more, and `layout_x + layout_w` at most 12 |
| `layout_y` | Row offset | below the last panel | 0 to 10000 |
| `layout_w` | Width in columns | 6 | 1 to 12 |
| `layout_h` | Height in rows | 4 | 1 to 100 |

A position outside these bounds is refused with `400`. Drag and resize panels in the UI; positions are saved automatically. A new panel is placed below the existing ones; through the API, leave out `layout_y` to get the same placement. Panels can also come from the SQL editor: see [Chart a result](/docs/querying#chart-a-result). Below 640 px of width (phones), panels stack in one full-width column and cannot be dragged.

## Roles

Viewers can open dashboards, run their panels, change the time range and variable values, see the version history and star dashboards. Adding, editing, moving and deleting panels, renaming, settings, variables, import, restore, templates, duplicate and sharing need the analyst or admin role, and the UI shows those controls only to them.

Panels run on the connection of your session. A panel saved on another connection says so in its header ("Built on X, running on Y").

## Time Range

A shared time-range selector applies to all panels in the dashboard: quick ranges (last 5 minutes to last 30 days), named ranges (today, yesterday, this or last week, this or last month, last 3 or 6 months) and an absolute range picked on a calendar, in UTC or the browser time zone.

The range is kept in the address bar as `?from=&to=` (for example `?from=now-6h&to=now`, or ISO timestamps for an absolute range; Grafana-style epoch milliseconds are accepted too), so a copied link opens on the same range. Without one, the last range you used is restored (`ch-ui-dashboard-time-range` in localStorage, default `1h`).

Relative values are a number and a unit: `s`, `m` (minutes), `h`, `d`, `w`, `M` (months) and `y`, optionally prefixed with `now-`. `M` and `m` are different: `1M` is one month, `1m` is one minute. Long forms such as `15min` or `2months` work too.

## Time macros

Panel queries support these macros, replaced before the query runs:

| Variable | Replaced with |
|---|---|
| `$__timeFilter(col)` | A range predicate on `col`, in parentheses. See below. |
| `$__timeFilter(col, unit)` | The same, for a column of the given unit: `s`, `ms`, `us`, `ns`, or `datetime` (also for `Date`) |
| `$__timeFilter` | The same, on the `time_field` of the request (default `timestamp`) |
| `$__timestamp(col)` | `(col >= toDateTime(<from>) AND col <= toDateTime(<to>))`, with second precision |
| `$__timeFrom`, `$__timeTo` | The range bounds as epoch milliseconds (or in `time_field_unit` when the request sets one) |
| `$__interval` | The bucket size in whole seconds: the range divided by `max_data_points` (default 1000), at least 1 |
| `$__interval_ms` | The same bucket size in milliseconds |
| `$__timeField` | The request's `time_field` (default `timestamp`) |
| `$__table` | The request's `table` |

How `$__timeFilter(col)` picks the comparison:

1. A unit written in the macro wins: `$__timeFilter(created_at, ms)`.
2. Else the request's `time_field_unit`.
3. Else a unit suffix on the column name: `_s`, `_ms`, `_us`, `_ns` (`created_ms` is milliseconds).
4. Else the type is unknown and the predicate matches the range in seconds or in milliseconds:
   `((col >= <from_s> AND col <= <to_s>) OR (col >= <from_ms> AND col <= <to_ms>))`.
   This is right for `DateTime`, `DateTime64` and integer epoch columns in seconds or milliseconds, and still uses the primary key. ClickHouse refuses a `Date` column here ("Illegal types"): write `$__timeFilter(col, datetime)`.

With `datetime`, the predicate compares with `fromUnixTimestamp64Milli(<ms>)`, which is right for `Date`, `Date32`, `DateTime` and `DateTime64` columns in any time zone. A `Date` counts as its midnight, so use it with ranges of whole days. The column can be an expression: `$__timeFilter(toUnixTimestamp(event_time), s)`.

### Example Queries

Queries per bucket over the selected range:

```sql
SELECT
  toStartOfInterval(event_time, INTERVAL $__interval second) AS ts,
  count() AS queries
FROM system.query_log
WHERE $__timeFilter(event_time)
GROUP BY ts
ORDER BY ts
```

Millisecond buckets on a DateTime64 column:

```sql
SELECT
  toStartOfInterval(event_time_microseconds, toIntervalMillisecond($__interval_ms)) AS ts,
  count() AS queries
FROM system.query_log
WHERE $__timeFilter(event_time)
GROUP BY ts
ORDER BY ts
```

Epoch bounds directly:

```sql
SELECT count()
FROM system.query_log
WHERE toUnixTimestamp64Milli(event_time_microseconds) BETWEEN $__timeFrom AND $__timeTo
```

## Dashboard variables

Variables are the controls in the bar under the dashboard header. A panel query uses one as `$name` or `${name}`; when the value changes, every panel runs again. Define them in **Settings → Variables** (writers only). Their order matters: a variable can use the variables above it.

| Type | Values come from |
|---|---|
| `query` | A SQL query: the first column is the value, an optional second column the label shown. At most 1,000 rows. |
| `custom` | A comma separated list, for example `prod,staging,dev`. At most 1,000 values. |
| `text` | A free text input |
| `constant` | A fixed value, not shown in the bar (useful for a database name used by many panels) |
| `interval` | A comma separated list of durations, for example `1m,5m,1h`. Substituted as a number of seconds. |

`query`, `custom` and `interval` variables can be **multi-value**, and can offer **All**, which selects every option. Each variable can have a default; without one, a variable starts on its first option (or on All for a multi-value variable that offers it).

The selected values are kept in the address bar as `?var-name=value`, repeated for several values (`?var-database=default&var-database=system`), so a copied link opens with the same selection. All is written as `$__all`.

### How values are substituted

Values are always quoted by the server as ClickHouse string literals; the browser never pastes them into SQL. Quotes and backslashes in a value are escaped, so a value such as `x') OR 1=1 --` stays a string and matches nothing.

| You write | With values | The query gets |
|---|---|---|
| `$env` or `${env}` | `prod` | `'prod'` |
| `IN (${database})` | `default`, `system` | `IN ('default', 'system')` |
| `IN (${database})` | nothing selected | `IN (NULL)`, which matches no row |
| `${top:number}` | `10` | `10`, refused unless every value is a number |
| `$bucket` (interval) | `5m` | `300` |

- Do not put quotes around a variable: write `WHERE env = $env`, not `WHERE env = '$env'`. A variable inside a string literal is refused with an error, because the value is quoted already.
- A string literal compares fine with a number column: `WHERE id = $id` works for a `UInt64` column. Use `${name:number}` where ClickHouse needs a number, such as `LIMIT`.
- `${name:raw}` is not supported, on purpose: no value is ever put into SQL unquoted.
- Variables inside comments, quoted identifiers and `$$heredoc$$` strings are left alone. Names are word bounded: `$db` does not touch `$db_name`. Names starting with `__` are reserved for the time macros.
- Variables are substituted after the time macros, so a query variable can use `$__timeFilter` too.
- **All with more than 1,000 options.** The option list stops at 1,000 rows, but All never stops there. When a query variable returns 1,000 options or more, All is substituted as a subquery over the variable's own query (with its time macros and parent variables already filled in), so `host IN (${host})` becomes `host IN (SELECT host FROM (...))` and covers every value. That form only means the same thing inside `IN (...)`, so with such a selection the variable must be used exactly as `column IN ($name)`; anywhere else (`= $name`, `${name:number}`, inside a function) the panel fails with an error that says so, instead of quietly filtering to the first 1,000 values. The same applies on share links and in report summaries.

### Examples

A database and table picker, where the table list follows the database:

```sql
-- variable "database", type query, multi-value, All
SELECT name FROM system.databases ORDER BY name

-- variable "table", type query, multi-value, All
SELECT name FROM system.tables WHERE database IN (${database}) ORDER BY name

-- panel
SELECT table, count() AS parts, sum(rows) AS rows
FROM system.parts
WHERE active AND database IN (${database}) AND table IN (${table})
GROUP BY table
ORDER BY parts DESC
```

A bucket size the viewer picks (variable `bucket`, type interval, options `1m,5m,15m,1h`):

```sql
SELECT
  toStartOfInterval(event_time, INTERVAL $bucket second) AS ts,
  count() AS queries
FROM system.query_log
WHERE $__timeFilter(event_time, datetime)
GROUP BY ts
ORDER BY ts
```

A free text search (type text) and a top N (type custom, options `10,50,100`):

```sql
SELECT event_time, user, left(query, 200) AS query
FROM system.query_log
WHERE type = 'QueryFinish'
  AND $__timeFilter(event_time, datetime)
  AND positionCaseInsensitive(query, ${search}) > 0
ORDER BY event_time DESC
LIMIT ${top:number}
```

A query variable whose second column is the label:

```sql
SELECT name, concat(name, ' (', engine, ')') FROM system.databases ORDER BY name
```

### Variables API

```bash
# Replace the variable definitions (writers)
PUT /api/dashboards/{id}/variables
{ "variables": [
  { "name": "database", "label": "Database", "type": "query",
    "query": "SELECT name FROM system.databases ORDER BY name",
    "multi": true, "include_all": true, "default": ["$__all"] },
  { "name": "bucket", "type": "interval", "options": "1m,5m,1h", "default": ["5m"] }
] }

# Options of one variable; values of the variables above it come from "variables"
POST /api/dashboards/{id}/variables/{name}/options
{ "variables": { "database": ["system"] }, "time_range": { "type": "relative", "from": "1h", "to": "now" } }
```

`GET /api/dashboards/{id}` returns the definitions as `variables`. An options query runs with your credentials, read only (`readonly=1`), at most 1,000 rows, and your governance policies apply.

## Drilldown links

A panel can have up to 10 links that run when you click a data point, a bar, a pie slice, a heatmap cell or a table cell. Add them under **Links** in the panel editor, for table, time series, bar, pie, histogram, scatter and heatmap panels. With one link, a click runs it; with several, a menu opens where you clicked and shows each link's label. Panels without links behave as before.

| Kind | What a click does |
|---|---|
| Filter (`setVariable`) | Sets a dashboard variable to the clicked value, or to the **Value** template, and runs the panels again |
| Dashboard (`dashboard`) | Opens another dashboard. **Set variables** gives the target's variables a value each (templates); **Pass the current variable values along** copies this dashboard's selection. The time range is the bucket around the clicked point, or the current range when the click has no time. A deleted target says "That dashboard no longer exists" |
| SQL (`sql`) | Opens the filled-in query in a new SQL editor tab. It is never run |
| URL (`url`) | Opens an `http://` or `https://` URL in a new browser tab. Anything else (`javascript:`, `data:`, relative URLs) is refused |

Templates take these tokens:

| Token | Replaced with |
|---|---|
| `${__value}` | The clicked value, as ClickHouse returned it: a `Date` stays `2026-01-01` in every time zone, a `DateTime` is the UTC instant panels receive (`2026-01-01T10:00:00Z`) |
| `${__field}` | The column of the clicked value |
| `${__series}` | The series of the clicked point; empty when the panel has none |
| `${__row.<column>}` | Another column of the clicked row, such as `${__row.host}` |
| `${<variable>}` | A dashboard variable's current value; several values are joined with commas |
| `${__value:raw}` | SQL links only: the clicked value unquoted |

What is clicked, per panel: on a time series or bar, the X value, with the series name in `${__series}`; on a scatter, the point's row; on a heatmap, the X value of the cell, with the Y value in `${__series}`; on a pie, the slice label; on a histogram, the bucket's lower bound, with `${__row.from}`, `${__row.to}` and `${__row.count}`; on a table, the cell.

Values are encoded for where they go: percent-encoded in a URL, and in SQL quoted like the server quotes variables (finite numbers stay bare, everything else is a string literal, `NULL` for null). Tokens that match nothing are left as written.

```json
"links": [
  { "kind": "setVariable", "label": "Filter by host", "variable": "host" },
  { "kind": "dashboard", "label": "Host detail", "dashboardId": "<dashboard id>",
    "carryVariables": true, "params": { "host": "${__value}" } },
  { "kind": "sql", "label": "Recent errors",
    "template": "SELECT * FROM logs WHERE host = ${__value} ORDER BY ts DESC LIMIT 100" },
  { "kind": "url", "label": "Runbook", "template": "https://wiki.example.com/hosts/${__value}" }
]
```

Links are checked when a panel is saved, imported or restored: a known kind, at most 10 links, a variable or dashboard picked, a URL that starts with `http://` or `https://`, and `${__value:raw}` only in SQL links. A bad link is refused with `400`.

On a [public share link](#public-sharing), only Filter and URL links are kept; the server leaves SQL and Dashboard links out of what viewers receive, and a Filter link loses its value template there, so it uses the clicked value. A Filter applies only to `query`, `custom` and `interval` variables, and only when the clicked value is one of the variable's options. The fullscreen panel view has no drilldown.

## Panel Query Execution

Panel queries use a dedicated endpoint with variable interpolation:

```bash
POST /api/dashboards/query
```

```json
{
  "query": "SELECT ...",
  "timeout": 30,
  "time_range": { "type": "absolute", "from": "2026-03-15T00:00:00Z", "to": "2026-03-15T01:00:00Z" },
  "time_field": "timestamp",
  "time_field_unit": "ms",
  "max_data_points": 1000,
  "dashboard_id": "<dashboard id>",
  "variables": { "database": ["default", "system"], "bucket": ["5m"] }
}
```

With `dashboard_id`, the dashboard's variable definitions apply: missing values take their defaults, `$__all` expands to every option, and interval values become seconds. Without it, the values in `variables` are substituted as quoted strings.

`time_range.type` is `relative` (with `from` like `1h` or `now-1h` and `to` like `now`) or `absolute` (ISO timestamps). Timeout max: 5 minutes. Results are capped at 100,000 rows.

The expanded query is checked against your [governance](/docs/governance) policies exactly like a query from the SQL editor; a blocked query gets `403` with `"code": "policy_blocked"`. The response includes the expanded query and the resolved variable values.

## Panel CRUD

```bash
# Create panel
POST /api/dashboards/{id}/panels
{
  "name": "Queries per bucket",
  "panel_type": "timeseries",
  "query": "SELECT toStartOfInterval(event_time, INTERVAL $__interval second) AS ts, count() AS queries FROM system.query_log WHERE $__timeFilter(event_time) GROUP BY ts ORDER BY ts",
  "config": "{\"chartType\":\"timeseries\",\"xColumn\":\"ts\",\"yColumns\":[\"queries\"]}",
  "layout_x": 0, "layout_y": 0, "layout_w": 12, "layout_h": 4
}

# Update panel (only the fields you send change)
PUT /api/dashboards/{id}/panels/{panelId}
{ "name": "Updated Title", "query": "..." }

# Delete panel
DELETE /api/dashboards/{id}/panels/{panelId}
```

## Import and Export

**Settings → Export / Import** downloads a dashboard as JSON and loads a file into the JSON editor for review. **Apply** replaces the dashboard's name, description, variables and panels. A file without `variables` keeps the current ones.

```bash
POST /api/dashboards/{id}/import
{
  "version": 1,
  "dashboard": { "name": "Production Metrics", "description": "" },
  "variables": [ { "name": "env", "type": "custom", "options": "prod,dev", "default": ["prod"] } ],
  "panels": [
    { "name": "Queries", "panel_type": "timeseries", "query": "SELECT ...", "config": { "chartType": "timeseries" },
      "layout_x": 0, "layout_y": 0, "layout_w": 12, "layout_h": 4 }
  ]
}
```

Every panel is validated first (name, type, query, config, layout; at most 200 panels), and the swap runs in one transaction: a bad file is refused with `400` naming the panel, and the dashboard stays as it was. Recorded in the audit log as `dashboard.imported`.

## Version history

Every save of a dashboard is kept as a version: panels added, edited, moved or deleted, settings, variables, imports and restores. **Settings → History** lists the versions with when, who and a note, and **Restore** puts the dashboard back to one of them.

- Moving and resizing panels within 30 seconds of the last version updates that version instead of adding one, so dragging panels around does not flood the history. Any other change always adds a version.
- The latest 50 versions of each dashboard are kept.
- A restore goes through the same validation and single transaction as an import, and is itself recorded as a new version ("Restored version 12"), so it can be undone.
- A version is the same JSON as an export: dashboard, variables and panels.

```bash
GET  /api/dashboards/{id}/versions                 # newest first, without the JSON
GET  /api/dashboards/{id}/versions/{n}             # one version with its snapshot
POST /api/dashboards/{id}/versions/{n}/restore     # writers; audit: dashboard.version_restored
```

## Templates

**New from template** in the dashboard browser creates a dashboard from a built-in template, with its panels and variables, in one transaction:

| Template | Panels |
|---|---|
| ClickHouse server health | Queries and inserts per second, memory, merges (from `system.metric_log`), parts per partition for the top tables, replication lag (`system.replicas`), errors (`system.errors`), disk usage (`system.disks`), slowest queries of the last hour. A `database` variable filters the parts and replication panels. |
| Query activity | Queries over time, top users, top query patterns by `normalized_query_hash`, failed queries. A `user` variable (multi-value, All by default) and a `bucket` interval filter every panel. |

Both read only system tables and run on ClickHouse 25.8. A panel whose table is missing or not readable (for example `system.metric_log` turned off) shows its own error; the rest of the dashboard still loads.

```bash
GET  /api/dashboards/templates
POST /api/dashboards/from-template
{ "template_id": "query-activity", "name": "Query activity (prod)", "folder_id": "" }
```

## Duplicate

**Duplicate** (in the dashboard browser, or **Settings → General**) copies a dashboard with its panels and variables into a new dashboard called "<name> (copy)", in the same folder and with the same tags, in one transaction. Share links and history are not copied.

```bash
POST /api/dashboards/{id}/duplicate
{ "name": "Optional new name" }
```

## Public Sharing

Any dashboard can be shared with a **public read-only link**; no CH-UI login is required to view it. Create a share from the dashboard's share menu (or via the API) and send the link; revoke it anytime to cut access instantly.

```bash
# List share links for a dashboard
GET /api/dashboards/{id}/shares

# Create a share (returns the public token)
POST /api/dashboards/{id}/shares

# Invite someone by email to a share
POST /api/dashboards/{id}/shares/{shareId}/invite

# Revoke a share
DELETE /api/dashboards/{id}/shares/{shareId}
```

Public viewers load the dashboard at `/public/d/<token>`. Their panel queries execute through a token-scoped endpoint (`/api/public/dashboards/{token}/query`). They can only run the queries the dashboard's panels define, never arbitrary SQL, and they never see your connection credentials. Queries run read only, for at most 30 seconds and 10,000 rows, and your governance policies apply.

Variables work on public links too, with the values limited to what the dashboard offers: a value must be one of the variable's options (the options query runs again on the server with the sharer's credentials, cached for 60 seconds), and a text or constant variable is always its default. Anything else is refused with `400`. Public viewers do not see the options queries.

Panel and options queries from a share link carry the `public_dashboard` [query tag](/docs/query-insights#query-tags) in `log_comment`, naming the person who created the share and the dashboard (`{"chui":"public_dashboard","person":"ana@example.com","object":"dash:<id>"}`), so they are attributed to the sharer in `system.query_log`. A share created by an SSO person on a connection with [per-person ClickHouse users](/docs/sso#per-person-clickhouse-users) stores no password: it runs as that person's live user, survives the password rotation at each sign-in, and answers `410 Gone` once the user is locked or dropped.

Link options for wall screens and TVs:

| Parameter | Example | Effect |
|---|---|---|
| `from`, `to` | `?from=now-24h&to=now` | Time range, as in the app |
| `theme` | `?theme=light` | `dark` or `light`, for this page only |
| `refresh` | `?refresh=30s` | Auto refresh: seconds, `m` or `h`, at least 5 s. A refresh is skipped while the previous one is still running. |
| `var-<name>` | `?var-env=prod` | Variable values, repeated for several values |

Public panel results are cached in memory for 20 seconds, with time ranges rounded to 15 seconds, so many screens showing the same dashboard run each panel once. Queries that miss the cache are limited to 300 per minute per share link and IP address, and all public requests to 1200 per minute per IP address.
