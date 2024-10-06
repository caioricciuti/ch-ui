import React from "react";
import Link from "@docusaurus/Link";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import Layout from "@theme/Layout";
import HomepageFeatures from "@site/src/components/HomepageFeatures";
import ProductShowcase from "../components/ShowCase";
import { motion } from "framer-motion";

function HomepageHeader() {
  const { siteConfig } = useDocusaurusContext();
  return (
    <header className="py-24 overflow-hidden">
      <div className="container mx-auto px-4 relative">
        <div className="flex flex-col lg:flex-row items-center justify-around">
          <motion.div
            className="lg:w-1/2 mb-12 lg:mb-0 z-10 ml-0 lg:ml-24"
            initial={{ opacity: 0, x: -50 }}
            animate={{ opacity: 1, x: 0 }}
            transition={{ duration: 0.8 }}
          >
            <h1 className="text-6xl font-bold mb-6 bg-clip-text text-transparent bg-gradient-to-r from-orange-500 to-orange-200 dark:from-orange-700 dark:to-orange-300">
              CH-UI
            </h1>
            <p className="text-4xl mb-8 font-semibold">{siteConfig.tagline}</p>
            <motion.div
              initial={{ opacity: 0, y: 20 }}
              animate={{ opacity: 1, y: 0 }}
              transition={{ duration: 0.5, delay: 0.3 }}
            >
              <Link
                to="/docs/getting-started"
                className="inline-block font-bold text-2xl rounded-full  transition duration-300 transform hover:scale-105 hover:shadow-lg"
              >
                Get Started 🚀 - in 2min ⏱️
              </Link>
            </motion.div>
          </motion.div>
          <motion.div
            className="hidden lg:block lg:w-1/3"
            initial={{ opacity: 0, scale: 0.9 }}
            animate={{ opacity: 1, scale: 1 }}
            transition={{ duration: 0.8 }}
          >
            <img
              src="/img/logo.png"
              alt="CH-UI Logo"
              className="max-w-[400px] m-auto"
            />
          </motion.div>
        </div>
        <div className="absolute top-1/2 left-1/2 transform -translate-x-1/3 -translate-y-1/1 w-full h-full">
          <svg
            viewBox="0 0 200 200"
            xmlns="http://www.w3.org/2000/svg"
            className="w-full h-full opacity-0"
          >
            <path
              fill="#f97316"
              d="M47.1,-57.5C61.9,-46.8,75,-32.5,79.7,-15.3C84.4,1.9,80.6,22,70.3,37.3C60,52.6,43.2,63.1,25.5,68.1C7.8,73.1,-10.7,72.6,-27.7,66.6C-44.7,60.6,-60.1,49,-69.1,33.6C-78.1,18.2,-80.7,-1,-75.6,-17.6C-70.6,-34.2,-57.9,-48.1,-43.5,-58.9C-29,-69.6,-14.5,-77.1,1.3,-78.7C17.1,-80.3,34.1,-75.9,47.1,-57.5Z"
              transform="translate(100 100)"
            />
          </svg>
        </div>
      </div>
    </header>
  );
}
export default function Home(): JSX.Element {
  const { siteConfig } = useDocusaurusContext();
  return (
    <Layout
      title={`Welcome to ${siteConfig.title}`}
      description="CH-UI - Data is better when we see it!"
    >
      <HomepageHeader />
      <main>
        <section>
          <div className="container mx-auto px-4">
            <HomepageFeatures />
          </div>
        </section>
        <section className="py-24">
          <div className="container mx-auto px-4">
            {/* <ProductShowcase /> */}
          </div>
        </section>
      </main>
    </Layout>
  );
}
