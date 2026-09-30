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
| `table` | Tabular data display (default) |
| `stat` | Single metric / KPI card |
| `timeseries` | Line/area chart over time (via uplot) |
| `bar` | Bar chart (via uplot) |

## Panel Editor

Each panel is configured with:

| Field | Description | Default |
|---|---|---|
| Name | Panel title | Required |
| Panel Type | `table`, `stat`, `timeseries`, or `bar` | `table` |
| Query | SQL query (supports template variables) | Required |
| Config | Chart options (JSON): xColumn, yColumns, colors, legendPosition | `{}` |

### Chart Config

```json
{
  "chartType": "timeseries",
  "xColumn": "timestamp",
  "yColumns": ["value", "count"],
  "colors": ["#3b82f6", "#ef4444"],
  "legendPosition": "bottom"
}
```

## Layout

Panels are arranged on a **12-column grid**. Each panel stores its position:

| Field | Description | Default |
|---|---|---|
| `layout_x` | Column offset (0-11) | 0 |
| `layout_y` | Row offset | 0 |
| `layout_w` | Width in columns | 6 |
| `layout_h` | Height in rows | 4 |

Drag and resize panels in the UI to adjust layout. Positions are saved automatically.

## Time Range

A shared time-range selector applies to all panels in the dashboard.

### Presets

`1h`, `6h`, `24h`, `7d`, `30d`, or custom ISO timestamps.

Persisted in localStorage (`ch-ui-dashboard-time-range`, default `1h`).

### Template Variables

Panel queries support time-range macros that are interpolated before execution:

| Variable | Description | Example |
|---|---|---|
| `$__from` | Start timestamp (ISO 8601) | `2026-03-15T00:00:00Z` |
| `$__to` | End timestamp (ISO 8601) | `2026-03-15T01:00:00Z` |
| `$__interval` | Bucket interval | `1m`, `5m` |
| `$__rate` | Rate query modifier | None |

### Example Query

```sql
SELECT
  toStartOfInterval(timestamp, INTERVAL $__interval) AS ts,
  count() AS events
FROM default.events
WHERE timestamp BETWEEN '$__from' AND '$__to'
GROUP BY ts
ORDER BY ts
```

## Panel Query Execution

Panel queries use a dedicated endpoint with time-range interpolation:

```bash
POST /api/dashboards/query
```

```json
{
  "query": "SELECT ...",
  "timeout": 30,
  "time_range": { "from": "2026-03-15T00:00:00Z", "to": "2026-03-15T01:00:00Z" },
  "time_field": "timestamp",
  "max_data_points": 1000
}
```

Timeout max: 5 minutes. `max_data_points` controls downsampling (default 1000).

The response includes the interpolated query and the resolved variable values.

## Panel CRUD

```bash
# Create panel
POST /api/dashboards/{id}/panels
{
  "name": "Events per Hour",
  "panel_type": "timeseries",
  "query": "SELECT toStartOfHour(timestamp) AS ts, count() AS n FROM events WHERE timestamp > '$__from' GROUP BY ts ORDER BY ts",
  "config": "{\"xColumn\":\"ts\",\"yColumns\":[\"n\"]}",
  "layout_x": 0, "layout_y": 0, "layout_w": 12, "layout_h": 4
}

# Update panel
PUT /api/dashboards/{id}/panels/{panelId}
{ "name": "Updated Title", "query": "..." }

# Delete panel
DELETE /api/dashboards/{id}/panels/{panelId}
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

Public viewers load the dashboard at `/public/d/<token>`. Their panel queries execute through a token-scoped endpoint (`/api/public/dashboards/{token}/query`). They can only run the queries the dashboard's panels define, never arbitrary SQL, and they never see your connection credentials.
