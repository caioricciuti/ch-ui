import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";
import React from "react";

const config: Config = {
  title: "CH-UI",
  tagline: "Data is better when we see it!",
  favicon: "img/logo.png",

  url: "https://ch-ui.caioricciuti.com",
  baseUrl: "/",
  organizationName: "caioricciuti",
  projectName: "ch-ui",

  onBrokenLinks: "throw",
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
          editUrl: "https://github.com/caioricciuti/ch-ui/edit/main/docs/",
        },
        blog: {
          showReadingTime: true,
          feedOptions: {
            type: ["rss", "atom"],
            xslt: true,
          },
          editUrl: "https://github.com/caioricciuti/ch-ui/edit/main/docs/",
          onInlineTags: "warn",
          onInlineAuthors: "warn",
          onUntruncatedBlogPosts: "warn",
        },
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    image: "img/logo.png",
    colorMode: {
      defaultMode: "dark",
      disableSwitch: false,
      respectPrefersColorScheme: false,
    },
    navbar: {
      title: "CH-UI",
      logo: {
        alt: "CH-UI Logo",
        src: "img/logo.png",
      },
      items: [
        {
          type: "docSidebar",
          sidebarId: "tutorialSidebar",
          position: "left",
          label: "Docs",
        },
        { to: "/blog", label: "Blog", position: "left" },
        {
          to: "https://github.com/caioricciuti/ch-ui",
          label: "GitHub",
          position: "right",
          // Add a custom icon
        },
        {
          to: "https://join.slack.com/t/ch-ui-v2/shared_invite/zt-2r6xwoizm-PMlCH6ZZAi5hK3fPagypQw",
          position: "right",
          label: "Slack",
          // Add a custom icon
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
              label: "Intro",
              to: "/docs/introduction",
            },
            {
              label: "Get Started",
              to: "/docs/getting-started",
            },
            {
              label: "Contributing",
              to: "/docs/contributing",
            },
          ],
        },
        {
          title: "Community",
          items: [
            {
              label: "Slack",
              href: "https://join.slack.com/t/ch-ui-v2/shared_invite/zt-2r6xwoizm-PMlCH6ZZAi5hK3fPagypQw",
            },
          ],
        },
        {
          title: "More",
          items: [
            {
              label: "Blog",
              to: "/blog",
            },
            {
              label: "GitHub",
              href: "https://github.com/caioricciuti/ch-ui?utm_source=ch-ui-docs&utm_medium=footer",
            },
          ],
        },
      ],
      copyright: `${new Date().getFullYear()} CH-UI made with ❤`,
    },

    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,

  scripts: [
    {
      src: "https://umami.caioricciuti.com/script.js",
      "data-website-id": "ba7d406b-f617-44ac-9c7f-6c3b864fa863",
      async: true,
    },
    {
      src: "https://consent.cookiebot.com/uc.js",
      "data-cbid": "92463182-1238-4c58-82fb-86dd7fc0c26a",
      "data-blockingmode": "auto",
      async: true,
    },
  ],
};

export default config;
