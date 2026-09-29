# Single Sign-On (OIDC SSO)

CH-UI supports OpenID Connect SSO against any compliant identity provider
(Okta, Microsoft Entra ID, Google Workspace, Keycloak, Auth0, …). SSO is a Pro
feature.

## How it works

OIDC authenticates the **person**. CH-UI does not receive a ClickHouse password
from the IdP, so queries run through a **per-connection ClickHouse service
account** that you configure. The person's identity (email) drives their CH-UI
role and is recorded in the audit trail; password login keeps working alongside
SSO.

```
Person ── OIDC ──▶ CH-UI   (identity, role, audit are per person)
                   CH-UI ── service account ──▶ ClickHouse
```

## 1. Register CH-UI with your IdP

Create an OAuth/OIDC application and set the redirect URI to:

```
https://ch-ui.yourcompany.com/api/auth/oidc/callback
```

Note the **issuer URL**, **client ID**, and **client secret**. If you want
role mapping, have the IdP include a `groups` claim in the ID token.

## 2. Configure CH-UI

Set these (env vars shown; `oidc_*` keys also work in the server config file):

```bash
OIDC_ISSUER_URL=https://accounts.google.com
OIDC_CLIENT_ID=your-client-id
OIDC_CLIENT_SECRET=your-client-secret
OIDC_REDIRECT_URL=https://ch-ui.yourcompany.com/api/auth/oidc/callback

# Optional:
OIDC_CONNECTION_ID=            # connection SSO uses (default: embedded connection, else the first one)
OIDC_ALLOWED_DOMAINS=yourcompany.com   # restrict by email domain (comma-separated)
OIDC_GROUPS_CLAIM=groups       # ID-token claim holding group memberships
OIDC_ADMIN_GROUPS=ch-ui-admins # IdP groups → admin role (comma-separated)
OIDC_ANALYST_GROUPS=data-analysts  # IdP groups → analyst role
```

On startup you should see `OIDC SSO enabled`. A "Sign in with SSO" button then
appears on the login page. (If discovery fails, CH-UI logs the error and starts
with SSO disabled rather than refusing to boot.)

## 3. Set the ClickHouse service account

Configure the ClickHouse account that SSO sessions query through, on the target
connection (admin only):

```bash
curl -X PUT https://ch-ui.yourcompany.com/api/connections/<CONNECTION_ID>/sso-account \
  -H 'Content-Type: application/json' \
  --cookie 'chui_session=<admin session>' \
  -d '{"username": "ch_sso_reader", "password": "..."}'
```

The password is encrypted at rest with `APP_SECRET_KEY`. Until this is set, SSO
logins fail with "SSO is not finished being set up (no ClickHouse service
account on the connection)".

## Role mapping

| Condition | CH-UI role |
| --- | --- |
| Member of an `OIDC_ADMIN_GROUPS` group | `admin` |
| Member of an `OIDC_ANALYST_GROUPS` group | `analyst` |
| Otherwise | `viewer` |

An admin can override one person's role in **Admin, Users**. SSO people are
listed by email with an **SSO** badge and the service account they query as.
An override applies to that person only; it wins over the group mapping until
it is removed.

## What is per person

SSO people share one ClickHouse account, but CH-UI keeps their data and their
actions apart by email:

- **Roles:** group mapping and overrides are per person.
- **Query history:** each person sees, deletes and clears only their own
  history, including queries their MCP agents ran. The 500-entry limit is per
  person.
- **Brain chats:** private to the person who started them.
- **Dashboard stars** are per person. `created_by` on saved queries,
  dashboards, schedules, pipelines, models and saved views is the person.
- **Audit log:** the actor is the person's email; the ClickHouse account the
  action ran as is kept alongside it.

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

## Security notes

- The flow uses `state` (CSRF), `nonce` (replay) and PKCE, all verified on
  callback; the ID-token signature and audience are verified against the IdP's
  JWKS.
- Because all SSO users share one ClickHouse service account at the database
  layer, ClickHouse-native per-user grants do not apply to them — CH-UI's own
  RBAC (admin/analyst/viewer) is their access control. Pick the service
  account's ClickHouse grants accordingly (least privilege).
