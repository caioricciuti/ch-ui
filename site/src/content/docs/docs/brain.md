---
title: Brain (AI Assistant)
description: "Schema-aware AI copilot for ClickHouse: bring your own model (Anthropic, OpenAI or local), with custom skills, approvals, usage tracking and query result artifacts."
---

Brain is a built-in AI copilot that understands your ClickHouse schema and writes executable, ClickHouse-idiomatic SQL. It supports persisted multi-chat history, streaming responses, **skills** (reusable expertise applied to every chat), and **query result artifacts** (tables you can view as a chart, re-run from the SQL block, or open in the editor).

## Powering Brain

Brain runs on **your own LLM provider**: your key, your cost, unmetered. Configure it at **Admin → Brain** (admin only).

| Kind | Base URL | Auth | Notes |
|---|---|---|---|
| `anthropic` | `https://api.anthropic.com` (default) | API key | Native Anthropic Messages API: Claude models, prompt caching, native tool use. See [Anthropic](#anthropic) |
| `openai` | `https://api.openai.com` (default) | API key | Native OpenAI API |
| `openai_compatible` | Any URL | API key, or none | Azure, OpenRouter, Together, Groq, an Anthropic gateway, or a local server (vLLM, LM Studio, llama.cpp) |
| `ollama` | `http://localhost:11434` (default) | None | Local models via Ollama |

An `openai_compatible` provider may be saved without a key. CH-UI then sends no `Authorization` header, which is what keyless local servers expect.

### Anthropic

Pick **Anthropic** under **Admin → Brain → Add provider**, paste an API key from the Anthropic Console (`sk-ant-...`, stored encrypted like every provider key) and click **Sync models**. Leave the base URL at `https://api.anthropic.com`, or point it at a proxy that speaks the Messages API (a root URL or one ending in `/v1` both work). If the proxy has no `/v1/models` endpoint, sync offers the current Claude models instead. **+ Model id** on a provider adds any other model id by hand, for a new release or a proxy alias.

| Model | Use |
|---|---|
| `claude-sonnet-5-5` | The agent model: everyday Brain chat with tools |
| `claude-opus-5-5` | Harder agent work, at about twice the Sonnet price |
| `claude-haiku-5-5` | Quick tasks such as Ask AI |
| `claude-fable-5-1` | The most capable and most expensive; only used when you choose it |

How CH-UI calls it:

- `POST {base}/v1/messages` with `x-api-key`, `anthropic-version: 2023-06-01`, streamed over SSE.
- **Prompt caching.** The system prompt and the tool list carry an ephemeral `cache_control` marker, so that stable prefix is written to the cache once and read back at a tenth of the input price (or less) on every later request. One more marker on the newest message lets each step of an agent turn read the earlier steps from cache. Cache reads and writes show up in the usage numbers.
- **Tool use.** Brain's tools are sent with their JSON schemas and `tool_choice: auto` (never forced). Tool results go back as `tool_result` blocks, with `is_error` when a tool failed. Within a turn, the model's own blocks (including its thinking, with signature) are sent back unchanged; if the API rejects a replayed thinking block, CH-UI retries once without thinking blocks.
- **No `temperature` or `top_p`.** Current Claude models reject non-default sampling.
- **Output limit.** `max_tokens` defaults to 16,384 (8,192 for Haiku); thinking counts toward it. Change it with **Output token limit** under **Model roles and limits**. When an answer reaches the limit, Brain says it was cut off. A refusal is reported in plain words too.
- **Errors and retries.** `429` (rate limit) and `529` (overloaded) are retried up to three times, waiting what `retry-after` asks (at most 60 seconds) or 1, 2, then 4 seconds. Other errors are shown with the API's error type, message and request id.

### Model roles

**Admin → Brain → Model roles and limits** sets two models:

- **Agent model** answers Brain chats that have no model picked.
- **Quick model** answers Ask AI and Fix with AI. A small model such as `claude-haiku-5-5` keeps it fast and cheap. Empty means the agent model. Until an admin saves this setting, the form pre-selects `claude-haiku-5-5` when that model is synced and active; nothing is stored until **Save**, and a saved choice (Automatic included) is never replaced.

Either can stay on **Automatic**, which uses the default model.

### Recommended models

**Activate recommended** on a provider picks one default model. Current Claude models lead: `claude-sonnet-5-5` for the agent, then `claude-opus-5-5` for harder work. `claude-haiku-5-5` suits quick tasks such as Ask AI. `claude-fable-5-1` is never picked automatically; choose it yourself if you want it. Among models of the same rank, a plain alias beats a dated snapshot or a `mini` / `preview` variant. Claude models are sent no `temperature`, since they reject non-default values.

## Configuring AI

All AI configuration lives at **Admin → Brain** and is restricted to **admins**:

- **Providers**: add/edit/remove providers, sync their model lists, activate models, set a default.
- **Skills**: create, edit, and activate the reusable expertise Brain draws on (see [Skills](#skills)).

API keys are encrypted at rest with the app secret key.

### Model resolution

When you send a message, the model is resolved in this order:

1. Model selected in the request
2. The chat's stored model
3. The **agent model** (Ask AI: the **quick model**, then the agent model)
4. The default active model

## Ask AI (text-to-SQL)

You don't have to open a chat to get a query. In the **SQL editor**, click
**Ask AI** in the toolbar, describe what you want in plain English (e.g. *"top 10
users by orders last month"*), and CH-UI writes a ClickHouse query straight into
the editor.

The generation is **grounded in your metadata**, not guessed:

- The most relevant **tables and columns** from the active connection's live
  schema are retrieved and given to the model.
- Your **documented models** (names + descriptions) are included so the model
  prefers curated, governed datasets.
- Tables **and views** are considered. They are ranked by how well the
  question matches their names and their column names, and the top 16 are sent
  with their column types and **comments**.
- The model is instructed to use **only** those tables/columns and ClickHouse
  syntax.
- When the request carries the editor's current SQL (`current_sql`), the model
  edits that query instead of starting over.

It uses the **quick model** (see [Model roles](#model-roles)) and streams: the
query appears under the box as it is written, then goes into the editor. If the
answer hit the output limit, you are told the SQL may be incomplete. Always
review generated SQL before running it.

### Self-checking

Before the query reaches the editor, ClickHouse checks it as you, without
running it: `EXPLAIN PLAN` for a `SELECT` or `WITH` query (tables, columns and
types are resolved), `EXPLAIN AST` for anything else (syntax only). The check
runs with `readonly = 1`, a 10 second cap and the same governance policies as the
editor.

- When ClickHouse rejects the query with an error a model can fix (syntax,
  unknown table, database, column or function, type mismatches, aggregation
  mistakes; codes 62, 47, 46, 60, 81, 43, 42, 215, 184, 53, 386, 352, 10, 36),
  the error goes back to the model and it writes the query again. At most two
  repairs, and only while your daily budget lasts. The preview is cleared and
  refilled while a repair streams.
- Errors no edit fixes (access denied, read-only profile, limits, timeouts,
  missing parameter values; codes 497, 164, 201, 159, 241, 456), an offline
  tunnel and a governance block skip the check.
- If ClickHouse still rejects the final query, you get a warning instead of a
  success message. The query is inserted either way, so you can edit it.

:::note
Endpoints: `POST /api/brain/generate-sql` with `{ "question": "...", "model_id": "?", "current_sql": "?" }`
returns JSON with the SQL, the tables it grounded on, the usage and
`validation` (`status`: `passed`, `failed` or `skipped`, plus `error`, `reason`
and `rounds`, the number of repairs). `POST /api/brain/generate-sql/stream`
takes the same body and answers with Server-Sent Events: `delta` events,
`validating` before each check, `repair` (`round`, `error`) and `reset` before a
repair writes the query again, then one `done` event (`sql`, `tables_used`,
`model`, `usage`, `validation`) or an `error` event.
:::

## Fix with AI

When a query fails with an error a model can fix (the same codes as above), the
error panel shows **Fix with AI** next to **Go to error**. It appears only with a
Pro license, when the failed run was the plain editor statement (not a sorted or
filtered grid re-query), and while that text is still where it ran from.

A sheet opens with the model's one-line reason, a side-by-side diff of the failed
query and the fix, and a badge: **Checked with EXPLAIN** (ClickHouse accepts the
fix), **Not verified** (the check was skipped) or **Still fails**. The fix goes
through the same check and up to two repairs as Ask AI, with the query's
parameter values bound. The model is asked for the smallest edit that keeps the
query's meaning. **Replace query** puts the fix in place of exactly that
statement as one change, so a single undo brings the original back. If you edited
the statement while the sheet was open, nothing is replaced.

:::note
Endpoints: `POST /api/brain/fix-sql` with `{ "sql": "...", "error": "...", "params": { "name": "value" }, "model_id": "?" }`
returns JSON with `sql`, `explanation`, `validation`, `tables_used`, `model` and
`usage`. `POST /api/brain/fix-sql/stream` takes the same body and sends the same
events as the Ask AI stream.
:::

## Chat workflow

### Creating a chat

Open **Brain** in the sidebar and click **New Chat**. If no provider is configured yet, Brain points you to **Admin → Brain** to add one.

### Schema context

Type `@` in the message box to add context. A menu opens with **Tables**, **Dashboards**, **Pipelines**, **Models** and **Saved Queries**. For a table, pick the database and then the table; it shows up in the message as a chip like `table:default.events`. You can mention as many items as you like in one message.

Context belongs to the message you mention it in, not to the whole chat. When you send, CH-UI fetches the column names and types of each mentioned table and adds them to the system prompt for that request:

```
Schema context:
Table 1: default.events
Columns:
- id (UInt64)
- timestamp (DateTime)
- user_id (String)
- event_type (String)
```

A mentioned dashboard, pipeline, model or saved query adds a short description instead: a dashboard's panels and their SQL, a saved query's or model's SQL (shortened), a pipeline's status. Later messages still see the earlier `@default.events` text in the chat history, but not the columns, so mention the table again when you need its schema.

### Sending messages

Messages stream via **Server-Sent Events (SSE)**. While Brain waits on an approval, a tool or the provider, the stream sends a `: ping` comment every 15 seconds so proxies keep it open. The chat history (excluding errors) is sent to the model with each request, newest first up to about 24,000 tokens; older turns are left out and the model is told so. Each earlier assistant turn carries a short summary of its tool results (ids, names, row counts), so a follow-up like "add a panel to it" knows which dashboard you mean. Pressing **Stop** ends the turn and saves it as stopped, including while an approval is pending. The chat title is auto-generated from the first prompt. Chats are scoped to the authenticated user + connection, so users can't see each other's chats. For [SSO](/docs/sso) people, who share one ClickHouse service account, chats are kept per person (by email) since v2.13.2.

### Base prompt

Brain uses a focused base prompt (prioritize correctness, SQL-first, safe `LIMIT 100` on exploratory selects, ask one clarifying question when needed). On top of that, it appends the active **skill**.

## Agentic tools (Pro)

With a Pro license (and a provider that supports tool calling), Brain runs an **agentic loop**: instead of only writing SQL for you, it can call tools to inspect your schema, run queries, and create or modify workspace objects. The catalog is 39 tools:

| Group | Tools |
|---|---|
| Query & schema | `list_tables`, `describe_table`, `run_query` |
| Saved queries | `list_saved_queries`, `create_saved_query`, `update_saved_query`, `delete_saved_query` |
| Dashboards | `list_dashboards`, `get_dashboard`, `create_dashboard`, `add_dashboard_panel`, `delete_dashboard_panel`, `delete_dashboard` |
| Models | `list_models`, `create_model`, `update_model`, `run_model`, `build_model`, `schedule_model`, `delete_model` |
| Pipelines | `list_pipelines`, `get_pipeline_graph`, `create_pipeline`, `configure_pipeline`, `start_pipeline`, `delete_pipeline` |
| Observability | `list_services`, `query_logs`, `query_traces`, `find_trace`, `list_metrics`, `query_metrics` |
| Insights | `get_insights` |
| Performance | `analyze_query_log`, `query_pattern`, `explain_query`, `table_storage`, `merges_and_mutations`, `compare_query_estimates` |

Read-only tools (listing, describing, querying) run without ceremony. [Ask AI](#ask-ai-text-to-sql) is Pro too. `build_model` does the same as `run_model`: models have no data tests yet.

### Performance diagnostics

Ask Brain why things are slow and it reads ClickHouse's own system tables as
you, read-only, with no approval and for every role, viewers included:

| Tool | What it reads |
|---|---|
| `analyze_query_log(view, window?, user?, table?, min_duration_ms?)` | The top 20 from `system.query_log` over 1h, 6h, 24h (default) or 7d. Views: `slow` (patterns by p95), `failed` (by error code), `heavy_readers` (patterns by bytes read), `patterns` (most run), `users` (load per user). Sample queries are clipped to 400 characters. |
| `query_pattern(hash, window?)` | One pattern: p50, p95 and max latency, rows and bytes read, average `SelectedParts`, `SelectedMarks` and `SelectedRows`, a per-user split and one example query. |
| `explain_query(sql, mode?)` | `EXPLAIN indexes = 1` (default), `PIPELINE`, `ESTIMATE` or `PLAN` of a single `SELECT` or `WITH` query, without running it. |
| `table_storage(database, table?)` | Active parts: rows, bytes, compression ratio, partitions and parts per table, partition skew, the 50 largest partitions and the 20 largest columns. Flags tables with over 1,000 partitions or over 300 parts in one partition. |
| `merges_and_mutations(database?, table?)` | Running merges and unfinished mutations. Flags merges running over an hour and mutations older than 30 minutes that still have parts to do or a failure reason. |
| `compare_query_estimates(original_sql, candidate_sql)` | Rows, marks, parts and granules each query would read, with deltas. |

Every diagnostic statement carries its own `LIMIT` and, for `system.query_log`,
its own time window, so it stays bounded even for users whose ClickHouse profile
is read only (CH-UI's caps are dropped for them). CH-UI's own queries
(`log_comment` `ch-ui:brain` and `ch-ui:query-insights`) are left out of the
numbers. When `system.query_log` is missing or not granted, Brain says so and
suggests `GRANT SELECT ON system.query_log TO <user>`. The tools read the
connected node only; on a cluster, Brain says the results are local-node only.

Ask Brain to **optimize** a query and it runs `explain_query` with indexes,
looks at the tables with `describe_table`, writes a rewrite and compares both
with `compare_query_estimates`. It shows the SQL change and the before and after
numbers, says these are estimates of data read, not latency, and states any
change in meaning. In the chat, EXPLAIN output shows in a monospace block and the
comparison as a small before and after table.

### What the model sees

The person sees every tool result in full. The model gets a capped copy: query results as CSV with at most 100 rows, other results with every list cut to 100 items, and at most 64 KB per result, with a note saying what was cut. Within one turn, when the conversation passes about 64,000 tokens, the oldest tool outputs are replaced with a short note.

Tool output is wrapped in `<tool_output>` tags, and the system prompt tells the model that anything inside them is data from your database and workspace, never instructions. This limits prompt injection through table rows, query text or object names; the approval card below is the backstop.

### Roles

Tools that change the workspace need the **admin** or **analyst** role, the same rule as the REST API. For a **viewer**, Brain is not offered those tools, its system prompt says the person has read-only access, and a write the model asks for anyway is refused without showing an approval card. Approving a pending action also needs a writer role.

### Governance guardrails

Every query Brain runs (`run_query`, `describe_table` and its sample, `get_insights`, telemetry tools, panel validation, and query result artifacts) goes through the same [governance policies](/docs/governance) as the SQL editor. A blocked query does not run; the model is told which policy blocked it. Queries you or Brain run land in your query history with source `brain`, and in `system.query_log` with `log_comment = 'ch-ui:brain'`.

### Approval workflow

Every mutating tool (creates, updates, deletes, runs, scheduling, pipeline configuration) **requires your explicit approval**. Brain proposes the action as a card in the chat that shows the tool and every argument in full (SQL, target ids, names), with **Approve** / **Decline** buttons; nothing is executed until you approve. Deletes ask once more before running. If you decline, Brain acknowledges and offers a different approach rather than re-proposing the same thing.

The gate fails closed: if CH-UI cannot record the approval request, the action does not run and both you and the model are told. Each action keeps one card, which is updated with its outcome, so a reload shows the same history.

Only the person who started the chat can approve or decline its pending actions; anyone else gets `404 Approval not found`.

Pending approvals don't linger: requests left undecided for more than 10 minutes are swept to an `interrupted` state, so a stale card can never be approved into action later.

## Skills

A **skill** is reusable expertise Brain applies to every chat: domain instructions, conventions, and example queries written as plain text. Skills make Brain better at *your* data and *your* workflows without re-explaining context each time. Manage them at **Admin → Brain** (admin only), where the active skill is shown under **System prompt** and edited with **Edit prompt**.

Each skill has a **name**, its **instructions** (free-form text, included verbatim in Brain's system prompt), and two flags:

- **Active**: eligible to be used.
- **Default**: preferred when more than one skill is active.

One skill applies at a time: Brain uses the active skill, preferring the default one, then the most recently updated. Its content is appended to the system prompt of every chat, so keep it focused. Schema conventions, naming rules, common joins, and known gotchas earn their tokens; long prose doesn't.

### The default skill

A new install seeds one skill, **Default Brain Skill**, active and marked default. It is a good starting point to edit rather than replace. Its content, shortened:

```text
You are Brain, a senior ClickHouse copilot.

Priorities:
- Give correct SQL first, concise explanation second.
- Keep queries safe and cost-aware: start with LIMIT 100 unless user asks otherwise.
- Prefer explicit columns over SELECT * on large tables.
- Use only schema fields known in context; if missing, ask a short clarifying question.

Tool behavior:
- Read-only queries by default.
- Never execute DDL/DROP/TRUNCATE/ALTER unless user explicitly asks and confirms.
- For expensive requests, propose a lightweight preview query first.

Formatting:
1) One-line intent acknowledgment.
2) SQL in a fenced sql block.
3) Short explanation and optional next-step variants.
```

The full version also has an **Artifacts** block (return runnable SQL blocks, reference query result artifacts by title, reuse earlier artifacts in follow-ups). The seed only runs when there are no skills at all, so your edits are not overwritten on upgrade or restart.

## Query result artifacts

When Brain runs `run_query`, or you press **Run** on a SQL block in its answer, the result is stored as a `query_result` artifact on that message. The card shows the rows as a table or a chart. From a SQL block you can also **Copy** the SQL or **Open in Editor**.

### Running queries safely

Query result artifacts are **read-only only**: `SELECT`, `WITH`, `SHOW`, `DESC`, `DESCRIBE`, `EXPLAIN`. They run with ClickHouse `readonly = 1` and return at most 1,000 rows and 16 MB; a `SETTINGS` or trailing `FORMAT` clause in the SQL is rejected, and governance policies apply. Default timeout 30s (max 5 min). Runs are logged as `brain.query.run`.

## Usage and budgets

Every chat turn, Ask AI and Fix with AI request is recorded with the provider, the model and its tokens: input, output, cache read and cache write. An agent turn sums all of its steps. Counts come from what the provider reports: Anthropic's usage (including `cache_read_input_tokens` and `cache_creation_input_tokens`), OpenAI-style `usage` (cached prompt tokens split out as cache reads) and Ollama's eval counts. A provider that reports nothing records zeros.

- **Under each answer**, a small footer shows the model, the tokens and, when the model has a price, the estimated cost. Hover it for the exact numbers.
- **Admin → Brain → Usage** lists tokens per person and model for today, the last 7, 30 or 90 days, with totals and estimated cost. **Requests** counts every call; its tooltip splits chat turns from Ask AI and Fix with AI requests, and **Repairs** counts the extra calls that repaired SQL ClickHouse rejected (ledger purposes `ask_ai_repair` and `fix_sql_repair`).
- **Prices** (admin) are US dollars per million tokens for input, output, cache read and cache write. New installs start with the Claude list prices; edit, add or clear any of them. A model without input and output prices shows no cost. Costs are estimates, not a bill. Some models charge more for very long prompts (for example `claude-haiku-5-5` above 100K input tokens); the estimate uses the base price, so it can read low for those requests.
- **Daily token budget per person** (admin, optional) caps input, output and cache-write tokens per UTC day. Cache reads are not counted. A person past the budget gets a clear message (`429`) before the turn starts and nothing is sent to the provider; it resets at 00:00 UTC.

The ledger lives in the `brain_usage` table; each assistant message also keeps its own counts.

:::note
Admin endpoints: `GET /api/admin/brain/usage?days=30`, `GET` and `PUT /api/admin/brain/settings` (`agentModelId`, `quickModelId`, `dailyTokenBudget`, `maxOutputTokens`, `prices`), `POST /api/admin/brain/providers/{id}/models` with `{ "name": "claude-sonnet-5-5" }`.
:::

## Storage

Chats, messages, artifacts, skills and the usage ledger are stored in CH-UI's **local SQLite database** on your own server. History survives restarts.

## Safety defaults

| Control | Value |
|---|---|
| Default LIMIT | 100 (enforced in base prompt) |
| Artifact queries | Read-only only (SELECT, WITH, SHOW, DESC, DESCRIBE, EXPLAIN), ClickHouse `readonly = 1`, 1,000 rows, 16 MB |
| `run_query` tool | ClickHouse `readonly = 1`, its `limit` (max 1,000) as a row cap, 30s, 16 MB; a `SETTINGS` or trailing `FORMAT` clause is rejected; `LIMIT` added only to `SELECT` / `WITH` |
| What the model sees | 100 rows as CSV, 64 KB per tool result, fenced as data |
| Write tools | Admin or analyst only; each needs an approval, and the gate fails closed |
| Governance | Same policies as the SQL editor (Pro) |
| Artifact timeout | 30s default, 5 min max |
| Schema context | Per message, from `@` mentions; no fixed table limit |
| API key storage | Encrypted at rest with app secret key |
| Token budget | Optional per person per UTC day, checked before a turn, an Ask AI or Fix with AI request, and each SQL repair |
| Diagnostics tools | ClickHouse `readonly = 1`, 30s per call, every statement with its own `LIMIT` and, on `system.query_log`, its own time window; local node only |
| SQL self-check | `EXPLAIN PLAN` or `EXPLAIN AST` as the person, `readonly = 1`, 10s, governance applies; at most 2 repairs |
| Chat / artifact isolation | Scoped to username + connection; artifact actions verify chat ownership |
