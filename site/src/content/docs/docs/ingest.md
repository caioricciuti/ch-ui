---
title: Ingest
description: Send OpenTelemetry logs and traces, or JSON events, straight to CH-UI. It spools them on disk and writes them to ClickHouse
---

CH-UI can receive data itself. Shippers (the OpenTelemetry Collector, an OTel
SDK, Vector, Fluent Bit, or plain `curl`) send to CH-UI over HTTP with an
ingest key, CH-UI writes each request to an on-disk spool before it answers,
and a pipeline drains the spool into a ClickHouse table. Logs and traces land
in the OpenTelemetry exporter's table layout, so [Telemetry](/docs/telemetry)
reads them with no extra setup.

Ingest is part of the open-source core: an ingest source is a
[pipeline](/docs/pipelines) source, and pipelines are free.

## How it works

1. Create a pipeline with an **Ingest** source and a **ClickHouse** sink.
2. Pick the source's signal: `logs`, `traces` or `events`.
3. Create an ingest key for the pipeline and copy it. It is shown once.
4. Run the pipeline.
5. Point your shipper at `https://<your CH-UI>/ingest/v1/<signal>` with the key.

| Signal | Endpoint | Body | Table layout |
|---|---|---|---|
| `logs` | `POST /ingest/v1/logs` | OTLP logs, protobuf or JSON | the exporter's `otel_logs` |
| `traces` | `POST /ingest/v1/traces` | OTLP traces, protobuf or JSON | the exporter's `otel_traces`, plus `<table>_trace_id_ts` and its materialized view |
| `events` | `POST /ingest/v1/events` | a JSON object, an array of objects, or NDJSON | one JSON object per row, inserted as `JSONEachRow` |

With **Create Table If Not Exists** on the sink, the first batch creates the
table: for `logs` and `traces` the exporter's schema (with the source's
**TTL days** applied), for `events` a table inferred like any other pipeline
(see [Auto Table Creation](/docs/pipelines#auto-table-creation)). Name logs
and traces tables so that Telemetry's **Detect sources** finds them: names
starting with `otel` or containing `logs` or `traces`, such as `otel_logs`
and `otel_traces`.

## Source settings

| Field | Description |
|---|---|
| Signal | `logs`, `traces` or `events`. A key only works on its pipeline's endpoint |
| TTL days | Used when the sink creates a logs or traces table. `0` keeps data forever. Rows already older than the TTL are dropped by ClickHouse on insert |
| Rate limit (requests/s) | Per key. `0` means 200 |
| Daily quota (MB) | Per pipeline, counted on the decompressed request bodies, resets at 00:00 UTC. `0` means unlimited |

The rate limit and quota are kept in memory, so a restart refills them.

## Ingest keys

Keys are created per pipeline under the pipeline's **Ingest keys** panel, by
analysts and admins on the pipeline's connection. A key looks like
`chi_` followed by 40 characters. CH-UI stores only its SHA-256, shows the
full key once at creation, and afterwards only its first 8 characters after
`chi_` so you can tell keys apart. Revoking a key stops it at once. Creating
and revoking keys is recorded in the [audit log](/docs/audit-log)
(`ingest_key.created`, `ingest_key.revoked`), never with the key itself.
Deleting a pipeline deletes its keys.

Send the key in a header, either one works:

```
Authorization: Bearer chi_...
X-CHUI-Ingest-Key: chi_...
```

Keys in the query string are ignored: URLs end up in proxy and access logs.

## Responses

| Status | When | What the shipper should do |
|---|---|---|
| `200` | The data is on disk in the spool | Nothing. OTLP endpoints answer with an empty OTLP export response; `events` answers `{"accepted": N}` |
| `400` | The body does not decode (bad OTLP, bad JSON, broken gzip) | Fix the sender; retrying sends the same bad data |
| `401` | No key, an unknown key or a revoked key | Check the key |
| `404` | The key belongs to a pipeline with another signal | Use the endpoint of the key's signal |
| `413` | Over 32 MB after decompression, or over 100,000 records | Send smaller batches |
| `415` | A content type or `Content-Encoding` CH-UI does not read | OTLP needs `application/x-protobuf` or `application/json`; events need `application/json`, `application/x-ndjson` or `text/plain` (NDJSON); only `gzip` compression |
| `429` | Over the key's rate limit (`Retry-After: 1`) or the pipeline's daily quota (`Retry-After` until midnight UTC) | Retry later |
| `503` | The pipeline is stopped, or the spool is full (`Retry-After`) | Retry later; nothing was stored |

OTLP errors come as a `google.rpc.Status` in the request's encoding, which is
what OTLP exporters expect. Error bodies never say which pipelines exist.

To parse log bodies, set severity, drop noise, sample or redact before the
rows are written, add [transform nodes](/docs/pipelines#transforms) between
the source and the sink. On a `logs` pipeline, Parse writes into
`LogAttributes` by default and Map fields' **severity from** sets
`SeverityText` and `SeverityNumber`. Transforms run when the spool is written
to ClickHouse, so data that waited in the spool gets the chain of the current
run.

## Durability

A `200` means the request is fsynced to the spool in
`<data dir>/ingest-spool` (next to `ch-ui.db`). The pipeline writes the spool
to ClickHouse one segment at a time and deletes a segment only after
ClickHouse accepted it. If ClickHouse is down the data waits in the spool and
the pipeline retries with backoff; if CH-UI stops or crashes, the next start
writes what is left.

Each segment is inserted with an `insert_deduplication_token` that stays the
same across retries and restarts, so a segment written just before a crash and
written again after it is stored once. Tables the sink creates for `logs` and
`traces` have `non_replicated_deduplication_window = 1000` for that.
`Replicated*MergeTree` tables and ClickHouse Cloud deduplicate by default. For
an `events` table you create yourself on plain `MergeTree`, set the window as
described in [Delivery and duplicates](/docs/pipelines#delivery-and-duplicates).

Rows ClickHouse refuses because of their data go to the sink's
[dead-letter table](/docs/pipelines#dead-letter-table), as for every pipeline.

Stopping a pipeline keeps its spooled data (the endpoints answer `503` while
it is stopped) and the next run writes it. Deleting the pipeline deletes its
spooled data.

The spool is capped at 2 GiB by default across all pipelines (webhook
pipelines use it too). Set `INGEST_SPOOL_MAX_BYTES` (or
`ingest_spool_max_bytes` in the config file) to change it. When it is full the
endpoints answer `503` until the pipelines catch up.

## Live counters

The pipeline editor shows, while the panel is open, the requests, records and
bytes received, rejections by reason (auth, rate limit, quota, too large,
malformed, spool full), rows and batches written, what is waiting in the spool
and its age, and the rate over the last minute. The counters start at zero
when CH-UI starts. The same data is at
`GET /api/pipelines/{id}/ingest-stats`.

## Send data here

The **Send data here** panel in the pipeline editor fills these snippets with
your URL and the key you just created. Replace `https://ch-ui.example.com`
and `chi_...` below.

### OpenTelemetry Collector

The `otlphttp` exporter adds `/v1/logs` and `/v1/traces` to its endpoint. One
key serves one signal, so logs and traces use two exporters with their own
keys:

```yaml
exporters:
  otlphttp/chui-logs:
    endpoint: https://ch-ui.example.com/ingest
    headers:
      Authorization: "Bearer ${env:CHUI_LOGS_KEY}"
  otlphttp/chui-traces:
    endpoint: https://ch-ui.example.com/ingest
    headers:
      Authorization: "Bearer ${env:CHUI_TRACES_KEY}"

service:
  pipelines:
    logs:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/chui-logs]
    traces:
      receivers: [otlp]
      processors: [batch]
      exporters: [otlphttp/chui-traces]
```

`compression: gzip` is the exporter's default and CH-UI reads it.

### OpenTelemetry SDKs

Use the signal-specific variables: their URLs are used as they are.

```bash
export OTEL_EXPORTER_OTLP_PROTOCOL=http/protobuf
export OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=https://ch-ui.example.com/ingest/v1/logs
export OTEL_EXPORTER_OTLP_LOGS_HEADERS="Authorization=Bearer%20chi_..."
export OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=https://ch-ui.example.com/ingest/v1/traces
export OTEL_EXPORTER_OTLP_TRACES_HEADERS="Authorization=Bearer%20chi_..."
```

The space after `Bearer` is written `%20`: the headers variable is URL-encoded.

### Vector

Events with the `http` sink:

```toml
[sinks.chui]
type = "http"
inputs = ["app"]
uri = "https://ch-ui.example.com/ingest/v1/events"
method = "post"
compression = "gzip"
encoding.codec = "json"
framing.method = "newline_delimited"
request.headers.Authorization = "Bearer chi_..."
```

For logs and traces, send OTLP with Vector's `opentelemetry` sink to
`/ingest/v1/logs` or `/ingest/v1/traces` with the same header.

### Fluent Bit

Logs over OTLP:

```ini
[OUTPUT]
    Name        opentelemetry
    Match       *
    Host        ch-ui.example.com
    Port        443
    Tls         On
    Logs_uri    /ingest/v1/logs
    Header      Authorization Bearer chi_...
```

Events with the `http` output:

```ini
[OUTPUT]
    Name        http
    Match       *
    Host        ch-ui.example.com
    Port        443
    Tls         On
    URI         /ingest/v1/events
    Format      json_lines
    Header      Authorization Bearer chi_...
```

### curl

```bash
curl -X POST https://ch-ui.example.com/ingest/v1/events \
  -H "Authorization: Bearer chi_..." \
  -H "Content-Type: application/x-ndjson" \
  --data-binary $'{"event":"signup","plan":"pro"}\n{"event":"login","user":42}\n'
```

## A separate port

`--ingest-addr` (or `INGEST_ADDR`, `ingest_addr` in the config file) serves
the receivers on a listener of their own, for example `:4318`, the usual
OTLP/HTTP port. It serves only `/ingest/v1/*`, the same endpoints at `/v1/*`
(so a bare `https://host:4318` works as `OTEL_EXPORTER_OTLP_ENDPOINT`), and
`/health`. It uses the same TLS certificate as the main port when native TLS
is on.

The main port keeps serving `/ingest/*` too, unless
`INGEST_ONLY_SEPARATE=true` (`ingest_only_separate: true`), which keeps
shipper traffic off the port users sign in on.

`GET /api/ingest/info` (any signed-in user) returns the base URL shippers
should use: `APP_URL` plus `/ingest`, with the ingest listener's port when
`--ingest-addr` is set.

| Setting | Env | Default |
|---|---|---|
| `ingest_spool_max_bytes` | `INGEST_SPOOL_MAX_BYTES` | 2 GiB |
| `ingest_addr` | `INGEST_ADDR` (flag `--ingest-addr`) | off |
| `ingest_only_separate` | `INGEST_ONLY_SEPARATE` | `false` |
