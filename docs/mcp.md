# MCP server

CH-UI embeds a [Model Context Protocol](https://modelcontextprotocol.io) server,
so AI clients (claude.ai, ChatGPT, Claude Code, Cursor, VS Code, Zed, and any
other MCP client) can browse schemas and run read-only queries against your
self-hosted ClickHouse — through CH-UI, with CH-UI's auth, guardrails, and
audit trail. No ClickHouse credentials on laptops, no extra process: it is the
same `ch-ui` binary, serving streamable HTTP at `/mcp`.

Two ways to authenticate:

- **Sign in with OAuth** (recommended): the client sends you to CH-UI, you
  approve, and it gets a short-lived token tied to *you*: your connection,
  your ClickHouse grants, your name in the audit log. This is what claude.ai
  and ChatGPT require, and what Claude Code, Cursor and VS Code do when you
  add the server without a header.
- **An admin-created key** (`chm_...`): a bearer token bound to a dedicated
  ClickHouse user. Right for CI, scripts, and shared read-only access.

The server speaks the current MCP revision (2026-07-28, stateless) and the
earlier handshake-based revisions, so old and new clients both work.

## Connect a client with OAuth

Give the client the URL `https://your-ch-ui.example.com/mcp` and nothing
else. It discovers CH-UI's authorization server from the `401` response,
registers itself (or presents a Client ID Metadata Document), and opens the
consent page in your browser. Sign in to CH-UI if you are not already, review
what the client asks for, and click **Allow access**.

- **claude.ai / Claude Desktop**: Settings → Connectors → Add custom
  connector → paste the URL. No client id or secret needed.
- **ChatGPT**: add the URL as an MCP server in developer mode / plugins.
- **Claude Code**: `claude mcp add --transport http ch-ui https://your-ch-ui.example.com/mcp`
  then `/mcp` and choose *Authenticate*.
- **Cursor, VS Code, Zed, Devin**: add the URL without a `headers` block; the
  client offers to sign in.

What the person approving needs to know:

- The token runs queries **as them**: their connection, their ClickHouse user
  (or the SSO service account), their guardrails. Every call is audited under
  their name (`mcp.oauth.consent`, then the usual `mcp.query.execute` and
  `mcp.tool.call` rows).
- Scope `read` is the default. Scope `write` (draft creation in CH-UI) is only
  granted to admin and analyst roles.
- Access tokens last one hour and are refreshed silently by the client for up
  to 30 days of use. Admins see and revoke grants in **Admin → MCP Server**
  (badge `oauth`).
- CH-UI is the authorization server; nothing leaves your deployment. It
  supports authorization code + PKCE (S256, mandatory), refresh token
  rotation, Dynamic Client Registration for public clients, and Client ID
  Metadata Documents (fetched over https only, no redirects, private
  addresses refused). Metadata lives at
  `/.well-known/oauth-protected-resource` and
  `/.well-known/oauth-authorization-server`.
- Behind a reverse proxy, forward `X-Forwarded-Proto` and `X-Forwarded-Host`
  (or set `app_url`) so the metadata advertises the public origin.

## Connect a client with a key

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

### claude.ai, Claude Desktop, ChatGPT

These accept OAuth only; use the OAuth section above. (claude.ai's org-scoped
`static_headers` beta also works with a key if your organization enabled it.)

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

Keys **expire** (default 90 days when created from the UI; 30 days, 1 year,
or never are the other options) and can be **rotated** in place: rotation
issues a new secret with the same connection, ClickHouse user, scope,
allowlist and expiry, and revokes the old one in the same transaction. An
expired key gets `401` with an "expired" message so the client shows why.

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
| `search_catalog` | Find tables, columns, saved queries and dashboards whose name or comment contains a term. The first call to make when the model does not know where the data lives |
| `list_databases` | Databases visible to the connection (allowlist-filtered) |
| `list_tables` | Tables in a database with engine, rows, size. `like` filter, `page_size` (default 50, max 500), `cursor` |
| `describe_table` | Columns with types, comments, compressed/uncompressed size; engine, keys, `CREATE TABLE` statement; active parts, partitions, last modification; `sample_rows` (default 3, max 20, 0 to skip) |
| `estimate_query` | `EXPLAIN ESTIMATE` for a SELECT: parts, rows and marks per table, totals, and a plain assessment. No data is read |
| `run_select` | Read-only SQL (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN). `max_rows` (default 100, max 2000), `format` json or csv, `max_bytes` budget |
| `run_saved_query` | Run a saved query by `id` or `name` with `params` for its `{name:Type}` parameters. Reports whether the query is **verified** |
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

**Byte budget.** `max_bytes` on `run_select` and `run_saved_query` maps to
ClickHouse's `max_bytes_to_read`: the query is aborted once it has read more
uncompressed bytes than the budget. The intended loop is `estimate_query`
first, then `run_select` with a budget sized from the estimate. Nothing else
in the ClickHouse MCP space offers a pre-flight estimate today.

**Verified saved queries.** In Saved Queries, mark a query as verified once a
human has confirmed the SQL is correct. `run_saved_query`, `search_catalog`
and `list_saved_queries` surface the flag, and the server's instructions
tell the model to prefer verified queries over writing new SQL for the same
question. Verification is a review mark, not a permission: unverified saved
queries still run.

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
- The server sends short instructions to the model at connect time (search
  the catalog first, describe before querying, use the sorting key, prefer
  verified saved queries, estimate before big scans). Good prompts on the
  client side still help: tell the model which database matters.
- For Claude Code there is a plugin with a ClickHouse analytics skill that
  teaches the model this workflow in more depth; see
  `integrations/claude-code-plugin/README.md`.
