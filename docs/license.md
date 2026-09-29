# CH-UI Licensing

CH-UI uses a dual-license model: an Apache 2.0 open source core, plus Pro features that are source-available under the Business Source License 1.1.

---

## CH-UI Core (Community Edition)

**License:** [Apache License 2.0](../LICENSE.md)

The core of CH-UI is free and open source. This includes:

- SQL Editor (multi-tab, formatting, profiling, streaming results, query plan analysis, query history)
- Query parameters (`{name:Type}`) in the editor
- Schema Explorer (database/table/column browser, data preview)
- Saved Queries (create, edit, share, duplicate)
- Dashboards (panel builder, multiple chart types, time ranges, public dashboards, folders)
- Brain AI chat (bring your own key: OpenAI, OpenAI-compatible, Ollama; multi-chat, artifacts, skills)
- Data Pipelines (Webhook, S3, Kafka, Database sources into ClickHouse)
- Models (dbt-style SQL transformations with DAG and materialization), including model and pipeline cron schedules
- Telemetry Logs, Sources and saved searches
- MCP server (tools, write tools, OAuth, API keys)
- Admin Panel (users, roles, ClickHouse users, connections and multiple connections, tunnel agents, Brain providers and skills, retention; admin role required, not Pro)
- Command palette (actions, tables, tabs)
- Tunnel connector (`ch-ui connect`) for remote ClickHouse access
- Embedded web frontend
- All CLI commands

You can use, modify, and distribute CH-UI Core freely under the Apache 2.0 license.

## CH-UI Pro

**License:** [Business Source License 1.1](../LICENSE.BSL) (source-available; production use requires a valid CH-UI Pro license; converts to Apache 2.0 on the Change Date).

The Pro source is published in this repository under BSL 1.1 — every Pro source file carries an `SPDX-License-Identifier: BUSL-1.1` header. See [`LICENSING.md`](../LICENSING.md) for the authoritative list of Pro paths and the exact terms. All other code is Apache 2.0.

Pro features:

- Scheduled query jobs (cron, run history, timezones) and parameterized saved-query runs
- Governance: catalog, lineage, access matrix, query audit, incidents, policies, guardrails (query blocking in the editor and MCP), audit log viewer
- Alerts: channels (SMTP, Resend, Brevo), rules, delivery
- Cluster Health
- Query Insights
- Cost Center
- Performance: regressions, investigations, hourly scans
- Fleet overview
- Schema comparison
- Weekly operations reports
- Incident timeline
- Telemetry Traces, Metrics, Service map, Monitors
- Ask AI (text-to-SQL in the editor) and Brain agentic tools (tool-calling)
- GitHub sync for models
- SSO (OIDC) with per-person roles and attribution
- Audit forwarding to a SIEM (webhook, file, stdout)
- MCP Pro tools: `query_insights_top`, `costs_summary`
- Command palette Pro search (entity search, scope prefixes)

Pro features require a valid license file. Licenses are per-deployment and include a customer name and an expiration date. A license is all-or-nothing: an active Pro license unlocks every Pro feature; there are no per-feature entitlements.

### How to activate

1. Open CH-UI in your browser as an admin (activating and removing a license is admin only)
2. Go to **Settings > License**
3. Paste or upload your license file
4. Pro features unlock immediately

### How to get a license

Visit [ch-ui.com/pricing](https://ch-ui.com/pricing) or contact **me@caioricciuti.com**.

## License boundary

The licensing boundary is enforced server-side via HTTP 402 middleware on Pro-only routes:

- **Free routes:** queries, saved queries, dashboards, pipelines, models, Brain chat, telemetry logs, MCP, admin, connections
- **Pro routes:** `/api/schedules/*`, `/api/governance/*` (including alerts), `/api/cluster-health/*`, `/api/query-insights/*`, `/api/costs/*`, `/api/performance/*`, `/api/fleet/*`, `/api/schema-compare/*`, `/api/operations-reports/*`, `/api/incident-timeline/*`, the Pro telemetry routes (traces, metrics, service map, monitors), and `POST /api/saved-queries/{id}/run`
- **Pro inside free routes:** Ask AI, Brain agentic tools, guardrails, SSO and GitHub sync are checked in their handlers

The Pro license check is enforced both server-side (HTTP 402 middleware) and client-side (UI gate).

## FAQ

**Can I use CH-UI Core in production?**
Yes, freely. Apache 2.0 allows commercial use.

**Can I modify CH-UI Core?**
Yes. You must retain the copyright notice and license.

**Do I need Pro for dashboards, Brain, or pipelines?**
No. Dashboards, Brain AI chat, data pipelines, models (including their cron schedules), telemetry logs, the MCP server, and admin are all free. Ask AI and Brain agentic tools are Pro.

**What features require Pro?**
The list under [CH-UI Pro](#ch-ui-pro): scheduled query jobs, governance and guardrails, alerts, Cluster Health, Query Insights, Cost Center, performance, fleet, schema comparison, weekly reports, incident timeline, telemetry traces, metrics and monitors, Ask AI and agentic tools, GitHub sync, SSO, SIEM audit forwarding, MCP Pro tools, and command palette Pro search.

**What happens when a Pro license expires?**
For 14 days after the expiry date, Pro pages stay open read-only: reads keep working and changes are refused with HTTP 402 until a renewed license is activated. Enabled schedules and monitors keep running during these 14 days. After that, Pro features are locked, and enabled schedules and monitors pause. Nothing is deleted: they resume when a license is activated. Core features keep working throughout, and your data is never lost. Details: [ch-ui.com/docs/license](https://ch-ui.com/docs/license).
