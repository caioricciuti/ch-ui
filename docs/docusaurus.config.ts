import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

const config: Config = {
  title: "CH-UI: ClickHouse Data Visualization & Management Made Easy", // More specific, includes key terms
  tagline:
    "Effortlessly connect to your ClickHouse instance, visualize your data, and gain insights with our intuitive open-source UI.", // Benefit-oriented tagline
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
          priority: 0.7, // Increased priority
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
          "CH-UI is an open-source ClickHouse UI for data visualization, analytics, and management.  Quickly connect to your ClickHouse instance and start exploring your data.", // Improved description
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "keywords",
        content:
          "ClickHouse UI, ClickHouse GUI, data visualization, data analytics, data management, open source, ClickHouse client, query ClickHouse, ClickHouse charts, CH-UI", // More targeted keyword list
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
        content: "CH-UI: Your Open-Source ClickHouse Data Visualization Tool", // More compelling OG title
      },
    },
    {
      tagName: "meta",
      attributes: {
        property: "og:description",
        content:
          "Connect to your ClickHouse instance in seconds and visualize your data with ease using CH-UI, the open-source ClickHouse UI.", // Improved OG description
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
        content:
          "CH-UI: Open-Source ClickHouse UI for Effortless Data Visualization", // Improved Twitter title
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "twitter:description",
        content:
          "Visualize, query, and manage your ClickHouse data with CH-UI.  A free, open-source tool for connecting to your ClickHouse instance and gaining data insights.", // Improved Twitter description
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
        content: "CH-UI", // Capitalized
      },
    },
    {
      tagName: "meta",
      attributes: {
        name: "apple-mobile-web-app-title",
        content: "CH-UI", // Capitalized
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
        alt: "CH-UI Logo", // Capitalized for consistency
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
      copyright: `Copyright © ${new Date().getFullYear()} CH-UI. All rights reserved.`, // Added copyright symbol
    },
    prism: {
      theme: prismThemes.dracula,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
