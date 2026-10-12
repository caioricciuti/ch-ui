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
| Batch Size | Records per batch before flushing (default: 100, minimum 1) |
| Batch Timeout (ms) | Max wait before flushing partial batch (default: 2000, minimum 100) |

When authentication is enabled, a Bearer token is generated in the browser. Copy it before you save: the token is stored encrypted and is not shown again. **Generate new token** replaces it, after which senders need the new value. Include it in requests:

```bash
curl -X POST http://your-ch-ui:3488/api/pipelines/webhook/{pipeline-id} \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer {token}" \
  -d '[{"event":"click","user_id":42,"ts":"2025-01-15T10:30:00Z"}]'
```

Send the token in the `Authorization` header. A `?token={token}` query parameter is still accepted so existing senders keep working, but avoid it: query strings end up in reverse proxy and access logs, which leaks the token to anyone who can read them.

Accepted formats: a JSON object (pretty-printed is fine), a JSON array of objects, or newline-delimited JSON (NDJSON, one object per line). The whole body is tried as JSON first, then as NDJSON.

A request is accepted whole or not at all. When the pipeline's buffer (1000 records) has no room for every record in the request, nothing is queued and CH-UI answers `429 Too Many Requests` with `Retry-After: 1`; retry the same request. A single request with more than 1000 records gets `413`.

A `200` response means the records are queued, not yet written. If ClickHouse is down the pipeline retries the write and the buffer fills, so senders start getting `429`. Records still in the buffer when CH-UI stops are lost: webhook delivery is at-most-once for that window. Senders that need stronger guarantees should write to Kafka and use the Kafka source.

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

Offsets are committed only after a batch is written to ClickHouse. Messages that are not a JSON object are inserted as `{"_raw": "...", "_topic", "_partition", "_offset", "_timestamp"}`, so the target table needs a `_raw` column to keep them. When a broker is unreachable the source reconnects with backoff (1 s doubling up to 60 s) instead of stopping.

### Database

Poll rows from PostgreSQL, MySQL, or SQLite with optional watermark-based incremental reads.

| Field | Description |
|---|---|
| Database Type | `PostgreSQL` (default), `MySQL`, or `SQLite` |
| Connection String | DSN (e.g. `postgres://user:pass@host/db` or `/path/to/file.db` for SQLite). Stored encrypted |
| SQL Query | Query to execute each poll (use `$1` for watermark parameter) |
| Poll Interval (seconds) | Seconds between polls (default: 60, minimum 1) |
| Watermark Column | Column for incremental polling (e.g. `id` or `created_at`) |
| Batch Size | Max rows per batch (default: 1000, minimum 1) |

Watermark example query:

```sql
SELECT * FROM events WHERE id > $1 ORDER BY id
```

The pipeline passes the watermark of the last **written** batch as `$1` on each poll, and the next poll waits until the rows of the previous one are written. Order the query by the watermark column. The watermark is kept in memory: after a restart the first poll runs without `$1` and reads from the start again (see [Delivery and duplicates](#delivery-and-duplicates)). Without a watermark column every poll reads the whole query result again.

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

A file counts as processed only after all its rows are written. The list of processed files is kept in memory, so after a restart every file under the prefix is read again (see [Delivery and duplicates](#delivery-and-duplicates)).

## ClickHouse Sink

The sink writes batches to a ClickHouse table using `INSERT FORMAT JSONEachRow` through the tunnel gateway. It runs with the connection's `pipeline` [background account](/docs/background-accounts), fetched again for every batch, so the account needs INSERT on the target (and CREATE TABLE if the sink creates it).

| Field | Description |
|---|---|
| Target Database | ClickHouse database name (default: `default`) |
| Target Table | Table name |
| Create Table If Not Exists | Auto-create table from first batch schema |
| Table Engine | `MergeTree`, `ReplacingMergeTree`, `SummingMergeTree` or `AggregatingMergeTree` (also their `Replicated` forms), without arguments |
| ORDER BY | One ClickHouse expression (default: `tuple()`) |
| PARTITION BY | Optional, one ClickHouse expression |

ORDER BY and PARTITION BY must be a single expression: semicolons, comments and clause keywords such as `SETTINGS` or `AS` are refused. Database, table and column names are quoted, so any name works.

### Auto Table Creation

When **Create Table If Not Exists** is enabled, the sink inspects the first incoming batch and infers column types:

| JSON type | ClickHouse type |
|---|---|
| string | `String` |
| number | `Float64` |
| boolean | `UInt8` |
| null | `Nullable(String)` |

The table is created before the first INSERT. If creating it fails, the next attempt tries again.

### Delivery and duplicates

Pipelines deliver **at least once**. A batch is acknowledged to its source (Kafka offset commit, database watermark, S3 file marked processed) only after ClickHouse accepts the INSERT. When a write fails, the pipeline retries the same batch with backoff (1 s doubling up to 60 s) and the source pauses meanwhile, so nothing is skipped. After three failures in a row the error is shown on the pipeline (status stays `running`, the badge says retrying) and in the run log; it clears once data flows again.

Retries and restarts can therefore send rows twice. Every batch carries an `insert_deduplication_token` derived from its source position (Kafka topic, partition and offset range; S3 file, ETag and row range; database watermark range), so a repeated batch has the same token. ClickHouse drops the repeat only when deduplication is on for the table:

- `Replicated*MergeTree` tables (and ClickHouse Cloud) deduplicate inserts by default.
- Plain `MergeTree` tables need the setting, for example `ALTER TABLE events MODIFY SETTING non_replicated_deduplication_window = 1000`.

The webhook source has no replayable position: see [Webhook](#webhook). A database source without a watermark column sends no token.

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
7. Click **Save**, then **Run** (Run saves first and does not start if the save fails)

The status bar at the bottom shows live metrics: rows ingested, bytes, batches, failed write attempts, the time of the last written batch, and the last error. **Runs** opens the history of runs with the log of each one.

Changes to a running pipeline are saved but take effect only after you stop it and run it again. Leaving the editor with unsaved changes asks for confirmation.

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
