---
title: Single Sign-On (SSO)
description: "OpenID Connect SSO for CH-UI: Okta, Microsoft Entra ID, Google Workspace, Keycloak, Auth0"
---

CH-UI supports **OpenID Connect (OIDC) SSO** against any compliant identity
provider: Okta, Microsoft Entra ID, Google Workspace, Keycloak, Auth0, and
others. SSO is a Pro feature.

SSO is configured once, server-wide, via environment variables (or the
matching `oidc_*` keys in `server.yaml`), or from **Admin → Settings** in the UI.

## How it works

OIDC authenticates the **person**. CH-UI never receives a ClickHouse password
from your IdP, so queries run through a **per-connection ClickHouse service
account** that you configure. The person's identity (email) drives their CH-UI
role and is recorded in the audit trail. Password login keeps working alongside
SSO.

```
Person ── OIDC ──▶ CH-UI    (identity, role, audit are per person)
                   CH-UI ── service account ──▶ ClickHouse
```

Because all SSO users share one ClickHouse service account at the database
layer, ClickHouse-native per-user grants do not apply to them. CH-UI's own RBAC
(admin / analyst / viewer) is their access control. Grant the service account
the least privilege your SSO users need.

## 1. Register CH-UI with your IdP

Create an OAuth/OIDC application in your IdP and set the redirect URI to:

```
https://ch-ui.yourcompany.com/api/auth/oidc/callback
```

Note the **issuer URL**, **client ID**, and **client secret**. To use role
mapping, configure the IdP to include a `groups` claim in the ID token.

## 2. Configure CH-UI

Set these via environment variables (or the matching `oidc_*` keys in
`server.yaml`), or fill in the same fields under **Admin → Settings → Configure SSO**:

```bash
OIDC_ISSUER_URL=https://accounts.google.com
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://ch-ui.yourcompany.com/api/auth/oidc/callback

# Optional
OIDC_CONNECTION_ID=                  # connection SSO uses (default: embedded connection)
OIDC_ALLOWED_DOMAINS=yourcompany.com # restrict by email domain (comma-separated)
OIDC_GROUPS_CLAIM=groups             # ID-token claim holding group memberships
OIDC_ADMIN_GROUPS=ch-ui-admins       # IdP groups → admin role (comma-separated)
OIDC_ANALYST_GROUPS=data-analysts    # IdP groups → analyst role
```

If `OIDC_CONNECTION_ID` is empty, SSO sessions use the embedded connection. If
there is no embedded connection, they use the oldest connection (the first one
created). With no connections at all, SSO sign-in fails.

On startup the server logs `OIDC SSO enabled` and a **Sign in with SSO** button
appears on the login page. If discovery fails (IdP unreachable, bad issuer),
CH-UI logs the error and starts with SSO disabled rather than refusing to boot.

### Configure from the UI (v2.6.0+)

Since v2.6.0 you can configure SSO entirely from the UI instead of the
environment. Since v2.11.0 it lives in **Admin → Settings**: a status panel
shows the current config, and **Configure SSO** opens the form in a side
sheet (`GET`/`PUT /api/admin/sso`):

- Settings are stored in the CH-UI database, with the client secret encrypted
  with `APP_SECRET_KEY`. The secret is never returned by the API, only a
  `has_secret` flag; leaving the field blank on save keeps the stored one.
- Saves apply immediately: the OIDC provider is re-initialized in place, no
  server restart needed. If discovery fails, the config is saved and the
  response carries a `reload_error` so you can fix and retry.
- **Env/YAML always wins.** When OIDC is configured via environment variables
  or `server.yaml`, the Admin UI shows that config read-only and `PUT` is
  rejected; edit the server config instead.
- The DB config supports two fields with no env equivalent:
  `viewer_groups` (IdP groups explicitly mapped to the `viewer` role, the
  default for unmatched users anyway) and `redirect_base_url` (base URL used to
  build the callback URL, falling back to the server's app URL when empty).

Config changes are recorded in the audit log (`sso.config_update`), without any
secret material.

## 3. Set the ClickHouse service account

Configure the ClickHouse account that SSO sessions query through, on the target
connection (admin only):

```bash
curl -X PUT https://ch-ui.yourcompany.com/api/connections/<CONNECTION_ID>/sso-account \
  -H 'Content-Type: application/json' \
  --cookie 'chui_session=<admin session>' \
  -d '{"username": "ch_sso_reader", "password": "..."}'
```

The password is encrypted at rest with `APP_SECRET_KEY`. Until a service account
is set, SSO logins fail with "SSO is not finished being set up (no ClickHouse
service account on the connection)".

## Role mapping

| Condition | CH-UI role |
|---|---|
| Member of an `OIDC_ADMIN_GROUPS` group | `admin` |
| Member of an `OIDC_ANALYST_GROUPS` group | `analyst` |
| Otherwise | `viewer` |

Group matching is case-insensitive. A user in both an admin and an analyst group
gets `admin`.

An admin can override one person's role in **Admin, Users**. SSO people are
listed by email with an **SSO** badge and the service account they query as.
An override applies to that person only and wins over the group mapping until
it is removed. In the API, an SSO person's override is keyed `sso:<email>`
(URL-encode it in `PUT`/`DELETE /api/admin/user-roles/{username}`).

## What is per person

SSO people share one ClickHouse account, but CH-UI keeps their data and their
actions apart by email:

- **Roles:** group mapping and overrides are per person.
- **Query history:** each person sees, deletes and clears only their own
  history, including queries their MCP clients ran. The 500-entry limit is
  per person and connection.
- **Brain chats** are private to the person who started them, and only that
  person can approve or decline a pending Brain action.
- **Dashboard stars** are per person. `created_by` on saved queries,
  dashboards, schedules, pipelines, models and saved views is the person.
- **Audit log:** the actor is the person's email; the ClickHouse account the
  action ran as is kept alongside it (`ch_user`).

Shared by design: ClickHouse itself only sees the service account, so
`system.query_log` and the query profile cannot tell SSO people apart.

### Upgrading from v2.13.1 or earlier

Before v2.13.2 these were keyed on the service account, so everyone on SSO
shared them. On upgrade:

- Query history written by SSO sessions before the upgrade is hidden from
  everyone, because it cannot be traced back to one person. Password users'
  history is unchanged.
- Brain chats and dashboard stars created by SSO people before the upgrade
  stay under the service account name and no longer appear for SSO people.
- A role override set on the service account name no longer applies to SSO
  people (only to password logins as that account). CH-UI logs a warning at
  startup for each one; set roles per person instead.
- Audit rows written before the upgrade keep the service account as the actor.

## Security

- The login flow uses a `state` parameter (CSRF protection), a `nonce`
  (replay protection), and **PKCE** (RFC 7636, S256 code challenge), all
  verified on callback via short-lived `HttpOnly` cookies scoped to
  `/api/auth/oidc`.
- The ID token's signature and audience are verified against the IdP's JWKS, and
  the nonce is checked before a session is issued.
- Use `OIDC_ALLOWED_DOMAINS` to restrict sign-in to your corporate email
  domain(s).
- The session shows the person's email (`via_sso: true` in `/api/auth/session`),
  while queries authenticate to ClickHouse as the service account.

## Endpoints

| Method | Path | Description |
|---|---|---|
| `GET` | `/api/auth/config` | Reports `oidc_enabled` and the SSO login URL (unauthenticated) |
| `GET` | `/api/auth/oidc/login` | Starts the SSO flow (redirects to the IdP) |
| `GET` | `/api/auth/oidc/callback` | IdP redirect target; creates the session |
| `PUT` | `/api/connections/{id}/sso-account` | Set the connection's ClickHouse service account (admin) |
| `GET` | `/api/admin/sso` | Current SSO config, secret redacted (admin) |
| `PUT` | `/api/admin/sso` | Update DB-managed SSO config and hot-reload the provider (admin) |
