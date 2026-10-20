---
title: Ingest
description: Send OpenTelemetry logs and traces, JSON events, Elasticsearch bulk requests or Splunk HEC events straight to CH-UI. It spools them on disk and writes them to ClickHouse
---

CH-UI can receive data itself. Shippers (the OpenTelemetry Collector, an OTel
SDK, Vector, Fluent Bit, Filebeat, Logstash, Splunk HEC clients, or plain
`curl`) send to CH-UI over HTTP with an ingest key, CH-UI writes each request to an on-disk spool before it answers,
and a pipeline drains the spool into a ClickHouse table. Logs and traces land
in the OpenTelemetry exporter's table layout, so [Telemetry](/docs/telemetry)
reads them with no extra setup.

Ingest is part of the open-source core: an ingest source is a
[pipeline](/docs/pipelines) source, and pipelines are free.

## How it works

The quickest way to receive logs or traces: open **Telemetry**, click **Send
data here** and then **Set up**. In one step CH-UI creates the `otel_logs` or
`otel_traces` table (in `default` unless you pick another database, with a
30-day TTL unless you change it), creates and starts an ingest pipeline that
writes to it, creates an ingest key and shows it once, and registers the
table as a Telemetry source. The sheet then shows ready-to-paste configs for
your shipper. It needs the analyst or admin role, a ClickHouse user that can
create the table, and pipelines allowed under the connection's
[background accounts](/docs/background-accounts). If an `otel_logs` table already
exists with the exporter's columns, it is reused.

To build it by hand, or for the `events` signal:

1. Create a pipeline with an **Ingest** source and a **ClickHouse** sink.
2. Pick the source's signal: `logs`, `traces` or `events`.
3. Create an ingest key for the pipeline and copy it. It is shown once.
4. Run the pipeline.
5. Point your shipper at `https://<your CH-UI>/ingest/v1/<signal>` with the key,
   or, for a shipper that speaks Elasticsearch or Splunk HEC, at
   [`/ingest/es`](#elasticsearch-bulk-api) or
   [`/ingest/services/collector`](#splunk-http-event-collector).

| Signal | Endpoint | Body | Table layout |
|---|---|---|---|
| `logs` | `POST /ingest/v1/logs` | OTLP logs, protobuf or JSON | the exporter's `otel_logs` |
| `traces` | `POST /ingest/v1/traces` | OTLP traces, protobuf or JSON | the exporter's `otel_traces`, plus `<table>_trace_id_ts` and its materialized view |
| `events` | `POST /ingest/v1/events` | a JSON object, an array of objects, or NDJSON | one JSON object per row, inserted as `JSONEachRow` |

With **Create Table If Not Exists** on the sink, the first batch creates the
table: for `logs` and `traces` the exporter's schema (with the source's
**TTL days** applied), for `events` a table inferred like any other pipeline
(see [Auto Table Creation](/docs/pipelines#auto-table-creation)). For JSON
events and logs the sink's **Wide events** layout is often the better fit:
typed `timestamp`, `service`, `level`, `message` and `trace_id` columns and
everything else in one `attributes` column (see
[Table layouts](/docs/pipelines#table-layouts)). Name logs
and traces tables so that Telemetry's **Detect sources** finds them: names
starting with `otel` or containing `logs` or `traces`, such as `otel_logs`
and `otel_traces`.

## Source settings

| Field | Description |
|---|---|
| Signal | `logs`, `traces` or `events`. A key only works on its pipeline's endpoint |
| TTL days | Used when the sink creates a logs or traces table in an OpenTelemetry layout. New sources start at 30, `0` keeps data forever. Rows already older than the TTL are dropped by ClickHouse on insert |
| Rate limit (requests/s) | Per key. `0` means 200 |
| Daily quota (MB) | Per pipeline, counted on the decompressed request bodies, resets at 00:00 UTC. `0` means unlimited |
| Stall alert (minutes) | Raise `ingest.stalled` when the pipeline received data before and then gets nothing for this long. Default 15, `0` turns it off. Webhook sources have it too |

The rate limit and quota are kept in memory, so a restart refills them.

## Alerts

Ingest problems become [alert events](/docs/alerts#ingest-and-pipeline-alerts), so a rule on `ingest.*` sends them to email, Slack, Teams or a webhook: `ingest.stalled` (no data for the stall threshold), `ingest.spool_backlog` (spooled data older than 5 minutes, or the spool above 80% of its cap), `ingest.rejecting` (more than 10% of requests rejected in 5 minutes, not counting key failures), `ingest.auth_failures` (300 or more requests with a revoked key in 5 minutes) and `pipeline.sink_failing` (ClickHouse writes keep failing). Each opens one incident, links to the pipeline, and resolves on its own. Rules, channels and delivery are part of [Alerts](/docs/alerts), which is Pro.

## Ingest keys

Keys are created per pipeline under the pipeline's **Ingest keys** panel, by
analysts and admins on the pipeline's connection. A key looks like
`chi_` followed by 40 characters. CH-UI stores only its SHA-256, shows the
full key once at creation, and afterwards only its first 8 characters after
`chi_` so you can tell keys apart. Revoking a key stops it at once. Creating
and revoking keys is recorded in the [audit log](/docs/audit-log)
(`ingest_key.created`, `ingest_key.revoked`), never with the key itself.
Deleting a pipeline deletes its keys.

Send the key in a header. Every endpoint reads all of these, so each shipper
can use the form it has a setting for:

```
Authorization: Bearer chi_...
X-CHUI-Ingest-Key: chi_...
Authorization: Splunk chi_...
Authorization: Basic base64(<any user>:chi_...)
Authorization: ApiKey base64(<any id>:chi_...)
Authorization: ApiKey chi_...
```

`Basic` is a username and password setting with any username and the key as
the password. `ApiKey` is an Elasticsearch API key setting: the key alone, or
`<anything>:chi_...` in the shippers that take an `id:api_key` pair.

Keys in the query string are ignored: URLs end up in proxy and access logs.

## Responses

| Status | When | What the shipper should do |
|---|---|---|
| `200` | The data is on disk in the spool | Nothing. OTLP endpoints answer with an empty OTLP export response; `events` answers `{"accepted": N}` |
| `400` | The body does not decode (bad OTLP, bad JSON, broken gzip) | Fix the sender; retrying sends the same bad data |
| `401` | No key, an unknown key or a revoked key | Check the key |
| `404` | The key belongs to a pipeline with another signal | Use the endpoint of the key's signal |
| `413` | Over 32 MB after decompression, or, on the OTLP endpoints, over 100,000 log records or spans | Send smaller batches |
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
run. A [Route node](/docs/pipelines#route-pro) (Pro) writes one stream to
several tables, for example `ERROR` logs to their own table and the rest to
another; each sink creates its own exporter table.

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

The **Send data here** sheet, on the Telemetry page and in the pipeline
editor, fills these snippets with your URL and the key you just created. Replace `https://ch-ui.example.com`
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

## Elasticsearch bulk API

Shippers with an Elasticsearch output (Filebeat, Logstash, Fluent Bit, Vector)
can send to CH-UI by changing the host to `https://<your CH-UI>/ingest/es`.
CH-UI answers the Elasticsearch bulk API and the calls these shippers make at
startup. It is not Elasticsearch: there is no search, no index, no mapping.
The documents go to the key's pipeline like any other ingest request.

The key's pipeline must have the `logs` or `events` signal. A `traces` key is
refused with `400`: send traces as OTLP to `/ingest/v1/traces`.

| Endpoint | What CH-UI does |
|---|---|
| `POST` or `PUT /_bulk`, `/{index}/_bulk` | Takes NDJSON action and document line pairs. `index` and `create` are stored; `update` and `delete` are refused per item (status `400`, `errors: true`), never dropped silently |
| `GET /`, `HEAD /` | Cluster info: version `8.19.0`, `build_flavor: default`, `tagline: "You Know, for Search"` |
| `GET /_license`, `GET /_xpack` | An active basic license. `_xpack` reports ILM as unavailable |
| `GET /_cluster/health` | `green` (Vector's health check) |
| `GET /_nodes`, `/_nodes/*` | No nodes. Keep sniffing off, which is every shipper's default |
| `GET`, `HEAD`, `PUT`, `POST /_index_template/*`, `/_template/*`, `/_ilm/policy/*`, `/_data_stream/*` | Answered as existing, `PUT` and `POST` are acknowledged. Nothing is stored: the table layout comes from the pipeline |

Every response carries `X-Elastic-Product: Elasticsearch`, which the 7.14+
clients check. All of these need a valid key; the startup calls work while
the pipeline is stopped, `_bulk` answers `503` until it runs.

The bulk response has the Elasticsearch shape: `took`, `errors`, and one
item per action in order, `{"create": {"_index": ..., "_id": ..., "status":
201, "result": "created"}}`, with an `_id` generated when the action has
none. A document line that is not a JSON object fails its own item
(`document_parsing_exception`); a broken action line fails the whole request
with `400`, as in Elasticsearch. The `filter_path` forms the clients send
(`errors,items.*.error,items.*.status` and the like) are applied. The
stored items of a request are fsynced to the spool before the response, the
same as every ingest endpoint; a request that fails as a whole stores
nothing.

Request errors use the Elasticsearch error body
(`{"error": {"root_cause": [...], "type": ..., "reason": ...}, "status": N}`)
with the statuses of [Responses](#responses), except that a traces key is
`400`. `429` and `503` come with `Retry-After`, and the shippers retry them.

### Filebeat

Filebeat 8 with templates and ILM setup off (CH-UI has neither):

```yaml
output.elasticsearch:
  hosts: ["ch-ui.example.com:443"]
  protocol: https
  path: /ingest/es
  username: chui
  password: chi_...
  # or: api_key: "chui:chi_..."

setup.ilm.enabled: false
setup.template.enabled: false
```

Filebeat writes to its default `filebeat-<version>` data stream with
`create` actions; the index name ends up in the `es.index` attribute.
CH-UI reports version 8.19.0. If a 9.x Filebeat or Elastic Agent refuses
the cluster as too old, set `output.elasticsearch.allow_older_versions: true`.

### Logstash

```ruby
output {
  elasticsearch {
    hosts => ["https://ch-ui.example.com:443/ingest/es"]
    user => "chui"
    password => "chi_..."
    ilm_enabled => false
    manage_template => false
    data_stream => false
    index => "logstash-%{+yyyy.MM.dd}"
  }
}
```

### Fluent Bit

```ini
[OUTPUT]
    Name               es
    Match              *
    Host               ch-ui.example.com
    Port               443
    Tls                On
    Path               /ingest/es
    HTTP_User          chui
    HTTP_Passwd        chi_...
    Suppress_Type_Name On
    Logstash_Format    On
```

Fluent Bit makes no startup calls. Without `Suppress_Type_Name` it sends a
`_type`, which CH-UI ignores.

### Vector

```toml
[sinks.chui]
type = "elasticsearch"
inputs = ["app"]
endpoints = ["https://ch-ui.example.com/ingest/es"]
api_version = "v8"
mode = "bulk"
compression = "gzip"
auth.strategy = "basic"
auth.user = "chui"
auth.password = "chi_..."
```

`api_version = "v8"` skips version detection. Vector's health check calls
`/_cluster/health`, which CH-UI answers.

## Splunk HTTP Event Collector

Clients of the Splunk HTTP Event Collector (HEC) send to
`https://<your CH-UI>/ingest/services/collector` with the key as the token,
`Authorization: Splunk chi_...`. As with the Elasticsearch API, the key's
pipeline must have the `logs` or `events` signal.

| Endpoint | Body |
|---|---|
| `POST /services/collector`, `/services/collector/event` (and `/1.0`) | Event objects, back to back or one per line: `time` (epoch seconds, fractions allowed), `host`, `source`, `sourcetype`, `index`, `event` (a string or an object, required), `fields` (an object) |
| `POST /services/collector/raw` (and `/1.0`) | Lines of text, one event per line. `host`, `source`, `sourcetype`, `index` and `time` come from the query string |
| `GET /services/collector/health` (and `/1.0`) | `{"text":"HEC is healthy","code":17}`. With a token it also checks the token (`403`, code 4) and that the pipeline runs (`503`, code 18) |

A request is stored whole or not at all: one bad event fails the request and
nothing of it is spooled. Responses use the HEC format:

| Status | Body | When |
|---|---|---|
| `200` | `{"text":"Success","code":0}` | The events are on disk in the spool |
| `401` | code `2` Token is required, code `3` Invalid authorization | No key, or an `Authorization` scheme CH-UI does not read |
| `403` | code `4` Invalid token | An unknown or revoked key |
| `400` | code `5` No data | The body has no events |
| `400` | code `6` Invalid data format, with `invalid-event-number` | Broken JSON, a value that is not an object, a bad `time`, a traces key |
| `400` | code `12` Event field is required, code `13` Event field cannot be blank, code `15` Error in handling indexed fields | The event at `invalid-event-number` (counted from 0) |
| `413` | code `6` | Over 32 MB after decompression, or over 100,000 events |
| `415` | code `6` | A `Content-Encoding` other than `gzip` |
| `503` | code `9` Server is busy, with `Retry-After` | The pipeline is stopped, the rate limit or quota is reached, or the spool is full |

Indexer acknowledgement is not offered: a `200` already means the data is
durable. Turn it off in shippers that ask for it.

### Vector

```toml
[sinks.chui]
type = "splunk_hec_logs"
inputs = ["app"]
endpoint = "https://ch-ui.example.com/ingest"
default_token = "chi_..."
encoding.codec = "json"
compression = "gzip"
acknowledgements.indexer_acknowledgements_enabled = false
```

Vector adds `/services/collector/event` to `endpoint` (or `/raw` with
`endpoint_target = "raw"`).

### OpenTelemetry Collector

```yaml
exporters:
  splunk_hec:
    endpoint: https://ch-ui.example.com/ingest/services/collector
    token: chi_...
```

For a Collector the [OTLP endpoints](#opentelemetry-collector) are the better
fit; this is for pipelines that already export HEC.

### Fluent Bit and Docker

Fluent Bit's `splunk` output and Docker's `splunk` logging driver always post
to `/services/collector/...` at the root of the host, without a path prefix.
Point them at the [separate ingest port](#a-separate-port), which serves HEC at
the root:

```ini
[OUTPUT]
    Name          splunk
    Match         *
    Host          ch-ui.example.com
    Port          4318
    TLS           On
    Splunk_Token  chi_...
```

```bash
docker run --log-driver=splunk \
  --log-opt splunk-url=https://ch-ui.example.com:4318 \
  --log-opt splunk-token=chi_... \
  --log-opt splunk-format=json \
  my-image
```

The Splunk universal forwarder speaks its own protocol, not HEC, and cannot
send here.

### curl

```bash
curl https://ch-ui.example.com/ingest/services/collector \
  -H "Authorization: Splunk chi_..." \
  -d '{"time": 1792152000.25, "host": "web-1", "sourcetype": "app", "event": "user signed in", "fields": {"env": "prod"}}{"event": {"message": "payment failed", "level": "error"}}'
```

## How documents become logs

For a `logs` key, each Elasticsearch document and each HEC event becomes one
row of the `otel_logs` layout, so Telemetry searches it like OTLP logs:

| Column | From |
|---|---|
| `Timestamp` | `@timestamp`, `timestamp` or `time`, an ISO 8601 string or an epoch number (seconds, milliseconds, microseconds or nanoseconds). HEC's own `time` wins. Otherwise the receive time |
| `Body` | `message`, `log` or `event`, the first present: a string as it is, anything else as JSON. A document with none of them has the JSON of its remaining fields as the body |
| `SeverityText`, `SeverityNumber` | `level`, `log.level` or `severity`; the number follows OTLP (`info` 9, `warn` 13, `error` 17, and so on) |
| `ServiceName` | `service.name`, `service`, `host.name` or `host` |
| `ResourceAttributes` | The `host`, `agent`, `cloud`, `kubernetes`, `container` and `service` fields, flattened with dots. A plain `host` string, and HEC's `host`, become `host.name` |
| `LogAttributes` | Every other field, flattened with dots, values as strings (arrays as JSON). Plus `es.index` for Elasticsearch, and for HEC the `fields` and `splunk.source`, `splunk.sourcetype`, `splunk.index` |

ECS fields nested as objects (`{"log": {"level": "warn"}}`) and written with
dots (`"log.level": "warn"`) are read the same way. The limits of OTLP
requests apply: 128 attributes per map, 16 KB per value, 1 MB per body.

For an `events` key the document is stored as it is, one JSON object per row.
A HEC event object is stored with `time`, `host`, `source`, `sourcetype`,
`index` and the `fields` merged in where the event has no such key; a string
event becomes `{"event": "..."}` with the same fields.

## A separate port

`--ingest-addr` (or `INGEST_ADDR`, `ingest_addr` in the config file) serves
the receivers on a listener of their own, for example `:4318`, the usual
OTLP/HTTP port. It serves only `/ingest/*` (OTLP, events, the Elasticsearch
bulk API and Splunk HEC), the same endpoints at the root (`/v1/*`, `/es`,
`/services/collector`, so a bare `https://host:4318` works as
`OTEL_EXPORTER_OTLP_ENDPOINT` and as a HEC URL), and `/health`. It uses the same TLS certificate as the main port when native TLS
is on.

The main port keeps serving `/ingest/*` too, unless
`INGEST_ONLY_SEPARATE=true` (`ingest_only_separate: true`), which keeps
shipper traffic off the port users sign in on. It only applies when
`--ingest-addr` is set.

`GET /api/ingest/info` (any signed-in user) returns the URLs shippers
should use: `base_url` is `APP_URL` plus `/ingest`, with the ingest
listener's port when `--ingest-addr` is set, and `es_url` and `hec_url` are
that URL plus `/es` and `/services/collector`.

| Setting | Env | Default |
|---|---|---|
| `ingest_spool_max_bytes` | `INGEST_SPOOL_MAX_BYTES` | 2 GiB |
| `ingest_addr` | `INGEST_ADDR` (flag `--ingest-addr`) | off |
| `ingest_only_separate` | `INGEST_ONLY_SEPARATE` | `false` |
