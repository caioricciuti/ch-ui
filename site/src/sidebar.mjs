// The docs sidebar: the only place that orders the pages. astro.config.mjs
// hands it to Starlight, and pages/llms.txt.ts lists the docs in the same
// groups. A new page needs an entry here, or it is reachable by URL and
// search only. A bare string is a page slug; its label is the page title.
const pro = { text: "Pro", variant: "note" };

export const sidebar = [
  {
    label: "Start here",
    items: [
      { label: "Introduction", slug: "docs" },
      "docs/installation",
      "docs/deployment",
      "docs/configuration",
      "docs/upgrade",
    ],
  },
  {
    label: "Workspace",
    items: [
      "docs/connections",
      "docs/ingestion",
      "docs/querying",
      "docs/dashboards",
      "docs/models",
      "docs/pipelines",
      { slug: "docs/schedules", badge: pro },
      { slug: "docs/github", badge: pro },
    ],
  },
  {
    label: "AI",
    items: ["docs/brain", "docs/mcp"],
  },
  {
    label: "Operate",
    items: [
      "docs/telemetry",
      { slug: "docs/cluster-health", badge: pro },
      { slug: "docs/query-insights", badge: pro },
      { slug: "docs/cost-center", badge: pro },
      { slug: "docs/performance", badge: pro },
      { slug: "docs/incident-timeline", badge: pro },
      { slug: "docs/operations-reports", badge: pro },
      { slug: "docs/schema-compare", badge: pro },
      "docs/clusters",
      "docs/monitoring",
    ],
  },
  {
    label: "Govern and secure",
    items: [
      { slug: "docs/governance", badge: pro },
      { slug: "docs/alerts", badge: pro },
      { slug: "docs/audit-log", badge: pro },
      "docs/authentication",
      { slug: "docs/sso", badge: pro },
      "docs/background-accounts",
      "docs/security",
      "docs/admin",
    ],
  },
  {
    label: "Reference",
    items: [
      "docs/cli",
      "docs/api",
      "docs/compatibility",
      "docs/cant-login",
      "docs/troubleshooting",
      "docs/development",
      "docs/changelog",
    ],
  },
  {
    label: "Plans",
    items: ["docs/pricing", "docs/license"],
  },
];
