# Changelog

All notable changes to CH-UI are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [2.30.0] - 2026-10-23

A faster, more accurate MCP server and the end of the Resend and Brevo alert channels. MCP requests spend about a tenth of the server time they did, results travel gzipped and come back exactly as ClickHouse sent them, and models can describe several tables or run several queries in one call. Retention now covers the governance and alert history that used to be kept for fixed windows.

### Upgrade notes

- **Resend and Brevo alert channels are removed.** On upgrade, existing Resend and Brevo channels are turned off and marked Unsupported in Governance, Alert channels. Delete them and recreate them as SMTP or webhook channels; alerts routed only to them stop delivering until you do (#402).

### Security

- DOMPurify 3.4.17 in the frontend (GHSA-cg2g-xh64-v4q7, GHSA-mrv2-3pqr-j5jr) (#403).
- The ch-ui.com site build pins zod to 4.6.6 (GHSA-mrhx-c7xc-m3w5) (#407).

### Added

- **MCP batching.** `describe_table` accepts `tables` to describe up to 10 tables in one call, and `run_select` accepts `queries` to run up to 5 statements together, so models need fewer turns. Each statement keeps every guard and its own query history entry, and a failed one reports in place (#410).

### Changed

- **Data retention covers more history.** Admin > Settings > Data retention now sets the windows for the governance query log, policy violations, access history and alert evaluations, which used to be fixed in code. Defaults are unchanged (30, 30, 180 and 30 days), and 0 keeps rows forever (#399).
- MCP clients are told to request CSV for multi-row results, which roughly halves the tokens a result costs (#406).
- Incident timeline: events are compact rows in a virtualized list that fills the window, so long windows scroll smoothly and the source filters stay in view. Coverage moved to a Coverage sheet; click a row for its full details (#401).
- Performance, Operations reports, SQL alerts and the report editor link straight to the right Background account for the current connection, and row actions on Admin, Connections are always visible (#400).

### Performance

- Encrypted credentials are decrypted without re-deriving the key each time, cutting about 25 ms of CPU from every MCP request and every other decrypt in the app (#404).
- MCP responses are gzipped for clients that accept it: a 500-row result goes from about 72 KB to under 5 KB on the wire, and event streams still deliver each event immediately (#408).
- MCP `describe_table` and `search_catalog` run their metadata lookups in parallel, answering in one round trip instead of four or two (#405).

### Fixed

- MCP `run_select` and `run_saved_query` return columns in the order the query selected them, and no longer round high-precision Decimal and large integer values through float64. A `Decimal(38, 10)` of `123456789012345678.1234567891` used to come back as `123456789012345680` (#409).
- Brain sees high-precision Decimal and large integer values with every digit, instead of rounded through float64 (#411).

## [2.29.0] - 2026-10-21

A usability release: every page now follows one design contract, Home walks a new instance through getting started, Telemetry receives logs and traces from one sheet, errors say what to do next, and five how-do-I guides cover the first steps. It also hardens credentials and browser requests and moves to Go 1.26.10.

### Upgrade notes

- **ClickHouse redirects are refused** for sign-in, queries, streaming and exports, so credentials never follow a redirect to another host. A connection URL that redirects must point at the final endpoint (#385).
- **Link-local ClickHouse targets are refused.** Every resolved address is checked at connection time and the checked IP is dialled. Private and loopback targets keep working; a connection on a link-local address (169.254.0.0/16, fe80::/10) needs a private or public one (#385).
- **Password login takes JSON only.** The bundled frontend already sends it; a custom login client must send `Content-Type: application/json` (#385).
- **Unsafe browser requests to `/api` must come from an allowed origin.** A separate browser frontend needs its origin allowed. Behind a TLS proxy with no `APP_URL`, the same host is accepted on either scheme; set `APP_URL` to enforce HTTPS origins. Webhook and ingest authentication are unchanged (#385, #386).
- **Docker Compose installs on the old default `APP_SECRET_KEY` upgrade on their own.** The first start generates a new key, saves it as `.app_secret_key` next to the database and re-encrypts stored secrets; nobody is signed out. Remove the old placeholder from your environment afterwards (#385, #386).

### Security

- Built with **Go 1.26.10**, which stops trusting compromised intermediate certificates when verifying TLS (GO-2026-6809). Affects outgoing SMTP over TLS (#394).
- Query audit previews are redacted before truncation, so SQL credentials no longer land in the audit log; existing entries are not rewritten (#385).

### Added

- **Getting-started checklist on Home** for a fresh instance: connect ClickHouse, run and save a query, make a dashboard, add an AI provider, connect an AI client, invite a teammate. Each step opens the right place and ticks itself off; admins see every step, others see theirs (#384).
- **Telemetry: Send data here sets up receiving logs or traces in one step.** It creates the table, the ingest pipeline, a key and the Telemetry source, then shows the shipper config with the key filled in (#388).
- **Five how-do-I guides** on ch-ui.com (first query, first dashboard, ship logs, invite a teammate, connect Claude Code), linked from Home and the matching empty states (#389).
- **Command palette** reaches every page section and adds actions for new dashboard, sending data to Telemetry, new MCP key and new ClickHouse user (#390).

### Changed

- **Navigation**: Operate pages are grouped under Health, Performance and Control; the License page is now **Instance** (old links still work); rail tooltips appear instantly (#391).
- **Admin, MCP Server** leads with connecting a client as yourself through OAuth. Admin-created keys are now called service keys, the table shows who or what each one runs as, and write keys on an admin account are flagged (#372).
- **Errors** across the app say what happened and what to do next, with the original detail underneath, instead of raw server text. Creating or renaming a model onto a taken name returns a clear message (#387).
- **Keyboard**: Esc closes only the topmost menu, dropdown or sheet; Enter submits sheets; focus returns where it was; shortcuts no longer act behind an open sheet (#390).
- **One page shell everywhere.** Home, the pipeline editor, dashboards, database and table tabs use the shared header (with a back arrow and breadcrumbs where it applies), tabs and stat tiles. Every form sheet submits from its footer. Dashboard Move, Edit tags, the version viewer and the alert channel test open as sheets. Creating a pipeline, folder or saved search opens a sheet with a name field (#378, #379, #382, #384).
- **Lists are tables**: Pipelines, Schedules, Fleet, Schema Compare, Reports, Performance and the Models run history are sortable tables with a toolbar instead of card grids; Governance Tables and Query Audit scroll inside the table (#380).
- **Settings show state first** and edit in a sheet: Reports schedule, GitHub sync, governance settings, the Instance page forms and the Performance baseline capture (#381).
- **Models** shows its dependency graph and run history as sections instead of full-screen overlays, with search and a pipeline filter; the header badge counting pipelines is gone (#382, #392).
- Lists and panels show a skeleton while loading, empty lists show the next action, and toggles are real switches for screen readers (#378, #379).
- Charts, gauges, the pipeline canvas and mention chips take their colours from the theme, so they match in light and dark; body text is 13 px everywhere; toggle buttons share one pressed style (#383, #395).
- Brain explains that a provider must be configured instead of failing the first prompt, and Governance offers to turn on sync on a fresh instance instead of an upgrade notice; Turn on sync there asks for the same confirmation as the Settings switch (#373, #397).
- A docs link on the query workspace and dashboard pages, like every other page (#392).
- The SQL editor toolbar uses the shared buttons; Cancel is red while a query runs (#382).
- Connector: failed queries and errors go to stderr and, under systemd, log at WARNING and ERR priority instead of INFO (#376).

### Fixed

- MCP `subscriptions/listen` streams from newer clients (Claude Code 2.1.30 and later) are no longer closed after five minutes (#374).
- Schema changes ClickHouse refuses for a missing grant return 403, and malformed ones 400, instead of 502 (#375).
- Last login, created, updated and audit log times were shifted by the browser's UTC offset (#377).
- Starting or testing a pipeline with no source or sink explains what to add instead of showing a backend error beside a "saved" toast (#377).
- Reports and Performance link to Background accounts instead of describing the path; `/admin?section=connections` opens Connections; the Instance header no longer shows the edition twice (#377).
- Send data here on the Traces tab offered the logs pipeline when it had been opened on Logs first (#393).
- Send data here refused 0 days of retention, which the API accepts as keep forever (#397).
- Toasts were light in dark mode (#395).

### Documentation

- Five how-do-I guides, linked from Home and the matching empty states (#389).
- Pages and steps updated for the Instance page, the Operate subgroups, the table layouts, state-first settings, the keyboard rules in sheets, `POST /api/telemetry/receive` and the connection security rules (#396).

## [2.28.1] - 2026-10-17

OAuth sign-in from Claude Code and other native MCP clients works, and every page in the app links to its docs.

### Added

- **Docs pill in every page header** that opens the matching ch-ui.com page, and per section where one exists (Governance Access control opens RBAC, Schedules Reports opens Reports, Telemetry Sources opens Ingest, Admin MCP opens MCP). Home gets the same pill and a **CH-UI Docs** card under Resources (#367).
- **Dashboard panels can start from a saved query**: a picker above the panel's SQL editor copies the query in, fills the empty name and description, and runs the preview (#368).

### Changed

- The SQL editor shows a hint while it is empty, with the run shortcut, in the query workspace and in the panel builder. **Save** in the editor toolbar has a label instead of a bare icon. Home says Welcome instead of Welcome back on a first run (#368).

### Fixed

- **OAuth sign-in from Claude Code and other native MCP clients.** They register a loopback redirect such as `http://localhost/callback` and then listen on a random port; the authorization endpoint compared the two exactly and refused with `redirect_uri is not registered for this client`. Loopback http redirects now match on scheme, host and path with any port, as RFC 8252 requires; every other redirect stays an exact match (#369).
- **OAuth consent for read-only people.** Claude Code always asks for `read write actions`, and the consent endpoint refused anyone whose CH-UI role is not admin or analyst. Consent now grants what the role allows (read for a viewer), the tokens carry that scope, the audit row records what was requested, and the consent page marks the rest as not granted (#370).

## [2.28.0] - 2026-10-17

Every SSO person can now be their own ClickHouse user, every query CH-UI runs is tagged with the person and object behind it, and Cost Center, Query Insights and the governance query log attribute work to people instead of shared accounts. MCP covers the whole product, and SSO only mode turns off password sign-in.

### Upgrade notes

- **The agent authenticates to ClickHouse with the `X-ClickHouse-User` and `X-ClickHouse-Key` headers** instead of HTTP Basic auth, which cannot carry a user name with a colon such as `sso:ana@example.com`. A proxy between the agent and ClickHouse (chproxy, HAProxy, nginx) must pass those headers through. Update remote agents (`ch-ui connect`) before switching a connection to per-person SSO users (#361).
- **Every query CH-UI runs on someone's behalf now sets `log_comment`** to a JSON tag such as `{"chui":"editor","person":"ana@example.com","object":"dash:42"}`. Brain, MCP, scheduled reports, SQL alerts, dashboard variables and public dashboards no longer use their old `ch-ui:brain`, `ch-ui:mcp`, `ch-ui:report`, `ch-ui:sql-alert`, `ch-ui:dashboard-variable` and `ch-ui:public-dashboard` tags; anything of yours that filtered on them needs the new form. CH-UI's own introspection (Cost Center, Query Insights, Performance, Cluster Health, governance harvest, Incident Timeline) keeps its `ch-ui:<feature>` tags. Users whose ClickHouse profile is `readonly=1` cannot change settings and run untagged (#357).
- **Cost Center and Query Insights count people, not accounts**, wherever the tag names one: the `user` column is the SSO email or job owner, with the ClickHouse accounts shown underneath, and cost-center team rules match that name. Rows without a tag (third-party tools) still show the ClickHouse user (#364).
- **Turning on SSO only signs out every password session** at once, and at startup while it is on (#355).
- Existing MCP keys get no new permissions: lifecycle actions need the per-key opt-in (#353).
- Schema version 2026.10.18 adds the `sso_identities` and `sso_clickhouse_users` tables, `connections.sso_identity_config`, `sessions.sso_identity_id`, `sso_identity_id` on `dashboard_shares`, `oauth_codes` and `mcp_keys`, and `gov_query_log.actor` (#361, #363, #364).

### Added

- **Per-person ClickHouse users for SSO** (Pro, per connection): every person who signs in through SSO gets their own ClickHouse user named `sso:<email>` (prefix configurable), created and kept in sync at each sign-in by a narrow provisioning account scoped to that prefix, with ClickHouse roles taken from their IdP groups, a random password rotated at every sign-in and stored encrypted, and a `HOST` clause pinned to the address ClickHouse sees the agent from. A pre-existing user CH-UI did not create is refused, never taken over; a provisioning failure refuses the sign-in instead of falling back to the shared account. Connection switch provisions the person on the target from their last verified groups. `GET`/`PUT /api/connections/{id}/sso-identity` (#361).
- **Admin → Connections → SSO**: a sheet per connection to choose the shared account or per-person users, set the service or provisioning account (until now API only), map IdP groups to ClickHouse roles, render the one-time **setup SQL** and **Verify** the provisioner with a probe that creates a user under the prefix with each role, confirms it cannot create users outside the prefix or roles, and drops it. **Admin → Users** shows each SSO person's ClickHouse users per connection and can **Remove access** on every connection at once (#362).
- **Lifecycle of per-person users**: an hourly Pro sweep locks users not seen for `lock_after_days` (`HOST NONE`, so `system.query_log` history stays attributable) and drops them after `drop_after_days`. Dashboard shares and MCP OAuth tokens created by an SSO person follow their current ClickHouse user, so password rotation never breaks them, and answer 410 or 401 once the person's access is removed (#363).
- **Query tags**: every workload query carries the feature, the person (SSO email, ClickHouse user, job owner, OAuth subject) and the workspace object (`dash:<id>`, `saved_query:<id>`, `chat:<id>`, `schedule:<id>`, `alert:<id>`, `report:<id>`, `model_run:<id>`, `pipeline:<id>`, `mcp_key:<id>`) in `log_comment`, so `system.query_log` can be split per person even behind a shared SSO or background account. Documented on the Query Insights page (#357).
- **Attribution by person**: Cost Center (active users, trend, users table), Query Insights (user filter, users) and the governance query log attribute rows to the person behind the tag; a per-person SSO user's queries from outside CH-UI land on the same row; the governance log keeps `ch_user` for policies and adds `actor` (#364).
- **SSO only** (Pro): turn off ClickHouse username/password sign-in from Admin → Settings → SSO or with `OIDC_SSO_ONLY=true`. It can only be turned on from an SSO session, applies only while the provider is loaded, and `CHUI_ALLOW_PASSWORD_LOGIN=true` is the break-glass override (#355).
- **MCP operations tools** (Pro, read-only): `firing_alerts`, `list_alerts`, `alert_events`, `list_schedules`, `list_reports` (OAuth sign-ins only) and `cluster_health`; free `query_history` returns the queries this MCP client ran. Keys with a database allow-list see only SQL alerts inside it and cannot read cluster health (#352).
- **MCP lifecycle actions** behind a per-key opt-in (Admin → MCP Server, read + write keys; OAuth scope `actions`): `run_model`, `start_pipeline`, `stop_pipeline`, and on Pro `run_schedule_now` and `run_saved_report_now` (OAuth, own reports); read-only `get_model_run` and `pipeline_status` for every key. Models run from MCP use the key's own ClickHouse user and allow-list (#353).
- **MCP observability tools**: free `telemetry_sources`, `search_logs`, `ingest_status` and `table_lineage`; Pro `search_traces`, `get_trace`, `service_map`, `list_metrics`, `query_metrics` and `list_monitors`, all with the key's own ClickHouse user and allow-list (#354).

### Changed

- Background jobs in session mode never borrow a per-person SSO user; such connections need a dedicated background account (#361).
- The access-control guard labels the SSO provisioner and every provisioned user (#361).
- Brain's query-log diagnostics exclude Brain's own queries by their new tag; Performance no longer hides Brain and MCP queries (#357).
- New audit actions: `sso.user_provisioned`, `sso.user_synced`, `sso.user_locked`, `sso.user_dropped`, `sso.access_removed`, `connection.sso_identity.updated`, `connection.sso_identity.verified`, `sso.password_sessions_revoked` (#355, #361, #362, #363).

### Documentation

- SSO, authentication, admin, audit log, dashboards, MCP, background accounts, governance, security, connections, clusters, Cost Center and Query Insights pages describe per-person users, query tags, attribution and header-based ClickHouse auth (#361, #362, #365).
- The MCP page covers the permission levels and every tool; license, pricing and README list the current MCP Pro tools (#356).

### Dependencies

- golang.org/x/crypto 0.58.0 (#358); astro 7.3.8 and @astrojs/starlight 0.42.6 for the site (#359); svelte 5.57.2, vite 8.3.4, marked 18.1.0, @codemirror/language 6.13.1, @codemirror/view 6.43.14, @types/node 26.6.5 and jsdom 30.1.1 for the UI (#360).

## [2.27.0] - 2026-10-16

CH-UI receives logs, traces and events itself: ingest endpoints for OpenTelemetry, Elasticsearch bulk and Splunk HEC, a durable spool, pipeline transforms and routing, table layouts and ingest alerts.

### Upgrade notes

- **Webhook pipelines are durable.** A 200 means the request is on disk. A full spool answers 503 with Retry-After (was 429), the 1,000-records-per-request limit is gone, and Batch Size and Batch Timeout no longer apply (#342).
- **An `ingest-spool` folder appears next to `ch-ui.db`**, holding requests until ClickHouse has them (2 GiB cap by default, `INGEST_SPOOL_MAX_BYTES`). Include it in backups if you use ingest or webhook pipelines (#342).
- **Tables the ClickHouse sink creates** get `non_replicated_deduplication_window = 1000` and parse ISO 8601 date-times with zones (`date_time_input_format=best_effort`), so rows with such dates that used to go to the dead-letter table now insert. Sinks writing to existing tables are unchanged (#346, #349).
- **Starting a pipeline with an invalid graph or node settings** returns 400 naming the node (was 500), and unknown node types are refused at start (#345, #346).
- **The sink's grant check** needs the updated agent; older agents keep showing "Could not verify" (#343).
- Schema version 2026.10.16 adds the `ingest_keys` table (#342).

### Added

- **Ingest**: CH-UI receives OpenTelemetry logs and traces (OTLP over HTTP, protobuf or JSON, gzip) and JSON events at `/ingest/v1/logs`, `/ingest/v1/traces` and `/ingest/v1/events`, as a new **Ingest** pipeline source (#342).
  - Ingest keys per pipeline: shown once, stored hashed, revocable and audited, accepted as `Authorization: Bearer`, `Splunk`, Basic (key as the password) or ApiKey, never from the URL. Per-key rate limit and per-pipeline daily quota (#342, #344).
  - Every request is fsynced to an on-disk spool before it is acknowledged, then inserted with a deduplication token, so restarts and ClickHouse outages lose nothing and replays are not stored twice (#342).
  - Logs and traces tables are created in the OpenTelemetry layout Telemetry reads, with retention. Live counters in the pipeline editor (#342).
  - `--ingest-addr` / `INGEST_ADDR` serves the receivers on their own port; `INGEST_ONLY_SEPARATE`, `INGEST_SPOOL_MAX_BYTES` (#342).
- **Elasticsearch bulk API** at `/ingest/es`: Filebeat, Logstash, Fluent Bit and Vector Elasticsearch outputs send to CH-UI by changing the host; `index` and `create` are stored, `update` and `delete` are refused per item, and the startup handshake is answered. **Splunk HTTP Event Collector** at `/ingest/services/collector` (event, raw, health) with HEC response codes. Documents and events sent with a logs key are mapped to the `otel_logs` layout (#344).
- **Send data here** in Telemetry: ready configs for the OpenTelemetry Collector, OTel SDKs, Fluent Bit, Vector, Filebeat, Logstash, HEC clients and curl, with the URL and key filled in (#342, #344).
- **Pipeline transforms**: Parse (JSON, logfmt, regex, nginx/Apache, syslog), Map fields (copy, rename, set, drop, severity and timestamp from a field) and Filter between a pipeline's source and sink, for every source. Sample (deterministic, keyed, always-keep rules) and Redact (emails, IPs, card numbers, JWTs, bearer tokens, AWS keys, custom patterns) need Pro. Live before/after preview and per-node counters (#345).
- **Pipeline routing (Pro)**: a Route node sends records to up to 8 ClickHouse sinks by rules, first match or every match, with an optional output for unmatched records. Each branch can have its own transforms, table and dead-letter table; a batch is acknowledged only after every sink took its rows, each sink with its own dedup token (#346).
- **Table layouts** for the ClickHouse sink: Custom from data, Wide events (typed timestamp, service, level, message and trace_id columns plus a JSON or Map attributes column, field mapping, retention, optional raw record), OpenTelemetry logs and traces. **Add columns for new fields** adds a column with `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`, recorded in the run and audit logs (#349).
- **Ingest alerts**: `ingest.stalled` (no data for a per-pipeline threshold, default 15 minutes), `ingest.spool_backlog`, `ingest.rejecting`, `ingest.auth_failures` and `pipeline.sink_failing` reach every alert channel through rules; each opens one incident linked to the pipeline and resolves on its own; open pipeline incidents show in Governance > Alerts (#348).
- Brain's configure_pipeline and the MCP create_pipeline tool can create Ingest pipelines (#342).

### Changed

- Custom table inference reads up to 100 records after the transforms and infers Int64, Float64, Bool, DateTime64(3), Array, JSON, LowCardinality and Nullable types (#349).
- Saving a pipeline checks transform settings, and starting it checks the graph, naming the node at fault; the editor marks it (#345, #346).
- The pipeline editor notices a pipeline started or stopped elsewhere within 10 seconds; pipelines created outside the editor open fitted to the view (#345, #346).
- Notifications appear at the bottom right (#346).
- The pipeline list API includes each pipeline's source type and, for ingest pipelines, its signal (#342).
- The website, docs and README cover ingest, transforms, routing, table layouts, Windows and `ch-ui desktop` (#347, #350).

### Fixed

- Rare "database is locked" errors when two parts of CH-UI wrote to its database at the same moment (#341).
- Pipeline sink checks verify the INSERT and CREATE TABLE grants again instead of always saying "Could not verify" (#343).
- `ch-ui server --port` without `APP_URL` kept the app and tunnel URLs on port 3488 (#342).
- Pipeline runs and test results showed empty Started, Finished, Rows, Bytes and Errors columns (#342).
- Filter, sample and route rules hid their value box (#346).
- Creating a second pipeline with the MCP create_pipeline tool failed with a duplicate node id (#342).

## [2.26.0] - 2026-10-15

Security and bug fixes from the audit follow-up, creating users on import, Windows binaries and a personal `ch-ui desktop` mode.

### Upgrade notes

- **Existing MCP OAuth tokens** are read-only until the person signs in to CH-UI again, unless they have a live session. Tokens now follow the person's current role (#335).
- **Removing a role override** signs that person's affected sessions out, so their role is worked out again at the next sign-in. An admin who removes their own override is signed out (#335).
- **Access files exported by this version** can include `auth` and `hosts` under users; older CH-UI versions refuse those keys (#338).
- **Acknowledging access drift** now requires an admin (#332).
- **Windows support is new**: the binaries are built and tested on Linux and macOS, not yet on Windows itself, and are not code signed (#337).

### Added

- **Create users on import**: access control import can create the users an access file lists that the connection lacks. Each gets a generated password (hashed in the browser), a pasted sha256, double SHA-1 or bcrypt hash, LDAP, Kerberos, an SSL certificate, or no password, plus hosts and an optional settings profile. Generated passwords are shown once after the apply, with copy buttons. Prepare rollback drops the users that apply created; reverting from access history never drops or recreates users (#338).
  - Access exports include each user's authentication type and allowed hosts so an import preselects them. Hashes and salts are never exported.
  - Review checks: an existing user, a missing settings profile or read-only user storage blocks the plan; a user without a password needs an acknowledgement.
- **Windows binaries**: `ch-ui-windows-amd64.exe` and `ch-ui-windows-arm64.exe` in every release, listed in the signed checksums.txt. On Windows, `ch-ui update` replaces the running .exe, PID checks work, `ch-ui server --detach` starts without a console, the connector keeps its files in `%AppData%\ch-ui\connect`, and `ch-ui service` says it is not supported yet (#337).
- **`ch-ui desktop`** runs CH-UI for yourself on this computer: it listens on 127.0.0.1 only, keeps the database and secret key in your user data folder (owner-only), uses port 3488 or any free port and opens your browser. Running it again opens the instance already running. On Windows, `ch-ui.exe` with no arguments (a double-click) starts it; `ch-ui.exe server` runs a server. macOS and Linux are unchanged (#337).
- **Alert emails** include an "Open in CH-UI" link to the page the alert is about, and custom templates can use `{{link}}` (#332).
- **Open access drift incidents** appear in Governance › Alerts for admins and can be acknowledged there (#332).

### Changed

- `ch-ui uninstall` stops a running `ch-ui desktop` and keeps its data folder, printing where it is and how to delete it (#337).
- The 200-change cap message in access control says to import or apply in batches (#338).

### Fixed

- **Dashboards**: clicking a DateTime value with a "Filter by" link selects the matching variable option and filters the other panels, also on share links (#333).
- **SQL workspace** (#334):
  - Saving a query no longer silently overwrites a newer save or a version restore of the same saved query; the editor offers to overwrite, open the newer version in a new tab, or cancel.
  - The inline profile of an earlier run no longer appears under a newer run.
  - Sorting or filtering a truncated result while Run All is running no longer stops the remaining statements.
- **Reports** stop sending a saved query their author can no longer open, for example after the owner made it private. The run is recorded as failed with the reason and counts toward the automatic disable (#334).
- **Roles**: removing a role override no longer forces "viewer" on the person's open sessions (#335).
- **Admin › SSO** keeps showing a failed provider reload after the page is reloaded (#335).
- **Access control review**: several warnings of the same kind, such as dropping two users, each need their own tick (#338).
- **Version indicator** notices a new release within an hour, also in tabs that stay open (#336).

### Security

- MCP OAuth tokens follow the person's current CH-UI role: a demoted person loses the write tools on the next MCP request, and refreshing drops write scope for good (#335).
- The MCP database allow-list checks `IN` inside function arguments such as `position(..., 1 IN otherdb.t)` (#335).
- SSO sign-in always applies the latest saved allowed domains, group mapping and email verification setting, even when reloading the provider failed. If the identity provider itself changed and could not be loaded, SSO sign-in is refused instead of using the old one (#335).
- Brain's delete_model refuses models on other connections and GitHub-managed models (#335).
- A new user's password hash and salt appear only in the statement sent to ClickHouse; the review, results, plan hash, audit log, access history and server logs show '***', and ClickHouse errors that quote the statement are scrubbed. CH-UI's server never receives a plaintext password (#338).

## [2.25.0] - 2026-10-15

### Added

- **Data lineage**: see how data flows between tables, materialized views, views, dictionaries and external sources (Kafka, S3, URL, MySQL and table functions).
  - **Explore › Lineage**: pick databases, search for a table to focus on, choose 1 to 4 hops and filter by kind. Other databases collapse into one box each.
  - **Lineage view on every table page**, next to Overview, Schema and Data Sample, with "Open in Lineage".
  - Click a node for its engine, size, refresh status and definition, with Focus here, Open in editor and Open table.
  - Solid edges come from ClickHouse's own dependency columns; dashed edges are parsed from view and MV definitions and labelled "parsed". Inner tables fold into their materialized view; objects you cannot see or that no longer exist are marked.
  - Built to stay fast: nothing runs until you open it, it reads system tables once with your own ClickHouse account and caches the result for 30 seconds, never reads `query_log`, and asks you to narrow the view above 400 objects.
  - Free in every edition, for every role. API: `GET /api/lineage`, `GET /api/lineage/node` (#329, #330, Discussion #105).

## [2.24.0] - 2026-10-15

Bug fixes from a full audit of the workspace, dashboards, alerting, schedules and secrets, with a few behaviour changes listed below.

### Upgrade notes

- **Cron day rules follow standard cron.** Schedules, reports and model pipelines that restrict both day of month and day of week now run when either matches, so they may run more often (`0 9 1 * 1` now runs on the 1st and on every Monday). Day of week `7` is Sunday, so `5-7` now includes Sunday (#324).
- **Scheduled queries run in their own timezone.** Before, they ran in UTC whatever timezone was set. Their next run is recomputed once on upgrade; an unknown timezone is refused when saving (#324).
- **Run now on someone else's SQL alert is a dry run.** It shows the result without recording state or raising events; the creator and admins still record (#325).
- **Variable "All" with 1,000 or more options** now works only as `column IN ($v)` and fails with a clear message elsewhere; custom lists over 1,000 values are refused on save (#327).
- **Weekly operations reports accept only email channels** (#325).

### Added

- **`APP_SECRET_KEY_PREVIOUS`**: rotate the app secret key safely. Stored secrets decrypt with the previous key as a fallback, and on startup every stored secret is re-encrypted with the current key (#323).

### Fixed

- **SQL workspace** (#326):
  - Users whose ClickHouse profile is `readonly=1` can run queries; CH-UI enforces the row limit when ClickHouse refuses the setting.
  - The column preview no longer runs statements that change data (for example `REPLACE TABLE`, or `CREATE ... AS SELECT` after a `#` comment); only read-only queries are probed and everything else runs exactly once.
  - Closing a query tab, closing a running result, or switching connection cancels the running query.
  - "Go to error" lands on the right character; `SELECT 1; -- note` runs; Sample works with a trailing comment; String parameters accept an empty value; Option+W/D/S no longer fire while typing.
- **Dashboards** (#327):
  - Selecting All on a variable with 1,000 or more options covers every value instead of the first 1,000.
  - Report summaries split charts by their Series column, and "last" is the latest point on the x axis.
  - Stat and gauge panels no longer count NULL or empty values as 0.
  - Drilldown passes the clicked value as returned, so a Date no longer shifts a day east of UTC.
  - Brain keeps the panels it adds inside the grid and records dashboard versions.
- **Alerting** (#325):
  - Turning Cluster Health sampling or a check off resolves its open incidents and clears their acks.
  - Queued deliveries are cancelled when their rule, channel or binding is turned off, or a silence covers them.
  - Editing an alert rule with an invalid channel no longer saves part of the edit.
- **Schedules** (#324): cron day of week 7 is Sunday; both day fields use the standard "either" rule; scheduled queries run in their own timezone.
- **License** (#323): a license from `CHUI_LICENSE` or `CHUI_LICENSE_FILE` in its grace period is kept on restart (read-only Pro); the startup log names the active license.

### Security

- Share links no longer receive the dashboard owner, folder, tags or panel connection ids, and show a generic error instead of ClickHouse error text (#327).
- Non-admins no longer see policy violation and access drift events in the alert events list (#325).
- The governance access API refuses to drop the signed-in admin's own ClickHouse login or a login CH-UI uses unattended, unless forced (#325).

## [2.23.1] - 2026-10-15

Security release. Every install should upgrade.

### Upgrade notes

- **Role overrides apply per connection.** On upgrade, each existing role override for a ClickHouse user (password login) is copied to every saved connection, so current access is kept; review Admin › Users per connection afterwards. A connection added later starts with no overrides until an admin sets them. SSO people keep their overrides on all connections. Connections created from the login page (sign in with a ClickHouse URL) grant at most the analyst role. `ch-ui reset-admin --user` takes a new `--connection` flag (#321).
- **SSO needs a verified email.** If your identity provider never sends the `email_verified` claim (Microsoft Entra ID commonly does not), turn on "Accept emails without email_verified" in Admin › Settings › Configure SSO, or set `OIDC_ACCEPT_UNVERIFIED_EMAIL=true`, or people cannot sign in with SSO (#318).
- **Scheduled reports are private** to their creator and admins; the report list shows your own reports (#320).
- **Dashboard share links**: only the creator of a link or an admin can see or copy it; other writers can still see and revoke it (#320).

### Security

- **Tunnel tokens** are no longer included in connection list and detail responses; only admins can read them, from the token endpoints. Regenerating a token or deleting a connection disconnects any agent already attached (#319).
- **Role overrides** for ClickHouse users apply only on the connection they were set for; connections added from the login page never carry overrides, never make the first admin and grant at most analyst; password logins named `sso:...` are refused (#321).
- **SSO** refuses email addresses the identity provider has not verified (#318).
- **Dashboard share links**: listing them needs write access, and only the creator of a link or an admin sees its URL (#320).
- **Scheduled reports**, their previews, delivery history and CSV downloads are visible only to their creator and admins, and a report can only use a saved query its author can open (#320).
- **ClickHouse user passwords** are masked in executed commands, error messages and server logs (#320).
- The public **license endpoint** no longer returns the customer name or license ID (#320).

### Fixed

- The SQL editor showed "No rows returned" while a query was still running, before its rows arrived (#316).
- On a multi-node cluster, an access drift alert could resolve when snapshots were read from another node that lacked an entity kept in local storage (#317).

## [2.23.0] - 2026-10-14

### Upgrade notes

- **Two new tables are created on start** (access history snapshots, Brain budgets) and existing Brain usage rows get a stored cost. This runs once and needs no action (#300, #304).
- **Brain's daily token budget becomes the global daily rule** in Admin › Brain budgets; the old setting keeps working and stays in sync. Brain usage rows from before the upgrade are priced once from the current price table and marked "est." (#300).
- **Brain usage API keys renamed.** In `GET /api/admin/brain/usage` totals, `unpriced_rows` is now `unpriced_requests` and `users` is now `groups` (`users` is still sent when grouping by person) (#300).
- **`?section=alerts` now opens the fired alert events.** Rules and channels have their own sections, `?section=alert-rules` and `?section=alert-channels` (#310).
- **Access control previews print grants the way SHOW GRANTS does**, so `GRANT ALL ON db.*` shows as the privileges it expands to, and plans now follow ClickHouse when a revoke covers narrower grants (#302).
- **Multi-series charts change colors** to the new categorical palette; colors you picked on a panel are kept (#311).

### Added

- **Dashboard drilldown**: panels can have links that run on click. Filter the dashboard by the clicked value, open another dashboard with variables and the clicked time range, open a filled-in query in the SQL editor (not run), or open an http(s) URL. Templates take `${__value}`, `${__field}`, `${__series}`, `${__row.<col>}` and dashboard variables. Share links keep filter and URL links only (#305).
- **Histogram, Scatter and Heatmap** panel types, also in the SQL editor's chart view and to Brain; time series panels can be drawn as a filled area and stacked (#298).
- **Access history** (Governance, Pro): a history of each connection's ClickHouse access control, captured on every apply and revert, when Access control is opened and the state changed, on each governance sync, and on demand. Changes made outside CH-UI are flagged as drift; each snapshot shows what changed and who gained or lost access through which role, compares with any snapshot or the live server, and can be reverted through the normal review. Kept for 180 days, up to 500 snapshots per connection (#304).
- **Access drift alerts**: drift raises an `access.drift` alert event (warn for an outside change, error for an apply that did not land as planned) that rules, silences and acks can use, linking to Access history and resolving when access returns to the earlier state (#312).
- **Access as code** (Pro): export a connection's roles, default roles, privileges, role grants, row policies, quotas and settings profiles as YAML or JSON (plus read-only SQL), and import such a file into the same or another connection with optional per-kind prune, user/role and database renames and a kinds filter. Import previews the changes and what it skips, then stages them into the normal review. Passwords and users.xml entities are never included, and import never creates users (#306).
- **Brain budgets per person and per role** (Pro), per UTC day or month, with token and dollar limits; the most specific rule applies and a rule without limits exempts people from broader ones. People see a warning at 80%, and a refused request names the rule, the amount used and when it resets. Dollar budgets can refuse models without a price. Admin › Brain is split into Brain, Brain budgets and Brain usage (grouped by person or role) (#300).

### Changed

- **Brain costs are stored when a request runs**, so editing prices no longer rewrites past costs; unpriced requests show no cost instead of $0 (#300).
- **Chart colors**: multi-series charts use a categorical palette (orange, blue, teal, violet, pink, gold, lime, gray) with at least 3:1 contrast in both themes. Panels save only the series colors you pick, and the color picker offers the chart palette first (#311).
- **Governance alerts** are three sidebar sections instead of sub-tabs: Alerts, Alert rules and Alert channels (#310).

### Fixed

- **Access control rollback** no longer drops a privilege held on a database when undoing `GRANT ALL` on one of its tables, and previews and rollbacks follow ClickHouse for grants on nested objects and privilege groups (ALL, ALTER and similar). Privileges such as `dictGet` and `S3` can be granted (#302).
- **Telemetry** no longer reloads in a loop when the stored ClickHouse password cannot be decrypted; it shows the error (#297).
- **Alert rules for report events** (failed, disabled, all) could not be saved (#312).
- **Chart tooltips** were cut off at the edge of short dashboard panels (#313).
- **Time series split by a series column** drew nothing when the series did not share timestamps (#298).
- **Brain budgets match SSO people** by their user-role key (#300).

### Docs and site

- Docs for the new panel types, drilldown, access control previews, access as code, access history, Brain budgets, and the API and audit references (#308).
- The newsletter signup is removed; ch-ui.com is fully static, and the privacy policy no longer lists Resend (#314).

### Internal

- CI: the race detector runs in four parallel shards and, on pull requests, only for affected packages; live ClickHouse tests run in their own job. The longest check went from about 6m45s to about 2m30s (#309). Test databases copy a migrated template instead of migrating per test (#301, #307).

## [2.22.0] - 2026-10-14

### Upgrade notes

- **Scheduled reports need a dedicated background account.** Reports run only with a `report` background account set up in Admin › Connections, never as the person who created them (#292).
- **Switching to a different server asks for its password.** The switcher reuses your current ClickHouse password only for a connection to the same host and port; any other server, including agent connections, asks for its own (#282).
- **Refused exports return 422.** A server-side export that ClickHouse refuses (syntax, grants, export limits) now returns 422 instead of 502; scripts that check for 502 should accept both. An unreachable ClickHouse is still 502 (#281).
- **Alert fingerprints carry an incident id.** Health and SQL alert events now use `...:<severity>:<since>` style fingerprints so each incident is delivered; webhook receivers that deduplicate on the exact fingerprint see a new value per incident (#286).
- **Access control plans keep the order you staged.** A plan that uses a role or profile before the step that creates it is now refused with "stage the create first" instead of being reordered (#285).
- **Docker Compose quick start** now creates a ClickHouse `admin` user (password `change-me-admin-password`, override with `CLICKHOUSE_ADMIN_USER` / `CLICKHOUSE_ADMIN_PASSWORD`). The bundled ClickHouse refused the passwordless `default` user over the network, so the old file could not sign in (#283).

### Security

- **Connection switcher** no longer sends your current ClickHouse password to a different server, and nothing is sent to the target until you type its password (#282).

### Added

- **Chart a query result**: a Chart view in the SQL editor's result footer suggests a stat, time series or bar chart; change type, X, Y and a Series column, then **Add to dashboard**, placed below the existing panels. Dashboard charts can split one Y column into series by another column (#287).
- **Brain for ClickHouse performance** (Pro): the agent diagnoses slow and failed queries from the query log, EXPLAIN, parts, merges and mutations, and compares what a rewritten query would read against the original (#289).
- **Ask AI checks its SQL** with EXPLAIN and repairs it up to twice when ClickHouse rejects it; **Fix with AI** on query errors shows a checked, minimal fix as a diff and replaces the statement in one undo step (Pro). Brain usage shows SQL repairs, and Claude Haiku 5.5 is pre-selected as the quick model until an admin picks one (#289).
- **Effective access** (Governance, Pro, admins): what a ClickHouse user can do on a database, table or column and why, through role paths, partial revokes, row policies, settings profiles and quotas, at login and after SET ROLE; or who holds a privilege on an object. Each answer is cross-checked with SHOW GRANTS. Access control's user sheet links to it (#291).
- **Connection unreachable** health check: alerts when a connection's tunnel is offline or it stops answering, even with Cluster Health sampling off (#288).
- **Alert silences** (Governance › Silences) mute matching alerts for a maintenance window and deliver incidents still firing when it ends; **acknowledge** a firing health check or SQL alert to stop further notifications for that incident; alert rules can be **limited to specific connections** (#288).
- **Saved queries** get nested folders, tags, sections (All, Mine, Shared, Verified), visibility (everyone, owner, admins) enforced for Brain and MCP keys too, and a version history of the last 50 changes with restore. The editor's Save form asks for a folder and visibility (#290).
- **Scheduled report delivery** (Pro): a saved query result (CSV attachment, inline table or both) or a text summary of a dashboard on a cron to email, Slack, Teams or webhook channels, with delivery history, CSV download, retries, auto-disable after 10 failures and an optional allowed email domains setting (#292).
- **Subscribe** on dashboards and saved queries opens the report form prefilled (#292, #294).

### Changed

- **Ask AI edits the query in the editor** (the selection or the statement under the cursor) instead of starting over; one undo restores the previous SQL (#284).
- Login and installation docs cover the `--detach` setup code log, first steps, Anthropic as a Brain provider and first-admin rules (#283).

### Fixed

- **Access control** runs changes in the order staged, matching the preview; rollback restores the exact previous grants, default roles and memberships, including everything a dropped role was referenced by; the lockout guard catches quotas on your own session, revoking your own grant option, and read-only profiles that reach you through a role or inheritance (#285).
- **Alerts**: a re-fire shortly after a resolve is delivered instead of swallowed by the rule cooldown; turning off, editing, deleting or auto-disabling a firing alert sends RESOLVED; critical incidents step back to warning; incidents on nodes that left the cluster resolve; a failed event write retries instead of being lost (#286).
- **Brain**: a paragraph break between the steps of a multi-step answer; the daily token budget holds inside a long answer; the step limit is explained; renaming or archiving a chat during an answer is kept; an approval clicked at the 5-minute timeout runs the action (#284).
- The "Slowest queries today" starter on Home explains why it is unavailable when `system.query_log` cannot be read (#283).
- The active result tab is clearly marked in light mode (#293).

## [2.21.0] - 2026-10-13

### Upgrade notes

- **SQL alerts need a dedicated background account.** Scheduled SQL alerts run only with a `sql_alert` background account set up in Admin › Connections; there is no fallback to someone's session (#279).
- **ON CLUSTER DDL shows per-host rows.** Run from the SQL editor, it now returns one status row per host. Remote `ch-ui connect` agents need v2.21.0 for this; older agents keep working without the rows (#278).
- **Workspace tabs are kept per person and connection.** On first load after upgrading, existing tabs move to the current person and connection (#277).

### Security

- **Switching connection** requires a JSON, same-origin request, refuses the hidden setup connection, is rate limited per IP, and no longer turns an SSO session into a password session. A browser tab still on an old connection is refused instead of running queries on the new one (#277).

### Added

- **Connection switcher** in the sidebar and command palette. The same ClickHouse user switches with the password already in the session; tabs, pinned results and recent commands are kept per person and connection (#277).
- **Access control editor** (Governance › Access control, Pro, admins): ClickHouse roles, privilege and role grants, default roles, row policies, quotas and settings profiles. Every change set is reviewed first with a per-entity diff, numbered SQL, optional ON CLUSTER and warnings; statements stop at the first failure and can be rolled back from a preview. Changes that would lock your own user out are refused, and every statement is audited (#278).
- **Edit grants** from Admin › Users opens a ClickHouse user in Access control (#278).
- **Health alerts**: six built-in Cluster Health checks (replication lag, read-only replicas, disk, parts pressure, stuck mutations, unreachable nodes) with thresholds per connection, escalation and resolved events (#279).
- **SQL threshold alerts**: scheduled read-only queries compared to a threshold, with test run, run now, evaluation history and auto-disable after 5 errors (#279).
- Alert rules accept prefix wildcards such as `health.*` and `sql.*`; Cluster Health shows how many alerts are firing (#279).

### Fixed

- A failed connection switch no longer signs you out, and signing in as someone else without a reload no longer shows the previous person's tabs (#277).

## [2.20.0] - 2026-10-13

### Upgrade notes

- **Model schedules are now named pipelines.** Existing schedules are copied into pipelines automatically on first start. The API endpoints `/api/models/schedules` and `/api/models/schedule/{anchorId}` are removed; use `/api/models/pipelines` (#271).
- **Full result export needs an updated agent.** Remote `ch-ui connect` agents must run v2.20.0 to export from the server; older agents get a clear "update the agent" message. Limits are set with `export_max_bytes` / `export_max_rows` (#270).

### Added

- **Anthropic provider for Brain**: native Messages API with streaming, tool use and prompt caching; Sonnet 5.5 for the agent, Opus 5.5 for harder work, Haiku 5.5 for quick tasks (#268).
- **Brain usage and cost**: every chat turn and Ask AI request records model and tokens; answers show them with an estimated cost, and Admin > Brain > Usage lists them per person and model. A daily token budget per person, model roles (agent and quick), and streaming Ask AI (#268, #274).
- **Dashboard variables**: query-driven, custom, text, constant and interval, with multi-select, All, cascading options and URL state. Values are always quoted server side; share links accept only real option values (#272).
- **Dashboard templates**: ClickHouse server health and Query activity; **version history** with one-click restore; **duplicate** a dashboard (#272).
- **Incremental models** (append or delete+insert by unique key, watermark column), PARTITION BY and engine arguments (e.g. ReplacingMergeTree), and **materialized view models** (#271).
- **Data tests on models**: not null, accepted values, relationships and custom expressions, as warnings or errors (#271).
- **Named model pipelines** with selectors like `+orders+`, cron and timezone; renaming a model updates them (#271).
- **Compile and preview** a model before running it; GitHub-synced models support the new options and tests (#271).
- **Result tabs**: running several statements gives one result tab per statement, and results can be pinned so a re-run keeps them (#270).
- **Full result export** from the server as CSV, TSV, JSONLines or Parquet, without the row limit, read only (#270).
- **Pipeline checkpoints**: pipelines resume from the last written position after a restart, with no duplicate rows (#269).
- **Dead-letter table**: rows ClickHouse refuses go to `<table>_dlq` instead of blocking the pipeline (#269).
- **Pipeline Test button**: sample the source, check sink permissions and preview rows without writing; new sink tables get inferred column types (#269).

### Fixed

- **Brain**: token usage is stored before an answer is marked done, so it shows up right away (#274).
- Flaky tests in the telemetry monitor and query export suites (#273, #275).

## [2.19.0] - 2026-10-13

### Upgrade notes

- **Models never replace objects they did not build.** A table model whose name matches a table someone created by hand is now refused with a clear error instead of dropping that table. Models mark what they build with a `ch-ui model <id>` comment; `ALTER MODIFY COMMENT` is recommended for the model account but not required (#259).
- **Theme follows the system by default** for users who never picked one (#263).
- **Testing a connection with credentials is admin only** and shares the sign-in rate limit (#263).
- **Brain write tools need the admin or analyst role**, and approving a Brain action needs an active Pro license (#255, #264).

### Security

- **Brain respects roles and governance.** Viewers are no longer offered or allowed tools that change dashboards, models, saved queries, pipelines or schedules; approvals fail closed; Brain queries pass the same guardrails as the SQL editor, are logged as Brain, and tool output is fenced as data (#264).
- **Dashboard panel queries follow governance policies**, after variables are expanded, and results are capped at 100,000 rows. Public share links apply the sharer's policies (#262).
- **Pipeline secrets are encrypted** and shown as `***`; SQLite sources are admin only and cannot read the CH-UI data directory; auto-created tables quote identifiers and allowlist engines; new webhook sources require a token (#260).
- **Rate limit on every credential check**: switching connection and testing a connection count toward the sign-in lockout (#263).
- **Query audit is admin only**, queries can no longer hide from audit with a comment, failed or denied queries are harvested, and creating a ClickHouse user that already exists no longer grants it roles (#265).
- **MCP**: insights and costs tools respect database allow-lists, key changes are audited per person, and OAuth refresh tokens are single use (#264).
- **Webhook alert targets** on private or loopback addresses are refused unless an admin allows them on the channel; Cost Center config is admin only and audited (#265).
- **A license dated more than a day in the future is rejected** (#255).

### Added

- **Editor keyboard shortcuts**: Cmd/Ctrl+S saves, Shift+Alt+F formats, Esc or Cmd/Ctrl+. cancels a running query, Tab indents (#254).
- **Run one statement or all of them**: Cmd/Ctrl+Enter runs the statement under the cursor when the editor holds several; Shift+Cmd/Ctrl+Enter runs them all and stops at the first error (#261).
- **Cell inspector** with JSON, arrays and maps pretty-printed; **explorer shortcuts** Preview 100 Rows, Show DDL and Copy Column List; DDL and INSERT show rows written; rows appear while they stream (#261).
- **Webhook alert channel** for any HTTPS endpoint, Slack and Teams, with optional secret header and HMAC signature (#265).
- **Kill a running query or mutation from Cluster Health**, with confirmation and an audit entry; full query text opens in the editor; max disk used tile (#265).
- **Version indicator**: the sidebar always shows the running version and whether it is the latest (#263).
- **Keyboard shortcut sheet** (press ?), **first-run Home** with starter queries, **system theme**, and **gzip** for the web app (#263).
- **Pipeline Runs sheet** with runs and per-run logs, last error and last batch time in the editor (#260).

### Fixed

- **Models**: table rebuilds go through a temporary table and swap, so a failed run never loses data; failed scheduled runs are recorded as failures; schedules no longer duplicate and can be paused; Run saves first; GitHub sync retries failed commits and reads one commit; models stay on their connection; SQL with `--` inside strings runs unchanged; interrupted runs are closed on restart (#259).
- **Pipelines**: write failures retry with backoff and sources advance only after a successful write, with a dedup token per batch; shutdown no longer hangs and running pipelines resume after a restart; zero intervals no longer crash the server; Kafka reconnects; webhooks accept a request whole or not at all; S3 endpoint, CSV and JSON parsing fixed (#260).
- **Dashboards**: panels created outside the editor draw again; `$__timeFilter` works on DateTime and epoch columns with one predicate per column; legends, NULL gaps and time zones are right; import is all or nothing; stale responses no longer overwrite newer ones; public dashboards are cached and rate limited per link; viewers see no edit controls; auto-refresh stops when you leave the page (#253, #262).
- **SQL editor**: CSV and TSV exports keep negative numbers; Format uses ClickHouse `formatQuery()` and no longer changes queries; saved query parameters persist; Explain uses the same statement and parameters as Run; autocomplete refreshes and retries; queries no longer get stuck running; a cancelled query says so (#254, #261).
- **Telemetry**: log paging no longer skips rows; colon values and `!=` on attributes work; facets keep their other values and mark sampled counts; the service map keeps its edges; trace lookups are time-bounded; metric rates and percentiles are correct; monitors allow for ingestion lag and alert when evaluation fails; live tail fetches only new rows (#266).
- **Alerts and schedules**: stuck deliveries are retried, channel secrets survive edits, schedules run in parallel, the query harvest keeps its position, Cluster Health shows unknown instead of green when a metric fails (#265).
- **Brain**: long chats stay within context, long turns keep the stream alive, Stop during an approval saves the message, keyless OpenAI-compatible providers work, Ask AI uses views and comments (#264).
- **Sign-in and navigation**: a failed sign-in keeps the form; Option shortcuts work on macOS; unreachable ClickHouse is no longer reported as a wrong password; the explorer opens on small screens; an expired session returns you to the same page (#263).
- **Accessibility**: dialogs trap focus and close with Escape, side sheets open on their first field, clickable table rows work from the keyboard, icon buttons have labels; light theme tooltips are readable; detail panels ignore late responses (#256).
- **Grace period**: alerts, weekly reports and the performance monitor keep running; a damaged encrypted value no longer crashes the server; table-rebuild migrations are all or nothing (#255).

### Changed

- **Faster first load**: the sign-in page loads about 90 kB gzipped instead of about 600 kB, and pages load on first visit (#263).
- **CI and releases**: runs on main are never cancelled, releases run the full CI on the tagged commit, pull requests build the Docker image and check the Helm appVersion; Docker base images and Bun are pinned (#258).
- **Docs** describe what exists: Models, Dashboards macros, Brain, the CLI and licensing (#252, #259, #262, #264).
- Frontend cleanup: unused code removed, loose types replaced, 13 px body text everywhere (#257).

## [2.18.0] - 2026-10-12

### Changed

- **The GitHub push webhook needs a webhook secret.** Save one under
  Admin → GitHub (new **Webhook secret** field) and put the same value in the
  GitHub webhook settings. Until then pushes no longer trigger a sync; **Sync
  now** is unaffected (#248).
- **MCP keys with a database allow-list check the SQL they run.** Queries that
  read outside the allow-list, or that CH-UI cannot verify (most table
  functions, `dictGet`, system tables), are rejected with the reason. MCP and
  Brain queries can no longer carry a `SETTINGS` clause (#250).

### Security

- **Alert channels, alert rules and governance tags are admin only to
  change.** Any signed-in user, viewers included, could create, edit or delete
  alert channels and rules, send a test alert through a channel, and create or
  delete sensitivity tags. These now need the admin role, and the Alerts page
  hides those buttons for everyone else; reading stays open. Deleting a tag is
  also limited to tags on the current connection (#246).
- **Pipelines stay on their connection.** A pipeline runs with its
  connection's credentials, but the API accepted any `connection_id` when
  creating one and never checked the connection when reading, editing,
  starting or stopping one. A user signed in to one connection could create
  and run a pipeline on another and read its runs and logs. Pipelines are now
  created on the connection you are signed in to, the list shows only that
  connection's pipelines, and a pipeline on another connection is not found.
  Run logs are only returned for a run of the named pipeline, and the runs and
  logs lists are capped at 500 and 1,000 entries (#247).
- **The GitHub webhook requires a secret.** CH-UI never stored a webhook
  secret, so the push webhook accepted unsigned requests and anyone could
  trigger syncs. Every delivery must now carry a valid `X-Hub-Signature-256`,
  payloads are capped at 5 MiB, and one sync runs per connection at a time
  (#248).
- **MCP keys with a database allow-list can no longer read other databases
  through SQL.** `run_select`, `run_saved_query`, `explain_query` and
  `estimate_query` check every table a statement reads and reject anything
  outside the allow-list, including unqualified tables in a non-allowed current
  database, table functions such as `url()` or `remote()`, and `dictGet`. Keys
  without an allow-list are unchanged (#250).
- **MCP and Brain queries run with ClickHouse `readonly = 1`, so a query
  cannot lift its own row, time or size limits.** Brain chat artifacts return
  at most 1,000 rows (#250).
- **A query can no longer lift a row or time cap through the read-only
  retry.** When ClickHouse refused a setting, CH-UI dropped its own cap and
  retried, even when the statement itself set that setting. It now keeps the
  cap and returns the error (#249).
- **Parallel login attempts can no longer get past the rate limit.** The limit
  check and the attempt record happen together now, and a login that ends
  early gives its slot back (#248).
- **The agent WebSocket limits unauthenticated clients.** A client must
  authenticate within 10 seconds and cannot send frames over 64 KiB before it
  does (#249).
- **OAuth client metadata fetches check the address actually connected to**,
  so a DNS answer that switches to a private address is refused (#248).
- **Pipeline webhook tokens are compared in constant time.** Prefer the
  header; `?token=` still works but ends up in proxy logs (#248).
- **Identifier and string quoting escapes backslashes** in MCP, Brain,
  telemetry and admin user statements, so a name ending in `\` cannot break out
  of its quotes (#249, #250).

### Fixed

- **A cancelled streaming query no longer freezes its tunnel.** When the
  browser aborted a streamed query, the agent connection could stop processing
  messages, so every later query on that connection hung (#249).
- **A tunnel disconnecting during a stream no longer crashes the server.** A
  race could panic with "send on closed channel" and stop the whole process.
  Streams now end through a single path, and a crash in a tunnel connection is
  logged instead (#249).
- **`ch-ui update` no longer hangs on a stalled network**, and large list
  requests are capped: model runs at 500, audit log export at 100,000 rows
  (#248).

## [2.17.1] - 2026-10-12

### Security

- **Shared dashboard links run only their own panels.** The public query
  endpoint ran whatever SQL the request carried, with the ClickHouse
  credentials saved on the share, so anyone holding a link could run any
  statement those credentials allowed. A viewer now asks for a panel by ID and
  the server runs that panel's stored SQL, read only (`readonly=1`) and capped
  at 30 seconds and 32 MB of results. When the sharer's ClickHouse profile is
  already read only, which refuses those caps, the panel runs with
  `readonly=1` alone. The public dashboard response no longer includes panel
  SQL, and a share whose expiry date cannot be read is treated as expired
  (#243).

### Fixed

- **Read-only ClickHouse users on Pro pages.** Performance, Schema Compare,
  Operations Reports and Incident Timeline send limits such as
  `max_execution_time` and `log_comment` with their queries. ClickHouse
  refuses any such setting for a user whose profile is already `readonly=1`,
  so those pages failed for read-only accounts (Schema Compare said to check
  credentials). CH-UI now drops the setting ClickHouse names and retries;
  `readonly=1` and query parameters are always kept, and the profile still
  blocks writes (#244).
- **Helm chart image.** The chart's `appVersion` was still `v2.13.0`, so a
  default install ran an old release. It now points at v2.17.1 (chart
  0.7.1).

### Changed

- Contributor license agreement, CONTRIBUTING guide and a CLA check on pull
  requests (#242). No change to the shipped binaries or image.

## [2.17.0] - 2026-10-11

### Added

- **`ch-ui reset-admin`.** Recovers the CH-UI admin role when nobody who holds
  it can sign in. With no flags it removes every admin assignment, so the next
  ClickHouse admin who signs in becomes the CH-UI admin, as on a fresh install.
  `--user <name>` makes one user an admin instead. Safe to run while the server
  is up (`docker exec <container> ch-ui reset-admin` in Docker), and recorded in
  the audit log. The sign-in notice for a ClickHouse admin who is not a CH-UI
  admin now mentions it (#238).

### Fixed

- **`DELETE` and `UPDATE` in the SQL editor.** They were treated as reads, so
  CH-UI first sent them with `LIMIT 0` appended to fetch column metadata, and
  ClickHouse rejected that with a syntax error. They now run as writes, like
  `INSERT` and `ALTER`. `UNDROP` and `EXCHANGE` are classified as writes too
  (#237).

### Changed

- Site: http-cache-semantics 4.3.0 (GHSA-ch52-4w7c-c8xp, build-time only),
  and the site audit no longer ignores that advisory (#240). An MCP server
  test no longer flakes on background writes (#239). No change to the shipped
  binaries or image.

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
