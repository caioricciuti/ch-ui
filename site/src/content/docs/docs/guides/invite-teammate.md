---
title: Invite a teammate
description: Give someone access to CH-UI with a ClickHouse user or SSO, and set their role
---

CH-UI has no invite emails and no passwords of its own. People sign in either
with a ClickHouse user or through your identity provider (SSO). So inviting
someone means giving them one of those, then setting their CH-UI role. You
need the admin role for every step here.

## Pick a role

| Role | What they can do |
|---|---|
| `admin` | Everything, including Admin: users, connections, AI providers, MCP |
| `analyst` | Run queries; create and edit saved queries, dashboards, models, pipelines and schedules |
| `viewer` | Run queries and open everything, but create or change nothing |

The role decides what someone can do in CH-UI. What they can read in
ClickHouse is still up to the ClickHouse grants of the account they query as.

## Option A: a ClickHouse user

1. Open **Settings** in the rail, then **Admin**, then **Users**.
2. Switch to **ClickHouse users** and click **New user**.
3. Type a **Username** and a **Password**. Under **Default roles**, list the
   ClickHouse roles they should have, if you use roles.
4. Check the **Command preview**, then click **Create user**.
5. Make sure the user can read the data. With Pro, use **Edit grants** on
   their row. Otherwise run a `GRANT` in a query tab, for example
   `GRANT SELECT ON analytics.* TO ana`.
6. Send them the CH-UI address, the connection name and the password through
   a channel you trust.

## Option B: single sign-on (Pro)

1. Open **Admin**, **Settings** and click **Configure SSO**.
2. Fill in your identity provider's issuer, client ID and secret, and map
   IdP groups to the admin and analyst roles. Everyone else gets `viewer`.
3. Add the person to the right group in your identity provider.
4. Send them the CH-UI address. They click **Sign in with SSO**.

[Single Sign-On](/docs/sso) covers the provider setup, the ClickHouse service
account, and per-person ClickHouse users.

## Set their role

People show up under **Admin**, **Users**, **Application users** after their
first sign-in. CH-UI picks a starting role from their ClickHouse grants or
their SSO groups: a user with only `SELECT` starts as `viewer`. Set the role
yourself rather than relying on the detected one.

1. Find them in **Application users**.
2. Click the role you want in the **Role** column. It applies at once, even to
   a session that is already open.

For a ClickHouse user the role applies on the current connection only; switch
connection to set it on another one. For an SSO person it applies on every
connection. **Remove override** goes back to the detected role.

## Next

- [Admin Guide](/docs/admin#users) covers both user lists in detail.
- [Authentication](/docs/authentication#role-resolution) explains how the
  starting role is detected.
