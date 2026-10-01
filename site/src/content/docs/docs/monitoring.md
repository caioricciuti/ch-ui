---
title: Monitoring & SIEM
description: Health checks, Prometheus metrics, and forwarding CH-UI audit events to your SIEM
---

CH-UI exposes the signals an SRE or security team needs to run it in production:
a health endpoint, Prometheus metrics, and audit-event forwarding.

## Health

```bash
curl http://localhost:3488/health
```

Returns `200` when the server is up, with this body:

```json
{ "status": "ok", "service": "ch-ui", "timestamp": "2026-09-28T08:20:31Z" }
```

The endpoint is unauthenticated and does not include the CH-UI version. To
check the version, run `ch-ui version` on the host; signed-in callers also get
it as `appVersion` in the session object returned by `/api/auth/session`,
`/api/auth/login` and `/api/auth/switch-connection`. The Docker image also ships
a `HEALTHCHECK` against this endpoint.

## Prometheus metrics

CH-UI serves Prometheus metrics at `/metrics` (no authentication; scrape it on a
trusted network or restrict it at your proxy):

```bash
curl http://localhost:3488/metrics
```

Exposed series include:

| Metric | Type | Description |
|---|---|---|
| `ch_ui_build_info{version,commit,go_version}` | gauge | Always `1`; the labels carry the CH-UI version, commit and Go version |
| `ch_ui_uptime_seconds` | gauge | Seconds since start |
| `ch_ui_http_requests_total{class}` | counter | Requests by status class (`2xx`…`5xx`) |
| `ch_ui_http_requests_in_flight` | gauge | In-flight requests |
| `ch_ui_http_request_duration_seconds_sum` / `_count` | counter | Aggregate latency |
| `go_goroutines`, `go_memstats_*`, `go_gc_runs_total` | gauge/counter | Go runtime |

A minimal scrape config:

```yaml
scrape_configs:
  - job_name: ch-ui
    static_configs:
      - targets: ["ch-ui.internal:3488"]
```

## Audit forwarding (SIEM)

> Audit forwarding is a **Pro** feature. The audit trail itself is always
> recorded in the database; forwarding to external sinks requires a Pro license.

Every audit event (logins including failures, query execution, DDL, admin and
governance changes) is stored in CH-UI's database and can also be **forwarded**
to your tooling. Forwarding is best-effort and asynchronous: it never blocks a
request, and the authoritative copy always stays in the database. Enable any
combination:

```yaml
# server.yaml
audit_forward_stdout: true                       # structured stdout logs
audit_log_file: /var/log/ch-ui/audit.jsonl       # append JSON lines (JSONL)
audit_webhook_url: https://siem.example.com/hook # POST each event as JSON
```

Equivalent environment variables: `AUDIT_FORWARD_STDOUT`, `AUDIT_LOG_FILE`,
`AUDIT_WEBHOOK_URL`.

Each forwarded event is JSON:

```json
{
  "action": "user.login",
  "username": "alice@example.com",
  "connection_id": "…",
  "details": "SSO login via OIDC (role: admin, ch_account: ch_sso_reader)",
  "ip_address": "203.0.113.7",
  "timestamp": "2026-06-12T08:20:31Z"
}
```

Events that ran as a different ClickHouse account than `username` (people
signed in with SSO share a service account) also carry `ch_user`, the
ClickHouse account used.

- **stdout**: pick this up with any log pipeline (Fluent Bit, Vector, Loki,
  CloudWatch, Datadog agent).
- **file**: tail the JSONL file with a log shipper.
- **webhook**: POST directly to Splunk HEC, Datadog, Elastic, or a custom
  collector.

## Audit export (one-off)

Admins can export the stored trail directly:

```bash
curl "http://localhost:3488/api/governance/audit-logs/export?format=csv" \
  --cookie 'chui_session=<admin session>' -o audit.csv
# format=json is also supported
```

## Logging

CH-UI logs are structured (Go `slog`). Run behind a reverse proxy if you also
want HTTP access logs, or enable `audit_forward_stdout` for a security-event
stream.
