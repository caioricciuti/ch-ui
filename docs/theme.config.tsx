import { useRouter } from "next/router";
import type { DocsThemeConfig } from "nextra-theme-docs";
import { Link, useConfig } from "nextra-theme-docs";
import CustomLogo from "@/components/CustomLogo";
import SlackLogo from "@/components/SlackLogo";

const config: DocsThemeConfig = {
  banner: {
    key: "CH-UI",
    content: (
      <div className='before:content-["🚀_"]'>
        Start working with your data!{" "}
        <Link href="/docs" className='after:content-["_→"]'>
          How to use CH-UI
        </Link>
      </div>
    ),
  },
  chat: {
    link: "https://join.slack.com/t/ch-ui-v2/shared_invite/zt-2r6xwoizm-PMlCH6ZZAi5hK3fPagypQw",
    icon: <SlackLogo />,
  },
  project: {
    link: "https://github.com/caioricciuti/ch-ui",
  },
  footer: {
    content: <></>,
  },
  docsRepositoryBase: "https://github.com/caioricciuti/ch-ui/tree/main/docs",
  logo: <CustomLogo />,
  head: function useHead() {
    const config = useConfig();
    const { route } = useRouter();
    const isDefault = route === "/" || !config.title;
    const isBlog = route.startsWith("/blog");
    const image =
      "https://ch-ui.caioricciuti.com/" +
      (isDefault ? "og.png" : `api/og?title=${config.title}`);
    const description =
      config.frontMatter.description ||
      (isBlog
        ? "Insights and updates from the CH-UI team."
        : "Make beautiful websites with Next.js & MDX.");
    const title =
      config.title +
      (route === "/" ? "" : isBlog ? " - CH-UI Blog" : " - CH-UI");

    return (
      <>
        <title>{title}</title>
        <meta property="og:title" content={title} />
        <meta name="description" content={description} />
        <meta property="og:description" content={description} />
        <meta property="og:image" content={image} />

        <meta name="msapplication-TileColor" content="#fff" />
        <meta httpEquiv="Content-Language" content="en" />
        <meta
          name="apple-mobile-web-app-title"
          content="CH-UI | Just work with your data"
        />
        <link rel="icon" href="/logo.png" type="image/svg+xml" />
        <link rel="icon" href="/logo.png" type="image/png" />
        <link
          rel="icon"
          href="/favicon-dark.svg"
          type="image/svg+xml"
          media="(prefers-color-scheme: dark)"
        />
        <link
          rel="icon"
          href="/favicon-dark.png"
          type="image/png"
          media="(prefers-color-scheme: dark)"
        />
      </>
    );
  },
  editLink: {
    content: "Edit this page on GitHub →",
  },
  sidebar: {
    defaultMenuCollapseLevel: 1,
    toggleButton: true,
  },
  color: {
    hue: 23,
  },
};

export default config;
