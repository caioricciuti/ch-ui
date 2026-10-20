---
title: Run your first query
description: Sign in, pick a connection, find a table in the explorer, run SQL and save the query for later
---

This guide takes you from the sign-in page to a saved query. It assumes
CH-UI is already running and can reach a ClickHouse server. If it is not,
start with [Installation](/docs/installation).

## Sign in

1. Open CH-UI in your browser, for example `http://localhost:3488`.
2. Under **Connection**, pick the ClickHouse server you want to query.
3. Type your ClickHouse **Username** and **Password**. CH-UI sends them
   straight to ClickHouse; it does not keep its own passwords.
4. Click **Connect**.

If your team uses single sign-on, click **Sign in with SSO** instead. If the
page refuses you, see [Can't log in](/docs/cant-login).

## Open a query tab

You land on **Home**. Open an editor in any of these ways:

- Click **Run Query** in the page header.
- Press **⌥N** (Mac) or **Alt+N**.
- Click the **+** button at the end of the tab bar.

To switch to another ClickHouse server later, click the status dot near the
bottom of the rail on the left.

## Find a table

1. Click the **Query** icon (a terminal) at the top of the rail. The schema
   explorer opens next to it, listing your databases.
2. Expand a database to see its tables.
3. Click a table to open it in its own tab with a preview of its rows, or open
   the table's actions menu and pick **Query Table**. That opens a query tab
   with `SELECT * FROM <table> LIMIT 1000` ready to run.

## Run it

1. Write or edit your SQL in the editor.
2. Click **Run**, or press **⌘↵** (Mac) or **Ctrl+Enter**.
3. The results appear under the editor, with the row count and duration in
   the result tab. Click a column header to sort, or use the footer to export
   the rows as CSV, JSON, Parquet and more.

When the editor holds several statements, **Run** runs the one under the
cursor. **⇧⌘↵** or **Shift+Ctrl+Enter** runs them all, each in its own result
tab.

## Save it

1. Click **Save** in the editor toolbar, or press **⌘S** or **Ctrl+S**.
2. Give the query a **Name**. A description and a folder are optional.
3. Pick who sees it under **Visible to**: **Everyone** or **Only me**.
4. Click **Save**.

The query now shows on the **Saved Queries** page (Explore in the rail). Open
it from there to run it again. Saved queries also feed
[dashboards](/docs/guides/first-dashboard), schedules and alerts.

## Next

- [Build your first dashboard](/docs/guides/first-dashboard) from this query.
- [SQL Workspace](/docs/querying) covers every editor feature: parameters,
  history, Explain, uploads and more.
