---
title: API Reference
description: CH-UI REST API reference
---

Base URL (default local):

```text
http://localhost:3488
```

## Public Endpoints

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/health` | Health status |
| `GET` | `/metrics` | Prometheus metrics |
| `GET` | `/connect` | WebSocket tunnel upgrade for the connector (authenticates with the tunnel token) |
| `GET` | `/api/auth/config` | Available login methods (password, SSO), whether first-run setup is open (`setup_open`) and whether sign-in with a ClickHouse URL is on (`login_url_allowed`) |
| `POST` | `/api/auth/setup` | First-run setup: add a direct connection with the one-time setup code (see below) |
| `POST` | `/api/auth/login` | Session login |
| `POST` | `/api/auth/logout` | Session logout |
| `GET` | `/api/auth/session` | Session details |
| `GET` | `/api/auth/connections` | Login-time connection list (with `sso_ready` per connection when the caller has an SSO session) |
| `POST` | `/api/auth/switch-connection` | Move the session to another connection (needs the session cookie and `Content-Type: application/json`; see [Authentication](/docs/authentication#switching-connection)) |
| `GET` | `/api/auth/oidc/login` | Start OIDC SSO (redirects to IdP) |
| `GET` | `/api/auth/oidc/callback` | OIDC SSO redirect target |
| `GET` | `/api/license` | Current license status |
| `GET` | `/api/public/dashboards/{token}` | Shared dashboard by share token (120 requests/min per IP) |
| `POST` | `/api/public/dashboards/{token}/query` | Run a panel query on a shared dashboard (120 requests/min per IP) |

### First-run setup

Open only until the first admin signs in. The setup code is printed once to the
server log at startup (`setup_code`); see
[Can't login?](/docs/cant-login/#first-run-setup-from-the-login-page).
`GET /api/auth/config` returns `"setup_open": true` while a valid code exists
and setup has not closed.

```bash
curl -X POST http://localhost:3488/api/auth/setup \
  -H "Content-Type: application/json" \
  -d '{"code":"XXXX-XXXX-XXXX","name":"ClickHouse","clickhouse_url":"http://clickhouse:8123"}'
# { "success": true, "connection": { "id": "...", "name": "ClickHouse" } }
```

The code is case-insensitive and dashes and spaces are ignored. `name` is
required, up to 100 characters. `clickhouse_url` must be `http://` or
`https://` with a host and no credentials, query string or fragment; link-local
and cloud metadata addresses are refused. The first success creates a direct
connection; later successes update that same connection.

| Status | Meaning |
|---|---|
| `200` | Connection saved and its connector started |
| `400` | Bad body, missing code or name, name too long, or a rejected URL |
| `401` | Wrong setup code. After 10 wrong codes in total the code is discarded |
| `404` | Setup is closed (an admin exists or has signed in) |
| `410` | The code expired or was discarded. Restart CH-UI to get a new one |
| `429` | Too many wrong codes from this IP (5 per 15 minutes); the body has `retryAfter` in seconds |

### Webhooks

These skip the session cookie and authenticate with their own secret.

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/pipelines/webhook/{id}` | Webhook source endpoint for a pipeline |
| `POST` | `/ingest/v1/logs`, `/ingest/v1/traces`, `/ingest/v1/events` | OTLP/HTTP and JSON event receivers, with an ingest key (`chi_...`). See [Ingest](/docs/ingest) |
| `POST`, `PUT` | `/ingest/es/_bulk`, `/ingest/es/{index}/_bulk` | Elasticsearch bulk API receiver, with the startup calls shippers make. See [Elasticsearch bulk API](/docs/ingest#elasticsearch-bulk-api) |
| `POST` | `/ingest/services/collector`, `/ingest/services/collector/event`, `/ingest/services/collector/raw` | Splunk HEC receiver, plus `GET /ingest/services/collector/health`. See [Splunk HTTP Event Collector](/docs/ingest#splunk-http-event-collector) |
| `POST` | `/api/github/webhook/{connectionId}` | GitHub push webhook for model sync, checked against the webhook secret (**Pro**) |

### MCP and OAuth

These do not use the session cookie. `/mcp` requires a bearer key (`chm_...`)
or an OAuth access token on every request. See [MCP Server](/docs/mcp).

| Method | Path | Purpose |
|---|---|---|
| `POST` / `GET` | `/mcp` | Model Context Protocol endpoint (streamable HTTP) |
| `GET` | `/.well-known/oauth-protected-resource` | Protected resource metadata for `/mcp` |
| `GET` | `/.well-known/oauth-protected-resource/mcp` | Same metadata, at the path-suffixed location some clients probe |
| `GET` | `/.well-known/oauth-authorization-server` | Authorization server metadata |
| `GET` | `/oauth/authorize` | Start an authorization code + PKCE flow |
| `POST` | `/oauth/token` | Exchange a code or refresh token (60 requests/min per IP) |
| `POST` | `/oauth/register` | Dynamic Client Registration (20 requests/min per IP) |

The consent page in the app calls these with the session cookie:

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/oauth/consent/{id}` | Pending consent request details |
| `POST` | `/api/oauth/consent/{id}/approve` | Approve the client's access |
| `POST` | `/api/oauth/consent/{id}/deny` | Deny the client's access |

## Session-Protected Endpoints

All below require `chui_session` cookie.

Unsafe methods (anything but `GET`, `HEAD` and `OPTIONS`) under `/api` are
refused with `403 Cross-origin request denied` when the request carries an
`Origin` that is not CH-UI's own or one in `allowed_origins`. Scripts that send
no `Origin` (curl, for example) are not affected. `POST /api/auth/login` also
requires `Content-Type: application/json` and returns `415` without it.

Optional `X-CHUI-Connection: <connection id>` header: when present and the
session is on a different connection (it was switched in another tab), the
request is refused with `409 {"error":"connection_changed"}` instead of
running on the new connection. The UI sends it on every request.

### License

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/license/activate` | Activate a signed license (`{"license": "<json>"}`) (admin) |
| `POST` | `/api/license/deactivate` | Remove the license and return to community (admin) |

### Query

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/query/run` | Execute SQL query |
| `POST` | `/api/query/` | Same as `/api/query/run` |
| `POST` | `/api/query/stream` | Stream query results via NDJSON/SSE |
| `POST` | `/api/query/format` | Format/beautify SQL |
| `POST` | `/api/query/explain` | EXPLAIN output |
| `POST` | `/api/query/plan` | Parsed query plan tree |
| `POST` | `/api/query/profile` | Query log metrics for exact SQL |
| `POST` | `/api/query/sample` | Sample rows (per-shard or global) |
| `POST` | `/api/query/explorer-data` | Paginated table browsing for the data explorer |
| `POST` | `/api/query/estimate` | Cost estimate via `EXPLAIN ESTIMATE` |
| `GET` | `/api/query/databases` | List databases |
| `GET` | `/api/query/tables` | List tables (`?database=`) |
| `GET` | `/api/query/columns` | List columns (`?database=&table=`) |
| `GET` | `/api/query/data-types` | List ClickHouse data types |
| `GET` | `/api/query/clusters` | List clusters |
| `GET` | `/api/query/cluster-info` | Full cluster topology |
| `GET` | `/api/query/node-info` | Which ClickHouse node is serving requests, plus server identity |
| `GET` | `/api/query/host-info` | Host information |
| `GET` | `/api/query/completions` | ClickHouse functions and keywords for autocomplete |
| `POST` | `/api/query/schema/database` | Create database |
| `POST` | `/api/query/schema/database/drop` | Drop database |
| `POST` | `/api/query/schema/table` | Create table |
| `POST` | `/api/query/schema/table/drop` | Drop table |
| `POST` | `/api/query/upload/discover` | Detect schema from uploaded file |
| `POST` | `/api/query/upload/ingest` | Insert uploaded data into table |

When ClickHouse refuses one of the four `schema` calls, the status follows the
ClickHouse error code: `403` for a missing grant or a read-only user (codes
497, 164), `400` for syntax errors, unknown or duplicate databases, tables or
columns and bad arguments, `504` for a timeout (159), `503` for too many
simultaneous queries (202), and `502` for anything else.

### Query History

Per person and per connection. SSO users who share a ClickHouse account see
only their own entries.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/query-history` | Your history, newest first (`?search=&status=&limit=&offset=`) |
| `DELETE` | `/api/query-history/{id}` | Delete one entry |
| `DELETE` | `/api/query-history` | Clear your history on the current connection |

### Lineage

Any signed-in role, every edition. Read with your ClickHouse account; see [Data Lineage](/docs/lineage) for the graph shape and edge directions.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/lineage` | Lineage graph: `nodes`, `edges` and `meta` (`?databases=a,b` limits it to those databases and their direct neighbours in any database, including views that read them; `?refresh=1` skips the 30 s cache). No definitions in this response |
| `GET` | `/api/lineage/node` | One object with its `engine_full` and `ddl` (`?id=db.name`, or `?database=&name=` when the database name has a dot); `404` when not visible |

### Connections

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/connections` | List connections |
| `POST` | `/api/connections` | Create connection (admin) |
| `GET` | `/api/connections/{id}` | Get connection |
| `PUT` | `/api/connections/{id}` | Update connection (admin); a URL that reaches another ClickHouse server resets the connection and the response carries `reset` with the counts |
| `DELETE` | `/api/connections/{id}` | Delete connection (admin) |
| `POST` | `/api/connections/{id}/test` | Test connection |
| `GET` | `/api/connections/{id}/token` | Get tunnel token (admin) |
| `POST` | `/api/connections/{id}/regenerate-token` | Regenerate tunnel token (admin) |
| `PUT` | `/api/connections/{id}/sso-account` | Set the SSO ClickHouse service account (admin) |
| `GET` | `/api/connections/{id}/sso-identity` | SSO identity mode (shared or per-person users) and group to role mapping (admin) |
| `PUT` | `/api/connections/{id}/sso-identity` | Set the SSO identity mode and mapping; per-person needs Pro (admin) |
| `POST` | `/api/connections/{id}/sso-identity/setup-sql` | Render the admin setup SQL for an SSO identity config (admin) |
| `POST` | `/api/connections/{id}/sso-identity/verify` | Probe the SSO provisioner and return pass/fail checks (admin, Pro) |
| `GET` | `/api/connections/{id}/background-credentials` | Mode and account name for every background worker (admin, passwords never returned) |
| `PUT` | `/api/connections/{id}/background-credentials/{worker}` | Set a worker's mode: `service_account`, `session` or `disabled` (admin) |

Background accounts are available in every edition. `{worker}` is one of
`schedule`, `model`, `pipeline`, `governance`, `cluster_health`,
`telemetry.monitor`, `performance`, `operations.report`, `sql_alert`,
`report`. The `PUT` body is
`{"mode": "...", "username": "...", "password": "..."}`; for
`service_account` CH-UI runs `SELECT 1` with the account before saving. See
[Background Accounts](/docs/background-accounts).

When `PUT /api/connections/{id}` points a direct connection at a different
ClickHouse server, the response has a `reset` object with what was cleared:
`sessions`, `role_overrides`, `sso_users`, `sso_account`,
`background_accounts`, `mcp_keys`, `dashboard_shares`. It is left out when the
URL reaches the same server. See
[Repointing a direct connection](/docs/connections/#repointing-a-direct-connection).

Connection objects (v2.33.0+) can also carry `agent_endpoint`, the ClickHouse
server an agent last reported (`scheme://host:port/path`, absent until an agent
from v2.33.0 signs in), and `server_reset_at`, when the connection last moved
to a different server. See
[Repointing an agent](/docs/connections/#repointing-an-agent-v2330).

### Saved Queries

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/saved-queries` | List the saved queries you can see (`scope`, `folder`, `owners=all` for admins) |
| `POST` | `/api/saved-queries` | Create saved query (optional `folder_id`, `tags`, `visibility`) |
| `GET` | `/api/saved-queries/{id}` | Get saved query (with `can_edit`) |
| `PUT` | `/api/saved-queries/{id}` | Update saved query (also `verified`, `folder_id`, `tags`, `visibility`) |
| `DELETE` | `/api/saved-queries/{id}` | Delete saved query |
| `POST` | `/api/saved-queries/{id}/duplicate` | Duplicate as your own private copy |
| `GET` | `/api/saved-queries/{id}/versions` | List versions (no snapshots) |
| `GET` | `/api/saved-queries/{id}/versions/{n}` | Get one version with its snapshot |
| `POST` | `/api/saved-queries/{id}/versions/{n}/restore` | Restore a version |
| `GET` | `/api/saved-queries/folders` | List folders |
| `POST` | `/api/saved-queries/folders` | Create folder (`409` on a sibling with the same name) |
| `PUT` | `/api/saved-queries/folders/{folderId}` | Rename or move folder |
| `DELETE` | `/api/saved-queries/folders/{folderId}` | Delete folder and subfolders; queries move to the root |
| `POST` | `/api/saved-queries/{id}/run` | Run with parameters, result as JSON (**Pro**, see [Query Parameters](/docs/querying#running-a-saved-query-via-the-api)) |

Create, update, delete, duplicate, restore and folder changes need the admin or
analyst role. A query is created (and duplicated) on the session's connection;
a `connection_id` in the body may only be empty or that connection, anything
else answers `400`. Restoring a version keeps the query's current connection. Viewers can list, read and run what they can see. Only the owner
or an admin can change a query; a private query you cannot see answers `404`.
See [Saved Queries](/docs/querying#saved-queries).

### Dashboards

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/dashboards` | List dashboards (with folder, tags and your star) |
| `POST` | `/api/dashboards` | Create dashboard (optional `folder_id`, `tags`) |
| `GET` | `/api/dashboards/{id}` | Get dashboard + panels |
| `PUT` | `/api/dashboards/{id}` | Update dashboard |
| `DELETE` | `/api/dashboards/{id}` | Delete dashboard + panels |
| `PUT` | `/api/dashboards/{id}/move` | Move dashboard to a folder (or the root) |
| `PUT` | `/api/dashboards/{id}/tags` | Replace dashboard tags |
| `POST` | `/api/dashboards/{id}/star` | Star for the current user |
| `DELETE` | `/api/dashboards/{id}/star` | Remove your star |
| `GET` | `/api/dashboards/folders` | List folders |
| `POST` | `/api/dashboards/folders` | Create folder |
| `PUT` | `/api/dashboards/folders/{folderId}` | Rename or move folder |
| `DELETE` | `/api/dashboards/folders/{folderId}` | Delete folder (contents move up one level) |
| `POST` | `/api/dashboards/{id}/panels` | Create panel |
| `PUT` | `/api/dashboards/{id}/panels/{panelId}` | Update panel |
| `DELETE` | `/api/dashboards/{id}/panels/{panelId}` | Delete panel |
| `GET` | `/api/dashboards/{id}/shares` | List public share links |
| `POST` | `/api/dashboards/{id}/shares` | Create public share link |
| `DELETE` | `/api/dashboards/{id}/shares/{shareId}` | Revoke share link |
| `POST` | `/api/dashboards/{id}/shares/{shareId}/invite` | Email a share link |
| `POST` | `/api/dashboards/query` | Execute panel query with time-range interpolation |

### Telemetry

Logs, sources and saved searches. The traces, metrics, service map and
monitor endpoints are listed under [Pro API Groups](#telemetry-pro).

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/telemetry/sources` | List sources for the active connection |
| `POST` | `/api/telemetry/sources` | Create source |
| `POST` | `/api/telemetry/sources/detect` | Propose sources from the exporter's tables |
| `PUT` | `/api/telemetry/sources/{id}` | Update source |
| `DELETE` | `/api/telemetry/sources/{id}` | Delete source |
| `POST` | `/api/telemetry/sources/{id}/test` | Check the mapping and count recent rows |
| `POST` | `/api/telemetry/receive` | Set up receiving OTLP logs or traces in one step (analyst, admin). See below |
| `POST` | `/api/telemetry/logs/search` | Search logs |
| `POST` | `/api/telemetry/logs/histogram` | Log counts over time by severity |
| `POST` | `/api/telemetry/logs/facets` | Top values for facet fields |
| `POST` | `/api/telemetry/logs/context` | Rows around a log line |
| `GET` | `/api/telemetry/logs/by-trace/{traceId}` | Logs of one trace |
| `GET` | `/api/telemetry/saved-searches` | List saved searches |
| `POST` | `/api/telemetry/saved-searches` | Save a search |
| `PUT` | `/api/telemetry/saved-searches/{id}` | Update saved search |
| `DELETE` | `/api/telemetry/saved-searches/{id}` | Delete saved search |

`POST /api/telemetry/receive` is what **Send data here** calls. It creates (or
reuses) the table with your own ClickHouse user, creates and starts an ingest
pipeline named `Receive logs` or `Receive traces` (numbered if the name is
taken), creates an ingest key called `Default key`, and registers the
Telemetry source unless that table already is one. Every field is optional
except `signal`:

```json
POST /api/telemetry/receive
{ "signal": "logs", "database": "default", "table": "otel_logs", "ttl_days": 30 }
```

`signal` is `logs` or `traces`; `table` defaults to `otel_<signal>`, and
`ttl_days` to 30 (0 keeps data forever, at most 36500). The `201` response
holds `pipeline` (`id`, `name`, `status`, `signal`), `key`, `secret` (the key,
returned only here), `source`, `table_created`, `database` and `table`. It
returns `503` when the server has no ingest spool, `409` when pipelines are
disabled under the connection's background accounts or the table exists
without the OpenTelemetry columns (the body lists them in `missing`), `400`
for an unknown database or a bad name, and `403` when your ClickHouse user
cannot read or create the table. If a step after the pipeline is created
fails, the pipeline and its key are removed again; a table the call created
is kept and reused on the next try.

`GET` and `PUT /api/telemetry/config` (the single-table config from before
v2.11.0) now return `410 Gone`.

### Brain

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/brain/models` | List active models |
| `GET` | `/api/brain/usage/me` | What you used today and this month, and the [budget](/docs/brain#budgets) rules that apply to you |
| `GET` | `/api/brain/chats` | List user's chats (`?includeArchived=`) |
| `POST` | `/api/brain/chats` | Create chat |
| `GET` | `/api/brain/chats/{chatID}` | Get chat |
| `PUT` | `/api/brain/chats/{chatID}` | Update chat (title, model, archive, context) |
| `DELETE` | `/api/brain/chats/{chatID}` | Archive/delete chat |
| `GET` | `/api/brain/chats/{chatID}/messages` | Get chat messages |
| `POST` | `/api/brain/chats/{chatID}/messages/stream` | Stream LLM response (SSE) |
| `GET` | `/api/brain/chats/{chatID}/artifacts` | List artifacts |
| `POST` | `/api/brain/chats/{chatID}/artifacts/query` | Execute & persist SQL artifact |
| `GET` | `/api/brain/skills` | Get active skill |
| `POST` | `/api/brain/generate-sql` | Ask AI: text-to-SQL grounded in the live schema (**Pro**) |
| `POST` | `/api/brain/approvals/{approvalID}/approve` | Approve a pending agent action (only the person who requested it) |
| `POST` | `/api/brain/approvals/{approvalID}/decline` | Decline a pending agent action (only the person who requested it) |
| `GET` | `/api/brain/audit` | Agent approval records for every person (`?status=&limit=`, default 100, max 500). Admin only |

Approvals only come up in agent mode, which runs when a Pro license is active
and the provider supports tools.

### Models

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/models` | List models |
| `POST` | `/api/models` | Create model |
| `GET` | `/api/models/{id}` | Get model |
| `PUT` | `/api/models/{id}` | Update model |
| `DELETE` | `/api/models/{id}` | Delete model |
| `GET` | `/api/models/validate` | Validate all models (refs, cycles) |
| `POST` | `/api/models/run` | Execute all models, or `{"selector": "+name"}` |
| `POST` | `/api/models/{id}/run` | Execute a model with upstream deps; `{"scope": "upstream" \| "downstream" \| "only", "full_refresh": true}` |
| `POST` | `/api/models/{id}/compile` | Resolved SQL and planned statements (optional `{"sql_body"}`) |
| `POST` | `/api/models/preview` | First 100 rows of `{"sql_body", "model_id"?}`, read-only, session credentials |
| `GET` | `/api/models/{id}/tests` | List data tests |
| `POST` | `/api/models/{id}/tests` | Create data test |
| `PUT` | `/api/models/{id}/tests/{testId}` | Update data test |
| `DELETE` | `/api/models/{id}/tests/{testId}` | Delete data test |
| `GET` | `/api/models/dag` | Get DAG for visualization |
| `GET` | `/api/models/runs` | List runs (`?limit=&offset=`) |
| `GET` | `/api/models/runs/{runId}` | Get run + per-model results |
| `GET` | `/api/models/pipelines` | List model groups with picked models and next runs |
| `POST` | `/api/models/pipelines` | Create model group (`name`, `selector`, `cron`, `timezone`, `enabled`) |
| `PUT` | `/api/models/pipelines/{id}` | Update model group |
| `DELETE` | `/api/models/pipelines/{id}` | Delete model group |
| `POST` | `/api/models/pipelines/{id}/run` | Run model group |
| `GET` | `/api/models/schedule-preview` | Next 3 run times for `?cron=&timezone=` |

### Pipelines (Data Ingestion)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/pipelines` | List pipelines on the current connection |
| `POST` | `/api/pipelines` | Create pipeline on the current connection |
| `GET` | `/api/pipelines/{id}` | Get pipeline |
| `PUT` | `/api/pipelines/{id}` | Update pipeline |
| `DELETE` | `/api/pipelines/{id}` | Delete pipeline |
| `POST` | `/api/pipelines/{id}/start` | Start pipeline |
| `POST` | `/api/pipelines/{id}/stop` | Stop pipeline |
| `PUT` | `/api/pipelines/{id}/graph` | Save the whole graph (nodes and edges) |
| `GET` | `/api/pipelines/{id}/status` | Get pipeline status + metrics |
| `GET` | `/api/pipelines/{id}/runs` | List runs (`?limit=&offset=`) |
| `GET` | `/api/pipelines/{id}/runs/{runId}/logs` | Logs of one run |
| `POST` | `/api/pipelines/{id}/test` | Test the source and check the sink |
| `POST` | `/api/pipelines/{id}/preview-transforms` | Before and after records for a transform chain (see [Preview](/docs/pipelines#preview)) |
| `POST` | `/api/pipelines/{id}/reset-position` | Reset the source position |
| `GET` | `/api/pipelines/{id}/checkpoints` | Saved source positions |
| `GET` | `/api/pipelines/{id}/ingest-keys` | List the pipeline's ingest keys (prefix only) |
| `POST` | `/api/pipelines/{id}/ingest-keys` | Create an ingest key; the full key is in this response only |
| `DELETE` | `/api/pipelines/{id}/ingest-keys/{keyId}` | Revoke an ingest key |
| `GET` | `/api/pipelines/{id}/ingest-stats` | Live ingest counters |
| `GET` | `/api/ingest/info` | URLs shippers should use, `base_url`, `es_url` and `hec_url` (any signed-in user) |

Creating, editing, deleting, starting and stopping, test, preview, reset and
ingest keys need the admin or analyst role. Starting or previewing a pipeline
with a Sample, Redact or Route node needs **Pro**. The webhook source endpoint
and the ingest receivers are listed under [Webhooks](#webhooks).

### Admin

Every `/api/admin/*` route requires the admin role.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/admin/stats` | System statistics (users, connections, queries) |
| `GET` | `/api/admin/users` | List users with login history |
| `GET` | `/api/admin/user-roles` | Get role overrides |
| `PUT` | `/api/admin/user-roles/{username}` | Set role override (body `role`, optional `connection_id`, default the current connection; SSO overrides apply on all connections) |
| `DELETE` | `/api/admin/user-roles/{username}` | Remove role override (optional `?connection_id=`, default the current connection) |
| `DELETE` | `/api/admin/users/sso/{email}/access` | Drop an SSO person's per-person ClickHouse users on every connection and sign them out (admin) |
| `GET` | `/api/admin/connections` | List connections with tunnel status |
| `GET` | `/api/admin/clickhouse-users` | Query system.users from ClickHouse |
| `POST` | `/api/admin/clickhouse-users` | Create ClickHouse user |
| `PUT` | `/api/admin/clickhouse-users/{username}/password` | Change password |
| `DELETE` | `/api/admin/clickhouse-users/{username}` | Drop ClickHouse user |
| `GET` | `/api/admin/brain/providers` | List brain providers |
| `POST` | `/api/admin/brain/providers` | Create brain provider |
| `PUT` | `/api/admin/brain/providers/{id}` | Update brain provider |
| `DELETE` | `/api/admin/brain/providers/{id}` | Delete brain provider |
| `POST` | `/api/admin/brain/providers/{id}/sync-models` | Sync models from provider |
| `GET` | `/api/admin/brain/models` | List all models (all states) |
| `PUT` | `/api/admin/brain/models/{id}` | Update model flags |
| `POST` | `/api/admin/brain/models/bulk` | Bulk model action |
| `GET` | `/api/admin/brain/skills` | List skills |
| `POST` | `/api/admin/brain/skills` | Create skill |
| `PUT` | `/api/admin/brain/skills/{id}` | Update skill |
| `GET` | `/api/admin/brain/settings` | Model roles, output limit, prices |
| `PUT` | `/api/admin/brain/settings` | Save model roles, output limit, prices (partial body) |
| `POST` | `/api/admin/brain/providers/{id}/models` | Add a model id by hand |
| `GET` | `/api/admin/brain/usage` | Token and cost ledger (`?days=` 1 to 366, default 30; `?group_by=person\|role`) |
| `GET` | `/api/admin/brain/budgets` | Brain budget rules, each with its use this period |
| `POST` | `/api/admin/brain/budgets` | Create a budget rule (role, person and dollar rules need **Pro**) |
| `PUT` | `/api/admin/brain/budgets/{id}` | Replace a budget rule (role, person and dollar rules need **Pro**) |
| `DELETE` | `/api/admin/brain/budgets/{id}` | Delete a budget rule |
| `GET` | `/api/admin/governance/settings` | Governance background sync toggle |
| `PUT` | `/api/admin/governance/settings` | Turn governance sync on or off (turning it on needs **Pro**) |
| `GET` | `/api/admin/retention` | Data retention config, defaults and last run |
| `PUT` | `/api/admin/retention` | Update retention windows (partial body) |
| `GET` | `/api/admin/sso` | SSO config, secret never returned (**Pro**) |
| `PUT` | `/api/admin/sso` | Save SSO config (**Pro**) |
| `GET` | `/api/admin/github/{connectionId}` | GitHub model sync settings |
| `PUT` | `/api/admin/github/{connectionId}` | Save GitHub sync settings, including the optional `webhook_secret` (16+ characters; the push webhook is refused until one is set) (**Pro**) |
| `DELETE` | `/api/admin/github/{connectionId}` | Remove GitHub sync (**Pro**) |
| `POST` | `/api/admin/github/{connectionId}/test` | Test the GitHub connection (**Pro**) |
| `POST` | `/api/admin/github/{connectionId}/sync` | Sync models now; `409` while a sync is running (**Pro**) |
| `GET` | `/api/admin/github/{connectionId}/logs` | GitHub sync log |

### MCP Keys

Admin only, since keys carry ClickHouse credentials.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/mcp-keys` | List MCP keys and OAuth grants |
| `POST` | `/api/mcp-keys` | Create MCP key (secret returned once) |
| `POST` | `/api/mcp-keys/{id}/rotate` | Rotate a key in place |
| `DELETE` | `/api/mcp-keys/{id}` | Revoke a key or grant |

## Pro API Groups

These endpoints need an active Pro license (a license with the `enterprise`
edition unlocks the same). Without one they return `402 Payment Required`.
During the 14-day grace period after expiry, reads keep working and writes
return `402` with `"status": "grace"`; see
[What happens at expiry](/docs/license#what-happens-at-expiry).

Outside these groups, a few endpoints are Pro on their own:
`POST /api/saved-queries/{id}/run`, `POST /api/brain/generate-sql`,
`/api/admin/sso`, the write routes under `/api/admin/github/*`,
`POST /api/github/webhook/{connectionId}`, `/api/auth/oidc/*`, the traces,
metrics, service map and monitor routes under `/api/telemetry`, turning
governance sync on through `PUT /api/admin/governance/settings`, and Brain
budget rules per role or person or with a dollar limit
(`POST` and `PUT /api/admin/brain/budgets`).

### Schedules

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/schedules` | List the schedules of the session's connection |
| `GET` | `/api/schedules/{id}` | Get schedule |
| `POST` | `/api/schedules` | Create schedule (admin or analyst) |
| `PUT` | `/api/schedules/{id}` | Update schedule (admin or analyst) |
| `DELETE` | `/api/schedules/{id}` | Delete schedule (admin or analyst) |
| `POST` | `/api/schedules/{id}/run` | Manual execution with the caller's credentials (admin or analyst) |
| `GET` | `/api/schedules/{id}/runs` | Run history |

Every schedule route only reaches schedules of the session's connection; one on
another connection answers `404`. A new schedule runs on the session's
connection, and a `connection_id` for any other connection answers `400`.

### Cluster Health

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/cluster-health/summary` | Per-node health summary |
| `GET` | `/api/cluster-health/replication` | Replication state |
| `GET` | `/api/cluster-health/replication-queue` | Replication queue |
| `GET` | `/api/cluster-health/merges` | Running merges |
| `GET` | `/api/cluster-health/mutations` | Mutations |
| `GET` | `/api/cluster-health/parts` | Parts pressure |
| `GET` | `/api/cluster-health/disks` | Disks |
| `GET` | `/api/cluster-health/keeper` | Keeper |
| `GET` | `/api/cluster-health/backups` | Backups |
| `GET` | `/api/cluster-health/long-queries` | Long-running queries |
| `GET` | `/api/cluster-health/history` | Collected history for charts |
| `GET` | `/api/cluster-health/settings` | Collection settings |
| `PUT` | `/api/cluster-health/settings` | Update collection settings (admin) |

See [Cluster Health](/docs/cluster-health).

### Query Insights

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/query-insights/{section}` | `system.query_log` analytics. `section` is one of `summary`, `volume`, `latency`, `slow`, `memory`, `frequent`, `errors`, `users`, `tables` |

See [Query Insights](/docs/query-insights).

### Costs

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/costs/config` | Pricing and team mapping |
| `PUT` | `/api/costs/config` | Save pricing and team mapping |
| `GET` | `/api/costs/{section}` | Cost data. `section` is one of `summary`, `trend`, `users`, `queries`, `storage` |

See [Cost Center](/docs/cost-center).

### Governance

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/governance/overview` | Summary stats |
| `POST` | `/api/governance/sync` | Trigger full sync |
| `POST` | `/api/governance/sync/{type}` | Trigger single sync (metadata/query_log/access) |
| `GET` | `/api/governance/sync/status` | Sync state for all workers: `status`, `last_synced_at` (last success), `last_attempt_at`, `consecutive_failures`, `ch_user`, `last_error` |
| `GET` | `/api/governance/databases` | List databases |
| `GET` | `/api/governance/tables` | List tables (`?database=`) |
| `GET` | `/api/governance/tables/{db}/{table}` | Table detail + columns + tags + recent queries |
| `GET` | `/api/governance/tables/{db}/{table}/notes` | Table notes |
| `GET` | `/api/governance/tables/{db}/{table}/columns/{col}/notes` | Column notes |
| `POST` | `/api/governance/tables/{db}/{table}/notes` | Create table note (admin) |
| `POST` | `/api/governance/tables/{db}/{table}/columns/{col}/notes` | Create column note (admin) |
| `DELETE` | `/api/governance/notes/{id}` | Delete note (admin) |
| `PUT` | `/api/governance/tables/{db}/{table}/comment` | Update table comment (admin) |
| `PUT` | `/api/governance/tables/{db}/{table}/columns/{col}/comment` | Update column comment (admin) |
| `GET` | `/api/governance/schema-changes` | List schema changes |
| `GET` | `/api/governance/query-log` | Ingested query log |
| `GET` | `/api/governance/query-log/{query_id}` | One ingested query by ClickHouse `query_id` |
| `GET` | `/api/governance/query-harvest` | Query harvest mode and the connection's policy count |
| `PUT` | `/api/governance/query-harvest` | Set the query harvest mode (admin) |
| `GET` | `/api/governance/tags` | List tags |
| `POST` | `/api/governance/tags` | Create tag (admin) |
| `DELETE` | `/api/governance/tags/{id}` | Delete tag on the current connection (admin) |
| `GET` | `/api/governance/access/users` | List ClickHouse users |
| `POST` | `/api/governance/access/users` | Create ClickHouse user (admin) |
| `DELETE` | `/api/governance/access/users/{name}` | Drop ClickHouse user (admin) |
| `GET` | `/api/governance/access/roles` | List roles |
| `GET` | `/api/governance/access/matrix` | Access matrix (`?user=`) |
| `GET` | `/api/governance/access/over-permissions` | Unused privileges (`?days=`) |
| `GET` | `/api/governance/policies` | List policies (admin) |
| `POST` | `/api/governance/policies` | Create policy (admin) |
| `GET` | `/api/governance/policies/{id}` | Get policy (admin) |
| `PUT` | `/api/governance/policies/{id}` | Update policy (admin) |
| `DELETE` | `/api/governance/policies/{id}` | Delete policy (admin) |
| `GET` | `/api/governance/violations` | List violations |
| `POST` | `/api/governance/violations/{id}/incident` | Create incident from violation (admin) |
| `GET` | `/api/governance/incidents` | List incidents |
| `GET` | `/api/governance/incidents/{id}` | Get incident |
| `POST` | `/api/governance/incidents` | Create manual incident (admin) |
| `PUT` | `/api/governance/incidents/{id}` | Update incident (admin) |
| `GET` | `/api/governance/incidents/{id}/comments` | List incident comments |
| `POST` | `/api/governance/incidents/{id}/comments` | Add incident comment (admin) |
| `GET` | `/api/governance/audit-logs` | Query audit log (admin) |
| `GET` | `/api/governance/audit-logs/export` | Export audit log as CSV/JSON (admin, `?format=csv\|json`) |

### Access control

Admin only. Changes ClickHouse access control through a reviewed plan; see
[Access Control Editor](/docs/rbac#api) for request bodies.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/rbac/snapshot` | Roles, users, grants, row policies, quotas, profiles and your capabilities |
| `GET` | `/api/rbac/entity/{kind}/{name}` | `SHOW CREATE` (and `SHOW GRANTS`) for one entity |
| `GET` | `/api/rbac/effective` | Explain one user's access to an object |
| `GET` | `/api/rbac/who-can` | Every user holding a privilege on an object |
| `POST` | `/api/rbac/plan` | Build statements, diffs and warnings; changes nothing (read) |
| `POST` | `/api/rbac/apply` | Apply a plan (`402` during the grace period) |
| `GET` | `/api/rbac/export` | Download the access file (`?format=yaml\|json\|sql`) |
| `POST` | `/api/rbac/import/plan` | Plan the import of an access file; changes nothing (read) |
| `GET` | `/api/rbac/history` | Access history, newest first (`?drift=only&limit=&cursor=`) |
| `GET` | `/api/rbac/history/{id}` | One snapshot with its stored state |
| `GET` | `/api/rbac/history/{id}/diff` | Changes against `?against=previous\|live\|<id>` |
| `POST` | `/api/rbac/history/snapshot` | Store the live state now (`402` during the grace period) |
| `POST` | `/api/rbac/history/{id}/revert-plan` | The changes that bring the server back to a snapshot; changes nothing (read) |

### Governance Alerts

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/governance/alerts/channels` | List alert channels |
| `POST` | `/api/governance/alerts/channels` | Create channel (admin) |
| `PUT` | `/api/governance/alerts/channels/{id}` | Update channel (admin) |
| `DELETE` | `/api/governance/alerts/channels/{id}` | Delete channel (admin) |
| `POST` | `/api/governance/alerts/channels/{id}/test` | Test channel delivery (admin) |
| `GET` | `/api/governance/alerts/rules` | List rules (with routes) |
| `POST` | `/api/governance/alerts/rules` | Create rule (admin) |
| `PUT` | `/api/governance/alerts/rules/{id}` | Update rule (admin) |
| `DELETE` | `/api/governance/alerts/rules/{id}` | Delete rule (admin) |
| `GET` | `/api/governance/alerts/events` | List alert events |

### Telemetry (Pro)

| Method | Path | Purpose |
|---|---|---|
| `POST` | `/api/telemetry/traces/search` | Search traces |
| `POST` | `/api/telemetry/traces/histogram` | Trace counts and p50/p95 durations over time |
| `POST` | `/api/telemetry/traces/facets` | Top values for facet fields |
| `GET` | `/api/telemetry/traces/{traceId}` | Full trace: span tree, events, links |
| `GET` | `/api/telemetry/metrics/catalog` | Metric names, types, units, descriptions |
| `POST` | `/api/telemetry/metrics/query` | Aggregate a metric into series |
| `GET` | `/api/telemetry/metrics/attributes` | Attribute keys for group-by and filters |
| `POST` | `/api/telemetry/service-map` | Services and the calls between them |
| `GET` | `/api/telemetry/monitors` | List monitors |
| `POST` | `/api/telemetry/monitors` | Create monitor |
| `PUT` | `/api/telemetry/monitors/{id}` | Update monitor |
| `DELETE` | `/api/telemetry/monitors/{id}` | Delete monitor |
| `POST` | `/api/telemetry/monitors/{id}/run` | Evaluate a monitor now |

### Performance

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/performance/regressions` | Compare query patterns against the previous window (`?range=1h\|6h\|24h\|7d`, default `24h`; optional `?cluster=`), with the caller's grants |
| `GET` | `/api/performance/investigations` | List saved investigations for the active connection |
| `GET` | `/api/performance/investigations/{id}` | Investigation with its event history |
| `POST` | `/api/performance/investigations` | Create investigation and capture the baseline (admin or analyst) |
| `PUT` | `/api/performance/investigations/{id}` | Update title, owner or status (`open`, `monitoring`, `resolved`) and add a note; stale revisions get `409` (admin or analyst) |
| `POST` | `/api/performance/investigations/{id}/compare` | Capture an observed comparison (admin or analyst) |
| `GET` | `/api/performance/monitor` | Hourly background scan settings and latest report (admin) |
| `PUT` | `/api/performance/monitor` | Enable or disable hourly background scans (admin) |

See [Performance](/docs/performance).

### Fleet

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/fleet` | Every connection with availability, latest Cluster Health poll, open incidents and latest background performance scan (admin) |

### Schema Compare

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/schema-compare/connections` | Connections available to compare |
| `POST` | `/api/schema-compare/compare` | Compare two connection/database pairs; credentials for another connection are sent in the body and never stored |

Any signed-in user can compare. Both routes are reads, so they keep working
during the grace period, `POST /compare` included. See [Schema Compare](/docs/schema-compare).

### Incident Timeline

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/incident-timeline` | Combined timeline for a window of up to seven days (`?from=&to=`, optional `?incident_id=`) (admin) |
| `POST` | `/api/incident-timeline/annotations` | Add a deployment annotation (admin or analyst) |
| `DELETE` | `/api/incident-timeline/annotations/{id}` | Remove an annotation: your own as analyst, any on the current connection as admin |

See [Incident Timeline](/docs/incident-timeline).

### Operations Reports

All routes are admin-only, reads included.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/operations-reports` | List saved reports |
| `GET` | `/api/operations-reports/settings` | Weekly schedule, email channel and recipients |
| `PUT` | `/api/operations-reports/settings` | Save settings (enabling the schedule needs a dedicated background account) |
| `GET` | `/api/operations-reports/channels` | Email channels that can deliver reports |
| `POST` | `/api/operations-reports/generate` | Generate a report now with the caller's ClickHouse account |
| `POST` | `/api/operations-reports/{id}/send` | Queue an email of a saved report |

See [Operations Reports](/docs/operations-reports).

## Example: Login + Run Query

`clickhouse_url` in the login body is optional and only accepted when the
server runs with `allow_login_url` on (off by default, see
[Configuration](/docs/configuration/#sign-in-with-a-clickhouse-url)). The server
reuses or creates a direct connection for that URL and signs in against it.
Sending it with `connectionId` returns `400`, as does a URL that fails the
first-run setup URL checks. Without `clickhouse_url`, send `connectionId`:
leaving it out only works while the login page lists a single connection, and
otherwise answers `400`. Sending it while the setting is off returns `403`.
When the login page has already created 20 connections and the URL matches
none of the saved ones, the response is `429`.

```bash
# 1) Login
curl -i -X POST http://localhost:3488/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username":"default",
    "password":"secret",
    "connectionId":"<connection-id>"
  }'

# Or, when allow_login_url is on, sign in with a URL instead of a connection:
#   -d '{"username":"default","password":"secret","clickhouse_url":"http://clickhouse:8123"}'

# 2) Reuse set-cookie value as chui_session
curl -X POST http://localhost:3488/api/query/run \
  -H "Content-Type: application/json" \
  -H "Cookie: chui_session=<session-token>" \
  -d '{"query":"SELECT version()"}'
```
