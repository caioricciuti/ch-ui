---
title: Brain (AI Assistant)
description: "Schema-aware AI copilot for ClickHouse: bring your own model, with custom skills, approvals and query result artifacts."
---

Brain is a built-in AI copilot that understands your ClickHouse schema and writes executable, ClickHouse-idiomatic SQL. It supports persisted multi-chat history, streaming responses, **skills** (reusable expertise applied to every chat), and **query result artifacts** (tables you can view as a chart, re-run from the SQL block, or open in the editor).

## Powering Brain

Brain runs on **your own LLM provider**: your key, your cost, unmetered. Configure it at **Admin → Brain** (admin only).

| Kind | Base URL | Auth | Notes |
|---|---|---|---|
| `openai` | `https://api.openai.com` (default) | API key | Native OpenAI API |
| `openai_compatible` | Any URL | API key, or none | Azure, OpenRouter, Together, Groq, an Anthropic gateway, or a local server (vLLM, LM Studio, llama.cpp) |
| `ollama` | `http://localhost:11434` (default) | None | Local models via Ollama |

An `openai_compatible` provider may be saved without a key. CH-UI then sends no `Authorization` header, which is what keyless local servers expect.

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
3. The default active model

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

It uses the same AI provider as Brain. Always review generated SQL
before running it.

:::note
Endpoint: `POST /api/brain/generate-sql` with `{ "question": "...", "model_id": "?", "current_sql": "?" }`.
Returns the SQL plus the list of tables it grounded on.
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

With a Pro license (and a provider that supports tool calling), Brain runs an **agentic loop**: instead of only writing SQL for you, it can call tools to inspect your schema, run queries, and create or modify workspace objects. The catalog is 33 tools:

| Group | Tools |
|---|---|
| Query & schema | `list_tables`, `describe_table`, `run_query` |
| Saved queries | `list_saved_queries`, `create_saved_query`, `update_saved_query`, `delete_saved_query` |
| Dashboards | `list_dashboards`, `get_dashboard`, `create_dashboard`, `add_dashboard_panel`, `delete_dashboard_panel`, `delete_dashboard` |
| Models | `list_models`, `create_model`, `update_model`, `run_model`, `build_model`, `schedule_model`, `delete_model` |
| Pipelines | `list_pipelines`, `get_pipeline_graph`, `create_pipeline`, `configure_pipeline`, `start_pipeline`, `delete_pipeline` |
| Observability | `list_services`, `query_logs`, `query_traces`, `find_trace`, `list_metrics`, `query_metrics` |
| Insights | `get_insights` |

Read-only tools (listing, describing, querying) run without ceremony. [Ask AI](#ask-ai-text-to-sql) is Pro too. `build_model` does the same as `run_model`: models have no data tests yet.

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

## Storage

Chats, messages, artifacts, and skills are stored in CH-UI's **local SQLite database** on your own server. History survives restarts.

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
| Chat / artifact isolation | Scoped to username + connection; artifact actions verify chat ownership |
