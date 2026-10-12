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

A default "System Overview" dashboard is auto-created on first access.

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
| `timeseries` | Line chart over time |
| `bar` | Bar chart, grouped or stacked, over time, numbers or categories |
| `pie` | Pie or donut of a label column and a value column |
| `text` | Markdown text, no query |

Any other `panel_type` is refused with `400`.

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

`legendPosition` is `bottom` (default), `right` or `none`. Click a legend entry to hide or show that series. NULL values are drawn as gaps, not as zero.

Other keys: `barMode` (`grouped` or `stacked`), `gaugeMin` and `gaugeMax`, `pieLabelColumn`, `pieValueColumn` and `pieDonut`, `content` (Markdown for `text`), and for `stat` and `gauge`: `statField`, `statCalculation`, `statUnit`, `statPrefix`, `statSuffix`, `statDecimals`, `statColorMode`, `statThresholds`.

### Time zones

Panel queries run with `date_time_output_format=iso`, so DateTime values arrive as UTC instants and charts show them in the viewer's browser time zone. `Date` values have no time of day and are placed at local midnight, so each point keeps its calendar day.

## Layout

Panels are arranged on a **12-column grid**. Each panel stores its position:

| Field | Description | Default | Allowed |
|---|---|---|---|
| `layout_x` | Column offset | 0 | 0 or more, and `layout_x + layout_w` at most 12 |
| `layout_y` | Row offset | 0 | 0 to 10000 |
| `layout_w` | Width in columns | 6 | 1 to 12 |
| `layout_h` | Height in rows | 4 | 1 to 100 |

A position outside these bounds is refused with `400`. Drag and resize panels in the UI; positions are saved automatically. A new panel is placed below the existing ones. Below 640 px of width (phones), panels stack in one full-width column and cannot be dragged.

## Roles

Viewers can open dashboards, run their panels, change the time range and star dashboards. Adding, editing, moving and deleting panels, renaming, settings, import and sharing need the analyst or admin role, and the UI shows those controls only to them.

Panels run on the connection of your session. A panel saved on another connection says so in its header ("Built on X, running on Y").

## Time Range

A shared time-range selector applies to all panels in the dashboard: quick ranges (last 5 minutes to last 30 days), named ranges (today, yesterday, this or last week, this or last month, last 3 or 6 months) and an absolute range picked on a calendar, in UTC or the browser time zone.

The range is kept in the address bar as `?from=&to=` (for example `?from=now-6h&to=now`, or ISO timestamps for an absolute range; Grafana-style epoch milliseconds are accepted too), so a copied link opens on the same range. Without one, the last range you used is restored (`ch-ui-dashboard-time-range` in localStorage, default `1h`).

Relative values are a number and a unit: `s`, `m` (minutes), `h`, `d`, `w`, `M` (months) and `y`, optionally prefixed with `now-`. `M` and `m` are different: `1M` is one month, `1m` is one minute. Long forms such as `15min` or `2months` work too.

## Template Variables

Panel queries support these variables, replaced before the query runs:

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
  "max_data_points": 1000
}
```

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

**Settings → Export / Import** downloads a dashboard as JSON and loads a file into the JSON editor for review. **Apply** replaces the dashboard's name, description and panels:

```bash
POST /api/dashboards/{id}/import
{
  "version": 1,
  "dashboard": { "name": "Production Metrics", "description": "" },
  "panels": [
    { "name": "Queries", "panel_type": "timeseries", "query": "SELECT ...", "config": { "chartType": "timeseries" },
      "layout_x": 0, "layout_y": 0, "layout_w": 12, "layout_h": 4 }
  ]
}
```

Every panel is validated first (name, type, query, config, layout; at most 200 panels), and the swap runs in one transaction: a bad file is refused with `400` naming the panel, and the dashboard stays as it was. Recorded in the audit log as `dashboard.imported`.

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

Link options for wall screens and TVs:

| Parameter | Example | Effect |
|---|---|---|
| `from`, `to` | `?from=now-24h&to=now` | Time range, as in the app |
| `theme` | `?theme=light` | `dark` or `light`, for this page only |
| `refresh` | `?refresh=30s` | Auto refresh: seconds, `m` or `h`, at least 5 s. A refresh is skipped while the previous one is still running. |

Public panel results are cached in memory for 20 seconds, with time ranges rounded to 15 seconds, so many screens showing the same dashboard run each panel once. Queries that miss the cache are limited to 300 per minute per share link and IP address, and all public requests to 1200 per minute per IP address.
