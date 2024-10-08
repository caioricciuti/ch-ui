// theme.config.blog.tsx
import type { NextraThemeLayoutProps } from "nextra";
import { useRouter } from "next/router";

const config = {
  logo: <span>My Blog</span>,
  project: {
    link: "https://github.com/yourusername/your-repo",
  },
  docsRepositoryBase: "https://github.com/yourusername/your-repo/tree/main",
  footer: {
    text: `© ${new Date().getFullYear()} Your Name`,
  },
  useNextSeoProps() {
    const { asPath } = useRouter();
    if (asPath !== "/") {
      return {
        titleTemplate: "%s – My Blog",
      };
    }
  },
  head: (
    <>
      <meta name="viewport" content="width=device-width, initial-scale=1.0" />
      <meta property="og:title" content="My Blog" />
      <meta property="og:description" content="My awesome blog" />
    </>
  ),
};

export default config;

export function Layout({ children }: NextraThemeLayoutProps) {
  return (
    <div>
      <h1>My Theme</h1>
      <div style={{ border: "1px solid" }}>{children}</div>
    </div>
  );
}
