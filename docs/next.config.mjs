import nextra from "nextra";

const withNextra = nextra({
  theme: "nextra-theme-docs",
  themeConfig: "./theme.config.tsx",
  search: {
    codeblocks: false,
  },
});

export default withNextra({
  reactStrictMode: true,
  images: {
    domains: ["avatars.githubusercontent.com"],
  },
});
