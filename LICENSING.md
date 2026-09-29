# CH-UI Licensing

CH-UI is **dual-licensed**. Most of the project is open source under Apache 2.0;
a defined set of "Pro" features is source-available under the Business Source
License 1.1 (BSL 1.1).

This document is the **authoritative description** of which code is covered by
which license. Where this document and an individual file header disagree, the
file's `SPDX-License-Identifier` header governs that file.

> This is an engineering description of the licensing layout, not legal advice.
> Have the `LICENSE.BSL` parameters (especially the Additional Use Grant) reviewed
> by a lawyer before relying on them commercially.

## Community core — Apache License 2.0

Everything in the repository is licensed under **Apache 2.0** (`LICENSE.md`)
**except** the Pro paths listed below. This includes the SQL editor, schema
explorer, saved queries, dashboards, Brain AI chat, data pipelines, models,
admin panel, the tunnel connector, the embedded web frontend, and all CLI
commands.

Brain: the chat is part of the Apache 2.0 core. Ask AI (text-to-SQL in the
editor) and Brain agentic tools (tool-calling) are gated as Pro features at
runtime. Their code currently lives in files that carry no BUSL header and are
not listed as Pro paths below (`internal/brain/`,
`internal/server/handlers/brain.go`, `internal/server/handlers/brain_sql.go`).

## Pro features — Business Source License 1.1

The following are licensed under **BSL 1.1** (`LICENSE.BSL`). Each Pro source
file carries a header:

```
// SPDX-License-Identifier: BUSL-1.1
```

BSL 1.1 in plain terms: the source is public and you may read, modify, and use it
for non-production purposes freely. **Production use of the Pro features requires
a valid CH-UI Pro license** (the Additional Use Grant in `LICENSE.BSL`). On the
Change Date, each version converts automatically to Apache 2.0.

### Pro packages (entire directory)

- `internal/governance/` — metadata catalog, policies, lineage, incidents, audit
- `internal/clusterhealth/` — operations & database health monitoring
- `internal/queryinsights/` — `system.query_log` analytics
- `internal/scheduler/` — scheduled query jobs
- `internal/alerts/` — alert rules and dispatch (SMTP/Resend/Brevo)
- `internal/github/` — GitHub model sync
- `internal/costs/` — Cost Center showback/chargeback analytics
- `internal/performance/` — regression analysis, investigations and background scans
- `internal/schemacompare/` — schema comparison and SQL review plans
- `internal/operations/` — weekly operations reports and delivery
- `internal/incidenttimeline/` — correlated operational timelines
- `internal/license/` — the commercial licensing/entitlement machinery itself
- `internal/oidc/` — SSO (OIDC) provider settings and login

### Pro files in shared packages

- `internal/server/middleware/license.go` — the Pro entitlement gate
- `internal/server/handlers/schedules.go`
- `internal/server/handlers/governance.go`
- `internal/server/handlers/governance_alerts.go`
- `internal/server/handlers/governance_auditlog.go`
- `internal/server/handlers/admin_governance.go`
- `internal/server/handlers/clusterhealth.go`
- `internal/server/handlers/queryinsights.go`
- `internal/server/handlers/costs.go`
- `internal/server/handlers/performance.go`
- `internal/server/handlers/fleet.go`
- `internal/server/handlers/schema_compare.go`
- `internal/server/handlers/operations_reports.go`
- `internal/server/handlers/incident_timeline.go`
- `internal/database/performance.go`
- `internal/database/operations_reports.go`
- `internal/database/incident_timeline.go`
- `internal/server/handlers/admin_github.go`
- `internal/server/handlers/admin_sso.go` — SSO (OIDC) admin settings
- `internal/server/handlers/auth_oidc.go` — SSO (OIDC) login flow
- `internal/mcpserver/tools_pro.go` — Pro MCP tools (Query Insights, Cost Center)

The corresponding Pro UI (the Svelte pages/components and API clients for Governance, Cluster
Health, Query Insights, Cost Center, Performance, Fleet, Schema Compare, Reports, Incident Timeline,
Schedules, Alerts, SSO settings and GitHub sync) is covered by the same BSL 1.1
terms as the Pro backend it serves. The UI files that currently carry a BUSL
header are `ui/src/pages/{Fleet,IncidentTimeline,OperationsReports,Performance,SchemaCompare}.svelte`
and `ui/src/lib/api/{fleet,incidentTimeline,operationsReports,performance,schemaCompare}.ts`.

## Buying a Pro license

For commercial licensing, evaluation licenses, or alternative arrangements,
contact **me@caioricciuti.com**.
