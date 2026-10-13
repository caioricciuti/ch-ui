---
title: Cluster Health
description: "Operations & database monitoring for ClickHouse: replication, Keeper, merges, mutations, parts pressure, and long queries fanned out across every node"
---

Cluster Health gives you operational visibility into a running ClickHouse cluster: replication status, ZooKeeper/Keeper health, background merges and mutations, parts pressure, long-running queries, and data location, fanned out across **every node**, not just the one CH-UI is connected to.

Available on Pro and Enterprise plans.

It is built entirely from ClickHouse `system.*` tables, so there is **nothing to install on your nodes**: no agents, no exporters.

## Scope

Distributed-database monitoring has three pillars. Cluster Health deliberately covers two of them:

- **Operations**: replication, Keeper, merges/mutations, backups
- **Database**: long queries, timeouts, parts-vs-limit, data location
- **Infrastructure** (CPU / memory / disk / network): **out of scope**

Host infrastructure needs node-level agents (Prometheus, node_exporter, etc.). Everything Cluster Health shows comes from ClickHouse itself, which is why it works with zero setup.

## What it shows

### Operations

- **Replication**: per-replica delay, queue depth, inserts/merges in queue, and readonly/session-expired state (`system.replicas`).
- **Queue issues**: stalled or failing replication tasks with their retry count, postpone reason, and exception (`system.replication_queue`).
- **Keeper**: ZooKeeper/Keeper connection, session uptime, and expiry (`system.zookeeper_connection`).
- **Merges & mutations**: in-flight merges and unfinished mutations, including the failure reason for stuck mutations (`system.merges`, `system.mutations`).
- **Backups**: recent backup/restore operations and their status (`system.backups`).

### Database

- **Long queries**: currently-running queries over a configurable threshold (`system.processes`). Click a row to see the full query text, open it in a new editor tab, or kill it.
- **Parts pressure**: active parts per partition compared against `parts_to_throw_insert`, the classic "too many parts" early warning before inserts start failing (`system.parts`, `system.merge_tree_settings`).
- **Data location**: disk and S3 distribution with free/total usage (`system.disks`).

## How it works

### Fan-out across nodes

Queries run via `clusterAllReplicas('<cluster>', system.X)` with per-node attribution, so a problem on **any** replica is visible, not just the node CH-UI happens to be connected to. The cluster is auto-detected (the one containing the connected node); you can override it.

- **Single-node deployments** transparently fall back to the local node.
- Fan-out queries run with `skip_unavailable_shards = 1`, so one node being down does not blank the whole page. CH-UI compares the nodes that answered with the cluster definition in `system.clusters`: a node that is defined but did not answer is shown as **Down** (a badge in the header and a row in the node table) and is stored in history as a down sample.
- If remote nodes can't be reached at all (for example, the connection's credentials aren't valid cluster-wide), the view **degrades to the local node** and is flagged as such rather than failing. The other nodes are listed with every metric **unknown**, not as down.
- When one metric fails to collect (for example a missing grant on `system.merges`), that metric shows as **Unknown** instead of 0, in the tiles, the node table and the charts (a gap, not a healthy-looking zero). Hover the "metrics unknown" badge to see the error.
- Tables that don't exist on a given ClickHouse version (Keeper, backups) are skipped gracefully instead of erroring.

### History & retention

A background harvester samples lightweight per-node aggregates on an interval and stores them in SQLite to power the trend charts. Only compact numeric aggregates are stored (detailed drill-down lists are always fetched live), so storage stays tiny. The harvester runs with the connection's `cluster_health` [background account](/docs/background-accounts); when it has no credentials it skips that connection until the next tick.

Samples older than the **retention window are pruned automatically**. The default is **7 days**, adjustable from 1 to 365.

### Charts & filtering

The trend charts (replication delay, parts pressure) plot **one series per node** with time axes and a hover legend, over a selectable window (1h / 6h / 24h / 7d), so "which node is lagging" is visible at a glance instead of hidden in a cluster-wide aggregate.

The page also filters end-to-end: a free-text search narrows section rows (table names, postpone reasons, exceptions…), and a **node filter** (pick from the dropdown or click the funnel on any row of the per-node table) focuses the headline tiles, the charts, and every drill-down section on a single node. One click answers "is this one node's problem, or everyone's?"

### Status thresholds

The headline tiles turn amber or red at these levels:

| Tile | Warning | Critical |
| --- | --- | --- |
| Max replication delay | 10 s | 60 s |
| Replication queue | 10 | 100 |
| Readonly replicas | n/a | any |
| Parts pressure | 50% | 80% |
| Mutations pending | 5 pending, or the oldest older than 15 min | 20 pending, or the oldest older than 1 h |
| Long queries | 1 | 5 |
| Max disk used | 80% | 90% |

A single pending mutation is normal and stays neutral. Max disk used is the fullest disk on any node (`system.disks`), shown live and not stored in history.

These tile colors are display only. To be notified, use the health checks below.

## Health alerts

Every harvester poll also evaluates six built-in health checks per node: replication lag, read-only replicas, disk usage (with an optional free-space floor), parts pressure, stuck mutations and unreachable nodes. A seventh check, **Connection unreachable**, fires when the connection itself cannot be reached (tunnel offline, or no samples and a failed `SELECT 1`). They fire after consecutive breaches, resolve after consecutive OK polls, and send `health.*` events through your [alert rules](/docs/alerts#health-checks). Thresholds are edited per connection under **Governance → Health checks**; their defaults are looser than the tile colors above, since a notification should mean someone has to act.

The node-level checks only run while background collection is enabled for the connection; Connection unreachable also runs with collection off, using only `SELECT 1`. See [Health checks](/docs/alerts#health-checks) for the details, and [Silences and acknowledgements](/docs/alerts#silences-and-acknowledgements) to mute a known incident or a maintenance window. When any check is firing, the Cluster Health header shows an **N alerts firing** badge that links to the Health checks section.

## Killing queries and mutations

From **Long Queries** or **Mutations**, open a row and choose **Kill**. A confirmation shows the query or mutation command, the user and how long it has been running. CH-UI then runs `KILL QUERY` or `KILL MUTATION` with your own ClickHouse credentials, so ClickHouse decides whether you may (any user can kill their own queries; killing others needs the `KILL QUERY` grant, and killing a mutation needs `ALTER` on the table). In CH-UI it needs the admin or analyst role. On a cluster the statement is sent `ON CLUSTER`; when distributed DDL is not available it runs on the connected node only and the toast says so. Every kill is written to the audit log (`cluster_health.query.killed`, `cluster_health.mutation.killed`).

## Settings

Open **Cluster Health → Settings** (admin only):

| Setting | Default | Range | Purpose |
| --- | --- | --- | --- |
| Enable background collection | On | n/a | Turn the harvester on or off per connection |
| History retention (days) | 7 | 1–365 | TTL for stored samples |
| Poll interval (seconds) | 60 | 15–3600 | How often each cluster is sampled |
| Long-query threshold (seconds) | 30 | 1–3600 | When a running query counts as "long" |

## Requirements

- A **Pro** license. The page, the `/api/cluster-health/*` endpoints, and the background harvester are all disabled on the free edition.
- The connection's ClickHouse user needs read access to the relevant `system.*` tables, and to remote nodes for full cluster fan-out.

For load-balanced deployments where CH-UI sits behind chproxy / HAProxy / a Kubernetes Service, see [ClickHouse Clusters & Load Balancers](/docs/clusters) for the session-affinity setup that keeps `system.*` reads consistent.

Cluster Health pairs naturally with [Query Insights](/docs/query-insights): Cluster Health tells you *what the cluster is doing*, Query Insights tells you *which queries made it do that*.
