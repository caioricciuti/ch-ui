---
title: Security
description: "CH-UI security posture: TLS, authentication, RBAC, audit, encryption, and vulnerability disclosure"
---

This page summarizes CH-UI's security model for platform and security teams
evaluating a self-hosted deployment. For reporting vulnerabilities, see
[Vulnerability disclosure](#vulnerability-disclosure).

## Transport security (TLS)

CH-UI can terminate TLS natively, or run behind a reverse proxy / load balancer
that does.

- **Native TLS**: set `tls_cert_file` and `tls_key_file` (or `TLS_CERT_FILE` /
  `TLS_KEY_FILE`) and CH-UI serves HTTPS directly.
- **Reverse proxy**: terminate TLS at nginx/HAProxy/ingress and forward to
  CH-UI over the loopback interface. See [Deployment](/docs/deployment).

When neither is configured and the app is not bound to localhost, CH-UI logs a
prominent startup warning that it is serving plaintext HTTP. Never expose CH-UI
directly on an untrusted network without TLS: session cookies and ClickHouse
credentials must not cross a network in cleartext.

ClickHouse HTTP clients refuse redirects, including redirects from HTTPS to
HTTP, so credentials cannot be forwarded to another origin. Configure the final
ClickHouse HTTP endpoint directly. On every new connection the connector
resolves the hostname, rejects link-local, cloud metadata, unspecified and
multicast addresses, and dials a validated IP without a second DNS lookup.
Private and loopback ClickHouse targets remain supported.

## Authentication

- **Password login**: credentials are validated against ClickHouse over the
  connection tunnel; CH-UI issues an `HttpOnly`, `SameSite=Lax` session cookie
  (`Secure` in production).
- **SSO**: optional OIDC against your IdP. See [Single Sign-On](/docs/sso).
- **Brute-force protection**: progressive lockout, rate-limited per IP and per
  user; both successful and **failed** logins are written to the audit trail.
  The client address comes from `X-Forwarded-For` only when the request
  arrives through a proxy listed in `trusted_proxies` (default: loopback,
  private and link-local ranges), read from the proxy's end of the chain, so
  a client cannot dodge the per-IP limit or forge the audited address by
  sending the header itself. See
  [Behind a reverse proxy](/docs/configuration/#behind-a-reverse-proxy).
- **First-run setup**: until the first admin signs in, the unauthenticated
  `POST /api/auth/setup` can add one direct connection, so a fresh install
  whose embedded connection points at the wrong ClickHouse can be fixed from the
  login page. It is gated by a random 12-character code printed once to the
  server log at startup. Only its SHA-256 is kept, in memory. The code expires
  after 1 hour, is discarded after 10 wrong attempts in total, and each IP gets
  5 wrong attempts per 15 minutes. Setup closes for good on the first admin
  sign-in (password or SSO), or at startup when an admin already exists. It
  never edits the embedded connection, stores no ClickHouse credentials, and
  refuses URLs with credentials, query strings, fragments, or link-local and
  cloud metadata hosts. Every step is audited (`setup.code_rejected`,
  `setup.code_burned`, `setup.connection_saved`, `setup.closed`). See
  [Can't login?](/docs/cant-login/#first-run-setup-from-the-login-page).
- **Sign in with a ClickHouse URL**: off by default (`allow_login_url`,
  `ALLOW_LOGIN_URL`, `--allow-login-url`). When on, the unauthenticated
  `POST /api/auth/login` accepts a `clickhouse_url`, and the CH-UI server
  connects to it. Unlike v1, where the browser made that connection, this lets
  anyone who can reach the login page make the server connect to any address
  it can reach, so enable it only on a laptop, desktop or private LAN where
  everyone with access is trusted. The server logs a `WARN` line at startup
  while it is on. URLs get the same checks as first-run setup, one connection
  is kept per URL, the login page can create at most 20 connections (`429`
  after that), login rate limits apply, and each new connection is audited as
  `connection.created_from_login`. See
  [Configuration](/docs/configuration/#sign-in-with-a-clickhouse-url).

## Authorization (RBAC)

Three app-level roles (`admin`, `analyst`, `viewer`) are resolved per request.
Admin-only operations are enforced server-side (`RequireAdmin`), including:

- Admin panel and user management
- Schema-mutating DDL (create/drop database/table, uploads)
- Connection create/delete and tunnel-token read/rotate
- Governance policy, audit-log read and export, and the SSO service account

Shared workspace objects (dashboards, pipelines, models, saved queries,
schedules, telemetry monitors) are **read-only for viewers**: create/edit/delete
and pipeline/model/schedule runs require `admin` or `analyst` (`RequireWriter`).
Queries a user runs interactively are governed by that user's own ClickHouse
grants.

Unattended jobs can instead run with a **background account**: a dedicated
ClickHouse user an admin configures per connection and per worker. Admin and
analyst roles are instance-wide, and a connection is not a tenant boundary, so
giving someone a writer role delegates those accounts' grants to them through
the jobs they create or edit. Grant background accounts only what writers should
be able to do. Configuring them is admin-only. See
[Background Accounts](/docs/background-accounts).

For password sessions, the user's own ClickHouse grants are also enforced by
ClickHouse itself (queries run as that user). For SSO sessions, CH-UI RBAC plus
the service account's ClickHouse grants are the access control; on a connection
with [per-person ClickHouse users](/docs/sso#per-person-clickhouse-users) each
person has their own user with the roles their IdP groups map to, so ClickHouse
enforces grants per person there too. CH-UI authenticates to ClickHouse with
the `X-ClickHouse-User` and `X-ClickHouse-Key` headers, not HTTP Basic auth.

ClickHouse access entities can be edited from **Governance → Access control**
(Pro, admin only). The browser sends structured operations, never SQL: the
server validates names against `system.privileges` and `system.settings`,
quotes every identifier, checks row policy conditions with a lexer and
`EXPLAIN SYNTAX`, and runs each statement as the signed-in session's ClickHouse
user, never a background account. It refuses plans that would remove the
session's own access management or apply a restrictive policy or read-only
profile to it, requires typed confirmation for drops, re-checks a plan hash at
apply time, and audits every statement. See
[Access Control Editor](/docs/rbac).

## Audit logging & SIEM

Every security-relevant action, such as logins (including failures), logouts, query
execution, DDL, admin actions, connection and governance changes, is written to
an immutable audit trail with user, IP, and timestamp. The trail can be:

- **Read/exported** by admins (CSV or JSON) at `/api/governance/audit-logs`
- **Forwarded** to your SIEM via webhook, a JSONL file, or structured stdout

See [Monitoring & SIEM](/docs/monitoring) for setup.

## Secrets & encryption

- ClickHouse credentials, AI API keys, the SSO service-account password and
  the generated passwords of per-person SSO users are encrypted at rest
  (AES-256-GCM) with `APP_SECRET_KEY` before being stored in SQLite.
- Source `APP_SECRET_KEY` from the environment or `server.yaml`; keep it out of
  the database directory and use a different key per environment. Rotate it
  with `APP_SECRET_KEY_PREVIOUS`, which re-encrypts every stored secret on
  startup; see
  [Rotating the app secret key](/docs/configuration#rotating-the-app-secret-key).
- No telemetry or usage data is sent to the vendor. Outbound connections are
  only to destinations you configure (ClickHouse, your LLM provider, GitHub for
  updates, your alert provider). See the privacy policy for the full list.

## Application security

- **Browser requests**: unsafe requests to the browser API must come from its
  own origin or an explicitly allowed origin. Sibling origins on the same site
  are checked too. Password login also requires `application/json`. Configure
  `APP_URL` and `allowed_origins` for your public URL and any trusted separate
  frontend; webhook and ingest authentication are independent of browser CSRF
  checks.
- **Query audit redaction**: execution, streaming and export audit previews use
  the same credential redaction as query history, before truncation. Existing
  audit entries are not rewritten; restrict access and review retention if old
  queries contained secrets.

- **Content rendering**: all Markdown from AI/Brain output and dashboard text
  panels (including unauthenticated public dashboards) is sanitized before
  rendering, closing stored-XSS vectors. External links open with
  `rel="noopener noreferrer"`.
- **Security headers**: `X-Content-Type-Options`, `X-Frame-Options`,
  `Referrer-Policy`, `Content-Security-Policy`, and HSTS in production.
- **CSRF**: session cookies are `SameSite=Lax` and every state-changing
  endpoint is a non-`GET` method.
- **Rate & size limits**: unauthenticated public-dashboard and OAuth endpoints
  are per-IP rate-limited, keyed by the same trusted-proxy-aware client
  address as login; request bodies are capped.

## Supply chain

- CI runs `gofmt`, `go vet`, the test suite (with `-race`), `govulncheck`, and
  frontend typecheck/test/build on every change.
- Dependabot tracks Go modules, UI packages, and GitHub Actions.
- Releases publish SHA-256 checksums alongside binaries.

## Upgrading with the security hardening changes

Existing installations with a configured private key or their persisted
`.app_secret_key` retain that key. Saved credentials and sessions keep working;
normal HTTP/HTTPS ClickHouse endpoints on public, private and loopback addresses
remain supported. No user data migration is required for origin checks or audit
redaction.

Check these configuration exceptions before upgrading:

- **Old Compose encryption key:** rotate the public
  `please-change-me-to-a-random-secret` placeholder using
  [the key rotation procedure](/docs/configuration#rotating-the-app-secret-key).
  It is now refused as the current key. Keep the database volume and key backed
  up; an existing database without its key will stop with recovery instructions.
- **Redirecting ClickHouse URLs:** configure the final HTTP/HTTPS endpoint.
  Redirects are refused for sign-in, queries, streaming and exports.
- **Link-local ClickHouse addresses:** use a private or other supported address.
  Link-local and metadata targets, including DNS aliases to them, are refused.
- **Custom clients and separate frontends:** login must send
  `Content-Type: application/json`. Browser writes must use the app's origin or
  an origin explicitly allowed in configuration. Set `APP_URL` to the public URL
  when running behind a TLS proxy. The bundled frontend already sends JSON.

## Vulnerability disclosure

Please report security issues privately. Do **not** open a public GitHub issue.

- GitHub Security Advisories ("Report a vulnerability" under the repository's
  Security tab), or
- Email **me@caioricciuti.com**

You'll get an acknowledgement within 3 business days and a coordinated
disclosure timeline. See `SECURITY.md` in the repository for the full policy.
