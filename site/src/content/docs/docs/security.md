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

## Authentication

- **Password login**: credentials are validated against ClickHouse over the
  connection tunnel; CH-UI issues an `HttpOnly`, `SameSite=Lax` session cookie
  (`Secure` in production).
- **SSO**: optional OIDC against your IdP. See [Single Sign-On](/docs/sso).
- **Brute-force protection**: progressive lockout, rate-limited per IP and per
  user; both successful and **failed** logins are written to the audit trail.
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
the service account's ClickHouse grants are the access control.

## Audit logging & SIEM

Every security-relevant action, such as logins (including failures), logouts, query
execution, DDL, admin actions, connection and governance changes, is written to
an immutable audit trail with user, IP, and timestamp. The trail can be:

- **Read/exported** by admins (CSV or JSON) at `/api/governance/audit-logs`
- **Forwarded** to your SIEM via webhook, a JSONL file, or structured stdout

See [Monitoring & SIEM](/docs/monitoring) for setup.

## Secrets & encryption

- ClickHouse credentials, AI API keys, and the SSO service-account password are
  encrypted at rest (AES-256-GCM) with `APP_SECRET_KEY` before being stored in
  SQLite.
- Source `APP_SECRET_KEY` from the environment or `server.yaml`; keep it out of
  the database directory and rotate it per environment.
- No telemetry or usage data is sent to the vendor. Outbound connections are
  only to destinations you configure (ClickHouse, your LLM provider, GitHub for
  updates, your alert provider). See the privacy policy for the full list.

## Application security

- **Content rendering**: all Markdown from AI/Brain output and dashboard text
  panels (including unauthenticated public dashboards) is sanitized before
  rendering, closing stored-XSS vectors. External links open with
  `rel="noopener noreferrer"`.
- **Security headers**: `X-Content-Type-Options`, `X-Frame-Options`,
  `Referrer-Policy`, `Content-Security-Policy`, and HSTS in production.
- **CSRF**: session cookies are `SameSite=Lax` and every state-changing
  endpoint is a non-`GET` method.
- **Rate & size limits**: unauthenticated public-dashboard endpoints are
  per-IP rate-limited; request bodies are capped.

## Supply chain

- CI runs `gofmt`, `go vet`, the test suite (with `-race`), `govulncheck`, and
  frontend typecheck/test/build on every change.
- Dependabot tracks Go modules, UI packages, and GitHub Actions.
- Releases publish SHA-256 checksums alongside binaries.

## Vulnerability disclosure

Please report security issues privately. Do **not** open a public GitHub issue.

- GitHub Security Advisories ("Report a vulnerability" under the repository's
  Security tab), or
- Email **me@caioricciuti.com**

You'll get an acknowledgement within 3 business days and a coordinated
disclosure timeline. See `SECURITY.md` in the repository for the full policy.
