import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

const config: Config = {
  title: "CH-UI",
  tagline: "Reaching for making data accessible for everyone...",
  favicon: "img/logo.png",
  url: "https://ch-ui.caioricciuti.com",
  baseUrl: "/",
  organizationName: "Caio Ricciuti",
  projectName: "ch-ui",
  onBrokenLinks: "throw",
  onBrokenMarkdownLinks: "warn",
  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  plugins: ["./src/plugins/umami-plugin"],

  presets: [
    [
      "@docusaurus/preset-classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          editUrl: "https://github.com/caioricciuti/ch-ui/docs/s",
        },
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
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
          label: "Documentation",
        },
        {
          type: "html",
          position: "right",
          value:
            '<div><a href="https://github.com/caioricciuti/ch-ui?utm_source=ch-ui-docs&utm_medium=header" target="_blank"><img style="margin-top:6px" src="https://img.shields.io/github/stars/caioricciuti/ch-ui?style=social" alt="GitHub stars" /></a></div>',
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
              to: "/docs/intro",
            },
            {
              label: "Get Started",
              to: "/docs/documentation/getstarted",
            },
          ],
        },
      ],
      copyright: `Copyright © ${new Date().getFullYear()} CH-UI <br /> Made with ❤`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
  } satisfies Preset.ThemeConfig,

  customFields: {
    github: {
      username: "caioricciuti",
      repoName: "ch-ui",
    },
    host: "0.0.0.0",
    port: 3001,
  },
};

export default config;
