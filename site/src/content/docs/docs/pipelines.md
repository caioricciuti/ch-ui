---
title: Pipelines
description: Visual data pipeline builder for ingesting data into ClickHouse
---

Pipelines is a visual data pipeline builder that lets you create ingestion flows from multiple sources into ClickHouse tables using a drag-and-drop graph editor.

No external dependencies. Connectors compile into the CH-UI binary.

## Overview

Each pipeline has:

- **One source node** (where data comes from)
- **Zero or more transform nodes** that parse, reshape, filter, sample or redact records on the way (see [Transforms](#transforms))
- **One sink node** (ClickHouse table), or up to 8 behind a [Route](#route-pro) node (Pro)
- **Edges** connecting them in a single chain: source, transforms in order, sink. A Route node splits the chain into one branch per sink

Pipelines run as background goroutines inside the CH-UI server process. A pipeline that is running when CH-UI stops (a crash or a normal shutdown) keeps its `running` status and is started again on the next boot. Only **Stop** in the UI sets it to `stopped`.

## Source Connectors

### Webhook

Receive data via HTTP POST. When the pipeline starts, CH-UI exposes an endpoint:

```
POST /api/pipelines/webhook/{pipeline-id}
```

Configuration:

| Field | Description |
|---|---|
| Require Authentication | Toggle to enable Bearer token auth. On by default for new webhook nodes; existing pipelines keep their setting |
| Batch Size, Batch Timeout (ms) | Kept for older configs and still validated (minimum 1 and 100), but no longer used: the spool decides the batches (see below) |
| Stall Alert (minutes) | Raise an [`ingest.stalled`](/docs/alerts#ingest-and-pipeline-alerts) alert when the pipeline received data before and then gets nothing for this long. Default 15, `0` turns it off |

When authentication is enabled, a Bearer token is generated in the browser. Copy it before you save: the token is stored encrypted and is not shown again. **Generate new token** replaces it, after which senders need the new value. Include it in requests:

```bash
curl -X POST http://your-ch-ui:3488/api/pipelines/webhook/{pipeline-id} \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer {token}" \
  -d '[{"event":"click","user_id":42,"ts":"2025-01-15T10:30:00Z"}]'
```

Send the token in the `Authorization` header. A `?token={token}` query parameter is still accepted so existing senders keep working, but avoid it: query strings end up in reverse proxy and access logs, which leaks the token to anyone who can read them.

Accepted formats: a JSON object (pretty-printed is fine), a JSON array of objects, or newline-delimited JSON (NDJSON, one object per line). The whole body is tried as JSON first, then as NDJSON. A body is at most 10 MB (`413` above).

A request is accepted whole or not at all, and a `200` means it is on disk: the webhook writes every request to the [ingest spool](/docs/ingest#durability) before it answers, and the pipeline writes the spool to ClickHouse. A ClickHouse outage or a CH-UI restart loses nothing that was acknowledged; the records wait in the spool and are written when the pipeline runs again, with a deduplication token that stays the same across retries. When the spool is full, or CH-UI is shutting down, the webhook answers `503 Service Unavailable` with `Retry-After`; retry the same request.

For OpenTelemetry data, or to authenticate senders with revocable keys instead of one token per pipeline, use the [Ingest](#ingest) source.

### Ingest

Receive OpenTelemetry logs or traces (OTLP over HTTP, protobuf or JSON), JSON events, Elasticsearch bulk requests or Splunk HEC events from shippers such as the OpenTelemetry Collector, OTel SDKs, Vector, Fluent Bit, Filebeat and Logstash. Shippers authenticate with per-pipeline ingest keys and send to:

```
POST /ingest/v1/logs
POST /ingest/v1/traces
POST /ingest/v1/events
POST /ingest/es/_bulk                (logs and events keys)
POST /ingest/services/collector      (logs and events keys)
```

| Field | Description |
|---|---|
| Signal | `logs`, `traces` or `events` |
| TTL days | Applied when the sink creates a logs or traces table in an OpenTelemetry layout. New sources start at 30, `0` keeps data forever |
| Rate limit (requests/s) | Per key (`0` means 200) |
| Daily quota (MB) | Per pipeline, decompressed bytes per UTC day (`0` means unlimited) |
| Stall Alert (minutes) | Raise an [`ingest.stalled`](/docs/alerts#ingest-and-pipeline-alerts) alert when the pipeline received data before and then gets nothing for this long. Default 15, `0` turns it off |

With **Create Table If Not Exists** on the sink, logs and traces tables are created in the OpenTelemetry exporter's layout, so [Telemetry](/docs/telemetry) reads them as they are; pick another [table layout](#table-layouts) on the sink to change that. Requests are spooled on disk before they are answered, like the webhook. Keys, endpoints, responses, limits and shipper configs are on the [Ingest](/docs/ingest) page.

### Kafka

Consume messages from a Kafka topic using consumer groups.

| Field | Description |
|---|---|
| Brokers | Comma-separated broker addresses (e.g. `broker1:9092,broker2:9092`) |
| Topic | Kafka topic to consume |
| Consumer Group | Consumer group ID. New Kafka nodes get `ch-ui-<pipeline id>`; a config saved without one uses `ch-ui-pipeline` |
| SASL Mechanism | `PLAIN`, `SCRAM-SHA-256`, or `SCRAM-SHA-512` (optional) |
| SASL Username | Username for SASL auth |
| SASL Password | Password for SASL auth |
| Enable TLS | Toggle for TLS connections |
| Batch Size | Records per batch (default: 1000, minimum 1) |
| Batch Timeout (ms) | Max wait before flush (default: 5000, minimum 100) |

Offsets are committed only after a batch is written to ClickHouse. A restart resumes from the consumer group's committed offsets; CH-UI also records the next offset of each partition after every written batch, shown under **Runs > Saved position** for reference. Messages that are not a JSON object are inserted as `{"_raw": "...", "_topic", "_partition", "_offset", "_timestamp"}`, so the target table needs a `_raw` column to keep them. When a broker is unreachable the source reconnects with backoff (1 s doubling up to 60 s) instead of stopping.

### Database

Poll rows from PostgreSQL, MySQL, or SQLite with optional watermark-based incremental reads.

| Field | Description |
|---|---|
| Database Type | `PostgreSQL` (default), `MySQL`, or `SQLite` |
| Connection String | DSN (e.g. `postgres://user:pass@host/db` or `/path/to/file.db` for SQLite). Stored encrypted |
| SQL Query | Query to execute each poll. With a watermark column, `$1` (PostgreSQL) or `?` (MySQL, SQLite) is the last written watermark |
| Poll Interval (seconds) | Seconds between polls (default: 60, minimum 1) |
| Watermark Column | Column for incremental polling (e.g. `id` or `created_at`) |
| Batch Size | Max rows per batch (default: 1000, minimum 1) |

Watermark example query:

```sql
SELECT * FROM events WHERE id > COALESCE($1, 0) ORDER BY id
```

The pipeline passes the watermark of the last **written** batch as `$1` on each poll, and the next poll waits until the rows of the previous one are written. Before the first batch is written the parameter is `NULL`, so wrap it in `COALESCE` with a starting value (for a timestamp, for example `COALESCE($1, '1970-01-01'::timestamptz)`). Order the query by the watermark column.

The watermark is saved after every written batch (see [Positions and reset](#positions-and-reset)), so a restart continues after the last written row instead of reading from the start. Without a watermark column every poll reads the whole query result again and nothing is saved.

**SQLite is admin-only.** A SQLite source reads a file on the CH-UI server, so only admins can save or start a pipeline that uses one, and paths inside CH-UI's own data directory (its database, keys and backups) are refused.

### S3

Read files from S3-compatible storage (AWS S3, MinIO, etc.).

| Field | Description |
|---|---|
| Endpoint | Host and port, optionally with a scheme, e.g. `minio.example.com:9000` or `http://minio:9000`. No path. `http://` turns TLS off, `https://` turns it on. Empty means AWS S3 |
| Region | AWS region (default: `us-east-1`) |
| Bucket | Bucket name |
| Key Prefix | Filter files by prefix (e.g. `data/events/`) |
| Access Key ID | S3 credentials. Stored encrypted |
| Secret Access Key | S3 credentials. Stored encrypted |
| File Format | `JSON`, `JSON Lines (NDJSON)`, or `CSV` |
| Poll Interval (seconds) | Seconds between scans (default: 300, minimum 1) |
| Batch Size | Records per batch (default: 1000, minimum 1) |

`JSON` accepts either a JSON array of objects or NDJSON (one object per line), detected from the first character of the file. `NDJSON` reads one object per line. CSV files need a header row and follow RFC 4180 quoting (quoted fields may contain commas, quotes and line breaks); every value is inserted as a string.

A file counts as processed only after all its rows are written. The processed files (key and ETag) are saved per bucket and prefix, so a restart reads only new files and files whose ETag changed (rewritten objects). The saved list keeps the 2000 lexically largest keys; older keys are folded into a "processed up to key K" marker, and any key that sorts at or below K counts as processed. Prefixes whose keys grow over time (dates, sequence numbers) are not affected; a new file whose key sorts below K is skipped.

## Transforms

Transform nodes sit between the source and the sink and run, in chain order, on every batch before it is written. They work with every source. Drag one from the **Transforms** section of the palette, or drop it onto an existing edge to insert it there.

| Node | What it does | Edition |
|---|---|---|
| Parse | Parses a text field (JSON, logfmt, regex, nginx or Apache access logs, syslog) into fields | Free |
| Map fields | Copies, renames, sets and drops fields; sets severity and timestamp from a field | Free |
| Filter | Keeps or drops records that match rules | Free |
| Sample | Keeps a deterministic share of records | Pro |
| Redact | Replaces emails, IP addresses, card numbers, tokens and custom patterns | Pro |
| Route | Sends records to different sinks by rules | Pro |

Without a [Route](#route-pro) node the graph must be one straight line: one source, one sink, and every transform with exactly one connection in and one out. With a Route node the line ends at the route, and each of its outputs leads through its own transforms to its own sink (see [Route](#route-pro)). Branches anywhere else, merges, cycles and unconnected nodes are refused when the pipeline starts, with the node's label in the message. A pipeline can have up to 20 transform nodes in total.

Transform configs are checked when the graph is saved (paths, regular expressions, formats, operators, limits), and a node with a bad config is named in the error. A running pipeline keeps the chain it started with; stop and run it again to apply changes.

### Records and field paths

A record is the JSON object of one row. Paths name fields in it:

- `status` is a top-level field, `http.request.method` a nested object path.
- `attrs['user.id']` (or `attrs["user.id"]`) is a key that contains dots.
- On an ingest pipeline with the `logs` or `traces` signal, the map columns `LogAttributes`, `ResourceAttributes`, `ScopeAttributes` and `SpanAttributes` hold flat keys, so `LogAttributes.http.method` is the key `http.method`. `LogAttributes['k']` works too. Values written into these columns are turned into strings: numbers in their shortest form, booleans as `true`/`false`, objects and arrays as JSON.

Numbers keep every digit through a chain (a 20-digit id stays exact). Transforms for webhook and ingest sources run when the spool is written to ClickHouse, so data that waited in the spool is processed with the chain of the current run.

### Parse

| Field | Description |
|---|---|
| Field | The text field to parse. On a logs pipeline it defaults to `Body` |
| Format | `json`, `logfmt`, `regex`, `nginx`, `apache` or `syslog` |
| Pattern | For `regex`: a Go (RE2) regular expression with named groups, e.g. `(?P<ip>\S+) (?P<status>\d{3})`. Each named group becomes a field |
| Target | Where the parsed fields go: an object path, or empty for the top level of the record. On a logs pipeline a node saved without a target writes to `LogAttributes` |
| Prefix | Added to every parsed field name |
| On error | `keep` (default) passes the record on unchanged, `drop` drops it. Either way it counts as an error |
| Remove source | Delete the parsed field after a successful parse |

- `logfmt` reads `key=value` pairs separated by spaces; values may be double-quoted with escapes, a bare key is `"true"`. A line without any `key=value` pair is a parse error.
- `nginx` and `apache` read the combined log format (and the common format without referer and user agent) into `client_ip`, `ident`, `remote_user`, `time_local`, `request`, `method`, `path`, `protocol`, `status`, `body_bytes_sent`, `http_referer` and `http_user_agent`. `-` values are left out.
- `syslog` reads RFC 5424 and RFC 3164 messages into `priority`, `facility`, `severity` (keyword: `emerg`, `alert`, `crit`, `err`, `warning`, `notice`, `info`, `debug`), `timestamp`, `hostname`, `app_name`, `proc_id`, `msg_id`, `structured_data` (raw) and `message`.
- Parsed values are strings, except with `json`, which keeps types. Fields longer than 1 MB fail to parse.

### Map fields

Runs, in this order: **copy**, **rename**, **set**, **severity from**, **timestamp from**, **drop**. A missing source field is skipped.

- **Severity from** maps a level word (case-insensitive) to OpenTelemetry severity: `trace` 1, `debug` 5, `info`/`notice` 9, `warn`/`warning` 13, `error`/`err` 17, `fatal`/`critical`/`crit`/`alert`/`emerg`/`panic` 21. On a logs pipeline it sets `SeverityText` (`TRACE`, `DEBUG`, `INFO`, `WARN`, `ERROR`, `FATAL`) and `SeverityNumber`; on other pipelines it sets a `severity` field with the text. Unknown words leave the record as it is.
- **Timestamp from** reads a field as `rfc3339`, `unix` (seconds, decimals allowed), `unix_ms`, `unix_ns`, or `layout:<Go layout>` (for example `layout:02/Jan/2006:15:04:05 -0700`). On a logs or traces pipeline it sets `Timestamp`; on other pipelines a `timestamp` field in RFC 3339 (UTC). A layout without a time zone is read as UTC; a layout without a year gives year 0, so include one. A value that does not parse counts as an error and leaves the record as it is.

### Filter

| Field | Description |
|---|---|
| Mode | `keep`: only matching records pass. `drop`: matching records are dropped |
| Match | `all` rules or `any` rule |
| Rules | Field, operator, value |

Operators: `eq`, `ne`, `contains`, `not_contains`, `matches` (RE2), `gt`, `gte`, `lt`, `lte`, `exists`, `missing`, `in` (value is a JSON array such as `["ERROR","FATAL"]`). `eq`, `ne`, `in` and the comparisons compare as numbers when both sides are numbers, else as text. A missing field matches only `ne`, `not_contains` and `missing`. A filter without rules lets everything through. Up to 50 rules.

### Sample (Pro)

| Field | Description |
|---|---|
| Keep percent | 0 to 100, in steps of 0.01 |
| Always keep | Optional rules (same shape as Filter); matching records always pass, for example errors |
| Key | Optional field to sample on, such as `TraceId`, so all records of a trace are kept or dropped together |

Sampling is deterministic, never random: a record passes when the FNV-1a 64 hash of its key value (or of the whole record as canonical JSON, when there is no key or it is empty) modulo 10000 is below keep percent times 100. The same record always gets the same answer, so a retried or replayed batch gives the same rows.

### Redact (Pro)

| Field | Description |
|---|---|
| Kinds | `email`, `ipv4`, `ipv6`, `credit_card`, `jwt`, `bearer_token`, `aws_key`, `custom` |
| Custom | Named RE2 patterns (up to 20), e.g. name `order id`, pattern `ORD-\d+` |
| Fields | Paths to redact. Empty means every string in the record, including map values and arrays |
| Replacement | Default `[REDACTED:{kind}]`; `{kind}` is replaced with the kind or the custom pattern's name |

`credit_card` only replaces 13 to 19 digit numbers (spaces and dashes allowed) that pass the Luhn check, so order numbers and timestamps are left alone. `bearer_token` keeps the word `Bearer` and replaces the token. Redaction runs in the order tokens, AWS keys, emails, card numbers, IPv6, IPv4, custom. Original values are never logged or shown in errors.

### Route (Pro)

A Route node sends each record to one or more sinks, so one source can fill several tables: errors to one table and everything else to another, or one table per team.

| Field | Description |
|---|---|
| Mode | `first`: a record goes to the first route that matches. `all`: to every route that matches, so a record can be written to several tables |
| Routes | Up to 16, in order. Each has an id (`a-z`, `0-9`, `_`, `-`, up to 32 characters, unique in the node), a name, and rules with `all` or `any` match, the same rules as [Filter](#filter). A route without rules matches every record |
| Unmatched | `drop` (default): records that match no route are dropped and counted. `output`: they get their own output, which can lead to a sink too |

Each route is an output of the node, labelled with its name. Connect every route to a sink, directly or through transforms that only that branch runs. The `unmatched` output may stay unconnected; its records are then dropped and counted. The shape is checked when the pipeline starts:

- at most one Route node, and no Route node inside a branch;
- at most 8 sinks per pipeline, each fed by exactly one route output;
- every route connected; a route left unconnected saves, but the pipeline does not start and the message names the route.

A graph problem found at start (a shape problem like these, or a node config that does not validate) is refused with `400 Bad Request`, a message that starts with the node's label, and `node_id` naming the node, so the editor marks it.

The chain before the route runs once per batch, the route splits its output, and each branch runs on its share. Routing is deterministic, so a retried or replayed batch splits the same way. Each sink creates its own table (with the OpenTelemetry layout on an ingest logs or traces pipeline) and has its own [dead-letter table](#dead-letter-table). A sink that gets no rows from a batch does not insert. How a routed batch is retried is under [Delivery and duplicates](#delivery-and-duplicates).

### Preview

Each transform node has a **Preview** tab that shows the records before and after the chain up to that node: changed fields highlighted, dropped records struck out with the reason, parse errors per record. It uses the node configs in the editor, saved or not. The records come from, in order: records you paste (JSON objects, one per line, up to 100), the last 20 records that entered the chain of the running pipeline, or the source's [test sample](#test-a-pipeline). The API is `POST /api/pipelines/{id}/preview-transforms` (analysts and admins) with `{"nodes":[{"id","node_type","config"}, ...], "records":[...]}`, at most 1 MB. Each step in the response has the node's `output`, `kept` (each output record's index in the step's input), `drops` (`index` and `reason`) and `errors` (`index` and `message`).

`nodes` may contain a Route node. Its step also has `routes` (`[{"handle","kept"}]`, the input indexes each route got, plus `unmatched` when unmatched records have an output) and `unmatched` (the indexes that matched no route). To preview a node in a branch, send the nodes before the route, the route, and that branch's nodes up to the node, with `"branch":"<route id>"` (or `"unmatched"`): the route step then passes only that branch's records on. The Route node's own Preview tab shows, per record, the route or routes it goes to and the unmatched ones.

### Counters

While a pipeline runs, each transform node shows how many records came in, went out, were dropped, and failed to parse, since the run started. They are in `GET /api/pipelines/{id}/status` as `transforms: [{"node_id","in","out","dropped","errors"}]`, the shared chain first, then the route and each branch. A Route node's entry also has `outputs: [{"handle","out"}]`, records sent per route, and `unmatched_dropped`; its `out` is the sum of the outputs (in `all` mode a record sent to two routes counts twice) and `dropped` equals `unmatched_dropped`.

Every sink has its own totals in `sinks: [{"node_id","rows","batches","last_write_at","errors"}]`. The pipeline totals (`rows_ingested`, `batches_sent`, `errors_count`) are the sums over the sinks.

### Pro nodes

Sample, Redact and Route need CH-UI Pro to run or preview. A graph with them saves on any edition, so a license change never deletes a config, but starting or previewing them without Pro fails with `402 Payment Required` and the message `Sampling needs CH-UI Pro` (or `Redaction needs CH-UI Pro`, `Routing needs CH-UI Pro`), with `node_id` naming the node; the run log names the node by its label, and a running pipeline that loses its license (past the grace period) stops with that error at the next batch (the license is checked again once a minute). The read-only grace period after a license expires keeps them running.

### Duplicates and replays

Retries and replays send the same rows with the same deduplication token because every transform is deterministic: nothing reads the clock or a random source. If you change the chain while a spooled segment was inserted but not yet acknowledged (a crash in between), the replay under the new chain carries the old token and ClickHouse keeps the first version.

## ClickHouse Sink

The sink writes batches to a ClickHouse table using `INSERT FORMAT JSONEachRow` through the tunnel gateway. It runs with the connection's `pipeline` [background account](/docs/background-accounts), fetched again for every batch, so the account needs INSERT on the target (and CREATE TABLE if the sink creates it).

| Field | Description |
|---|---|
| Target Database | ClickHouse database name (default: `default`) |
| Target Table | Table name |
| Create Table If Not Exists | Auto-create the table before the first INSERT, in the chosen **Table layout** (see [Table layouts](#table-layouts)) |
| Table Engine | `MergeTree`, `ReplacingMergeTree`, `SummingMergeTree` or `AggregatingMergeTree` (also their `Replicated` forms), without arguments. Used by the custom and wide events layouts |
| ORDER BY | Custom layout only. One ClickHouse expression (default: `tuple()`) |
| PARTITION BY | Custom layout only. Optional, one ClickHouse expression |
| Dead-letter Table | On by default. Rows ClickHouse refuses go to `<table>_dlq` (see [Dead-letter table](#dead-letter-table)) |
| Max Error Ratio | Optional, 0 to 1 (default 0, off). Sent as `input_format_allow_errors_ratio`: ClickHouse skips up to this share of unparsable rows per INSERT. Skipped rows are dropped, not dead-lettered |
| Table layout | **Custom from data** (default), **Wide events**, **OpenTelemetry logs** or **OpenTelemetry traces**. See [Table layouts](#table-layouts) |
| Field mapping | Wide events only. The record fields read for each column, see [Wide events](#wide-events) |
| Keep raw record | Wide events only. Also stores each original record in a `raw String CODEC(ZSTD(3))` column. Off by default |
| TTL (days) | Wide events and OpenTelemetry layouts. Applied when the sink creates the table; `0` keeps data forever. An ingest `logs` or `traces` source sets the TTL of its OpenTelemetry table itself |
| Add columns for new fields | Off by default. See [New fields](#new-fields) |

ORDER BY and PARTITION BY must be a single expression: semicolons, comments and clause keywords such as `SETTINGS` or `AS` are refused. Database, table and column names are quoted, so any name works.

### Table layouts

The **Table layout** decides what table **Create Table If Not Exists** creates, and for wide events how each record is written. Layouts are free in every edition.

| Layout | Table |
|---|---|
| Custom from data | One column per top-level field, typed from a sample of the data (see [Auto Table Creation](#auto-table-creation)) |
| Wide events | `timestamp`, `service`, `level`, `message`, `trace_id` and `attributes`, for JSON events and logs from a webhook, Kafka or the ingest `events` signal (see [Wide events](#wide-events)) |
| OpenTelemetry logs | The `otel_logs` layout of the OpenTelemetry ClickHouse exporter, which [Telemetry](/docs/telemetry) reads |
| OpenTelemetry traces | The `otel_traces` layout, plus `<table>_trace_id_ts` and the materialized view that fills it |

A sink saved before layouts existed has none and keeps working as before: it is **Custom from data**, except behind an [Ingest](#ingest) source with the `logs` or `traces` signal, where it is the matching OpenTelemetry layout. The OpenTelemetry layouts ignore fields the table does not have (`input_format_skip_unknown_fields=1`).

### Wide events

One table that fits most JSON events and logs, sorted for the usual "this service, this level, this time range" query:

```sql
CREATE TABLE IF NOT EXISTS `db`.`events` (
  `timestamp` DateTime64(3) CODEC(Delta, ZSTD(1)),
  `service` LowCardinality(String),
  `level` LowCardinality(String),
  `message` String CODEC(ZSTD(1)),
  `trace_id` String,
  `attributes` JSON,
  INDEX idx_message `message` TYPE tokenbf_v1(32768, 3, 0) GRANULARITY 4,
  INDEX idx_trace_id `trace_id` TYPE bloom_filter(0.001) GRANULARITY 1
) ENGINE = MergeTree
PARTITION BY toDate(`timestamp`)
ORDER BY (`service`, `level`, `timestamp`)
TTL toDateTime(`timestamp`) + toIntervalDay(30)
SETTINGS non_replicated_deduplication_window = 1000, ttl_only_drop_parts = 1
```

`attributes` is the `JSON` type on ClickHouse 25.3 and later (the sink checks `SELECT version()` before creating the table). On older servers it is `Map(LowCardinality(String), String) CODEC(ZSTD(1))`, with nested fields flattened to dotted keys (`http.status`) and every value stored as text. The TTL line and `ttl_only_drop_parts` appear only with a TTL; a `Replicated` or `Shared` engine gets no deduplication window because it deduplicates on its own.

Each record is mapped when it is written. For each column the sink looks for the first of these fields that has a usable value, and removes it from the record:

| Column | Fields looked up (default) | Value |
|---|---|---|
| `timestamp` | `@timestamp`, `timestamp`, `time`, `ts`, `Timestamp` | ISO 8601 / RFC 3339 text, or a Unix epoch in seconds, milliseconds, microseconds or nanoseconds (as a number or numeric text). No usable value: the time of the insert |
| `service` | `service.name`, `service`, `host.name`, `app`, `ServiceName` | text |
| `level` | `level`, `log.level`, `severity`, `levelname`, `SeverityText` | lowercased to `trace`, `debug`, `info`, `warn`, `error` or `fatal` (`WARNING` is `warn`, `critical` is `fatal`); pino numbers (10 to 60) and OpenTelemetry severity numbers (1 to 24) are mapped too. Other text is kept lowercased |
| `message` | `message`, `msg`, `log`, `event`, `Body` | text |
| `trace_id` | `trace_id`, `traceId`, `trace.id`, `TraceId` | text |

A dotted name matches a key with dots in it or a nested field, so `service.name` reads `{"service":{"name":"api"}}` and leaves the rest of `service` in attributes. A field with the wrong kind of value (an object in `message`, say) is skipped and stays in attributes. Everything not used for a column goes into `attributes`. **Field mapping** in the sink panel replaces the list for a column with your own comma-separated field names; an empty box keeps the defaults. The capitalised names are the fields of an ingest `logs` source, so a wide events sink behind one also works.

### New fields

A custom table is created from the first batch and then stays as it is: fields the table does not have are dropped on insert (ClickHouse's default `input_format_skip_unknown_fields`).

With **Add columns for new fields** on, before each INSERT the sink compares the batch's top-level fields with the table's columns and runs `ALTER TABLE ... ADD COLUMN IF NOT EXISTS` for each new one, typed from the batch with the same rules as [Auto Table Creation](#auto-table-creation). Existing columns are never changed or dropped, even when a field's type changes. Each added column is written to the run log and to the audit log (`pipeline.column_added`). This also works for a table you created yourself, so only turn it on when the pipeline account may alter the table.

A wide events table puts new fields in `attributes`, so it never needs new columns for them. With the option on it only adds layout columns the table lacks, such as `raw` after **Keep raw record** is turned on for an existing table.

### Auto Table Creation

With the custom layout and **Create Table If Not Exists** enabled, the sink panel shows a **Columns** section. **Infer from source** saves the pipeline, reads up to 100 records from the source (as [Test](#test-a-pipeline) does; for a running webhook or ingest pipeline, the last records it received), runs them through the transforms on the way to that sink, and infers a type per top-level field:

| Values in the sample | ClickHouse type |
|---|---|
| integers that fit 64 bits | `Int64` |
| numbers with a fraction (or integers mixed with them) | `Float64` |
| integers too large for `Int64` | `String` |
| `true` / `false` | `Bool` |
| ISO 8601 / RFC 3339 date-times such as `2025-10-16T10:30:00Z` | `DateTime64(3)` |
| other text | `String`, or `LowCardinality(String)` when the sample has at least 20 records and at most 10% of the values are distinct |
| arrays of one scalar type | `Array(Int64)`, `Array(Float64)`, `Array(Bool)` or `Array(String)` |
| other arrays | `String` (the array's JSON text) |
| objects | `JSON` on ClickHouse 25.3 and later; before that `Map(String, String)` when every value is text, else `String` (the object's JSON text) |
| more than one of the above | `String` |
| only `null` | `Nullable(String)` |

`Nullable(...)` is added only when a field has both values and nulls; a missing field gets the column default. Inference also suggests:

- **ORDER BY**: the first date or time column (preferring names like `ts`, `created_at`, `*_time`) and the first id-like column (`id`, `*_id`, `uuid`), skipping `Nullable` ones, or `tuple()` when there is neither.
- **PARTITION BY**: `toYYYYMM(<time column>)` when there is a date or time column.

Each column's type can be edited, columns can be removed, and a DDL preview shows the exact `CREATE TABLE`. The columns are stored in the sink config and used when the table is created. Each type must be a single ClickHouse type such as `DateTime64(3)` or `Array(String)`; anything that would add a `DEFAULT`, `CODEC` or another column is refused. **Test** offers the same inferred schema with **Create table with these columns**.

Without saved columns the sink infers the columns the same way from the first 100 records of the first batch. Inserts into a table the sink creates parse date-times with `date_time_input_format=best_effort`, so ISO 8601 text with a zone lands in `DateTime64` columns.

The table is created before the first INSERT. If creating it fails, the next attempt tries again.

### Dead-letter table

When ClickHouse refuses an INSERT because of the data itself (a parse or type error: error codes 6, 25, 26, 27, 32, 33, 38, 41, 53, 69, 70, 72, 117, 131, 190, 349, 457, 467, 469 and 691), retrying the same rows would fail forever. Instead the pipeline splits the batch in halves until it finds the bad rows: good rows are inserted, and each bad row is written to `<target table>_dlq` in the same database. The batch then counts as written and the source moves on.

The dead-letter table is created on first use (names quoted like the target):

```sql
CREATE TABLE IF NOT EXISTS `db`.`events_dlq` (
  `ts` DateTime64(3) DEFAULT now64(3),
  `pipeline_id` String,
  `raw` String,              -- the refused record as JSON
  `error` String,            -- the ClickHouse error
  `source_position` String   -- e.g. topic/partition@offset, s3 key#line, id=42
) ENGINE = MergeTree
ORDER BY ts
```

Splitting is bounded: after 256 sub-inserts, or ranges split 12 times, a range that still fails goes to the dead-letter table whole (its `error` says so). Connection errors, timeouts and other server errors are not data errors: they are retried with backoff as before, and the split resumes where it stopped. The pipeline account needs CREATE TABLE and INSERT on the dead-letter table.

The status bar shows how many rows the pipeline has dead-lettered (across runs). Click it to open a query tab on that pipeline's rows in the dead-letter table. Turn **Dead-letter Table** off to keep the old behavior: a refused batch is retried until the data or the table is fixed.

**Max Error Ratio** is an alternative: ClickHouse itself skips up to that share of bad rows in each INSERT. Those rows are not kept anywhere. When a batch has more bad rows than the ratio allows, the dead-letter table (if on) handles it.

### Delivery and duplicates

Pipelines deliver **at least once**. A batch is acknowledged to its source (Kafka offset commit, database watermark, S3 file marked processed) only after ClickHouse accepts the INSERT. When a write fails, the pipeline retries the same batch with backoff (1 s doubling up to 60 s) and the source pauses meanwhile, so nothing is skipped. After three failures in a row the error is shown on the pipeline (status stays `running`, the badge says retrying) and in the run log; it clears once data flows again.

Retries and restarts can therefore send rows twice. Positions are saved right after each write, so after a restart this is at most the batch that was being written when CH-UI stopped. Every batch carries an `insert_deduplication_token` derived from its source position (Kafka topic, partition and offset range; S3 file, ETag and row range; database watermark range), so a repeated batch has the same token. ClickHouse drops the repeat only when deduplication is on for the table:

- `Replicated*MergeTree` tables (and ClickHouse Cloud) deduplicate inserts by default.
- Plain `MergeTree` tables need the setting, for example `ALTER TABLE events MODIFY SETTING non_replicated_deduplication_window = 1000`.

Webhook and ingest sources insert one spool segment per batch with a token made of the spool's instance id, the pipeline and the segment number, so a segment written again after a crash is dropped too. A database source without a watermark column sends no token.

Tables the sink creates get the deduplication window automatically (`SETTINGS non_replicated_deduplication_window = 1000`): the OpenTelemetry tables of ingest logs and traces, and tables created with a plain (not `Replicated` or `Shared`) engine. For such a table an insert without a token is sent with `insert_deduplicate = 0`, so identical batches from a source without positions are all kept. Tables created by earlier versions keep their settings; add the window with the `ALTER TABLE` above.

With a [Route](#route-pro) node every sink gets its own token, `<batch token>:<sink node id>`, and the batch is acknowledged (spool segment deleted, offsets committed, watermark or S3 position saved) only after every sink took its rows. When one sink fails, the whole batch is retried with backoff and the source pauses; sinks that already took their rows are not written again in that run, and when the batch is replayed after a restart they drop it by token.

### Positions and reset

After each batch is written, and before the source is told so, the runner saves the source's position in CH-UI's database, in the same order as the batches:

| Source | Saved position | On restart |
|---|---|---|
| Database (with watermark column) | The watermark of the last written row | Polls continue after it |
| S3 | Processed files with their ETag, per bucket and prefix | New and rewritten files only |
| Kafka | Next offset per partition, for display | Resumes from the consumer group's committed offsets |
| Webhook, Ingest | Nothing in the database: unwritten data waits in the ingest spool on disk | Writes what is left in the spool |

**Runs > Saved position** lists them. Deleting a webhook or ingest pipeline also deletes whatever it still had in the spool. **Reset position** (analysts and admins, with confirmation, only while the pipeline is stopped) clears them, so the next run reads the source from the beginning. Rows already in ClickHouse are sent again unless the table deduplicates them. Reset does not move Kafka offsets: to re-read a topic, set a new consumer group.

### Secrets

Passwords, secret keys, connection strings and webhook tokens in node configs are encrypted with the app secret key when saved and shown as `***` in the editor and API. Leaving `***` in place keeps the stored value. Configs saved by older versions keep working and are encrypted the next time they are saved.

## Using the Pipeline Editor

Pipelines belong to a connection and run with that connection's credentials, so the list shows only the pipelines on the connection you are signed in to. Switch connections to see and manage the others.

1. Navigate to **Pipelines** in the sidebar
2. Click **New Pipeline** and give it a name. It is created on the connection you are signed in to
3. Drag a **source** node onto the canvas from the left panel
4. Drag the **ClickHouse** sink node onto the canvas
5. Connect them by dragging from the source's output handle to the sink's input handle. To add [transforms](#transforms), drop them onto the edge
6. Click each node to configure it in the right panel
7. Click **Test** to preview the source and check the sink (see below)
8. Click **Save**, then **Run** (Run saves first and does not start if the save fails)

The status bar at the bottom shows live metrics: rows ingested, bytes, batches, failed write attempts, the time of the last written batch, rows sent to the dead-letter table, and the last error. **Runs** opens the history of runs with the log of each one.

Changes to a running pipeline are saved but take effect only after you stop it and run it again. Leaving the editor with unsaved changes asks for confirmation.

### Test a pipeline

**Test** (analysts and admins) saves the pipeline if it has unsaved changes, then reads up to 20 sample records from the source within 10 seconds and checks the sink. Nothing is written and no position moves:

| Source | Sample |
|---|---|
| Database | The query wrapped in `SELECT * FROM (<query>) LIMIT 20`, with the saved watermark (or `NULL`) as the parameter |
| S3 | The first records of the next file to process under the prefix (or the first file when all are processed) |
| Kafka | The latest messages of each partition, read without a consumer group, so no offset is committed |
| Webhook | The last 20 records received while the pipeline runs; when stopped, instructions instead |
| Ingest | Nothing to read; instructions instead (the editor's live counters show what arrives) |

The sink checks run with the pipeline's background account: the target database exists, the table exists or can be created (`CHECK GRANT CREATE TABLE`), INSERT is granted (`CHECK GRANT INSERT`), and the dead-letter table can be created and written. When the sample has records, the test also shows the schema inferred from it (see [Auto Table Creation](#auto-table-creation)).

## Pipeline Lifecycle

| Status | Meaning |
|---|---|
| `draft` | Created but never started |
| `running` | Actively processing data (shown as "running, retrying" while writes or polls keep failing) |
| `stopped` | Manually stopped by user |
| `error` | Failed with an error |
| `success` | Source completed (e.g. all S3 files processed) |

Pipelines in `running` state are automatically resumed on server restart, including after a normal shutdown. A pipeline that cannot be resumed (for example, its config no longer validates) is set to `error`.

## Example: Webhook to ClickHouse

A common pattern for receiving events from external services:

1. Create a pipeline with **Webhook** source and **ClickHouse** sink
2. Enable **Create Table If Not Exists** on the sink
3. Enable **Require Authentication** on the webhook source
4. Copy the webhook URL and Bearer token
5. Start the pipeline
6. POST data:

```bash
curl -X POST http://localhost:3488/api/pipelines/webhook/{id} \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer {token}" \
  -d '{"event":"signup","email":"user@example.com","plan":"pro"}'
```

The table is auto-created on the first request, and data flows into ClickHouse.
