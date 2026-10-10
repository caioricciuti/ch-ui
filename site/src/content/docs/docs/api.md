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
| `GET` | `/api/auth/config` | Available login methods (password, SSO) and whether first-run setup is open (`setup_open`) |
| `POST` | `/api/auth/setup` | First-run setup: add a direct connection with the one-time setup code (see below) |
| `POST` | `/api/auth/login` | Session login |
| `POST` | `/api/auth/logout` | Session logout |
| `GET` | `/api/auth/session` | Session details |
| `GET` | `/api/auth/connections` | Login-time connection list |
| `POST` | `/api/auth/switch-connection` | Switch active connection |
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

### Query History

Per person and per connection. SSO users who share a ClickHouse account see
only their own entries.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/query-history` | Your history, newest first (`?search=&status=&limit=&offset=`) |
| `DELETE` | `/api/query-history/{id}` | Delete one entry |
| `DELETE` | `/api/query-history` | Clear your history on the current connection |

### Connections

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/connections` | List connections |
| `POST` | `/api/connections` | Create connection (admin) |
| `GET` | `/api/connections/{id}` | Get connection |
| `PUT` | `/api/connections/{id}` | Update connection (admin) |
| `DELETE` | `/api/connections/{id}` | Delete connection (admin) |
| `POST` | `/api/connections/{id}/test` | Test connection |
| `GET` | `/api/connections/{id}/token` | Get tunnel token (admin) |
| `POST` | `/api/connections/{id}/regenerate-token` | Regenerate tunnel token (admin) |
| `PUT` | `/api/connections/{id}/sso-account` | Set the SSO ClickHouse service account (admin) |
| `GET` | `/api/connections/{id}/background-credentials` | Mode and account name for every background worker (admin, passwords never returned) |
| `PUT` | `/api/connections/{id}/background-credentials/{worker}` | Set a worker's mode: `service_account`, `session` or `disabled` (admin) |

Background accounts are available in every edition. `{worker}` is one of
`schedule`, `model`, `pipeline`, `governance`, `cluster_health`,
`telemetry.monitor`, `performance`, `operations.report`. The `PUT` body is
`{"mode": "...", "username": "...", "password": "..."}`; for
`service_account` CH-UI runs `SELECT 1` with the account before saving. See
[Background Accounts](/docs/background-accounts).

### Saved Queries

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/saved-queries` | List saved queries |
| `POST` | `/api/saved-queries` | Create saved query |
| `GET` | `/api/saved-queries/{id}` | Get saved query |
| `PUT` | `/api/saved-queries/{id}` | Update saved query (also sets `verified`) |
| `DELETE` | `/api/saved-queries/{id}` | Delete saved query |
| `POST` | `/api/saved-queries/{id}/duplicate` | Duplicate saved query |
| `POST` | `/api/saved-queries/{id}/run` | Run with parameters, result as JSON (**Pro**, see [Query Parameters](/docs/querying#running-a-saved-query-via-the-api)) |

Create, update, delete and duplicate need the admin or analyst role. Viewers
can list, read and run.

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
| `POST` | `/api/telemetry/logs/search` | Search logs |
| `POST` | `/api/telemetry/logs/histogram` | Log counts over time by severity |
| `POST` | `/api/telemetry/logs/facets` | Top values for facet fields |
| `POST` | `/api/telemetry/logs/context` | Rows around a log line |
| `GET` | `/api/telemetry/logs/by-trace/{traceId}` | Logs of one trace |
| `GET` | `/api/telemetry/saved-searches` | List saved searches |
| `POST` | `/api/telemetry/saved-searches` | Save a search |
| `PUT` | `/api/telemetry/saved-searches/{id}` | Update saved search |
| `DELETE` | `/api/telemetry/saved-searches/{id}` | Delete saved search |

`GET` and `PUT /api/telemetry/config` (the single-table config from before
v2.11.0) now return `410 Gone`.

### Brain

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/brain/models` | List active models |
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
| `POST` | `/api/models/run` | Execute all models |
| `POST` | `/api/models/{id}/run` | Execute single model + upstream deps |
| `GET` | `/api/models/dag` | Get DAG for visualization |
| `GET` | `/api/models/runs` | List runs (`?limit=&offset=`) |
| `GET` | `/api/models/runs/{runId}` | Get run + per-model results |
| `GET` | `/api/models/pipelines` | List pipelines (connected components) |
| `POST` | `/api/models/pipelines/{anchorId}/run` | Execute pipeline |
| `GET` | `/api/models/schedules` | List model schedules |
| `GET` | `/api/models/schedule/{anchorId}` | Get pipeline schedule |
| `PUT` | `/api/models/schedule/{anchorId}` | Create/update pipeline schedule |
| `DELETE` | `/api/models/schedule/{anchorId}` | Delete pipeline schedule |

### Pipelines (Data Ingestion)

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/pipelines` | List pipelines |
| `POST` | `/api/pipelines` | Create pipeline |
| `GET` | `/api/pipelines/{id}` | Get pipeline |
| `PUT` | `/api/pipelines/{id}` | Update pipeline |
| `DELETE` | `/api/pipelines/{id}` | Delete pipeline |
| `POST` | `/api/pipelines/{id}/start` | Start pipeline |
| `POST` | `/api/pipelines/{id}/stop` | Stop pipeline |
| `PUT` | `/api/pipelines/{id}/graph` | Save the whole graph (nodes and edges) |
| `GET` | `/api/pipelines/{id}/status` | Get pipeline status + metrics |
| `GET` | `/api/pipelines/{id}/runs` | List runs (`?limit=&offset=`) |
| `GET` | `/api/pipelines/{id}/runs/{runId}/logs` | Logs of one run |

Creating, editing, deleting, starting and stopping need the admin or analyst
role. The webhook source endpoint is listed under [Webhooks](#webhooks).

### Admin

Every `/api/admin/*` route requires the admin role.

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/admin/stats` | System statistics (users, connections, queries) |
| `GET` | `/api/admin/users` | List users with login history |
| `GET` | `/api/admin/user-roles` | Get role overrides |
| `PUT` | `/api/admin/user-roles/{username}` | Set role override |
| `DELETE` | `/api/admin/user-roles/{username}` | Remove role override |
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
| `GET` | `/api/admin/governance/settings` | Governance background sync toggle |
| `PUT` | `/api/admin/governance/settings` | Turn governance sync on or off (turning it on needs **Pro**) |
| `GET` | `/api/admin/retention` | Data retention config, defaults and last run |
| `PUT` | `/api/admin/retention` | Update retention windows (partial body) |
| `GET` | `/api/admin/sso` | SSO config, secret never returned (**Pro**) |
| `PUT` | `/api/admin/sso` | Save SSO config (**Pro**) |
| `GET` | `/api/admin/github/{connectionId}` | GitHub model sync settings |
| `PUT` | `/api/admin/github/{connectionId}` | Save GitHub sync settings (**Pro**) |
| `DELETE` | `/api/admin/github/{connectionId}` | Remove GitHub sync (**Pro**) |
| `POST` | `/api/admin/github/{connectionId}/test` | Test the GitHub connection (**Pro**) |
| `POST` | `/api/admin/github/{connectionId}/sync` | Sync models now (**Pro**) |
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
metrics, service map and monitor routes under `/api/telemetry`, and turning
governance sync on through `PUT /api/admin/governance/settings`.

### Schedules

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/schedules` | List schedules |
| `GET` | `/api/schedules/{id}` | Get schedule |
| `POST` | `/api/schedules` | Create schedule (admin or analyst) |
| `PUT` | `/api/schedules/{id}` | Update schedule (admin or analyst) |
| `DELETE` | `/api/schedules/{id}` | Delete schedule (admin or analyst) |
| `POST` | `/api/schedules/{id}/run` | Manual execution with the caller's credentials (admin or analyst) |
| `GET` | `/api/schedules/{id}/runs` | Run history |

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
| `GET` | `/api/governance/sync/status` | Sync state for all workers |
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
| `POST` | `/api/governance/tags` | Create tag |
| `DELETE` | `/api/governance/tags/{id}` | Delete tag |
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

### Governance Alerts

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/governance/alerts/channels` | List alert channels |
| `POST` | `/api/governance/alerts/channels` | Create channel |
| `PUT` | `/api/governance/alerts/channels/{id}` | Update channel |
| `DELETE` | `/api/governance/alerts/channels/{id}` | Delete channel |
| `POST` | `/api/governance/alerts/channels/{id}/test` | Test channel delivery |
| `GET` | `/api/governance/alerts/rules` | List rules (with routes) |
| `POST` | `/api/governance/alerts/rules` | Create rule |
| `PUT` | `/api/governance/alerts/rules/{id}` | Update rule |
| `DELETE` | `/api/governance/alerts/rules/{id}` | Delete rule |
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

```bash
# 1) Login
curl -i -X POST http://localhost:3488/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{
    "username":"default",
    "password":"secret",
    "connectionId":"<connection-id>"
  }'

# 2) Reuse set-cookie value as chui_session
curl -X POST http://localhost:3488/api/query/run \
  -H "Content-Type: application/json" \
  -H "Cookie: chui_session=<session-token>" \
  -d '{"query":"SELECT version()"}'
```
