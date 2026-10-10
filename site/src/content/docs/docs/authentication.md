---
title: Authentication & Roles
description: Login model, session cookies, and admin authorization
---

CH-UI authenticates users with ClickHouse credentials against a connection.

> **Single Sign-On:** CH-UI also supports OIDC SSO (Okta, Entra ID, Google,
> Keycloak, …) alongside password login. See **[Single Sign-On](/docs/sso)**.

## ClickHouse Credentials

The server authenticates against ClickHouse credentials over the selected connection tunnel.

### Login Flow

1. User picks a connection.
2. User provides ClickHouse `username/password`.
3. CH-UI verifies credentials through tunnel.
4. CH-UI issues `chui_session` cookie.

### Sign in with a ClickHouse URL

When the server runs with `allow_login_url` on (off by default), the
connection picker also offers **Other ClickHouse URL**. The user types a
ClickHouse URL instead of picking a connection, and the server finds or
creates a direct connection for that URL, then checks the credentials as
above. The connection is kept, so it shows in the picker next time. When there
are no connections at all, the login page opens straight in this mode.

The CH-UI server makes that connection, not the browser, so turn it on only on
trusted networks. Setup, limits and the risk are in
[Configuration](/docs/configuration/#sign-in-with-a-clickhouse-url).

## Session Endpoint

```bash
curl http://localhost:3488/api/auth/session \
  -H "Cookie: chui_session=..."
```

Response includes:

- `authenticated`
- `user` (the person's email for SSO sessions, the ClickHouse user otherwise)
- `user_role`
- `via_sso` (whether the session was created through SSO)
- active connection info
- app version

The `/api/auth/config` endpoint (unauthenticated) reports which login methods
are available so the login page can render the right options:

```bash
curl http://localhost:3488/api/auth/config
# { "password_login": true, "oidc_enabled": true, "setup_open": false, "login_url_allowed": false, "oidc_login_url": "/api/auth/oidc/login" }
```

`setup_open` is `true` while [first-run setup](/docs/cant-login/#first-run-setup-from-the-login-page)
accepts a setup code, which is what shows **Set up ClickHouse connection** on
the login page.
`login_url_allowed` is `true` when sign-in with a ClickHouse URL is on, which
is what shows **Other ClickHouse URL** in the picker.

## Role Resolution

CH-UI supports app-level roles, overridable per user by an admin:

| Role | Admin panel & settings | Workspace objects (dashboards, pipelines, models, saved queries, schedules) | Run queries |
|---|---|---|---|
| `admin` | ✓ | create / edit / delete / run | ✓ (per ClickHouse grants) |
| `analyst` | No | create / edit / delete / run | ✓ (per ClickHouse grants) |
| `viewer` | No | read-only | ✓ (per ClickHouse grants) |

Admin-only routes are guarded server-side (`RequireAdmin`); workspace writes are
guarded by `RequireWriter` (admin or analyst). Schedules follow the same rule:
creating, editing, deleting and running a schedule needs admin or analyst.

Queries you run yourself always use your own ClickHouse grants, whatever your
CH-UI role. Shared background jobs are different: an admin can give a worker
(scheduled queries, models, pipeline sinks, governance, Cluster Health,
telemetry monitors, performance scans, operations reports) its own ClickHouse
account for a connection. Those jobs then run with that account's grants, so
any admin or analyst who can create or edit the jobs can use those grants
through them. Keep background accounts no broader than your writers should
have. See
[Background Accounts](/docs/background-accounts).

## Login Failure Statuses

Common statuses returned by `/api/auth/login`:

- `400` bad request body or missing username, or (with `clickhouse_url`) both
  a URL and a connection were sent, or the URL failed validation
- `403` `clickhouse_url` was sent but sign-in with a ClickHouse URL is off
- `401` ClickHouse rejected the credentials, or the error could not be
  classified. Counts toward the rate limit.
- `429` IP/user rate limit (`retryAfter` in seconds in the body), or the login
  page has already created 20 connections and the URL matches none of them
- `503` the connection's tunnel agent is offline, or ClickHouse could not be
  reached (timeout, refused, network or TLS error). Does not count toward the
  rate limit, so an outage does not lock healthy accounts out.

## Logout

```bash
curl -X POST http://localhost:3488/api/auth/logout \
  -H "Cookie: chui_session=..."
```

This deletes the server session and clears cookie state.

## Audit

Both successful and **failed** logins are written to the immutable audit trail
(with user, IP, and timestamp), so brute-force and credential-stuffing attempts
are visible. Audit events can be forwarded to your SIEM. See
[Monitoring & SIEM](/docs/monitoring).

## Security Notes

- In production, cookie is `Secure` when app runs in production mode.
- Set strong `APP_SECRET_KEY`.
- Keep `ALLOWED_ORIGINS` strict to your UI origin(s).
- Terminate TLS natively or at a proxy. See [Security](/docs/security).

