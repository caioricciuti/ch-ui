// ProductShowcase.jsx

import React, { useState, useEffect } from "react";
import { motion, AnimatePresence } from "framer-motion";
import useDocusaurusContext from "@docusaurus/useDocusaurusContext";
import { useColorMode } from "@docusaurus/theme-common";

const screenshots = [
  {
    name: "Explore",
    light: "/img/screenshots/explore-light.png",
    dark: "/img/screenshots/explore-dark.png",
    description: "Effortlessly navigate through your data structures",
  },
  {
    name: "Data Sample",
    light: "/img/screenshots/data-sample-light.png",
    dark: "/img/screenshots/data-sample-dark.png",
    description: "View and analyze your data with ease",
  },
  {
    name: "Metrics",
    light: "/img/screenshots/metrics-light.png",
    dark: "/img/screenshots/metrics-dark.png",
    description: "Monitor key performance indicators in real-time",
  },
  {
    name: "Auto-complete",
    light: "/img/screenshots/auto-complete-light.png",
    dark: "/img/screenshots/auto-complete-dark.png",
    description: "Boost productivity with intelligent code suggestions",
  },
  {
    name: "Chart Metric",
    light: "/img/screenshots/chart-metric-light.png",
    dark: "/img/screenshots/chart-metric-dark.png",
    description: "Visualize your data with interactive charts",
  },
];

const ProductShowcase = () => {
  const [activeIndex, setActiveIndex] = useState(0);
  const { colorMode } = useColorMode();
  const { siteConfig } = useDocusaurusContext();

  useEffect(() => {
    const interval = setInterval(() => {
      setActiveIndex((prevIndex) => (prevIndex + 1) % screenshots.length);
    }, 5000);
    return () => clearInterval(interval);
  }, []);

  return (
    <section
      className={`py-24 bg-gradient-to-b ${
        colorMode === "dark"
          ? "from-gray-900 to-gray-800"
          : "from-gray-50 to-white"
      } overflow-hidden`}
    >
      <div className="container mx-auto px-4">
        <h2
          className={`text-4xl font-bold text-center mb-16 ${
            colorMode === "dark" ? "text-gray-200" : "text-gray-800"
          }`}
        >
          Discover {siteConfig.title}'s Powerful Features
        </h2>
        <div className="flex flex-col lg:flex-row items-center justify-between">
          {/* Features List */}
          <div className="w-full lg:w-1/3 mb-12 lg:mb-0">
            {screenshots.map((screenshot, index) => (
              <motion.div
                key={screenshot.name}
                initial={{ opacity: 0, y: 20 }}
                animate={{
                  opacity: index === activeIndex ? 1 : 0.6,
                  y: index === activeIndex ? 0 : 10,
                }}
                transition={{ duration: 0.5 }}
                className="mb-8 cursor-pointer transform hover:scale-105 transition-transform duration-300"
                onClick={() => setActiveIndex(index)}
              >
                <h3
                  className={`text-2xl font-semibold mb-2 ${
                    colorMode === "dark" ? "text-gray-200" : "text-gray-800"
                  }`}
                >
                  {screenshot.name}
                </h3>
                <p
                  className={`${
                    colorMode === "dark" ? "text-gray-400" : "text-gray-600"
                  }`}
                >
                  {screenshot.description}
                </p>
              </motion.div>
            ))}
          </div>

          {/* Image Showcase */}
          <div className="w-full lg:w-2/3 relative">
            <div
              className={`rounded-lg shadow-2xl overflow-hidden ${
                colorMode === "dark" ? "bg-gray-800" : "bg-white"
              }`}
            >
              <AnimatePresence mode="wait">
                <motion.img
                  key={activeIndex}
                  src={
                    colorMode === "dark"
                      ? screenshots[activeIndex].dark
                      : screenshots[activeIndex].light
                  }
                  alt={screenshots[activeIndex].name}
                  initial={{ opacity: 0, scale: 1.1 }}
                  animate={{ opacity: 1, scale: 1 }}
                  exit={{ opacity: 0, scale: 0.9 }}
                  transition={{ duration: 0.5 }}
                  className="w-full h-auto object-cover max-h-96"
                  loading="lazy"
                />
              </AnimatePresence>
            </div>

            {/* Decorative Background Circles */}
            <div
              className={`absolute -bottom-6 -right-6 w-64 h-64 rounded-full opacity-20 blur-3xl ${
                colorMode === "dark" ? "bg-orange-400" : "bg-orange-500"
              }`}
            ></div>
            <div
              className={`absolute -top-6 -left-6 w-64 h-64 rounded-full opacity-20 blur-3xl ${
                colorMode === "dark" ? "bg-blue-400" : "bg-blue-500"
              }`}
            ></div>
          </div>
        </div>
      </div>
    </section>
  );
};

export default ProductShowcase;
