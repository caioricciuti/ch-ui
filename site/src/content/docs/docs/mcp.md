---
title: MCP Server
description: Connect Claude, ChatGPT, Cursor, VS Code and other AI clients to your ClickHouse through CH-UI's embedded Model Context Protocol server, with OAuth sign-in, scoped keys, server-side guardrails and a full audit trail
---

CH-UI embeds a [Model Context Protocol](https://modelcontextprotocol.io) server,
so AI clients (claude.ai, ChatGPT, Claude Code, Cursor, VS Code, Zed, and any
other MCP client) can browse schemas and run read-only queries against your
self-hosted ClickHouse through CH-UI, with CH-UI's auth, guardrails and audit
trail. No ClickHouse credentials on laptops, no extra process: it is the same
`ch-ui` binary, serving streamable HTTP at `/mcp`.

:::note
The MCP server is part of the open-source core. Two tools,
`query_insights_top` and `costs_summary`, only appear when a Pro license is
active.
:::

There are two ways to authenticate:

- **Sign in with OAuth** (recommended). The client sends you to CH-UI, you
  approve, and it gets a short-lived token tied to *you*: your connection,
  your ClickHouse grants, your name in the audit log. This is what claude.ai
  and ChatGPT require, and what Claude Code, Cursor and VS Code do when you
  add the server without a header.
- **An admin-created key** (`chm_...`). A bearer token bound to a dedicated
  ClickHouse user. Right for CI, scripts and shared read-only access.

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
- **ChatGPT**: add the URL as an MCP server in developer mode.
- **Claude Code**: `claude mcp add --transport http ch-ui https://your-ch-ui.example.com/mcp`,
  then run `/mcp` and choose *Authenticate*.
- **Cursor, VS Code, Zed, Devin**: add the URL without a `headers` block; the
  client offers to sign in.

What the person approving needs to know:

- The token runs queries **as them**: their connection, their ClickHouse user
  (or the SSO service account), their guardrails. Every call is audited under
  their name (`mcp.oauth.consent`, then the usual `mcp.query.execute` and
  `mcp.tool.call` rows).
- Scope `read` is the default. Scope `write` (draft creation in CH-UI) is only
  granted to the admin and analyst roles.
- Access tokens last one hour and are refreshed silently by the client for up
  to 30 days of use. Admins see and revoke grants in **Admin → MCP Server**.
- CH-UI is the authorization server; nothing leaves your deployment. It
  supports authorization code with PKCE (S256, mandatory), refresh token
  rotation, Dynamic Client Registration for public clients, and Client ID
  Metadata Documents (fetched over https only, no redirects, private
  addresses refused). Metadata lives at
  `/.well-known/oauth-protected-resource` and
  `/.well-known/oauth-authorization-server`.

## Connect a client with a key

Create a key in **Admin → MCP Server** (admin only). The key is shown once.
Keep it out of shell history and config files where you can: every snippet
below reads it from the `CH_UI_MCP_KEY` environment variable or a prompt.

### Claude Code

```bash
claude mcp add --transport http ch-ui https://your-ch-ui.example.com/mcp \
  --header "Authorization: Bearer ${CH_UI_MCP_KEY}"
```

Or commit a project-level `.mcp.json`. The variable is expanded at load time,
so the key itself never lands in the file:

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
and keep it in its secret storage:

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

### Zed, Devin Desktop (Windsurf), Gemini / Antigravity CLI

All of them take a URL plus a `headers` map; use the same
`Authorization: Bearer chm_...` header as above. Check the client's docs for
the file location.

### claude.ai, Claude Desktop, ChatGPT

These accept OAuth only; use the OAuth section above. (claude.ai's org-scoped
`static_headers` beta also works with a key if your organization enabled it.)

## Claude Code plugin

The repository ships a Claude Code plugin that bundles the server connection
and a `clickhouse-analytics` skill. The skill teaches Claude the schema-first
workflow in depth: search the catalog, describe before querying, estimate
before scanning, prefer verified saved queries, and the ClickHouse SQL
details that trip models up.

```bash
export CH_UI_URL=https://your-ch-ui.example.com
export CH_UI_MCP_KEY=chm_...   # from Admin → MCP Server

claude plugin marketplace add caioricciuti/ch-ui
claude plugin install ch-ui@ch-ui
```

The plugin's server entry reads both variables at load time, so the key never
lands in a config file. Any MCP client can use the server without the plugin.

## Admin → MCP Server

The admin page for the server shows:

- Four counters: active keys, OAuth grants, keys expiring in the next 7 days,
  and keys used in the last 24 hours.
- The endpoint URL with a copy button and ready-made config snippets for
  Claude Code, Cursor, VS Code and a generic client.
- One table of **API keys** and one of **OAuth grants**, with search and a
  toggle to show revoked entries. Rotate or revoke from the row.
- **New key**: pick the connection, ClickHouse user, scope, database allowlist
  and expiry. After you create the key, the form turns into a one-time reveal
  with the snippets already filled in.

## What a key is

An MCP key binds three things:

- **A connection**: which ClickHouse the tools talk to, through the existing
  agent tunnel, so firewalled instances work without exposing ports.
- **A ClickHouse user**: the credentials queries run with. **This is the real
  permission boundary.** Create a dedicated, locked-down ClickHouse user for
  MCP and grant it only what the AI should see. ClickHouse's own guidance for
  agent users is a role with `readonly=1`, `max_execution_time`,
  `max_memory_usage`, `max_rows_to_read` and `max_bytes_to_read` set.
- **An optional database allowlist**: filters the schema-browsing tools
  (`list_databases`, `list_tables`, `describe_table`) and the target database
  of `create_model` / `create_pipeline`. It does **not** parse the SQL passed
  to `run_select` or `explain_query`. It is a visibility filter, not a
  security boundary; use ClickHouse grants for enforcement.

Keys are bearer tokens with a `chm_` prefix. Only a SHA-256 hash is stored.
There is no unauthenticated mode. Each key is limited to 120 requests per
minute; over that the server answers `429` with `Retry-After`.

Keys **expire**: 90 days by default when created from the UI, with 30 days,
1 year or never as the other options. They can be **rotated** in place:
rotation issues a new secret with the same connection, ClickHouse user,
scope, allowlist and expiry, and revokes the old one in the same transaction.
An expired key gets `401` with an "expired" message, so the client shows why.

Each key has a **scope**: `read` (default) or `read + write`. Write scope adds
tools that create CH-UI entities (saved queries, dashboards, draft models and
pipelines) in CH-UI's own store. It never lets the AI write ClickHouse data,
and a read key never even sees the write tools.

## Tools

Every tool carries a title and the MCP annotations clients use to decide
whether to ask you before calling it: read tools are `readOnlyHint: true`,
the write-scope tools are `readOnlyHint: false, destructiveHint: false`
(they only ever add drafts).

| Tool | What it does |
|---|---|
| `search_catalog` | Finds tables, columns, saved queries and dashboards whose name or comment contains a term. The first call to make when the model does not know where the data lives |
| `list_databases` | Databases visible to the connection (allowlist-filtered) |
| `list_tables` | Tables in a database with engine, rows and size. `like` filter, `page_size` (default 50, max 500), `cursor` |
| `describe_table` | Columns with types, comments and compressed/uncompressed size; engine, keys and the `CREATE TABLE` statement; active parts, partitions, last modification; `sample_rows` (default 3, max 20, 0 to skip) |
| `estimate_query` | `EXPLAIN ESTIMATE` for a SELECT: parts, rows and marks per table, totals, and a plain assessment. No data is read |
| `run_select` | Read-only SQL (SELECT / WITH / SHOW / DESCRIBE / EXPLAIN). `max_rows` (default 100, max 2000), `format` json or csv, `max_bytes` budget |
| `run_saved_query` | Runs a saved query by `id` or `name`, with `params` for its bind parameters. Reports whether the query is **verified** |
| `explain_query` | `EXPLAIN indexes = 1` plan for a SELECT |
| `list_saved_queries` / `list_dashboards` / `list_models` / `list_pipelines` | What already exists on this connection, paginated (`page_size`, `cursor`) |
| `save_query` (write scope) | Saves a query to the shared library |
| `create_dashboard` (write scope) | Dashboard with SQL panels, laid out automatically |
| `create_model` (write scope) | Draft SQL model (view or table, `$ref()` supported) |
| `create_pipeline` (write scope) | Draft pipeline: a Kafka, webhook, database or S3 source wired to a ClickHouse sink |
| `query_insights_top` (Pro) | Top query patterns by p95 latency, memory or frequency |
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
first, then `run_select` with a budget sized from the estimate.

**Verified saved queries.** In [Saved Queries](/docs/querying#saved-queries),
mark a query as verified once a human has confirmed the SQL is correct.
`run_saved_query`, `search_catalog` and `list_saved_queries` surface the flag,
and the server's instructions tell the model to prefer verified queries over
writing new SQL for the same question. Verification is a review mark, not a
permission: unverified saved queries still run.

Write tools create **drafts** tagged `mcp:<key name>`. Models and pipelines
never run from MCP; you review them and press play in the UI. Every create is
audit-logged.

## Safety model

Layered, server-side, not prompt-side:

1. **ClickHouse grants** of the key's user: the hard boundary.
2. **`readonly = 2`** forced on every query: no writes, no DDL, no
   `INTO OUTFILE`, settings changes limited to the query.
3. **Statement gate**: anything that is not SELECT / WITH / SHOW / DESCRIBE /
   EXPLAIN is rejected before it reaches ClickHouse.
4. **Caps**: `max_result_rows` (default 100, max 2000), a 60 s execution
   limit, and a 200 KB response cap on every tool, because results land in an
   LLM context, not a data pipeline. A client that disconnects or cancels
   mid-query has its query killed on the agent.
5. **Rate limit**: 120 requests per minute per key.
6. **Governance guardrails** (Pro): the same policies that gate the editor
   evaluate every `run_select`.
7. **Observability**: every `run_select` lands in query history tagged `MCP`
   and in the [audit log](/docs/audit-log) as `mcp.query.execute`; every other
   tool call is audited as `mcp.tool.call` with the tool name and outcome.

Write tools (`save_query`, `create_dashboard`, `create_model`,
`create_pipeline`) ask before they write when the client supports it. A
client on MCP protocol 2026-07-28 or later that declares form elicitation
shows the user a yes/no prompt naming the action and its target, for
example `Save query "weekly revenue" to CH-UI?`. Nothing is written until
the user confirms; a decline returns a plain "nothing was written" result
and is audited as `mcp.write.declined`. Clients on older protocol versions,
or without elicitation support, get no prompt and the write runs as before,
so the `read_write` scope on the key remains the control there.

## Behind a reverse proxy

`/mcp`, `/oauth/*` and `/.well-known/*` are ordinary HTTP routes, so the
`location /` block from the [deployment guide](/docs/deployment#5-reverse-proxy-nginx)
already covers them. Two things matter:

- Forward `X-Forwarded-Proto` and `X-Forwarded-Host` (or set
  [`app_url`](/docs/configuration)) so the OAuth metadata advertises the
  public origin instead of `127.0.0.1`.
- Run v2.10.1 or later. On v2.10.0, a proxy that connects to a loopback
  listener with a public `Host` header got `403 Forbidden: invalid Host
  header`, which clients reported as an auth failure. The workaround there is
  `MCPGODEBUG=disablelocalhostprotection=1` on the CH-UI process.

## Notes and troubleshooting

- MCP queries carry `log_comment = 'ch-ui:mcp'`, so you can slice them in
  `system.query_log`. They show up in [Query Insights](/docs/query-insights)
  like any other workload.
- The connection's agent must be online; tools return a clear "offline" error
  otherwise.
- Pro tools appear only when a Pro license is active.
- `401` with "expired": the key passed its expiry. Rotate it or create a new
  one.
- `429`: the key went over 120 requests per minute. The client should honour
  `Retry-After`.
- The server sends short instructions to the model at connect time (search
  the catalog first, describe before querying, use the sorting key, prefer
  verified saved queries, estimate before big scans). A good prompt on the
  client side still helps: tell the model which database matters.
