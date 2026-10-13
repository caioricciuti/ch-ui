---
title: Access Control Editor
description: Edit ClickHouse roles, grants, row policies, quotas and settings profiles with a reviewed, audited change set
---

The access control editor changes ClickHouse access entities from CH-UI: roles,
privilege and role grants, row policies, quotas and settings profiles. Every
change is staged first, previewed as the exact SQL that will run, checked for
risks, and applied in order with a result per statement.

It is a **Pro** feature for **CH-UI admins**, under
**Governance → Access control** (`/governance?section=rbac`).

:::note
This edits ClickHouse itself, not CH-UI's own `admin` / `analyst` / `viewer`
roles. For those, see [Admin → Users](/docs/admin#users). Read-only analysis of
grants (risky grants, the access matrix) stays under
[Governance → Access](/docs/governance#access-control).
:::

## What you can edit

| Entity | Actions |
|---|---|
| Roles | Create, drop, attach or clear a settings profile |
| Privileges | Grant or revoke on roles and existing users, at global, database, table or column level, with or without grant option |
| Role grants | Grant or revoke roles to users and roles, with or without admin option; set a user's default roles (all, none, or a list) |
| Row policies | Create, change, drop. `FOR SELECT`, permissive or restrictive, a condition, and who it applies to |
| Quotas | Create, change, drop. Key (`KEYED BY`), intervals with limits, and who it applies to |
| Settings profiles | Create, change, drop. Settings with a value, `MIN` / `MAX`, `CONST` / `WRITABLE` / `CHANGEABLE_IN_READONLY`, inherited profiles, and who it applies to |

Not covered: creating users, passwords and authentication (use
[Admin → Users](/docs/admin#clickhouse-user-management)), renaming entities,
LDAP / Kerberos / other external user directories, masking policies, and
applying one change set to several connections.

Entities defined in `users.xml` are listed but read-only; change them in the
server configuration.

## The page

A 40 px toolbar holds a selector for the entity kind (Roles, Users, Row
policies, Quotas, Settings profiles), a search box, **Pending changes (n)** once
something is staged, and the **New** button for the kind. The table lists each
entity with its storage (`local_directory`, `replicated`, `users_xml`), who it
is granted or applied to, and counts.

Clicking a row opens a sheet:

- **Role or user**: the settings profile (roles), privileges with a revoke
  button per grant, roles held with a revoke button, a form to add a privilege
  (picked from `system.privileges`, with database and table pickers and an
  optional column list), a form to grant a role, and default roles (users).
- **Row policy, quota, settings profile**: the full definition as a form, plus
  **Drop** for existing ones.

Nothing runs from a sheet. Each edit is **staged** into the change set for the
current browser tab. From **Admin → Users → ClickHouse**, the **Edit grants**
button on a user opens that user's sheet here (`?section=rbac&user=<name>`).

## Review and apply

**Pending changes (n)** opens the review:

1. **Pending changes**: every staged edit in plain words, each removable.
2. **Target**: runs on the connected node by default. **Run ON CLUSTER** with a
   cluster from `system.clusters` adds `ON CLUSTER` to every statement. It is
   off by default; leave it off for entities in `replicated` storage, which
   already reach every node.
3. **Warnings**: a **blocked** warning (red) refuses the plan until you remove
   the change. Every other warning needs its box ticked.
4. **Confirm drops**: each drop needs the entity name typed in.
5. **Changes by entity**: a side-by-side diff of each touched entity, before
   and after, one statement per line.
6. **Statements**: the numbered SQL, in the order it will run, each with the
   statement that would undo it.

**Apply** runs the statements one by one as your ClickHouse user and stops at
the first failure. The results list every statement as **applied**,
**failed** or **skipped**, with one row per host for `ON CLUSTER`. A `CREATE`
for a name that already exists fails with "already exists"; CH-UI never uses
`IF NOT EXISTS`, so an existing entity is never taken over silently. Drops use
`IF EXISTS`.

**Prepare rollback** stages the inverse of every applied statement as a new
change set and opens a fresh review. Nothing is rolled back automatically.

Statements always run in this order: create settings profiles, quotas, roles;
change roles, quotas and profiles; grant privileges; grant roles; create or
change row policies; default roles; revokes; then drop row policies, quotas,
roles and profiles.

### Safety checks

The server checks every plan, and again at apply time.

| Check | Effect |
|---|---|
| Revoking `ACCESS MANAGEMENT`, `ROLE ADMIN`, `SHOW USERS`, `SHOW ACCESS` or `ALL` from your own ClickHouse user, or from a role you hold | Blocked |
| Revoking from yourself a role that carries those privileges | Blocked |
| Dropping a role you hold | Blocked |
| A restrictive row policy, or a profile with `readonly` set, that would apply to you | Blocked |
| Changing an entity stored in `users.xml` | Blocked |
| Revoking what lets a user read `system.users` (CH-UI uses that to detect ClickHouse admins at sign-in) | Needs acknowledgement |
| Changing a user CH-UI runs unattended: a [background account](/docs/background-accounts), an [MCP key](/docs/mcp) account, or the [SSO](/docs/sso) service account | Needs acknowledgement |
| A new row policy, which can hide rows from users it does not mention (depending on ClickHouse's `users_without_row_policies_can_read_rows`) | Needs acknowledgement |
| `ON CLUSTER` while some entities use `replicated` storage | Needs acknowledgement |
| Revoking a harmless role from yourself, or changing your own default roles | Needs acknowledgement |

Row policy conditions are checked twice: a small lexer refuses `;`, comments
and unbalanced quotes or parentheses, and the server parses the condition
against the table with `EXPLAIN SYNTAX` as your user, so an unknown column is
reported in the review rather than at apply time.

The plan carries a hash of its statements and of the access state it was built
on. If anyone changes access control between the review and **Apply**, apply
returns `409` and the review rebuilds.

## Permissions

CH-UI side: admin only. ClickHouse side, for the user you signed in with (for
[SSO](/docs/sso), the connection's service account):

- `ACCESS MANAGEMENT`, or the granular `CREATE` / `ALTER` / `DROP` for `ROLE`,
  `ROW POLICY`, `QUOTA` and `SETTINGS PROFILE`, plus `ROLE ADMIN` for role
  grants
- the grant option on every privilege you grant
- `SHOW ACCESS` to read roles, policies, quotas and profiles
- `CLUSTER` for `ON CLUSTER`

The page reads your `SHOW GRANTS` and disables what your user cannot do. With
SSO, the audit log records the person; ClickHouse sees the service account.

## Audit

Every apply writes:

- `rbac.apply`: the number of changes and statements, the cluster and the plan
  hash
- `rbac.statement.applied` / `rbac.statement.failed`: one row per statement
  that ran, with the SQL (truncated to 2 KB) and, on failure, the error

Each row carries the person (actor), the ClickHouse user it ran as, the
connection and the IP. See [Audit Log](/docs/audit-log). After an apply, an
access sync refreshes [Governance → Access](/docs/governance#access-control)
when background sync is on.

## License

Reads and the review (`GET /api/rbac/snapshot`, `GET /api/rbac/entity/...`,
`POST /api/rbac/plan`) work with an active license and during the read-only
grace period. **Apply** needs an active license and returns `402` otherwise.

## API

All routes are admin only.

```bash
# Roles, users, grants, row policies, quotas, profiles, privileges and your capabilities
GET  /api/rbac/snapshot

# SHOW CREATE (and SHOW GRANTS for roles and users); kind is role, user,
# row_policy (name "policy ON db.table"), quota or settings_profile
GET  /api/rbac/entity/{kind}/{name}

# Build statements, diffs and warnings. Changes nothing.
POST /api/rbac/plan
{"ops": [{"kind": "grant_privilege", "target": {"type": "role", "name": "analyst"},
          "access": ["SELECT"], "database": "sales", "table": "orders"}],
 "cluster": ""}

# Re-plan, compare plan_hash, run in order
POST /api/rbac/apply
{"ops": [...], "cluster": "", "plan_hash": "<from plan>", "acknowledged": ["row_policy_scope"]}
```

Operation kinds: `create_role`, `drop_role`, `set_role_profile`,
`grant_privilege`, `revoke_privilege`, `grant_role`, `revoke_role`,
`set_default_roles`, `create_row_policy`, `alter_row_policy`,
`drop_row_policy`, `create_quota`, `alter_quota`, `drop_quota`,
`create_settings_profile`, `alter_settings_profile`, `drop_settings_profile`.
The server never accepts SQL text: it validates names, checks privilege and
setting names against `system.privileges` and `system.settings`, quotes every
identifier, and renders each statement itself.

Apply answers `409` when the plan hash no longer matches, `400` for a blocked
plan or a missing acknowledgement, and otherwise `200` with `results` (per
statement, with `hosts` for `ON CLUSTER`) and `rollback_ops`.
