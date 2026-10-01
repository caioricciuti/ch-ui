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

Pipelines run as background goroutines inside the CH-UI server process. They survive restarts (crash recovery resumes any pipeline that was in `running` state).

## Source Connectors

### Webhook

Receive data via HTTP POST. When the pipeline starts, CH-UI exposes an endpoint:

```
POST /api/pipelines/webhook/{pipeline-id}
```

Configuration:

| Field | Description |
|---|---|
| Require Authentication | Toggle to enable Bearer token auth |
| Batch Size | Records per batch before flushing (default: 100) |
| Batch Timeout (ms) | Max wait before flushing partial batch (default: 2000) |

When authentication is enabled, a Bearer token is auto-generated. Include it in requests:

```bash
curl -X POST http://your-ch-ui:3488/api/pipelines/webhook/{pipeline-id} \
  -H "Content-Type: application/json" \
  -H "Authorization: Bearer {token}" \
  -d '[{"event":"click","user_id":42,"ts":"2025-01-15T10:30:00Z"}]'
```

Accepted formats: JSON object, JSON array, or newline-delimited JSON (NDJSON). Auto-detected from the payload.

### Kafka

Consume messages from a Kafka topic using consumer groups.

| Field | Description |
|---|---|
| Brokers | Comma-separated broker addresses (e.g. `broker1:9092,broker2:9092`) |
| Topic | Kafka topic to consume |
| Consumer Group | Consumer group ID (default: `ch-ui-pipeline`) |
| SASL Mechanism | `PLAIN`, `SCRAM-SHA-256`, or `SCRAM-SHA-512` (optional) |
| SASL Username | Username for SASL auth |
| SASL Password | Password for SASL auth |
| Enable TLS | Toggle for TLS connections |
| Batch Size | Records per batch (default: 1000) |
| Batch Timeout (ms) | Max wait before flush (default: 5000) |

### Database

Poll rows from PostgreSQL, MySQL, or SQLite with optional watermark-based incremental reads.

| Field | Description |
|---|---|
| Database Type | `PostgreSQL`, `MySQL`, or `SQLite` |
| Connection String | DSN (e.g. `postgres://user:pass@host/db` or `/path/to/file.db` for SQLite) |
| SQL Query | Query to execute each poll (use `$1` for watermark parameter) |
| Poll Interval (seconds) | Seconds between polls (default: 60) |
| Watermark Column | Column for incremental polling (e.g. `id` or `created_at`) |
| Batch Size | Max rows per batch (default: 1000) |

Watermark example query:

```sql
SELECT * FROM events WHERE id > $1 ORDER BY id
```

The pipeline tracks the last seen watermark value and passes it as `$1` on each poll.

### S3

Read files from S3-compatible storage (AWS S3, MinIO, etc.).

| Field | Description |
|---|---|
| Endpoint | S3 endpoint URL (e.g. `https://s3.amazonaws.com`) |
| Region | AWS region (default: `us-east-1`) |
| Bucket | Bucket name |
| Key Prefix | Filter files by prefix (e.g. `data/events/`) |
| Access Key ID | S3 credentials |
| Secret Access Key | S3 credentials |
| File Format | `JSON`, `JSON Lines (NDJSON)`, or `CSV` |
| Poll Interval (seconds) | Seconds between scans (default: 300) |
| Batch Size | Records per batch (default: 1000) |

Files are tracked to avoid reprocessing. New files detected on each poll are parsed and ingested.

## ClickHouse Sink

The sink writes batches to a ClickHouse table using `INSERT FORMAT JSONEachRow` through the tunnel gateway. It runs with the connection's `pipeline` [background account](/docs/background-accounts), fetched again for every batch, so the account needs INSERT on the target (and CREATE TABLE if the sink creates it).

| Field | Description |
|---|---|
| Target Database | ClickHouse database name (default: `default`) |
| Target Table | Table name |
| Create Table If Not Exists | Auto-create table from first batch schema |
| Table Engine | `MergeTree`, `ReplacingMergeTree`, or `SummingMergeTree` |
| ORDER BY | ClickHouse ORDER BY clause (default: `tuple()`) |

### Auto Table Creation

When **Create Table If Not Exists** is enabled, the sink inspects the first incoming batch and infers column types:

| JSON type | ClickHouse type |
|---|---|
| string | `String` |
| number | `Float64` |
| boolean | `UInt8` |
| null | `Nullable(String)` |

The table is created once, before the first INSERT. Subsequent batches use the existing table.

## Using the Pipeline Editor

1. Navigate to **Pipelines** in the sidebar
2. Click **New Pipeline**, give it a name and select a connection
3. Drag a **source** node onto the canvas from the left panel
4. Drag the **ClickHouse** sink node onto the canvas
5. Connect them by dragging from the source's output handle to the sink's input handle
6. Click each node to configure it in the right panel
7. Click **Save**, then **Start**

The status bar at the bottom shows live metrics: rows ingested, bytes, batches, and errors.

## Pipeline Lifecycle

| Status | Meaning |
|---|---|
| `draft` | Created but never started |
| `running` | Actively processing data |
| `stopped` | Manually stopped by user |
| `error` | Failed with an error |
| `success` | Source completed (e.g. all S3 files processed) |

Pipelines in `running` state are automatically resumed on server restart.

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
