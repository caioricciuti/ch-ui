import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

const config: Config = {
  title: "CH-UI makes working with data easy.",
  tagline:
    "This UI connects you directly to your ClickHouse instance, allowing you to view, filter, and export your data with ease.",
  favicon: "img/logo.png",
  url: "https://ch-ui.com",
  baseUrl: "/",

  organizationName: "caioricciuti",
  projectName: "ch-ui",
  deploymentBranch: "gh-pages",

  onBrokenLinks: "warn",
  onBrokenMarkdownLinks: "warn",

  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          editUrl:
            "https://github.com/caioricciuti/ch-ui/tree/main/packages/create-docusaurus/templates/shared/",
          showLastUpdateAuthor: true,
          showLastUpdateTime: true,
          breadcrumbs: true,
        },

        theme: {
          customCss: "./src/css/custom.css",
        },
        sitemap: {
          changefreq: "weekly",
          priority: 0.5,
          ignorePatterns: ["/tags/**", "/page/*"],
          filename: "sitemap.xml",
        },
      } satisfies Preset.Options,
    ],
  ],

  headTags: [
    {
      tagName: "meta",
      attributes: {
        name: "description",
        content:
          "CH-UI data visualization,charts, analytics, open source, ClickHouse UI tool, data analysis, data tool",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "keywords",
        content:
          "data visualization,charts, analytics, open source, ClickHouse UI tool, data analysis, data tool",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "author",
        content: "Caio Ricciuti",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "viewport",
        content: "width=device-width, initial-scale=1.0",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "robots",
        content: "index, follow",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:title",
        content: "CH-UI: Data Made Easy",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:description",
        content:
          "Simply connect with your Clickhouse instance and start quering!",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:image",
        content: "https://ch-ui.com/img/social-card.png",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:image:width",
        content: "1200",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:image:height",
        content: "630",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:url",
        content: "https://ch-ui.com",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:type",
        content: "website",
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:site_name",
        content: "CH-UI Documentation",
      },
    },

    // Twitter Card
    {
      tagName: "meta",
      attributes: {
        name: "twitter:card",
        content: "summary_large_image",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "twitter:site",
        content: "@caioricciuti",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "twitter:creator",
        content: "@caioricciuti",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "twitter:title",
        content: "CH-UI: Working with data Made Easy",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "twitter:description",
        content:
          "Get started with CH-UI and use our app to work with ease on your ClickHouse instave. Open-source tool for connecting with your ClickHouse instance.",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "twitter:image",
        content: "https://ch-ui.com/img/social-card.png",
      },
    },

    // Additional SEO
    {
      tagName: "meta",
      attributes: {
        name: "application-name",
        content: "ch-ui",
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "apple-mobile-web-app-title",
        content: "ch-ui",
      },
    },
    {
      tagName: "link",
      attributes: {
        rel: "canonical",
        href: "https://ch-ui.com",
      },
    },
  ],

  scripts: [
    {
      src: "https://umami.ch-ui.com/script.js",
      async: true,
      defer: true,
      "data-website-id": "2fd4b78c-f4f2-40c8-bf84-f8d3af064a02",
    },
  ],

  themeConfig: {
    colorMode: {
      defaultMode: "dark",
      disableSwitch: true,
      respectPrefersColorScheme: false,
    },
    // Search
    algolia: {
      appId: "19STL6LT3R",
      apiKey: "faac7ca11f33ce85cee1dd5e53569ce1",
      indexName: "CH-UI Crawler",
      contextualSearch: true,
    },
    // Default image for social media sharing
    image: "img/social-card.png",
    // Navbar configuration
    navbar: {
      title: "CH-UI",
      logo: {
        alt: "ch-ui Logo",
        src: "img/logo.png",
      },
      items: [
        {
          type: "docSidebar",
          sidebarId: "tutorialSidebar",
          position: "right",
          label: "Docs",
        },
        {
          href: "https://buymeacoffee.com/caioricciuti",
          position: "right",
          label: "Donate",
        },
        {
          href: "https://github.com/caioricciuti/ch-ui",
          position: "right",
          className: "header-github-link",
          "aria-label": "GitHub repository",
        },
      ],
    },
    footer: {
      style: "dark",
      links: [
        {
          title: "Docs",
          items: [
            {
              label: "Getting Started",
              to: "/docs/getting-started",
            },
            {
              label: "Legal",
              to: "/docs/legal",
            },
          ],
        },
        {
          title: "Community",
          items: [
            {
              label: "GitHub",
              href: "https://github.com/caioricciuti/ch-ui",
            },
          ],
        },
        {
          title: "More",
          items: [
            {
              label: "Donate",
              href: "https://buymeacoffee.com/caioricciuti",
            },
          ],
        },
      ],
      copyright: `${new Date().getFullYear()} CH-UI. All rights reserved.`,
    },
    prism: {
      theme: prismThemes.dracula,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
