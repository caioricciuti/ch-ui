---
title: Alerts
description: "Alerting for governance, scheduled jobs, cluster health checks and SQL threshold alerts: SMTP, Resend, Brevo and webhook (generic JSON, Slack, Teams) channels, rules with severity thresholds and cooldowns, delivery retries, and an event history"
---

Alerts turn governance, scheduler, telemetry, cluster health and SQL alert events into notifications: a policy blocks a query, a scheduled job fails or runs slow, a telemetry monitor starts firing, a replica falls behind, a query you wrote crosses a threshold, and the right people hear about it through your own SMTP server, a transactional provider, or a webhook into Slack, Teams or your own endpoint.

Alerts are a Pro feature. Manage them under **Governance → Alerts**; the API lives at `/api/governance/alerts/*` (returns `402` on the free edition, and the background dispatcher only runs with a Pro license).

Everyone signed in can see channels, rules and events (channel secrets are never returned). Creating, editing or deleting a channel or rule, and sending a test alert, is admin only; other roles get `403` and do not see those buttons.

The pipeline is: **events** (emitted by CH-UI) → **rules** (which events matter, at what severity) → **channels** (how the notification leaves the building).

## What can trigger an alert

These event types are emitted automatically:

| Event type | Severity | Emitted when |
| --- | --- | --- |
| `policy.violation` | policy severity | A [governance policy](/docs/governance#policy-engine) is violated: a `block` policy rejects a query pre-execution, or a `warn` policy detects a violation post-execution during query-log sync. Also emitted at `warn` when a pre-execution guardrail check can't be trusted (access state stale or missing) and the query is allowed through |
| `schedule.failed` | `error` | A [scheduled query](/docs/schedules) run errors |
| `schedule.slow` | `warn` | A scheduled run succeeds but takes ≥ 80% of its timeout (60 seconds when unset; minimum threshold 1 second) |
| `telemetry.monitor` | monitor severity | A [telemetry monitor](/docs/telemetry#monitors-pro) goes from OK to firing. One event per transition, not one per evaluation |
| `health.replication_lag`, `health.replica_readonly`, `health.disk`, `health.parts`, `health.mutation_stuck`, `health.node_unreachable`, `health.connection_unreachable` | `warn` or `critical` | A built-in [health check](#health-checks) fires, escalates from warn to critical, or resolves |
| `health.summary` | worst of the batch | More than 20 health check changes on one connection in one poll; the rest are folded into this one event |
| `sql.alert` | alert severity (`warn` when disabled) | A [SQL alert](#sql-alerts) fires or resolves, or turns itself off after 5 failed runs |

So yes, **schedules alert out of the box**: every failed run emits a `schedule.failed` event with the schedule name, run ID, elapsed time, and error in the payload. You only need a rule and a channel to receive it.

Severities rank `info` < `warn` < `error` < `critical`.

## Channels

A channel is one way to deliver an alert. Three email provider types, plus webhooks:

| Provider | Required config | Optional config |
| --- | --- | --- |
| **SMTP** | `host`, `from_email` (`password` required if `username` is set) | `port`, `username`, `password`, `from_name`, `use_tls`, `starttls`, `insecure_skip_verify` |
| **Resend** | `api_key`, `from_email` | `from_name` |
| **Brevo** | `api_key`, `from_email` | `from_name` |
| **Webhook** | `url` | `format`, `secret_header_name`, `secret_header_value`, `signing_secret`, `allow_http`, `allow_private_network` |

Channel configs are **encrypted at rest** with the app secret key. Secrets are never echoed back: the API returns the password, API key or webhook secrets blanked, with a `has_secret` flag so the UI knows one is stored. Updating a channel without re-entering the secret keeps the existing one.

### SMTP options

| Field | Default | Meaning |
| --- | --- | --- |
| `host` | None | SMTP server hostname (required) |
| `port` | `587` | SMTP server port |
| `username` | None | Auth username; leave empty for unauthenticated relays |
| `password` | None | Auth password (required when `username` is set) |
| `from_email` | None | Envelope and header From address (required) |
| `from_name` | None | Display name, rendered as `Name <from_email>` |
| `use_tls` | `false` | Implicit TLS: the whole connection is TLS from the first byte |
| `starttls` | `true` when `use_tls` is off | Connect in plaintext, then upgrade via STARTTLS |
| `insecure_skip_verify` | `false` | Skip TLS certificate verification (self-signed certs; avoid in production) |

Common setups:

- **Port 465 (implicit TLS)**: `port: 465`, `use_tls: true`. Gmail, most managed SMTP.
- **Port 587 (STARTTLS)**: `port: 587`, leave the TLS flags alone: `starttls` defaults to on whenever `use_tls` is off. The most common submission setup.
- **Port 25, no TLS (internal relay)**: `port: 25`, `use_tls: false`, `starttls: false`.

One caveat worth knowing: STARTTLS is *opportunistic*. If the server doesn't advertise the STARTTLS extension, the mail is sent in plaintext rather than failing. For a guaranteed-encrypted connection, use implicit TLS (`use_tls: true`).

### Webhook channels

A webhook channel POSTs one JSON body per alert to its `url`. It needs no email recipients: rules bind it without any, and the dispatcher delivers to it like any other channel.

| Field | Default | Meaning |
| --- | --- | --- |
| `url` | None | Endpoint to POST to (required). Must be `https` unless `allow_http` is on |
| `format` | `generic` | `generic` (JSON below), `slack` (a Slack incoming webhook `{"text": ...}` body) or `teams` (an Adaptive Card message for a Teams workflow webhook) |
| `secret_header_name` / `secret_header_value` | None | Optional header sent with every request, for example `Authorization: Bearer ...` or `X-Api-Key` |
| `signing_secret` | None | When set, every request carries `X-CHUI-Timestamp` (Unix seconds) and `X-CHUI-Signature: sha256=<hex>`, the HMAC-SHA256 of `<timestamp>.<raw body>` with this secret |
| `allow_http` | `false` | Accept a plain `http://` URL |
| `allow_private_network` | `false` | Allow loopback, private (RFC 1918), link-local and other non-public addresses |

The generic body:

```json
{
  "event_id": "4f0c...",
  "event_type": "schedule.failed",
  "severity": "error",
  "title": "Scheduled query failed: nightly rollup",
  "message": "Code: 241. DB::Exception: Memory limit exceeded ...",
  "fingerprint": "schedule:8d1e...:error",
  "timestamp": "2026-10-12T08:00:03Z",
  "link": "https://ch-ui.example.com/governance?section=alerts",
  "rule_name": "On-call",
  "payload": { "schedule_id": "8d1e...", "elapsed_ms": 4120 }
}
```

`link` points at the alert events page and is built from `APP_URL`.

Outbound protection: by default CH-UI refuses a webhook URL whose host is, or resolves to, a loopback, private, link-local or multicast address. The check runs on the address actually dialed, so a hostname that later resolves somewhere internal is still refused. Redirects are not followed, proxies from the environment are not used, and each request times out after 15 seconds. A non-2xx response counts as a failed attempt and is retried like any other delivery. Only admins can create or edit channels, so turning on `allow_private_network` (for an internal Mattermost, say) is an admin decision.

Slack: create an [incoming webhook](https://api.slack.com/messaging/webhooks), paste its URL, pick format `slack`. Teams: create a workflow from the "Post to a channel when a webhook request is received" template, paste its URL, pick format `teams`.

### Testing a channel

Every channel has a **Send test** action (`POST /api/governance/alerts/channels/{id}/test` with `recipients` and optional `subject` / `message`; webhooks need no `recipients` and receive an event of type `test`). The test sends immediately, bypassing the rule engine and the dispatch queue, so it verifies exactly one thing: can this config deliver. Test before you depend on it.

## Rules

Rules decide which events become email, and where it goes:

| Field | Default | Description |
| --- | --- | --- |
| Name | None | Rule identifier (required) |
| `event_type` | None | Any event type above, a prefix wildcard (`health.*` for every health check, `sql.*` for every SQL alert), or `*` (everything, `any` also accepted) |
| `severity_min` | None | Only match events at or above this severity |
| `enabled` | `true` | Toggle |
| `cooldown_seconds` | `300` | Deduplication window: the same event fingerprint won't be re-sent to the same channel within this window |
| `max_attempts` | `5` | Delivery attempts before a job is marked failed |
| `subject_template` | built-in | Custom subject, supports `{{placeholders}}` |
| `body_template` | built-in | Custom plain-text body, supports `{{placeholders}}` |
| `channels[]` | None | One or more channel bindings, each with `channel_id`, `recipients` (email addresses; empty for webhook channels), and `is_active` |
| `connection_ids` | all | Limit the rule to events of these connections. Empty or `null` means every connection. Events without a connection (most policy and some schedule events) only match rules without a connection list. Ids are checked on save; the rule sheet flags a connection that was deleted later |

Available template variables: `{{event_type}}`, `{{severity}}`, `{{title}}`, `{{message}}`, `{{payload_json}}`, `{{channel_name}}`, `{{channel_type}}`, `{{created_at}}`, `{{event_id}}`, `{{rule_name}}`. Leave the templates empty for a sensible default (`[CH-UI][ERROR][schedule.failed] …` subject, plain-text body with the event details and payload). Templates shape email only; webhooks always send the event fields.

A good starter rule: `event_type: *`, `severity_min: error`, one channel, your on-call address. Widen from there.

## Dispatch

A background dispatcher ticks every **8 seconds**:

1. **Materialize**: new events first go through [silences and acknowledgements](#silences-and-acknowledgements); the rest are matched against enabled rules (event type, severity floor and connection scope); each matching active channel binding becomes a dispatch job (up to 100 events per tick). If a rule's channels cannot be read, the event stays new and is retried on the next tick; jobs already created for it are not created twice.
2. **Deliver**: due jobs are sent (up to 30 per tick).

Reliability behavior:

- **Cooldown dedupe**: events carry a fingerprint (e.g. `schedule:{id}:error`, or policy + user + query hash). Within a rule's cooldown window, the same fingerprint is not re-dispatched to the same channel. A job failing every minute produces one email per cooldown, not sixty.
- **Retries**: failed deliveries retry with exponential backoff: `10s × 2^(attempt−1)`, capped at 30 minutes, until `max_attempts` is reached, then the job is marked failed with its last error.
- **Crash recovery**: a job is claimed with a 2 minute lease before it is sent. If CH-UI stops mid-delivery, the job is picked up again once the lease expires (counting as an attempt). An interrupted final attempt is marked failed, so it no longer holds back the same fingerprint during the cooldown.

## Silences and acknowledgements

Two ways to stop notifications for something you already know about, without touching rules:

**Silences** (admin only) hold back every event their matchers select during a time window, for planned maintenance or a known noisy source. Create them under **Governance → Silences** with a reason, a start (default now) and a duration (1 h, 4 h, 24 h or custom). A silence has up to four matchers and every one that is set must match:

| Matcher | Matches |
| --- | --- |
| Connection | Events of that connection |
| Alert type | An event type or a prefix wildcard, as in rules (`health.*`, `sql.alert`, `schedule.failed`) |
| Specific alert | One SQL alert (its id) or one health check (`health:<check>`, e.g. `health:disk`) |
| Node | Health check events of one node |

A silence needs at least one matcher. A held-back event gets the status **silenced** in the event history and creates no deliveries. The `[RESOLVED]` event of an incident whose firing was silenced is held back too, so nobody gets a resolve for a page they never saw. When a silence ends (its window runs out, or an admin uses **Expire early**), every incident it held back that is still firing has its latest held-back event delivered once. A silence only acts on events that are still new, never on ones already sent. Deleting a connection deletes its silences.

**Acknowledgements** (admin and analyst) mute the rest of one incident. Firing rows in **Health checks** and **SQL alerts** have an **Acknowledge** action with an optional comment and expiry. Later firing and de-escalation events of that incident get the status **acknowledged** and are not delivered. The ack ends when the incident resolves (the `[RESOLVED]` event is still delivered), when it escalates above the acknowledged severity (that escalation is delivered), when it expires, or when someone removes it. Alerts that never resolve, like `schedule.failed`, get a 24 hour expiry unless you set one.

Events carry `alert_key` (the alert: one check on one node, or one SQL alert) and `incident_id` (when the incident started) in their payload; acknowledgements are keyed on both. Silences and acks are stored and evaluated by the dispatcher, so without a Pro license nothing is held back or released until the license is back; their state is kept.

Audit actions: `alerts.silence.created`, `.updated`, `.expired`, `alerts.incident.acknowledged`, `alerts.incident.unacknowledged`.

## Events history

**Governance → Alerts → Events** shows the raw event stream: every alert-worthy thing that happened, whether or not a rule matched it. Each event records its type, severity, title, message, JSON payload, fingerprint, and processing status (`new`, `processed`, `silenced` or `acknowledged`, with what held it back).

`GET /api/governance/alerts/events` supports `limit` (default 100), `event_type`, and `status` filters. If an email never arrived, check here first: an event with no matching rule means a routing gap, no event at all means the trigger never fired.

## Health checks

Six checks run on every [Cluster Health](/docs/cluster-health) poll, per connection and per node, using the numbers the poll already collected. They need Cluster Health background collection to be on for the connection; there are no extra queries in steady state. A seventh, **Connection unreachable**, watches the connection itself and also runs with collection off. Open **Governance → Health checks** to see each check, its thresholds and which nodes are pending or firing.

| Check | Event type | Value per node | Default warn | Default critical |
| --- | --- | --- | --- | --- |
| Replication lag | `health.replication_lag` | `max(absolute_delay)` from `system.replicas`, seconds | 60 s | 300 s |
| Read-only replicas | `health.replica_readonly` | count of read-only replicas | none | more than 0 |
| Disk usage | `health.disk` | fullest disk in `system.disks`, percent | 80% | 90% |
| Parts pressure | `health.parts` | most active parts in one partition, as a percent of `parts_to_throw_insert` | 50% | 80% |
| Stuck mutation | `health.mutation_stuck` | age of the oldest unfinished mutation, seconds | 1800 s | 7200 s |
| Node unreachable | `health.node_unreachable` | node defined in `system.clusters` that did not answer the fan-out | none | down |
| Connection unreachable | `health.connection_unreachable` | the connection as a whole (shown as node **Connection**) | none | down |

**Connection unreachable** is checked on each poll interval of the connection. A breach is the tunnel being offline, or a poll that returned no samples followed by a failed `SELECT 1` (10 second timeout). Samples coming back, or `SELECT 1` answering, is OK. With Cluster Health collection off the harvester only runs `SELECT 1`. Without a background account or session to run it with, the state is frozen and the reason is recorded in the evaluation history. Defaults: fire after 3 breaches, resolve after 2 OKs. Offline tunnels are not counted for 2 minutes after CH-UI starts, while agents reconnect. While the connection is down the node-level checks keep their state.

A value breaches a level when it is above that threshold. Disk usage can also have a free-space floor: when the emptiest disk on a node has fewer free bytes than the floor, the check is critical whatever the percentage.

Each check on each node is a small state machine: **ok**, then **pending** on the first breach, then **firing** after `for_evaluations` consecutive breaches (default 2). It resolves after `resolve_evaluations` consecutive OK polls (default 2). A firing check that moves from warn to critical raises a new event. Resolving raises an event of the same type and severity with a title starting `[RESOLVED]` and `"status": "resolved"` in the payload.

What does not count:

- A metric that could not be collected on a node (a missing grant, a failed query) is **unknown**. It neither breaches nor recovers; the state stays where it was.
- A node is only **down** after a real cluster fan-out. When the fan-out fails and Cluster Health falls back to the local node, the other nodes are unknown and keep their state.
- While a connection's tunnel is offline the node-level checks are not evaluated, so nothing resolves falsely; only Connection unreachable counts it.

When a check starts firing, CH-UI runs one bounded drill-down query (top 5 rows of the replication, mutations, parts or disks view) and puts the top offenders in the event payload. Fingerprints are `health:<connection>:<check>:<node>:<severity>:<incident>` for firing and `...:resolved:<incident>` for the resolve, so the rule cooldown dedupes repeats without swallowing an escalation. One poll raises at most 20 events per connection; anything above that becomes one `health.summary` event.

Admins edit thresholds per connection: enabled, warn, critical, the disk free-space floor, and the for / resolve evaluation counts (1 to 20). Read-only replicas, node unreachable and connection unreachable have no warn level.

## SQL alerts

A SQL alert runs a query of yours on an interval and compares the result to a threshold. Create them under **Governance → SQL alerts** (admin or analyst role; only the creator or an admin can edit or delete one).

| Field | Default | Meaning |
| --- | --- | --- |
| SQL | None | One `SELECT` or `WITH` statement. No `SETTINGS`, `FORMAT` or `INTO OUTFILE` clause, no second statement |
| Mode | `value` | `value` compares the first column of the first row; `rows` compares the number of rows returned |
| Comparator | None | `gt`, `gte`, `lt`, `lte`, `eq` or `ne` |
| Threshold | `0` | In rows mode, between 0 and 100 |
| Severity | `warn` | Severity of the firing and resolved events |
| Interval | 300 s | 60 s to 24 h |
| Timeout | 30 s | 5 to 60 s |
| For / resolve evaluations | 1 / 1 | Consecutive breaches before firing and OKs before resolving, 1 to 20 |

Every run is wrapped as `SELECT * FROM (<your sql>) LIMIT 101` and sent with `readonly=1`, `max_execution_time` (the timeout), `max_memory_usage` 512 MiB, `max_threads` 2, `max_result_bytes` 1 MiB and `log_comment = 'ch-ui:sql-alert'`. If the ClickHouse user's profile is itself read only, ClickHouse refuses the extra settings; CH-UI then drops them and the `LIMIT` still bounds the result. Governance [guardrails](/docs/governance#policy-engine) check the query when it is saved and on every run.

Background runs use the connection's `sql_alert` [background account](/docs/background-accounts). It must be a dedicated account: SQL alerts never borrow a signed-in session, and while no account is set they simply do not run. **Test run** in the editor runs the query once with your own credentials and stores nothing; it shows the value, whether it would fire, the duration and up to 10 rows. **Run now** on a saved alert also uses your credentials and records the result like a scheduled run.

State works like the health checks: pending, firing, resolved, with `[RESOLVED]` events. A failing run is shown as **error** and does not change the incident state. After 5 consecutive failed runs the alert disables itself and raises one `sql.alert` event at `warn`. Saving an alert with a changed query or condition starts it over at OK.

## Evaluation history

Health checks and SQL alerts keep an evaluation history: a row on every state change and every error, plus one sample per hour while nothing changes. Rows older than 30 days are pruned. The history is shown in each check's and alert's sheet.

## API

| Endpoint | Purpose |
| --- | --- |
| `GET` / `POST /api/governance/alerts/channels` | List / create channels |
| `PUT` / `DELETE /api/governance/alerts/channels/{id}` | Update / delete a channel |
| `POST /api/governance/alerts/channels/{id}/test` | Send a test notification |
| `GET` / `POST /api/governance/alerts/rules` | List / create rules (rules come back with their channel bindings) |
| `PUT` / `DELETE /api/governance/alerts/rules/{id}` | Update / delete a rule |
| `GET /api/governance/alerts/events` | Event history |
| `GET` / `POST /api/governance/alerts/silences` | List / create silences (`POST` is admin only) |
| `PUT /api/governance/alerts/silences/{id}` | Update a silence that has not ended (admin) |
| `POST /api/governance/alerts/silences/{id}/expire` | End a silence now (admin) |
| `GET /api/governance/alerts/firing` | Open health check and SQL alert incidents with their acknowledgement |
| `POST /api/governance/alerts/acks` | Acknowledge the incident of an event: `{event_id, comment, expires_in}` (seconds; admin and analyst) |
| `DELETE /api/governance/alerts/acks/{id}` | Remove an acknowledgement (admin and analyst) |
| `GET` / `PUT /api/alerting/health/settings` | Health check thresholds of the session connection (`PUT` is admin only) |
| `GET /api/alerting/health/state` | Pending and firing health check nodes |
| `GET` / `POST /api/alerting/sql` | List / create SQL alerts (the list includes the `sql_alert` worker mode) |
| `PUT` / `DELETE /api/alerting/sql/{id}` | Update / delete a SQL alert (creator or admin) |
| `POST /api/alerting/sql/test` | Run a query once with your credentials, nothing stored |
| `POST /api/alerting/sql/{id}/run` | Run a saved alert now with your credentials |
| `GET /api/alerting/evaluations?source=health\|sql&id=&limit=` | Evaluation history |

The `/api/alerting/*` routes are Pro. Everyone signed in can read them; writes need the roles above.

Channel and rule changes are recorded in the [Audit Log](/docs/audit-log) (`alerts.channel.*`, `alerts.rule.*` actions), as are health threshold changes (`health_alerts.settings_updated`) and SQL alert changes and transitions (`sql_alert.created`, `.updated`, `.deleted`, `.fired`, `.resolved`, `.auto_disabled`).

Alerts are the notification layer for [Governance](/docs/governance) policies and [Schedules](/docs/schedules): those pages define *what* gets watched, this one defines *who finds out*.
