---
title: Alerts
description: "Email alerting for governance and scheduled jobs: SMTP, Resend, and Brevo channels, rules with severity thresholds and cooldowns, delivery retries, and an event history"
---

Alerts turn governance, scheduler and telemetry events into email notifications: a policy blocks a query, a scheduled job fails or runs slow, a telemetry monitor starts firing, and the right people hear about it through your own SMTP server or a transactional provider.

Alerts are a Pro feature. Manage them under **Governance → Alerts**; the API lives at `/api/governance/alerts/*` (returns `402` on the free edition, and the background dispatcher only runs with a Pro license).

The pipeline is: **events** (emitted by CH-UI) → **rules** (which events matter, at what severity) → **channels** (how email leaves the building).

## What can trigger an alert

Four event types, emitted automatically:

| Event type | Severity | Emitted when |
| --- | --- | --- |
| `policy.violation` | policy severity | A [governance policy](/docs/governance#policy-engine) is violated: a `block` policy rejects a query pre-execution, or a `warn` policy detects a violation post-execution during query-log sync. Also emitted at `warn` when a pre-execution guardrail check can't be trusted (access state stale or missing) and the query is allowed through |
| `schedule.failed` | `error` | A [scheduled query](/docs/schedules) run errors |
| `schedule.slow` | `warn` | A scheduled run succeeds but takes ≥ 80% of its timeout (minimum threshold 5 seconds) |
| `telemetry.monitor` | monitor severity | A [telemetry monitor](/docs/telemetry#monitors-pro) goes from OK to firing. One event per transition, not one per evaluation |

So yes, **schedules alert out of the box**: every failed run emits a `schedule.failed` event with the schedule name, run ID, elapsed time, and error in the payload. You only need a rule and a channel to receive it.

Severities rank `info` < `warn` < `error` < `critical`.

## Channels

A channel is one way to deliver email. Three provider types:

| Provider | Required config | Optional config |
| --- | --- | --- |
| **SMTP** | `host`, `from_email` (`password` required if `username` is set) | `port`, `username`, `password`, `from_name`, `use_tls`, `starttls`, `insecure_skip_verify` |
| **Resend** | `api_key`, `from_email` | `from_name` |
| **Brevo** | `api_key`, `from_email` | `from_name` |

Channel configs are **encrypted at rest** with the app secret key. Secrets are never echoed back: the API returns the password / API key blanked, with a `has_secret` flag so the UI knows one is stored. Updating a channel without re-entering the secret keeps the existing one.

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

### Testing a channel

Every channel has a **Send test** action (`POST /api/governance/alerts/channels/{id}/test` with `recipients` and optional `subject` / `message`). The test sends immediately, bypassing the rule engine and the dispatch queue, so it verifies exactly one thing: can this config deliver mail. Test before you depend on it.

## Rules

Rules decide which events become email, and where it goes:

| Field | Default | Description |
| --- | --- | --- |
| Name | None | Rule identifier (required) |
| `event_type` | None | `policy.violation`, `schedule.failed`, `schedule.slow`, `telemetry.monitor`, or `*` (wildcard, `any` also accepted) |
| `severity_min` | None | Only match events at or above this severity |
| `enabled` | `true` | Toggle |
| `cooldown_seconds` | `300` | Deduplication window: the same event fingerprint won't be re-sent to the same channel within this window |
| `max_attempts` | `5` | Delivery attempts before a job is marked failed |
| `subject_template` | built-in | Custom subject, supports `{{placeholders}}` |
| `body_template` | built-in | Custom plain-text body, supports `{{placeholders}}` |
| `channels[]` | None | One or more channel bindings, each with `channel_id`, `recipients` (email addresses), and `is_active` |

Available template variables: `{{event_type}}`, `{{severity}}`, `{{title}}`, `{{message}}`, `{{payload_json}}`, `{{channel_name}}`, `{{channel_type}}`, `{{created_at}}`, `{{event_id}}`, `{{rule_name}}`. Leave the templates empty for a sensible default (`[CH-UI][ERROR][schedule.failed] …` subject, plain-text body with the event details and payload).

A good starter rule: `event_type: *`, `severity_min: error`, one channel, your on-call address. Widen from there.

## Dispatch

A background dispatcher ticks every **8 seconds**:

1. **Materialize**: new events are matched against enabled rules (event type + severity floor); each matching active channel binding becomes a dispatch job (up to 100 events per tick).
2. **Deliver**: due jobs are sent (up to 30 per tick).

Reliability behavior:

- **Cooldown dedupe**: events carry a fingerprint (e.g. `schedule:{id}:error`, or policy + user + query hash). Within a rule's cooldown window, the same fingerprint is not re-dispatched to the same channel. A job failing every minute produces one email per cooldown, not sixty.
- **Retries**: failed deliveries retry with exponential backoff: `10s × 2^(attempt−1)`, capped at 30 minutes, until `max_attempts` is reached, then the job is marked failed with its last error.

## Events history

**Governance → Alerts → Events** shows the raw event stream: every alert-worthy thing that happened, whether or not a rule matched it. Each event records its type, severity, title, message, JSON payload, fingerprint, and processing status.

`GET /api/governance/alerts/events` supports `limit` (default 100), `event_type`, and `status` filters. If an email never arrived, check here first: an event with no matching rule means a routing gap, no event at all means the trigger never fired.

## API

| Endpoint | Purpose |
| --- | --- |
| `GET` / `POST /api/governance/alerts/channels` | List / create channels |
| `PUT` / `DELETE /api/governance/alerts/channels/{id}` | Update / delete a channel |
| `POST /api/governance/alerts/channels/{id}/test` | Send a test notification |
| `GET` / `POST /api/governance/alerts/rules` | List / create rules (rules come back with their channel bindings) |
| `PUT` / `DELETE /api/governance/alerts/rules/{id}` | Update / delete a rule |
| `GET /api/governance/alerts/events` | Event history |

Channel and rule changes are recorded in the [Audit Log](/docs/audit-log) (`alerts.channel.*`, `alerts.rule.*` actions).

Alerts are the notification layer for [Governance](/docs/governance) policies and [Schedules](/docs/schedules): those pages define *what* gets watched, this one defines *who finds out*.
