---
title: Alerts
description: "Alerting for governance and scheduled jobs: SMTP, Resend, Brevo and webhook (generic JSON, Slack, Teams) channels, rules with severity thresholds and cooldowns, delivery retries, and an event history"
---

Alerts turn governance, scheduler and telemetry events into notifications: a policy blocks a query, a scheduled job fails or runs slow, a telemetry monitor starts firing, and the right people hear about it through your own SMTP server, a transactional provider, or a webhook into Slack, Teams or your own endpoint.

Alerts are a Pro feature. Manage them under **Governance → Alerts**; the API lives at `/api/governance/alerts/*` (returns `402` on the free edition, and the background dispatcher only runs with a Pro license).

Everyone signed in can see channels, rules and events (channel secrets are never returned). Creating, editing or deleting a channel or rule, and sending a test alert, is admin only; other roles get `403` and do not see those buttons.

The pipeline is: **events** (emitted by CH-UI) → **rules** (which events matter, at what severity) → **channels** (how the notification leaves the building).

## What can trigger an alert

Four event types, emitted automatically:

| Event type | Severity | Emitted when |
| --- | --- | --- |
| `policy.violation` | policy severity | A [governance policy](/docs/governance#policy-engine) is violated: a `block` policy rejects a query pre-execution, or a `warn` policy detects a violation post-execution during query-log sync. Also emitted at `warn` when a pre-execution guardrail check can't be trusted (access state stale or missing) and the query is allowed through |
| `schedule.failed` | `error` | A [scheduled query](/docs/schedules) run errors |
| `schedule.slow` | `warn` | A scheduled run succeeds but takes ≥ 80% of its timeout (60 seconds when unset; minimum threshold 1 second) |
| `telemetry.monitor` | monitor severity | A [telemetry monitor](/docs/telemetry#monitors-pro) goes from OK to firing. One event per transition, not one per evaluation |

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
| `event_type` | None | `policy.violation`, `schedule.failed`, `schedule.slow`, `telemetry.monitor`, or `*` (wildcard, `any` also accepted) |
| `severity_min` | None | Only match events at or above this severity |
| `enabled` | `true` | Toggle |
| `cooldown_seconds` | `300` | Deduplication window: the same event fingerprint won't be re-sent to the same channel within this window |
| `max_attempts` | `5` | Delivery attempts before a job is marked failed |
| `subject_template` | built-in | Custom subject, supports `{{placeholders}}` |
| `body_template` | built-in | Custom plain-text body, supports `{{placeholders}}` |
| `channels[]` | None | One or more channel bindings, each with `channel_id`, `recipients` (email addresses; empty for webhook channels), and `is_active` |

Available template variables: `{{event_type}}`, `{{severity}}`, `{{title}}`, `{{message}}`, `{{payload_json}}`, `{{channel_name}}`, `{{channel_type}}`, `{{created_at}}`, `{{event_id}}`, `{{rule_name}}`. Leave the templates empty for a sensible default (`[CH-UI][ERROR][schedule.failed] …` subject, plain-text body with the event details and payload). Templates shape email only; webhooks always send the event fields.

A good starter rule: `event_type: *`, `severity_min: error`, one channel, your on-call address. Widen from there.

## Dispatch

A background dispatcher ticks every **8 seconds**:

1. **Materialize**: new events are matched against enabled rules (event type + severity floor); each matching active channel binding becomes a dispatch job (up to 100 events per tick). If a rule's channels cannot be read, the event stays new and is retried on the next tick; jobs already created for it are not created twice.
2. **Deliver**: due jobs are sent (up to 30 per tick).

Reliability behavior:

- **Cooldown dedupe**: events carry a fingerprint (e.g. `schedule:{id}:error`, or policy + user + query hash). Within a rule's cooldown window, the same fingerprint is not re-dispatched to the same channel. A job failing every minute produces one email per cooldown, not sixty.
- **Retries**: failed deliveries retry with exponential backoff: `10s × 2^(attempt−1)`, capped at 30 minutes, until `max_attempts` is reached, then the job is marked failed with its last error.
- **Crash recovery**: a job is claimed with a 2 minute lease before it is sent. If CH-UI stops mid-delivery, the job is picked up again once the lease expires (counting as an attempt). An interrupted final attempt is marked failed, so it no longer holds back the same fingerprint during the cooldown.

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
