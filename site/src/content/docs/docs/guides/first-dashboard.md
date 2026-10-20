---
title: Build your first dashboard
description: Create a dashboard, add a panel from a saved query, arrange the layout and share a link
---

A dashboard is a grid of panels, and each panel runs one SQL query. The
fastest start is a query you already saved. If you have none yet,
[run and save one first](/docs/guides/first-query).

You need the analyst or admin role to create and edit dashboards. Viewers can
open and run them.

## Create the dashboard

1. Click **Visualize** in the rail, then **Dashboards**.
2. Click **New dashboard**.
3. Type a **Name**. Description, folder and tags are optional.
4. Click **Create dashboard**. The new, empty dashboard opens.

No idea where to begin? **New from template**, next to **New dashboard**,
creates a ready-made dashboard over ClickHouse's own system tables.

## Add a panel from a saved query

1. Click **Add Panel** in the header (or **Add panel** on the empty grid).
2. Under **Start from a saved query**, pick your query. CH-UI copies its SQL
   into the panel, fills in the name and description, and runs a preview.
3. Pick a visualization on the right: Table, Stat, Gauge, Time Series, Bar,
   Pie, Histogram, Scatter, Heatmap or Text. Set the columns it asks for.
4. Click **Create Panel**.

The SQL is copied, not linked: editing the saved query later does not change
the panel. You can also skip the picker and write SQL straight into the
panel's editor.

From the SQL editor there is a shortcut too: chart a result, then use **Add
to dashboard**. See [Chart a result](/docs/querying#chart-a-result).

## Arrange the layout

- Drag a panel by its header to move it.
- Drag its bottom-right corner to resize it.
- Use the time range picker in the header to change the window for every
  panel at once.

Positions save as you go; there is no separate save button.

## Share it

1. Click **Share** in the dashboard header.
2. Pick **Public** for a link anyone can open without signing in, or
   **Invite Only** to send magic links to specific emails through your
   configured email channel.
3. Click **Create Share Link**, then copy the link from the list below.

Revoke a link from the same sheet to cut access at once. Teammates who sign in
to CH-UI do not need a link: the dashboard is already in their Dashboards list.

## Next

[Dashboards](/docs/dashboards) covers variables, time macros, drilldown
links, version history and the public link options for wall screens.
