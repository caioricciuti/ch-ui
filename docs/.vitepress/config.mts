import { defineConfig } from "vitepress";

export default defineConfig({
  title: "CH-UI",
  description: "Modern web interface for ClickHouse",

  // Ignore localhost links in examples
  ignoreDeadLinks: [/^http:\/\/localhost/],

  head: [
    ["link", { rel: "icon", type: "image/png", href: "/logo.png" }],
    ["link", { rel: "shortcut icon", type: "image/png", href: "/logo.png" }],
    ["meta", { name: "theme-color", content: "#FF813F" }],
    [
      "script",
      {
        defer: "",
        "data-site": "site_5aa4b1afe72d1e41",
        src: "https://light.yaat.io/s.js",
      },
    ],
  ],

  themeConfig: {
    logo: "/logo.png",

    nav: [
      { text: "Home", link: "/" },
      { text: "Getting Started", link: "/getting-started" },
      { text: "Support", link: "https://buymeacoffee.com/caioricciuti" },
    ],

    sidebar: [
      {
        text: "Getting Started",
        items: [
          { text: "Quick Start", link: "/getting-started" },
          { text: "Environment Variables", link: "/environment-variables" },
          { text: "Troubleshooting", link: "/troubleshooting" },
        ],
      },
      {
        text: "Configuration",
        items: [
          { text: "Reverse Proxy Setup", link: "/reverse-proxy" },
          { text: "Distributed ClickHouse", link: "/distributed-clickhouse" },
          { text: "Permissions Guide", link: "/permissions" },
        ],
      },
      {
        text: "About",
        items: [
          {
            text: "Changelog",
            link: "https://github.com/caioricciuti/ch-ui/releases",
          },
          { text: "Contributing", link: "/contributing" },
          { text: "Acknowledgments", link: "/acknowledgments" },
          { text: "License", link: "/license" },
        ],
      },
    ],

    socialLinks: [
      { icon: "github", link: "https://github.com/caioricciuti/ch-ui" },
    ],

    footer: {
      message: "Released under the MIT License.",
      copyright: "Copyright © 2025 Caio Ricciuti and Ibero Data",
    },

    search: {
      provider: "local",
    },
  },
});
