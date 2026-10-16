---
title: MCP Server
description: Connect Claude, ChatGPT, Cursor, VS Code and other AI clients to your ClickHouse through CH-UI's embedded Model Context Protocol server, with OAuth sign-in, scoped keys, server-side guardrails and a full audit trail
---

CH-UI embeds a [Model Context Protocol](https://modelcontextprotocol.io) server,
so AI clients (claude.ai, ChatGPT, Claude Code, Cursor, VS Code, Zed, and any
other MCP client) can work with your self-hosted ClickHouse through CH-UI,
with CH-UI's auth, guardrails and audit trail. No ClickHouse credentials on
laptops, no extra process: it is the same `ch-ui` binary, serving streamable
HTTP at `/mcp`.

What a client can do depends on the permission it was given. By default it
browses schemas and runs read-only queries. With write permission it can also
save queries and create dashboards and draft models and pipelines in CH-UI.
With the separate actions opt-in it can run existing models, start and stop
pipelines, and on Pro run scheduled jobs and send reports now.

:::note
The MCP server is part of the open-source core. The tools marked Pro in the
[tool reference](#tools) only appear when a Pro license is active.
:::

There are two ways to authenticate:

- **Sign in with OAuth** (recommended). The client sends you to CH-UI, you
  approve, and it gets a short-lived token tied to *you*: your connection,
  your ClickHouse grants, your name in the audit log. This is what claude.ai
  and ChatGPT require, and what Claude Code, Cursor and VS Code do when you
  add the server without a header.
- **An admin-created key** (`chm_...`). A bearer token bound to a dedicated
  ClickHouse user. Right for CI, scripts and shared access.

The server speaks the current MCP revision (2026-07-28, stateless) and the
earlier handshake-based revisions, so old and new clients both work.

## Permission levels

Every key and every OAuth grant has one of three levels. Each level includes
everything below it.

| Level | Key setting | OAuth scope | What it unlocks |
|---|---|---|---|
| Read | Access: **Read** (default) | `read` (default) | Every read tool: schema browsing, read-only queries, saved query runs, status of models and pipelines, telemetry, lineage, the client's own query history, and on Pro the operations tools |
| Read + write drafts | Access: **Read + write** | `read write` | Adds `save_query`, `create_dashboard`, `create_model` and `create_pipeline`. These only add entities to CH-UI's own store; nothing runs against ClickHouse |
| Actions | Read + write plus **Allow lifecycle actions** | `read write actions` | Adds `run_model`, `start_pipeline`, `stop_pipeline`, and on Pro `run_schedule_now` and `run_saved_report_now`. These run existing work now and can change data in ClickHouse |

A client never sees a tool its level does not allow: the tool is not
registered for that request, so it does not show up in `tools/list`.

- The actions opt-in is off by default, and off for every key created before
  it existed. It can only be set on a read + write key; the API refuses
  `allow_actions` with a read key.
- Over OAuth, `actions` is only valid together with `write`. Both are only
  granted to the CH-UI admin and analyst roles, and the person's role is
  checked again on every request: a demotion removes write and actions at
  once, without waiting for the token to be refreshed.
- Keep in mind what the two together allow: a client with actions can create
  a draft model with `create_model` and then build it with `run_model`. Give
  actions only to keys whose ClickHouse user may create those objects.

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
- The consent screen lists each requested scope separately: **Read**,
  **Create drafts** (`write`) and **Run actions** (`actions`). See
  [Permission levels](#permission-levels) for what each one unlocks.
- Access tokens last one hour and are refreshed silently by the client for up
  to 30 days of use. Each refresh token works once: of two requests racing
  with the same token, one gets new tokens and the other `invalid_grant`.
  Admins see and revoke grants in **Admin → MCP Server**.
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
  toggle to show revoked entries. Rotate or revoke from the row. Read + write
  keys show a `read + write` badge, and keys with lifecycle actions an
  `actions` badge next to it.
- **New key**: pick the connection, ClickHouse user, access level (Read or
  Read + write), the **Allow lifecycle actions** checkbox (shown for
  Read + write only), database allowlist and expiry. After you create the
  key, the form turns into a one-time reveal with the snippets already
  filled in.

## What a key is

An MCP key binds four things:

- **A connection**: which ClickHouse the tools talk to, through the existing
  agent tunnel, so firewalled instances work without exposing ports.
- **A ClickHouse user**: the credentials queries run with. **This is the real
  permission boundary.** Create a dedicated, locked-down ClickHouse user for
  MCP and grant it only what the AI should see. ClickHouse's own guidance for
  agent users is a role with `readonly=1`, `max_execution_time`,
  `max_memory_usage`, `max_rows_to_read` and `max_bytes_to_read` set. A key
  with lifecycle actions runs models and scheduled jobs as this user too, so
  that user then needs the grants those builds need.
- **A permission level**: read, read + write, or read + write with actions.
  See [Permission levels](#permission-levels).
- **An optional database allowlist**: a second layer on top of ClickHouse
  grants, not a replacement for them. See
  [Database allowlist](#database-allowlist).

Keys are bearer tokens with a `chm_` prefix. Only a SHA-256 hash is stored.
There is no unauthenticated mode. Each key is limited to 120 requests per
minute; over that the server answers `429` with `Retry-After`.

Creating, rotating and revoking a key is written to the
[audit log](/docs/audit-log) as `mcp.key.created`, `mcp.key.rotated` and
`mcp.key.revoked`, attributed to the person who did it (their SSO email
when they signed in with SSO, not the shared ClickHouse account).

Keys **expire**: 90 days by default when created from the UI, with 30 days,
1 year or never as the other options. They can be **rotated** in place:
rotation issues a new secret with the same connection, ClickHouse user,
permission level, allowlist and expiry, and revokes the old one in the same
transaction. An expired key gets `401` with an "expired" message, so the
client shows why.

## Tools

Every tool carries a title and the MCP annotations clients use to decide
whether to ask you before calling it:

- Read tools are `readOnlyHint: true` and `idempotentHint: true`.
- Write tools are `readOnlyHint: false, destructiveHint: false`: they only
  ever add entities to CH-UI.
- Action tools are `readOnlyHint: false`. `run_model` and `run_schedule_now`
  are `destructiveHint: true` (a build can replace a table, a saved query can
  be any statement). `stop_pipeline` is the only idempotent one.
  `start_pipeline` and `run_saved_report_now` are marked open-world, since
  they read from or deliver to systems outside CH-UI.

In the tables below, **Read** means every key and grant has the tool,
**Write** means read + write, and **Actions** means the lifecycle actions
opt-in. Paginated tools return `next_cursor` when more items exist; pass it
back as `cursor`. Cursors are opaque.

### Query and catalog

| Tool | Tier | Permission | What it does |
|---|---|---|---|
| `search_catalog` | Free | Read | Finds tables, columns, saved queries and dashboards whose name or comment contains a term. The first call to make when the model does not know where the data lives |
| `list_databases` | Free | Read | Databases visible to the connection |
| `list_tables` | Free | Read | Tables in a database with engine, rows and size. `like` filter, `page_size` (default 50, max 500), `cursor` |
| `describe_table` | Free | Read | Columns with types, comments and compressed/uncompressed size; engine, keys and the `CREATE TABLE` statement; active parts, partitions, last modification; `sample_rows` (default 3, max 20, 0 to skip) |
| `estimate_query` | Free | Read | `EXPLAIN ESTIMATE` for a SELECT: parts, rows and marks per table, totals, and a plain assessment. No data is read |
| `run_select` | Free | Read | Read-only SQL (SELECT, WITH, SHOW, DESCRIBE, EXISTS, EXPLAIN). `max_rows` (default 100, max 2000), `format` json or csv, `max_bytes` budget |
| `run_saved_query` | Free | Read | Runs a saved query by `id` or `name`, with `params` for its bind parameters. Same caps and guards as `run_select`. Reports whether the query is **verified** |
| `explain_query` | Free | Read | `EXPLAIN indexes = 1` plan for a SELECT |

`run_select` returns the rows, the column list with ClickHouse types, and the
server's `rows_read` / `bytes_read`. With `format: csv` the rows come as an
RFC 4180 block (header row, ClickHouse column order), which costs roughly half
the tokens of JSON for wide results; the metadata follows as a second text
block. Results are capped at `max_rows` exactly. `truncated: true` appears
only when ClickHouse had more rows than `max_rows` (CH-UI asks for one row
more than the cap to tell), and tells the model to filter or aggregate.

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

### Saved queries and dashboards

| Tool | Tier | Permission | What it does |
|---|---|---|---|
| `list_saved_queries` | Free | Read | Shared saved queries on this connection or bound to no connection, plus the ones this key saved. Paginated |
| `list_dashboards` | Free | Read | Dashboards in the instance. Dashboards are not bound to one connection. Paginated |
| `save_query` | Free | Write | Saves a query to the shared library, visible to every user of the instance |
| `create_dashboard` | Free | Write | Dashboard with SQL panels (table, stat, line, bar, area, pie), laid out two per row unless coordinates are given |

A key only sees shared saved queries and the ones it saved itself; private and
admin-only queries never reach an MCP client.

### Models and pipelines

| Tool | Tier | Permission | What it does |
|---|---|---|---|
| `list_models` | Free | Read | Models on this connection with status, materialization and target database. Paginated |
| `list_pipelines` | Free | Read | Pipelines on this connection with status. Paginated |
| `get_model_run` | Free | Read | A model run by `run_id` with per-model status, timing, test results and errors, or a model's current status and latest result by `model` |
| `pipeline_status` | Free | Read | A pipeline's status, last error, dead-letter rows, live throughput while running, and recent runs (`runs`, default 5, max 50) |
| `create_model` | Free | Write | Draft SQL model (view or table, `$ref()` supported). It does not build until it is run |
| `create_pipeline` | Free | Write | Draft pipeline: a Kafka, webhook, database, S3 or ingest source wired to a ClickHouse sink. It does not start until it is started |

Everything the write tools create is tagged `mcp:<key name>` as its creator,
and each create is audited (`saved_query.created`, `dashboard.created`,
`model.created`, `pipeline.created`). Models and pipelines are created as
drafts. Without the actions opt-in, a person reviews them and runs or starts
them from the UI.

### Observability

| Tool | Tier | Permission | What it does |
|---|---|---|---|
| `telemetry_sources` | Free | Read | The OpenTelemetry logs, traces and metrics sources configured on this connection, with their database and tables. Gives the `source_id` the telemetry tools take |
| `search_logs` | Free | Read | Log lines of a logs source in a time range (`since`, default `1h`, or `from` / `to`), newest first, with the Logs page search language plus `severity` and `services` filters. `limit` (default 50, max 500), `cursor` |
| `ingest_status` | Free | Read | Push pipelines (OTLP, Elasticsearch, Splunk HEC, webhook) on this connection: signal, status, target tables, last error, and since the server started the requests received, rejected by reason, rows written and spool backlog. Never returns ingest keys |
| `table_lineage` | Free | Read | Upstream and downstream of a table from ClickHouse metadata: tables, materialized views, views, dictionaries and external sources, with edge kinds. `direction` (`upstream`, `downstream`, `both`, the default), `depth` (default 3, max 10) |
| `search_traces` | Pro | Read | Traces of a traces source in a time range: root span and service, duration, span and error counts. Filters `service`, `operation`, `min_duration_ms`, `errors_only` and a search. `limit` (default 50, max 500), `cursor` |
| `get_trace` | Pro | Read | One trace by id: summary, spans in tree order (`max_spans`, default 200, max 500) and up to 50 correlated log lines |
| `service_map` | Pro | Read | Services and the calls between them over a time range, with spans, errors and p50/p95 latency |
| `list_metrics` | Pro | Read | Metrics in a metrics source over a time range: name, type, unit, services and series |
| `query_metrics` | Pro | Read | Bucketed series of one metric with an aggregation (avg, sum, min, max, last, count, rate, p50/p95/p99), split by up to 2 attributes |
| `list_monitors` | Pro | Read | Telemetry monitors on this connection with threshold, window and current state (ok or firing, last value, last error) |

### Operations

All operations tools are Pro and read-only. They show what a non-admin sees
in the UI, for the key's connection only.

| Tool | Tier | Permission | What it does |
|---|---|---|---|
| `firing_alerts` | Pro | Read | Incidents open right now: firing health checks, firing SQL alerts, failing ingest pipelines, with severity, start time and acknowledgement |
| `list_alerts` | Pro | Read | Health check thresholds with the nodes pending or firing, and the SQL alerts with state, last value and last run. Paginated |
| `alert_events` | Pro | Read | Alert events, newest first. `range` 1h, 6h, 24h (default), 7d or 30d; `limit` (default 50, max 500) |
| `list_schedules` | Pro | Read | Scheduled query jobs that run on the connection, with last status and next run. Paginated |
| `list_reports` | Pro | Read | Your own scheduled reports on the connection, with channels and last delivery status. No recipients. OAuth sign-ins only: reports belong to people, so an API key gets an empty list |
| `cluster_health` | Pro | Read | Live per-node replication, merges, mutations, parts pressure, long queries and disk usage |
| `query_insights_top` | Pro | Read | Top query patterns from `system.query_log` by p95 latency (`slow`), peak memory (`memory`) or run count (`frequent`) |
| `costs_summary` | Pro | Read | Cost Center spend summary priced with the connection's configured rates |

### History

| Tool | Tier | Permission | What it does |
|---|---|---|---|
| `query_history` | Free | Read | The `run_select` and `run_saved_query` calls this client already made on the connection, newest first, with status, error, elapsed time and rows. `range` (1h, 6h, 24h, 7d default, 30d), `status` ok or error, `limit` (default 50, max 500) |

An API key sees only its own queries, even when its ClickHouse user is shared
with other keys or with people in the editor. An OAuth sign-in sees the
person's MCP queries across refreshed tokens. Queries run in the CH-UI editor,
and other people's queries, are never included.

### Actions

| Tool | Tier | Permission | What it does |
|---|---|---|---|
| `run_model` | Free | Actions | Builds a model now as the key's ClickHouse user. A view is replaced; a table model (or any model with `full_refresh`) is rebuilt and swapped in, and the previous table and its rows are dropped; an incremental model adds rows. Only objects the model created are replaced. `scope` only (default), upstream or downstream |
| `start_pipeline` | Free | Actions | Starts a pipeline on this connection. It keeps writing until it is stopped. Pipelines with a SQLite source can only be started by an admin from the UI |
| `stop_pipeline` | Free | Actions | Stops a running pipeline. Its source position is kept, so starting it again resumes |
| `run_schedule_now` | Pro | Actions | Runs a scheduled job's saved query once, now, as the key's ClickHouse user, like **Run now** on the Schedules page |
| `run_saved_report_now` | Pro | Actions | Runs one of your own reports on this connection and delivers it to its channels, like **Send now** on the Reports page. OAuth sign-ins only: reports belong to people, so API keys are refused |

Models and scheduled jobs run with the key's own ClickHouse user and its
grants, without `readonly = 1` for the build itself but with the usual time
limits. If that user lacks a grant, the run fails with a message saying so.
Pipelines and reports run with their own stored credentials, as they do from
the UI.

## Database allowlist

A key can be limited to a list of databases. The allowlist is a second layer
on top of ClickHouse grants, not a replacement for them. Keys without an
allowlist are not affected by anything in this section.

How each group of tools treats it:

- **Schema tools**: `list_databases`, `search_catalog` and `telemetry_sources`
  leave out other databases. `list_tables`, `describe_table`, `table_lineage`
  and the telemetry tools refuse a database or source outside the list.
  `table_lineage` drops nodes in other databases, so a path never leads
  through them.
- **SQL tools**: `run_select`, `run_saved_query`, `explain_query` and
  `estimate_query` check the SQL itself. See
  [Allowlist on SQL](#allowlist-on-sql).
- **Models and pipelines**: `create_model` and `create_pipeline` refuse a
  target database outside the list. `get_model_run` leaves out models that
  build elsewhere. `pipeline_status`, `start_pipeline` and `stop_pipeline`
  refuse a pipeline whose sink writes outside the list. `run_model` refuses
  when any selected model builds into, or reads from, a database outside the
  list, and nothing runs. `ingest_status` leaves out pipelines writing
  elsewhere, and `list_monitors` leaves out monitors on sources in other
  databases.
- **Refused outright**: `query_insights_top` and `costs_summary` read
  `system.query_log` across every database, query text included, and
  `cluster_health` aggregates system tables across all databases, so they
  answer with a clear refusal instead of data. `run_schedule_now` and
  `run_saved_report_now` are refused too: a schedule or report runs SQL that
  cannot be checked against the list.
- **Operations views**: SQL alerts are listed only when their SQL reads
  tables inside the allowlist (written as `database.table`). Alert events
  are limited to health checks and those SQL alerts, ingest incidents are
  left out, and job and report errors are omitted.

### Allowlist on SQL

When a key has a database allowlist, CH-UI reads every statement before it
reaches ClickHouse and rejects it unless every table it reads is in an
allowed database. The check fails closed: if it cannot tell which database
something reads, the query is rejected with a message naming the reason.
Rejected:

- A table in a database outside the allowlist, anywhere in the statement:
  `FROM`, `JOIN`, comma joins, `IN` subqueries or `IN table`, CTEs, scalar
  subqueries, `UNION` branches.
- An unqualified table when the ClickHouse user's current database is not
  in the allowlist. Unqualified names resolve against that database, so
  write `database.table`.
- `system` and `information_schema` tables, unless listed. `system.one`,
  `system.numbers` and `system.zeros` are always allowed.
- Table functions other than `numbers`, `zeros`, `generate_series`,
  `values`, `generateRandom` and `null`: `url()`, `remote()`, `file()`,
  `s3()`, `cluster()`, `merge()`, `view()` and the rest are refused.
- `dictGet*`, `joinGet*` and `hasColumnInTable`, which read by a name given
  as a string.
- `SHOW`, `DESCRIBE`, `EXISTS` and `EXPLAIN` statements in `run_select`; use
  `list_tables`, `describe_table` and `explain_query` instead.
- SQL the check cannot read safely: nested `/* */` comments, `#` comments,
  `$$` strings, and non-ASCII characters outside string literals.

What the check does not see is a ClickHouse object that reads another
database by itself: a view, a `Distributed`, `Merge` or dictionary table
living in an allowed database. ClickHouse grants on the key's user stay the
boundary for those.

## Confirmation prompts and audit

Write and action tools ask before they do anything when the client supports
it. A client on MCP protocol 2026-07-28 or later that declares form
elicitation shows the user a yes/no prompt naming the action and its target,
for example:

- `Save query "weekly revenue" to CH-UI?`
- `Run model "daily" (view in analytics) in ClickHouse now as user "mcp_runner"? The view is replaced.`
- `Stop pipeline "events"? Ingestion pauses until it is started again.`

Nothing happens until the user confirms. A decline returns a plain "nothing
was written" result and is audited as `mcp.write.declined`. Clients on older
protocol versions, or without elicitation support, get no prompt and the
call runs, so the key's permission level remains the control there. The
server's instructions also tell the model to name the exact target to the
user before calling an action tool.

What lands in the [audit log](/docs/audit-log), under the key's name and
the person behind it (the OAuth subject, or the ClickHouse user for an API
key):

- `mcp.query.execute` for every `run_select` and `run_saved_query`, which also
  land in query history tagged `MCP`.
- `mcp.tool.call` for every other tool call, with the tool name and outcome
  (`ok`, `error`, or `awaiting_confirmation` while a prompt is open).
- `saved_query.created`, `dashboard.created`, `model.created` and
  `pipeline.created` for the write tools.
- `model.run`, `pipeline.started`, `pipeline.stopped`,
  `schedule.manual_run` and `report.sent_manual` for the action tools,
  including runs that failed.
- `mcp.write.declined` when the user says no to a prompt.

## Safety model

Layered, server-side, not prompt-side:

1. **ClickHouse grants** of the key's user: the hard boundary.
2. **`readonly = 1`** forced on every query the AI writes: no writes, no DDL,
   no `INTO OUTFILE`, and no setting changes from inside the SQL, so the caps
   below cannot be lifted by the query. A `SETTINGS` clause is rejected
   before it reaches ClickHouse.
3. **Statement gate**: anything that is not SELECT, WITH, SHOW, DESCRIBE,
   EXISTS or EXPLAIN is rejected before it reaches ClickHouse. Keys with a
   database allowlist get the [allowlist check](#allowlist-on-sql) on top.
4. **Caps**: `max_result_rows` (default 100, max 2000), a 60 s execution
   limit, and a 200 KB response cap on every tool, because results land in an
   LLM context, not a data pipeline. A client that disconnects or cancels
   mid-query has its query killed on the agent.
5. **Rate limit**: 120 requests per minute per key.
6. **Governance guardrails** (Pro): the same policies that gate the editor
   evaluate every `run_select` and `run_saved_query`, and `describe_table`
   withholds sample rows a policy blocks.
7. **Permission levels**: write and action tools are only registered for
   keys and grants that have them, and ask for confirmation where the client
   can.
8. **Observability**: every call is audited, as listed above.

## What MCP never exposes

There are no MCP tools for these, at any permission level:

- The audit log, and other admin-only views: incident timeline, governance
  incidents, access drift and operations reports.
- Other people's query history, and editor history in general.
  `query_history` only returns queries this MCP client ran.
- RBAC and ClickHouse access control: users, roles, grants, CH-UI roles.
- SSO configuration.
- Brain: chats, providers, skills and artifacts.
- Secrets: ClickHouse passwords, ingest keys, alert channel secrets and
  report recipients.
- Private and admin-only saved queries.

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
- A tool you expected is missing: check the key's permission level. Write
  tools need Read + write, action tools need **Allow lifecycle actions** too,
  and an OAuth grant needs the matching scope and an admin or analyst role.
- `401` with "expired": the key passed its expiry. Rotate it or create a new
  one.
- `429`: the key went over 120 requests per minute. The client should honour
  `Retry-After`.
- The server sends short instructions to the model at connect time (search
  the catalog first, describe before querying, use the sorting key, prefer
  verified saved queries, estimate before big scans, check `query_history`
  before repeating a query, start with `firing_alerts` for operational state).
  A good prompt on the client side still helps: tell the model which database
  matters.
