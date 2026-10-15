---
title: Access Control Editor
description: Edit ClickHouse roles, grants, row policies, quotas and settings profiles with a reviewed, audited change set
---

The access control editor changes ClickHouse access entities from CH-UI: roles,
privilege and role grants, row policies, quotas and settings profiles. Every
change is staged first, previewed as the exact SQL that will run, checked for
risks, and applied in order with a result per statement. The access setup can
be exported and imported as a file ([Access as code](#access-as-code)), and
every change is kept in [Access history](#access-history).

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

Users are created only through [Import](#create-users-on-import), and dropped
only by **Prepare rollback** right after the apply that created them. Changing a user's password or
authentication stays in
[Admin → Users](/docs/admin#clickhouse-user-management). Not covered: renaming entities,
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

Statements always run in this order: create settings profiles, quotas, roles,
users; change roles, quotas and profiles; grant privileges; grant roles; create
or change row policies; default roles; revokes; then drop row policies, quotas,
users, roles and profiles.

### How grants are previewed

The diffs and **Prepare rollback** follow ClickHouse's own rules for
privileges, so the review shows what the server will hold afterwards:

- A revoke on a database (`db.*`) or on `*.*` also removes the narrower
  grants inside it.
- A grant clears the partial revokes inside it.
- Revoking something that is not held changes nothing.
- Groups such as `ALL` and `ALTER` act on the privileges they contain.
- `REVOKE GRANT OPTION` on a narrower object is shown as its own line.
- Grants are printed the way `SHOW GRANTS` prints them.

A rollback restores what was held through a broader object: undoing
`GRANT ALL ON d.t` for a role that also has `SHOW TABLES ON d.*` leaves its
`SHOW TABLES` on `d.t` in place. Privileges whose names have lowercase letters
or digits, such as `dictGet` and `S3`, can be granted and revoked.

### Safety checks

The server checks every plan, and again at apply time.

| Check | Effect |
|---|---|
| Revoking `ACCESS MANAGEMENT`, `ROLE ADMIN`, `SHOW USERS`, `SHOW ACCESS` or `ALL` from your own ClickHouse user, or from a role you hold | Blocked |
| Revoking from yourself a role that carries those privileges | Blocked |
| Dropping a role you hold | Blocked |
| A restrictive row policy, or a profile with `readonly` set, that would apply to you | Blocked |
| Changing an entity stored in `users.xml` | Blocked |
| Creating a user that already exists, or with a settings profile that does not exist | Blocked |
| Creating a user when the server keeps users only in read-only storage (`users.xml`) | Blocked |
| Dropping your own user, or a user CH-UI runs unattended | Blocked |
| Dropping any other user (it cannot be recreated: CH-UI never keeps passwords) | Needs acknowledgement |
| Creating a user with no password | Needs acknowledgement |
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

## Effective access

**Governance → Effective access** (`/governance?section=effective-access`)
answers two questions without changing anything:

- **What can this user do here, and why?** Pick a user, a database, a table
  (both optional) and a privilege (`SELECT` by default), then **Explain**.
- **Who can do this here?** Leave the user blank. The table lists every user
  holding the privilege, at login and after `SET ROLE ALL`, through which
  role path, and whether row policies filter what they read. Click a user to
  explain them.

The verdict line shows two badges: **At login** (the user's default roles)
and **After SET ROLE** (every granted role). Below it, one row per step:
the source path (`user ana > role analyst > role reader`), the statement, and
its effect (**Allows**, **Removes**, **Filters rows**, **Limits**,
**Blocks**). The **Explain access** link in a user's sheet under Access
control opens this section for that user (`?section=effective-access&user=<name>`).

How the answer is built:

- Roles are followed through roles granted to roles, with cycles stopped.
  Default roles honour `DEFAULT ROLE ALL EXCEPT`.
- A grant covers a privilege through `system.privileges` groups (`ALL`
  covers `SELECT`, `ALTER` covers `ALTER UPDATE`). For each user or role the
  most specific level wins, so a partial revoke only subtracts from the user
  or role it was made on: a revoke in role A never cancels a grant from role
  B. Column grants show as "Only a, b".
- Row policies that apply to the user or an active role combine as ClickHouse
  does: permissive ones with `OR`, restrictive ones with `AND`. When the table
  has policies but none permissive applies, the result depends on
  `users_without_row_policies_can_read_rows`, which CH-UI cannot read, so both
  outcomes are shown.
- Settings from profiles (assigned with `TO`, inherited, or set on a role or
  the user) are listed with their source. `readonly` 1 or 2 blocks writes,
  `allow_ddl = 0` blocks DDL, and `CONST` settings are reported.
- Matching quotas show their limits and, when `system.quotas_usage` is
  readable, current usage, with a note on how the quota is keyed.
- System tables such as `system.one` are readable by everyone; other
  `system` and `information_schema` tables depend on
  `select_from_system_db_requires_grant` and its `information_schema` twin.

Every answer is checked against `SHOW GRANTS FOR <user> WITH IMPLICIT FINAL`
(plain `FINAL` on servers without `IMPLICIT`). If ClickHouse disagrees, a
warning says so and ClickHouse wins. Wildcard grants (`db.prefix*`) are one
case where this matters, since `system.grants` does not mark them.

:::caution
Without `SHOW ACCESS`, ClickHouse only shows the signed-in user's own grants.
The answer is then marked incomplete and shows **Unknown**, never "No access".
:::

The answer comes from the node CH-UI is connected to; its host name is shown
in the toolbar. When a cluster of several nodes exists, any user, role,
policy, quota or profile not kept in replicated storage is marked **Local to
this node**, since other nodes may differ. These are reads: nothing is
written to the audit log.

## Access as code

**Export** in the Access control toolbar downloads the connection's access
setup as a file: roles and their settings profiles, users' default roles,
privileges, role grants, row policies, quotas and settings profiles.

| Format | Use |
|---|---|
| YAML (importable) | The canonical form |
| JSON (importable) | The same schema as the YAML |
| SQL (read only) | The statements that would create it, for reading. It cannot be imported |

The file is named `chui-access-<connection>-<date>.<ext>`. Passwords, hashes
and salts are never exported: a user entry carries its default roles, and as
hints for creating it elsewhere, its authentication type (`auth`) and the hosts
it may connect from (`hosts`). Entities defined in `users.xml` are left out and
listed in the comment at the top of the file, as are any parts that could not
be read.

```yaml
chui_access: 1
source: {connection: prod, host: ch-1, exported_at: 2026-10-14T10:00:00Z}
roles:
  analyst: {profile: readonly_p}
  auditor:
users:
  alice: {default_roles: all, except: [auditor], auth: sha256_password, hosts: [any]}
  bob: {default_roles: [analyst], auth: ldap, hosts: [local, ip 10.0.0.0/8]}
grants:
  analyst:
    - {access: [SELECT], on: "analytics.*"}
    - {access: SELECT, on: pii.users, columns: [id, country]}
    - {access: [SELECT], on: pii.users, columns: [email], revoke: true}
  role:auditor:
    - {access: [SHOW TABLES], on: "*.*", grant_option: true}
role_grants:
  alice: [analyst, {role: auditor, admin_option: true}]
row_policies:
  - {name: tenant, on: analytics.events, using: "tenant_id = 7", to: [analyst]}
quotas:
  daily:
    keyed_by: user_name
    intervals:
      - {every: 1 day, limits: {queries: 1000, read_rows: 1000000}}
      - {every: 60 minutes, randomized: true}
    to: all
    except: [bob]
settings_profiles:
  readonly_p:
    inherit: [default]
    settings:
      - {name: readonly, value: 1, writability: CONST}
      - {name: max_threads, max: "8"}
```

- `on` is `*.*`, `db.*` or `db.table`; names with dots, stars or spaces go in
  backticks.
- `default_roles` and `to` take `all`, `none` or a list; `except` goes with
  `all`.
- `hosts` entries are `any`, `local`, or `ip`, `name`, `regexp` or `like`
  followed by a value. `auth` is the ClickHouse authentication type
  (`sha256_password`, `ldap`, ...). Both are optional and only used when a
  user is created.
- A key under `grants` or `role_grants` must be listed under `roles` or
  `users`. Otherwise write it as `role:name` or `user:name`.
- Quota intervals are a number and a unit (`second` to `year`), with limits
  such as `queries`, `read_rows`, `result_bytes` or `execution_time`.
- Parsing is strict: an unknown key, a wrong `chui_access` version, a bad
  `on`, unit or limit is an error that names the path and line, such as
  `grants.analyst[0].on`.

### Import

**Import** opens a sheet. Choose a file or paste one (YAML or JSON, up to
1 MB), then:

- **What to import**: tick the parts of the file to use (roles, default roles
  of users, privileges, role grants, row policies, quotas, settings profiles).
- **Prune**: for each ticked part except default roles, revoke or drop what
  the file does not list. Without it, anything missing from the file is left
  alone. Entities in `users.xml` are never changed.
- **Rename**: map users and roles, and databases, from their name in the file
  to their name here. User and role renames apply everywhere the name appears:
  entries, grantees, granted roles, default roles and `TO` lists. Database
  renames apply to privileges and row policies.

**Preview** asks the server what the file would change on this connection and
changes nothing. It shows the number of changes per part and a **Skipped**
list with a reason for each entry it leaves out:

- A user in the file that does not exist here is skipped unless you tick it
  under **New users** (below).
- Grants and row policies on a database that does not exist here are skipped.
- Anything the editor cannot express, and anything in `users.xml`.

Privileges and settings that this server does not know are refused, with the
entry named. One plan holds at most 200 changes; a bigger import says so and
lists the counts per part, so you can import in batches: fewer parts, or fewer
new users, at a time. Each new user counts as one change.

### Create users on import

After a preview, **New users** lists the users the file has and this
connection lacks. Tick the ones to create and **Preview** again: their
privileges, role grants and default roles from the file are planned with
them, right after the `CREATE USER`. Pruning never drops users.

Each ticked user gets a sign-in, preselected from the file's `auth`, plus
hosts (from the file's `hosts`; empty means any host) and an optional
settings profile. **Defaults for ticked users** sets the sign-in, hosts and
profile of every ticked user at once; each user can still be changed after.

| Sign-in | What is sent to the server |
|---|---|
| Generate a password (default) | Your browser generates a random password and salt and sends only `sha256(password + salt)` and the salt (`IDENTIFIED WITH sha256_hash BY ... SALT ...`) |
| Paste a sha256, double SHA-1 or bcrypt hash | The hash you paste (and an optional salt for sha256) |
| LDAP server, Kerberos, SSL certificate | The server name, realm or certificate common name |
| No password | Nothing; the review asks you to acknowledge it |

A plaintext password never reaches CH-UI's server. The hash and salt are
secrets too: the review, the results, the audit log, access history and the
server logs show them as `'***'`, and only the statement sent to ClickHouse
carries them. Users are always created with a plain `CREATE USER`, never
`IF NOT EXISTS` or `OR REPLACE`, so an existing user is never replaced.
`ON CLUSTER` follows the cluster chosen in the review.

After the apply, **New user passwords** shows each generated password once,
with a copy button, for the users whose `CREATE USER` succeeded. CH-UI never
stores them; closing the review forgets them.

**Prepare rollback** drops the users the apply created. A user dropped this
way cannot be brought back by another rollback. A revert from access history
never drops or recreates users.

Users created through CH-UI are recorded as part of the apply, so they do not
raise a drift alert.

### Stage

**Stage N changes** puts the changes into the normal review (replacing any
pending changes), where the safety checks, acknowledgements, typed drop
confirmations, plan hash and rollback work as for changes made by hand.
Nothing runs until you **Apply**.

## Access history

**Governance → Access history** (`/governance?section=access-history`) keeps
a history of each connection's ClickHouse access control, flags changes made
outside CH-UI, and goes back to any earlier state through the normal review.
Pro, admins only.

A snapshot is stored:

| Source | When |
|---|---|
| **Apply** | After every apply in Access control |
| **Revert** | After an apply that came from **Revert to this state** |
| **Page view** | When Access control is opened, and just before an apply |
| **Governance sync** | At the end of each governance sync, when [background sync](/docs/governance#settings) is on. It reads with the governance account |
| **Manual** | **Take snapshot** in the toolbar |

A new row is added only when the state differs from the newest one; otherwise
that row is marked as seen again. Snapshots keep no passwords or authentication data.
History is per ClickHouse user that read it, so each comparison is between two
reads by the same user.

### Drift

The **Drift** column marks changes CH-UI did not make:

- **Outside change**: a page view, sync or manual snapshot differs from the
  previous complete one read by the same ClickHouse user. Not flagged when
  every changed entity was changed by an apply through CH-UI made by another
  ClickHouse user in between, or, on a cluster of several nodes, when the
  answering host changed and only entities in local (not replicated) storage
  differ.
- **Mixed**: an apply whose result is not only what its changes predict, so
  something else changed at the same time.

A partial read (a section that failed, or a login without `SHOW ACCESS`) is
stored and marked **Partial read**, and never judged as drift. The toolbar
filters to **Drift only**. Access control shows an **N outside changes**
badge when outside changes happened in the last 30 days; clicking it opens
Access history. Each new drift row also raises an `access.drift`
[alert event](/docs/alerts#access-drift), so an alert rule can send it by
email, Slack or webhook; it resolves when the state goes back to where it was
before the drift.

### Compare and revert

Clicking a row opens a sheet with the list of changes, the **Effective
access** each user gained or lost and the role it came through ("alice gained
SELECT ON `sales`.* via analyst"), and the before and after DDL of each
changed entity. **Compare with** picks what to compare against: the previous
snapshot, any other snapshot, or the live server.

**Revert to this state** builds the changes that bring the server back to that
snapshot, with every kind pruned, and opens them in Access control's review.
Nothing runs until you apply, and the apply is recorded as a **Revert** row.
Some differences cannot be reverted and are listed before you stage:

- Users: a revert never drops or recreates them. Users created since the
  snapshot are listed here (CH-UI does not record who created a user, so it
  leaves dropping them to you), and users deleted since stay deleted, since
  CH-UI never keeps passwords.
- Entities defined in `users.xml`.
- A role's settings profile, when the role has settings set on itself that
  changing the profile would remove.

A revert over 200 changes asks you to pick entities to revert now; repeat for
the rest.

Snapshots are kept for 180 days, at most 500 per connection, and the newest
one is always kept. Retention runs with governance pruning and on every new
snapshot, so it applies with background sync off too.

## Permissions

CH-UI side: admin only. ClickHouse side, for the user you signed in with (for
[SSO](/docs/sso), the connection's service account):

- `ACCESS MANAGEMENT`, or the granular `CREATE` / `ALTER` / `DROP` for `ROLE`,
  `ROW POLICY`, `QUOTA` and `SETTINGS PROFILE`, plus `ROLE ADMIN` for role
  grants
- the grant option on every privilege you grant
- `SHOW ACCESS` to read roles, policies, quotas and profiles
- `CLUSTER` for `ON CLUSTER`

Export, import and access history read the same way. For history snapshots
taken by the governance sync, the governance account needs `SHOW ACCESS` too;
without it those rows are partial reads.

The page reads your `SHOW GRANTS` and disables what your user cannot do. With
SSO, the audit log records the person; ClickHouse sees the service account.

## Audit

Every apply writes:

- `rbac.apply`: the number of changes and statements, the cluster and the plan
  hash
- `rbac.statement.applied` / `rbac.statement.failed`: one row per statement
  that ran, with the SQL (truncated to 2 KB) and, on failure, the error

Access as code and history also write:

- `rbac.exported`: the format and the number of roles, users, row policies,
  quotas, profiles and left-out entities (no file content)
- `rbac.import.planned`: a preview, with the source connection, the number of
  changes and skipped entries, the parts and the prune choices (no file
  content)
- `rbac.history.revert_prepared`: a revert built from a snapshot, with its id,
  hash and the number of changes and of differences it cannot revert

Each row carries the person (actor), the ClickHouse user it ran as, the
connection and the IP. See [Audit Log](/docs/audit-log). After an apply, an
access sync refreshes [Governance → Access](/docs/governance#access-control)
when background sync is on.

## License

Every route needs Pro. Reads and anything that only builds a plan work with an
active license and during the read-only grace period: the `GET` routes
(snapshot, entity, effective access, who can, export, history and diffs),
`POST /api/rbac/plan`, `POST /api/rbac/import/plan` and
`POST /api/rbac/history/{id}/revert-plan`. **Apply** and **Take snapshot**
need an active license and return `402` otherwise. Without a license, every
route returns `402`.

## API

All routes are admin only.

```bash
# Roles, users, grants, row policies, quotas, profiles, privileges and your capabilities
GET  /api/rbac/snapshot

# SHOW CREATE (and SHOW GRANTS for roles and users); kind is role, user,
# row_policy (name "policy ON db.table"), quota or settings_profile
GET  /api/rbac/entity/{kind}/{name}

# Explain one user's access: verdict (default, all_roles), evidence,
# row_policies, profile, quotas, verification, warnings, incomplete
GET  /api/rbac/effective?user=ana&database=sales&table=orders&column=&privilege=SELECT

# Every user holding a privilege on an object
GET  /api/rbac/who-can?database=sales&table=orders&privilege=INSERT

# Build statements, diffs and warnings. Changes nothing.
POST /api/rbac/plan
{"ops": [{"kind": "grant_privilege", "target": {"type": "role", "name": "analyst"},
          "access": ["SELECT"], "database": "sales", "table": "orders"}],
 "cluster": ""}

# Re-plan, compare plan_hash, run in order. revert_of (optional) is the
# history snapshot id a revert goes back to; the apply is then recorded as a revert.
POST /api/rbac/apply
{"ops": [...], "cluster": "", "plan_hash": "<from plan>", "acknowledged": ["row_policy_scope"]}

# Access as code: download the access file (format yaml, the default, json or sql)
GET  /api/rbac/export?format=yaml

# Plan an import. Changes nothing; stage the ops through /plan and /apply.
POST /api/rbac/import/plan
{"file": "chui_access: 1\n...", "prune": {"grants": true},
 "rename": {"principals": {"alice": "alice_eu"}, "databases": {"sales": "sales_eu"}},
 "kinds": ["roles", "grants"], "create_users": ["carol"]}
# -> {"ops": [...], "skipped": [{"kind", "name", "reason"}], "summary": {"grants": 3, ...},
#     "over_cap": false, "message": "", "source": {"connection", "host", "exported_at"},
#     "new_users": [{"name": "carol", "auth": "ldap", "hosts": [{"kind": "local"}]}]}
# A create_user op from an import has no "auth" yet; add it before /plan:
# {"kind": "create_user", "name": "carol", "auth": {"method": "sha256_hash", "hash": "<64 hex>", "salt": "<salt>"},
#  "hosts": [{"kind": "ip", "value": "10.0.0.0/8"}], "profile": "readonly_p"}

# Access history, newest first: drift=only for drift rows, limit (default 50,
# max 200) and cursor (next_cursor of the previous page); also outside_last_30d
GET  /api/rbac/history?drift=only&limit=50

# One snapshot with its stored state
GET  /api/rbac/history/{id}

# Changes between two states: against=previous (default), live, or another snapshot id
GET  /api/rbac/history/{id}/diff?against=live

# Store the live state now
POST /api/rbac/history/snapshot

# The ops that bring the server back to a snapshot, what they cannot revert,
# over_cap, max_ops and the ops per entity. "only" limits it to entity names.
POST /api/rbac/history/{id}/revert-plan
{"only": ["analyst"]}
```

`prune` keys are `roles`, `grants`, `role_grants`, `row_policies`, `quotas`
and `profiles`; `kinds` takes those plus `users` (default roles). Leave
`kinds` out to import every part.

Operation kinds: `create_role`, `drop_role`, `set_role_profile`,
`grant_privilege`, `revoke_privilege`, `grant_role`, `revoke_role`,
`set_default_roles`, `create_row_policy`, `alter_row_policy`,
`drop_row_policy`, `create_quota`, `alter_quota`, `drop_quota`,
`create_settings_profile`, `alter_settings_profile`, `drop_settings_profile`,
`create_user`, `drop_user`. A `create_user` `auth.method` is `sha256_hash`,
`double_sha1_hash`, `bcrypt_hash` (with `hash`, and `salt` for sha256),
`ldap` (`server`), `kerberos` (optional `realm`), `ssl_certificate` (`cn`) or
`no_password`; there is no plaintext password method.
The server never accepts SQL text: it validates names, checks privilege and
setting names against `system.privileges` and `system.settings`, quotes every
identifier, and renders each statement itself.

Apply answers `409` when the plan hash no longer matches, `400` for a blocked
plan or a missing acknowledgement, and otherwise `200` with `results` (per
statement, with `hosts` for `ON CLUSTER`) and `rollback_ops`.
