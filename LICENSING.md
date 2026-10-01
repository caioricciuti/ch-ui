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

## Community core: Apache License 2.0

Everything in the repository is licensed under **Apache 2.0** (`LICENSE.md`)
**except** the Pro paths listed below and the website in `site/` (see "The
website" further down). This includes the SQL editor, schema
explorer, saved queries, dashboards, Brain AI chat, data pipelines, models,
admin panel, the tunnel connector, the embedded web frontend, and all CLI
commands.

Core also includes the shared building blocks Pro features use: tunnel and
session tokens (`internal/tokens/`), cron parsing (`internal/cronexpr/`), mail
delivery through SMTP, Resend and Brevo (`internal/mail/`), and the Pro gate
that decides what is unlocked (`internal/config` ProAccess and ProGate,
`internal/server/handlers/license.go`, and the UI license store and paywall).
Wiring files that only register or switch Pro code (`internal/server/server.go`,
route setup, migrations, config fields) are core too.

## Pro features: Business Source License 1.1

The following are licensed under **BSL 1.1** (`LICENSE.BSL`). Each Pro source
file carries this header (Svelte files use `<!-- SPDX-License-Identifier: BUSL-1.1 -->`,
other frontend files the first line only):

```
// SPDX-License-Identifier: BUSL-1.1
// Copyright (C) 2024-2026 Caio Ricciuti.
// Part of CH-UI Pro. Licensed under the Business Source License 1.1 (see
// LICENSE.BSL), NOT the Apache-2.0 LICENSE that governs the rest of the repo.
```

Code published before v2.14.0 without this header was released under Apache 2.0
and remains available under Apache 2.0 in those versions.

BSL 1.1 in plain terms: the source is public and you may read, modify, and use it
for non-production purposes freely. **Production use of the Pro features requires
a valid CH-UI Pro license** (the Additional Use Grant in `LICENSE.BSL`). On the
Change Date, each version converts automatically to Apache 2.0.

### Pro packages (entire directory)

- `internal/governance/`: metadata catalog, policies, guardrails, incidents, audit
- `internal/clusterhealth/`: operations and database health monitoring
- `internal/queryinsights/`: `system.query_log` analytics
- `internal/scheduler/`: scheduled query jobs
- `internal/alerts/`: alert rules and dispatch
- `internal/github/`: GitHub model sync
- `internal/costs/`: Cost Center showback and chargeback
- `internal/performance/`: regression analysis, investigations and background scans
- `internal/schemacompare/`: schema comparison and SQL review plans
- `internal/operations/`: weekly operations reports and delivery
- `internal/incidenttimeline/`: correlated operational timelines
- `internal/license/`: license signing verification and entitlement
- `internal/oidc/`: SSO (OIDC) provider settings and login
- `internal/audit/`: audit forwarding to a SIEM (webhook, file, stdout)
- `internal/brain/tools/`: Brain agentic tools
- `internal/telemetry/monitor/`: telemetry monitors

### Pro files in shared packages

Backend:

- `internal/server/middleware/license.go`: the Pro entitlement gate middleware
- `internal/server/handlers/`: `schedules.go`, `saved_queries_run.go`, `governance.go`,
  `governance_alerts.go`, `governance_auditlog.go`, `admin_governance.go`,
  `query_guardrails.go`, `clusterhealth.go`, `queryinsights.go`, `costs.go`,
  `performance.go`, `fleet.go`, `schema_compare.go`, `operations_reports.go`,
  `incident_timeline.go`, `admin_github.go`, `admin_sso.go`, `auth_oidc.go`,
  `brain_sql.go` (Ask AI), `brain_agent.go` (agentic chat and approvals),
  `telemetry_traces.go`, `telemetry_metrics.go`, `telemetry_servicemap.go`,
  `telemetry_monitors.go`
- `internal/database/`: `schedules.go`, `alerts.go`, `costs.go`, `github_sync.go`,
  `sso.go`, `governance_settings.go`, `brain_approvals.go`, `telemetry_monitors.go`,
  `performance.go`, `operations_reports.go`, `incident_timeline.go`
- `internal/telemetry/`: `traces.go`, `metrics.go`, `servicemap.go`
- `internal/brain/tool_chat.go`: the tool-calling loop
- `internal/mcpserver/`: `tools_pro.go` (Query Insights and Cost Center tools),
  `tools_guardrails.go`

Frontend (`ui/src/`):

- Pages: `Schedules`, `Governance`, `ClusterHealth`, `QueryInsights`, `CostCenter`,
  `Performance`, `Fleet`, `SchemaCompare`, `OperationsReports`, `IncidentTimeline`
- Components: `lib/components/governance/`, the telemetry Traces, Metrics,
  Service map and Monitors components (`TracesSection`, `TraceView`,
  `TraceWaterfall`, `TraceFacets`, `TracesHistogram`, `SpanPanel`, `services.ts`,
  `MetricsSection`, `MetricPicker`, `MetricQueryCard`, `ServiceMapSection`,
  `MonitorsSection`, `MonitorEditor`, `LogTraceWaterfall`), `admin/GitHubSection`,
  `admin/SSOSettingsSection`, `editor/AskAIBar`, `brain/BrainApprovalActions`,
  `models/GitHubSyncButton`, `layout/commandPaletteSearch.pro.ts`
- API clients and types: `lib/api/{governance,alerts,clusterHealth,queryInsights,costs,github,sso,fleet,incidentTimeline,operationsReports,performance,schemaCompare,brainPro,telemetryPro}.ts`,
  `lib/types/{governance,alerts,telemetryPro}.ts`

Tests that cover only Pro code carry the same header.

## The website: not open source

`site/` holds ch-ui.com: the landing pages, the documentation and the legal
texts. It is in this repository so the docs change with the code, but it is
**not** covered by Apache 2.0 or BSL 1.1. Its text, screenshots and images, and
the CH-UI name and logo, are copyright (C) 2024-2026 Caio Ricciuti, all rights
reserved. You may read it, build it locally and send corrections; you may not
republish it or reuse it for another product.

The fonts under `site/public/fonts/` keep their own licenses, which sit beside
them.

## Buying a Pro license

For commercial licensing, evaluation licenses, or alternative arrangements,
contact **me@caioricciuti.com**.
