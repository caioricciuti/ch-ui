---
title: Pipelines
description: Visual data pipeline builder for ingesting data into ClickHouse
---

Pipelines is a visual data pipeline builder that lets you create ingestion flows from multiple sources into ClickHouse tables using a drag-and-drop graph editor.

No external dependencies. Connectors compile into the CH-UI binary.

## Overview

Each pipeline has:

- **One source node** (where data comes from)
- **One sink node** (ClickHouse table)
- **An edge** connecting them

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

Receive OpenTelemetry logs or traces (OTLP over HTTP, protobuf or JSON) or JSON events from shippers such as the OpenTelemetry Collector, OTel SDKs, Vector and Fluent Bit. Shippers authenticate with per-pipeline ingest keys and send to:

```
POST /ingest/v1/logs
POST /ingest/v1/traces
POST /ingest/v1/events
```

| Field | Description |
|---|---|
| Signal | `logs`, `traces` or `events` |
| TTL days | Applied when the sink creates a logs or traces table (`0` keeps data forever) |
| Rate limit (requests/s) | Per key (`0` means 200) |
| Daily quota (MB) | Per pipeline, decompressed bytes per UTC day (`0` means unlimited) |

With **Create Table If Not Exists** on the sink, logs and traces tables are created in the OpenTelemetry exporter's layout, so [Telemetry](/docs/telemetry) reads them as they are. Requests are spooled on disk before they are answered, like the webhook. Keys, endpoints, responses, limits and shipper configs are on the [Ingest](/docs/ingest) page.

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

## ClickHouse Sink

The sink writes batches to a ClickHouse table using `INSERT FORMAT JSONEachRow` through the tunnel gateway. It runs with the connection's `pipeline` [background account](/docs/background-accounts), fetched again for every batch, so the account needs INSERT on the target (and CREATE TABLE if the sink creates it).

| Field | Description |
|---|---|
| Target Database | ClickHouse database name (default: `default`) |
| Target Table | Table name |
| Create Table If Not Exists | Auto-create the table before the first INSERT, from saved columns or the first batch (see [Auto Table Creation](#auto-table-creation)) |
| Table Engine | `MergeTree`, `ReplacingMergeTree`, `SummingMergeTree` or `AggregatingMergeTree` (also their `Replicated` forms), without arguments |
| ORDER BY | One ClickHouse expression (default: `tuple()`) |
| PARTITION BY | Optional, one ClickHouse expression |
| Dead-letter Table | On by default. Rows ClickHouse refuses go to `<table>_dlq` (see [Dead-letter table](#dead-letter-table)) |
| Max Error Ratio | Optional, 0 to 1 (default 0, off). Sent as `input_format_allow_errors_ratio`: ClickHouse skips up to this share of unparsable rows per INSERT. Skipped rows are dropped, not dead-lettered |

ORDER BY and PARTITION BY must be a single expression: semicolons, comments and clause keywords such as `SETTINGS` or `AS` are refused. Database, table and column names are quoted, so any name works.

### Auto Table Creation

When **Create Table If Not Exists** is enabled, the sink panel shows a **Columns** section. **Infer from source** saves the pipeline, reads a sample from the source (as [Test](#test-a-pipeline) does) and asks ClickHouse for the types with `DESC format(JSONEachRow, '<sample>')`, run read-only with your session credentials. Top-level `Nullable` is dropped (missing and null values get the column default on insert). It also suggests:

- **ORDER BY**: the first date or time column (preferring names like `ts`, `created_at`, `*_time`) and the first id-like column (`id`, `*_id`, `uuid`), or `tuple()` when there is neither.
- **PARTITION BY**: `toYYYYMM(<time column>)` when there is a date or time column.

Each column's type can be edited, columns can be removed, and a DDL preview shows the exact `CREATE TABLE`. The columns are stored in the sink config and used when the table is created. Each type must be a single ClickHouse type such as `DateTime64(3)` or `Array(String)`; anything that would add a `DEFAULT`, `CODEC` or another column is refused. **Test** offers the same inferred schema with **Create table with these columns**.

Without saved columns the sink falls back to inspecting the first incoming batch:

| JSON type | ClickHouse type |
|---|---|
| string | `String` |
| number | `Float64` |
| boolean | `UInt8` |
| null | `Nullable(String)` |

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

Webhook and ingest sources insert one spool segment per batch with a token made of the spool's instance id, the pipeline and the segment number, so a segment written again after a crash is dropped too. Tables the sink creates for ingest logs and traces get the deduplication window automatically. A database source without a watermark column sends no token.

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
5. Connect them by dragging from the source's output handle to the sink's input handle
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

The sink checks run with the pipeline's background account: the target database exists, the table exists or can be created (`CHECK GRANT CREATE TABLE`), INSERT is granted (`CHECK GRANT INSERT`), and the dead-letter table can be created and written. When the sample has records, the test also shows the schema ClickHouse infers from it (see [Auto Table Creation](#auto-table-creation)).

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
