---
title: Changelog
description: Notable changes across CH-UI releases
---

The authoritative, full changelog lives in [`CHANGELOG.md`](https://github.com/caioricciuti/ch-ui/blob/main/CHANGELOG.md)
in the repository, and every release is published on the
[GitHub Releases](https://github.com/caioricciuti/ch-ui/releases) page with
signed checksums and an SBOM. This page highlights the headline changes;
subscribe via the [RSS feed](/changelog.xml).

## v2.14.0 - 2026-09-29

A licensing release with no behaviour change. Every Pro feature is now under
the Business Source License 1.1, including Ask AI and the Brain agentic tools,
audit forwarding, and telemetry traces, metrics, service map and monitors. The
free Apache 2.0 core no longer depends on any BSL code. Versions published
before v2.14.0 keep their original licenses. See [License](/docs/license).

## v2.13.3 - 2026-09-29

One consistent answer to what is Pro, in the product and on this site. See
[License](/docs/license) for the full Free and Pro lists.

- **Security:** only admins can activate, replace or remove the license.
- **Query parameters** in the editor are now free.
- An **Enterprise** license unlocks the same as Pro.
- After a license expires and the 14-day grace ends, scheduled queries and
  telemetry monitors **pause** and resume when a license is activated.
- Activating a license takes effect without a restart everywhere, including
  Cluster Health collection and audit forwarding.
- During grace, read-only telemetry searches and schema comparison keep
  working.

## v2.13.2 - 2026-09-29

People who sign in with [SSO](/docs/sso) share one ClickHouse service account,
and CH-UI now keeps them apart.

- **Security:** role overrides apply to one SSO person instead of everyone on
  the service account. Admin, Users lists SSO people separately by email.
- **Security:** query history (including MCP queries), Brain chats and
  approvals, and dashboard stars are per person. Before, SSO users could see,
  delete and clear each other's history.
- **Audit log** names the person and keeps the ClickHouse account the action
  ran as.
- **Upgrading:** SSO history from before this release is hidden from everyone,
  since it cannot be traced to one person. A role override on the service
  account name no longer applies to SSO users; CH-UI logs a warning at startup.
  See [Upgrading from v2.13.1](/docs/sso).
- Settings, License can replace an active license, so a paid or renewed
  license activates without deactivating first.

## v2.13.1 - 2026-09-29

- **[License](/docs/license):** activating or deactivating a license unlocks
  or locks Pro pages right away, without a reload. After a Pro license
  expires, Pro pages stay open read-only for the 14-day grace period.
- **Login:** an unreachable ClickHouse shows "Connection unavailable" with a
  hint to start the connector, instead of "Login failed".

## v2.13.0 - 2026-09-21

Operations workflows for finding regressions, measuring improvements and
reviewing events across ClickHouse environments. All new features are Pro.

- **[Performance regressions](/docs/performance)**: compare normalized query
  patterns across equal time windows, with minimum sample counts and stated
  coverage limits. Admins can turn on hourly scans, which run with a dedicated
  [background account](/docs/background-accounts).
- **[Saved investigations](/docs/performance)**: keep a baseline, an owner,
  notes and before/after measurements, including against a rewritten query.
- **[Fleet overview](/docs/performance)**: an admin view of connection
  availability, retained health, open incidents and recent regression scans.
  Stale or missing data is shown as such.
- **[Schema comparison](/docs/schema-compare)**: compare metadata across
  environments and download a commented SQL review plan. Plans never run on
  their own, and supplied credentials are not stored.
- **[Incident timeline](/docs/incident-timeline)**: query failures and
  latency, merges and mutations, health, governance incidents and deployment
  annotations on one timeline.
- **[Weekly reports](/docs/operations-reports)**: saved operations summaries,
  optionally emailed through your configured channels with bounded retries.
- Fixed: log and trace histograms keep the requested time window, align
  buckets in UTC, and show tooltip values for the hovered interval. SMTP
  delivery honors deadlines. A tunnel race right after an agent connects is
  gone. Helm defaults pick the published image tag.

Automatic scans and weekly reports start disabled. Back up the SQLite
database and keep the application secret before upgrading.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.13.0)

## v2.12.0 - 2026-09-20

Dedicated accounts for background work.

- **[Background accounts](/docs/background-accounts)**: for each connection
  and worker (schedules, models, the pipelines sink, governance, Cluster
  Health, telemetry monitors), an admin picks a verified dedicated ClickHouse
  account, an active user session, or Disabled. Passwords are encrypted and
  never returned. A configured account never falls back to a person's
  session. Existing installs keep session mode.
- **Schedules need admin or analyst to change or run.** Viewers can still
  read them.
- Fixed: Parquet uploads read column types from the file schema, so valid
  files import. Logging in while ClickHouse is unreachable returns `503` and
  no longer counts toward the login lockout.
- **`/health` no longer shows the version.** It returns status, service and
  timestamp. Signed-in users still see the version in the app, and operators
  have `ch-ui version` on the host.
- Updated Go and frontend dependencies, including MCP SDK 1.8.0 and
  x/crypto 0.57.0.

Old binaries ignore the new account settings, so restore the pre-upgrade
snapshot if you roll back.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.12.0)

## v2.11.1 - 2026-09-11

Audit coverage for background work. No dependency changes.

- **Every background credential borrow is audited.** Schedules, models, the
  pipelines sink, the governance syncer, the Cluster Health harvester and
  telemetry monitors run with the ClickHouse credentials of an active session
  on the connection. Only the governance syncer used to audit that; now all
  six write `<worker>.credential_borrow` to the [audit log](/docs/audit-log),
  at most once per worker and connection per hour.
- In-app help and the MCP OAuth metadata link to these docs instead of GitHub,
  and the alert rule error message lists `telemetry.monitor`.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.11.1)

## v2.11.0 - 2026-09-11

A rebuilt interface and a real telemetry stack. No dependency changes.

- **Telemetry, rebuilt**: logs, traces, metrics, a service map, monitors and
  saved searches over the OpenTelemetry ClickHouse exporter's tables. Sources
  point CH-UI at any table and say which column plays which role, with
  one-click detection for the exporter's own layout. A search language (free
  text, phrases, `field:value`, comparisons, existence, `AND`/`OR`/`NOT`,
  wildcards) compiles to guarded ClickHouse SQL. Trace detail draws a
  waterfall with span events, links and correlated logs; monitors raise alert
  events through your existing channels. Logs, sources and saved searches stay
  in the open-source core; traces, metrics, the service map and monitors are
  Pro. See [Telemetry](/docs/telemetry).
- **Dashboard folders**: nested folders, per-user stars and tags, drag to
  move, and folder paths in the command palette. See
  [Dashboards](/docs/dashboards#folders-tags-and-stars).
- **The whole interface, rebuilt**: one page header, one table, one side
  sheet, one set of tokens. The rail groups pages into Query, Explore,
  Visualize, Build, Operate and Settings, and drill-downs became sidebar
  sections instead of tab strips. Saved Queries, Governance, Admin, Settings,
  MCP and the dashboard browser were rebuilt. Light mode is a real theme, and
  fonts are served by the app instead of a CDN.
- Fixed: SMTP alert channels could not be created from the UI; span events
  and links never appeared; a firing monitor raised a new alert event on every
  evaluation instead of once on the transition into firing.
- Security: Brain's SQL highlighter could inject HTML into a rendered message.
  Every character is now escaped.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.11.0)

## v2.10.1 - 2026-09-10

Hotfix for MCP behind a reverse proxy. Clients got `403 Forbidden: invalid
Host header` when a proxy reached a loopback listener with a public `Host`
header, which is exactly what the shipped nginx layout does. `/mcp` already
requires a key or OAuth token on every request, so the SDK's DNS-rebinding
guard is now off. Docker deployments were not affected.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.10.1)

## v2.10.0 - 2026-09-10

The MCP server catches up with the field. No dependency changes.

- **OAuth 2.1 sign-in for `/mcp`**: CH-UI is its own authorization server.
  claude.ai custom connectors, ChatGPT, and the sign-in path of Claude Code,
  Cursor and VS Code connect as the signed-in person, with their connection,
  their grants and their name in the audit log. PKCE is mandatory, access
  tokens last an hour with refresh rotation, and admins can revoke grants.
- **Catalog tools**: `search_catalog`, `estimate_query` (`EXPLAIN ESTIMATE`
  with a plain assessment), `run_saved_query`, and a `max_bytes` budget on
  `run_select`.
- **Verified saved queries**: a human review mark that AI clients are told to
  prefer.
- **Claude Code plugin** with a `clickhouse-analytics` skill.
- **Key expiry and rotation**: keys expire (90 days by default) and rotate in
  place with the same binding.
- Pagination and CSV output on the list tools and `run_select`, richer
  `describe_table`, a 120 requests per minute limit per key, and every tool
  call audited as `mcp.tool.call`.
- Fixed: a client disconnect or cancel now kills the ClickHouse query instead
  of letting it run to the 60 s timeout.

See [MCP Server](/docs/mcp).

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.10.0)

## v2.9.3 - 2026-09-09

Maintenance. SQLite 3.53.4 (through `modernc.org/sqlite` 1.58.0) with
upstream's fix for the journal-rollback data-corruption bug, and go-oidc
3.21.0, which skips JWKS entries with unsupported key types instead of
failing SSO verification.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.9.3)

## v2.9.0 - 2026-08-31

- **Embedded MCP server**: CH-UI serves the Model Context Protocol over
  streamable HTTP at `/mcp`, in the same binary. AI clients authenticate with
  revocable `chm_` keys that bind a connection, a dedicated ClickHouse user
  and an optional database allowlist. Safety is server-side: forced
  `readonly=2`, row caps, a 60 s limit, a read-only statement gate and
  governance guardrails (Pro). Every MCP query lands in query history and the
  audit log.
- **Write tools behind per-key scopes**: `read + write` keys can create saved
  queries, dashboards, and draft models and pipelines in CH-UI's own store.
  Nothing is executed from MCP and nothing writes ClickHouse data.
- Browser icons: the app now ships a favicon set instead of a blank tab icon.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.9.0)

## v2.8.0 - 2026-08-23

- **Live query progress**: while a query runs, the result panel shows elapsed
  time, percent complete, rows and bytes read, and read throughput, sampled
  from `system.processes`. Finished queries report rows and bytes read too.
- **Cancel means cancel**: cancelling a query or closing its tab now stops it
  on ClickHouse with `KILL QUERY`.
- The column-metadata probe wraps the statement as
  `SELECT * FROM (query) LIMIT 0`, so unusual `LIMIT` expressions no longer
  break it.
- Security: Go 1.25.14 for six standard-library vulnerabilities, plus
  dependency bumps.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.8.0)

## v2.7.0 - 2026-08-13

**Cost Center (Pro)**: showback/chargeback analytics for self-hosted
ClickHouse. Real consumption is priced from `system.query_log` (CPU core-hours)
and `system.parts` (GB-month storage) with configurable rates and currency:
team attribution via user-to-team rules, compute spend trend stacked by team,
per-team/per-user tables, top cost-driving query patterns, storage cost per
table with compression ratio, failed-query waste tracking, and CSV showback
export. Cluster-aware with local-node fallback. See [Cost Center](/docs/cost-center).

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.7.0)

## v2.6.2 - 2026-08-13

A fix round for telemetry and querying:

- Telemetry time filters now work with `DateTime64` columns (bounds go through
  `parseDateTime64BestEffort` instead of raw string literals).
- The Telemetry Setup Wizard's saved logs database/table is actually used;
  the Log Explorer no longer falls back to `default.otel_logs`.
- Queries with negative `LIMIT` (e.g. `LIMIT -10`) no longer fail in the
  column-metadata rewrite.
- Dependency bumps (sarama, modernc.org/sqlite, docker/login-action).

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.6.2)

## v2.6.1 - 2026-07-23

- **License as configuration**: `CHUI_LICENSE_FILE` (e.g. a mounted Kubernetes
  Secret) and `CHUI_LICENSE` (inline JSON) activate the Pro license at startup;
  the Helm chart exposes `license.existingSecret` / `license.secretKey`. An
  invalid environment license logs a warning and never blocks startup.
- GitHub model sync now recurses into subdirectories, so nested dbt layouts
  (`models/staging/`, `models/marts/`) import correctly. Thanks to @bfxavierpx
  for the first outside contribution.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.6.1)

## v2.6.0 - 2026-07-16

CH-UI went all-in on self-hosted: the cloud proof of concept is gone and its
best features moved here, behind the same offline-verified Pro license.

- **Ask AI** in the SQL editor: describe the query you want, get SQL generated
  against your schema with your own AI provider key (Pro).
- **Self-serve Pro licensing**: buy at ch-ui.com, receive the signed license
  by email, activate in Settings; 30-day free trial straight from the app.
  Licenses verify offline, so air-gapped installs keep working.
- **SSO configuration UI**: OIDC is set up from the Admin page instead of
  environment variables; the flow now uses PKCE.
- **Data retention manager**: background pruning of history tables with
  per-table windows, on by default (check the windows before upgrading if you
  need long history).
- Runtime multi-connection support, Helm chart, `session_max_age` config.
- Simpler alert routing (rules bind directly to channels), one JSON error
  shape across the API, production mode by default.
- Removed: governance lineage (slow and unreliable; its tables are dropped on
  upgrade), CH-UI Cloud, the Gitpod demo config.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.6.0)

## v2.5.3 - 2026-06-29

- Command palette no longer enters an infinite update loop when opened, and
  `⌘/Ctrl+K` now toggles it closed as documented.
- New doc covering the two supported ClickHouse connection models (direct URL
  and the outbound tunnel).

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.5.3)

## v2.5.2 - 2026-06-29

- Destructive-action alerts in the light theme are legible again.
- Go and UI dependency bumps, GitHub Actions updates.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.5.2)

## v2.5.1 - 2026-06-15

- Release binaries and the Docker image rebuilt on Go 1.25.11, patching 23
  standard-library vulnerabilities reachable from the codebase; `golang.org/x/net`
  bumped for GO-2026-4918.
- CI now actually compiles the backend on every PR, so `go vet`, `go test`,
  and `govulncheck` run for real.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.5.1)

## v2.5.0 - 2026-06-15

The enterprise-hardening release.

**Security**

- **OIDC Single Sign-On** (Pro): Okta/Entra/Google/Keycloak. See [SSO](/docs/sso).
- **Native TLS** termination (`tls_cert_file`/`tls_key_file`); a startup warning
  when serving plaintext HTTP.
- Markdown from AI/Brain and dashboards is sanitized (stored-XSS fix).
- Admin-gated connection tokens and audit-log read/export; failed logins audited;
  `viewer` role is read-only on shared workspace objects.
- Per-IP rate limiting on public dashboards; request-body caps.

**Operations**

- **Prometheus `/metrics`** and **audit forwarding (SIEM, Pro)** via webhook,
  file, or stdout, plus a CSV/JSON audit export. See [Monitoring & SIEM](/docs/monitoring).
- License **grace period**: an expired Pro license enters a 14-day read-only
  window instead of a hard lockout.
- Panic-recovery for HTTP handlers and background workers; Docker `HEALTHCHECK`.
- **Helm chart** and **Docker Compose** quick-start; `ch-ui backup` for a
  consistent database snapshot; schema-version tracking on upgrade.

**Reliability**

- Kafka pipeline ingestion is now **at-least-once** (offsets commit after the
  sink write).

**Supply chain**

- CI on every change (tests with `-race`, `govulncheck`, lint, typecheck);
  Dependabot; SECURITY.md; the self-updater verifies checksums **fail-closed**;
  releases publish a CycloneDX **SBOM** and **cosign**-signed checksums and images.

**Licensing**

- Pro modules are now published under the **Business Source License 1.1**
  (source-available; converts to Apache-2.0 on the Change Date). The community
  core stays Apache-2.0. See [Plans & Licensing](/docs/license).

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.5.0)

## v2.4.0 - 2026-06-10

- Query Insights (Pro): `system.query_log` analytics.
- Cluster Health (Pro): operations and database monitoring.
- Result filters and ClickHouse error parsing in the query results view.

[Full release notes](https://github.com/caioricciuti/ch-ui/releases/tag/v2.4.0)

---

For older releases and exact commit-level detail, see
[GitHub Releases](https://github.com/caioricciuti/ch-ui/releases).
