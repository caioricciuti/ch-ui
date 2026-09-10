# MCP server

CH-UI embeds a [Model Context Protocol](https://modelcontextprotocol.io) server,
so AI clients (Claude Code, Cursor, VS Code, Zed, and any other MCP client that
can send a bearer header) can browse schemas and run read-only queries against
your self-hosted ClickHouse — through CH-UI, with CH-UI's auth, guardrails, and
audit trail. No ClickHouse credentials on laptops, no extra process: it is the
same `ch-ui` binary, serving streamable HTTP at `/mcp`.

The server speaks the current MCP revision (2026-07-28, stateless) and the
earlier handshake-based revisions, so old and new clients both work.

## Connect a client

Create a key in **Admin → MCP Server** (admin only). The key is shown once.
Keep it out of shell history and config files where you can: every snippet
below reads it from the `CH_UI_MCP_KEY` environment variable or a prompt.

### Claude Code

```bash
claude mcp add --transport http ch-ui https://your-ch-ui.example.com/mcp \
  --header "Authorization: Bearer ${CH_UI_MCP_KEY}"
```

Or commit a project-level `.mcp.json` (the variable is expanded at load time,
the key itself never lands in the file):

```json
{
  "mcpServers": {
    "ch-ui": {
      "type": "http",
      "url": "https://your-ch-ui.example.com/mcp",
      "headers": { "Authorization": "Bearer ${CH_UI_MCP_KEY}" }
    }
  }
}
```

### Cursor

`~/.cursor/mcp.json` (global) or `.cursor/mcp.json` (project):

```json
{
  "mcpServers": {
    "ch-ui": {
      "url": "https://your-ch-ui.example.com/mcp",
      "headers": { "Authorization": "Bearer ${env:CH_UI_MCP_KEY}" }
    }
  }
}
```

### VS Code / GitHub Copilot

`.vscode/mcp.json`. The `inputs` block makes VS Code prompt for the key once
and store it in its secret storage:

```json
{
  "inputs": [
    { "id": "ch-ui-key", "type": "promptString", "description": "CH-UI MCP key", "password": true }
  ],
  "servers": {
    "ch-ui": {
      "type": "http",
      "url": "https://your-ch-ui.example.com/mcp",
      "headers": { "Authorization": "Bearer ${input:ch-ui-key}" }
    }
  }
}
```

### Zed, Devin Desktop (Windsurf), Gemini/Antigravity CLI

All take a URL plus a `headers` map; use the same
`Authorization: Bearer chm_...` header as above. Check the client's docs for
the file location.

### claude.ai and Claude Desktop

Custom connectors in claude.ai require OAuth. The only header-based option is
the org-scoped `static_headers` beta, which an organization admin has to
enable; if you have it, add the same `Authorization` header there. Per-user
OAuth login for CH-UI's MCP server is planned; until then, claude.ai users
should use Claude Code.

### ChatGPT

ChatGPT plugins (formerly apps/connectors) accept OAuth only, so CH-UI's MCP
server cannot be added to ChatGPT yet.

## What a key is

An MCP key binds three things:

- **a connection** — which ClickHouse the tools talk to (through the existing
  agent/tunnel, so firewalled instances work without exposing ports)
- **a ClickHouse user** — the credentials queries run with. **This is the real
  permission boundary.** Create a dedicated, locked-down ClickHouse user for
  MCP and grant it only what the AI should see. ClickHouse's own guidance for
  agent users is a role with `readonly=1`, `max_execution_time`,
  `max_memory_usage`, `max_rows_to_read` and `max_bytes_to_read` set.
- **an optional database allowlist** — filters the schema-browsing tools
  (`list_databases`, `list_tables`, `describe_table`) and the target database
  of `create_model` / `create_pipeline`. It does **not** parse the SQL passed
  to `run_select` or `explain_query`; it is a visibility filter, not a
  security boundary. Use ClickHouse grants for enforcement.

Keys are bearer tokens (`chm_` prefix). Only a SHA-256 hash is stored; revoke
them any time in Admin. There is no unauthenticated mode. Each key is limited
to 120 requests per minute; over that the server answers `429` with
`Retry-After`.

Each key has a **scope**: `read` (default) or `read + write`. Write scope adds
tools that create CH-UI entities (saved queries, dashboards, draft models and
pipelines) in CH-UI's own store — it never lets the AI write ClickHouse data,
and a read key never even sees the write tools.

## Tools

Every tool carries a title and the MCP annotations clients use to decide
whether to ask you before calling it: read tools are `readOnlyHint: true`,
the write-scope tools are `readOnlyHint: false, destructiveHint: false`
(they only ever add drafts).

| Tool | What it does |
|---|---|
| `list_databases` | Databases visible to the connection (allowlist-filtered) |
| `list_tables` | Tables in a database with engine, rows, size. `like` filter, `page_size` (default 50, max 500), `cursor` |
| `describe_table` | Columns, types, comments, sorting/partition keys |
| `run_select` | Read-only SQL (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN). `max_rows` (default 100, max 2000), `format` json or csv |
| `explain_query` | `EXPLAIN indexes = 1` plan for a SELECT |
| `list_saved_queries` / `list_dashboards` / `list_models` / `list_pipelines` | What already exists on this connection, paginated (`page_size`, `cursor`) |
| `save_query` (write scope) | Save a query to the shared library |
| `create_dashboard` (write scope) | Dashboard with SQL panels, auto-laid-out |
| `create_model` (write scope) | Draft SQL model (view or table, `$ref()` supported) |
| `create_pipeline` (write scope) | Draft pipeline: kafka/webhook/database/s3 source wired to a ClickHouse sink |
| `query_insights_top` (Pro) | Top query patterns by p95 latency, memory, or frequency |
| `costs_summary` (Pro) | Cost Center spend summary with your configured rates |

Paginated tools return `next_cursor` when more items exist; pass it back as
`cursor`. Cursors are opaque.

`run_select` returns the rows, the column list with ClickHouse types, and the
server's `rows_read` / `bytes_read`. With `format: csv` the rows come as an
RFC 4180 block (header row, ClickHouse column order), which costs roughly half
the tokens of JSON for wide results; the metadata follows as a second text
block. Results are capped at `max_rows` exactly, and `truncated: true` tells
the model to filter or aggregate.

Write tools create **drafts** tagged `mcp:<key name>`: models and pipelines
never run from MCP — you review and press play in the UI. Every create is
audit-logged.

## Safety model

Layered, server-side, not prompt-side:

1. **ClickHouse grants** of the key's user — the hard boundary.
2. **`readonly = 2`** forced on every query: no writes, no DDL, no
   `INTO OUTFILE`, settings changes limited to the query.
3. **Statement gate**: anything that isn't SELECT / WITH / SHOW / DESCRIBE /
   EXPLAIN is rejected before reaching ClickHouse.
4. **Caps**: `max_result_rows` (default 100, max 2000), 60s execution limit,
   200KB response cap on every tool — results land in an LLM context, not a
   data pipeline. A client that disconnects or cancels mid-query has its query
   killed on the agent.
5. **Rate limit**: 120 requests per minute per key.
6. **Governance guardrails** (Pro): the same policies that gate the editor
   evaluate every `run_select`.
7. **Observability**: every `run_select` lands in query history tagged `MCP`
   and in the audit log as `mcp.query.execute`; every other tool call is
   audited as `mcp.tool.call` with the tool name and outcome. You can always
   see what the AI did.

## Notes

- MCP queries carry `log_comment = 'ch-ui:mcp'`, so you can slice them in
  `system.query_log` (and they are visible in Query Insights like any other
  workload).
- The connection's agent must be online; tools return a clear "offline" error
  otherwise.
- Pro tools appear only when a Pro license is active.
- The server sends short instructions to the model at connect time (work
  schema-first, use the sorting key, aggregate rather than page). Good prompts
  on the client side still help: tell the model which database matters.
