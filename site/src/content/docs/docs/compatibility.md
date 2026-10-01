---
title: Compatibility & Support Matrix
description: Supported ClickHouse versions, browsers, platforms, and sizing guidance for CH-UI
---

## ClickHouse versions

CH-UI talks to ClickHouse over the HTTP interface and reads standard system
tables. It works with current ClickHouse releases and ClickHouse Cloud.

| ClickHouse | Status |
|---|---|
| 24.x and newer | **Recommended**: fully supported, tested target |
| 23.x | Supported |
| ≤ 22.x | Best effort: core querying works; some Pro analytics may be limited |
| ClickHouse Cloud | Supported (connect over HTTPS) |

Pro analytics rely on system tables present in modern ClickHouse:

- **Query Insights**: `system.query_log` (enable query logging)
- **Cluster Health**: `system.clusters`, `system.replicas`, `system.merges`,
  `system.mutations`, `system.parts`, Keeper tables
- **Governance**: `system.grants`, `system.tables`, `system.columns`

## Browsers

The web UI targets current evergreen browsers: the latest two stable versions
of Chrome, Edge, Firefox, and Safari. Internet Explorer is not supported.

## Platforms

| Component | Linux | macOS | Windows |
|---|---|---|---|
| `ch-ui server` | amd64, arm64 | amd64, arm64 | n/a |
| `ch-ui connect` (connector) | amd64, arm64 | amd64, arm64 | supported |
| Docker image | amd64, arm64 (multi-arch) | n/a | n/a |

The server is distributed as a static binary (no CGO) and a multi-arch Docker
image (`ghcr.io/caioricciuti/ch-ui`). A Helm chart ships under
`deploy/helm/ch-ui`. See [Deployment](/docs/deployment).

## Architecture

CH-UI is a **single-instance** application: it stores state in a local SQLite
database and keeps tunnel state in memory. Run one instance behind a load
balancer for TLS/routing. Do not run multiple replicas against one database.

## Sizing guidance

CH-UI itself is lightweight; heavy work runs in ClickHouse. Starting points:

| Deployment | CPU | Memory | Disk |
|---|---|---|---|
| Small team (≤ 25 users) | 0.5 vCPU | 256 MB | 1–5 GB for `ch-ui.db` |
| Team (25–100 users) | 1 vCPU | 512 MB | 5–10 GB |
| Large (100+ users) | 2 vCPU | 1 GB | 10–20 GB |

Disk grows with stored query history, audit logs, and governance metadata.
Streaming query results are capped server-side rather than buffered, so large
result sets do not scale memory linearly. Scrape `/metrics` to track real usage;
see [Monitoring & SIEM](/docs/monitoring).
