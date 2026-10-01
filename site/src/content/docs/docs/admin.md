---
title: Admin Guide
description: Users, ClickHouse user management, Brain providers, and operational controls
---

Admin is where operators manage users, connections, AI providers, and system configuration.

Admin is part of the open-source core: no Pro license is needed and nothing in it is limited by plan. It is only visible to users with the `admin` role. A few settings inside it (SSO, GitHub sync) need Pro.

## Sections

Open **Admin** from the Settings group in the rail. Its sections are listed in the sidebar, and each one has its own URL (`/admin?section=...`):

| Section | What it holds |
|---|---|
| **Overview** | System statistics and the cluster topology |
| **Connections** | Every ClickHouse connection, direct or remote agent, with its status; details open in a side sheet |
| **Users** | Application users with role overrides, and ClickHouse users |
| **Brain** | AI providers, models and skills |
| **GitHub** | GitHub sync for models, per connection |
| **MCP Server** | Endpoint, client snippets, MCP keys and OAuth grants |
| **Settings** | Data retention and SSO |

## System Statistics

The Overview section shows key metrics:

| Metric | Description |
|---|---|
| Users Count | Total users with sessions |
| Connections | Total ClickHouse connections |
| Online | Connections with active tunnels |
| Login Count | Total login events |
| Query Count | Total queries executed |

```bash
GET /api/admin/stats
```

## Connections Overview

View all ClickHouse connections with their type (Embedded for the one from server config, Direct or Agent), target, online status, creation date, and last seen timestamp.

**Add connection** defaults to **Direct URL**: give it a name and a ClickHouse HTTP(S) URL the server can reach, and it connects right away. Choose **Remote agent** instead for a ClickHouse behind a firewall; CH-UI then shows a token to run `ch-ui connect` with next to that server. Agent rows have buttons to show or regenerate the token. See [Direct vs tunnel connections](/docs/connections).

Each connection row has a **Background accounts** button. It sets which ClickHouse account unattended jobs (schedules, models, pipeline sinks, governance, Cluster Health, telemetry monitors, performance monitoring and weekly reports) use on that connection. See [Background Accounts](/docs/background-accounts).

```bash
GET /api/admin/connections
```

## Users

CH-UI provides two user views:

- **Application users**: active users with role overrides and login history
- **ClickHouse users**: live users from the connected ClickHouse instance

People who sign in with [SSO](/docs/sso) are listed one row per person, by
email, with an **SSO** badge and the ClickHouse service account they query as.
Their role override applies to that person only. An override set on the service
account name applies to password logins as that account, not to SSO people.

### Role Overrides

Set CH-UI role overrides to control application-level permissions:

| Role | Access |
|---|---|
| `admin` | Full access: schema operations, user management, brain providers, governance policies |
| `analyst` | Query execution, saved queries, dashboards, brain chat |
| `viewer` | Read-only access |

```bash
# Set role
PUT /api/admin/user-roles/{username}
{ "role": "admin" }

# Remove override (reverts to viewer)
DELETE /api/admin/user-roles/{username}
```

For an SSO person, `{username}` is `sso:<email>`, URL-encoded (for example
`sso%3Aana%40example.com`). `GET /api/admin/users` returns this key as
`username`, plus `display_name`, `clickhouse_user` and `via_sso`.

Safety: cannot remove the last admin role. Role changes refresh active sessions immediately.

## ClickHouse User Management

Manage ClickHouse users directly from the admin UI.

### Create User

```bash
POST /api/admin/clickhouse-users
{
  "name": "analyst_user",
  "password": "secure_password",
  "auth_type": "sha256_password",
  "default_roles": ["analyst_role"],
  "if_not_exists": true
}
```

| Field | Description | Default |
|---|---|---|
| `name` | Username | Required |
| `password` | Password (empty for `no_password`) | None |
| `auth_type` | `no_password`, `plaintext_password`, `sha256_password`, `double_sha1_password` | Inferred from password |
| `default_roles` | Array of role names or `["ALL"]` | None |
| `if_not_exists` | Skip if user exists | `false` |

When `auth_type` is omitted, it's inferred: `sha256_password` if a password is provided, `no_password` otherwise.

The operation generates up to three commands: `CREATE USER`, `GRANT` roles, and `ALTER USER SET DEFAULT ROLE`.

### Change Password

```bash
PUT /api/admin/clickhouse-users/{username}/password
{
  "password": "new_password",
  "auth_type": "sha256_password",
  "if_exists": true
}
```

### Delete User

```bash
DELETE /api/admin/clickhouse-users/{username}?if_exists=true
```

Safety: cannot delete the current session's ClickHouse user.

## Data Retention

CH-UI's SQLite database accumulates append-only history (audit logs, alert events, run logs). The retention manager prunes each table on its own window. **Admin → Settings** shows a status table (what each table holds, its window, the default, and the last cleanup), and **Edit retention** opens a sheet where you change the windows or reset them to defaults. The same is available through the API:

```bash
GET /api/admin/retention
PUT /api/admin/retention
```

`GET` returns the current config, the built-in defaults, and stats from the most recent run. `PUT` accepts partial bodies; omitted fields keep their current value.

| Table | Default window |
|---|---|
| `audit_logs` | 90 days |
| `alert_events` | 60 days |
| `alert_dispatch_jobs` | 30 days |
| `schedule_runs` | 60 days |
| `pipeline_runs` | 90 days |
| `pipeline_run_logs` | 30 days |
| `model_runs` | 90 days |
| `model_run_results` | 30 days |
| `github_sync_logs` | 30 days |
| `gov_schema_changes` | 180 days |
| `operations_reports` | 180 days |
| `incident_annotations` (`incident_deployment_annotations`, by when the deployment happened) | 180 days |
| `resolved_investigations` (`performance_investigations` that are resolved, by resolution time; open and monitoring ones are never pruned) | 180 days |

Set a window to `0` to keep that table forever (pruning disabled); the maximum is 3650 days. The background job runs hourly and deletes in small batches so it never holds the SQLite write lock for long. Config changes are recorded in the audit log (`retention.config_update`).

## SSO

SSO status and configuration also live in **Admin → Settings**: a status panel when SSO is on, and a **Configure SSO** sheet with the fields grouped by provider, role mapping and service account. See [Single Sign-On](/docs/sso).

## MCP Server

**Admin → MCP Server** manages the embedded MCP endpoint: counters for active keys, OAuth grants, keys expiring soon and keys used in the last day, the endpoint URL with config snippets per client, and the tables of API keys and OAuth grants where you create, rotate and revoke access. See [MCP Server](/docs/mcp).

## Brain configuration

Brain's AI configuration (providers, models, and **skills**) lives in the **Admin → Brain** section, restricted to admins. Configure a provider (OpenAI, OpenAI-compatible, or Ollama), sync and activate models, and author skills there.

See [Brain → Configuring AI](/docs/brain#configuring-ai) for providers/models and [Brain → Skills](/docs/brain#skills) for authoring reusable expertise.

## Operational Commands

```bash
ch-ui server status
ch-ui server restart
ch-ui server stop

ch-ui service status
ch-ui service restart
```

## Cluster Topology

The Overview section includes a **Cluster Topology** panel that surfaces what `system.clusters` reports for the connection: every shard, every replica, with host / address / port / shard_num / replica_num / is_local. A green dot marks the node that's currently serving your session. Single-node deployments show "Single-node setup detected" with just the hostname.

For multi-pod setups behind a load balancer, see [ClickHouse Clusters & Load Balancers](/docs/clusters) for the sticky-routing config that keeps your session pinned to one pod.

## Production Notes

- Keep at least one admin override account
- Rotate provider API keys periodically
- Rotate `APP_SECRET_KEY` per environment (encrypts API keys and session credentials)
- Keep governance sync healthy before relying on policy alerts
- Test alert channels before enabling rules
