# CH-UI Licensing

CH-UI uses a dual-license model: open source core + commercial Pro modules.

---

## CH-UI Core (Community Edition)

**License:** [Apache License 2.0](../LICENSE)

The core of CH-UI is free and open source. This includes:

- SQL Editor (multi-tab, formatting, profiling, streaming results, query plan analysis)
- Schema Explorer (database/table/column browser, data preview)
- Saved Queries
- Dashboards (panel builder, multiple chart types, time ranges)
- Brain AI Assistant (OpenAI, OpenAI-compatible, Ollama — multi-chat, artifacts, skills)
- Data Pipelines (Webhook, S3, Kafka, Database sources into ClickHouse)
- Models (dbt-style SQL transformations with DAG and materialization)
- Admin Panel (user management, connection management, provider configuration)
- Multi-connection management
- Tunnel connector (`ch-ui connect`) for remote ClickHouse access
- Embedded web frontend
- All CLI commands

You can use, modify, and distribute CH-UI Core freely under the Apache 2.0 license.

## CH-UI Pro

**License:** Commercial (proprietary)

Pro modules extend CH-UI with enterprise features:

- Scheduled query jobs (cron-based scheduling, execution history, timezone support)
- Governance (metadata sync, query log analytics, data lineage, access matrix, tagging)
- Policies and incident management (violation detection, incident workflow, severity tracking)
- Alerting (SMTP, Resend, Brevo — rules by event type/severity, escalation)

Pro features require a valid license file. Licenses are per-deployment and include a customer name, expiration date, and feature set.

### How to activate

1. Open CH-UI in your browser
2. Go to **Settings > License**
3. Paste or upload your license file
4. Pro features unlock immediately

### How to get a license

Visit [ch-ui.com/pricing](https://ch-ui.com/pricing) or contact **c.ricciuti@ch-ui.com**.

## License boundary

The licensing boundary is enforced server-side via HTTP 402 middleware on Pro-only routes:

- **Free routes:** queries, saved queries, dashboards, pipelines, models, brain, admin, connections
- **Pro routes:** `/api/schedules/*`, `/api/governance/*` (including alerts)

The Pro license check is enforced both server-side (HTTP 402 middleware) and client-side (UI gate).

## FAQ

**Can I use CH-UI Core in production?**
Yes, freely. Apache 2.0 allows commercial use.

**Can I modify CH-UI Core?**
Yes. You must retain the copyright notice and license.

**Do I need Pro for dashboards, Brain, or pipelines?**
No. Dashboards, Brain AI, data pipelines, models, and admin are all free.

**What features require Pro?**
Only scheduled query jobs, governance (lineage, policies, incidents, access matrix), and alerting.

**What happens when a Pro license expires?**
Pro features become locked. Core features continue working. Your data is never lost.
