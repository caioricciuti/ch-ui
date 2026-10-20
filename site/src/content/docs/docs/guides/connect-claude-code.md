---
title: Connect Claude Code
description: Add CH-UI as an MCP server in Claude Code and sign in as yourself, so Claude can browse your schema and run read-only queries
---

CH-UI serves an MCP endpoint at `/mcp`. Add it to Claude Code with the URL
and nothing else, sign in in your browser, and Claude works with your
ClickHouse as you: your connection, your grants, your name in the audit log.
No key to create or paste. This needs CH-UI v2.28.1 or later.

## Add the server

1. Find your CH-UI address, for example `https://ch-ui.example.com`. Admins
   can copy the exact endpoint from **Admin**, **MCP Server**, under
   **Connect a client as yourself**.
2. In a terminal, run:

   ```bash
   claude mcp add --transport http ch-ui https://ch-ui.example.com/mcp
   ```

## Sign in

1. Start Claude Code and run `/mcp`.
2. Pick **ch-ui** and choose **Authenticate**. Your browser opens CH-UI.
3. Sign in to CH-UI if you are not signed in already.
4. The consent page lists what Claude Code asks for: **Read**, **Create
   drafts** and **Run actions**. Click **Allow access**.
5. Go back to Claude Code. `/mcp` now shows ch-ui as connected.

What you grant follows your CH-UI role. A viewer grants **Read** only, and the
consent page marks the rest as not granted. Admins and analysts grant all
three. See [Permission levels](/docs/mcp#permission-levels).

## Try it

Ask Claude something about your data, for example "Which tables in the
analytics database are the largest?". It searches the catalog, describes the
tables and runs read-only `SELECT`s. Every call shows up in your query history
and the audit log.

## Manage access

- The token lasts an hour and Claude Code refreshes it on its own for up to
  30 days of use. After that, run `/mcp` and **Authenticate** again.
- Admins see every grant under **Admin**, **MCP Server**, **OAuth grants**,
  and can revoke one from its row.

## For CI and scripts

Automation cannot open a browser. An admin creates a service key in **Admin**,
**MCP Server** instead, and the client sends it as a bearer header. See
[Connect a client with a key](/docs/mcp#connect-a-client-with-a-key), and the
[Claude Code plugin](/docs/mcp#claude-code-plugin) for a ready-made setup.
