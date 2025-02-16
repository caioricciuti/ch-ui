import type { ReactNode } from "react";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import Layout from "@theme/Layout";
import HeroSection from "../components/Hero";
import Sponsors from "../components/Sponsors";
import HomepageFeatures from "../components/HomePageFeatures";

export default function Home(): ReactNode {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout
      title={`${siteConfig.title}`}
      description="CH-UI - Get start with ClickHouse UI, a tool to visualize and analyze your data with ease from your self-hosted ClickHouse instance."
    >
      <main>
        <HeroSection />
        <HomepageFeatures />
        <Sponsors />
      </main>
    </Layout>
  );
}
