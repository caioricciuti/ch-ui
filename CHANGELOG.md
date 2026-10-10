# Changelog

All notable changes to CH-UI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.16.0] - 2026-10-10

### Security

- **Client addresses behind a reverse proxy.** `X-Forwarded-For`, `X-Real-IP`,
  `X-Forwarded-Proto` and `X-Forwarded-Host` are now believed only when the
  request arrives from a proxy in the new `trusted_proxies` setting
  (`TRUSTED_PROXIES`, `--trusted-proxies`; default: loopback, private and
  link-local ranges; `none` or `[]` trusts no proxy). `X-Forwarded-For` is
  read from the proxy's end, skipping trusted hops, so a client cannot prepend
  an address. Before, any client could send `X-Forwarded-Proto` together with
  a made-up `X-Forwarded-For` and dodge the per-IP limits on login, first-run
  setup, the OAuth token and registration endpoints and public dashboards, put
  a false address in the audit log, and steer the origin in the MCP OAuth
  metadata. Native TLS no longer counts as a proxy indicator. A request from
  outside the trusted ranges that carries forwarding headers is logged once
  per address (`Ignoring X-Forwarded-For from an address that is not a trusted
  proxy`). If your proxy has a public address, add it to `trusted_proxies`,
  or every user behind it shares one per-IP login limit. See
  [Behind a reverse proxy](https://ch-ui.com/docs/configuration/#behind-a-reverse-proxy)
  (#233).
- **Audit entries record the client, not the proxy.** Every audit event
  written by the API handlers used the TCP peer address, which behind a
  reverse proxy is the proxy itself plus an ephemeral port. They now record
  the same trusted-proxy-aware client address as login and setup (#234).

### Added

- **Edit connections in Admin.** Admin > Connections has an Edit button per
  row. Direct connections can be renamed or pointed at another ClickHouse URL;
  saving a new URL restarts that connection's connector. Agent connections can
  be renamed. The embedded connection stays read-only and shows "Server
  config", since `clickhouse_url` and `connection_name` set it. Backed by
  `PUT /api/connections/{id}` (admin) (#231).
- **First-run setup from the login page.** When no admin has ever signed in,
  the server prints a one-time setup code to its log at startup (a `WARN` line
  with `setup_code`). **Set up ClickHouse connection** on the login page takes
  the code, a name and a ClickHouse URL and adds a direct connection, so a fresh
  install whose embedded connection cannot reach ClickHouse no longer needs a
  restart to fix. The embedded connection is not changed. The code expires
  after 1 hour, after 10 wrong attempts, or when the first admin signs in
  (password or SSO); restart for a new one. Setup closes for good after the
  first admin sign-in, and at startup when an admin already exists. Wrong codes
  are limited to 5 per IP per 15 minutes, URLs with credentials, query strings,
  fragments or link-local and cloud metadata hosts are refused, and every step
  is audited (`setup.code_rejected`, `setup.code_burned`,
  `setup.connection_saved`, `setup.closed`). New endpoint
  `POST /api/auth/setup`; `GET /api/auth/config` gains `setup_open` (#230).
- **Sign in with a ClickHouse URL, opt-in.** With `allow_login_url: true`,
  `ALLOW_LOGIN_URL=true` or `--allow-login-url` (flag over env over YAML), the
  connection picker on the login page offers **Other ClickHouse URL**: type a
  URL, username and password and sign in, as in v1. Off by default, because the
  CH-UI server, not the browser, makes the connection, so anyone who can reach
  the login page could make the server connect to any address it can reach.
  Enable it only on trusted networks (a laptop, a desktop, a private LAN); the
  server logs a `WARN` at startup while it is on. URLs get the same checks as
  first-run setup. One direct connection is kept per URL and reused, including
  direct connections an admin created; new ones are named `host:port` and stay
  in the picker. The login page creates at most 20 connections (`429` when
  full, delete unused ones in Admin > Connections), and each new one is audited
  as `connection.created_from_login`. `POST /api/auth/login` takes an optional
  `clickhouse_url` (not together with `connectionId`); `GET /api/auth/config`
  gains `login_url_allowed` (#232).

### Changed

- github.com/IBM/sarama 1.61.1 and modernc.org/sqlite 1.60.1 (#223).
- Site: Astro 7.3.5 and Starlight 0.42.5 (#226).
- Build: CI pulls its test ClickHouse from a mirror on ghcr.io instead of
  Docker Hub (#221), anchore/sbom-action 0.24.3 (#222), Dependabot watches
  `ui/` and `site/` through the bun ecosystem (#224), and `ui/` gets the same
  7-day release-age floor and empty `trustedDependencies` as `site/` (#227,
  #229). No change to the shipped binaries or image.

## [2.15.0] - 2026-10-09

### Added

- **Open a table from the SQL editor.** Hold Cmd (macOS) or Ctrl and click a
  database or table name to open its tab. Names that resolve get a faint dotted
  underline and a hover hint; in `db.table` each part is its own link. A bare
  table name links only when exactly one database has it (#214).
- **MCP write tools ask before writing.** `save_query`, `create_dashboard`,
  `create_model` and `create_pipeline` show the user a yes/no prompt on clients
  that speak MCP protocol 2026-07-28 or later with form elicitation. A decline
  writes nothing and is audited as `mcp.write.declined`. Older clients behave as
  before (#216).
- **Docs links on the Pro pages.** Performance, Fleet, Schema comparison,
  Incident timeline, Reports and the background accounts sheet link to their
  ch-ui.com docs (#215).

### Security

- **Go 1.26.9 and golang.org/x/net v0.60.0.** Fixes five net/http and x/net
  advisories reported by govulncheck (GO-2026-6610, 6611, 6612, 6613, 6617).
  Binaries built with v2.14.3 and earlier carry the vulnerable versions (#217).
- **UI dependencies.** devalue 5.9.4 and source-map-js 1.2.2, plus a vetted
  update of the UI toolchain (vite 8.3.2, svelte 5.57.1, CodeMirror); versions
  are now pinned exactly (#217).

### Changed

- github.com/IBM/sarama 1.61.0 (#212).

## [2.14.3] - 2026-10-01

### Security

- **`/metrics` no longer names the build.** `ch_ui_build_info` keeps only the
  `go_version` label; `version` and `commit` are removed from the
  unauthenticated endpoint, as they were from `/health` earlier. Dashboards that
  grouped by `version` lose that label (#210).
- **Deleting another person's query history entry answers 404.** It never
  deleted anything, but it answered 200 (#208).

### Fixed

- **Audit Log shows the ClickHouse user.** A "ClickHouse user" column and detail
  row on the page, a `ch_user` column in the CSV export, and `ch_user` on
  forwarded SIEM events, set when it differs from the actor (#208).
- **ClickHouse users with `@` or `:` in their name** can have their password
  changed and be deleted from Admin; the endpoints now decode the name (#208).
- **Login says "Connector online / offline"** instead of "Connected /
  Unreachable": the status is the connector, ClickHouse is checked at sign-in
  (#208).
- **`/settings` opens the License page** (keeping `?section=`) instead of the
  workspace home (#208).
- **Telemetry explains missing tables.** When the OpenTelemetry tables do not
  exist, Logs, Traces and Metrics show setup help instead of the raw ClickHouse
  error (#208).
- **Scheduled queries skip runs missed while unlicensed.** After a license is
  activated again, an overdue schedule waits for its next slot instead of
  running late (#209).
- **Creating or editing a schedule keeps its last run** time, status and error
  (#209).
- **A weekly report settings update keeps omitted fields.** A body without
  `weekday` or `hour` no longer saves Sunday 00:00 (#210).

### Changed

- **Data retention covers operations history.** Operations reports, deployment
  annotations and resolved performance investigations are pruned after 180 days
  by default, editable in Admin → Settings. Open and monitoring investigations
  are never pruned (#210).
- An expired license is logged once per state (grace, then expired), not on
  every check (#209).

## [2.14.2] - 2026-10-01

### Security

- **DOMPurify 3.4.16.** The UI moves from 3.4.15, which is covered by
  GHSA-p98j-92pf-mc4p (low). The UI's only DOMPurify hook does not use
  `IN_PLACE`, so it was not exposed as written; the version is now pinned
  exactly (#205).

### Changed

- **The website and documentation now live in this repository, under `site/`.**
  ch-ui.com is built from it, so a change and its documentation land in the
  same pull request. The changelog page and the RSS feed on ch-ui.com are
  generated from this file. `site/` is not open source; see `LICENSING.md`
  (#204, #206).

## [2.14.1] - 2026-09-29

### Security

- **The Brain approval audit log is admin only.** `GET /api/brain/audit`
  returned every person's agent approvals (tool, arguments, who asked, who
  decided) to any signed-in user, viewers included (#202).

### Changed

- **Documentation lives only on [ch-ui.com/docs](https://ch-ui.com/docs).** The
  in-repo `docs/` folder is removed; the README, the Settings legal links and
  the Claude Code plugin point at ch-ui.com, and the Terms and Privacy links go
  to the current ch-ui.com/terms and ch-ui.com/privacy (#201).
- "Lineage" is no longer listed as a Governance feature in the README,
  LICENSING.md and the Pro paywall; the feature was removed earlier (#201).

## [2.14.0] - 2026-09-29

Licensing release: every Pro feature is now under the Business Source License
1.1, and the free core no longer depends on any BSL code. No behaviour change.

### Changed

- **License of Pro code.** From this version, all code behind Pro features is
  under the Business Source License 1.1 (`LICENSE.BSL`) and carries the
  `BUSL-1.1` header. This now includes Ask AI and the Brain agentic tools,
  audit forwarding to a SIEM, telemetry traces, metrics, service map and
  monitors, editor guardrails, parameterized saved-query runs, and the Pro UI
  pages. `LICENSING.md` lists every BSL path. Code published in earlier
  versions without the header remains available under Apache 2.0 in those
  versions (#198, #199).
- **Core no longer depends on BSL code.** Tunnel and session tokens
  (`internal/tokens`), cron parsing (`internal/cronexpr`) and mail delivery
  through SMTP, Resend and Brevo (`internal/mail`) moved out of BSL packages
  into Apache 2.0 core. Dashboard share invites use core mail delivery (#197).
- Files that mixed free and Pro code are split so each file has one license
  (#198). Small side effects: the Logs trace tab reloads the trace when you
  return to it, and the Models GitHub sync button appears slightly earlier.

## [2.13.3] - 2026-09-29

One consistent answer to "what is Pro", enforced the same way by the server,
the UI and the docs.

### Security

- **Only admins can activate, replace or remove the license.** Any signed-in
  user, viewers included, could remove it and lock Pro for everyone (#193).

### Changed

- **Query parameters in the editor are free.** Parameterized saved-query runs
  stay Pro (#194).
- **An Enterprise license unlocks the same as Pro.** Before, it activated but
  unlocked nothing (#194).
- **After a Pro license expires and the 14-day grace ends, enabled scheduled
  queries and telemetry monitors pause** and resume when a license is
  activated. Nothing is disabled or deleted. Before, they kept running without
  a license. Schedules that fell due while paused run once on resume (#194).
- Activating or removing a license now takes effect without a restart for
  Cluster Health collection, governance sync and audit forwarding to a SIEM.
  Audit events are not forwarded while no license is active (#194).
- The Pro paywall, Settings, README, license docs and LICENSING.md describe the
  same Free and Pro split. Pro modules are the Business Source License 1.1
  (source-available), not "proprietary" (#195).

### Fixed

- During the grace period, read-only searches that use POST (telemetry traces,
  metrics, service map, schema comparison) returned 402; they work now (#194).
- Enabling governance sync in Admin settings no longer starts it without Pro
  (#194).
- Command palette: telemetry entries open the right sections, and "New
  dashboard / model / pipeline / Brain chat" show for everyone (#194).
- `SECURITY.md` lists 2.13.x as supported. Backup docs use `ch-ui backup` and
  include the secret key file (#195).

## [2.13.2] - 2026-09-29

SSO users who share one ClickHouse service account are now separate people in
CH-UI: roles, query history, Brain chats, stars and the audit log are per person.

### Security

- **SSO roles are per person.** Role overrides were keyed on the shared
  ClickHouse service account, so setting a role for one SSO user applied to
  every SSO user on the connection and overrode their IdP group mapping. An
  override now applies to one person (stored as `sso:<email>`). An override on
  the service account name applies only to password logins as that account;
  CH-UI logs a warning at startup for each such row (#190).
- **SSO users no longer see each other's data.** Query history (including
  queries run by MCP agents), Brain chats and dashboard stars were shared by
  everyone on the service account, and anyone could delete or clear the others'
  history. They are now kept per person. Only the person who requested a Brain
  approval can approve or decline it (#191).
- **Audit log names the person.** Audit rows record the SSO user's email, and
  the ClickHouse account the action ran as is kept in a new `ch_user` field.
  `created_by` on saved queries, dashboards, schedules, pipelines, models and
  saved views is the person (#191).

### Changed

- **Upgrading:** query history written by SSO users before this release is
  hidden from everyone, because it cannot be traced back to one person. Brain
  chats and stars created by SSO users before this release no longer appear for
  them. Password users see no change. Details in `docs/sso.md` (#191).
- Admin, Users lists SSO people separately by email, with an SSO badge and the
  account they query as (#190).
- Settings, License shows **Replace license** while a license is active, so a
  paid or renewed license can be activated without deactivating first (#189).

### Fixed

- Role changes for users whose name contains `@` or `:` were stored under the
  URL-encoded name and never applied (#190).
- Login shows "Connection unavailable" when the selected connection is already
  known to be offline, instead of "Login failed" (#188).

## [2.13.1] - 2026-09-29

License state that follows activation, a read-only grace period after a Pro
license expires, and clearer login errors.

### Fixed

- **License:** activating or deactivating a license in Settings now unlocks or
  locks Pro pages immediately. Before, Pro pages kept the old state until the
  page was reloaded (#186).
- **License:** during the grace period after a Pro license expires, Pro pages
  open read-only with a notice, matching the backend, which already served reads
  and refused changes. Settings shows when the grace period ends (#186).
- **Login:** a login that fails because ClickHouse is unreachable now shows
  "Connection unavailable" with a hint, instead of a generic "Login failed" (#185).

### Dependencies

- `modernc.org/sqlite` 1.58.0 to 1.59.0 (#183).
- CI: `docker/setup-buildx-action` 4.3.0 to 4.4.1 (#182),
  `docker/build-push-action` 7.3.0 to 7.4.0 (#184).

## [2.13.0] - 2026-09-21

Production operations workflows for finding regressions, measuring improvements
and reviewing events across ClickHouse environments.

### Added

- **Performance regressions:** compare normalized query patterns across equal
  windows, with minimum sample requirements and explicit coverage limits.
  Administrators can enable hourly scans using a dedicated background account.
- **Saved investigations:** retain baselines, owners, notes and immutable
  before/after measurements, including comparisons with rewritten queries.
- **Fleet overview:** administrator view of connection availability, retained
  health, open incidents and recent regression scans, with stale/missing data
  shown explicitly.
- **Schema comparison:** compare metadata across environments using supplied
  credentials and download commented SQL review plans. Plans never execute
  automatically; credentials are not persisted.
- **Incident timeline:** correlate query failures and latency, merges/mutations,
  retained health, governance incidents/comments and deployment annotations.
- **Weekly reports:** save operations summaries and optionally deliver them
  through configured email channels, with persisted snapshots and bounded retries.
- Live ClickHouse acceptance tests for regression aggregation, report generation,
  schema comparison, incident queries and telemetry time boundaries.

### Fixed

- Log histogram tooltip labels now match their severity counts and colors.
- Log/trace histograms preserve the fetched time window, align buckets in UTC,
  select tooltip values by the hovered interval and zoom without an extra bucket.
  Search and histogram upper time bounds are consistently exclusive.
- SMTP connections now honor cancellation and deadlines, preventing an
  unresponsive server from indefinitely blocking report delivery or shutdown.
- Tunnel authentication replies and query writes share one write lock, avoiding
  a race when work starts immediately after an agent connects. Heartbeat/status
  timestamp access is also synchronized.
- The Operate navigation group opens a page accessible to the signed-in role.
- Helm defaults now select the published, version-prefixed Docker image tag.

### Upgrade

- New operations features require Pro; the telemetry fixes apply to the existing
  telemetry experience. Automatic scans and weekly reports start disabled and
  require explicit dedicated-account configuration.
- Back up the SQLite database and preserve the application secret before
  upgrading. See [operations setup](docs/operations.md) and
  [performance investigations](docs/performance-investigations.md).

## [2.12.0] - 2026-09-20

Dedicated accounts for reliable background execution, with explicit workspace
permission delegation and tested credential rotation.

### Added

- **Background accounts per connection and worker.** Administrators can choose
  a verified dedicated ClickHouse account, an active user session, or Disabled
  for schedules, models, pipeline sinks, governance, Cluster Health and telemetry
  monitors. Passwords are encrypted and omitted from responses. Existing installs
  retain session mode. Credential changes and account use are audited; failed
  verification preserves the previous account and configured accounts never
  silently fall back to a human session.
- Regression coverage for account verification/cancellation, all six workers,
  rotation and disabling, migration/restart/backup restore, real DOM sanitization
  and Parquet upload. CI exercises background workers against ClickHouse and
  audits frontend dependencies.

### Changed

- Updated Go dependencies, including MCP SDK 1.8.0, mysql 1.10.1,
  x/crypto 0.57.0, x/oauth2 0.37.0 and Thrift 0.24.0; frontend dependencies
  include Svelte 5.57.0, Vite 8.3.0, Vitest 5.0.0 and DOMPurify 3.4.15.
- Documented background-account grants, instance-wide writer delegation,
  manual-run behavior and rollback precautions. Old binaries ignore the new
  account/Disabled settings; restore the pre-upgrade snapshot when rolling back.

### Security

- **Schedule mutations and manual runs require admin or analyst access.**
  Viewers retain read access but cannot create, change, delete or run shared
  schedules, including schedules that use a dedicated account.
- Updated vulnerable frontend packages and transitive pins; the release
  candidate's dependency audit reports no vulnerabilities.
- CI/release actions are pinned by commit SHA and Dependabot updates observe
  a release-age cooldown. Cosign remains pinned to its v2 line to preserve
  the existing checksum signature artifacts.


- **`/health` no longer discloses the version.** The endpoint is
  unauthenticated so that liveness probes work without credentials, which
  also meant anyone scanning for CH-UI instances could read the exact build
  and look up which advisories applied to it. The response is now status,
  service and timestamp. Signed-in callers still get `appVersion` from the
  session endpoints, and operators have `ch-ui version` on the host. Probes
  that check the status code (the Docker `HEALTHCHECK` and the Helm
  liveness and readiness probes) are unaffected.

### Fixed

- Parquet uploads now derive row types from the file schema, allowing valid
  files to import instead of failing schema construction.
- Background account forms submit with Enter, respect native validation, and
  prevent duplicate saves. The UI explains which manual runs use the account.
- Accessibility warnings in the panel editor, color picker and Brain input.
- **An unreachable ClickHouse locked people out of their own account.** A
  failed connection test counted against the login rate limiter exactly like
  a wrong password, so a database, agent or network outage burned through the
  three attempts per user and blocked login for 15 minutes after the outage
  ended. Failures are now classified: a ClickHouse that never answered
  returns `503` and counts nothing, while rejected credentials, and any
  message that cannot be classified, still count and still return `401`.

## [2.11.1] - 2026-09-11

Audit coverage for background work, and docs that point at the docs site.
No dependency changes.

### Security

- **Every background credential borrow is audited.** Schedules, models, the
  pipelines sink, the governance syncer, the Cluster Health harvester and
  telemetry monitors run with the ClickHouse credentials of an active
  session on the connection. Only the governance syncer used to write an
  audit row for that; the other five borrowed silently. All six now share
  one lookup that audits each borrow as `<worker>.credential_borrow`
  (`schedule`, `model`, `pipeline`, `governance`, `cluster_health`,
  `telemetry.monitor`), at most once per worker and connection per hour.
  Which session gets borrowed is unchanged (#168).

### Fixed

- In-app documentation links (the Telemetry setup guide, the can't-login help
  and the license policy) and the MCP OAuth metadata
  (`resource_documentation`, `service_documentation`) point at
  ch-ui.com/docs instead of GitHub blob URLs (#167).
- The alert rule validation error lists `telemetry.monitor` as an accepted
  `event_type`. The check itself already accepted it (#167).
- `docs/telemetry.md` corrected against the code: how monitors borrow
  credentials, query history, the real query limits, when a monitor raises
  an alert, and the aggregations each metric type offers (#167).

## [2.11.0] - 2026-09-11

A rebuilt interface and a real telemetry stack. No dependency changes.

### Added

- **Telemetry, rebuilt**: logs, traces, metrics, a service map, monitors and
  saved searches over the OpenTelemetry ClickHouse exporter's tables. Sources
  let you point CH-UI at any database and table and say which column plays
  which role, with one-click detection for the exporter's own layout and a
  `DESCRIBE`-backed allowlist so only mapped columns ever reach SQL. A
  HyperDX-style search language (free text, phrases, `field:value`,
  comparisons, existence, `AND`/`OR`/`NOT`, wildcards) compiles to guarded
  ClickHouse SQL. Trace detail assembles the span tree server-side and draws a
  waterfall with span events, links and correlated logs. Monitors evaluate a
  search on a schedule and raise alert events through existing channels
  (#166).
- **Dashboard folders**: nested folders with per-user stars and tags, drag to
  move, cycle-checked reparenting, and deletion that re-parents contents one
  level up in a transaction. Folder paths show in the command palette (#166).

### Changed

- **The whole interface**: one page header, one table, one sheet, one empty
  state, one set of tokens. Drill-downs became sidebar sections instead of tab
  strips you had to scroll to reach; only the query workspace keeps tabs.
  Saved Queries, Governance, Admin, Settings, MCP and the dashboard browser
  were rebuilt on the shared primitives. Charts share one floating tooltip.
  Light mode is a real theme rather than a set of overrides, and Inter and
  JetBrains Mono are served by the app instead of a CDN (#166).
- Traces, metrics, the service map and monitors require a Pro licence,
  matching the other Operate depth. Logs, sources and saved searches stay
  community (#166).

### Fixed

- **SMTP alert channels could never be created from the UI.** The form sent
  `smtp_host`, `smtp_port`, `smtp_username` and `smtp_password` while the
  dispatcher and its validation read `host`, `port`, `username` and
  `password` (#166).
- **Span events and links never appeared.** Source detection blanked the
  `Events` and `Links` Nested prefixes, because ClickHouse describes them
  flattened as `Events.Name` and a plain column lookup always missed. Sources
  already saved with the empty value repair themselves (#166).
- **A firing monitor raised a new alert event on every evaluation.** The
  fingerprint bucket was derived from the monitor's own interval, so
  consecutive runs never shared one and de-duplication never engaged. A 30
  second monitor left firing for a day meant thousands of events and a
  notification for each. It now emits on the transition into firing, like the
  audit log beside it (#166).

### Security

- **Brain's SQL highlighter could inject HTML into a rendered message.**
  `highlightSQL` built markup from a regex that matched only strings,
  comments, numbers and words; every other character, `<` included, fell
  through untouched into `{@html}`. All characters are now escaped, with
  regression tests (#165).

## [2.10.1] - 2026-09-10

Hotfix for MCP behind a reverse proxy. No dependency changes.

### Fixed

- **MCP clients got `403 Forbidden: invalid Host header` through a reverse
  proxy.** The MCP Go SDK's DNS-rebinding guard rejects requests that reach a
  loopback listener with a public Host header, which is exactly what the
  shipped nginx layout (`upstream 127.0.0.1:3488`, `Host $host`) produces.
  The guard ran after the key had already been accepted, so clients reported
  it as an auth failure. `/mcp` requires a bearer key or OAuth token on every
  request, so the guard added nothing; it is now off, with a regression test.
  Docker deployments were not affected. Workaround on 2.10.0:
  `MCPGODEBUG=disablelocalhostprotection=1` on the CH-UI process (#164).

## [2.10.0] - 2026-09-10

The MCP server catches up with the field. No dependency changes.

### Added

- **OAuth 2.1 sign-in for the MCP server**: CH-UI is its own authorization
  server for `/mcp`. Clients that only speak OAuth (claude.ai custom
  connectors, ChatGPT) and the sign-in path of Claude Code, Cursor and VS
  Code connect as the signed-in person: their connection, their ClickHouse
  grants, their name in the audit log. Discovery metadata under
  `/.well-known`, Dynamic Client Registration for public clients, Client ID
  Metadata Documents (SSRF-guarded), PKCE S256 mandatory, a consent page in
  the UI, one-hour access tokens with refresh rotation, admin revocation
  (#163).
- **Catalog tools for better plans**: `search_catalog` (tables, columns,
  saved queries, dashboards by name or comment), `estimate_query`
  (`EXPLAIN ESTIMATE` totals with a plain assessment), `run_saved_query`
  (by id or name, with parameters), and a `max_bytes` budget on
  `run_select` mapped to `max_bytes_to_read` (#162).
- **Verified saved queries**: a human review mark on saved queries, toggled
  from the Saved Queries page and surfaced to AI clients, which are told
  to prefer verified SQL (#162).
- **Claude Code plugin** at `integrations/claude-code-plugin` with a
  `clickhouse-analytics` skill; `claude plugin marketplace add
  caioricciuti/ch-ui` (#162).
- **MCP key expiry and rotation**: keys expire (default 90 days from the
  UI) and rotate in place with the same binding (#161).
- **Pagination and CSV**: `list_tables` and the CH-UI list tools take
  `page_size` and `cursor`; `run_select` offers `format: csv`, returns
  column types and `rows_read` / `bytes_read` (#160).
- **Tool titles and annotations** on every tool, plus server instructions
  sent at connect time (#156).

### Changed

- `describe_table` returns the `CREATE TABLE` statement, per-column sizes,
  active parts, partitions, last modification and sample rows (#162).
- `/mcp` is rate limited to 120 requests per minute per key; every tool
  call is audited (`mcp.tool.call`), not only `run_select` (#160).
- `list_saved_queries` and `list_pipelines` are scoped to the key's
  connection (#160).
- `docs/mcp.md` rewritten: OAuth-first setup, per-client snippets, honest
  scope of the database allowlist (#160, #163).

### Fixed

- A client disconnect or MCP cancel now cancels the ClickHouse query on
  the agent instead of running to the 60 s timeout (#160).
- `run_select` trims rows to `max_rows` exactly; `result_overflow_mode=break`
  could return more (#160).
- The 200 KB response cap applies to every tool, not only `run_select`
  (#160).

## [2.9.3] - 2026-09-09

### Changed

- `modernc.org/sqlite` 1.58.0 (SQLite 3.53.4), which carries upstream's fix for
  the journal-rollback data-corruption bug and replaces the interim
  super-journal patch. `modernc.org/libc` 1.75.6 and `modernc.org/memory`
  1.12.1 move in lockstep (#153).
- `github.com/coreos/go-oidc/v3` 3.21.0: JWKS entries with unsupported key
  types are skipped instead of failing SSO verification (#153).
- CI pins `govulncheck` to v1.7.0; `@latest` now requires Go 1.26 and broke
  the backend job (#155).

## [2.9.0] - 2026-08-31

### Added

- **Embedded MCP server**: CH-UI now serves the Model Context Protocol over
  streamable HTTP at `/mcp`, in the same binary. AI clients (Claude Code,
  claude.ai custom connectors, Cursor) authenticate with revocable `chm_` keys
  (SHA-256 at rest, admin-managed in Admin → MCP Server) that bind a
  connection, a dedicated ClickHouse user, and an optional database allowlist.
  Free tools: `list_databases`, `list_tables`, `describe_table`, `run_select`,
  `explain_query`. Pro tools: `query_insights_top`, `costs_summary`. Safety is
  server-side: forced `readonly=2`, row caps (default 100, max 2000), 60s
  execution limit, a read-only statement gate, and governance guardrails (Pro).
  Every MCP query is recorded in query history (tagged MCP) and the audit log
  (`mcp.query.execute`), and carries `log_comment='ch-ui:mcp'`. See docs/mcp.md.
- New dependency: `github.com/modelcontextprotocol/go-sdk` v1.7.0 (official MCP
  Go SDK, Apache-2.0, maintained with Google).
- **MCP write tools, behind per-key scopes**: keys are `read` (default) or
  `read + write`. Write keys get `save_query`, `create_dashboard` (SQL panels,
  automatic layout), `create_model` and `create_pipeline` — all created as
  drafts tagged `mcp:<key name>`, audit-logged, and never executed from MCP
  (models/pipelines are started from the UI). Every key also gets
  `list_saved_queries` / `list_dashboards` / `list_models` / `list_pipelines`.
  These write to CH-UI's own store, never to ClickHouse data.
- Browser icons: the app shipped without a favicon, so browsers fell back to
  requesting `/favicon.ico`, got the SPA's `index.html` from the catch-all
  route, and showed a blank tab icon. `ui/public` now carries a `favicon.ico`
  (32/48), a vector `favicon.svg`, 16/32/192/512 px PNGs and a 180 px
  `apple-touch-icon.png`, generated by `ui/scripts/generate-favicons.py`, and
  `index.html` links them. The set is optically sized: tab-sized icons carry the
  two rings, drawn thicker than in the logo so they survive 16 px, while the
  full mark — whose lettering blurs into the rings below ~48 px — is used from
  192 px up.

## [2.8.0] - 2026-08-23

### Added

- **Live query progress in the editor** (#147): while a query runs, the result
  panel shows elapsed time, percent complete, rows and bytes read, and read
  throughput — the numbers ClickHouse's own `/play` reports. Progress is sampled
  from `system.processes` (300 ms) and streamed to the browser as `progress`
  messages on the existing NDJSON query stream. A user without `SELECT` on
  `system.processes` gets no live readout and no errors: the first refused
  sample stops sampling for that query. Statements that work while the
  connection is open report progress too (`INSERT ... SELECT`, `OPTIMIZE TABLE
  ... FINAL`); mutations still run in the background, so their readout covers
  only the statement.
- Finished streaming queries now report rows and bytes read. Previously only
  elapsed time was available, because `JSONCompactEachRow` carries no
  statistics; the numbers now come from ClickHouse's `X-ClickHouse-Summary`
  header reconciled with the last progress sample. The readout stays in place
  after the query ends, so a query too short to report progress still shows
  what it read (the duplicate rows/bytes chips were dropped from the result
  footer).

### Fixed

- Cancelling a query (or closing the tab) now stops it on ClickHouse. The agent
  tags every streamed query with a `query_id`, and an abandoned stream is killed
  with `KILL QUERY` instead of being left to run to completion unattended.
- Metadata probe for SELECT queries now wraps the statement as
  `SELECT * FROM (query) LIMIT 0` instead of rewriting the `LIMIT` clause
  textually (#139). Immune to `LIMIT` expressions (`10*2`), `WITH TIES`, and
  negative limits with unusual spacing. Non-wrappable statements (`SHOW`,
  `DESCRIBE`) keep the textual path. Suggested by @mywalcoin-gif.

### Security

- Go toolchain bumped to 1.25.14: govulncheck flagged six standard-library
  vulnerabilities in 1.25.12 (`net/http`, `crypto/tls`, `net/url`,
  `encoding/xml`, `encoding/asn1`).
- Dependency bumps: chi 5.3.2, minio-go 7.3.0, golang.org/x/crypto 0.55.0,
  modernc.org/sqlite 1.57.0, docker/login-action 4.6.0.

## [2.7.0] - 2026-08-13

### Added

- **Cost Center (Pro)**: showback/chargeback analytics for self-hosted
  ClickHouse. Prices real consumption from `system.query_log` (CPU
  core-hours via `OSCPUVirtualTimeMicroseconds`) and `system.parts`
  (GB-month storage) with configurable rates and currency. Includes
  team attribution via user-to-team rules with an allocation-coverage
  KPI, compute spend trend stacked by team, per-team/per-user tables,
  top cost-driving query patterns, storage cost per table with
  compression ratio, failed-query waste tracking, and CSV showback
  export. Cluster-aware with local-node fallback, soft-fails when
  `query_log` is unavailable.

## [2.6.2] - 2026-08-13

### Fixed

- Telemetry time filters now work with `DateTime64` columns: RFC3339 bounds
  are validated and rendered via `parseDateTime64BestEffort` instead of raw
  string literals that failed with `TYPE_MISMATCH` (#143).
- Telemetry Setup Wizard values are actually used: the saved logs
  database/table is loaded on mount and passed up from the wizard, so the
  Log Explorer no longer falls back to `default.otel_logs` (#142).
- Queries with negative `LIMIT` (e.g. `LIMIT -10`) no longer fail: the
  column-metadata rewrite now recognizes negative limits and offsets (#139).

### Changed

- Dependency bumps: `github.com/IBM/sarama` 1.60.1, `modernc.org/sqlite`
  (go-dependencies group), `docker/login-action` 4.5.2 (#144, #140).

## [2.6.1] - 2026-07-23

### Added

- License as configuration: `CHUI_LICENSE_FILE` (path to the license JSON,
  e.g. a mounted Kubernetes Secret) and `CHUI_LICENSE` (inline JSON) load and
  activate the Pro license at startup, taking precedence over one activated in
  the UI. An invalid or missing environment license logs a warning and never
  blocks startup. The Helm chart exposes it as `license.existingSecret` /
  `license.secretKey`.

### Fixed

- GitHub model sync now recurses into subdirectories, so nested dbt layouts
  (`models/staging/`, `models/marts/`) import correctly. Thanks @bfxavierpx
  for the first outside contribution (#138).

## [2.6.0] - 2026-07-16

CH-UI is now all-in on self-hosted: the cloud proof of concept is gone and its
best features live here, behind the same offline-verified Pro license.

### Added

- Ask AI in the SQL editor: describe the query you want and get SQL generated
  against your schema, using your own AI provider key (Pro).
- Self-serve Pro licensing: buy at [ch-ui.com](https://ch-ui.com), receive the
  signed license by email within a minute, and activate it in Settings. A
  30-day free trial can be started right from the app. Licenses stay verified
  offline, so air-gapped installs keep working.
- SSO configuration UI: OIDC is now set up from the Admin page instead of
  environment variables, and the authorization flow uses PKCE.
- Data retention manager: background pruning of history tables (audit logs,
  alert events, schedule/pipeline/model runs, sync logs) with per-table
  windows configurable in Admin. New databases use incremental auto-vacuum so
  reclaimed space is returned to the OS.
- Runtime multi-connection support: add direct-URL ClickHouse connections and
  switch between them without restarting.
- Helm chart under `deploy/helm/ch-ui` for Kubernetes installs.
- `session_max_age` config option (and `SESSION_MAX_AGE` env var) to control
  session lifetime; default stays 7 days.

### Changed

- Alert routing is simpler: rules bind directly to channels. Existing
  rule-to-channel bindings are migrated automatically; digest and escalation
  policies were removed.
- Dev mode is now opt-in (`--dev` flag or `NODE_ENV=development`). Production
  is the default: HSTS is sent, logs are info-level, and localhost origins are
  no longer auto-allowed in CORS.
- All API errors now share one JSON shape.
- Unknown keys in the config file are reported at startup instead of being
  silently ignored.
- Trial licenses are delivered by email only, with server-side abuse
  protection (alias dedupe, disposable-domain blocking, MX checks, daily cap).
- Builds require Go 1.25.12 (fixes crypto/tls GO-2026-5856 from the standard
  library).

### Removed

- Governance lineage: it was slow and unreliable, and its tables are dropped
  on upgrade.
- CH-UI Cloud: all references removed; the product is self-hosted only.
- Gitpod demo configuration.

### Upgrade notes

- The database migrates automatically on first start and the upgrade was
  tested against a v2.5.3 schema. Alert routes carry over.
- Retention pruning is ON by default (audit logs 90 days, alert events 60,
  pipeline run logs 30, and so on). If you need longer history, set a window
  to 0 (keep forever) in Admin before old rows age out.
- Lineage data is deleted on upgrade. Back up your database first if you want
  to keep it.

## [2.5.3] - 2026-06-29

### Fixed

- Command palette no longer enters an infinite Svelte update loop
  (`effect_update_depth_exceeded`) when opened: the open routine runs `loadAll()`
  inside `untrack()` so its writes to the databases/data stores can't re-trigger
  the effect that started it (#129).
- `⌘/Ctrl+K` now toggles the command palette, so pressing it again closes the
  palette as the help text describes (#129).

### Added

- `docs/connecting-to-clickhouse.md` documenting the two supported ClickHouse
  connection models — direct (`CLICKHOUSE_URL`, including reverse-proxied
  `https://`) and the outbound tunnel (`ch-ui connect`) (#128).

## [2.5.2] - 2026-06-29

### Fixed

- Improve destructive-action alert contrast in the light theme so error and
  delete-confirmation panels in the database explorer are legible (#111).

### Changed

- Bump the Go dependency group: `IBM/sarama` 1.47.0→1.50.3, `coreos/go-oidc/v3`
  3.18.0→3.19.0, `go-chi/chi/v5` 5.2.5→5.3.0, `go-sql-driver/mysql`
  1.9.3→1.10.0, `minio/minio-go/v7` 7.0.98→7.2.0, `lib/pq` 1.11.2→1.12.3,
  `modernc.org/sqlite` 1.44.3→1.52.0, `fatih/color` 1.18.0→1.19.0 (#126).
- Bump `@types/node` 25→26 in the UI dev dependencies (#127).
- Bump `actions/checkout` 6→7 in the CI and release workflows (#125).

## [2.5.1] - 2026-06-15

### Security

- Rebuild release binaries and the Docker image on Go 1.25.11, patching 23
  standard-library vulnerabilities reachable from the codebase (crypto/x509,
  crypto/tls, net/http, net/textproto, mime, net/url, os, …).
- Bump `golang.org/x/net` to v0.56.0 (GO-2026-4918).

### Fixed

- CI: the backend job now compiles (a `//go:embed ui/dist` placeholder), so
  `go vet`/`go test`/`govulncheck` actually run on every PR and push.

### Changed

- Bump GitHub Actions to Node 24-compatible versions (checkout v6, setup-go v6,
  docker buildx v4 / login v4 / build-push v7).

## [2.5.0] - 2026-06-15

### Security

- Audit failed login attempts (`user.login_failed`) in the immutable audit trail,
  not just successful logins — enables brute-force and credential-stuffing detection.
- Require admin role to create or delete a connection and to read or rotate a
  connection's tunnel token. Reads (list/get/test) remain available to any
  authenticated user.
- Admin-only access to the audit-log read and export endpoints (the trail
  contains other users' usernames, IPs, and query text).
- `viewer` role is now read-only on shared workspace objects (dashboards,
  pipelines, models, saved queries): create/edit/delete and pipeline/model runs
  require `admin` or `analyst`. Viewing and running queries are unchanged and
  remain governed by each user's ClickHouse grants.
- Sanitize all rendered Markdown (Brain AI output and dashboard text panels,
  including unauthenticated public dashboard share links) with DOMPurify to close
  a stored-XSS vector. External links now open with `rel="noopener noreferrer"`.
- Per-IP rate limiting on the unauthenticated public-dashboard endpoints, and a
  32 MB cap on request bodies.
- Native TLS termination (`tls_cert_file`/`tls_key_file`); when serving plaintext
  HTTP the server now logs a prominent warning instead of staying silent.

### Added

- **OIDC Single Sign-On (Pro)**: log in via any OpenID Connect provider
  (Okta/Entra/Google/Keycloak). OIDC authenticates the person (identity, role,
  and audit are per-person); queries run through a per-connection ClickHouse
  service account. Role is mapped from IdP groups, with optional email-domain
  restriction. The flow uses state + nonce and verifies the ID token. Password
  login keeps working alongside. See `docs/sso.md`.
- **License grace period**: an expired Pro license now enters a 14-day read-only
  window (monitoring keeps working, writes are blocked) instead of hard-locking
  the installation at the moment of expiry.
- **Prometheus `/metrics`** endpoint (HTTP counters, latency, in-flight, Go
  runtime, build info) — no external dependency.
- **Audit forwarding (SIEM, Pro)**: optionally stream audit events to a webhook,
  a JSONL file, or structured stdout, plus an admin CSV/JSON export endpoint. The
  authoritative copy always stays in the database.
- Panic-recovery middleware for HTTP handlers and a panic-safe wrapper around
  background workers (scheduler, alert dispatcher, governance syncer, cluster
  health harvester, model scheduler).
- Helm chart (`deploy/helm/ch-ui`) and a `docker-compose.yml` quick-start, both
  documenting the single-instance constraint.
- `ch-ui backup` command — a consistent SQLite snapshot via `VACUUM INTO` (safe
  to run against a live, WAL-mode database), with an `APP_SECRET_KEY` reminder.
- Database schema-version tracking recorded on each migration run for upgrade
  observability.
- Release artifacts now ship a CycloneDX **SBOM** and **cosign**-signed checksums;
  Docker images are cosign-signed with SBOM + provenance attestations.
- Docker `HEALTHCHECK`.
- Continuous Integration workflow: gofmt check, `go vet`, `go test -race`,
  `govulncheck`, frontend typecheck, unit tests, and production build now run on
  every pull request and push to `main`.
- Dependabot configuration for Go modules, UI npm packages, and GitHub Actions.
- `SECURITY.md` vulnerability disclosure policy.
- Tests for license validation (valid/grace/expired/tampered/wrong-key) and the
  recovery/metrics middleware.
- This changelog.

### Fixed

- Kafka pipeline ingestion is now at-least-once: consumer offsets are committed
  only after the batch is durably written to the sink, instead of when the
  message is first read (previously a crash mid-batch silently dropped data).
- `ch-ui update` now verifies the download checksum **fail-closed**: it refuses to
  install if a checksum cannot be fetched or verified, instead of warning and
  continuing.
- WebSocket tunnel (`/connect`) upgrades no longer break when the metrics
  middleware is in the chain (the response-writer wrapper now preserves
  `http.Hijacker`).

### Changed

- Privacy policy now accurately lists every optional third-party egress path
  (your LLM provider, GitHub for updates/model sync, configured email/alert
  providers) instead of only OpenAI.
- Reproducible release builds: frontend build uses `bun install --frozen-lockfile`
  and the release/CI Go toolchain is pinned via `go-version-file: go.mod`.

### Removed

- Removed all remaining Langfuse references from documentation and README. The
  Langfuse integration is no longer part of CH-UI.

## [2.4.0]

- Query Insights (Pro): `system.query_log` analytics.
- Cluster Health (Pro): operations and database monitoring.
- Result filters and ClickHouse error parsing in the query results view.

[2.10.1]: https://github.com/caioricciuti/ch-ui/compare/v2.10.0...v2.10.1
[2.10.0]: https://github.com/caioricciuti/ch-ui/compare/v2.9.3...v2.10.0
[2.9.3]: https://github.com/caioricciuti/ch-ui/compare/v2.9.0...v2.9.3
[2.9.0]: https://github.com/caioricciuti/ch-ui/compare/v2.8.0...v2.9.0
[2.8.0]: https://github.com/caioricciuti/ch-ui/compare/v2.7.0...v2.8.0
[2.5.3]: https://github.com/caioricciuti/ch-ui/compare/v2.5.2...v2.5.3
[2.5.2]: https://github.com/caioricciuti/ch-ui/compare/v2.5.1...v2.5.2
[2.5.1]: https://github.com/caioricciuti/ch-ui/compare/v2.5.0...v2.5.1
[2.5.0]: https://github.com/caioricciuti/ch-ui/compare/v2.4.0...v2.5.0
[2.4.0]: https://github.com/caioricciuti/ch-ui/releases/tag/v2.4.0
