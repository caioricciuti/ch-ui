---
title: Admin Guide
description: Users, ClickHouse user management, Brain providers, and operational controls
---

Admin is where operators manage users, connections, AI providers, and system configuration.

Admin is part of the open-source core: no Pro license is needed and nothing in it is limited by plan. It is only visible to users with the `admin` role. A few settings inside it (SSO, GitHub sync, Brain budgets per role or person and dollar limits) need Pro.

## Sections

Open **Admin** from the Settings group in the rail. Its sections are listed in the sidebar, and each one has its own URL (`/admin?section=...`):

| Section | What it holds |
|---|---|
| **Overview** | System statistics and the cluster topology |
| **Connections** | Every ClickHouse connection, direct or remote agent, with its status; details open in a side sheet |
| **Users** | Application users with role overrides, and ClickHouse users |
| **Brain** | AI providers, models, model roles, prices and the system prompt |
| **Brain budgets** | Token and dollar limits on Brain per person, role or everyone |
| **Brain usage** | Tokens and cost per person or role and model |
| **GitHub** | GitHub sync for models, per connection |
| **MCP Server** | Connecting a client as yourself (OAuth), service keys, and the OAuth grants and keys in one table |
| **Settings** | Data retention and SSO |

The Connections section's id is `tunnels`; `/admin?section=connections` opens it too.

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

**Edit** on a row opens a sheet. Direct connections can be renamed and pointed at another ClickHouse URL; saving a new URL restarts that connection's connector. Agent connections can only be renamed, and the agent and its token keep working. The embedded connection has no Edit or Delete: it shows **Server config**, because its name and URL come from `clickhouse_url` and `connection_name` in the server config (hover the label to see that).

Each connection row has a **Background accounts** button. It sets which ClickHouse account unattended jobs (schedules, models, pipeline sinks, governance, Cluster Health, telemetry monitors, performance monitoring and weekly reports) use on that connection. See [Background Accounts](/docs/background-accounts).

Each row also has an **SSO** button: the ClickHouse account SSO sessions use on that connection, and whether SSO people share it or get [their own ClickHouse users](/docs/sso#per-person-clickhouse-users), with the group to role mapping, **Show setup SQL** and **Verify**.

```bash
GET /api/admin/connections
```

## Users

Adding a person? [Invite a teammate](/docs/guides/invite-teammate) is the short version of this section.

CH-UI provides two user views:

- **Application users**: active users with role overrides and login history
- **ClickHouse users**: live users from the connected ClickHouse instance

People who sign in with [SSO](/docs/sso) are listed one row per person, by
email, with an **SSO** badge and the ClickHouse account they query as: the
shared service account, or, on connections with
[per-person ClickHouse users](/docs/sso#per-person-clickhouse-users), one line
per connection with their own user, its roles, the last sync and a **locked**
badge once the sweep locked it. Their role override applies to that person
only. An override set on the service account name applies to password logins as
that account, not to SSO people.

**Remove access** on an SSO person drops their ClickHouse users on every
connection and signs them out at once (`DELETE /api/admin/users/sso/{email}/access`).
A connection whose agent is offline is reported in the response and its user
is kept for the hourly sweep to retire. Their IdP account is untouched: a later sign-in provisions them again,
with their current groups.

The application list shows the current connection: its password users and every
SSO person. Switch connection to manage another connection's users.

### Role Overrides

Set CH-UI role overrides to control application-level permissions:

| Role | Access |
|---|---|
| `admin` | Full access: schema operations, user management, brain providers, governance policies |
| `analyst` | Query execution, saved queries, dashboards, brain chat |
| `viewer` | Read-only access |

Overrides for ClickHouse users (password login) apply per connection: an admin
role on one connection does nothing for the same username on another. Overrides
for SSO people apply on all connections, since the identity provider verified
who they are. Connections added from the login page (sign-in with a ClickHouse
URL) take no overrides and never grant `admin`; the most a user gets there is
`analyst`.

```bash
# Set role (connection_id defaults to your current connection; ignored for sso:<email>)
PUT /api/admin/user-roles/{username}
{ "role": "admin", "connection_id": "<connection id>" }

# Remove override (reverts to the detected role)
DELETE /api/admin/user-roles/{username}?connection_id=<connection id>
```

`GET /api/admin/user-roles` lists every override with `connection_id` and
`connection_name`; `connection_id` is empty for SSO overrides.

For an SSO person, `{username}` is `sso:<email>`, URL-encoded (for example
`sso%3Aana%40example.com`). `GET /api/admin/users` returns this key as
`username`, plus `display_name`, `clickhouse_user` and `via_sso`.

Safety: cannot remove the last admin role. Role changes refresh active sessions immediately.

Lost the admin role, or nobody who has it can sign in? Run `ch-ui reset-admin` on the server. See [Recover the admin role](/docs/cant-login/#recover-the-admin-role).

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

### Roles and grants

Each ClickHouse user row has an **Edit grants** button. It opens the user in **Governance → Access control** (`/governance?section=rbac&user=<name>`), where roles, privileges, default roles, row policies, quotas and settings profiles are edited through a reviewed change set. Pro, admin only. See [Access Control Editor](/docs/rbac).

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
| `gov_query_log` (queries synced from ClickHouse for [governance](/docs/governance)) | 30 days |
| `gov_policy_violations` (queries that broke a governance policy) | 30 days |
| `rbac_snapshots` ([access history](/docs/rbac#access-history); at most 500 rows per connection whatever the window, and the newest is always kept) | 180 days |
| `alert_evaluations` ([evaluation history](/docs/alerts#evaluation-history) of health checks and SQL alerts) | 30 days |

The last four (v2.30.0+, fixed windows before that) are pruned by the job that owns each table, not by the hourly cleanup: the governance sync prunes the query log, violations and access history, access history is also pruned on every new snapshot, and the cluster health harvester prunes alert evaluations. They show the owning job under **Last cleanup** and are not counted in the run stats `GET` returns.

Set a window to `0` to keep that table forever (pruning disabled); the maximum is 3650 days. The background job runs hourly and deletes in small batches so it never holds the SQLite write lock for long. Config changes are recorded in the audit log (`retention.config_update`).

## SSO

SSO status and configuration also live in **Admin → Settings**: a status panel when SSO is on, and a **Configure SSO** sheet with the fields grouped by provider, role mapping and service account. See [Single Sign-On](/docs/sso).

## MCP Server

**Admin → MCP Server** manages the embedded MCP endpoint: counters for active keys, OAuth grants, keys expiring soon and keys used in the last day, the endpoint URL with config snippets per client, and the tables of service keys and OAuth grants where you create, rotate and revoke access. See [MCP Server](/docs/mcp).

## Brain configuration

Brain's AI configuration (providers, models, model roles, prices and **skills**) lives in the **Admin → Brain** section, restricted to admins. Configure a provider (Anthropic, OpenAI, OpenAI-compatible, or Ollama), sync and activate models, and author skills there.

**Admin → Brain budgets** (`/admin?section=brain-budgets`) lists the budget rules with each one's use this period, and **Admin → Brain usage** (`/admin?section=brain-usage`) shows tokens and cost by person or by role. Global token budgets work on every plan; role and person rules and dollar limits need Pro.

See [Brain → Configuring AI](/docs/brain#configuring-ai) for providers/models, [Brain → Budgets](/docs/brain#budgets) for limits and [Brain → Skills](/docs/brain#skills) for authoring reusable expertise.

## Operational Commands

```bash
ch-ui server status
ch-ui server restart
ch-ui server stop

ch-ui service status
ch-ui service restart

ch-ui reset-admin            # recover the admin role, see Can't login?
```

If no CH-UI admin can sign in, `ch-ui reset-admin` hands the role back. See [Can't login?](/docs/cant-login/#recover-the-admin-role) and the [CLI reference](/docs/cli/#reset-admin).

## Cluster Topology

The Overview section includes a **Cluster Topology** panel that surfaces what `system.clusters` reports for the connection: every shard, every replica, with host / address / port / shard_num / replica_num / is_local. A green dot marks the node that's currently serving your session. Single-node deployments show "Single-node setup detected" with just the hostname.

For multi-pod setups behind a load balancer, see [ClickHouse Clusters & Load Balancers](/docs/clusters) for the sticky-routing config that keeps your session pinned to one pod.

## Production Notes

- Keep at least one admin override account
- Rotate provider API keys periodically
- Use a different `APP_SECRET_KEY` per environment (it encrypts API keys, tokens and session credentials). To change it, follow [Rotating the app secret key](/docs/configuration#rotating-the-app-secret-key): replacing the key alone makes every stored secret unreadable
- Keep governance sync healthy before relying on policy alerts
- Test alert channels before enabling rules
