---
title: Brain (AI Assistant)
description: "Schema-aware AI copilot for ClickHouse: bring your own model, with custom skills and living artifacts."
---

Brain is a built-in AI copilot that understands your ClickHouse schema and writes executable, ClickHouse-idiomatic SQL. It supports persisted multi-chat history, streaming responses, **skills** (reusable expertise applied to every chat), and **living artifacts** (charts, reports, and query results you can re-run, edit, and promote into the rest of CH-UI).

## Powering Brain

Brain runs on **your own LLM provider**: your key, your cost, unmetered. Configure it at **Admin → Brain** (admin only).

| Kind | Base URL | Auth | Notes |
|---|---|---|---|
| `openai` | `https://api.openai.com` (default) | API key | Native OpenAI API |
| `openai_compatible` | Any URL | API key | Azure, OpenRouter, Together, Groq, etc. |
| `ollama` | `http://localhost:11434` (default) | None | Local models via Ollama |

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
- The model is instructed to use **only** those tables/columns and ClickHouse
  syntax.

It uses the same AI provider as Brain. Always review generated SQL
before running it.

:::note
Endpoint: `POST /api/brain/generate-sql` with `{ "question": "...", "model_id": "?" }`.
Returns the SQL plus the list of tables it grounded on.
:::

## Chat workflow

### Creating a chat

Open **Brain** in the sidebar and click **New Chat**. If no provider is configured yet, Brain points you to **Admin → Brain** to add one.

### Schema context

Use the header dropdowns to attach table context (up to **10 tables** per chat): pick a database, pick a table, click **Add**. Column names and types are fetched and sent with every message:

```
Schema context (database: default, table: events):
Columns:
- id: UInt64
- timestamp: DateTime
- user_id: String
- event_type: String
```

### Sending messages

Messages stream via **Server-Sent Events (SSE)**. The chat history (excluding errors) is sent to the model with each request. The chat title is auto-generated from the first prompt. Chats are scoped to the authenticated user + connection, so users can't see each other's chats. For [SSO](/docs/sso) people, who share one ClickHouse service account, chats are kept per person (by email) since v2.13.2.

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

Read-only tools (listing, describing, querying) run without ceremony. [Ask AI](#ask-ai-text-to-sql) is Pro too.

### Approval workflow

Every mutating tool (creates, updates, deletes, runs, scheduling, pipeline configuration) **requires your explicit approval**. Brain proposes the action as a card in the chat with **Approve** / **Decline** buttons; nothing is executed until you approve. If you decline, Brain acknowledges and offers a different approach rather than re-proposing the same thing.

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

## Living artifacts

Brain doesn't just answer: it produces **artifacts** you can keep, refine, and turn into real CH-UI objects. There are four types:

| Type | What it is |
|---|---|
| `query_result` | A table from a query Brain ran |
| `chart` | A visualization (Brain picks chart type + axes via `create_chart`) |
| `report` | A markdown document: findings, summaries, runbooks (`create_report`) |
| `sql` | A saved SQL snippet |

Every artifact is **versioned** and carries its re-runnable definition, so from the chat you can:

- **Re-run**: re-execute the query against your ClickHouse and refresh the data (bumps the version).
- **Edit**: change the title, SQL, or markdown in place; SQL edits re-run automatically.
- **Promote**: turn the artifact into a first-class object:
  - **Save as query** → a saved query in your workspace
  - **Add to dashboard** → a panel on a dashboard you pick (charts carry their config across)
  - **Open in SQL editor** → load the query in the full editor
  - **Export markdown** → download the report (or query) as a `.md` file

### Running queries safely

Query-backed artifacts are **read-only only**: `SELECT`, `WITH`, `SHOW`, `DESC`, `DESCRIBE`, `EXPLAIN`. Default timeout 30s (max 5 min). Promoting to a dashboard panel or saved query is an explicit, audited action (`brain.artifact.promoted`); ad-hoc runs are logged as `brain.query.run`.

## Storage

Chats, messages, artifacts, and skills are stored in CH-UI's **local SQLite database** on your own server. History survives restarts.

## Safety defaults

| Control | Value |
|---|---|
| Default LIMIT | 100 (enforced in base prompt) |
| Artifact queries | Read-only only (SELECT, WITH, SHOW, DESC, DESCRIBE, EXPLAIN) |
| Artifact timeout | 30s default, 5 min max |
| Artifact re-run / promote | Read-only re-run; promote to query/panel is an audited action |
| Max schema contexts | 10 tables per chat |
| API key storage | Encrypted at rest with app secret key |
| Chat / artifact isolation | Scoped to username + connection; artifact actions verify chat ownership |
