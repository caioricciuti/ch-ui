import { themes as prismThemes } from "prism-react-renderer";
import type { Config } from "@docusaurus/types";
import type * as Preset from "@docusaurus/preset-classic";

const config: Config = {
  title: "ch-ui",
  tagline: "Democratizing data is necessary... together it's possible!",
  favicon: "img/logo.png",

  // Set the production url of your site here
  url: "https://ch-ui.caioricciuti.com",
  // Set the /<baseUrl>/ pathname under which your site is served
  // For GitHub pages deployment, it is often '/<projectName>/'
  baseUrl: "/",

  // GitHub pages deployment config.
  // If you aren't using GitHub pages, you don't need these.
  organizationName: "Caio Ricciuti", // Usually your GitHub org/user name.
  projectName: "ch-ui", // Usually your repo name.

  onBrokenLinks: "throw",
  onBrokenMarkdownLinks: "warn",

  // Even if you don't use internationalization, you can use this field to set
  // useful metadata like html lang. For example, if your site is Chinese, you
  // may want to replace "en" with "zh-Hans".
  i18n: {
    defaultLocale: "en",
    locales: ["en"],
  },

  plugins: ["./src/plugins/umami-plugin"],

  presets: [
    [
      "classic",
      {
        docs: {
          sidebarPath: "./sidebars.ts",
          // Please change this to your repo.
          // Remove this to remove the "edit this page" links.
          editUrl: "https://github.com/caioricciuti/ch-ui/docs/s",
        },
        theme: {
          customCss: "./src/css/custom.css",
        },
      } satisfies Preset.Options,
    ],
  ],

  themeConfig: {
    // Replace with your project's social card
    image: "img/docusaurus-social-card.jpg",
    navbar: {
      title: "ch-ui",
      logo: {
        alt: "ch-ui Logo",
        src: "img/logo.png",
      },
      items: [
        {
          type: "docSidebar",
          sidebarId: "tutorialSidebar",
          position: "left",
          label: "Intro",
        },
        {
          href: "https://github.com/caioricciuti/ch-ui?utm_source=ch-ui-docs&utm_medium=navbar",
          label: "GitHub",
          position: "right",
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
      copyright: `Copyright © ${new Date().getFullYear()} ch-ui - Caio Ricciuti <br/> Built with Docusaurus.`,
    },
    prism: {
      theme: prismThemes.github,
      darkTheme: prismThemes.dracula,
    },
    customFields: {
      // This allows the dev server to be accessed from other devices on the network
      host: "0.0.0.0",
      // Optionally, specify a port (default is 3000)
      port: 3001,
    },
  } satisfies Preset.ThemeConfig,
};

export default config;
