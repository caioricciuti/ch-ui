import type { ReactNode } from "react";
import Layout from "@theme/Layout";
import HeroSection from "../components/Hero";
import Sponsors from "../components/Sponsors";
import HomepageFeatures from "../components/HomePageFeatures";

export default function Home(): ReactNode {
  return (
    <Layout>
      <main>
        <HeroSection />
        <HomepageFeatures />
        <Sponsors />
      </main>
    </Layout>
  );
}
