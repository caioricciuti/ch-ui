// ch-ui.com. Everything under /docs/ is Markdown in src/content/docs/docs.
// The sidebar below is the only place that orders the pages; a new page
// needs an entry here or it is reachable by URL and search only.
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";

const pro = { text: "Pro", variant: "note" };

export default defineConfig({
  site: "https://ch-ui.com",
  integrations: [
    starlight({
      title: "CH-UI",
      description:
        "The ClickHouse control plane for teams: SQL workspace, dashboards, pipelines, governance and an AI copilot in one binary.",
      logo: { src: "./src/assets/logo.png", replacesTitle: false },
      favicon: "/favicon.svg",
      head: [
        { tag: "link", attrs: { rel: "icon", href: "/favicon.ico", sizes: "32x32" } },
        { tag: "link", attrs: { rel: "apple-touch-icon", href: "/apple-touch-icon.png" } },
        { tag: "meta", attrs: { property: "og:image", content: "https://ch-ui.com/images/og.png" } },
        { tag: "meta", attrs: { name: "twitter:image", content: "https://ch-ui.com/images/og.png" } },
        // Page views, cookieless, on the self-hosted Umami.
        {
          tag: "script",
          attrs: {
            defer: true,
            src: "https://a.caioricciuti.com/b.js",
            "data-website-id": "c56fe216-10f5-433a-9e33-8efd0bbfe2ec",
          },
        },
      ],
      social: [
        { icon: "github", label: "GitHub", href: "https://github.com/caioricciuti/ch-ui" },
      ],
      editLink: {
        baseUrl: "https://github.com/caioricciuti/ch-ui/edit/main/site/",
      },
      components: {
        SocialIcons: "./src/components/HeaderLinks.astro",
      },
      customCss: ["./src/styles/tokens.css", "./src/styles/custom.css"],
      expressiveCode: {
        themes: ["github-dark-dimmed", "github-light"],
        styleOverrides: {
          borderRadius: "6px",
          borderColor: "var(--chui-edge)",
          codeFontFamily: "var(--sl-font-mono)",
          codeFontSize: "0.8125rem",
          codeLineHeight: "1.6",
          codePaddingBlock: "0.75rem",
          codePaddingInline: "1rem",
          codeBackground: "var(--chui-surface-2)",
          uiFontFamily: "var(--sl-font)",
          frames: {
            shadowColor: "transparent",
          },
        },
        // No terminal title bar on shell blocks: three dots and an empty
        // bar cost a line of height on every command.
        defaultProps: { frame: "none" },
      },
      sidebar: [
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
      ],
      lastUpdated: false,
      pagination: true,
    }),
  ],
});
