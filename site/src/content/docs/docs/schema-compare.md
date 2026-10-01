---
title: Schema Compare
description: Compare table and column metadata between two ClickHouse connections or databases and download a commented SQL review plan.
---

Schema Compare shows how one database differs from another, for example staging against production before a migration, or two regions that should be identical. It reads metadata only and produces a SQL review plan in which every line is commented out. Nothing is executed.

Available on Pro and Enterprise plans.

Open it from **Build → Schema Compare**.

## Comparing two databases

The page has two sides:

- **Source · desired schema**: the definition you want to end up with.
- **Target · compare against**: the database you are checking.

Review suggestions describe how to bring the target toward the source.

1. For each side, choose a **Connection** and type a **Database** name. Both sides can use the same connection with different databases.
2. If a side uses your current connection, it runs with your current session. For any other connection, enter a **ClickHouse username** and **Password** for that connection. These are used for this one comparison and are never saved.
3. Click **Compare schemas**.

The result shows how many tables each side has, how many match, and the number of differences. Filter the list by table or field, or by type:

- **Changed**: the table or column exists on both sides with a different definition.
- **Missing in target**: exists only in the source.
- **Only in target**: exists only in the target.

Each difference shows the source and target values and a **Review suggestion**. Click **Download review SQL** to save the whole plan as `schema-review.sql`.

## What is compared

For each table: engine, sorting key, partition key, primary key, sampling key, TTL, and for views the view definition (including a materialized view's `TO` destination).

For each column: name, type, default kind and expression, compression codec, and position.

Not compared: engine arguments, table settings, grants, dictionaries, and data. Temporary tables are skipped.

## The review plan

The downloaded plan starts with a header saying it has not been executed. Every line, including any line breaks that came from the metadata itself, is prefixed with `--`, so pasting the file into a SQL editor runs nothing. You have to uncomment and adapt each statement yourself.

What the suggestions look like:

- **Missing column**: `ALTER TABLE ... ADD COLUMN ...`
- **Changed column**: `ALTER TABLE ... MODIFY COLUMN ...`, with a reminder to check type conversions and dependent views
- **Missing table**: a `CREATE TABLE` with the source columns, keys and TTL. The engine is written as `ENGINE = <name> /* supply reviewed engine arguments */`, because engine arguments can hold credentials or cluster-specific paths and are never copied.
- **Missing view**: a `CREATE VIEW` or `CREATE MATERIALIZED VIEW` with the source definition
- **Changed TTL**: `MODIFY TTL` or `REMOVE TTL`, with a warning that TTL changes can expire existing data
- **Changed sorting key**: `MODIFY ORDER BY`, with a note to check engine restrictions
- **Changed engine, partition, primary or sampling key**: a note that the change may need a table rebuild, with the desired value
- **Only in target** (table or column): a note that it was kept. Nothing suggests dropping it; removal needs your explicit review.

Treat engine, key, view and destructive changes with care. Some of them cannot be applied with `ALTER` and need a rebuild or a manual migration.

## Limits

- Both connections must be online, otherwise the comparison fails.
- The comparison only sees objects visible to the accounts used. Use accounts with the same visibility on both sides, or the result will show differences that are really permission gaps.
- A permission or query error fails the whole comparison. It never shows two empty schemas as identical. Error messages from ClickHouse are not passed through, since they could contain connection details.
- Each database can have at most 10,000 tables and 10,000 columns. Larger databases are refused; compare a smaller one.
- Metadata reads run with `readonly=1`, a 15 second execution limit, and a 16 MiB result limit.
- The accounts need `SELECT` on `system.databases`, `system.tables` and `system.columns`.

## Permissions

Any signed-in role (viewer, analyst, admin) can run a comparison. What it can read is limited by the ClickHouse accounts used. Credentials for another connection are required every time; CH-UI does not reuse another user's session or a background account for this.

Comparisons are not stored and are not written to the [audit log](/docs/audit-log).

## API

| Method | Path | Purpose |
|---|---|---|
| `GET` | `/api/schema-compare/connections` | Connections with `online` and `uses_session` (true for your current connection) |
| `POST` | `/api/schema-compare/compare` | Run a comparison |

```json
POST /api/schema-compare/compare
{
  "source": { "connection_id": "conn-staging", "database": "shop" },
  "target": { "connection_id": "conn-prod", "database": "shop", "username": "reader", "password": "..." }
}
```

`username` and `password` are required for any side that is not your current connection. The response has `source`, `target`, `captured_at`, `scope`, and `result` with `differences`, `source_tables`, `target_tables`, `matching_tables` and the full commented plan in `review_sql`.

Status codes: `400` for a missing database or missing credentials, `404` for an unknown connection, `503` if either connection is offline, `502` if metadata could not be read.
