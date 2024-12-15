import React, { useEffect, useState } from "react";
import { useRouter } from "next/router";
import type { DocsThemeConfig } from "nextra-theme-docs";
import { Link, useConfig } from "nextra-theme-docs";
import { useTheme } from "next-themes";
import CustomLogo from "@/components/CustomLogo";
import Footer from "./components/Footer";
import { Sun, Moon } from "lucide-react";

function ThemeToggle() {
  const [mounted, setMounted] = useState(false);
  const { setTheme, resolvedTheme } = useTheme();

  useEffect(() => setMounted(true), []);

  if (!mounted) {
    return null;
  }

  const handleToggle = () => {
    setTheme(resolvedTheme === "dark" ? "light" : "dark");
  };

  return (
    <button onClick={handleToggle} className="p-2 text-current">
      {resolvedTheme === "dark" ? <Moon /> : <Sun />}
    </button>
  );
}

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

  project: {
    link: "https://github.com/caioricciuti/ch-ui",
  },
  footer: {
    content: <Footer />,
  },
  themeSwitch: {
    component: () => null,
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
        <>
          <script
            src="https://umami.caioricciuti.com/script.js"
            data-website-id="ba7d406b-f617-44ac-9c7f-6c3b864fa863"
            async
          />
          <script
            src="https://consent.cookiebot.com/uc.js"
            data-cbid="92463182-1238-4c58-82fb-86dd7fc0c26a"
            data-blockingmode="auto"
            async
          />
        </>
      </>
    );
  },
  editLink: {
    content: "Edit this page on GitHub →",
  },
  feedback: {
    content: null,
  },
  sidebar: {
    defaultMenuCollapseLevel: 1,
    toggleButton: true,
  },
  color: {
    hue: 23,
  },
  navbar: {
    extraContent: () => <ThemeToggle />,
  },
};

export default config;
