// Content for the /vs/* comparison pages. Facts checked August 2026 against
// each project's own site/repo (pricing pages, GitHub activity, plugin docs).
// Keep claims honest and dated; these pages only work if they stay accurate.

export interface ComparisonRow {
  aspect: string;
  competitor: string;
  chui: string;
}

export interface ComparisonFaqItem {
  question: string;
  answer: string;
}

export interface ComparisonData {
  /** URL slug under /vs/, also used in data-cta tags (e.g. "tabix"). */
  slug: string;
  /** Competitor display name (e.g. "Tabix"). */
  name: string;
  /** <title> / OG title. */
  metaTitle: string;
  /** Meta description. */
  metaDescription: string;
  /** One-paragraph verdict under the H1. */
  verdict: string;
  /** "Choose <name> if…" bullets. */
  chooseCompetitor: string[];
  /** "Choose CH-UI if…" bullets. */
  chooseChui: string[];
  rows: ComparisonRow[];
  faq: ComparisonFaqItem[];
}

const tabixComparison: ComparisonData = {
  slug: 'tabix',
  name: 'Tabix',
  metaTitle: 'CH-UI vs Tabix for ClickHouse: honest comparison',
  metaDescription:
    'Tabix is a zero-install browser UI for ClickHouse; CH-UI is a self-hosted workspace with dashboards, pipelines, cluster health, query insights, and an AI copilot. An honest side-by-side.',
  verdict:
    'Tabix is the classic lightweight ClickHouse web client: open a browser tab, point it at your server, run SQL. It needs no backend and no install, and for a quick look at a database that is genuinely hard to beat. The trade-off is that the project has been effectively dormant since mid-2022, and it stops at querying and basic charts. CH-UI is a self-hosted workspace that covers the rest of the job: dashboards you can share, ingestion pipelines, dbt-style models, cluster health, query-log analytics, cost showback, governance, and an AI copilot, at the cost of actually running a (single) binary.',
  chooseCompetitor: [
    'You want zero installation: a static web page pointed straight at ClickHouse HTTP',
    'You need a quick, throwaway SQL console for a dev or demo server',
    'You have no place (or no desire) to run any extra service at all',
    'Free and Apache-2.0 is a hard requirement and querying is all you need',
  ],
  chooseChui: [
    'You want dashboards, pipelines, models, and alerts around the same ClickHouse, not just a SQL console',
    'You need cluster-level visibility: replication, merges, mutations, Keeper, query-log analytics, cost',
    'Multiple people use the workspace and you care about auth, audit, and governance',
    'You want a maintained project with active releases (v2.11.0 shipped September 2026)',
  ],
  rows: [
    {
      aspect: 'Deployment',
      competitor:
        'Static browser app. Use the hosted page or serve the files yourself; talks directly to ClickHouse HTTP from the browser. Nothing to run server-side.',
      chui: 'One self-hosted binary (or Docker/Helm) with state in a local SQLite file. Connects directly or over an outbound tunnel, so ClickHouse needs no inbound exposure.',
    },
    {
      aspect: 'Maintenance status',
      competitor:
        'Effectively unmaintained: the last commit to tabixio/tabix was May 2022. It still works, but do not expect fixes or new ClickHouse feature support.',
      chui: 'Actively developed; regular releases (v2.11.0 in September 2026), public changelog, signed release artifacts.',
    },
    {
      aspect: 'SQL editing',
      competitor:
        'Solid SQL editor with autocomplete for fields/functions/dictionaries, query formatting, and a process list with KILL QUERY.',
      chui: 'SQL workspace with streaming results, sort and filter on results, automatic query history, ClickHouse error parsing with jump-to-error, and Ask AI query generation (Pro).',
    },
    {
      aspect: 'Dashboards',
      competitor: 'Inline charts and simple drawn dashboards for query results.',
      chui: 'Dashboard builder with shareable public links (rate-limited, sanitized), scheduled refresh, and alerting on top (Pro).',
    },
    {
      aspect: 'ClickHouse operations',
      competitor: 'Realtime metrics view from system.metrics / system.events, process list.',
      chui: 'Cluster Health (per-node replication, merges, mutations, parts, Keeper, backups) and Query Insights (query_log latency percentiles, slow patterns, failures, hot tables) as Pro modules.',
    },
    {
      aspect: 'Cost visibility',
      competitor: 'None.',
      chui: 'Cost Center (Pro, v2.7.0): showback/chargeback from query_log CPU time and parts storage, team attribution, CSV export.',
    },
    {
      aspect: 'Ingestion & modeling',
      competitor: 'None, query-only.',
      chui: 'Pipelines (Kafka, S3, webhooks, databases) and dbt-style SQL models with scheduling, plus GitHub sync (Pro).',
    },
    {
      aspect: 'AI',
      competitor: 'None.',
      chui: 'Brain AI chat, bring-your-own-key, your provider, your data path; in-editor Ask AI and agentic tools are Pro. Plus an embedded MCP server, so Claude, ChatGPT or Cursor query through CH-UI with read-only guardrails and an audit trail.',
    },
    {
      aspect: 'Auth & governance',
      competitor:
        'Credentials live in the browser; no workspace users, audit, or roles beyond what ClickHouse itself enforces.',
      chui: 'Workspace users with roles, SSO/OIDC (Pro), immutable audit log, governance catalog and guardrails (Pro).',
    },
    {
      aspect: 'Licensing & price',
      competitor: 'Free, Apache-2.0.',
      chui: 'Core free, Apache-2.0. Pro modules (BSL, source-available) at $1,199/year flat per instance, never per seat.',
    },
  ],
  faq: [
    {
      question: 'Is Tabix still maintained?',
      answer:
        'The tabixio/tabix repository has had no commits since May 2022. It still works for basic querying against current ClickHouse versions, but new ClickHouse features, bug fixes, and security updates are not arriving. CH-UI ships regular releases; v2.11.0 landed in September 2026.',
    },
    {
      question: 'Tabix needs no server. Does CH-UI?',
      answer:
        'Yes, one. CH-UI is a single self-hosted binary (also available as Docker image and Helm chart) with all state in a local SQLite file and no external database or services to operate. Tabix runs entirely in the browser, which is genuinely simpler if a SQL console is all you need.',
    },
    {
      question: 'Is CH-UI free like Tabix?',
      answer:
        'The core is free and Apache-2.0: SQL workspace, dashboards, pipelines, models, Brain AI chat (bring your own key), the MCP server for AI clients, and the OpenTelemetry log explorer. Pro modules (Cluster Health, Query Insights, Cost Center, Governance, scheduled query jobs, Alerts, OpenTelemetry traces and metrics, Ask AI and Brain agentic tools, SSO) cost $1,199/year flat per instance, with a 30-day free trial.',
    },
    {
      question: 'Can I migrate from Tabix to CH-UI?',
      answer:
        'There is nothing to migrate: Tabix stores no server-side state. Install CH-UI, add your ClickHouse connection (direct URL or outbound tunnel), and your databases, tables, and data appear as-is.',
    },
  ],
};

const dbeaverComparison: ComparisonData = {
  slug: 'dbeaver',
  name: 'DBeaver',
  metaTitle: 'CH-UI vs DBeaver for ClickHouse: honest comparison',
  metaDescription:
    'DBeaver is a superb multi-database desktop client; CH-UI is a shared, self-hosted ClickHouse workspace. Which fits your team? An honest side-by-side on deployment, features, and price.',
  verdict:
    'DBeaver is one of the best desktop database clients ever made: 80+ databases over JDBC, a mature SQL editor, ER diagrams, and serious data import/export, with a free Apache-2.0 Community edition that connects to ClickHouse out of the box. If your day spans Postgres, MySQL, and ClickHouse from one laptop, keep DBeaver. CH-UI answers a different question: a shared, browser-based workspace that lives next to your ClickHouse (team dashboards, pipelines, models, cluster health, query-log analytics, and cost showback) instead of a per-person desktop install.',
  chooseCompetitor: [
    'You work across many databases (Postgres, MySQL, Oracle, MongoDB, …) and want one desktop tool for all of them',
    'You need heavyweight desktop features: ER diagrams, data compare, cross-database migration and export',
    'You prefer a local client per engineer over a shared web workspace',
    'The free Community edition covers you: it includes the ClickHouse JDBC driver',
  ],
  chooseChui: [
    'ClickHouse is the center of gravity and you want a tool built only for it',
    'The whole team should share one URL: dashboards, saved queries, and pipelines in one place, not per-laptop config',
    'You want ClickHouse ops built in: cluster health, query_log analytics, cost showback',
    'You need workspace-level auth, SSO, and an audit trail of who ran what',
  ],
  rows: [
    {
      aspect: 'Deployment',
      competitor:
        'Desktop application installed per user (Windows/macOS/Linux); CloudBeaver exists as a separate web product.',
      chui: 'One self-hosted binary (Docker/Helm available) serving a web workspace for the whole team; state in local SQLite.',
    },
    {
      aspect: 'Database breadth',
      competitor:
        'Exceptional: 80+ databases via JDBC, including ClickHouse in the free Community edition. The clear winner if you juggle many engines.',
      chui: 'ClickHouse only, on purpose. Depth over breadth.',
    },
    {
      aspect: 'SQL editing',
      competitor:
        'Mature editor: autocomplete, formatting, execution plans, scripts, refined over a decade.',
      chui: 'ClickHouse-tuned workspace: streaming results, result sort/filter, automatic history, ClickHouse error parsing with jump-to-error, Ask AI (Pro).',
    },
    {
      aspect: 'Dashboards & sharing',
      competitor:
        'Charts and dashboards exist in the PRO editions, but they are per-desktop. There is no URL a teammate can open.',
      chui: 'Web dashboards with shareable public links; alerts and scheduled runs on top (Pro).',
    },
    {
      aspect: 'ClickHouse operations',
      competitor:
        'Generic database admin over JDBC: sessions, metadata. No ClickHouse-specific replication/merges/Keeper views.',
      chui: 'Cluster Health and Query Insights (Pro): per-node replication, merges, mutations, parts, Keeper, backups, and query_log analytics.',
    },
    {
      aspect: 'Cost visibility',
      competitor: 'None.',
      chui: 'Cost Center (Pro): compute and storage showback priced from system tables, per team and per user, CSV export.',
    },
    {
      aspect: 'Ingestion & modeling',
      competitor:
        'Strong data transfer/import-export tooling between databases and files, a real DBeaver strength.',
      chui: 'Continuous pipelines (Kafka, S3, webhooks, databases) plus dbt-style models with schedules, plus GitHub sync (Pro).',
    },
    {
      aspect: 'AI',
      competitor: 'AI assistant available in paid PRO editions.',
      chui: 'Brain AI chat with your own provider key, in core with no license needed; Ask AI and agentic tools are Pro.',
    },
    {
      aspect: 'Auth & governance',
      competitor:
        'Local tool: credentials per user, no shared audit trail. Team/server features live in the separate Team Edition and CloudBeaver products.',
      chui: 'Workspace users and roles, SSO/OIDC (Pro), immutable audit log, governance catalog and guardrails (Pro).',
    },
    {
      aspect: 'Licensing & price',
      competitor:
        'Community edition free (Apache-2.0). PRO desktop editions are per user per year, roughly $110 (Lite) to $500 (Ultimate); team/web products priced separately.',
      chui: 'Core free (Apache-2.0). Pro is $1,199/year flat per instance, unlimited users. A team of 12 pays the same as a team of 2.',
    },
  ],
  faq: [
    {
      question: 'Does DBeaver support ClickHouse for free?',
      answer:
        'Yes. DBeaver Community is free, Apache-2.0, and ships a ClickHouse JDBC driver, so basic browsing and SQL work without paying. PRO features (advanced editors, AI assistant, dashboards, more drivers) are per-user subscriptions, roughly $110 to $500 per user per year depending on edition.',
    },
    {
      question: 'Is CH-UI a replacement for DBeaver?',
      answer:
        'Only for the ClickHouse part of your work. CH-UI does not connect to Postgres, Oracle, or MongoDB and does not try to. Many teams keep DBeaver for multi-database desktop work and run CH-UI as the shared ClickHouse workspace for dashboards, pipelines, and monitoring.',
    },
    {
      question: 'How does pricing compare for a team?',
      answer:
        'DBeaver PRO is licensed per user per year; ten engineers on Enterprise-level desktop licenses adds up linearly. CH-UI Pro is $1,199/year flat per instance regardless of how many people use it, and the Apache-2.0 core is free for unlimited users.',
    },
    {
      question: 'Can both be used side by side?',
      answer:
        'Yes, and it is common. They do not conflict: DBeaver talks JDBC straight to ClickHouse, CH-UI runs as its own service with its own connection. Nothing about CH-UI changes your ClickHouse server.',
    },
  ],
};

const metabaseComparison: ComparisonData = {
  slug: 'metabase',
  name: 'Metabase',
  metaTitle: 'CH-UI vs Metabase for ClickHouse: honest comparison',
  metaDescription:
    'Metabase is self-serve BI for the whole company; CH-UI is a ClickHouse-native workspace for the team that runs it. An honest comparison of features, deployment, and pricing.',
  verdict:
    'Metabase is the tool you hand to people who will never write SQL: a visual question builder, friendly dashboards, embedding, and an official ClickHouse driver, free to self-host under AGPL. If the goal is company-wide self-serve BI, Metabase is the better fit and this page will not pretend otherwise. CH-UI is built for the other side of the same table: the data and platform engineers who run ClickHouse: SQL-first workspace, ingestion pipelines, dbt-style models, cluster health, query-log analytics, and cost showback that BI tools do not attempt.',
  chooseCompetitor: [
    'Non-technical stakeholders need to answer their own questions without SQL',
    'You want polished, embeddable BI dashboards across several databases, not just ClickHouse',
    'A visual query builder and a curated semantic layer matter more than raw SQL power',
    'The free AGPL self-hosted edition covers your needs',
  ],
  chooseChui: [
    'Your users are the engineers and analysts who live in SQL and run ClickHouse',
    'You also need ingestion, modeling, scheduling, and alerting around ClickHouse, not just charts on top',
    'You want operational depth: replication/merges/Keeper health, query_log analytics, cost showback',
    'Per-user pricing rubs you wrong; CH-UI Pro is flat per instance',
  ],
  rows: [
    {
      aspect: 'Primary audience',
      competitor: 'Business users and analysts across the whole company: self-serve BI.',
      chui: 'The team that runs and develops on ClickHouse: engineers and SQL-first analysts.',
    },
    {
      aspect: 'Deployment',
      competitor:
        'Self-host the JVM app (JAR/Docker; a production app database like Postgres is recommended) or use Metabase Cloud.',
      chui: 'One binary, state in local SQLite, no external services. Docker and Helm available. Self-hosted only.',
    },
    {
      aspect: 'ClickHouse support',
      competitor:
        'Official ClickHouse driver, community-maintained with ClickHouse, now shipped in the main Metabase repo. Solid for querying; ClickHouse is one of 20+ supported sources.',
      chui: 'ClickHouse is the whole product: native client, system-table integrations, ClickHouse error parsing.',
    },
    {
      aspect: 'Querying',
      competitor: 'Visual question builder plus a capable native SQL editor with variables.',
      chui: 'SQL-first workspace: streaming results, filters, automatic history, jump-to-error, Ask AI (Pro).',
    },
    {
      aspect: 'Dashboards',
      competitor:
        'Excellent: interactive filters, drill-through, subscriptions, static and interactive embedding. The stronger pure-BI experience.',
      chui: 'Solid dashboards with public share links and alerting, built for engineering visibility, not embedded customer-facing BI.',
    },
    {
      aspect: 'ClickHouse operations',
      competitor: 'None. Metabase reads your data, it does not manage or monitor ClickHouse.',
      chui: 'Cluster Health, Query Insights, and Cost Center (Pro) built on system tables.',
    },
    {
      aspect: 'Ingestion & modeling',
      competitor:
        'Models/semantic layer for BI curation; no data ingestion, so data must already be in the warehouse.',
      chui: 'Pipelines (Kafka, S3, webhooks, databases) and dbt-style SQL models with schedules, plus GitHub sync (Pro).',
    },
    {
      aspect: 'AI',
      competitor: 'Metabot AI features are part of paid plans.',
      chui: 'Brain copilot in core with your own provider key; Ask AI in the editor (Pro).',
    },
    {
      aspect: 'Governance & SSO',
      competitor:
        'Row/column-level permissions, sandboxing, SSO, on Pro/Enterprise plans.',
      chui: 'Roles, immutable audit log, governance catalog and guardrails, SSO/OIDC (Pro). Query permissions stay with ClickHouse grants.',
    },
    {
      aspect: 'Licensing & price',
      competitor:
        'Open Source free (AGPL). Starter cloud from $100/month + $6 per user; Pro (cloud or self-hosted) from $575/month + $12 per user; Enterprise from about $20,000/year.',
      chui: 'Core free (Apache-2.0). Pro $1,199/year flat per instance, unlimited users, offline license activation.',
    },
  ],
  faq: [
    {
      question: 'Is Metabase good with ClickHouse?',
      answer:
        'Yes. The ClickHouse driver is official (it was developed with ClickHouse and now ships in the main Metabase repository) and works in both self-hosted and cloud Metabase. For BI-style querying and dashboards on ClickHouse data it is a solid choice.',
    },
    {
      question: 'Do I have to pick one?',
      answer:
        'No, and pairing them is a sensible architecture: CH-UI for the engineers running ClickHouse (ingestion, modeling, monitoring, cost) and Metabase for company-wide self-serve dashboards on the resulting tables. They read the same database and do not interfere.',
    },
    {
      question: 'How do the free self-hosted editions compare?',
      answer:
        'Metabase Open Source is AGPL and free with unlimited users; it includes the question builder and dashboards but not SSO or row-level permissions. CH-UI core is Apache-2.0 and free with unlimited users; it includes the SQL workspace, dashboards, pipelines, models, Brain AI chat, and the OTel log explorer, with Pro adding governance, monitoring, cost, and SSO.',
    },
    {
      question: 'What does Pro cost on each side?',
      answer:
        'Metabase Pro is $575/month (about $6,210/year) plus $12 per user per month beyond the first ten, self-hosted or cloud. CH-UI Pro is $1,199/year flat per instance with no per-user charge, activated offline with a signed license file.',
    },
  ],
};

const grafanaComparison: ComparisonData = {
  slug: 'grafana',
  name: 'Grafana',
  metaTitle: 'CH-UI vs Grafana for ClickHouse: honest comparison',
  metaDescription:
    'Grafana is the observability dashboard standard with an official ClickHouse plugin; CH-UI is a full ClickHouse workspace. An honest look at where each one wins.',
  verdict:
    'Grafana is the default answer for observability dashboards, and its official ClickHouse data source (maintained by Grafana Labs) is good: SQL and visual query builders, time series, logs, traces, and Grafana alerting on top. If you already run Grafana for metrics and want ClickHouse panels next to Prometheus ones, that is a strong setup. CH-UI is not trying to be your metrics wall. It is the workspace for developing on and operating ClickHouse itself: SQL editor, pipelines, models, governance, replication and Keeper health, query-log analytics, and cost showback. Fittingly, CH-UI exposes a Prometheus /metrics endpoint you can scrape into Grafana.',
  chooseCompetitor: [
    'You want ClickHouse panels alongside Prometheus, Loki, and the rest of your observability stack',
    'Dashboard-as-code, a huge plugin ecosystem, and a mature alerting/on-call toolchain matter',
    'Ops teams already live in Grafana and one more data source is the path of least resistance',
    'The free OSS edition or Grafana Cloud free tier covers your dashboard needs',
  ],
  chooseChui: [
    'You need a place to develop on ClickHouse (SQL workspace, saved queries, models, pipelines), not only to chart it',
    'You want ClickHouse-specific operations out of the box: replication, merges, mutations, parts, Keeper, backups',
    'Query-log analytics and cost showback matter and you would rather not build those dashboards by hand',
    'You want table management, ingestion, and governance in the same tool',
  ],
  rows: [
    {
      aspect: 'Product focus',
      competitor:
        'General-purpose observability and dashboarding across dozens of data sources.',
      chui: 'A dedicated ClickHouse workspace: development, ingestion, monitoring, governance, cost.',
    },
    {
      aspect: 'ClickHouse support',
      competitor:
        'Official grafana-clickhouse-datasource plugin (Grafana Labs): SQL editor + visual builder, time series, tables, logs, traces; an Altinity community plugin also exists.',
      chui: 'Native throughout: the entire product is built on the ClickHouse client and system tables.',
    },
    {
      aspect: 'Deployment',
      competitor:
        'Self-host Grafana OSS/Enterprise or use Grafana Cloud; plugin installed per instance.',
      chui: 'One self-hosted binary with SQLite state; Docker/Helm; direct connection or outbound tunnel.',
    },
    {
      aspect: 'Dashboards & alerting',
      competitor:
        'Best-in-class: templating, provisioning/dashboards-as-code, unified alerting, on-call ecosystem. Grafana wins this row.',
      chui: 'Dashboards with public links plus alert rules and channels (Pro), deliberately simpler.',
    },
    {
      aspect: 'SQL development',
      competitor:
        'The plugin query editor is built for panels, not for iterative SQL work: no query history, schema workspace, or result exploration.',
      chui: 'Full SQL workspace: streaming results, sort/filter, automatic history, jump-to-error, Ask AI (Pro).',
    },
    {
      aspect: 'ClickHouse operations',
      competitor:
        'Whatever you build: you can hand-write system-table dashboards, and community boards exist, but nothing is ClickHouse-aware out of the box.',
      chui: 'Cluster Health and Query Insights (Pro) prebuilt: per-node replication, merges, mutations, parts, Keeper, backups, query_log percentiles and patterns.',
    },
    {
      aspect: 'Cost visibility',
      competitor: 'Build-it-yourself from system tables.',
      chui: 'Cost Center (Pro): priced compute and storage showback with team attribution and CSV export.',
    },
    {
      aspect: 'Ingestion & modeling',
      competitor: 'None. Grafana visualizes, it does not load or transform data.',
      chui: 'Pipelines (Kafka, S3, webhooks, databases) and dbt-style models with schedules, plus GitHub sync (Pro).',
    },
    {
      aspect: 'Governance & audit',
      competitor:
        'Team sync, data-source permissions, and reporting are Enterprise features; auditing targets Grafana itself.',
      chui: 'Immutable audit of workspace actions and queries, governance catalog and guardrails, SSO/OIDC (Pro).',
    },
    {
      aspect: 'Licensing & price',
      competitor:
        'OSS free (AGPLv3). Grafana Cloud: free tier (3 users, usage limits), Pro from $19/month plus usage; Enterprise from about $25,000/year.',
      chui: 'Core free (Apache-2.0). Pro $1,199/year flat per instance, unlimited users, offline activation.',
    },
  ],
  faq: [
    {
      question: 'Does Grafana work well with ClickHouse?',
      answer:
        'Yes. The official ClickHouse data source plugin is maintained by Grafana Labs, supports SQL and visual query building, and renders time series, tables, logs, and traces, with Grafana alerting on top. For observability dashboards over ClickHouse data it is the established choice.',
    },
    {
      question: 'Does CH-UI replace Grafana?',
      answer:
        'Not for general observability. If Grafana is your metrics wall for Prometheus, Loki, and friends, keep it. CH-UI even exposes a Prometheus /metrics endpoint you can scrape into it. CH-UI replaces the hand-built ClickHouse system-table dashboards and adds the workspace parts Grafana never had: SQL development, ingestion, modeling, governance.',
    },
    {
      question: 'Can I monitor ClickHouse with both?',
      answer:
        'Yes, and the split is natural: Grafana for fleet-wide infrastructure metrics and alert routing, CH-UI for ClickHouse-native depth (replication and Keeper state, merges and mutations, query_log analytics, and cost attribution) without writing the SQL for those panels yourself.',
    },
    {
      question: 'How does pricing differ?',
      answer:
        'Grafana OSS is free (AGPLv3) and self-hosted Enterprise or Grafana Cloud scale with users and usage (Cloud Pro starts at $19/month plus usage; Enterprise around $25,000/year minimum). CH-UI core is free (Apache-2.0) and Pro is a flat $1,199/year per instance regardless of team size.',
    },
  ],
};

export const comparisons: ComparisonData[] = [
  tabixComparison,
  dbeaverComparison,
  metabaseComparison,
  grafanaComparison,
];
