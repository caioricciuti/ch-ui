---
title: Incident Timeline
description: One chronological view of query failures, latency, merges and mutations, cluster pressure, governance incidents and deployment annotations for a time window.
---

When something breaks, the first question is usually "what else happened around then?". The Incident Timeline puts the evidence for one connection in a single list, in time order: query failures and latency, merges and mutations, cluster pressure, governance incidents and their comments, and deployments your team recorded.

Available on Pro and Enterprise plans.

Open it from **Operate → Incident Timeline**. It is only available to administrators.

The timeline shows what was observed at the same time. It does not claim that one event caused another.

## Reading the timeline

1. Pick a time range at the top. The default is the last 6 hours, and the window can be at most 7 days.
2. Use the source buttons to show or hide each source.
3. Check **Source coverage** first. Each source shows how many observations it returned, whether it is **Unavailable**, or whether it was cut at **First 500 events**, with a note on what that source can and cannot show.
4. Read the list. Events with failures, errors or pressure are highlighted. Events tied to a governance incident have an **Open incident** link that shows its details and comments without leaving the page.

Times are shown in your browser's timezone. You can link to a specific window with `?from=...&to=...` (RFC 3339 timestamps) and to an incident with `?incident_id=...`.

## Sources

| Source | Where it comes from | Notes |
|---|---|---|
| Queries | `system.query_log`, live | Initial queries grouped into time buckets: query count, failures, p95 and maximum latency. No query text or users. |
| Merges / mutations | `system.part_log`, live | Completed `MergeParts` and `MutatePart` events with table, duration and error code. Needs `system.part_log` enabled. |
| Cluster pressure | Retained [Cluster Health](/docs/cluster-health) samples | Only samples with replication delay of 30 s or more, readonly replicas, or parts pressure of 80% or more. Normal samples are left out. |
| Incidents | Governance incidents | By first-seen time |
| Comments | Comments on governance incidents | By creation time |
| Deployments | Deployment annotations recorded here | See below |

Query buckets scale with the window: 1 minute for windows up to 6 hours, 5 minutes up to 24 hours, and 30 minutes beyond that.

The two live sources run with your own ClickHouse credentials, on the connected node only, with `readonly=1`, a 10 second execution limit, 512 MiB of memory and 2 threads. Activity on other nodes may be missing, and what you see depends on your system log settings and retention. If the connection is offline or a query fails, that source is marked unavailable and the others still load.

Retained Cluster Health data only exists if background collection is enabled for the connection and the samples have not expired.

Each source returns at most 500 events for the window. If you hit that limit, narrow the window.

## Recording a deployment

Click **Record deployment** to put a release or schema change on the timeline next to the operational evidence. Fill in when it happened (in your local time), a title, and optional details. The annotation stores your identity (SSO subject if you signed in with SSO, otherwise your ClickHouse user) and the connection.

If the deployment falls outside the window you are looking at, the view moves to 30 minutes either side of it.

A deployment marker only says the deployment happened then. It does not mean it caused nearby failures.

## Limits

- Window: more than zero and at most 7 days.
- 500 events per source.
- Annotation title 1 to 200 characters, details up to 4,000.
- An empty timeline does not prove nothing happened. Check coverage, retention and permissions.

## Permissions

| Action | Analyst | Admin |
|---|---|---|
| View the timeline | No | Yes |
| Add a deployment annotation | Yes (API) | Yes |
| Delete your own annotation | Yes (API) | Yes (API) |
| Delete someone else's annotation | No | Yes (API) |

Viewers have no access. The combined view is admin-only because retained samples can come from a privileged background account. Analysts can add annotations through the API even though they cannot open the page. There is no delete button in the UI; deleting is done through the API.

Creating and deleting annotations is recorded in the [audit log](/docs/audit-log) as `incident.annotation.created` and `incident.annotation.deleted`, with the annotation id in the details.

Annotations are pruned 180 days after the deployment they mark (v2.14.3+; change it under [Data Retention](/docs/admin#data-retention), `incident_annotations`).

## API

| Method | Path | Role | Purpose |
|---|---|---|---|
| `GET` | `/api/incident-timeline/?from=...&to=...` | Admin | Timeline for the window. Optional `incident_id` checks the incident belongs to this connection. |
| `POST` | `/api/incident-timeline/annotations` | Analyst, admin | Add a deployment annotation |
| `DELETE` | `/api/incident-timeline/annotations/{id}` | Analyst (own), admin (any) | Delete an annotation |

```json
POST /api/incident-timeline/annotations
{ "occurred_at": "2026-09-21T14:05:00Z", "title": "API v1.8 deployed", "details": "Release notes: ..." }
```

The `GET` response contains `events`, `coverage` (one entry per source with `available`, `events`, `truncated` and `message`), `from`, `to` and `bucket_seconds`.
