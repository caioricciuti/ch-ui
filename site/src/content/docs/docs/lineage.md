---
title: Data Lineage
description: See which tables feed which materialized views, views and dictionaries, and where they write, read from ClickHouse system tables with your own account.
---

Data lineage shows how data moves between objects on your ClickHouse server: which table triggers which materialized view, where that view writes, which views and dictionaries read from which tables, and where data enters from outside ClickHouse (Kafka, S3, MySQL and so on).

Available in every edition, to every signed-in role. The UI shows the graph on the Explore page and for a single table in its table view.

## How it is read

- **On demand.** Nothing runs until you open lineage. There is no background job and CH-UI never reads `system.query_log` for it.
- **With your ClickHouse account.** You see the objects your account can see, nothing more.
- **From metadata only.** One read of `system.tables`, plus `system.dictionaries`, `system.view_refreshes` and the server's host name when your account can read them. Definitions are parsed by CH-UI; no query is run against your data.
- **Cached for 30 seconds** per connection, ClickHouse user and database selection. Refresh to read again.
- **On a cluster, the connected node only.** Lineage reads the system tables of the node CH-UI is connected to. The graph says which node that is and whether the server is part of a multi-node cluster.

## Nodes

| Kind | What it is |
|---|---|
| `table` | A table that stores data (MergeTree family, Log, Memory, ...) |
| `mv` | A materialized view. Without `TO`, its inner table (`.inner_id.<uuid>` or `.inner.<name>`) is folded into it: the node shows the inner table's rows and bytes, and the inner table never appears on its own |
| `refreshable_mv` | A materialized view with `REFRESH`, with its refresh status, last success and next run when `system.view_refreshes` is readable |
| `view` | A plain view |
| `dictionary` | A dictionary |
| `distributed`, `buffer`, `merge` | Tables with those engines |
| `external` | Data from outside ClickHouse: a table with an engine such as Kafka, RabbitMQ, NATS, S3, S3Queue, URL, MySQL or PostgreSQL, or a table function such as `s3(...)` or `url(...)` in a view's query. It carries a short description such as `kafka://broker:9092/topic` or the object URL. Credentials, query strings and signatures are never included |
| `hidden` | A materialized view that a table you can see lists as dependent, but that your account cannot see |
| `missing` | An object a definition names that does not exist (or is not visible): a dangling `TO` target, a dropped table a view still reads |

## Edges

Every edge points the way data flows, from source to consumer.

| Edge | Meaning | Read from |
|---|---|---|
| table `triggers` MV | Inserts into the table run the materialized view | `dependencies_*` in `system.tables` |
| MV `writes` table | The view's `TO` target | Its definition |
| table `reads` view or MV | The view's query reads the table: `FROM`, `JOIN`, subqueries, `IN (SELECT ...)`, `dictGet('db.dict', ...)`, table functions and a refreshable view's `DEPENDS ON` | Its definition |
| Buffer `routes` table | The Buffer flushes into its destination | `engine_full` |
| Distributed `routes` table | Inserts go to the local table | `engine_full` |
| table `routes` Merge | A Merge table reads the tables its pattern matches | `engine_full` |
| source `sources` dictionary | The dictionary's source table or query, or an external source | Its definition, or `system.dictionaries` |

Each edge has a **confidence**:

- `system`: ClickHouse states it in a metadata column.
- `parsed`: CH-UI read it from a view, materialized view or dictionary definition.

When both describe the same pair, the system edge is kept: the table a materialized view reads on insert shows as `triggers`, not `reads`.

Definitions are parsed by a tokenizer, not pattern matching, so a table name inside a string or a comment is never taken for a reference. Common table expressions (`WITH x AS (...)`) and aliases are not tables, and an unqualified table name belongs to the object's own database. Generator functions such as `numbers()` add nothing. A Distributed table's local table on other nodes shows as `missing` when the connected node does not have it.

## Choosing databases

With no selection, the graph holds every database your account can see except `system` and `information_schema`. Selecting databases keeps their objects plus every object one edge away, wherever it lives: a view, materialized view or dictionary in another database that reads a selected table shows, and so does a materialized view there that a selected table triggers. View, materialized view and dictionary definitions are read in every database for this; other objects outside the selection are read by name only.

Up to 50,000 visible objects are read. Above that, choose databases: only their objects are then read, references outside them show as `missing`, and the graph says so in its warnings. If the selected databases alone hold more than 50,000 objects, the request is refused with `422`.

## Warnings

The graph lists what it could not read, for example:

- `system.dictionaries not readable`: dictionary sources come from their definitions only.
- `system.view_refreshes not readable`: refresh status is missing on refreshable views.
- `system.clusters not readable`: CH-UI cannot tell whether the server is one node of a cluster.

Grant `SELECT` on those tables to the ClickHouse user to remove them.

## API

`GET /api/lineage` returns the graph; `GET /api/lineage/node?id=db.name` returns one object with its definition. See the [API reference](/docs/api#lineage).

```json
{
  "nodes": [
    {"id": "app.events", "database": "app", "name": "events", "kind": "table", "engine": "MergeTree", "rows": 1200, "bytes": 48213, "refresh": null},
    {"id": "app.events_mv", "database": "app", "name": "events_mv", "kind": "mv", "engine": "MaterializedView", "rows": null, "bytes": null, "refresh": null}
  ],
  "edges": [
    {"from": "app.events", "to": "app.events_mv", "kind": "triggers", "confidence": "system"}
  ],
  "meta": {"host": "chn1", "multi_node": false, "generated_at": "2026-10-15T09:00:00Z", "cached": false, "node_count": 2, "warnings": []}
}
```

Nodes found in a table function or a dictionary source outside ClickHouse have an id starting with `external:` and an empty `database`.
