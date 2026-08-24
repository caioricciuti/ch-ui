# MCP server

CH-UI embeds a [Model Context Protocol](https://modelcontextprotocol.io) server,
so AI clients (Claude Code, claude.ai custom connectors, Cursor, and any other
MCP client) can browse schemas and run read-only queries against your
self-hosted ClickHouse — through CH-UI, with CH-UI's auth, guardrails, and
audit trail. No ClickHouse credentials on laptops, no extra process: it is the
same `ch-ui` binary, serving streamable HTTP at `/mcp`.

## Connect a client

Create a key in **Admin → MCP Server** (admin only). The key is shown once.
Then:

```bash
# Claude Code
claude mcp add --transport http ch-ui https://your-ch-ui.example.com/mcp \
  --header "Authorization: Bearer chm_..."
```

For claude.ai custom connectors or Cursor, use the same URL and an
`Authorization: Bearer chm_...` header.

## What a key is

An MCP key binds three things:

- **a connection** — which ClickHouse the tools talk to (through the existing
  agent/tunnel, so firewalled instances work without exposing ports)
- **a ClickHouse user** — the credentials queries run with. **This is the real
  permission boundary.** Create a dedicated, locked-down ClickHouse user for
  MCP and grant it only what the AI should see.
- **an optional database allowlist** — filters the schema-browsing tools
  (`list_databases`, `list_tables`, `describe_table`). It is a visibility
  filter, not a security boundary; use ClickHouse grants for enforcement.

Keys are bearer tokens (`chm_` prefix). Only a SHA-256 hash is stored; revoke
them any time in Settings. There is no unauthenticated mode.

## Tools

| Tool | What it does |
|---|---|
| `list_databases` | Databases visible to the connection (allowlist-filtered) |
| `list_tables` | Tables in a database with engine, rows, size |
| `describe_table` | Columns, types, comments, sorting/partition keys |
| `run_select` | Read-only SQL (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN), row-capped |
| `explain_query` | `EXPLAIN indexes = 1` plan for a SELECT |
| `query_insights_top` (Pro) | Top query patterns by p95 latency, memory, or frequency |
| `costs_summary` (Pro) | Cost Center spend summary with your configured rates |

## Safety model

Layered, server-side, not prompt-side:

1. **ClickHouse grants** of the key's user — the hard boundary.
2. **`readonly = 2`** forced on every query: no writes, no DDL, no
   `INTO OUTFILE`, settings changes limited to the query.
3. **Statement gate**: anything that isn't SELECT / WITH / SHOW / DESCRIBE /
   EXPLAIN is rejected before reaching ClickHouse.
4. **Caps**: `max_result_rows` (default 100, max 2000), 60s execution limit,
   200KB response cap — results land in an LLM context, not a data pipeline.
5. **Governance guardrails** (Pro): the same policies that gate the editor
   evaluate every MCP query.
6. **Observability**: every query lands in query history tagged `MCP` and in
   the audit log as `mcp.query.execute`. You can always see what the AI did.

## Notes

- MCP queries carry `log_comment = 'ch-ui:mcp'`, so you can slice them in
  `system.query_log` (and they are visible in Query Insights like any other
  workload).
- The connection's agent must be online; tools return a clear "offline" error
  otherwise.
- Pro tools appear only when a Pro license is active.
