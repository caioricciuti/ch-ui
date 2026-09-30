// ch-ui.com. The landing pages are in src/pages on layouts/Landing.astro;
// everything under /docs/ is Markdown in src/content/docs/docs, ordered by
// src/sidebar.mjs. The changelog page is generated from ../CHANGELOG.md by
// scripts/sync-changelog.ts before every build.
import { defineConfig } from "astro/config";
import starlight from "@astrojs/starlight";
import { sidebar } from "./src/sidebar.mjs";

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
        Footer: "./src/components/DocsFooter.astro",
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
      sidebar,
      lastUpdated: false,
      pagination: true,
    }),
  ],
});
