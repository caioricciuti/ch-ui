---
title: Plans & Licensing
description: "How CH-UI is licensed: open-source core (Apache-2.0) and the Pro License for self-hosted deployments."
---

CH-UI is **self-hosted software**: an open-source core you run on your own infrastructure, optionally unlocked to Pro with a license file.

## Open Source + Pro License

CH-UI comes in two tiers.

### Open Source (Apache-2.0): free

The CH-UI core is open source under the [Apache License 2.0](https://github.com/caioricciuti/ch-ui/blob/main/LICENSE.md). You can use, modify, and distribute it freely, including commercially (retain the copyright and license notice).

Open Source includes the full workspace, running on your own infrastructure:

- SQL editor with streaming results, query history, formatting, `EXPLAIN`, query profile, result filtering, and error navigation
- [Query parameters](/docs/querying#query-parameters) (`{name:Type}`) in the editor
- Database explorer and saved queries (create, edit, share, duplicate)
- Dashboards, public dashboards, and dashboard folders
- Pipelines, and models including cron schedules for models and pipelines
- Brain AI chat (bring your own key), with skills and artifacts
- Telemetry logs, sources, and saved searches
- The MCP server for AI clients (tools, write tools, OAuth, API keys)
- The admin panel: users, roles, ClickHouse users, connections (including multiple connections), tunnel agents, Brain providers and skills, retention. These need the admin role, not Pro.
- The command palette (actions, tables, tabs) and the tunnel connector

 See the **[Installation](/docs/installation)** and **[Deployment](/docs/deployment)** guides.

> CH-UI stores its state in a local **SQLite** database, so there is no external database to operate.

### Pro License: $1,199/year per instance

The **Pro License** unlocks the full Pro module set on top of the Open Source core:

- **[Query Insights](/docs/query-insights)**: `system.query_log` analytics: latency percentiles, slow and memory-heavy query patterns, failures, users, hot tables
- **[Cluster Health](/docs/cluster-health)**: per-node replication, merges, mutations, parts pressure, Keeper, and backups monitoring
- **[Cost Center](/docs/cost-center)**: showback/chargeback analytics: compute and storage costs priced from `system.query_log` and `system.parts`, with team attribution and CSV export
- **[Performance](/docs/performance)**: regression scans across equal time windows, saved before/after investigations, and an admin fleet overview
- **[Schema comparison](/docs/schema-compare)**: compare metadata across environments and download a commented SQL review plan that never runs on its own
- **[Incident timeline](/docs/incident-timeline)**: query failures, latency, merges, mutations, health and deployment annotations on one timeline
- **[Weekly operations reports](/docs/operations-reports)**: saved weekly summaries, optionally emailed through your alert channels
- **[Telemetry depth](/docs/telemetry)**: traces with waterfalls, the metrics explorer, the service map, and monitors that raise alerts (logs stay in the open-source core)
- **[MCP Pro tools](/docs/mcp#tools)**: `query_insights_top` and `costs_summary` for AI clients (the MCP server itself is in the core)
- **[Governance](/docs/governance)**: catalog, access matrix, query audit, incidents, policies, guardrails (query blocking in the editor and MCP), and the audit log viewer
- **[Alerts](/docs/alerts)**: channels (SMTP, Resend, Brevo), rules, and delivery
- **[Scheduled query jobs](/docs/schedules)**: cron-driven query runs with run history and timezones, plus the parameterized saved-query Run API (model and pipeline schedules stay in the open-source core)
- **[Ask AI and Brain agentic tools](/docs/brain)**: text-to-SQL in the editor grounded in your live schema, and the tool-calling agent loop (still bring-your-own AI key)
- **[GitHub sync](/docs/github)**: keep models in version control
- **[Single Sign-On](/docs/sso)**: OIDC SSO (Okta, Entra ID, Google, Keycloak) with per-person roles and attribution
- **[Audit forwarding](/docs/monitoring)**: stream audit events to your SIEM (webhook, file, or stdout)
- **Command palette Pro search**: entity search and scope prefixes

Pricing is **flat per instance**: no per-seat math, regardless of how many people use the instance. Buy self-serve on the **[pricing page](/pricing)**. Checkout is handled by Stripe (annual subscription), and your signed `license.json` arrives by email right after payment. Or [start a **30-day free trial**](/license) first, no card required.

The Pro modules' source is published under the **Business Source License 1.1**
(BSL 1.1). It is source-available; production use requires a valid Pro license, and
each version converts to Apache-2.0 on its Change Date. Every Pro source file
carries an `SPDX-License-Identifier: BUSL-1.1` header; the open-source core
remains Apache-2.0. See `LICENSING.md` in the repository for the exact scope.

### How activation works

You receive a **signed license file** (JSON) and an admin activates it on your instance's **Settings → License** page: paste the JSON or upload the file. Activating, replacing, and removing a license are admin only. Verification happens **locally** against a public key embedded in the binary:

- **No call-home**: works in air-gapped environments
- **No account required**: the license is the whole relationship
- Licenses are per deployment: each carries a customer name and an expiry date, and one license covers one running instance
- The license is all-or-nothing: it unlocks every Pro feature. A license with the `enterprise` edition unlocks exactly what `pro` does.

Activating or deactivating a license takes effect right away, without a page
reload or a restart. That covers every Pro feature: Pro pages in the open
browser tab, and background work such as Cluster Health collection and audit
forwarding.

To move from a trial to a paid license, or to apply a renewed or upgraded one,
use **Replace license** in Settings, License (shown while a license is active).
The new license replaces the current one; there is no need to deactivate first.

### The Settings page

**Settings** has three tabs: License, Instance and Legal.

**License** shows the edition, customer, license ID and expiry, and a status badge: Pro Active, Pro Grace Period, Pro Expired or Community. Below that:

- **Start free trial** (Community only): enter an email, and optionally a name, and CH-UI asks the license server for a 30-day Pro trial. One trial per email. The license is emailed to you; paste it or upload the file to activate. Only admins can start a trial.
- **Activate Pro** or **Replace license** (admins): paste the license JSON or upload the file. **Buy Pro License** opens Stripe checkout in a new tab.
- **Manage license & billing**: **Resend license email** re-sends your license file to the email used at purchase or trial, and **Manage billing** emails you a link to the Stripe billing portal for your subscription.

The trial, checkout, resend and billing requests go from your browser to `https://license.ch-ui.com`, so they need internet access from the browser. Activation itself stays offline.

**Instance** shows who you are connected as, your role, the current connection and the CH-UI version. **Legal** summarizes which parts are Apache-2.0 and which are BSL 1.1, with links to the license texts, terms and privacy policy.

### License as configuration (Kubernetes, Terraform)

Since v2.6.1 you can supply the license through the environment instead of the
UI, which fits config-as-code deploys and secret managers:

- `CHUI_LICENSE_FILE`: path to a file containing the license JSON (mount your
  Kubernetes Secret and point this at it). Takes precedence.
- `CHUI_LICENSE`: the license JSON inline.

The Helm chart wires this up for you:

```bash
kubectl create secret generic ch-ui-license --from-file=license.json
helm upgrade ch-ui ./deploy/helm/ch-ui --set license.existingSecret=ch-ui-license
```

An environment-supplied license takes precedence over one activated in the UI.
If the file is missing or the license is invalid, CH-UI logs a warning and
starts normally with whatever license the database holds. Rotating the secret
takes effect on the next restart.

### What happens at expiry

A license never hard-locks your instance in the middle of an incident. When it
expires, Pro features enter a **14-day read-only grace period**, counted from
the expiry date, giving you time to renew:

- Pro pages still open, with a warning bar at the top: "Pro license expired.
  Read-only until *date*. Changes are blocked until a renewed license is
  activated." The **Manage license** button in that bar opens Settings, License.
- Reads keep working, so dashboards and monitoring stay usable. The server
  answers `GET` and `HEAD` on Pro endpoints as before, along with the
  read-only `POST` searches (for example trace search, the metrics query, and
  the service map). Every other write is refused with `402 Payment Required`
  (`"status": "grace"` in the JSON body). Pro responses carry the header
  `X-CH-UI-License-Status: grace`.
- Enabled scheduled queries and telemetry monitors keep running during grace.
- Pro features outside the Pro pages stay locked until a license is active
  again: SSO settings, GitHub sync, Ask AI in the editor, and Pro search in
  the command palette.
- Settings, License shows a **Pro Grace Period** badge with the expiry date and
  the date the grace period ends.

After the grace window, Pro features lock until a valid license is activated.
Enabled scheduled queries and telemetry monitors pause; nothing is deleted, and
they resume when a license is activated. Activating a renewed license, during
grace or after, unlocks Pro right away, no reload needed.

## Open Source vs Pro License

| | Open Source | Pro License |
|---|---|---|
| Setup | Run the binary (SQLite) | Run the binary + upload license |
| SQL editor, query parameters, saved queries, dashboards | Included | Included |
| Pipelines, models, model and pipeline schedules | Included | Included |
| Telemetry logs, sources, saved searches | Included | Included |
| MCP server | Included | Included, plus `query_insights_top` and `costs_summary` |
| Admin panel, multiple connections (admin role) | Included | Included |
| Brain (AI chat) | BYO key | BYO key |
| Ask AI (text-to-SQL) + Brain agentic tools | Not included | **Included** (BYO key) |
| Scheduled query jobs + parameterized saved-query runs | Not included | **Included** |
| Governance (incl. guardrails) + Alerts | Not included | **Included** |
| Query Insights + Cluster Health + Cost Center + Performance | Not included | **Included** |
| Fleet overview, schema comparison, weekly reports, incident timeline | Not included | **Included** |
| Telemetry traces, metrics, service map, monitors | Not included | **Included** |
| SSO + GitHub sync + Audit forwarding | Not included | **Included** |
| Command palette Pro search | Not included | **Included** |
| Cost | Free | $1,199/year per instance |
| License | Apache-2.0 | Apache-2.0 core + BSL 1.1 for Pro modules |

## License boundary

The server enforces the split, not the UI. A Pro route called without a license answers `402 Payment Required`; during the grace period reads still work, as described above.

**Free (no license)**: everything else under `/api`, including auth, query execution and history, connections, saved queries (except the run endpoint below), dashboards and public dashboards, pipelines, models, Brain chat, telemetry logs, sources and saved searches, MCP keys, and the admin panel.

**Pro route prefixes** (the whole prefix is gated):

- `/api/schedules`
- `/api/governance`
- `/api/cluster-health`
- `/api/query-insights`
- `/api/costs`
- `/api/performance`
- `/api/fleet`
- `/api/schema-compare`
- `/api/operations-reports`
- `/api/incident-timeline`

**Pro routes inside free prefixes**:

- Telemetry traces, metrics, the service map and monitors under `/api/telemetry` (`/traces/...`, `/metrics/...`, `/service-map`, `/monitors`)
- `POST /api/saved-queries/{id}/run`, the parameterized saved-query run API

**Checks inside handlers** (the route is free, the Pro part is checked when it runs). Unless noted, these need an active license and are off during the grace period:

- **Ask AI**: text-to-SQL in the editor returns `402` without a license
- **Brain agentic tools**: without a license Brain still answers as plain chat, without calling tools
- **Guardrails**: query policies are only enforced with a license; without one, queries run unchecked
- **SSO**: reading or saving the SSO settings returns `402`. The OIDC login and callback also return `402` without a license, but keep working during grace so people can still sign in
- **GitHub sync**: saving, removing, testing and syncing the integration, and the push webhook, return `402`
- **Governance background sync**: turning it on in Governance, Settings returns `402` without a license (allowed during grace); turning it off always works

Nothing in the free tier is limited by count: no caps on users, connections, dashboards or queries.

## FAQ

**Do I need a license file?** Only for the Pro modules: Query Insights, Cluster Health, Cost Center, Performance, fleet overview, schema comparison, weekly reports, incident timeline, Governance, Alerts, scheduled query jobs and parameterized saved-query runs, telemetry traces, metrics, service map and monitors, Ask AI and Brain agentic tools, GitHub sync, SSO, audit forwarding, the MCP Pro tools, and command palette Pro search. The Open Source tier is fully unlocked otherwise: Apache-2.0, no key, no activation. That includes query parameters, model schedules, multiple connections, and the admin panel.

**Is there a separate Enterprise edition?** No. A license with the `enterprise` edition unlocks the same features as a Pro license. Enterprise differs in commercial terms (multiple instances, invoicing, custom terms), not in features.

**Does the license phone home?** No. Activation is a signed file verified locally, so air-gapped deployments work fine.

**What does the $1,199/year buy me?** The full Pro module set on your own deployment, plus email support, with your data never leaving your infrastructure.

**Can I try before buying?** Yes, [start a 30-day free trial](/license). You get a time-limited license that unlocks every Pro module; no card required.

**Can I use the open-source core in production?** Yes, Apache-2.0 permits commercial use.

Questions or Enterprise terms: use the **[contact form](/license)** or email **me@caioricciuti.com**.
